// SPDX-License-Identifier: LGPL-3.0-only
// Package kylok embeds enrollment, app delivery and device policies in a
// host-owned HTTP or HTTPS server, with durable defaults and optional adapters.
// Public APIs are experimental until the v1 release gates pass.
package kylok

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"github.com/thienvu18/kylok/go/adapters/filesystem"
	"github.com/thienvu18/kylok/go/adapters/sqlite"
	kylokv1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/gen/kylok/v1/kylokv1connect"
	"github.com/thienvu18/kylok/go/ports"
	"github.com/thienvu18/kylok/go/provisioning"
)

// Options configures one embeddable SDK. Supply verified bootstrap content and a
// device-reachable origin; durable defaults use DataDir. Supplied providers remain host-owned.
type Options struct {
	// HTTP/HTTPS origin advertised to devices; it does not bind or start a listener.
	PublicURL string
	// Signed DPC APK verified by provisioning.Inspect.
	Bootstrap *provisioning.APK
	// Persistent default database/artifact directory; unnecessary when both providers are supplied.
	DataDir string
	// Optional concurrent transactional provider; never replaced with an in-memory fallback on failure.
	Store ports.StateStore
	// Optional immutable content provider; must preserve the ArtifactStore integrity contract.
	Artifacts ports.ArtifactStore
	// Explicit opt-in for local provisioning proofs; disabled for normal hosts.
	AllowDebugAPK bool
	// Optional wall clock for deterministic expiry/deadline checks; defaults to time.Now.
	Clock func() time.Time
	// Optional APK origin. Defaults to PublicURL; independently supports HTTP/HTTPS
	// and domain/IP hosts. Mount BootstrapHandler on a separate origin as needed.
	BootstrapURL string
	// Optional sanitized best-effort committed-event sink; see EventSink for cancellation/delivery rules.
	Events EventSink
	// Per callback; defaults to one second, at most 30 seconds. A non-cooperative
	// host callback can stall only its one worker, never authentication or sync.
	EventTimeout time.Duration
}

// SDK owns the management facade and agent handlers. Hosts own authorization and
// listeners, drain requests before Close, and close their own supplied providers.
type SDK struct {
	store         ports.StateStore
	artifacts     ports.ArtifactStore
	bootstrap     *provisioning.APK
	origin        string
	bootstrapURL  string
	clock         func() time.Time
	closeStore    func() error
	closed        atomic.Bool
	closeOnce     sync.Once
	closeErr      error
	allowDebugAPK bool
	events        *eventDispatcher
}
type Enrollment struct {
	CorrelationID string
	Token         string
	ScopeID       string
	ExpiresAt     time.Time
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var devicePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// New validates options/current storage schema and registers the verified APK.
// It starts no listeners. An optional event sink uses one bounded SDK worker.
// The host owns TLS, authorization, supplied providers and shutdown.
// Custom providers remain host-owned even if construction fails.
// Usage: sdk, err := kylok.New(ctx, kylok.Options{PublicURL: origin, Bootstrap: apk, DataDir: dataDir}).
func New(ctx context.Context, o Options) (_ *SDK, err error) {
	if o.EventTimeout == 0 {
		o.EventTimeout = time.Second
	}
	if o.EventTimeout < time.Millisecond || o.EventTimeout > 30*time.Second {
		return nil, errors.New("EventTimeout must be 1ms–30s")
	}
	for _, provider := range []any{o.Store, o.Artifacts} {
		if provider != nil && reflect.ValueOf(provider).Kind() == reflect.Ptr && reflect.ValueOf(provider).IsNil() {
			return nil, errors.New("typed nil persistence provider")
		}
	}
	origin, err := provisioning.Origin(o.PublicURL)
	if err != nil {
		return nil, err
	}
	bootstrapURL := origin
	if o.BootstrapURL != "" {
		bootstrapURL, err = provisioning.Origin(o.BootstrapURL)
		if err != nil {
			return nil, err
		}
	}
	if !o.Bootstrap.IsDPC() {
		return nil, errors.New("verified Bootstrap APK required")
	}
	if o.Bootstrap.Metadata().Debuggable && !o.AllowDebugAPK {
		return nil, errors.New("debuggable bootstrap APK requires explicit AllowDebugAPK")
	}
	if o.Store == nil || o.Artifacts == nil {
		if o.DataDir == "" {
			return nil, errors.New("DataDir required for default persistence providers")
		}
		if err = os.MkdirAll(o.DataDir, 0700); err != nil {
			return nil, fmt.Errorf("create DataDir: %w", err)
		}
	}
	s := &SDK{origin: origin, bootstrapURL: bootstrapURL, bootstrap: o.Bootstrap, store: o.Store, artifacts: o.Artifacts, clock: o.Clock, allowDebugAPK: o.AllowDebugAPK}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	if s.clock == nil {
		s.clock = time.Now
	}
	if s.store == nil {
		store, e := sqlite.Open(ctx, filepath.Join(o.DataDir, "kylok.sqlite"))
		if e != nil {
			return nil, e
		}
		s.store, s.closeStore = store, store.Close
	}
	if s.artifacts == nil {
		s.artifacts, err = filesystem.Open(filepath.Join(o.DataDir, "artifacts"))
		if err != nil {
			return nil, err
		}
	}
	metadata := s.bootstrap.Metadata()
	if err = s.artifacts.Put(ctx, metadata.SHA256, s.bootstrap.Bytes()); err != nil {
		return nil, fmt.Errorf("persist bootstrap: %w", err)
	}
	reader, err := s.artifacts.Open(ctx, metadata.SHA256)
	if err != nil {
		return nil, fmt.Errorf("open persisted bootstrap: %w", err)
	}
	actual, readErr := io.ReadAll(io.LimitReader(reader, provisioning.MaxAPKBytes+1))
	closeErr := reader.Close()
	if err = errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if !bytes.Equal(actual, s.bootstrap.Bytes()) {
		return nil, errors.New("artifact provider returned different bootstrap content")
	}
	if err = s.store.RegisterArtifact(ctx, artifactMetadata("", s.bootstrap)); err != nil {
		return nil, err
	}
	s.events = newEvents(o.Events, o.EventTimeout)
	return s, nil
}

// Close cancels SDK-owned notifications and storage exactly once; supplied providers remain host-owned.
// Usage: defer sdk.Close() after draining host listeners; hosts close supplied stores separately.
func (s *SDK) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.events.close()
		if s.closeStore != nil {
			s.closeErr = s.closeStore()
		}
	})
	return s.closeErr
}

// Generate 256 unpredictable bits for opaque single-use tokens, identities and device credentials.
func secret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Store a fixed-size verifier instead of persisting a recoverable bearer secret.
func hash(value string) []byte { h := sha256.Sum256([]byte(value)); return h[:] }

// CreateEnrollment creates a single-use token bound to the authorized scope and current verified DPC APK.
// Usage: enrollment, err := sdk.CreateEnrollment(ctx, scope, 15*time.Minute).
func (s *SDK) CreateEnrollment(ctx context.Context, scope string, ttl time.Duration) (Enrollment, error) {
	return s.CreateEnrollmentWithOptions(ctx, EnrollmentOptions{ScopeID: scope, TTL: ttl})
}

// CreateEnrollmentWithOptions creates an opaque correlation binding and a private single-use token.
// Nonempty correlation IDs are unique in their scope, including after consumption/expiry.
// Duplicate creation returns ErrConflict, never the original token. If the creation response
// is lost, use a new correlation ID and let the unclaimed token expire; no read exposes it.
// Usage: e, err := sdk.CreateEnrollmentWithOptions(ctx, EnrollmentOptions{ScopeID: scope, TTL: 15*time.Minute, CorrelationID: requestID}).
func (s *SDK) CreateEnrollmentWithOptions(ctx context.Context, input EnrollmentOptions) (Enrollment, error) {
	if err := s.checkOpen(ctx); err != nil {
		return Enrollment{}, err
	}
	if !idPattern.MatchString(input.ScopeID) || input.TTL < time.Minute || input.TTL > 24*time.Hour || (input.CorrelationID != "" && !idPattern.MatchString(input.CorrelationID)) {
		return Enrollment{}, fmt.Errorf("%w: scope/correlation invalid; TTL must be 1 minute to 24 hours", ErrInvalidArgument)
	}
	token, err := secret()
	if err != nil {
		return Enrollment{}, err
	}
	expiry := s.clock().UTC().Add(input.TTL).Truncate(time.Millisecond)
	e := ports.Enrollment{TokenHash: hash(token), ScopeID: input.ScopeID, ExpiresAt: expiry, ArtifactSHA256: s.bootstrap.Metadata().SHA256, CorrelationID: input.CorrelationID}
	if err = s.store.CreateEnrollment(ctx, e); err != nil {
		return Enrollment{}, err
	}
	return Enrollment{Token: token, ScopeID: input.ScopeID, ExpiresAt: expiry, CorrelationID: input.CorrelationID}, nil
}

// QR renders private provisioning JSON using this host's configured API and bootstrap origins.
// Usage: payload, err := sdk.QR(ctx, enrollment.Token); keep the returned JSON private.
func (s *SDK) QR(ctx context.Context, token string) ([]byte, error) {
	return s.QRWithOrigins(ctx, token, s.origin, s.bootstrapURL)
}

// QRWithOrigins issues the same verified enrollment through host-selected API
// and APK origins, for example another bound LAN interface or a custom domain.
// Changing a QR does not change an already enrolled device's endpoint.
// Usage: payload, err := sdk.QRWithOrigins(ctx, token, "http://192.168.1.5:8080", "http://192.168.1.5:8080").
func (s *SDK) QRWithOrigins(ctx context.Context, token, apiOrigin, bootstrapOrigin string) ([]byte, error) {
	if err := s.checkOpen(ctx); err != nil {
		return nil, err
	}
	apiOrigin, err := provisioning.Origin(apiOrigin)
	if err != nil {
		return nil, err
	}
	bootstrapOrigin, err = provisioning.Origin(bootstrapOrigin)
	if err != nil {
		return nil, err
	}
	e, err := s.store.Enrollment(ctx, hash(token))
	if err != nil {
		return nil, err
	}
	if !s.clock().Before(e.ExpiresAt) {
		return nil, ports.ErrExpired
	}
	if e.ArtifactSHA256 != s.bootstrap.Metadata().SHA256 {
		return nil, errors.New("enrollment references a different bootstrap APK")
	}
	extras := map[string]string{"kylok.server": apiOrigin, "kylok.enrollment_token": token}
	return s.bootstrap.QR(bootstrapOrigin, extras)
}

// ListDevices and RevokeDevice are in-process management APIs. Hosts must authorize
// the business user and scope before invoking them; no remote management route exists.
// Usage: devices, err := sdk.ListDevices(ctx, scope, "", 100).
func (s *SDK) ListDevices(ctx context.Context, scope, after string, limit int) ([]Device, error) {
	if !idPattern.MatchString(scope) || limit < 1 || limit > 100 {
		return nil, invalidArgument("scope required; limit must be 1–100")
	}
	if err := s.checkOpen(ctx); err != nil {
		return nil, err
	}
	if after != "" && !devicePattern.MatchString(after) {
		return nil, ErrInvalidArgument
	}
	values, err := s.store.ListDevices(ctx, scope, after, limit)
	return publicDevices(values), err
}

// RevokeDevice rejects future authenticated activity and recovery for one authorized scope/device.
// Usage: err := sdk.RevokeDevice(ctx, scope, deviceID).
func (s *SDK) RevokeDevice(ctx context.Context, scope, id string) error {
	if err := s.checkOpen(ctx); err != nil {
		return err
	}
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(id) {
		return invalidArgument("scope and device ID required")
	}
	err := s.store.Revoke(ctx, scope, id)
	if err == nil {
		s.emit("device_revoked", scope, id, 0, "")
	}
	return err
}

// Handler exposes agent RPCs, public bootstrap APKs and authorized managed APKs,
// never administrator routes. Hosts enable HTTP/2 (including h2c for HTTP). TLS
// is required for HTTPS origins; forwarded TLS headers are not trusted.
// Usage: server.Handler = sdk.Handler(); enable HTTP/2 and drain the listener before sdk.Close().
func (s *SDK) Handler() http.Handler {
	mux := http.NewServeMux()
	path, handler := kylokv1connect.NewAgentServiceHandler(&agentService{s}, connect.WithReadMaxBytes(1<<20))
	mux.Handle(path, handler)
	mux.Handle("/bootstrap/", s.bootstrapHandler())
	mux.Handle("/artifacts/", s.applicationHandler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.closed.Load() {
			http.Error(w, "SDK closed", http.StatusServiceUnavailable)
			return
		}
		if strings.HasPrefix(s.origin, "https://") && r.TLS == nil {
			http.Error(w, "HTTPS required", http.StatusUpgradeRequired)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// BootstrapHandler serves only immutable APKs, never agent/management routes or QR secrets.
// It follows the selected BootstrapURL scheme, independently of the API origin.
// Usage: bootstrapServer.Handler = sdk.BootstrapHandler().
func (s *SDK) BootstrapHandler() http.Handler {
	handler := s.bootstrapHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.closed.Load() {
			http.Error(w, "SDK closed", http.StatusServiceUnavailable)
			return
		}
		if strings.HasPrefix(s.bootstrapURL, "https://") && r.TLS == nil {
			http.Error(w, "HTTPS required", http.StatusUpgradeRequired)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

// Serve only explicitly public bootstrap catalog entries and verify their retained bytes before delivery.
func (s *SDK) bootstrapHandler() http.Handler {
	// Keep already-issued bootstrap URLs valid after a host APK upgrade/restart.
	// Only empty-scope catalog entries are public; managed app bytes stay authorized.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.closed.Load() {
			http.Error(w, "SDK closed", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		digest := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/bootstrap/"), ".apk")
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != 32 || r.URL.Path != "/bootstrap/"+digest+".apk" {
			http.NotFound(w, r)
			return
		}
		if _, err := s.store.Artifact(r.Context(), "", digest); err != nil {
			http.NotFound(w, r)
			return
		}
		reader, err := s.artifacts.Open(r.Context(), digest)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer reader.Close()
		data, err := io.ReadAll(io.LimitReader(reader, provisioning.MaxAPKBytes+1))
		actual := sha256.Sum256(data)
		if err != nil || len(data) > provisioning.MaxAPKBytes || hex.EncodeToString(actual[:]) != digest {
			http.Error(w, "Artifact integrity failure", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.android.package-archive")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", `"`+digest+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, digest+".apk", time.Time{}, bytes.NewReader(data))
	})
}

// Project stored self-reported inventory into the protocol without adding support or attestation claims.
func inventoryMessage(i ports.Inventory) *kylokv1.Inventory {
	result := &kylokv1.Inventory{ApiLevel: i.APILevel, Model: i.Model, BuildFingerprint: i.BuildFingerprint, AgentVersion: i.AgentVersion, DeviceOwner: i.DeviceOwner, Capabilities: i.Capabilities, SupportedAbis: i.SupportedABIs}
	for _, app := range i.Applications {
		result.Applications = append(result.Applications, &kylokv1.InstalledApplication{PackageName: app.PackageName, VersionCode: app.VersionCode, SigningCertificates: app.SigningCertificates})
	}
	return result
}

// Expose public device identity, revisions and timestamps without its credential verifier.
func deviceMessage(d ports.Device) *kylokv1.Device {
	state := kylokv1.DeviceState_DEVICE_STATE_ENROLLED
	if d.State == "MANAGED" {
		state = kylokv1.DeviceState_DEVICE_STATE_MANAGED
	}
	if d.State == "REVOKED" {
		state = kylokv1.DeviceState_DEVICE_STATE_REVOKED
	}
	var seen int64
	if !d.LastSeenAt.IsZero() {
		seen = d.LastSeenAt.UnixMilli()
	}
	return &kylokv1.Device{Id: d.ID, ScopeId: d.ScopeID, State: state, Inventory: inventoryMessage(d.Inventory), CreatedAtUnixMillis: d.CreatedAt.UnixMilli(), LastSeenAtUnixMillis: seen, DesiredRevision: d.DesiredRevision, ReportedRevision: d.ReportedRevision}
}
