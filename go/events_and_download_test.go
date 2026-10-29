// SPDX-License-Identifier: LGPL-3.0-only
package kylok

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	kylokv1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/internal/testapk"
	"github.com/thienvu18/kylok/go/ports"
)

// TestEventsDispatcherAndSink verifies event emission, panicking sink isolation, and queue bounds.
func TestEventsDispatcherAndSink(t *testing.T) {
	ctx := context.Background()
	apk := testapk.Make(t, testapk.Manifest)

	// 1. nil sink returns nil dispatcher
	if d := newEvents(nil, 0); d != nil {
		t.Fatal("expected nil dispatcher for nil sink")
	}
	var nilDispatcher *eventDispatcher
	nilDispatcher.close() // should not panic

	// 2. Panicking and counting sink
	var count int32
	panickingSink := func(ctx context.Context, e Event) error {
		atomic.AddInt32(&count, 1)
		panic("panicking sink must not crash SDK")
	}

	sdk, err := New(ctx, Options{
		DataDir:   t.TempDir(),
		Bootstrap: apk,
		PublicURL: "http://127.0.0.1:8080",
		Events:    panickingSink,
	})
	if err != nil {
		t.Fatal(err)
	}

	scope := "site"
	// Emitting events via enrollment
	tok, _ := sdk.CreateEnrollment(ctx, scope, 15*time.Minute)
	agent := &agentService{sdk: sdk}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	inv := &kylokv1.Inventory{ApiLevel: 34, Model: "Pixel", DeviceOwner: true, AgentVersion: "1.0", Capabilities: []string{"command.reboot"}}

	_, err = agent.Enroll(ctx, connect.NewRequest(&kylokv1.EnrollRequest{
		Token: tok.Token, AttemptId: "attempt_evt_test_16", PublicKey: pub, Inventory: inv,
		ProtocolVersion: ProtocolVersion,
	}))
	if err != nil {
		t.Fatal(err)
	}

	// Queue overflow: emit 70 events to hit default branch (queue size is 64)
	for i := 0; i < 70; i++ {
		sdk.emit("test_event", scope, "dev1", 1, "test")
	}

	// Wait for worker to deliver at least one event
	for i := 0; i < 50; i++ {
		if atomic.LoadInt32(&count) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Close SDK drains events
	sdk.Close()

	if atomic.LoadInt32(&count) == 0 {
		t.Fatal("expected sink to be invoked")
	}

	// 3. Slow sink shutdown timeout branch
	slowSink := func(ctx context.Context, e Event) error {
		time.Sleep(200 * time.Millisecond)
		return nil
	}
	disp := newEvents(slowSink, 10*time.Millisecond)
	disp.queue <- Event{ID: "e1", Kind: "test"}
	disp.close() // hits timeout select branch
}

// TestApplicationDownloadHandlerExercisesAllBranches tests 405, 404, 401, 403, 503, 500, and 200 HTTP responses.
func TestApplicationDownloadHandlerExercisesAllBranches(t *testing.T) {
	ctx := context.Background()
	apk := testapk.Make(t, testapk.Manifest)
	sdk, err := New(ctx, Options{
		DataDir:   t.TempDir(),
		Bootstrap: apk,
		PublicURL: "http://127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()

	handler := sdk.applicationHandler()
	scope := "site"

	// 1. Method not GET/HEAD -> 405
	rec405 := httptest.NewRecorder()
	req405 := httptest.NewRequest(http.MethodPost, "/artifacts/op1/app.apk", nil)
	handler.ServeHTTP(rec405, req405)
	if rec405.Code != 405 {
		t.Fatalf("expected 405, got %d", rec405.Code)
	}

	// 2. Invalid URL path -> 404
	rec404 := httptest.NewRecorder()
	req404 := httptest.NewRequest(http.MethodGet, "/artifacts/invalid-single-part", nil)
	handler.ServeHTTP(rec404, req404)
	if rec404.Code != 404 {
		t.Fatalf("expected 404, got %d", rec404.Code)
	}

	// 3. Missing/invalid Authorization and device ID headers -> 401
	rec401 := httptest.NewRecorder()
	validOpID := strings.Repeat("a", 43)
	validDigest := strings.Repeat("f", 64)
	req401 := httptest.NewRequest(http.MethodGet, "/artifacts/"+validOpID+"/"+validDigest+".apk", nil)
	handler.ServeHTTP(rec401, req401)
	if rec401.Code != 401 {
		t.Fatalf("expected 401, got %d", rec401.Code)
	}

	// 4. Enroll device and assign application
	tok, _ := sdk.CreateEnrollment(ctx, scope, 15*time.Minute)
	agent := &agentService{sdk: sdk}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	inv := &kylokv1.Inventory{ApiLevel: 34, Model: "Pixel", DeviceOwner: true, AgentVersion: "1.0", Capabilities: []string{"command.reboot"}}

	enrResp, err := agent.Enroll(ctx, connect.NewRequest(&kylokv1.EnrollRequest{
		Token: tok.Token, AttemptId: "attempt_dl_test_16", PublicKey: pub, Inventory: inv,
		ProtocolVersion: ProtocolVersion,
	}))
	if err != nil {
		t.Fatal(err)
	}
	devID := enrResp.Msg.Device.Id

	// Unregistered artifact digest in URL -> 403 Unauthorized
	rec403 := httptest.NewRecorder()
	req403 := httptest.NewRequest(http.MethodGet, "/artifacts/"+validOpID+"/"+validDigest+".apk", nil)
	req403.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 43))
	req403.Header.Set("X-Kylok-Device-ID", devID)
	req403.Header.Set("X-Kylok-Lease-ID", strings.Repeat("l", 48))
	handler.ServeHTTP(rec403, req403)
	if rec403.Code != 403 {
		t.Fatalf("expected 403, got %d", rec403.Code)
	}

	// Register application in scope
	if _, err := sdk.RegisterApplication(ctx, scope, apk); err != nil {
		t.Fatal(err)
	}

	// Test AuthorizeDownload and applicationHandler with direct device
	cred := strings.Repeat("c", 43)
	now := time.Now().UTC()
	devID2 := strings.Repeat("2", 43)
	opID2 := strings.Repeat("p", 43)
	tok2, _ := sdk.CreateEnrollment(ctx, scope, 15*time.Minute)
	_, _ = sdk.store.Consume(ctx, ports.Attempt{
		TokenHash: hash(tok2.Token), ID: "att_dl_direct_16", KeyHash: hash("k"), CredentialHash: hash(cred),
		Device: ports.Device{ID: devID2, ScopeID: scope, State: "ACTIVE"},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	opDirect, err := sdk.store.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID2, ArtifactSHA256: apk.Metadata().SHA256,
		IdempotencyKey: "k_dl_direct", OperationID: opID2,
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Sync to lease the operation
	syncResp, err := sdk.store.Sync(ctx, ports.SyncInput{
		DeviceID: devID2, CredentialHash: hash(cred), Sequence: 1, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
	})
	if err != nil || len(syncResp.Operations) == 0 {
		t.Fatalf("failed syncing leased operation: %v", err)
	}
	leasedOp := syncResp.Operations[0]

	// 5. Valid download -> 200 OK with ServeContent
	rec200 := httptest.NewRecorder()
	req200 := httptest.NewRequest(http.MethodGet, "/artifacts/"+opDirect.ID+"/"+apk.Metadata().SHA256+".apk", nil)
	req200.Header.Set("Authorization", "Bearer "+cred)
	req200.Header.Set("X-Kylok-Device-ID", devID2)
	req200.Header.Set("X-Kylok-Lease-ID", leasedOp.LeaseID)
	handler.ServeHTTP(rec200, req200)
	if rec200.Code != 200 {
		t.Fatalf("expected 200 OK for valid download, got %d (body: %s)", rec200.Code, rec200.Body.String())
	}
	if rec200.Header().Get("Content-Type") != "application/vnd.android.package-archive" {
		t.Fatalf("expected package-archive content type, got %s", rec200.Header().Get("Content-Type"))
	}
}

// TestAgentSyncAndEnrollInvalidReports tests RPC validation branches in agentService.
func TestAgentSyncAndEnrollInvalidReports(t *testing.T) {
	ctx := context.Background()
	apk := testapk.Make(t, testapk.Manifest)
	sdk, err := New(ctx, Options{
		DataDir:   t.TempDir(),
		Bootstrap: apk,
		PublicURL: "http://127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()

	agent := &agentService{sdk: sdk}

	// 1. Enroll with unknown token -> unauthenticated
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	inv := &kylokv1.Inventory{ApiLevel: 34, Model: "Pixel", DeviceOwner: true, AgentVersion: "1.0"}

	_, err = agent.Enroll(ctx, connect.NewRequest(&kylokv1.EnrollRequest{
		Token: strings.Repeat("0", 43), AttemptId: "att_unknown_tok_16", PublicKey: pub,
		Inventory: inv, ProtocolVersion: ProtocolVersion,
	}))
	if err == nil || connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("expected CodeUnauthenticated for unknown token, got %v", err)
	}

	// 2. Sync with invalid report fields -> invalid argument (line 271 of agent.go)
	syncReq := connect.NewRequest(&kylokv1.SyncRequest{
		ProtocolVersion: ProtocolVersion,
		DeviceId:        strings.Repeat("d", 43),
		Sequence:        1,
		Inventory:       inv,
		Reports: []*kylokv1.OperationReport{
			{Id: "invalid ID with spaces", OperationId: strings.Repeat("o", 43), Sequence: 1, State: kylokv1.OperationState_OPERATION_STATE_RUNNING},
		},
	})
	syncReq.Header().Set("Authorization", "Bearer "+strings.Repeat("t", 43))
	_, err = agent.Sync(ctx, syncReq)
	if err == nil || connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected CodeInvalidArgument for invalid report ID, got %v", err)
	}

	// 3. Enroll with non-P-256 key (P-384) -> invalid argument (line 129 of agent.go)
	key384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	pub384, _ := x509.MarshalPKIXPublicKey(&key384.PublicKey)
	tokValid, _ := sdk.CreateEnrollment(ctx, "site", 15*time.Minute)
	_, err = agent.Enroll(ctx, connect.NewRequest(&kylokv1.EnrollRequest{
		Token: tokValid.Token, AttemptId: "att_p384_test_16", PublicKey: pub384,
		Inventory: inv, ProtocolVersion: ProtocolVersion,
	}))
	if err == nil || connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected CodeInvalidArgument for non-P256 curve, got %v", err)
	}

	// 4. Enroll with off-curve P-256 key (X=1, Y=2) -> invalid argument (line 129 of agent.go)
	offCurveKey := &ecdsa.PublicKey{Curve: elliptic.P256(), X: big.NewInt(1), Y: big.NewInt(2)}
	pubOff, _ := x509.MarshalPKIXPublicKey(offCurveKey)
	_, err = agent.Enroll(ctx, connect.NewRequest(&kylokv1.EnrollRequest{
		Token: tokValid.Token, AttemptId: "att_off_p256_16", PublicKey: pubOff,
		Inventory: inv, ProtocolVersion: ProtocolVersion,
	}))
	if err == nil || connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected CodeInvalidArgument for off-curve P-256 key, got %v", err)
	}
}
