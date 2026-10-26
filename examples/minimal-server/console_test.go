// SPDX-License-Identifier: LGPL-3.0-only
package main

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
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/thienvu18/kylok/go"
	v1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/gen/kylok/v1/kylokv1connect"
	"github.com/thienvu18/kylok/go/provisioning"
)

func TestConsoleTokenIsPrivatePersistentAndNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	first, err := consoleToken(path)
	if err != nil || len(first) != 43 {
		t.Fatal(first, err)
	}
	again, err := consoleToken(path)
	if err != nil || again != first {
		t.Fatal("token changed", err)
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = consoleToken(path); err == nil {
		t.Fatal("accepted public credential file")
	}
}

func TestConsoleRejectsAnonymousAndCrossOriginActions(t *testing.T) {
	token := strings.Repeat("x", 43)
	c := &console{tokenHash: sha256.Sum256([]byte(token))}
	for _, tc := range []struct {
		auth, origin string
		want         int
	}{{"", "", 401}, {"Bearer wrong", "", 401}, {"Bearer " + token, "http://attacker.invalid", 403}} {
		r := httptest.NewRequest("POST", "http://localhost:8081/api/revoke", strings.NewReader(`{}`))
		r.Header.Set("Authorization", tc.auth)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		c.handler().ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal(w.Code, tc.want)
		}
	}
}

func TestInterfaceCandidatesHonorSpecificBinding(t *testing.T) {
	choices := origins("http://127.0.0.1:8080", "127.0.0.1:8080")
	for _, choice := range choices {
		if !strings.Contains(choice.Origin, "127.0.0.1:8080") {
			t.Fatal("unreachable interface advertised", choice)
		}
	}
}

func TestConsoleEnrollmentCatalogPolicyAndRelativeDownload(t *testing.T) {
	path := os.Getenv("KYLOK_CONSOLE_APK")
	if path == "" {
		t.Skip("real signed APK fixture is supplied by make test-console")
	}
	ctx := context.Background()
	apk, err := provisioning.Inspect(ctx, path, provisioning.Tools{})
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	sdk, err := kylok.New(ctx, kylok.Options{PublicURL: "http://configured.invalid:8080", Bootstrap: apk, DataDir: data, AllowDebugAPK: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()
	agent := httptest.NewServer(sdk.Handler())
	defer agent.Close()
	adminToken := strings.Repeat("a", 43)
	c := &console{sdk: sdk, apk: apk, apiOrigin: agent.URL, bootstrapOrigin: agent.URL, agentListen: "0.0.0.0:8080", dataDir: data, tokenHash: sha256.Sum256([]byte(adminToken))}
	ui := httptest.NewServer(c.handler())
	defer ui.Close()
	call := func(path string, body any) map[string]json.RawMessage {
		t.Helper()
		var source io.Reader
		method := "GET"
		if body != nil {
			encoded, _ := json.Marshal(body)
			source = bytes.NewReader(encoded)
			method = "POST"
		}
		r, _ := http.NewRequest(method, ui.URL+path, source)
		r.Header.Set("Authorization", "Bearer "+adminToken)
		r.Header.Set("Origin", ui.URL)
		response, e := ui.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		payload, _ := io.ReadAll(response.Body)
		if response.StatusCode != 200 {
			t.Fatalf("%s: %d %s", path, response.StatusCode, payload)
		}
		var result map[string]json.RawMessage
		if e = json.Unmarshal(payload, &result); e != nil {
			t.Fatal(e)
		}
		return result
	}
	created := call("/api/enroll", map[string]any{"scope": "demo", "origin": agent.URL, "bootstrap": agent.URL, "ttl_seconds": 900})
	var token string
	json.Unmarshal(created["token"], &token)
	if !bytes.Contains(created["image"], []byte("data:image/png;base64,")) {
		t.Fatal("missing QR PNG")
	}
	rewritten := call("/api/qr", map[string]any{"token": token, "origin": "http://mdm.lan:8080", "bootstrap": "http://apk.lan:8080"})
	if !bytes.Contains(rewritten["payload"], []byte("http://mdm.lan:8080")) || !bytes.Contains(rewritten["payload"], []byte("http://apk.lan:8080")) {
		t.Fatal("custom origins missing", string(rewritten["payload"]))
	}
	var secondToken string
	json.Unmarshal(rewritten["token"], &secondToken)
	if token != secondToken {
		t.Fatal("QR switch rotated token")
	}
	keys, _ := ecdh.P256().GenerateKey(rand.Reader)
	public, _ := x509.MarshalPKIXPublicKey(keys.PublicKey())
	client := kylokv1connect.NewAgentServiceClient(agent.Client(), agent.URL)
	inventory := &v1.Inventory{ApiLevel: 35, Model: "console fixture", AgentVersion: kylok.Version(), DeviceOwner: true, Capabilities: []string{"install_apk", "durable_sync"}}
	enrolled, err := client.Enroll(ctx, connect.NewRequest(&v1.EnrollRequest{Token: token, AttemptId: "console-fixture-attempt", PublicKey: public, ProtocolVersion: 1, Inventory: inventory}))
	if err != nil {
		t.Fatal(err)
	}
	id := enrolled.Msg.Device.Id
	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	writer.WriteField("scope", "demo")
	file, _ := writer.CreateFormFile("apk", "host.apk")
	file.Write(apk.Bytes())
	writer.Close()
	r, _ := http.NewRequest("POST", ui.URL+"/api/register", &upload)
	r.Header.Set("Authorization", "Bearer "+adminToken)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := ui.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var artifact kylok.Artifact
	if response.StatusCode != 200 {
		b, _ := io.ReadAll(response.Body)
		t.Fatal(response.StatusCode, string(b))
	}
	json.NewDecoder(response.Body).Decode(&artifact)
	call("/api/assign", map[string]any{"scope": "demo", "device": id, "digest": artifact.SHA256, "request_key": "console-install", "timeout_seconds": 3600})
	call("/api/set-policy", map[string]any{"scope": "demo", "device": id, "request_key": "console-policy", "policy": map[string]any{"camera_disabled": true}})
	policy := call("/api/policy?scope=demo&device="+id, nil)
	if !bytes.Contains(policy["desired"], []byte("camera_disabled")) {
		t.Fatal("policy not stored")
	}
	// Recover the actual credential as a real agent would; the console never receives it.
	peer, err := x509.ParsePKIXPublicKey(enrolled.Msg.Credential.EphemeralPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := peer.(interface {
		ECDH() (*ecdh.PublicKey, error)
	}).ECDH()
	shared, _ := keys.ECDH(key)
	derived, _ := hkdf.Key(sha256.New, shared, nil, "kylok/enrollment/v1", 32)
	block, _ := aes.NewCipher(derived)
	gcm, _ := cipher.NewGCM(block)
	credential, err := gcm.Open(nil, enrolled.Msg.Credential.Nonce, enrolled.Msg.Credential.Ciphertext, []byte("kylok/v1\n"+id+"\ndemo\nconsole-fixture-attempt"))
	if err != nil {
		t.Fatal(err)
	}
	syncRequest := connect.NewRequest(&v1.SyncRequest{DeviceId: id, Inventory: inventory, ProtocolVersion: 1, Sequence: 1})
	syncRequest.Header().Set("Authorization", "Bearer "+string(credential))
	snapshot, err := client.Sync(ctx, syncRequest)
	if err != nil {
		t.Fatal(err)
	}
	op := snapshot.Msg.Operations[0]
	if !strings.HasPrefix(op.DownloadUrl, "/artifacts/") {
		t.Fatal("download bound to old origin", op.DownloadUrl)
	}
	download, _ := http.NewRequest("GET", agent.URL+op.DownloadUrl, nil)
	download.Header.Set("Authorization", "Bearer "+string(credential))
	download.Header.Set("X-Kylok-Device-ID", id)
	download.Header.Set("X-Kylok-Lease-ID", op.LeaseId)
	download.Header.Set("Range", "bytes=0-15")
	got, err := agent.Client().Do(download)
	if err != nil {
		t.Fatal(err)
	}
	got.Body.Close()
	if got.StatusCode != 206 {
		t.Fatal("selected-origin download failed", got.StatusCode)
	}
	call("/api/revoke", map[string]any{"scope": "demo", "device": id})
	_, err = client.Sync(ctx, syncRequest)
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("revocation failed", err)
	}
	if files, _ := filepath.Glob(filepath.Join(data, "console-upload-*")); len(files) > 0 {
		t.Fatal("staged upload leaked", files)
	}
	if _, err = sdk.CreateEnrollment(ctx, "demo", time.Minute); err != nil {
		t.Fatal(err)
	}
}
