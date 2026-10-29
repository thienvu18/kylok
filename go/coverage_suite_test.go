// SPDX-License-Identifier: LGPL-3.0-only
package kylok_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thienvu18/kylok/go"
	"github.com/thienvu18/kylok/go/adapters/sqlite"
	kylokv1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/internal/testapk"
	"github.com/thienvu18/kylok/go/ports"
	"github.com/thienvu18/kylok/go/provisioning"

	"connectrpc.com/connect"
)

// TestClosedSDKRejection verifies that every SDK method fails cleanly when the SDK is closed.
func TestClosedSDKRejection(t *testing.T) {
	sdk, _ := setup(t, nil)
	apk := testapk.Make(t, testapk.Manifest)
	art, err := sdk.RegisterApplication(ctx, "site", apk)
	if err != nil {
		t.Fatal(err)
	}

	validDevID := strings.Repeat("d", 43)

	// Close SDK
	if err := sdk.Close(); err != nil {
		t.Fatal(err)
	}

	// Verify methods reject calls after Close()
	if _, err := sdk.RegisterApplication(ctx, "site", apk); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if err := sdk.RemoveApplication(ctx, "site", art.SHA256); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.ListApplications(ctx, "site", "", 10); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.AssignApplication(ctx, "site", validDevID, art.SHA256, "k", time.Hour); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.ListOperations(ctx, "site", validDevID, "", 10); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.GetApplication(ctx, "site", art.SHA256); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.GetDeviceState(ctx, "site", validDevID); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.ReadProjection(ctx, "site", "", 10); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.ListChanges(ctx, "site", "", 10); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.GetGroup(ctx, "site", "g1"); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.ReplaceGroup(ctx, "site", "g1", nil, 0); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.QueryGroups(ctx, "site", kylok.PageRequest{Limit: 10}); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.GetFleetAssignment(ctx, "site", "f1"); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.CreateFleetAssignment(ctx, kylok.FleetRequest{ScopeID: "site", ID: "f1", GroupID: "g1", ArtifactSHA256: art.SHA256}); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.QueryFleetAssignments(ctx, "site", kylok.PageRequest{Limit: 10}); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.AdvanceFleetAssignment(ctx, "site", "f1", 10); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.CancelFleetAssignment(ctx, "site", "f1"); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.CancelOperation(ctx, "site", validDevID, "op1"); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.ScheduleApplication(ctx, "site", validDevID, art.SHA256, "k", time.Now(), time.Now().Add(time.Hour)); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.GetCommand(ctx, "site", validDevID, "c1"); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.CancelCommand(ctx, "site", validDevID, "c1"); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.QueryCommands(ctx, "site", validDevID, kylok.PageRequest{Limit: 10}); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.SubmitCommand(ctx, "site", validDevID, kylok.CommandLock, "k", time.Hour); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.GetNetworkPolicy(ctx, "site", validDevID); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.SetNetworkPolicy(ctx, "site", validDevID, "k", kylok.NetworkPolicy{}); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.GetPolicy(ctx, "site", validDevID); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.SetPolicy(ctx, "site", validDevID, "k", kylok.Policy{}); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.ListDevices(ctx, "site", "", 10); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if err := sdk.RevokeDevice(ctx, "site", validDevID); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if err := sdk.Ready(ctx); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.CreateEnrollment(ctx, "site", time.Minute); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.GetDevice(ctx, "site", validDevID); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.GetOperation(ctx, "site", validDevID, "op1"); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.QueryDevices(ctx, "site", kylok.DeviceFilter{}, kylok.PageRequest{Limit: 10}); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if _, err := sdk.QueryOperations(ctx, "site", validDevID, kylok.OperationFilter{}, kylok.PageRequest{Limit: 10}); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

// TestApplicationManagementEdgeCases tests RemoveApplication, ListOperations pagination, and validation.
func TestApplicationManagementEdgeCases(t *testing.T) {
	sdk, _ := setup(t, nil)
	client, _ := start(t, sdk)
	devID, _ := enrolled(t, sdk, client, "site")

	apk := testapk.Make(t, testapk.Manifest)
	art, err := sdk.RegisterApplication(ctx, "site", apk)
	if err != nil {
		t.Fatal(err)
	}

	// RemoveApplication validation
	if err := sdk.RemoveApplication(ctx, "invalid scope!", art.SHA256); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid scope in RemoveApplication")
	}
	if err := sdk.RemoveApplication(ctx, "site", "invalid-digest"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid digest in RemoveApplication")
	}

	// Assign application
	op, err := sdk.AssignApplication(ctx, "site", devID, art.SHA256, "assign-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// Removing an active/referenced application must fail with ErrReferenced
	if err := sdk.RemoveApplication(ctx, "site", art.SHA256); !errors.Is(err, kylok.ErrReferenced) {
		t.Fatalf("expected ErrReferenced, got %v", err)
	}

	// ListOperations validation
	if _, err := sdk.ListOperations(ctx, "invalid scope", devID, "", 10); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := sdk.ListOperations(ctx, "site", "invalid-dev", "", 10); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := sdk.ListOperations(ctx, "site", devID, "", 0); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit 0")
	}
	if _, err := sdk.ListOperations(ctx, "site", devID, "", 101); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit > 100")
	}
	if _, err := sdk.ListOperations(ctx, "site", devID, "invalid-after-id", 10); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid after ID")
	}

	// ListOperations pagination with after
	ops, err := sdk.ListOperations(ctx, "site", devID, "", 10)
	if err != nil || len(ops) != 1 {
		t.Fatalf("expected 1 op, got %+v, err %v", ops, err)
	}
	opsAfter, err := sdk.ListOperations(ctx, "site", devID, op.ID, 10)
	if err != nil || len(opsAfter) != 0 {
		t.Fatalf("expected 0 ops after %s, got %+v", op.ID, opsAfter)
	}

	// AssignApplication validation
	if _, err := sdk.AssignApplication(ctx, "invalid scope", devID, art.SHA256, "k", time.Hour); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := sdk.AssignApplication(ctx, "site", "invalid-dev", art.SHA256, "k", time.Hour); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := sdk.AssignApplication(ctx, "site", devID, "invalid-digest", "k", time.Hour); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := sdk.AssignApplication(ctx, "site", devID, art.SHA256, "invalid key!", time.Hour); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}

	// RegisterApplication with generic non-DPC APK
	genericManifest := `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="io.generic.app" android:versionCode="1"><uses-sdk android:minSdkVersion="21" android:targetSdkVersion="35"/><application/></manifest>`
	genericAPK, err := testapk.InspectApplication(t, genericManifest)
	if err != nil {
		t.Fatal(err)
	}
	genericArt, err := sdk.RegisterApplication(ctx, "site", genericAPK)
	if err != nil {
		t.Fatal(err)
	}
	// Removing generic unreferenced application succeeds
	if err := sdk.RemoveApplication(ctx, "site", genericArt.SHA256); err != nil {
		t.Fatalf("failed to remove unreferenced application: %v", err)
	}
}

// TestNetworkPolicyAndProbeEdgeCases tests network secret validation, policy retrieval, and probe handler.
func TestNetworkPolicyAndProbeEdgeCases(t *testing.T) {
	sdk, _ := setup(t, nil)
	client, _ := start(t, sdk)
	devID, credential := enrolled(t, sdk, client, "site")

	// GetNetworkPolicy validation & not found
	if _, err := sdk.GetNetworkPolicy(ctx, "invalid scope", devID); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := sdk.GetNetworkPolicy(ctx, "site", "invalid-dev"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	// Non-configured policy returns empty/not found or defaults
	status, err := sdk.GetNetworkPolicy(ctx, "site", devID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Desired != nil {
		t.Fatalf("expected nil desired policy initially, got %+v", status.Desired)
	}

	// SetNetworkPolicy validation
	if _, err := sdk.SetNetworkPolicy(ctx, "invalid scope", devID, "k", kylok.NetworkPolicy{}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := sdk.SetNetworkPolicy(ctx, "site", "invalid-dev", "k", kylok.NetworkPolicy{}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := sdk.SetNetworkPolicy(ctx, "site", devID, "invalid key!", kylok.NetworkPolicy{}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}

	// Test networkProbe via SDK Handler
	handler := sdk.Handler()

	// Probe: Method not GET -> 405
	req := httptest.NewRequest(http.MethodPost, "/network-probe", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}

	// Probe: Missing or invalid headers -> 401
	req = httptest.NewRequest(http.MethodGet, "/network-probe", nil)
	req.TLS = &tls.ConnectionState{}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// Probe: Valid headers with 43-char nonce
	nonce := "1234567890123456789012345678901234567890123"
	req = httptest.NewRequest(http.MethodGet, "/network-probe", nil)
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("X-Kylok-Device-ID", devID)
	req.Header.Set("X-Kylok-Probe", nonce)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 NoContent, got %d", rec.Code)
	}

	// Probe: Wrong credential -> 401
	wrongCred := "0000000000000000000000000000000000000000000"
	req = httptest.NewRequest(http.MethodGet, "/network-probe", nil)
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Authorization", "Bearer "+wrongCred)
	req.Header.Set("X-Kylok-Device-ID", devID)
	req.Header.Set("X-Kylok-Probe", nonce)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// TestPolicyEdgeCases tests GetPolicy and SetPolicy validation and not found conditions.
func TestPolicyEdgeCases(t *testing.T) {
	sdk, _ := setup(t, nil)
	client, _ := start(t, sdk)
	devID, _ := enrolled(t, sdk, client, "site")

	// GetPolicy validation
	if _, err := sdk.GetPolicy(ctx, "invalid scope", devID); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := sdk.GetPolicy(ctx, "site", "invalid-dev"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}

	// SetPolicy validation
	if _, err := sdk.SetPolicy(ctx, "invalid scope", devID, "k", kylok.Policy{}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := sdk.SetPolicy(ctx, "site", "invalid-dev", "k", kylok.Policy{}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := sdk.SetPolicy(ctx, "site", devID, "invalid key!", kylok.Policy{}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}

	// Read unassigned policy returns default empty policy
	pol, err := sdk.GetPolicy(ctx, "site", devID)
	if err != nil {
		t.Fatal(err)
	}
	if pol.Desired != nil {
		t.Fatalf("expected nil desired initially, got %+v", pol.Desired)
	}
}

// TestDownloadHandlerEdgeCases tests application download handler branches.
func TestDownloadHandlerEdgeCases(t *testing.T) {
	sdk, _ := setup(t, nil)
	handler := sdk.Handler()

	// Non-GET/HEAD request -> 405
	req := httptest.NewRequest(http.MethodPut, "/artifacts/op1/app.apk", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}

	// Invalid URL path -> 404
	req = httptest.NewRequest(http.MethodGet, "/artifacts/not-enough-parts", nil)
	req.TLS = &tls.ConnectionState{}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	// Missing authorization -> 401
	digest := strings.Repeat("a", 64)
	validDevID := strings.Repeat("d", 43)
	req = httptest.NewRequest(http.MethodGet, "/artifacts/"+validDevID+"/"+digest+".apk", nil)
	req.TLS = &tls.ConnectionState{}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// TestSDKConstructorAndBootstrapHandler tests New options validation and BootstrapHandler error handling.
func TestSDKConstructorAndBootstrapHandler(t *testing.T) {
	apk := testapk.Make(t, testapk.Manifest)

	// Missing options in New
	if _, err := kylok.New(ctx, kylok.Options{}); err == nil {
		t.Fatal("expected error on empty options")
	}
	if _, err := kylok.New(ctx, kylok.Options{PublicURL: "not a url", Bootstrap: apk, DataDir: t.TempDir()}); err == nil {
		t.Fatal("expected error on invalid PublicURL")
	}
	if _, err := kylok.New(ctx, kylok.Options{PublicURL: "https://example.com", Bootstrap: apk}); err == nil {
		t.Fatal("expected error on missing DataDir")
	}

	sdk, _ := setup(t, nil)

	// QRWithOrigins validation
	if _, err := sdk.QRWithOrigins(ctx, "token", "ftp://bad-origin", "https://valid.origin"); err == nil {
		t.Fatal("expected error on invalid origin in QRWithOrigins")
	}

	// ListDevices and RevokeDevice validation
	if _, err := sdk.ListDevices(ctx, "invalid scope!", "", 10); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := sdk.ListDevices(ctx, "site", "", 0); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit 0")
	}
	if _, err := sdk.ListDevices(ctx, "site", "", 101); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit > 100")
	}
	if err := sdk.RevokeDevice(ctx, "invalid scope!", "d-1234567890123456"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if err := sdk.RevokeDevice(ctx, "site", "invalid-dev"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}

	// BootstrapHandler: method not allowed
	bHandler := sdk.BootstrapHandler()
	req := httptest.NewRequest(http.MethodPost, apk.Path(), nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	bHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}

	// BootstrapHandler: 404 on wrong path
	req = httptest.NewRequest(http.MethodGet, "/bootstrap/wrong.apk", nil)
	req.TLS = &tls.ConnectionState{}
	rec = httptest.NewRecorder()
	bHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	// BootstrapHandler: 404 on invalid digest hex
	req = httptest.NewRequest(http.MethodGet, "/bootstrap/nothex.apk", nil)
	req.TLS = &tls.ConnectionState{}
	rec = httptest.NewRecorder()
	bHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	// BootstrapHandler: 404 on nonexistent digest
	req = httptest.NewRequest(http.MethodGet, "/bootstrap/"+strings.Repeat("0", 64)+".apk", nil)
	req.TLS = &tls.ConnectionState{}
	rec = httptest.NewRecorder()
	bHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	// BootstrapHandler: HEAD on valid path
	req = httptest.NewRequest(http.MethodHead, apk.Path(), nil)
	req.TLS = &tls.ConnectionState{}
	rec = httptest.NewRecorder()
	bHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// TestSQLStoreDirectCoverage tests internal sqlstore edge conditions directly.
func TestSQLStoreDirectCoverage(t *testing.T) {
	ctx := context.Background()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	// We can test cancelled context on sqlstore methods
	sdk, _ := setup(t, nil)
	if _, err := sdk.GetDeviceState(cancelled, "site", "d-1234567890123456"); err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

// TestSDKConstructorOptionsValidation tests Options validation when constructing SDK.
func TestSDKConstructorOptionsValidation(t *testing.T) {
	apk := testapk.Make(t, testapk.Manifest)
	ctx := context.Background()

	// EventTimeout too small
	_, err := kylok.New(ctx, kylok.Options{
		PublicURL:    "https://kylok.test",
		Bootstrap:    apk,
		DataDir:      t.TempDir(),
		EventTimeout: time.Microsecond,
	})
	if err == nil {
		t.Fatal("expected error on EventTimeout < 1ms")
	}

	// EventTimeout too large
	_, err = kylok.New(ctx, kylok.Options{
		PublicURL:    "https://kylok.test",
		Bootstrap:    apk,
		DataDir:      t.TempDir(),
		EventTimeout: 31 * time.Second,
	})
	if err == nil {
		t.Fatal("expected error on EventTimeout > 30s")
	}

	// Typed nil provider
	var nilStore *sqlite.Store = nil
	_, err = kylok.New(ctx, kylok.Options{
		PublicURL: "https://kylok.test",
		Bootstrap: apk,
		DataDir:   t.TempDir(),
		Store:     nilStore,
	})
	if err == nil || !strings.Contains(err.Error(), "typed nil") {
		t.Fatalf("expected typed nil provider error, got %v", err)
	}

	// Invalid BootstrapURL
	_, err = kylok.New(ctx, kylok.Options{
		PublicURL:    "https://kylok.test",
		BootstrapURL: "https://kylok.test/invalid-path",
		Bootstrap:    apk,
		DataDir:      t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error on invalid BootstrapURL")
	}

	// Uncreatable DataDir (e.g. nested inside a file)
	tempFile := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(tempFile, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = kylok.New(ctx, kylok.Options{
		PublicURL: "https://kylok.test",
		Bootstrap: apk,
		DataDir:   filepath.Join(tempFile, "subdir"),
	})
	if err == nil {
		t.Fatal("expected error on uncreatable DataDir")
	}
}

// TestSDKHandlerAndBootstrapHandlerEdgeCases tests HTTP handlers under edge conditions.
func TestSDKHandlerAndBootstrapHandlerEdgeCases(t *testing.T) {
	sdk, _ := setup(t, nil)
	handler := sdk.Handler()
	bHandler := sdk.BootstrapHandler()

	// 1. Without TLS when origin is https: 426 StatusUpgradeRequired
	req := httptest.NewRequest(http.MethodGet, "/bootstrap/some.apk", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUpgradeRequired {
		t.Fatalf("expected 426 without TLS, got %d", rec.Code)
	}

	// 2. Closed SDK: 503 StatusServiceUnavailable
	sdk.Close()

	req = httptest.NewRequest(http.MethodGet, "/network-probe", nil)
	req.TLS = &tls.ConnectionState{}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on closed SDK handler, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/bootstrap/test.apk", nil)
	req.TLS = &tls.ConnectionState{}
	rec = httptest.NewRecorder()
	bHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on closed SDK bootstrapHandler, got %d", rec.Code)
	}
}

// TestManagementListAndQRValidation tests ListDevices and QR validation paths.
func TestManagementListAndQRValidation(t *testing.T) {
	sdk, _ := setup(t, nil)
	ctx := context.Background()

	// ListDevices invalid after pattern
	if _, err := sdk.ListDevices(ctx, "site", "invalid-after-pattern", 10); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid after pattern, got %v", err)
	}
	if _, err := sdk.ListDevices(ctx, "site", "", 0); err == nil {
		t.Fatal("expected error on limit 0")
	}
	if _, err := sdk.ListDevices(ctx, "site", "", 101); err == nil {
		t.Fatal("expected error on limit > 100")
	}

	// QRWithOrigins invalid origins
	enr := issue(t, sdk, "site")
	if _, err := sdk.QRWithOrigins(ctx, enr.Token, "invalid://origin", "https://kylok.test"); err == nil {
		t.Fatal("expected error on invalid apiOrigin")
	}
	if _, err := sdk.QRWithOrigins(ctx, enr.Token, "https://kylok.test", "invalid://origin"); err == nil {
		t.Fatal("expected error on invalid bootstrapOrigin")
	}
	if _, err := sdk.QRWithOrigins(ctx, "non-existent-token", "https://kylok.test", "https://kylok.test"); err == nil {
		t.Fatal("expected error on non-existent token")
	}
}

// TestQueryDevicesAndOperationsPagination tests keyset cursor decoding and pagination.
func TestQueryDevicesAndOperationsPagination(t *testing.T) {
	sdk, _ := setup(t, nil)
	ctx := context.Background()

	// QueryDevices invalid filter state
	if _, err := sdk.QueryDevices(ctx, "site", kylok.DeviceFilter{State: "INVALID_STATE"}, kylok.PageRequest{Limit: 10}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// QueryDevices malformed cursor: non-base64
	if _, err := sdk.QueryDevices(ctx, "site", kylok.DeviceFilter{}, kylok.PageRequest{Cursor: "!!!not-b64!!!"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on invalid base64 cursor, got %v", err)
	}

	// QueryDevices malformed cursor: invalid JSON
	badJSON := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	if _, err := sdk.QueryDevices(ctx, "site", kylok.DeviceFilter{}, kylok.PageRequest{Cursor: badJSON}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on bad JSON cursor, got %v", err)
	}

	// QueryDevices malformed cursor: wrong binding
	wrongBinding := base64.RawURLEncoding.EncodeToString([]byte(`{"Version":1,"Binding":"wrong","After":"dev"}`))
	if _, err := sdk.QueryDevices(ctx, "site", kylok.DeviceFilter{}, kylok.PageRequest{Cursor: wrongBinding}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on wrong binding cursor, got %v", err)
	}

	// QueryOperations invalid state
	devID := strings.Repeat("d", 43)
	if _, err := sdk.QueryOperations(ctx, "site", devID, kylok.OperationFilter{State: "INVALID"}, kylok.PageRequest{Limit: 10}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.QueryOperations(ctx, "site", devID, kylok.OperationFilter{}, kylok.PageRequest{Cursor: "invalid-b64"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

// TestApplicationsValidationAndErrorHandling tests Application registration and assignment validation.
func TestApplicationsValidationAndErrorHandling(t *testing.T) {
	sdk, _ := setup(t, nil)
	ctx := context.Background()

	// RegisterApplication with unverified APK
	unverified := &provisioning.APK{}
	if _, err := sdk.RegisterApplication(ctx, "site", unverified); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for unverified APK, got %v", err)
	}

	// RegisterApplication invalid scope
	apk := testapk.Make(t, testapk.Manifest)
	if _, err := sdk.RegisterApplication(ctx, "invalid_scope_!", apk); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid scope, got %v", err)
	}

	// AssignApplication validation bounds
	validDevID := strings.Repeat("d", 43)
	validDigest := strings.Repeat("a", 64)
	if _, err := sdk.AssignApplication(ctx, "", validDevID, validDigest, "k", time.Hour); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty scope, got %v", err)
	}
	if _, err := sdk.AssignApplication(ctx, "site", "invalid-dev", validDigest, "k", time.Hour); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid dev, got %v", err)
	}
	if _, err := sdk.AssignApplication(ctx, "site", validDevID, "short-digest", "k", time.Hour); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for bad digest, got %v", err)
	}
	if _, err := sdk.AssignApplication(ctx, "site", validDevID, validDigest, "k", 30*time.Second); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for timeout < 1m, got %v", err)
	}
	if _, err := sdk.AssignApplication(ctx, "site", validDevID, validDigest, "k", 8*24*time.Hour); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for timeout > 7d, got %v", err)
	}
}

// TestCommandsValidationAndPublicObservation tests commands validation and querying.
func TestCommandsValidationAndPublicObservation(t *testing.T) {
	sdk, _ := setup(t, nil)
	ctx := context.Background()
	validDevID := strings.Repeat("d", 43)

	// SubmitCommand validation
	if _, err := sdk.SubmitCommand(ctx, "", validDevID, kylok.CommandReboot, "k", time.Hour); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.SubmitCommand(ctx, "site", "bad-dev", kylok.CommandReboot, "k", time.Hour); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.SubmitCommand(ctx, "site", validDevID, kylok.CommandKind("UNKNOWN"), "k", time.Hour); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.SubmitCommand(ctx, "site", validDevID, kylok.CommandReboot, "k", 10*time.Second); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for timeout < 1m, got %v", err)
	}

	// GetCommand validation
	if _, err := sdk.GetCommand(ctx, "", validDevID, validDevID); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.GetCommand(ctx, "site", "bad-dev", validDevID); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// QueryCommands validation
	if _, err := sdk.QueryCommands(ctx, "", validDevID, kylok.PageRequest{}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.QueryCommands(ctx, "site", "bad-dev", kylok.PageRequest{}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.QueryCommands(ctx, "site", validDevID, kylok.PageRequest{Cursor: "invalid-b64"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on invalid cursor, got %v", err)
	}

	// CancelCommand validation
	if _, err := sdk.CancelCommand(ctx, "", validDevID, validDevID); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.CancelCommand(ctx, "site", "bad-dev", validDevID); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

// TestFleetValidationAndTargets tests fleet groups, targets, and operations.
func TestFleetValidationAndTargets(t *testing.T) {
	sdk, _ := setup(t, nil)
	ctx := context.Background()
	validDevID := strings.Repeat("d", 43)

	// ReplaceGroup with invalid revision
	if _, err := sdk.ReplaceGroup(ctx, "site", "group1", []string{validDevID}, 1<<62); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	// ReplaceGroup with invalid device ID
	if _, err := sdk.ReplaceGroup(ctx, "site", "group1", []string{"invalid-device-id"}, 0); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// GetGroup validation
	if _, err := sdk.GetGroup(ctx, "", "g1"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// QueryGroups validation
	if _, err := sdk.QueryGroups(ctx, "", kylok.PageRequest{}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.QueryGroups(ctx, "site", kylok.PageRequest{Cursor: "invalid-b64"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// CreateFleetAssignment validation
	if _, err := sdk.CreateFleetAssignment(ctx, kylok.FleetRequest{ScopeID: "", ID: "f1", ArtifactSHA256: strings.Repeat("a", 64)}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.GetFleetAssignment(ctx, "", "f1"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.CancelFleetAssignment(ctx, "", "f1"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.CancelOperation(ctx, "", validDevID, validDevID); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

// TestChangesFeedCursorValidation tests feed cursor decoding and boundary checks.
func TestChangesFeedCursorValidation(t *testing.T) {
	sdk, _ := setup(t, nil)
	ctx := context.Background()

	// ListChanges with invalid cursor: cursor too large
	hugeCursor := strings.Repeat("A", 1025)
	if _, err := sdk.ListChanges(ctx, "site", hugeCursor, 10); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for huge cursor, got %v", err)
	}

	// ListChanges with invalid cursor: non-base64
	if _, err := sdk.ListChanges(ctx, "site", "!@#$%", 10); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for non-base64 cursor, got %v", err)
	}

	// ListChanges with invalid cursor: wrong scope
	wrongScope := base64.RawURLEncoding.EncodeToString([]byte(`{"Version":1,"Scope":"other","Kind":"changes"}`))
	if _, err := sdk.ListChanges(ctx, "site", wrongScope, 10); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for wrong scope cursor, got %v", err)
	}

	// ListChanges with invalid limit
	if _, err := sdk.ListChanges(ctx, "site", "", 0); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0, got %v", err)
	}
	if _, err := sdk.ListChanges(ctx, "site", "", 101); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit > 100, got %v", err)
	}

	// ReadProjection with invalid limit
	if _, err := sdk.ReadProjection(ctx, "site", "", 0); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0, got %v", err)
	}

	// GetDeviceState with invalid device
	if _, err := sdk.GetDeviceState(ctx, "site", "invalid-dev"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid dev, got %v", err)
	}
}

// TestDownloadHandlerScenarios tests artifact download endpoint.
func TestDownloadHandlerScenarios(t *testing.T) {
	sdk, opts := setup(t, nil)
	client, _ := start(t, sdk)
	ctx := context.Background()
	handler := sdk.Handler()

	apk := testapk.Make(t, testapk.Manifest)
	art, err := sdk.RegisterApplication(ctx, "site", apk)
	if err != nil {
		t.Fatal(err)
	}

	enr := issue(t, sdk, "site")
	req, key := request(t, enr.Token)
	resp, err := client.Enroll(ctx, connect.NewRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	cred := decrypt(t, key, resp.Msg, req.AttemptId)
	devID := resp.Msg.Device.Id

	op, err := sdk.AssignApplication(ctx, "site", devID, art.SHA256, "k-down-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// Sync to lease the operation
	syncReq := syncRequest(devID, cred)
	syncResp, err := client.Sync(ctx, syncReq)
	if err != nil {
		t.Fatal(err)
	}
	if len(syncResp.Msg.Operations) == 0 {
		t.Fatal("expected leased operation in sync response")
	}
	leasedOp := syncResp.Msg.Operations[0]

	downloadPath := "/artifacts/" + op.ID + "/" + art.SHA256 + ".apk"

	// 1. Wrong method: POST -> 405
	httpReq := httptest.NewRequest(http.MethodPost, downloadPath, nil)
	httpReq.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}

	// 2. Missing auth -> 401
	httpReq = httptest.NewRequest(http.MethodGet, downloadPath, nil)
	httpReq.TLS = &tls.ConnectionState{}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// 3. Bad lease -> 403
	httpReq = httptest.NewRequest(http.MethodGet, downloadPath, nil)
	httpReq.TLS = &tls.ConnectionState{}
	httpReq.Header.Set("Authorization", "Bearer "+cred)
	httpReq.Header.Set("X-Kylok-Device-ID", devID)
	httpReq.Header.Set("X-Kylok-Lease-ID", "wrong_lease")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	// 4. Valid lease -> 200
	httpReq = httptest.NewRequest(http.MethodGet, downloadPath, nil)
	httpReq.TLS = &tls.ConnectionState{}
	httpReq.Header.Set("Authorization", "Bearer "+cred)
	httpReq.Header.Set("X-Kylok-Device-ID", devID)
	httpReq.Header.Set("X-Kylok-Lease-ID", leasedOp.LeaseId)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty body")
	}

	// 5. Artifact size mismatch in database -> 500
	db, err := sql.Open("sqlite", filepath.Join(opts.DataDir, "kylok.sqlite"))
	if err == nil {
		defer db.Close()
		_, _ = db.Exec("UPDATE artifacts SET data=json_set(data, '$.size_bytes', 999999)")
		httpReq = httptest.NewRequest(http.MethodGet, downloadPath, nil)
		httpReq.TLS = &tls.ConnectionState{}
		httpReq.Header.Set("Authorization", "Bearer "+cred)
		httpReq.Header.Set("X-Kylok-Device-ID", devID)
		httpReq.Header.Set("X-Kylok-Lease-ID", leasedOp.LeaseId)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httpReq)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on size mismatch, got %d", rec.Code)
		}
	}

	// 6. Artifact missing completely -> 503
	artFile := filepath.Join(opts.DataDir, "artifacts", art.SHA256+".apk")
	_ = os.Remove(artFile)
	httpReq = httptest.NewRequest(http.MethodGet, downloadPath, nil)
	httpReq.TLS = &tls.ConnectionState{}
	httpReq.Header.Set("Authorization", "Bearer "+cred)
	httpReq.Header.Set("X-Kylok-Device-ID", devID)
	httpReq.Header.Set("X-Kylok-Lease-ID", leasedOp.LeaseId)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on missing artifact, got %d", rec.Code)
	}
}

type mockCloser struct {
	*bytes.Reader
}

func (mockCloser) Close() error { return nil }

type mockArtifactStore struct {
	putErr      error
	openErr     error
	corruptOpen bool
	data        []byte
}

func (m *mockArtifactStore) Put(ctx context.Context, digest string, data []byte) error {
	if m.putErr != nil {
		return m.putErr
	}
	m.data = data
	return nil
}

func (m *mockArtifactStore) Open(ctx context.Context, digest string) (ports.ReadSeekCloser, error) {
	if m.openErr != nil {
		return nil, m.openErr
	}
	if m.corruptOpen {
		return mockCloser{bytes.NewReader([]byte("corrupted"))}, nil
	}
	return mockCloser{bytes.NewReader(m.data)}, nil
}

// TestSDKNewArtifactStoreFailures tests bootstrap persistence and integrity checks during New.
func TestSDKNewArtifactStoreFailures(t *testing.T) {
	apk := testapk.Make(t, testapk.Manifest)
	ctx := context.Background()

	// 1. Put fails -> persist bootstrap error
	{
		mock := &mockArtifactStore{putErr: errors.New("put failed")}
		opts := kylok.Options{
			PublicURL: "https://kylok.test",
			Bootstrap: apk,
			DataDir:   t.TempDir(),
			Artifacts: mock,
		}
		if _, err := kylok.New(ctx, opts); err == nil || !strings.Contains(err.Error(), "persist bootstrap") {
			t.Fatalf("expected persist bootstrap error, got %v", err)
		}
	}

	// 2. Open fails -> open persisted bootstrap error
	{
		mock := &mockArtifactStore{openErr: errors.New("open failed")}
		opts := kylok.Options{
			PublicURL: "https://kylok.test",
			Bootstrap: apk,
			DataDir:   t.TempDir(),
			Artifacts: mock,
		}
		if _, err := kylok.New(ctx, opts); err == nil || !strings.Contains(err.Error(), "open persisted bootstrap") {
			t.Fatalf("expected open persisted bootstrap error, got %v", err)
		}
	}

	// 3. Different content returned -> artifact provider returned different bootstrap content
	{
		mock := &mockArtifactStore{corruptOpen: true}
		opts := kylok.Options{
			PublicURL: "https://kylok.test",
			Bootstrap: apk,
			DataDir:   t.TempDir(),
			Artifacts: mock,
		}
		if _, err := kylok.New(ctx, opts); err == nil || !strings.Contains(err.Error(), "different bootstrap content") {
			t.Fatalf("expected different content error, got %v", err)
		}
	}
}

type mockNetworkSecrets struct {
	secret []byte
	err    error
}

func (m mockNetworkSecrets) ReadNetworkSecret(ctx context.Context, scope, ref string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.secret, nil
}

// TestNetworkSecretsAndProbeScenarios tests Wi-Fi secret formats, resolution, and probe endpoints.
func TestNetworkSecretsAndProbeScenarios(t *testing.T) {
	ctx := context.Background()

	// 1. Missing NetworkSecrets provider when configuring protected Wi-Fi
	sdkNoSec, _ := setup(t, nil)
	clientNoSec, _ := start(t, sdkNoSec)
	enr := issue(t, sdkNoSec, "site")
	req, key := request(t, enr.Token)
	resp, _ := clientNoSec.Enroll(ctx, connect.NewRequest(req))
	cred := decrypt(t, key, resp.Msg, req.AttemptId)
	devID := resp.Msg.Device.Id

	_, err := sdkNoSec.SetNetworkPolicy(ctx, "site", devID, "k-net-1", kylok.NetworkPolicy{
		PrimaryID: "wpa-1",
		Profiles: []kylok.WifiProfile{
			{ID: "wpa-1", SSID: "SecNet", Security: "WPA2_PSK", SecretRef: "sec-ref-1"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "NetworkSecrets provider required") {
		t.Fatalf("expected NetworkSecrets required error, got %v", err)
	}

	// 2. Wi-Fi with OPEN security -> no secret needed, succeeds
	_, err = sdkNoSec.SetNetworkPolicy(ctx, "site", devID, "k-net-open", kylok.NetworkPolicy{
		PrimaryID: "open-1",
		Profiles: []kylok.WifiProfile{
			{ID: "open-1", SSID: "OpenNet", Security: "OPEN"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	syncReq := syncRequest(devID, cred)
	syncReq.Msg.Sequence = 1
	if _, err := clientNoSec.Sync(ctx, syncReq); err != nil {
		t.Fatalf("unexpected error on OPEN Wi-Fi sync: %v", err)
	}

	// 3. Various secret formats with mock provider
	apk := testapk.Make(t, testapk.Manifest)
	mock := &mockNetworkSecrets{}
	opts := kylok.Options{
		PublicURL:      "https://kylok.test",
		Bootstrap:      apk,
		DataDir:        t.TempDir(),
		NetworkSecrets: mock,
	}
	sdkSec, err := kylok.New(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer sdkSec.Close()

	// Enroll device on sdkSec
	clientSec, _ := start(t, sdkSec)
	enrSec := issue(t, sdkSec, "site")
	reqSec, keySec := request(t, enrSec.Token)
	respSec, _ := clientSec.Enroll(ctx, connect.NewRequest(reqSec))
	_ = decrypt(t, keySec, respSec.Msg, reqSec.AttemptId)
	devIDSec := respSec.Msg.Device.Id

	// 3a. Secret too short (< 8 chars for WPA2_PSK)
	mock.secret = []byte("short")
	_, err = sdkSec.SetNetworkPolicy(ctx, "site", devIDSec, "k-net-short", kylok.NetworkPolicy{
		PrimaryID: "wpa-short",
		Profiles: []kylok.WifiProfile{
			{ID: "wpa-short", SSID: "ShortNet", Security: "WPA2_PSK", SecretRef: "sec-short"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid Wi-Fi secret format") {
		t.Fatalf("expected invalid format for short secret, got %v", err)
	}

	// 3b. Secret with byte < 32
	mock.secret = []byte("password\x01\x02")
	_, err = sdkSec.SetNetworkPolicy(ctx, "site", devIDSec, "k-net-ctrl", kylok.NetworkPolicy{
		PrimaryID: "wpa-ctrl",
		Profiles: []kylok.WifiProfile{
			{ID: "wpa-ctrl", SSID: "CtrlNet", Security: "WPA2_PSK", SecretRef: "sec-ctrl"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid Wi-Fi secret format") {
		t.Fatalf("expected invalid format for control chars, got %v", err)
	}

	// 3c. Secret with byte > 126
	mock.secret = []byte("password\x7f\x80")
	_, err = sdkSec.SetNetworkPolicy(ctx, "site", devIDSec, "k-net-high", kylok.NetworkPolicy{
		PrimaryID: "wpa-high",
		Profiles: []kylok.WifiProfile{
			{ID: "wpa-high", SSID: "HighNet", Security: "WPA2_PSK", SecretRef: "sec-high"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid Wi-Fi secret format") {
		t.Fatalf("expected invalid format for high chars, got %v", err)
	}

	// 3d. 64-char valid hex PSK
	mock.secret = []byte(strings.Repeat("a", 64))
	_, err = sdkSec.SetNetworkPolicy(ctx, "site", devIDSec, "k-net-hex", kylok.NetworkPolicy{
		PrimaryID: "wpa-hex",
		Profiles: []kylok.WifiProfile{
			{ID: "wpa-hex", SSID: "HexNet", Security: "WPA2_PSK", SecretRef: "sec-hex"},
		},
	})
	if err != nil {
		t.Fatalf("expected valid 64-hex PSK to succeed, got %v", err)
	}

	// 3e. Provider error -> ErrNetworkSecretUnavailable
	mock.err = errors.New("vault error")
	_, err = sdkSec.SetNetworkPolicy(ctx, "site", devIDSec, "k-net-err", kylok.NetworkPolicy{
		PrimaryID: "wpa-err",
		Profiles: []kylok.WifiProfile{
			{ID: "wpa-err", SSID: "ErrNet", Security: "WPA2_PSK", SecretRef: "sec-err"},
		},
	})
	if !errors.Is(err, kylok.ErrNetworkSecretUnavailable) {
		t.Fatalf("expected ErrNetworkSecretUnavailable, got %v", err)
	}
}

// TestManagementAndKylokEdgeCases tests edge cases for enrollment TTL, QR artifact mismatch, observations, and limits.
func TestManagementAndKylokEdgeCases(t *testing.T) {
	sdk, opts := setup(t, nil)
	client, _ := start(t, sdk)
	ctx := context.Background()

	// 1. CreateEnrollment invalid TTL bounds
	if _, err := sdk.CreateEnrollment(ctx, "site", 30*time.Second); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for TTL < 1m, got %v", err)
	}
	if _, err := sdk.CreateEnrollment(ctx, "site", 25*time.Hour); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for TTL > 24h, got %v", err)
	}

	// 2. QR with mismatched bootstrap APK SHA256
	enr := issue(t, sdk, "site")
	db, err := sql.Open("sqlite", filepath.Join(opts.DataDir, "kylok.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, _ = db.Exec("UPDATE enrollments SET artifact_sha256=?", strings.Repeat("0", 64))
	if _, err := sdk.QR(ctx, enr.Token); err == nil || !strings.Contains(err.Error(), "different bootstrap APK") {
		t.Fatalf("expected different bootstrap APK error in QR, got %v", err)
	}

	// 3. GetDevice on device with installed applications
	enr2 := issue(t, sdk, "site")
	req2, key2 := request(t, enr2.Token)
	req2.Inventory.Capabilities = []string{"command.reboot", "command.lock"}
	req2.Inventory.Applications = []*kylokv1.InstalledApplication{
		{
			PackageName:         "com.example.installed",
			VersionCode:         10,
			SigningCertificates: []string{strings.Repeat("a", 64)},
		},
	}
	resp2, err := client.Enroll(ctx, connect.NewRequest(req2))
	if err != nil {
		t.Fatal(err)
	}
	_ = decrypt(t, key2, resp2.Msg, req2.AttemptId)
	devID2 := resp2.Msg.Device.Id

	dev, err := sdk.GetDevice(ctx, "site", devID2)
	if err != nil {
		t.Fatal(err)
	}
	if len(dev.Inventory.Applications) == 0 {
		t.Fatal("expected device to have installed applications")
	}

	// 4. GetCommand on command with observation
	cmd, err := sdk.SubmitCommand(ctx, "site", devID2, kylok.CommandReboot, "k-obs", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	obsJSON := `{"boot_before":10,"boot_after":11,"secure":true,"locked":true}`
	_, _ = db.Exec("UPDATE commands SET data=json_set(data, '$.observation', json(?)) WHERE id=?", obsJSON, cmd.ID)
	readCmd, err := sdk.GetCommand(ctx, "site", devID2, cmd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if readCmd.Observation == nil || readCmd.Observation.BootBefore != 10 {
		t.Fatalf("expected observation on command, got %+v", readCmd.Observation)
	}

	// 5. ReplaceGroup with > 1024 devices
	hugeTargets := make([]string, 1025)
	for i := range hugeTargets {
		hugeTargets[i] = strings.Repeat("d", 43)
	}
	if _, err := sdk.ReplaceGroup(ctx, "site", "g_huge", hugeTargets, 0); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for > 1024 targets, got %v", err)
	}

	// 6. SetNetworkPolicy idempotency conflict: same key, different policy
	_, err = sdk.SetNetworkPolicy(ctx, "site", devID2, "k-net-pol-1", kylok.NetworkPolicy{
		PrimaryID: "w1",
		Profiles:  []kylok.WifiProfile{{ID: "w1", SSID: "Net1", Security: "OPEN"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sdk.SetNetworkPolicy(ctx, "site", devID2, "k-net-pol-1", kylok.NetworkPolicy{
		PrimaryID: "w2",
		Profiles:  []kylok.WifiProfile{{ID: "w2", SSID: "Net2", Security: "OPEN"}},
	})
	if !errors.Is(err, kylok.ErrConflict) {
		t.Fatalf("expected ErrConflict on SetNetworkPolicy idempotency mismatch, got %v", err)
	}
}

// TestApplicationsSelfUpdateAndDebugValidation tests debuggable and self-update validation on applications.
func TestApplicationsSelfUpdateAndDebugValidation(t *testing.T) {
	sdk, _ := setup(t, nil)
	ctx := context.Background()

	// 1. debuggable APK without allowDebugAPK -> invalidArgument
	debugManifest := strings.Replace(testapk.Manifest, `<application android:allowBackup="false">`, `<application android:allowBackup="false" android:debuggable="true">`, 1)
	debugAPK := testapk.Make(t, debugManifest)
	if _, err := sdk.RegisterApplication(ctx, "site", debugAPK); err == nil || !strings.Contains(err.Error(), "AllowDebugAPK") {
		t.Fatalf("expected AllowDebugAPK error, got %v", err)
	}

	// 2. self-update APK with same package name as bootstrap but not DPC -> invalidArgument
	samePkgNonDPCManifest := `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="io.kylok.unit" android:versionCode="4"><uses-sdk android:minSdkVersion="33" android:targetSdkVersion="37"/><application/></manifest>`
	samePkgNonDPC, err := testapk.InspectApplication(t, samePkgNonDPCManifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sdk.RegisterApplication(ctx, "site", samePkgNonDPC); err == nil || !strings.Contains(err.Error(), "self-update APK must retain the DPC provisioning components") {
		t.Fatalf("expected self-update DPC error, got %v", err)
	}
}

// TestManagementAndFleetValidationMore tests error branches in management, fleet, and ready checks.
func TestManagementAndFleetValidationMore(t *testing.T) {
	sdk, opts := setup(t, nil)
	ctx := context.Background()
	validDevID := strings.Repeat("d", 43)
	validOpID := strings.Repeat("o", 43)

	// GetDevice invalid args
	if _, err := sdk.GetDevice(ctx, "invalid_scope!", validDevID); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.GetDevice(ctx, "site", "invalid_dev"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// GetOperation invalid args
	if _, err := sdk.GetOperation(ctx, "invalid_scope!", validDevID, validOpID); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.GetOperation(ctx, "site", "invalid_dev", validOpID); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.GetOperation(ctx, "site", validDevID, "invalid_op"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// GetFleetAssignment invalid args
	if _, err := sdk.GetFleetAssignment(ctx, "invalid_scope!", "f1"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.GetFleetAssignment(ctx, "site", "invalid_id!"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// CancelFleetAssignment invalid args
	if _, err := sdk.CancelFleetAssignment(ctx, "invalid_scope!", "f1"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// CancelOperation invalid args
	if _, err := sdk.CancelOperation(ctx, "invalid_scope!", validDevID, validOpID); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk.CancelOperation(ctx, "site", "invalid_dev", validOpID); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// CreateFleetAssignment invalid artifact SHA256
	if _, err := sdk.CreateFleetAssignment(ctx, kylok.FleetRequest{ScopeID: "site", ID: "f1", ArtifactSHA256: "bad-sha"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on bad artifact sha in FleetRequest, got %v", err)
	}

	// QueryOperations pagination NextCursor check
	client, _ := start(t, sdk)
	enr := issue(t, sdk, "site")
	req, key := request(t, enr.Token)
	resp, _ := client.Enroll(ctx, connect.NewRequest(req))
	_ = decrypt(t, key, resp.Msg, req.AttemptId)
	devID := resp.Msg.Device.Id

	apk := testapk.Make(t, testapk.Manifest)
	art, _ := sdk.RegisterApplication(ctx, "site", apk)
	_, _ = sdk.AssignApplication(ctx, "site", devID, art.SHA256, "k-page-1", time.Hour)
	_, _ = sdk.AssignApplication(ctx, "site", devID, art.SHA256, "k-page-2", time.Hour)

	page, err := sdk.QueryOperations(ctx, "site", devID, kylok.OperationFilter{}, kylok.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.NextCursor == "" {
		t.Fatal("expected NextCursor for QueryOperations when items > limit")
	}

	// Ready check failure when artifact is deleted
	_ = os.Remove(filepath.Join(opts.DataDir, "artifacts", opts.Bootstrap.Metadata().SHA256+".apk"))
	if err := sdk.Ready(ctx); err == nil {
		t.Fatal("expected error on Ready when bootstrap artifact is missing")
	}
}

// TestFinalRootSDKCoverageGate tests the remaining boundary conditions in the root SDK.
func TestFinalRootSDKCoverageGate(t *testing.T) {
	ctx := context.Background()

	// 1. Debuggable bootstrap APK without AllowDebugAPK -> error
	debugManifest := strings.Replace(testapk.Manifest, `<application android:allowBackup="false">`, `<application android:allowBackup="false" android:debuggable="true">`, 1)
	debugAPK := testapk.Make(t, debugManifest)
	if _, err := kylok.New(ctx, kylok.Options{
		PublicURL: "https://kylok.test",
		Bootstrap: debugAPK,
		DataDir:   t.TempDir(),
	}); err == nil || !strings.Contains(err.Error(), "AllowDebugAPK") {
		t.Fatalf("expected AllowDebugAPK error on debug bootstrap, got %v", err)
	}

	sdk, opts := setup(t, nil)
	validDevID := strings.Repeat("d", 43)

	// 2. Tampered bootstrap APK serving in BootstrapHandler -> 404 (artifact integrity error from Open)
	bootstrapFile := filepath.Join(opts.DataDir, "artifacts", opts.Bootstrap.Metadata().SHA256+".apk")
	_ = os.Chmod(bootstrapFile, 0600)
	_ = os.WriteFile(bootstrapFile, []byte("tampered bootstrap"), 0600)

	bHandler := sdk.BootstrapHandler()
	req := httptest.NewRequest(http.MethodGet, opts.Bootstrap.Path(), nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	bHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on tampered bootstrap APK, got %d", rec.Code)
	}

	// 3. networkProbe when SDK is closed -> 503
	enr := issue(t, sdk, "site")
	client, _ := start(t, sdk)
	reqEnroll, key := request(t, enr.Token)
	respEnroll, _ := client.Enroll(ctx, connect.NewRequest(reqEnroll))
	cred := decrypt(t, key, respEnroll.Msg, reqEnroll.AttemptId)
	devID := respEnroll.Msg.Device.Id

	// Close SDK and verify probe returns 503
	sdk.Close()
	probeReq := httptest.NewRequest(http.MethodGet, "/network-probe", nil)
	probeReq.Header.Set("Authorization", "Bearer "+cred)
	probeReq.Header.Set("X-Kylok-Device-ID", devID)
	probeReq.Header.Set("X-Kylok-Probe", strings.Repeat("p", 43))
	rec = httptest.NewRecorder()
	sdk.Handler().ServeHTTP(rec, probeReq)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on probe with closed SDK, got %d", rec.Code)
	}

	// 3b. BootstrapHandler when SDK is closed -> 503
	rec = httptest.NewRecorder()
	sdk.BootstrapHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on bootstrap handler when SDK closed, got %d", rec.Code)
	}

	// 4. SetNetworkPolicy invalid policy
	sdk2, _ := setup(t, nil)
	defer sdk2.Close()
	if _, err := sdk2.SetNetworkPolicy(ctx, "site", validDevID, "k1", kylok.NetworkPolicy{PrimaryID: "not_in_profiles"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid network policy, got %v", err)
	}

	// 4b. QRWithOrigins with invalid bootstrap origin
	enr2 := issue(t, sdk2, "site")
	if _, err := sdk2.QRWithOrigins(ctx, enr2.Token, "http://127.0.0.1:8080", "invalid-bootstrap-origin"); err == nil {
		t.Fatal("expected error on invalid bootstrap origin in QRWithOrigins")
	}

	// 4c. New with invalid DataDir -> error from sqlite.Open
	if _, err := kylok.New(ctx, kylok.Options{DataDir: "/dev/null/forbidden", Bootstrap: opts.Bootstrap}); err == nil {
		t.Fatal("expected error on uncreatable DataDir")
	}

	// 4d. Revoked deviceMessage
	client2, _ := start(t, sdk2)
	reqE, keyE := request(t, enr2.Token)
	respE, _ := client2.Enroll(ctx, connect.NewRequest(reqE))
	_ = keyE
	devRevID := respE.Msg.Device.Id
	_ = sdk2.RevokeDevice(ctx, "site", devRevID)
	revDev, err := sdk2.GetDevice(ctx, "site", devRevID)
	if err != nil || revDev.State != "REVOKED" {
		t.Fatalf("expected REVOKED state, got %+v, err=%v", revDev, err)
	}

	// 5. ListChanges with feedCursor having Snapshot in changes kind
	badCursor := base64.RawURLEncoding.EncodeToString([]byte(`{"Version":1,"Scope":"site","Kind":"changes","Snapshot":"invalid-snapshot-field"}`))
	if _, err := sdk2.ListChanges(ctx, "site", badCursor, 10); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on invalid changes cursor, got %v", err)
	}

	// 6. Fleet and commands invalid arguments
	if _, err := sdk2.CreateFleetAssignment(ctx, kylok.FleetRequest{ScopeID: "site", ID: "invalid_id!", ArtifactSHA256: strings.Repeat("a", 64)}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk2.GetFleetAssignment(ctx, "site", "invalid_id!"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk2.CancelFleetAssignment(ctx, "site", "invalid_id!"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk2.CancelOperation(ctx, "site", validDevID, "invalid_id!"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := sdk2.CancelCommand(ctx, "site", "invalid_dev", "c1"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// 7. RegisterApplication with cancelled context -> artifacts.Put error
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := sdk2.RegisterApplication(cancelledCtx, "site", opts.Bootstrap); err == nil {
		t.Fatal("expected error on cancelled RegisterApplication")
	}

	// 8. CreateFleetAssignment with duplicate targets -> validFleetTargets error
	if _, err := sdk2.CreateFleetAssignment(ctx, kylok.FleetRequest{
		ScopeID: "site", ID: "dup_targets", ArtifactSHA256: opts.Bootstrap.Metadata().SHA256,
		DeviceIDs: []string{validDevID, validDevID}, NotBefore: time.Now().UTC(), Deadline: time.Now().UTC().Add(time.Hour),
	}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on duplicate targets, got %v", err)
	}

	// 9. QueryFleetAssignments and QueryGroups with invalid cursor
	if _, err := sdk2.QueryFleetAssignments(ctx, "site", kylok.PageRequest{Cursor: "invalid-cursor"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on invalid fleet cursor, got %v", err)
	}
	if _, err := sdk2.QueryGroups(ctx, "site", kylok.PageRequest{Cursor: "invalid-cursor"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on invalid groups cursor, got %v", err)
	}

	// 10. QueryDevices, QueryOperations, QueryCommands with invalid cursor
	if _, err := sdk2.QueryDevices(ctx, "site", kylok.DeviceFilter{}, kylok.PageRequest{Cursor: "invalid-cursor"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on invalid devices cursor, got %v", err)
	}
	if _, err := sdk2.QueryOperations(ctx, "site", validDevID, kylok.OperationFilter{}, kylok.PageRequest{Cursor: "invalid-cursor"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on invalid operations cursor, got %v", err)
	}
	if _, err := sdk2.QueryCommands(ctx, "site", validDevID, kylok.PageRequest{Cursor: "invalid-cursor"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on invalid commands cursor, got %v", err)
	}

	// 11. DecodeFeedCursor with projection cursor having sequence != 0 -> ErrInvalidArgument
	badProjCursor := base64.RawURLEncoding.EncodeToString([]byte(`{"Version":1,"Scope":"site","Kind":"projection","Snapshot":"dev-1234567890123456","Sequence":5}`))
	if _, err := sdk2.ReadProjection(ctx, "site", badProjCursor, 10); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on projection cursor with sequence, got %v", err)
	}

	// 12. ReadProjection with assigned application exercising line 224 (Desired application mapping)
	enrDev3 := issue(t, sdk2, "site")
	reqE3, _ := request(t, enrDev3.Token)
	respE3, _ := client2.Enroll(ctx, connect.NewRequest(reqE3))
	devAppID := respE3.Msg.Device.Id
	if _, err := sdk2.RegisterApplication(ctx, "site", opts.Bootstrap); err != nil {
		t.Fatal(err)
	}
	if _, err := sdk2.AssignApplication(ctx, "site", devAppID, opts.Bootstrap.Metadata().SHA256, "k_proj_app", time.Hour); err != nil {
		t.Fatal(err)
	}
	projPage, err := sdk2.ReadProjection(ctx, "site", "", 10)
	if err != nil || len(projPage.Items) == 0 {
		t.Fatalf("failed to read projection with assigned app: %v", err)
	}

	// 13. Ready on closed SDK -> ErrClosed
	if err := sdk.Ready(ctx); !errors.Is(err, kylok.ErrClosed) {
		t.Fatalf("expected ErrClosed on Ready for closed SDK, got %v", err)
	}
}








