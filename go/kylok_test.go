// SPDX-License-Identifier: LGPL-3.0-only
package kylok_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/thienvu18/kylok/go"
	"github.com/thienvu18/kylok/go/adapters/filesystem"
	"github.com/thienvu18/kylok/go/adapters/sqlite"
	v1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/gen/kylok/v1/kylokv1connect"
	"github.com/thienvu18/kylok/go/internal/testapk"
	"github.com/thienvu18/kylok/go/ports"
	"github.com/thienvu18/kylok/go/provisioning"
	"google.golang.org/protobuf/proto"
)

var ctx = context.Background()

func inventory() *v1.Inventory {
	return &v1.Inventory{ApiLevel: 35, Model: "unit model", BuildFingerprint: "unit/build", AgentVersion: "unit/1", DeviceOwner: true}
}
func start(t *testing.T, sdk *kylok.SDK) (kylokv1connect.AgentServiceClient, *httptest.Server) {
	t.Helper()
	server := httptest.NewUnstartedServer(sdk.Handler())
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	return kylokv1connect.NewAgentServiceClient(server.Client(), server.URL, connect.WithGRPC()), server
}
func setup(t *testing.T, clock func() time.Time) (*kylok.SDK, kylok.Options) {
	t.Helper()
	o := kylok.Options{PublicURL: "https://kylok.test", Bootstrap: testapk.Make(t, testapk.Manifest), DataDir: t.TempDir(), Clock: clock}
	s, err := kylok.New(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, o
}
func issue(t *testing.T, s *kylok.SDK, scope string) kylok.Enrollment {
	t.Helper()
	e, err := s.CreateEnrollment(ctx, scope, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func request(t *testing.T, token string) (*v1.EnrollRequest, *ecdh.PrivateKey) {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(key.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	return &v1.EnrollRequest{Token: token, AttemptId: "unit-attempt-0123456789", PublicKey: der, Inventory: inventory(), ProtocolVersion: 1}, key
}
func decrypt(t *testing.T, key *ecdh.PrivateKey, r *v1.EnrollResponse, attempt string) string {
	t.Helper()
	public, err := x509.ParsePKIXPublicKey(r.Credential.EphemeralPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(public)
	parsed, _ := x509.ParsePKIXPublicKey(der)
	ec := parsed.(interface {
		ECDH() (*ecdh.PublicKey, error)
	})
	recipient, err := ec.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	shared, err := key.ECDH(recipient)
	if err != nil {
		t.Fatal(err)
	}
	derived, err := hkdf.Key(sha256.New, shared, nil, "kylok/enrollment/v1", 32)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := aes.NewCipher(derived)
	gcm, _ := cipher.NewGCM(block)
	plaintext, err := gcm.Open(nil, r.Credential.Nonce, r.Credential.Ciphertext, []byte("kylok/v1\n"+r.Device.Id+"\n"+r.Device.ScopeId+"\n"+attempt))
	if err != nil {
		t.Fatal(err)
	}
	return string(plaintext)
}
func syncRequest(id, credential string) *connect.Request[v1.SyncRequest] {
	r := connect.NewRequest(&v1.SyncRequest{DeviceId: id, Inventory: inventory(), ProtocolVersion: 1, Sequence: 1})
	r.Header().Set("Authorization", "Bearer "+credential)
	return r
}
func reason(t *testing.T, err error, want v1.ErrorReason) {
	t.Helper()
	var ce *connect.Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected RPC error, got %v", err)
	}
	for _, d := range ce.Details() {
		v, e := d.Value()
		if e == nil {
			if detail, ok := v.(*v1.ErrorDetail); ok && detail.Reason == want {
				return
			}
		}
	}
	t.Fatalf("expected reason %s, got %v", want, err)
}

func TestEnrollmentRecoveryRestartAndRevocation(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	s, o := setup(t, func() time.Time { return now })
	client, server := start(t, s)
	e := issue(t, s, "site-a")
	req, key := request(t, e.Token)
	first, err := client.Enroll(ctx, connect.NewRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	credential := decrypt(t, key, first.Msg, req.AttemptId)
	if len(credential) != 43 {
		t.Fatal("credential entropy")
	}
	if _, err = client.Sync(ctx, syncRequest(first.Msg.Device.Id, credential)); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListDevices(ctx, "site-a", "", 100)
	if err != nil || len(list) != 1 || list[0].State != "MANAGED" {
		t.Fatalf("list: %+v %v", list, err)
	}
	other, err := s.ListDevices(ctx, "site-b", "", 100)
	if err != nil || len(other) != 0 {
		t.Fatal("scope leak", other, err)
	}
	if err = s.RevokeDevice(ctx, "site-b", first.Msg.Device.Id); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("cross-scope revoke", err)
	}
	server.Close()
	s.Close()
	now = now.Add(2 * time.Minute) // expired token still permits its bound encrypted recovery.
	s2, err := kylok.New(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	client, _ = start(t, s2)
	recovered, err := client.Enroll(ctx, connect.NewRequest(req))
	if err != nil || !proto.Equal(first.Msg, recovered.Msg) {
		t.Fatal("lost-response recovery", err)
	}
	wrong, _ := request(t, e.Token)
	_, err = client.Enroll(ctx, connect.NewRequest(wrong))
	reason(t, err, v1.ErrorReason_ERROR_REASON_TOKEN_CONSUMED)
	changed := proto.Clone(req).(*v1.EnrollRequest)
	changed.AttemptId = "different-attempt-0123456789"
	_, err = client.Enroll(ctx, connect.NewRequest(changed))
	reason(t, err, v1.ErrorReason_ERROR_REASON_TOKEN_CONSUMED)
	_, err = client.Sync(ctx, syncRequest(first.Msg.Device.Id, strings.Repeat("x", 43)))
	reason(t, err, v1.ErrorReason_ERROR_REASON_UNAUTHENTICATED)
	if err = s2.RevokeDevice(ctx, "site-a", first.Msg.Device.Id); err != nil {
		t.Fatal(err)
	}
	_, err = client.Sync(ctx, syncRequest(first.Msg.Device.Id, credential))
	reason(t, err, v1.ErrorReason_ERROR_REASON_DEVICE_REVOKED)
	_, err = client.Enroll(ctx, connect.NewRequest(req))
	reason(t, err, v1.ErrorReason_ERROR_REASON_DEVICE_REVOKED)
	// No plaintext credential or token is persisted in any database/WAL file.
	files, _ := filepath.Glob(filepath.Join(o.DataDir, "kylok.sqlite*"))
	for _, file := range files {
		b, _ := os.ReadFile(file)
		if bytes.Contains(b, []byte(credential)) || bytes.Contains(b, []byte(e.Token)) {
			t.Fatal("plaintext secret in", file)
		}
	}
}

func TestConcurrentTokenConsumption(t *testing.T) {
	s, o := setup(t, nil)
	// Separate connections model a host plus CLI using the same single-node database.
	second, err := kylok.New(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	c1, _ := start(t, s)
	c2, _ := start(t, second)
	req1, _ := request(t, issue(t, s, "race").Token)
	req2, _ := request(t, req1.Token)
	clients := []kylokv1connect.AgentServiceClient{c1, c2}
	requests := []*v1.EnrollRequest{req1, req2}
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := map[string]bool{}
	successes := 0
	for n := 0; n < 24; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r, err := clients[n%2].Enroll(ctx, connect.NewRequest(requests[n%2]))
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				ids[r.Msg.Device.Id] = true
				successes++
			} else if connect.CodeOf(err) != connect.CodeAlreadyExists {
				t.Error(err)
			}
		}(n)
	}
	wg.Wait()
	list, err := s.ListDevices(ctx, "race", "", 100)
	if err != nil || len(list) != 1 || len(ids) != 1 || successes != 12 {
		t.Fatalf("not atomic: rows=%d ids=%v success=%d err=%v", len(list), ids, successes, err)
	}
}

func TestExpiryAndInvalidRequests(t *testing.T) {
	now := time.Now().UTC()
	s, _ := setup(t, func() time.Time { return now })
	client, _ := start(t, s)
	req, _ := request(t, issue(t, s, "expiry").Token)
	now = now.Add(time.Minute)
	_, err := client.Enroll(ctx, connect.NewRequest(req))
	reason(t, err, v1.ErrorReason_ERROR_REASON_TOKEN_EXPIRED)
	if _, err = s.QR(ctx, req.Token); !errors.Is(err, ports.ErrExpired) {
		t.Fatal(err)
	}
	active, _ := request(t, issue(t, s, "expiry").Token)
	first, err := client.Enroll(ctx, connect.NewRequest(active))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(24 * time.Hour)
	_, err = client.Enroll(ctx, connect.NewRequest(active))
	reason(t, err, v1.ErrorReason_ERROR_REASON_RECOVERY_EXPIRED)
	if first.Msg.Device.ScopeId != "expiry" {
		t.Fatal("scope")
	}
	for _, tt := range []struct {
		name   string
		mutate func(*v1.EnrollRequest)
		reason v1.ErrorReason
	}{
		{"protocol", func(r *v1.EnrollRequest) { r.ProtocolVersion = 2 }, v1.ErrorReason_ERROR_REASON_PROTOCOL_UNSUPPORTED},
		{"not owner", func(r *v1.EnrollRequest) { r.Inventory.DeviceOwner = false }, v1.ErrorReason_ERROR_REASON_INVALID_INPUT},
		{"old API", func(r *v1.EnrollRequest) { r.Inventory.ApiLevel = 32 }, v1.ErrorReason_ERROR_REASON_INVALID_INPUT},
		{"short attempt", func(r *v1.EnrollRequest) { r.AttemptId = "a" }, v1.ErrorReason_ERROR_REASON_INVALID_INPUT},
		{"key", func(r *v1.EnrollRequest) { r.PublicKey = []byte("bad") }, v1.ErrorReason_ERROR_REASON_INVALID_INPUT},
		{"unknown token", func(r *v1.EnrollRequest) { r.Token = strings.Repeat("x", 43) }, v1.ErrorReason_ERROR_REASON_TOKEN_INVALID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := proto.Clone(req).(*v1.EnrollRequest)
			tt.mutate(r)
			_, err := client.Enroll(ctx, connect.NewRequest(r))
			reason(t, err, tt.reason)
		})
	}
}

func TestBootstrapQRAndTLSBoundary(t *testing.T) {
	s, o := setup(t, nil)
	_, server := start(t, s)
	metadata := o.Bootstrap.Metadata()
	response, err := server.Client().Get(server.URL + o.Bootstrap.Path())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !bytes.Equal(body, o.Bootstrap.Bytes()) || !strings.Contains(response.Header.Get("Cache-Control"), "immutable") {
		t.Fatal("bootstrap content")
	}
	qr, err := s.QR(ctx, issue(t, s, "qr").Token)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(qr, &payload); err != nil {
		t.Fatal(err)
	}
	checksum := payload["android.app.extra.PROVISIONING_DEVICE_ADMIN_PACKAGE_CHECKSUM"].(string)
	digest, _ := base64.RawURLEncoding.DecodeString(checksum)
	sum := sha256.Sum256(body)
	if !bytes.Equal(digest, sum[:]) || payload["android.app.extra.PROVISIONING_ALLOW_OFFLINE"] != true || metadata.Receiver != "io.kylok.unit.Admin" {
		t.Fatal("QR metadata mismatch")
	}
	for _, path := range []string{"/management/devices", "/bootstrap/other.apk", "/qr", "/"} {
		r, e := server.Client().Get(server.URL + path)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != 404 {
			t.Fatal(path, r.StatusCode)
		}
	}
	clear := httptest.NewServer(s.Handler())
	defer clear.Close()
	r, err := http.Get(clear.URL + o.Bootstrap.Path())
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 426 {
		t.Fatal("cleartext accepted")
	}
}

func TestConstructorValidationAndCustomProviders(t *testing.T) {
	apk := testapk.Make(t, testapk.Manifest)
	for _, o := range []kylok.Options{
		{PublicURL: "https://host", Bootstrap: &provisioning.APK{}, DataDir: t.TempDir()},
		{}, {PublicURL: "ftp://host", Bootstrap: apk, DataDir: t.TempDir()},
		{PublicURL: "https://host/path", Bootstrap: apk, DataDir: t.TempDir()},
		{PublicURL: "https://host", Bootstrap: apk},
		{PublicURL: "https://host", DataDir: t.TempDir()},
	} {
		if s, err := kylok.New(ctx, o); err == nil {
			s.Close()
			t.Fatal("accepted invalid options", o.PublicURL)
		}
	}
	debug := testapk.Make(t, strings.Replace(testapk.Manifest, "<application ", `<application android:debuggable="true" `, 1))
	if _, err := kylok.New(ctx, kylok.Options{PublicURL: "https://host", Bootstrap: debug, DataDir: t.TempDir()}); err == nil {
		t.Fatal("debug enabled by default")
	}
	dir := t.TempDir()
	store, err := sqlite.Open(ctx, filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	artifacts, err := filesystem.Open(filepath.Join(dir, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	sdk, err := kylok.New(ctx, kylok.Options{PublicURL: "https://host", Bootstrap: apk, Store: store, Artifacts: artifacts})
	if err != nil {
		t.Fatal(err)
	}
	sdk.Close()
	sdk.Close()
	// Closing the SDK must not close a supplied store.
	if _, err := store.ListDevices(ctx, "scope", "", 1); err != nil {
		t.Fatal("custom store ownership", err)
	}
	corrupt := filepath.Join(t.TempDir(), "corrupt.db")
	os.WriteFile(corrupt, []byte("not sqlite"), 0600)
	if s, err := sqlite.Open(ctx, corrupt); err == nil {
		s.Close()
		t.Fatal("corrupt DB accepted")
	}
}

func TestSeparateBootstrapAndAgentOrigins(t *testing.T) {
	_, o := setup(t, nil)
	o.PublicURL = "https://mdm.lan:8443"
	o.BootstrapURL = "http://192.168.101.5:8080"
	s, err := kylok.New(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	qr, err := s.QR(ctx, issue(t, s, "lan").Token)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(qr, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["android.app.extra.PROVISIONING_DEVICE_ADMIN_PACKAGE_DOWNLOAD_LOCATION"] != o.BootstrapURL+o.Bootstrap.Path() {
		t.Fatal("not using explicit bootstrap origin")
	}
	extras := payload["android.app.extra.PROVISIONING_ADMIN_EXTRAS_BUNDLE"].(map[string]any)
	if extras["kylok.server"] != o.PublicURL {
		t.Fatal("agent TLS downgraded")
	}
	checksum := payload["android.app.extra.PROVISIONING_DEVICE_ADMIN_PACKAGE_CHECKSUM"].(string)
	decoded, err := base64.RawURLEncoding.DecodeString(checksum)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(o.Bootstrap.Bytes())
	if !bytes.Equal(sum[:], decoded) {
		t.Fatal("checksum changed")
	}
	plain := httptest.NewServer(s.BootstrapHandler())
	defer plain.Close()
	r, err := http.Get(plain.URL + o.Bootstrap.Path())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 200 || !bytes.Equal(body, o.Bootstrap.Bytes()) {
		t.Fatal("HTTP bootstrap content")
	}
	for _, path := range []string{"/kylok.v1.AgentService/Enroll", "/kylok.v1.AgentService/Sync", "/management/devices", "/qr", "/"} {
		r, err := http.Get(plain.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 404 {
			t.Fatal("HTTP exposed non-bootstrap route", path, r.StatusCode)
		}
	}
	agentHTTP := httptest.NewServer(s.Handler())
	defer agentHTTP.Close()
	r, err = http.Get(agentHTTP.URL + o.Bootstrap.Path())
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 426 {
		t.Fatal("agent listener accepted cleartext")
	}
	client, _ := start(t, s)
	req, key := request(t, issue(t, s, "lan").Token)
	response, err := client.Enroll(ctx, connect.NewRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	credential := decrypt(t, key, response.Msg, req.AttemptId)
	if _, err := client.Sync(ctx, syncRequest(response.Msg.Device.Id, credential)); err != nil {
		t.Fatal(err)
	}
	production, _ := setup(t, nil)
	disabled := httptest.NewServer(production.BootstrapHandler())
	defer disabled.Close()
	r, err = http.Get(disabled.URL + o.Bootstrap.Path())
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 426 {
		t.Fatal("HTTPS bootstrap accepted cleartext")
	}
}

func TestPlainHTTP2Enrollment(t *testing.T) {
	_, o := setup(t, nil)
	o.PublicURL = "http://192.168.101.5:8080"
	s, err := kylok.New(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	server := httptest.NewUnstartedServer(s.Handler())
	server.Config.Protocols = protocols
	server.Start()
	defer server.Close()
	transport := &http.Transport{Protocols: protocols}
	defer transport.CloseIdleConnections()
	client := kylokv1connect.NewAgentServiceClient(&http.Client{Transport: transport}, server.URL, connect.WithGRPC())
	req, key := request(t, issue(t, s, "http-site").Token)
	response, err := client.Enroll(ctx, connect.NewRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	credential := decrypt(t, key, response.Msg, req.AttemptId)
	if _, err := client.Sync(ctx, syncRequest(response.Msg.Device.Id, credential)); err != nil {
		t.Fatal(err)
	}
	r, err := http.Get(server.URL + o.Bootstrap.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal("HTTP APK download", r.StatusCode)
	}
}
