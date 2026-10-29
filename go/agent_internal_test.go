// SPDX-License-Identifier: LGPL-3.0-only
package kylok

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/thienvu18/kylok/go/adapters/filesystem"
	"github.com/thienvu18/kylok/go/adapters/sqlite"
	kylokv1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/internal/testapk"
	"github.com/thienvu18/kylok/go/ports"
	"google.golang.org/protobuf/proto"
)

// TestInternalStoreErrorMapping verifies context and generic error mapping to Connect RPC errors.
func TestInternalStoreErrorMapping(t *testing.T) {
	cErr := storeError(context.Canceled)
	if connect.CodeOf(cErr) != connect.CodeCanceled {
		t.Fatalf("expected CodeCanceled, got %v", connect.CodeOf(cErr))
	}

	dErr := storeError(context.DeadlineExceeded)
	if connect.CodeOf(dErr) != connect.CodeDeadlineExceeded {
		t.Fatalf("expected CodeDeadlineExceeded, got %v", connect.CodeOf(dErr))
	}

	uErr := storeError(errors.New("generic backend storage failure"))
	if connect.CodeOf(uErr) != connect.CodeUnavailable {
		t.Fatalf("expected CodeUnavailable, got %v", connect.CodeOf(uErr))
	}

	mappings := []struct {
		err  error
		code connect.Code
	}{
		{ports.ErrInvalidArgument, connect.CodeInvalidArgument},
		{ports.ErrNotFound, connect.CodeUnauthenticated},
		{ports.ErrExpired, connect.CodeFailedPrecondition},
		{ports.ErrConsumed, connect.CodeAlreadyExists},
		{ports.ErrRecoveryExpired, connect.CodeFailedPrecondition},
		{ports.ErrRevoked, connect.CodePermissionDenied},
		{ports.ErrUnauthorized, connect.CodeUnauthenticated},
		{ports.ErrConflict, connect.CodeAlreadyExists},
		{ports.ErrStale, connect.CodeFailedPrecondition},
	}
	for _, m := range mappings {
		res := storeError(m.err)
		if connect.CodeOf(res) != m.code {
			t.Fatalf("for error %v, expected code %v, got %v", m.err, m.code, connect.CodeOf(res))
		}
	}
}

// TestInternalCredentialSealing exercises authenticated ECDH credential encryption.
func TestInternalCredentialSealing(t *testing.T) {
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	envelope, err := sealCredential(priv.PublicKey(), "secret-credential-payload", "dev-1234", "site-a", "attempt-1")
	if err != nil {
		t.Fatalf("failed to seal credential: %v", err)
	}
	if len(envelope.EphemeralPublicKey) == 0 || len(envelope.Nonce) == 0 || len(envelope.Ciphertext) == 0 {
		t.Fatal("empty fields in credential envelope")
	}
}

// TestInternalHelperRoutines exercises inventory, artifact and window helper mappings.
func TestInternalHelperRoutines(t *testing.T) {
	now := time.Now().UTC()
	if windowStartMillis(time.Time{}) != 0 {
		t.Fatal("expected 0 for zero time")
	}
	if windowStartMillis(now) != now.UnixMilli() {
		t.Fatal("expected millis for non-zero time")
	}

	art := ports.Artifact{
		SHA256:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PackageName: "io.kylok.pkg",
		VersionCode: 10,
		MinSDK:      33,
		MaxSDK:      37,
	}
	msg := artifactMessage(art)
	if msg.PackageName != "io.kylok.pkg" || msg.VersionCode != 10 {
		t.Fatalf("unexpected artifact message: %+v", msg)
	}

	dev := ports.Device{
		ID:        "dev-rev",
		ScopeID:   "site",
		State:     "REVOKED",
		CreatedAt: now,
	}
	dMsg := deviceMessage(dev)
	if dMsg.Id != "dev-rev" {
		t.Fatalf("unexpected device message: %+v", dMsg)
	}
}

// TestInternalBootstrapHandlerEdgeCases exercises internal bootstrapHandler branches.
func TestInternalBootstrapHandlerEdgeCases(t *testing.T) {
	ctx := context.Background()
	sdk, err := New(ctx, Options{
		DataDir:   t.TempDir(),
		Bootstrap: testapk.Make(t, testapk.Manifest),
		PublicURL: "http://127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Test s.bootstrapHandler() when s.closed is true -> line 352
	sdk.closed.Store(true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, sdk.bootstrap.Path(), nil)
	sdk.bootstrapHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on closed bootstrapHandler, got %d", rec.Code)
	}

	// 2. Re-open and test line 379: mock artifacts returning reader with mismatched bytes
	sdk.closed.Store(false)
	badStore := &mockBadArtifactStore{ArtifactStore: sdk.artifacts}
	sdk.artifacts = badStore
	rec = httptest.NewRecorder()
	sdk.bootstrapHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on corrupted artifact read in bootstrapHandler, got %d", rec.Code)
	}
}

type mockBadArtifactStore struct {
	ports.ArtifactStore
}

func (m *mockBadArtifactStore) Open(ctx context.Context, digest string) (ports.ReadSeekCloser, error) {
	return &nopCloserReader{Reader: bytes.NewReader([]byte("corrupt-bytes-mismatched-digest"))}, nil
}

type nopCloserReader struct {
	*bytes.Reader
}

func (n *nopCloserReader) Close() error { return nil }

// TestInternalProbeHandlerClosed exercises the 503 Service Unavailable path when SDK is closed during probe.
func TestInternalProbeHandlerClosed(t *testing.T) {
	ctx := context.Background()
	sdk, err := New(ctx, Options{
		DataDir:   t.TempDir(),
		Bootstrap: testapk.Make(t, testapk.Manifest),
		PublicURL: "http://127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()
	sdk.closed.Store(true)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/_kylok/probe", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("b", 43))
	req.Header.Set("X-Kylok-Device-ID", strings.Repeat("d", 43))
	req.Header.Set("X-Kylok-Probe", strings.Repeat("a", 43))
	sdk.networkProbe(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on closed probeHandler, got %d", rec.Code)
	}
}

// TestInternalSDKReadyStoreError exercises Ready when store.Check returns an error.
func TestInternalSDKReadyStoreError(t *testing.T) {
	ctx := context.Background()
	sdk, err := New(ctx, Options{
		DataDir:   t.TempDir(),
		Bootstrap: testapk.Make(t, testapk.Manifest),
		PublicURL: "http://127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()
	sdk.store = &mockStoreCheckError{StateStore: sdk.store}
	if err := sdk.Ready(ctx); err == nil {
		t.Fatal("expected error on failing store Check in Ready")
	}
}

type mockStoreCheckError struct {
	ports.StateStore
}

func (m *mockStoreCheckError) Check(ctx context.Context) error {
	return errors.New("injected store check error")
}

// TestInternalEventsBufferOverflowAndTimeout exercises event queue saturation and timeout handling.
func TestInternalEventsBufferOverflowAndTimeout(t *testing.T) {
	ctx := context.Background()
	blockedSink := func(ctx context.Context, e Event) error {
		<-ctx.Done()
		return ctx.Err()
	}
	sdk, err := New(ctx, Options{
		DataDir:      t.TempDir(),
		Bootstrap:    testapk.Make(t, testapk.Manifest),
		PublicURL:    "http://127.0.0.1:8080",
		Events:       blockedSink,
		EventTimeout: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Queue overflow (exercises line 92 default:)
	for i := 0; i < 1100; i++ {
		sdk.emit("test_overflow", "site", strings.Repeat("d", 43), 1, "test")
	}

	// 2. Close with timing out sink (exercises line 79 case <-timer.C:)
	if err := sdk.Close(); err != nil {
		t.Fatal(err)
	}

	// 3. Emit after context done (exercises line 90 case <-s.events.ctx.Done():)
	sdk.emit("test_after_done", "site", strings.Repeat("d", 43), 1, "test")
}

// TestInternalNetworkSecretContextCancelled exercises networkSecret when context is cancelled.
func TestInternalNetworkSecretContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sdk, err := New(context.Background(), Options{
		DataDir:        t.TempDir(),
		Bootstrap:      testapk.Make(t, testapk.Manifest),
		PublicURL:      "http://127.0.0.1:8080",
		NetworkSecrets: &mockFailingSecretProvider{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()
	_, err = sdk.networkSecret(ctx, "site", WifiProfile{Security: "WPA2_PSK", SecretRef: "ref1"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

type mockFailingSecretProvider struct{}

func (m *mockFailingSecretProvider) ReadNetworkSecret(ctx context.Context, scope, ref string) ([]byte, error) {
	return nil, errors.New("injected secret provider error")
}

// TestInternalRegisterApplicationDebuggableRejection exercises debuggable APK validation.
func TestInternalRegisterApplicationDebuggableRejection(t *testing.T) {
	ctx := context.Background()
	sdk, err := New(ctx, Options{
		DataDir:   t.TempDir(),
		Bootstrap: testapk.Make(t, testapk.Manifest),
		PublicURL: "http://127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()

	debugManifest := strings.Replace(testapk.Manifest, `package="io.kylok.unit"`, `package="io.kylok.app"`, 1)
	debugManifest = strings.Replace(debugManifest, `<application `, `<application android:debuggable="true" `, 1)
	debugAPK, err := testapk.InspectApplication(t, debugManifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sdk.RegisterApplication(ctx, "site", debugAPK); err == nil {
		t.Fatal("expected error registering debuggable APK without AllowDebugAPK")
	}
}

// TestInternalSDKClosedOperations exercises CreateEnrollment and QRWithOrigins when closed.
func TestInternalSDKClosedOperations(t *testing.T) {
	ctx := context.Background()
	sdk, err := New(ctx, Options{
		DataDir:   t.TempDir(),
		Bootstrap: testapk.Make(t, testapk.Manifest),
		PublicURL: "http://127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sdk.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := sdk.CreateEnrollment(ctx, "site", 15*time.Minute); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed on CreateEnrollment, got %v", err)
	}
	if _, err := sdk.QRWithOrigins(ctx, "token", "http://127.0.0.1:8080", "http://127.0.0.1:8080"); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed on QRWithOrigins, got %v", err)
	}
}

// TestInternalInventoryValidation exercises boundary validation of agent self-reported inventory.
func TestInternalInventoryValidation(t *testing.T) {
	validCert := strings.Repeat("a", 64)
	base := func() *kylokv1.Inventory {
		return &kylokv1.Inventory{
			ApiLevel:         34,
			DeviceOwner:      true,
			Model:            "Pixel 8",
			BuildFingerprint: "google/shiba/shiba:14/...",
			AgentVersion:     "1.0.0",
			Capabilities:     []string{"REBOOT", "LOCK"},
			SupportedAbis:    []string{"arm64-v8a"},
			Applications: []*kylokv1.InstalledApplication{
				{
					PackageName:         "com.example.app",
					VersionCode:         1,
					SigningCertificates: []string{validCert},
				},
			},
		}
	}

	if _, err := validateInventory(nil); err == nil {
		t.Fatal("expected error for nil inventory")
	}

	// API Level boundaries
	inv := base()
	inv.ApiLevel = 32
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for api level 32")
	}
	inv = base()
	inv.ApiLevel = 38
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for api level 38")
	}

	// Device owner requirement
	inv = base()
	inv.DeviceOwner = false
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for non-device owner")
	}

	// Model length
	inv = base()
	inv.Model = ""
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for empty model")
	}
	inv = base()
	inv.Model = strings.Repeat("x", 257)
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for model exceeding 256 bytes")
	}

	// Build fingerprint length
	inv = base()
	inv.BuildFingerprint = strings.Repeat("x", 1025)
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for fingerprint exceeding 1024 bytes")
	}

	// Agent version length
	inv = base()
	inv.AgentVersion = ""
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for empty agent version")
	}
	inv = base()
	inv.AgentVersion = strings.Repeat("v", 129)
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for agent version exceeding 128 bytes")
	}

	// Slice size boundaries
	inv = base()
	inv.Capabilities = make([]string, 33)
	for i := range inv.Capabilities {
		inv.Capabilities[i] = "CAP"
	}
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for capabilities count > 32")
	}

	inv = base()
	inv.SupportedAbis = make([]string, 17)
	for i := range inv.SupportedAbis {
		inv.SupportedAbis[i] = "abi"
	}
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for supported abis count > 16")
	}

	// Capability item length
	inv = base()
	inv.Capabilities = []string{""}
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for empty capability item")
	}
	inv = base()
	inv.Capabilities = []string{strings.Repeat("c", 65)}
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for capability item > 64 chars")
	}

	// Application count
	inv = base()
	inv.Applications = make([]*kylokv1.InstalledApplication, 129)
	for i := range inv.Applications {
		inv.Applications[i] = &kylokv1.InstalledApplication{
			PackageName:         "com.app." + string(rune('a'+(i%26))) + string(rune('0'+(i/26))),
			VersionCode:         1,
			SigningCertificates: []string{validCert},
		}
	}
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for applications count > 128")
	}

	// Application package name
	inv = base()
	inv.Applications = []*kylokv1.InstalledApplication{{PackageName: "", VersionCode: 1, SigningCertificates: []string{validCert}}}
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for empty package name")
	}
	inv = base()
	inv.Applications = []*kylokv1.InstalledApplication{{PackageName: strings.Repeat("p", 257), VersionCode: 1, SigningCertificates: []string{validCert}}}
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for package name > 256 chars")
	}

	// Version code < 1
	inv = base()
	inv.Applications = []*kylokv1.InstalledApplication{{PackageName: "com.pkg", VersionCode: 0, SigningCertificates: []string{validCert}}}
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for version code 0")
	}

	// Duplicate package name
	inv = base()
	inv.Applications = []*kylokv1.InstalledApplication{
		{PackageName: "com.pkg", VersionCode: 1, SigningCertificates: []string{validCert}},
		{PackageName: "com.pkg", VersionCode: 2, SigningCertificates: []string{validCert}},
	}
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for duplicate package name")
	}

	// Signing certificates count
	inv = base()
	inv.Applications = []*kylokv1.InstalledApplication{{PackageName: "com.pkg", VersionCode: 1, SigningCertificates: nil}}
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for empty signing certificates")
	}
	inv = base()
	inv.Applications = []*kylokv1.InstalledApplication{{
		PackageName:         "com.pkg",
		VersionCode:         1,
		SigningCertificates: make([]string, 9),
	}}
	for i := range inv.Applications[0].SigningCertificates {
		inv.Applications[0].SigningCertificates[i] = validCert
	}
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for > 8 signing certificates")
	}

	// Invalid certificate digest format
	inv = base()
	inv.Applications = []*kylokv1.InstalledApplication{{
		PackageName:         "com.pkg",
		VersionCode:         1,
		SigningCertificates: []string{"not-a-valid-sha256-hex"},
	}}
	if _, err := validateInventory(inv); err == nil {
		t.Fatal("expected error for invalid certificate digest")
	}
}

// TestInternalProtocolVersionCheck exercises wire protocol version enforcement.
func TestInternalProtocolVersionCheck(t *testing.T) {
	if err := protocol(ProtocolVersion); err != nil {
		t.Fatalf("expected valid protocol, got %v", err)
	}
	if err := protocol(0); err == nil {
		t.Fatal("expected error for protocol version 0")
	}
}

// TestInternalFeedCursorDecoding exercises feed cursor validation rules.
func TestInternalFeedCursorDecoding(t *testing.T) {
	// Empty cursor is valid initial cursor
	c, err := decodeFeedCursor("", "scope1", "changes")
	if err != nil || c.Scope != "scope1" || c.Kind != "changes" {
		t.Fatalf("unexpected result on empty cursor: %+v, err: %v", c, err)
	}

	// Too large
	if _, err := decodeFeedCursor(strings.Repeat("a", maximumCursorBytes+1), "scope1", "changes"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for oversized cursor")
	}

	// Invalid base64
	if _, err := decodeFeedCursor("!!!not-base64!!!", "scope1", "changes"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid base64")
	}

	// Invalid JSON
	badJSON := encodeFeedCursor(feedCursor{})
	// Manually corrupt raw bytes
	if _, err := decodeFeedCursor("bm90LWpzb24", "scope1", "changes"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid json cursor")
	}
	_ = badJSON

	// Scope mismatch
	cur := encodeFeedCursor(feedCursor{Version: 1, Scope: "scope1", Kind: "changes", Sequence: 0})
	if _, err := decodeFeedCursor(cur, "different_scope", "changes"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for scope mismatch")
	}

	// Kind mismatch
	if _, err := decodeFeedCursor(cur, "scope1", "projection"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for kind mismatch")
	}

	// Changes kind with snapshot or after
	badChanges := encodeFeedCursor(feedCursor{Version: 1, Scope: "scope1", Kind: "changes", Snapshot: "snap1"})
	if _, err := decodeFeedCursor(badChanges, "scope1", "changes"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for changes cursor with snapshot")
	}

	// Projection kind with sequence != 0 or bad snapshot
	badProj := encodeFeedCursor(feedCursor{Version: 1, Scope: "scope1", Kind: "projection", Snapshot: "snap1", Sequence: 5})
	if _, err := decodeFeedCursor(badProj, "scope1", "projection"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for projection cursor with sequence")
	}
}

// TestInternalSDKClosedAndValidationCoverage exercises SDK public methods when closed and on parameter boundaries.
func TestInternalSDKClosedAndValidationCoverage(t *testing.T) {
	ctx := context.Background()
	sdk, err := New(ctx, Options{
		DataDir:   t.TempDir(),
		Bootstrap: testapk.Make(t, testapk.Manifest),
		PublicURL: "http://127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Test parameter validation on open SDK
	if _, err := sdk.SubmitCommand(ctx, "bad!scope", "0000000000000000000000000000000000000000000", CommandReboot, "key1", time.Hour); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on invalid scope")
	}
	if _, err := sdk.SubmitCommand(ctx, "site", "bad_device", CommandReboot, "key1", time.Hour); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on invalid device")
	}
	if _, err := sdk.SubmitCommand(ctx, "site", "0000000000000000000000000000000000000000000", CommandReboot, "bad!key", time.Hour); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on invalid key")
	}
	if _, err := sdk.SubmitCommand(ctx, "site", "0000000000000000000000000000000000000000000", CommandKind("UNKNOWN"), "key1", time.Hour); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on invalid command kind")
	}
	if _, err := sdk.SubmitCommand(ctx, "site", "0000000000000000000000000000000000000000000", CommandReboot, "key1", 30*time.Second); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on timeout < 1m")
	}
	if _, err := sdk.SubmitCommand(ctx, "site", "0000000000000000000000000000000000000000000", CommandReboot, "key1", 8*24*time.Hour); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on timeout > 7d")
	}

	if _, err := sdk.GetCommand(ctx, "bad!scope", "0000000000000000000000000000000000000000000", "0000000000000000000000000000000000000000000"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on GetCommand bad scope")
	}
	if _, err := sdk.GetCommand(ctx, "site", "bad_dev", "0000000000000000000000000000000000000000000"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on GetCommand bad device")
	}
	if _, err := sdk.GetCommand(ctx, "site", "0000000000000000000000000000000000000000000", "bad_cmd"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on GetCommand bad command id")
	}

	if _, err := sdk.QueryCommands(ctx, "bad!scope", "0000000000000000000000000000000000000000000", PageRequest{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on QueryCommands bad scope")
	}
	if _, err := sdk.QueryCommands(ctx, "site", "bad_dev", PageRequest{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on QueryCommands bad device")
	}

	if _, err := sdk.CancelCommand(ctx, "bad!scope", "0000000000000000000000000000000000000000000", "0000000000000000000000000000000000000000000"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on CancelCommand bad scope")
	}
	if _, err := sdk.CancelCommand(ctx, "site", "bad_dev", "0000000000000000000000000000000000000000000"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on CancelCommand bad device")
	}
	if _, err := sdk.CancelCommand(ctx, "site", "0000000000000000000000000000000000000000000", "bad_cmd"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on CancelCommand bad command id")
	}

	if _, err := sdk.ListChanges(ctx, "bad!scope", "", 50); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on ListChanges bad scope")
	}
	if _, err := sdk.ListChanges(ctx, "site", "", 0); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on ListChanges limit < 1")
	}
	if _, err := sdk.ListChanges(ctx, "site", "", 501); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on ListChanges limit > 500")
	}

	if _, err := sdk.ReadProjection(ctx, "bad!scope", "", 50); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on ReadProjection bad scope")
	}
	if _, err := sdk.ReadProjection(ctx, "site", "", 0); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on ReadProjection limit < 1")
	}
	if _, err := sdk.ReadProjection(ctx, "site", "", 501); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on ReadProjection limit > 500")
	}

	// Parameter validation on open SDK for fleet, policy, network and management
	if _, err := sdk.QueryDevices(ctx, "bad!scope", DeviceFilter{}, PageRequest{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on QueryDevices bad scope")
	}
	if _, err := sdk.QueryOperations(ctx, "bad!scope", "0000000000000000000000000000000000000000000", OperationFilter{}, PageRequest{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on QueryOperations bad scope")
	}
	if _, err := sdk.QueryOperations(ctx, "site", "bad_dev", OperationFilter{}, PageRequest{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on QueryOperations bad device")
	}
	if _, err := sdk.QueryFleetAssignments(ctx, "bad!scope", PageRequest{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on QueryFleetAssignments bad scope")
	}
	if _, err := sdk.GetFleetAssignment(ctx, "bad!scope", "fa1"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on GetFleetAssignment bad scope")
	}
	if _, err := sdk.GetFleetAssignment(ctx, "site", "bad!id"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on GetFleetAssignment bad id")
	}
	if _, err := sdk.CreateFleetAssignment(ctx, FleetRequest{ScopeID: "bad!scope", ID: "fa1", ArtifactSHA256: strings.Repeat("0", 64)}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on CreateFleetAssignment bad scope")
	}
	if _, err := sdk.CreateFleetAssignment(ctx, FleetRequest{ScopeID: "site", ID: "bad!id", ArtifactSHA256: strings.Repeat("0", 64)}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on CreateFleetAssignment bad id")
	}
	if _, err := sdk.CreateFleetAssignment(ctx, FleetRequest{ScopeID: "site", ID: "fa1", ArtifactSHA256: "bad_sha"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on CreateFleetAssignment bad sha")
	}
	if _, err := sdk.CreateFleetAssignment(ctx, FleetRequest{ScopeID: "site", ID: "fa1", ArtifactSHA256: strings.Repeat("0", 64), GroupID: "bad!grp"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on CreateFleetAssignment bad group id")
	}
	if _, err := sdk.SetNetworkPolicy(ctx, "bad!scope", "0000000000000000000000000000000000000000000", "k1", NetworkPolicy{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on SetNetworkPolicy bad scope")
	}
	if _, err := sdk.SetNetworkPolicy(ctx, "site", "bad_dev", "k1", NetworkPolicy{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on SetNetworkPolicy bad device")
	}
	if _, err := sdk.SetNetworkPolicy(ctx, "site", "0000000000000000000000000000000000000000000", "bad!key", NetworkPolicy{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on SetNetworkPolicy bad key")
	}
	if _, err := sdk.GetNetworkPolicy(ctx, "bad!scope", "0000000000000000000000000000000000000000000"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on GetNetworkPolicy bad scope")
	}
	if _, err := sdk.GetNetworkPolicy(ctx, "site", "bad_dev"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on GetNetworkPolicy bad device")
	}
	if _, err := sdk.SetPolicy(ctx, "bad!scope", "0000000000000000000000000000000000000000000", "k1", Policy{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on SetPolicy bad scope")
	}
	if _, err := sdk.SetPolicy(ctx, "site", "bad_dev", "k1", Policy{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on SetPolicy bad device")
	}
	if _, err := sdk.SetPolicy(ctx, "site", "0000000000000000000000000000000000000000000", "bad!key", Policy{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on SetPolicy bad key")
	}
	if _, err := sdk.GetPolicy(ctx, "bad!scope", "0000000000000000000000000000000000000000000"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on GetPolicy bad scope")
	}
	if _, err := sdk.GetPolicy(ctx, "site", "bad_dev"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument on GetPolicy bad device")
	}

	// Close SDK and verify all methods return ErrClosed
	if err := sdk.Close(); err != nil {
		t.Fatal(err)
	}

	validDev := "0000000000000000000000000000000000000000000"
	validDigest := strings.Repeat("0", 64)
	if _, err := sdk.SubmitCommand(ctx, "site", validDev, CommandReboot, "key1", time.Hour); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on SubmitCommand")
	}
	if _, err := sdk.GetCommand(ctx, "site", validDev, validDev); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on GetCommand")
	}
	if _, err := sdk.QueryCommands(ctx, "site", validDev, PageRequest{}); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on QueryCommands")
	}
	if _, err := sdk.CancelCommand(ctx, "site", validDev, validDev); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on CancelCommand")
	}
	if _, err := sdk.ListChanges(ctx, "site", "", 50); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on ListChanges")
	}
	if _, err := sdk.GetDeviceState(ctx, "site", validDev); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on GetDeviceState")
	}
	if _, err := sdk.GetApplication(ctx, "site", validDigest); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on GetApplication")
	}
	if _, err := sdk.ReadProjection(ctx, "site", "", 50); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on ReadProjection")
	}

	// Additional closed checks for fleet, policy, network and management
	if _, err := sdk.QueryDevices(ctx, "site", DeviceFilter{}, PageRequest{}); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on QueryDevices")
	}
	if _, err := sdk.QueryOperations(ctx, "site", validDev, OperationFilter{}, PageRequest{}); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on QueryOperations")
	}
	if _, err := sdk.ScheduleApplication(ctx, "site", validDev, validDigest, "key1", time.Now().UTC(), time.Now().UTC().Add(time.Hour)); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on ScheduleApplication")
	}
	if _, err := sdk.CreateFleetAssignment(ctx, FleetRequest{ScopeID: "site", ID: "fa1", ArtifactSHA256: validDigest}); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on CreateFleetAssignment")
	}
	if _, err := sdk.GetFleetAssignment(ctx, "site", "fa1"); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on GetFleetAssignment")
	}
	if _, err := sdk.QueryFleetAssignments(ctx, "site", PageRequest{}); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on QueryFleetAssignments")
	}
	if _, err := sdk.AdvanceFleetAssignment(ctx, "site", "fa1", 10); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on AdvanceFleetAssignment")
	}
	if _, err := sdk.CancelFleetAssignment(ctx, "site", "fa1"); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on CancelFleetAssignment")
	}
	if _, err := sdk.SetNetworkPolicy(ctx, "site", validDev, "key1", NetworkPolicy{}); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on SetNetworkPolicy")
	}
	if _, err := sdk.GetNetworkPolicy(ctx, "site", validDev); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on GetNetworkPolicy")
	}
	if _, err := sdk.SetPolicy(ctx, "site", validDev, "key1", Policy{}); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on SetPolicy")
	}
	if _, err := sdk.GetPolicy(ctx, "site", validDev); !errors.Is(err, ErrClosed) {
		t.Fatal("expected ErrClosed on GetPolicy")
	}
}

type mockMismatchedArtifactStore struct {
	ports.ArtifactStore
}

func (m *mockMismatchedArtifactStore) Open(ctx context.Context, digest string) (ports.ReadSeekCloser, error) {
	return struct {
		*bytes.Reader
		io.Closer
	}{Reader: bytes.NewReader([]byte("completely different bootstrap content")), Closer: io.NopCloser(nil)}, nil
}

type mockFailingRegisterStore struct {
	ports.StateStore
}

func (m *mockFailingRegisterStore) RegisterArtifact(ctx context.Context, a ports.Artifact) error {
	return errors.New("injected register artifact failure")
}

// TestInternalSDKInitializationEdgeCases exercises initialization failure branches.
func TestInternalSDKInitializationEdgeCases(t *testing.T) {
	ctx := context.Background()
	bootstrap := testapk.Make(t, testapk.Manifest)

	// 1. kylok.sqlite is a directory -> sqlite.Open fails
	dir1 := t.TempDir()
	_ = os.Mkdir(filepath.Join(dir1, "kylok.sqlite"), 0700)
	if _, err := New(ctx, Options{DataDir: dir1, Bootstrap: bootstrap, PublicURL: "http://127.0.0.1:8080"}); err == nil {
		t.Fatal("expected error when kylok.sqlite is a directory")
	}

	// 2. artifacts is a regular file -> filesystem.Open fails
	dir2 := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir2, "artifacts"), []byte("not-a-dir"), 0600)
	if _, err := New(ctx, Options{DataDir: dir2, Bootstrap: bootstrap, PublicURL: "http://127.0.0.1:8080"}); err == nil {
		t.Fatal("expected error when artifacts path is a file")
	}

	// 3. Artifact provider returns mismatched bytes
	dir3 := t.TempDir()
	fsStore, _ := filesystem.Open(filepath.Join(dir3, "artifacts"))
	if _, err := New(ctx, Options{
		DataDir:   dir3,
		Bootstrap: bootstrap,
		PublicURL: "http://127.0.0.1:8080",
		Artifacts: &mockMismatchedArtifactStore{ArtifactStore: fsStore},
	}); err == nil || !strings.Contains(err.Error(), "artifact provider returned different bootstrap content") {
		t.Fatalf("expected different bootstrap content error, got %v", err)
	}

	// 4. Store fails on RegisterArtifact
	dir4 := t.TempDir()
	sqlStore, _ := sqlite.Open(ctx, filepath.Join(dir4, "kylok.sqlite"))
	if _, err := New(ctx, Options{
		DataDir:   dir4,
		Bootstrap: bootstrap,
		PublicURL: "http://127.0.0.1:8080",
		Store:     &mockFailingRegisterStore{StateStore: sqlStore},
	}); err == nil || !strings.Contains(err.Error(), "injected register artifact failure") {
		t.Fatalf("expected injected register artifact failure, got %v", err)
	}
}

// TestInternalSDKDeepCoverage exercises pagination cursor generation, command/application boundary errors, and network probe branches.
func TestInternalSDKDeepCoverage(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	apk := testapk.Make(t, testapk.Manifest)
	sdk, err := New(ctx, Options{
		DataDir:   dir,
		Bootstrap: apk,
		PublicURL: "http://127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()

	scope := "site"

	// 1. AssignApplication timeouts
	if _, err := sdk.AssignApplication(ctx, scope, strings.Repeat("a", 43), strings.Repeat("b", 64), "k1", 30*time.Second); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for timeout < 1m, got %v", err)
	}
	if _, err := sdk.AssignApplication(ctx, scope, strings.Repeat("a", 43), strings.Repeat("b", 64), "k1", 8*24*time.Hour); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for timeout > 7d, got %v", err)
	}

	// 2. SubmitCommand timeouts
	if _, err := sdk.SubmitCommand(ctx, scope, strings.Repeat("a", 43), CommandReboot, "k1", 30*time.Second); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for timeout < 1m, got %v", err)
	}
	if _, err := sdk.SubmitCommand(ctx, scope, strings.Repeat("a", 43), CommandReboot, "k1", 8*24*time.Hour); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for timeout > 7d, got %v", err)
	}

	// 3. ScheduleApplication argument validation
	now := time.Now().UTC()
	if _, err := sdk.ScheduleApplication(ctx, scope, strings.Repeat("a", 43), strings.Repeat("b", 64), "k1", time.Time{}, now.Add(time.Hour)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for notBefore <= 0, got %v", err)
	}
	if _, err := sdk.ScheduleApplication(ctx, scope, strings.Repeat("a", 43), strings.Repeat("b", 64), "k1", now.Add(2*time.Hour), now.Add(time.Hour)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for notBefore >= deadline, got %v", err)
	}
	if _, err := sdk.ScheduleApplication(ctx, scope, strings.Repeat("a", 43), strings.Repeat("b", 64), "k1", now, now.Add(8*24*time.Hour)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for deadline - notBefore > 7d, got %v", err)
	}

	// 4. Enroll 2 devices to test pagination with NextCursor
	tok1, _ := sdk.CreateEnrollment(ctx, scope, 15*time.Minute)
	tok2, _ := sdk.CreateEnrollment(ctx, scope, 15*time.Minute)
	agent := &agentService{sdk: sdk}
	key1, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub1, _ := x509.MarshalPKIXPublicKey(&key1.PublicKey)
	key2, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub2, _ := x509.MarshalPKIXPublicKey(&key2.PublicKey)
	inv := &kylokv1.Inventory{ApiLevel: 34, Model: "Pixel", DeviceOwner: true, AgentVersion: "1.0", Capabilities: []string{"command.reboot"}}

	enrResp1, err := agent.Enroll(ctx, connect.NewRequest(&kylokv1.EnrollRequest{
		Token: tok1.Token, AttemptId: "attempt_page_one_16", PublicKey: pub1, Inventory: inv,
		ProtocolVersion: ProtocolVersion,
	}))
	if err != nil {
		t.Fatal(err)
	}
	devID1 := enrResp1.Msg.Device.Id

	enrResp2, err := agent.Enroll(ctx, connect.NewRequest(&kylokv1.EnrollRequest{
		Token: tok2.Token, AttemptId: "attempt_page_two_16", PublicKey: pub2, Inventory: inv,
		ProtocolVersion: ProtocolVersion,
	}))
	if err != nil {
		t.Fatal(err)
	}
	_ = enrResp2

	// QueryDevices with Limit 1 -> generates NextCursor
	devPage, err := sdk.QueryDevices(ctx, scope, DeviceFilter{}, PageRequest{Limit: 1})
	if err != nil || len(devPage.Items) != 1 || devPage.NextCursor == "" {
		t.Fatalf("expected 1 item and non-empty NextCursor, got %+v, err: %v", devPage, err)
	}
	// Query page 2 with cursor
	devPage2, err := sdk.QueryDevices(ctx, scope, DeviceFilter{}, PageRequest{Cursor: devPage.NextCursor})
	if err != nil || len(devPage2.Items) != 1 {
		t.Fatalf("expected 1 item on page 2, got %+v, err: %v", devPage2, err)
	}

	// Submit 2 commands on devID1 -> QueryCommands pagination
	cmd1, _ := sdk.SubmitCommand(ctx, scope, devID1, CommandReboot, "k_cmd_1", time.Hour)
	cmd2, _ := sdk.SubmitCommand(ctx, scope, devID1, CommandReboot, "k_cmd_2", time.Hour)
	_ = cmd1
	_ = cmd2
	cmdPage, err := sdk.QueryCommands(ctx, scope, devID1, PageRequest{Limit: 1})
	if err != nil || len(cmdPage.Items) != 1 || cmdPage.NextCursor == "" {
		t.Fatalf("expected 1 item and NextCursor in QueryCommands, got %+v, err: %v", cmdPage, err)
	}

	// Replace 2 groups -> QueryGroups pagination
	_, _ = sdk.ReplaceGroup(ctx, scope, "grp_1", []string{devID1}, 0)
	_, _ = sdk.ReplaceGroup(ctx, scope, "grp_2", []string{devID1}, 0)
	grpPage, err := sdk.QueryGroups(ctx, scope, PageRequest{Limit: 1})
	if err != nil || len(grpPage.Items) != 1 || grpPage.NextCursor == "" {
		t.Fatalf("expected 1 item and NextCursor in QueryGroups, got %+v, err: %v", grpPage, err)
	}

	// Register application in scope for fleet & operations
	_, _ = sdk.RegisterApplication(ctx, scope, apk)

	// Create 2 fleet assignments -> QueryFleetAssignments pagination
	_, _ = sdk.CreateFleetAssignment(ctx, FleetRequest{ScopeID: scope, ID: "fl_p_1", ArtifactSHA256: apk.Metadata().SHA256, DeviceIDs: []string{devID1}, NotBefore: now.Add(time.Minute), Deadline: now.Add(time.Hour)})
	_, _ = sdk.CreateFleetAssignment(ctx, FleetRequest{ScopeID: scope, ID: "fl_p_2", ArtifactSHA256: apk.Metadata().SHA256, DeviceIDs: []string{devID1}, NotBefore: now.Add(time.Minute), Deadline: now.Add(time.Hour)})
	flPage, err := sdk.QueryFleetAssignments(ctx, scope, PageRequest{Limit: 1})
	if err != nil || len(flPage.Items) != 1 || flPage.NextCursor == "" {
		t.Fatalf("expected 1 item and NextCursor in QueryFleetAssignments, got %+v, err: %v", flPage, err)
	}

	// QueryOperations pagination
	// Assign 2 applications
	op1, _ := sdk.AssignApplication(ctx, scope, devID1, apk.Metadata().SHA256, "k_op_p1", time.Hour)
	op2, _ := sdk.AssignApplication(ctx, scope, devID1, apk.Metadata().SHA256, "k_op_p2", time.Hour)
	_ = op1
	_ = op2
	opPage, err := sdk.QueryOperations(ctx, scope, devID1, OperationFilter{}, PageRequest{Limit: 1})
	if err != nil || len(opPage.Items) != 1 || opPage.NextCursor == "" {
		t.Fatalf("expected 1 item and NextCursor in QueryOperations, got %+v, err: %v", opPage, err)
	}

	// 5. networkProbe HTTP branches
	// Method not GET -> 405
	rec405 := httptest.NewRecorder()
	req405 := httptest.NewRequest(http.MethodPost, "/kylok/v1/network/probe", nil)
	sdk.networkProbe(rec405, req405)
	if rec405.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got %d", rec405.Code)
	}

	// Invalid headers -> 401
	rec401 := httptest.NewRecorder()
	req401 := httptest.NewRequest(http.MethodGet, "/kylok/v1/network/probe", nil)
	sdk.networkProbe(rec401, req401)
	if rec401.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", rec401.Code)
	}

	// 6. networkMessage nil
	msgNil, err := sdk.networkMessage(ctx, scope, nil)
	if err != nil || msgNil != nil {
		t.Fatalf("expected nil for nil desired network policy, got %v, err: %v", msgNil, err)
	}

	// 7. networkMessage with CA Certificate
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Test CA"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		IsCA:         true,
	}
	caPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caDER, _ := x509.CreateCertificate(rand.Reader, template, template, &caPriv.PublicKey, caPriv)
	msgWithCert, err := sdk.networkMessage(ctx, scope, &ports.DesiredNetworkPolicy{
		Revision: 1,
		Policy: ports.NetworkPolicy{
			PrimaryID: "w1",
			Profiles:  []ports.WifiProfile{{ID: "w1", SSID: "WiFi-1", Security: "OPEN"}},
			Certificates: []ports.CACertificate{{ID: "ca1", DER: caDER}},
		},
	})
	if err != nil || len(msgWithCert.Policy.Certificates) != 1 {
		t.Fatalf("expected 1 CA certificate in networkMessage, got %+v, err: %v", msgWithCert, err)
	}

	// 8. SetNetworkPolicy with expired CA Certificate -> invalidArgument (network.go:81)
	expiredTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "Expired CA"},
		NotBefore:    now.Add(-2 * time.Hour),
		NotAfter:     now.Add(-time.Hour),
		IsCA:         true,
	}
	expiredDER, _ := x509.CreateCertificate(rand.Reader, expiredTemplate, expiredTemplate, &caPriv.PublicKey, caPriv)
	_, err = sdk.SetNetworkPolicy(ctx, scope, devID1, "k_exp_net", NetworkPolicy{
		PrimaryID: "w1",
		Profiles:  []WifiProfile{{ID: "w1", SSID: "WiFi-1", Security: "OPEN"}},
		Certificates: []CACertificate{{ID: "ca_exp", DER: expiredDER}},
	})
	if err == nil {
		t.Fatal("expected error on expired CA certificate in SetNetworkPolicy")
	}

	// 9. ListChanges with invalid cursor -> error
	if _, err := sdk.ListChanges(ctx, scope, "invalid-cursor-format", 10); err == nil {
		t.Fatal("expected error on invalid cursor in ListChanges")
	}

	// 10. Direct SDK input validation error branches
	if _, err := sdk.CreateEnrollmentWithOptions(ctx, EnrollmentOptions{ScopeID: scope, TTL: time.Second}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for short TTL, got %v", err)
	}
	if _, err := sdk.CreateEnrollmentWithOptions(ctx, EnrollmentOptions{ScopeID: scope, TTL: 25 * time.Hour}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for long TTL, got %v", err)
	}
	if _, err := sdk.CreateEnrollmentWithOptions(ctx, EnrollmentOptions{ScopeID: scope, TTL: 15 * time.Minute, CorrelationID: "bad correlation id spaces!"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for bad correlation ID, got %v", err)
	}
	if _, err := sdk.AssignApplication(ctx, scope, devID1, strings.Repeat("a", 64), "key", 30*time.Second); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for short assign timeout, got %v", err)
	}
	if _, err := sdk.AssignApplication(ctx, scope, devID1, strings.Repeat("a", 64), "key", 8*24*time.Hour); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for long assign timeout, got %v", err)
	}
	if _, err := sdk.ScheduleApplication(ctx, scope, devID1, strings.Repeat("a", 64), "key", now.Add(time.Hour), now); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for notBefore after deadline, got %v", err)
	}
	if _, err := sdk.ScheduleApplication(ctx, scope, devID1, strings.Repeat("a", 64), "key", time.Unix(0, 0), now.Add(time.Hour)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for notBefore <= 0, got %v", err)
	}
	if _, err := sdk.ScheduleApplication(ctx, scope, devID1, strings.Repeat("a", 64), "key", now, now.Add(8*24*time.Hour)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for schedule window > 7d, got %v", err)
	}
	if _, err := sdk.SubmitCommand(ctx, scope, devID1, CommandKind("SHUTDOWN"), "k", time.Hour); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid command kind, got %v", err)
	}
	if _, err := sdk.SubmitCommand(ctx, scope, devID1, CommandReboot, "k", 30*time.Second); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for short command timeout, got %v", err)
	}
	if _, err := sdk.SubmitCommand(ctx, scope, devID1, CommandReboot, "k", 8*24*time.Hour); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for long command timeout, got %v", err)
	}
	if _, err := sdk.GetCommand(ctx, scope, devID1, "invalid_not_43_chars"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid command ID format, got %v", err)
	}
	if _, err := sdk.QueryCommands(ctx, "invalid scope spaces!", devID1, PageRequest{Limit: 10}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid scope in QueryCommands, got %v", err)
	}
	if _, err := sdk.QueryCommands(ctx, scope, "invalid_dev_not_43", PageRequest{Limit: 10}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid dev in QueryCommands, got %v", err)
	}
	if _, err := sdk.QueryDevices(ctx, scope, DeviceFilter{State: "INVALID_STATE"}, PageRequest{Limit: 10}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid state in QueryDevices, got %v", err)
	}
	if _, err := sdk.QueryDevices(ctx, scope, DeviceFilter{CorrelationID: "bad spaces!"}, PageRequest{Limit: 10}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid correlation in QueryDevices, got %v", err)
	}
	if _, err := sdk.QueryOperations(ctx, scope, devID1, OperationFilter{State: "INVALID_STATE"}, PageRequest{Limit: 10}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid state in QueryOperations, got %v", err)
	}
	if _, err := sdk.QueryGroups(ctx, "invalid scope spaces!", PageRequest{Limit: 10}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid scope in QueryGroups, got %v", err)
	}
	if _, err := sdk.QueryFleetAssignments(ctx, "invalid scope spaces!", PageRequest{Limit: 10}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid scope in QueryFleetAssignments, got %v", err)
	}
}

type testFaultStore struct {
	ports.StateStore
	failListCommands bool
	failListGroups   bool
	failListFleet    bool
	failQueryDevs    bool
	failQueryOps     bool
	failRegArt       bool
	failListChanges  bool
	failReadProj     bool
}

func (s *testFaultStore) ListCommands(ctx context.Context, scope, device, after string, limit int, now time.Time) ([]ports.Command, error) {
	if s.failListCommands {
		return nil, errors.New("simulated ListCommands error")
	}
	return s.StateStore.ListCommands(ctx, scope, device, after, limit, now)
}

func (s *testFaultStore) ListGroups(ctx context.Context, scope, after string, limit int) ([]ports.Group, error) {
	if s.failListGroups {
		return nil, errors.New("simulated ListGroups error")
	}
	return s.StateStore.ListGroups(ctx, scope, after, limit)
}

func (s *testFaultStore) ListFleetAssignments(ctx context.Context, scope, after string, limit int) ([]ports.FleetAssignment, error) {
	if s.failListFleet {
		return nil, errors.New("simulated ListFleetAssignments error")
	}
	return s.StateStore.ListFleetAssignments(ctx, scope, after, limit)
}

func (s *testFaultStore) QueryDevices(ctx context.Context, input ports.DeviceQuery) ([]ports.Device, error) {
	if s.failQueryDevs {
		return nil, errors.New("simulated QueryDevices error")
	}
	return s.StateStore.QueryDevices(ctx, input)
}

func (s *testFaultStore) QueryOperations(ctx context.Context, input ports.OperationQuery) ([]ports.Operation, error) {
	if s.failQueryOps {
		return nil, errors.New("simulated QueryOperations error")
	}
	return s.StateStore.QueryOperations(ctx, input)
}

func (s *testFaultStore) RegisterArtifact(ctx context.Context, a ports.Artifact) error {
	if s.failRegArt {
		return errors.New("simulated RegisterArtifact error")
	}
	return s.StateStore.RegisterArtifact(ctx, a)
}

func (s *testFaultStore) ListChanges(ctx context.Context, scope string, sequence uint64, limit int) (ports.ChangesPage, error) {
	if s.failListChanges {
		return ports.ChangesPage{}, errors.New("simulated ListChanges error")
	}
	return s.StateStore.ListChanges(ctx, scope, sequence, limit)
}

func (s *testFaultStore) ReadProjection(ctx context.Context, scope, snapshot string, after, limit int, now time.Time) (ports.ProjectionPage, error) {
	if s.failReadProj {
		return ports.ProjectionPage{}, errors.New("simulated ReadProjection error")
	}
	return s.StateStore.ReadProjection(ctx, scope, snapshot, after, limit, now)
}

type testFaultArtifacts struct {
	ports.ArtifactStore
	failPut   bool
	failClose bool
}

func (a *testFaultArtifacts) Put(ctx context.Context, digest string, data []byte) error {
	if a.failPut {
		return errors.New("simulated Put error")
	}
	return a.ArtifactStore.Put(ctx, digest, data)
}

func (a *testFaultArtifacts) Open(ctx context.Context, digest string) (ports.ReadSeekCloser, error) {
	r, err := a.ArtifactStore.Open(ctx, digest)
	if err != nil {
		return nil, err
	}
	if a.failClose {
		return &testFaultReader{ReadSeekCloser: r, failClose: true}, nil
	}
	return r, nil
}

type testFaultReader struct {
	ports.ReadSeekCloser
	failClose bool
}

func (r *testFaultReader) Close() error {
	if r.failClose {
		_ = r.ReadSeekCloser.Close()
		return errors.New("simulated close failure")
	}
	return r.ReadSeekCloser.Close()
}

// TestSDKStoreFailureBranches covers store failure propagation in SDK public methods.
func TestSDKStoreFailureBranches(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	apk := testapk.Make(t, testapk.Manifest)
	sdk, err := New(ctx, Options{
		DataDir:   dir,
		Bootstrap: apk,
		PublicURL: "http://127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()

	scope := "site"
	devID := strings.Repeat("a", 43)

	faultStore := &testFaultStore{StateStore: sdk.store}
	sdk.store = faultStore
	faultArt := &testFaultArtifacts{ArtifactStore: sdk.artifacts}
	sdk.artifacts = faultArt

	// 1. commands.go:91 (QueryCommands store error)
	faultStore.failListCommands = true
	if _, err := sdk.QueryCommands(ctx, scope, devID, PageRequest{Limit: 10}); err == nil {
		t.Fatal("expected error from QueryCommands")
	}
	faultStore.failListCommands = false

	// 2. fleet.go:99 (QueryGroups store error)
	faultStore.failListGroups = true
	if _, err := sdk.QueryGroups(ctx, scope, PageRequest{Limit: 10}); err == nil {
		t.Fatal("expected error from QueryGroups")
	}
	faultStore.failListGroups = false

	// 3. fleet.go:178 (QueryFleetAssignments store error)
	faultStore.failListFleet = true
	if _, err := sdk.QueryFleetAssignments(ctx, scope, PageRequest{Limit: 10}); err == nil {
		t.Fatal("expected error from QueryFleetAssignments")
	}
	faultStore.failListFleet = false

	// 4. management.go:263 (QueryDevices store error)
	faultStore.failQueryDevs = true
	if _, err := sdk.QueryDevices(ctx, scope, DeviceFilter{}, PageRequest{Limit: 10}); err == nil {
		t.Fatal("expected error from QueryDevices")
	}
	faultStore.failQueryDevs = false

	// 5. management.go:290 (QueryOperations store error)
	faultStore.failQueryOps = true
	if _, err := sdk.QueryOperations(ctx, scope, devID, OperationFilter{}, PageRequest{Limit: 10}); err == nil {
		t.Fatal("expected error from QueryOperations")
	}
	faultStore.failQueryOps = false

	// 6. applications.go:53 (artifacts.Put error on RegisterApplication)
	faultArt.failPut = true
	if _, err := sdk.RegisterApplication(ctx, scope, apk); err == nil {
		t.Fatal("expected error on failing Put")
	}
	faultArt.failPut = false

	// 7. applications.go:56 (store.RegisterArtifact error on RegisterApplication)
	faultStore.failRegArt = true
	if _, err := sdk.RegisterApplication(ctx, scope, apk); err == nil {
		t.Fatal("expected error on failing RegisterArtifact")
	}
	faultStore.failRegArt = false

	// 8. network.go:75 (NormalizeNetworkPolicy error on SetNetworkPolicy)
	if _, err := sdk.SetNetworkPolicy(ctx, scope, devID, "net_k_inval", NetworkPolicy{}); err == nil {
		t.Fatal("expected error on invalid NetworkPolicy in SetNetworkPolicy")
	}

	// 9. changes.go:131 (store.ListChanges error in ListChanges)
	faultStore.failListChanges = true
	if _, err := sdk.ListChanges(ctx, scope, "", 10); err == nil {
		t.Fatal("expected error from ListChanges on failing ListChanges")
	}
	faultStore.failListChanges = false

	// 10. changes.go:190 (store.ReadProjection error in ReadProjection)
	faultStore.failReadProj = true
	if _, err := sdk.ReadProjection(ctx, scope, "", 10); err == nil {
		t.Fatal("expected error from ReadProjection on failing ReadProjection")
	}
	faultStore.failReadProj = false
}

// TestAgentEnrollKeyValidationAndBootstrapFailures covers RSA/invalid curve public keys in Enroll,
// artifact reader close failure in Ready, and mismatched bootstrap bytes in New.
func TestAgentEnrollKeyValidationAndBootstrapFailures(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	apk := testapk.Make(t, testapk.Manifest)
	sdk, err := New(ctx, Options{
		DataDir:   dir,
		Bootstrap: apk,
		PublicURL: "http://127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()

	enr, err := sdk.CreateEnrollment(ctx, "scope_val", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	agent := &agentService{sdk: sdk}

	// 1. Enroll with RSA key (agent.go:123-125)
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaDER, err := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	reqRSA := connect.NewRequest(&kylokv1.EnrollRequest{
		ProtocolVersion: ProtocolVersion,
		Token:           enr.Token,
		AttemptId:       "attempt_rsa_12345",
		PublicKey:       rsaDER,
	})
	if _, err := agent.Enroll(ctx, reqRSA); err == nil {
		t.Fatal("expected error on RSA public key")
	}

	// 2. Ready with failing reader Close (management.go:167-169)
	faultArt := &testFaultArtifacts{ArtifactStore: sdk.artifacts, failClose: true}
	sdk.artifacts = faultArt
	if err := sdk.Ready(ctx); err == nil {
		t.Fatal("expected error from Ready on failing reader close")
	}
	sdk.artifacts = faultArt.ArtifactStore

	// 3. New with mismatched bootstrap bytes returned by artifact store (kylok.go:170-172)
	mismatchStore := &testMismatchArtifactStore{ArtifactStore: sdk.artifacts}
	_, err = New(ctx, Options{
		DataDir:   t.TempDir(),
		Bootstrap: apk,
		PublicURL: "http://127.0.0.1:8080",
		Artifacts: mismatchStore,
	})
	if err == nil || !strings.Contains(err.Error(), "different bootstrap content") {
		t.Fatalf("expected different bootstrap content error, got %v", err)
	}

	// 4. Enroll with corrupted receipt returned by store (agent.go:163-165)
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rawKey, err := x509.MarshalPKIXPublicKey(&ecdsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	enrBadRec, err := sdk.CreateEnrollment(ctx, "scope_val", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	faultStore := &testReceiptFaultStore{StateStore: sdk.store, badReceipt: []byte("invalid proto receipt bytes")}
	sdk.store = faultStore
	reqBadReceipt := connect.NewRequest(&kylokv1.EnrollRequest{
		ProtocolVersion: ProtocolVersion,
		Token:           enrBadRec.Token,
		AttemptId:       "attempt_bad_rec_1",
		PublicKey:       rawKey,
	})
	if _, err := agent.Enroll(ctx, reqBadReceipt); err == nil {
		t.Fatal("expected error on corrupted receipt from Consume")
	}

	// 5. Enroll with mismatched scope in receipt (agent.go:167-169)
	enrBadScope, err := sdk.CreateEnrollment(ctx, "scope_val", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	badScopeResp := &kylokv1.EnrollResponse{
		Device: &kylokv1.Device{
			Id:      "dev_scope_mismatch",
			ScopeId: "other_mismatched_scope",
		},
		Credential:      &kylokv1.CredentialEnvelope{},
		ProtocolVersion: ProtocolVersion,
	}
	badScopeBytes, _ := proto.Marshal(badScopeResp)
	faultStore.badReceipt = badScopeBytes
	reqBadScope := connect.NewRequest(&kylokv1.EnrollRequest{
		ProtocolVersion: ProtocolVersion,
		Token:           enrBadScope.Token,
		AttemptId:       "attempt_bad_scope_2",
		PublicKey:       rawKey,
	})
	if _, err := agent.Enroll(ctx, reqBadScope); err == nil {
		t.Fatal("expected error on mismatched scope in receipt")
	}
	sdk.store = faultStore.StateStore
}

type testReceiptFaultStore struct {
	ports.StateStore
	badReceipt []byte
}

func (s *testReceiptFaultStore) Consume(ctx context.Context, att ports.Attempt) ([]byte, error) {
	if s.badReceipt != nil {
		return s.badReceipt, nil
	}
	return s.StateStore.Consume(ctx, att)
}

type testMismatchArtifactStore struct {
	ports.ArtifactStore
}

func (m *testMismatchArtifactStore) Open(ctx context.Context, digest string) (ports.ReadSeekCloser, error) {
	return nopCloser{bytes.NewReader([]byte("different content than bootstrap"))}, nil
}

type nopCloser struct {
	*bytes.Reader
}

func (nopCloser) Close() error { return nil }



