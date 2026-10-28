// SPDX-License-Identifier: LGPL-3.0-only
package kylok

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

// Management errors support errors.Is independently of transport and persistence adapters.
var (
	ErrInvalidArgument = ports.ErrInvalidArgument
	ErrNotFound        = ports.ErrNotFound
	ErrConflict        = ports.ErrConflict
	ErrExpired         = ports.ErrExpired
	ErrReferenced      = ports.ErrReferenced
	ErrConsumed        = ports.ErrConsumed
	ErrRecoveryExpired = ports.ErrRecoveryExpired
	ErrRevoked         = ports.ErrRevoked
	ErrUnauthorized    = ports.ErrUnauthorized
	ErrStale           = ports.ErrStale
	ErrClosed          = errors.New("SDK closed")
)

// EnrollmentOptions binds a private token to an authorized scope and optional opaque host correlation.
// CorrelationID is association metadata, never authorization; it must not contain secrets.
type EnrollmentOptions struct {
	ScopeID       string
	TTL           time.Duration
	CorrelationID string
}

// Device is a detached host-facing identity and observation. CorrelationID is host-only association metadata.
type Device struct {
	ID               string    `json:"id"`
	ScopeID          string    `json:"scope_id"`
	CorrelationID    string    `json:"correlation_id,omitempty"`
	State            string    `json:"state"`
	Inventory        Inventory `json:"inventory"`
	CreatedAt        time.Time `json:"created_at"`
	LastSeenAt       time.Time `json:"last_seen_at"`
	DesiredRevision  uint64    `json:"desired_revision"`
	ReportedRevision uint64    `json:"reported_revision"`
}

// Inventory contains device-reported observations, not attestation or a firmware support guarantee.
type Inventory struct {
	APILevel         uint32                 `json:"api_level"`
	Model            string                 `json:"model"`
	BuildFingerprint string                 `json:"build_fingerprint"`
	AgentVersion     string                 `json:"agent_version"`
	DeviceOwner      bool                   `json:"device_owner"`
	Capabilities     []string               `json:"capabilities"`
	Applications     []InstalledApplication `json:"applications"`
	SupportedABIs    []string               `json:"supported_abis"`
}

// InstalledApplication identifies an observed package version and signing certificates.
type InstalledApplication struct {
	PackageName         string   `json:"package_name"`
	VersionCode         int64    `json:"version_code"`
	SigningCertificates []string `json:"signing_certificates"`
}

// Artifact is immutable verified application metadata, with no storage location or download credentials.
type Artifact struct {
	ScopeID             string   `json:"scope_id"`
	SHA256              string   `json:"sha256"`
	PackageName         string   `json:"package_name"`
	VersionCode         int64    `json:"version_code"`
	MinSDK              uint32   `json:"min_sdk"`
	MaxSDK              uint32   `json:"max_sdk"`
	SigningCertificates []string `json:"signing_certificates"`
	SupportedABIs       []string `json:"supported_abis"`
	SizeBytes           int64    `json:"size_bytes"`
}

// Operation exposes durable progress/results without agent lease tokens or report checkpoint internals.
type Operation struct {
	ID              string                `json:"id"`
	DeviceID        string                `json:"device_id"`
	Artifact        Artifact              `json:"artifact"`
	Revision        uint64                `json:"revision"`
	State           string                `json:"state"`
	NotBefore       time.Time             `json:"not_before"`
	Deadline        time.Time             `json:"deadline"`
	DownloadedBytes int64                 `json:"downloaded_bytes"`
	ErrorCode       string                `json:"error_code"`
	ErrorMessage    string                `json:"error_message"`
	Observed        *InstalledApplication `json:"observed,omitempty"`
}

// DeviceFilter selects exact state/correlation matches within one authorized scope.
type DeviceFilter struct {
	State         string
	CorrelationID string
}

// OperationFilter selects an exact operation state for one scoped device.
type OperationFilter struct{ State string }

// PageRequest selects a live ascending-ID page; zero Limit defaults to 20, and the maximum is 100.
// Cursor is opaque and bound to the scope/device/filter; authorization is still required on every request.
type PageRequest struct {
	Cursor string
	Limit  int
}

// DevicePage returns detached observations and a cursor only when more matching results exist.
type DevicePage struct {
	Items      []Device
	NextCursor string
}

// OperationPage returns detached progress/results and a cursor only when more matching results exist.
type OperationPage struct {
	Items      []Operation
	NextCursor string
}

type pageCursor struct {
	Version int    `json:"v"`
	Binding string `json:"b"`
	After   string `json:"a"`
}

const defaultPageSize = 20
const maximumPageSize = 100
const maximumCursorBytes = 1024

// Reject work after Close, without closing or claiming ownership of supplied providers.
func (s *SDK) checkOpen(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed.Load() {
		return ErrClosed
	}
	return nil
}

// Ready checks current backend/bootstrap availability without mutating state or starting a listener.
// Drain host handlers before Close. Supplied providers remain host-owned and readiness is a point-in-time observation.
// Usage: if err := sdk.Ready(ctx); err != nil { respondUnavailable(err) }.
func (s *SDK) Ready(ctx context.Context) error {
	if err := s.checkOpen(ctx); err != nil {
		return err
	}
	if err := s.store.Check(ctx); err != nil {
		return err
	}
	reader, err := s.artifacts.Open(ctx, s.bootstrap.Metadata().SHA256)
	if err != nil {
		return err
	}
	if err = reader.Close(); err != nil {
		return err
	}
	return s.checkOpen(ctx)
}

// GetDevice reads one identity in an authorized scope; absent and foreign identities both return ErrNotFound.
// Usage: device, err := sdk.GetDevice(ctx, scope, deviceID).
func (s *SDK) GetDevice(ctx context.Context, scope, id string) (Device, error) {
	if err := s.checkOpen(ctx); err != nil {
		return Device{}, err
	}
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(id) {
		return Device{}, ErrInvalidArgument
	}
	value, err := s.store.GetDevice(ctx, scope, id)
	return publicDevice(value), err
}

// GetOperation reads scoped durable progress and expires overdue work; it never exposes a download lease.
// Absent, foreign-scope and foreign-device operations all return ErrNotFound.
// Usage: operation, err := sdk.GetOperation(ctx, scope, deviceID, operationID).
func (s *SDK) GetOperation(ctx context.Context, scope, device, id string) (Operation, error) {
	if err := s.checkOpen(ctx); err != nil {
		return Operation{}, err
	}
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(device) || !devicePattern.MatchString(id) {
		return Operation{}, ErrInvalidArgument
	}
	value, err := s.store.GetOperation(ctx, scope, device, id, s.clock().UTC())
	return publicOperation(value), err
}

// Bind a keyset cursor to every authorization/filter dimension; cursors are not authorization capabilities.
func cursorBinding(kind, scope, device, state, correlation string) string {
	data, _ := json.Marshal([]string{kind, scope, device, state, correlation})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// Decode only the supported bounded cursor shape and reject reuse across scopes/devices/filters.
func readPage(page PageRequest, binding string) (string, int, error) {
	return readKeysetPage(page, binding, devicePattern)
}

// Validate the key shape appropriate to the resource while preserving query-bound opaque cursors.
func readKeysetPage(page PageRequest, binding string, pattern *regexp.Regexp) (string, int, error) {
	if page.Limit == 0 {
		page.Limit = defaultPageSize
	}
	if page.Limit < 1 || page.Limit > maximumPageSize || len(page.Cursor) > maximumCursorBytes {
		return "", 0, ErrInvalidArgument
	}
	if page.Cursor == "" {
		return "", page.Limit, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(page.Cursor)
	if err != nil {
		return "", 0, fmt.Errorf("%w: malformed cursor", ErrInvalidArgument)
	}
	var cursor pageCursor
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cursor); err != nil {
		return "", 0, fmt.Errorf("%w: malformed cursor", ErrInvalidArgument)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || cursor.Version != 1 || cursor.Binding != binding || !pattern.MatchString(cursor.After) {
		return "", 0, fmt.Errorf("%w: cursor does not match query", ErrInvalidArgument)
	}
	return cursor.After, page.Limit, nil
}

// Encode a versioned continuation token without any bearer credentials or storage internals.
func nextPage(binding, after string) string {
	data, _ := json.Marshal(pageCursor{Version: 1, Binding: binding, After: after})
	return base64.RawURLEncoding.EncodeToString(data)
}

// QueryDevices reads a live keyset page with exact optional filters. Concurrent inserts behind
// the cursor or mutable filter changes require a new scan; this is not a frozen snapshot.
// Usage: page, err := sdk.QueryDevices(ctx, scope, DeviceFilter{CorrelationID: requestID}, PageRequest{Limit: 20}).
func (s *SDK) QueryDevices(ctx context.Context, scope string, filter DeviceFilter, page PageRequest) (DevicePage, error) {
	if err := s.checkOpen(ctx); err != nil {
		return DevicePage{}, err
	}
	if !idPattern.MatchString(scope) || !slices.Contains([]string{"", "ENROLLED", "MANAGED", "REVOKED"}, filter.State) || (filter.CorrelationID != "" && !idPattern.MatchString(filter.CorrelationID)) {
		return DevicePage{}, ErrInvalidArgument
	}
	binding := cursorBinding("devices", scope, "", filter.State, filter.CorrelationID)
	after, limit, err := readPage(page, binding)
	if err != nil {
		return DevicePage{}, err
	}
	values, err := s.store.QueryDevices(ctx, ports.DeviceQuery{ScopeID: scope, After: after, State: filter.State, CorrelationID: filter.CorrelationID, Limit: limit + 1})
	if err != nil {
		return DevicePage{}, err
	}
	result := DevicePage{}
	if len(values) > limit {
		result.NextCursor = nextPage(binding, values[limit-1].ID)
		values = values[:limit]
	}
	result.Items = publicDevices(values)
	return result, nil
}

// QueryOperations reads a live operation page for one authorized device; expiry is evaluated on each read.
// Usage: page, err := sdk.QueryOperations(ctx, scope, deviceID, OperationFilter{State: "SUCCEEDED"}, PageRequest{Limit: 20}).
func (s *SDK) QueryOperations(ctx context.Context, scope, device string, filter OperationFilter, page PageRequest) (OperationPage, error) {
	if err := s.checkOpen(ctx); err != nil {
		return OperationPage{}, err
	}
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(device) || !slices.Contains([]string{"", "QUEUED", "RUNNING", "WAITING_USER", "SUCCEEDED", "FAILED", "SUPERSEDED", "CANCELLED"}, filter.State) {
		return OperationPage{}, ErrInvalidArgument
	}
	binding := cursorBinding("operations", scope, device, filter.State, "")
	after, limit, err := readPage(page, binding)
	if err != nil {
		return OperationPage{}, err
	}
	values, err := s.store.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: device, After: after, State: filter.State, Limit: limit + 1, Now: s.clock().UTC()})
	if err != nil {
		return OperationPage{}, err
	}
	result := OperationPage{}
	if len(values) > limit {
		result.NextCursor = nextPage(binding, values[limit-1].ID)
		values = values[:limit]
	}
	result.Items = publicOperations(values)
	return result, nil
}

// Detach nested arrays so a consumer cannot mutate provider-owned memory through public values.
func publicInstalled(value ports.InstalledApplication) InstalledApplication {
	return InstalledApplication{PackageName: value.PackageName, VersionCode: value.VersionCode, SigningCertificates: slices.Clone(value.SigningCertificates)}
}

// Project observation fields into host domain types without aliasing provider slices.
func publicDevice(value ports.Device) Device {
	inventory := Inventory{APILevel: value.Inventory.APILevel, Model: value.Inventory.Model, BuildFingerprint: value.Inventory.BuildFingerprint, AgentVersion: value.Inventory.AgentVersion, DeviceOwner: value.Inventory.DeviceOwner, Capabilities: slices.Clone(value.Inventory.Capabilities), SupportedABIs: slices.Clone(value.Inventory.SupportedABIs)}
	for _, app := range value.Inventory.Applications {
		inventory.Applications = append(inventory.Applications, publicInstalled(app))
	}
	return Device{ID: value.ID, ScopeID: value.ScopeID, CorrelationID: value.CorrelationID, State: value.State, Inventory: inventory, CreatedAt: value.CreatedAt, LastSeenAt: value.LastSeenAt, DesiredRevision: value.DesiredRevision, ReportedRevision: value.ReportedRevision}
}

// Keep catalog metadata detached from provider-owned backing storage.
func publicArtifact(value ports.Artifact) Artifact {
	return Artifact{ScopeID: value.ScopeID, SHA256: value.SHA256, PackageName: value.PackageName, VersionCode: value.VersionCode, MinSDK: value.MinSDK, MaxSDK: value.MaxSDK, SigningCertificates: slices.Clone(value.SigningCertificates), SupportedABIs: slices.Clone(value.SupportedABIs), SizeBytes: value.SizeBytes}
}

// Strip lease/checkpoint fields at the host boundary while retaining truthful observed results.
func publicOperation(value ports.Operation) Operation {
	result := Operation{ID: value.ID, DeviceID: value.DeviceID, Artifact: publicArtifact(value.Artifact), Revision: value.Revision, State: value.State, Deadline: value.Deadline, NotBefore: value.NotBefore, DownloadedBytes: value.DownloadedBytes, ErrorCode: value.ErrorCode, ErrorMessage: value.ErrorMessage}
	if value.Observed != nil {
		observed := publicInstalled(*value.Observed)
		result.Observed = &observed
	}
	return result
}

// Project a page without leaking persistence types to host application services.
func publicDevices(values []ports.Device) []Device {
	result := make([]Device, 0, len(values))
	for _, value := range values {
		result = append(result, publicDevice(value))
	}
	return result
}

// Project a page without exposing operation authorization leases.
func publicOperations(values []ports.Operation) []Operation {
	result := make([]Operation, 0, len(values))
	for _, value := range values {
		result = append(result, publicOperation(value))
	}
	return result
}

// Classify caller input failures independently of transport-specific error codes.
func invalidArgument(message string) error { return fmt.Errorf("%w: %s", ErrInvalidArgument, message) }
