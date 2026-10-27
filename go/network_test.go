// SPDX-License-Identifier: LGPL-3.0-only
package kylok_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/thienvu18/kylok/go"
	v1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/ports"
	"google.golang.org/protobuf/proto"
)

type testNetworkSecrets struct {
	calls       atomic.Int64
	unavailable atomic.Bool
}

func (v *testNetworkSecrets) ReadNetworkSecret(ctx context.Context, scope, ref string) ([]byte, error) {
	v.calls.Add(1)
	if v.unavailable.Load() {
		return nil, errors.New("provider diagnostic contains private-passphrase")
	}
	if scope != "scope" || ref != "key-v1" {
		return nil, errors.New("not found")
	}
	return []byte("private-passphrase"), nil
}
func TestNetworkSecretsStayOutsideSnapshotsAndAuthenticatedReplay(t *testing.T) {
	original, options := setup(t, nil)
	original.Close()
	vault := &testNetworkSecrets{}
	options.NetworkSecrets = vault
	s, err := kylok.New(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	client, server := start(t, s)
	id, credential := enrolled(t, s, client, "scope")
	foreign, _ := enrolled(t, s, client, "foreign")
	policy := kylok.NetworkPolicy{PrimaryID: "office", Profiles: []kylok.WifiProfile{{ID: "office", SSID: "Office", Security: "WPA2_PSK", SecretRef: "key-v1"}}}
	if _, err := s.SetNetworkPolicy(ctx, "scope", foreign, "foreign", policy); !errors.Is(err, kylok.ErrNotFound) || vault.calls.Load() != 0 {
		t.Fatal("foreign identity accessed vault", err)
	}
	desired, err := s.SetNetworkPolicy(ctx, "scope", id, "network", policy)
	if err != nil || desired.Revision != 1 {
		t.Fatal(desired, err)
	}
	count := vault.calls.Load()
	vault.unavailable.Store(true)
	if receipt, err := s.SetNetworkPolicy(ctx, "scope", id, "network", policy); err != nil || receipt.Revision != 1 || vault.calls.Load() != count {
		t.Fatal("retry re-resolved immutable input", receipt, err)
	}
	if _, err := s.SetNetworkPolicy(ctx, "scope", id, "unavailable", policy); !errors.Is(err, kylok.ErrNetworkSecretUnavailable) || strings.Contains(err.Error(), "private-passphrase") {
		t.Fatal("provider error leaked credentials", err)
	}
	vault.unavailable.Store(false)
	request := syncRequest(id, credential)
	request.Msg.Sequence = 1
	request.Msg.Inventory.Capabilities = []string{"network.lifecycle"}
	response, err := client.Sync(ctx, request)
	if err != nil || response.Msg.DesiredNetworkPolicy.Policy.Profiles[0].Passphrase == nil || string(response.Msg.DesiredNetworkPolicy.Policy.Profiles[0].Passphrase) != "private-passphrase" {
		t.Fatal(response, err)
	}
	beforeUnauthorized := vault.calls.Load()
	unauthorized := syncRequest(id, strings.Repeat("x", 43))
	unauthorized.Msg.Sequence = 2
	if _, err := client.Sync(ctx, unauthorized); err == nil || vault.calls.Load() != beforeUnauthorized {
		t.Fatal("unauthenticated sync accessed vault", err)
	}
	vault.unavailable.Store(true)
	if _, err := client.Sync(ctx, request); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatal("vault failure did not fail closed", err)
	}
	vault.unavailable.Store(false)
	retry, err := client.Sync(ctx, request)
	if err != nil || !proto.Equal(response.Msg, retry.Msg) {
		t.Fatal("immutable secret replay changed response", retry, err)
	}
	badProof := syncRequest(id, credential)
	badProof.Msg.Sequence = 2
	badProof.Msg.ReportedRevision = 1
	badProof.Msg.Inventory.Capabilities = []string{"network.lifecycle"}
	badProof.Msg.NetworkReport = &v1.NetworkReport{Revision: 1, State: "APPLIED", ActiveProfileId: "office", CleanupComplete: true}
	if _, err := client.Sync(ctx, badProof); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("invalid network proof classified as retryable storage failure", err)
	}
	badProof.Msg.NetworkReport.ConnectivityVerified = true
	if _, err := client.Sync(ctx, badProof); err != nil {
		t.Fatal("rejected report committed its sequence", err)
	}
	state, err := s.GetDeviceState(ctx, "scope", id)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(state)
	if err != nil || bytes.Contains(data, []byte("private-passphrase")) || !bytes.Contains(data, []byte("key-v1")) {
		t.Fatal("host snapshot resolved secret", err)
	}
	page, err := s.ReadProjection(ctx, "scope", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(page)
	if bytes.Contains(data, []byte("private-passphrase")) {
		t.Fatal("projection leaked secret")
	}
	for _, name := range []string{"kylok.sqlite", "kylok.sqlite-wal"} {
		data, err := os.ReadFile(filepath.Join(options.DataDir, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("private-passphrase")) {
			t.Fatal("SQLite sync checkpoint persisted delivered secret", name)
		}
	}
	nonce := strings.Repeat("n", 43)
	probe := func(token string) *http.Response {
		req, _ := http.NewRequest("GET", server.URL+"/network-probe", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Kylok-Device-ID", id)
		req.Header.Set("X-Kylok-Probe", nonce)
		result, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		result.Body.Close()
		return result
	}
	if result := probe(credential); result.StatusCode != 204 || result.Header.Get("X-Kylok-Probe") != nonce || result.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(result.StatusCode)
	}
	if result := probe(strings.Repeat("x", 43)); result.StatusCode != 401 {
		t.Fatal("probe unauthenticated", result.StatusCode)
	}
	if err := s.RevokeDevice(ctx, "scope", id); err != nil {
		t.Fatal(err)
	}
	if result := probe(credential); result.StatusCode != 401 {
		t.Fatal("revoked probe", result.StatusCode)
	}
	if _, err := s.SetNetworkPolicy(ctx, "scope", id, "network", policy); !errors.Is(err, kylok.ErrRevoked) {
		t.Fatal("revoked write retry", err)
	}
}

func TestNetworkPolicyNormalizationAndCertificateValidity(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	s, _ := setup(t, func() time.Time { return now })
	client, _ := start(t, s)
	id, _ := enrolled(t, s, client, "scope")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Network test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	withoutUsage := *template
	withoutUsage.KeyUsage = 0
	noUsageDER, err := x509.CreateCertificate(rand.Reader, &withoutUsage, &withoutUsage, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ports.NormalizeNetworkPolicy(kylok.NetworkPolicy{Certificates: []kylok.CACertificate{{ID: "root", DER: noUsageDER}}}); err != nil {
		t.Fatal("valid CA without optional Key Usage rejected", err)
	}
	profile := kylok.WifiProfile{ID: "office", SSID: "Office", Security: "OPEN"}
	valid := kylok.NetworkPolicy{PrimaryID: "office", Profiles: []kylok.WifiProfile{profile}, Certificates: []kylok.CACertificate{{ID: "root", DER: der}}}
	desired, err := s.SetNetworkPolicy(ctx, "scope", id, "ca", valid)
	if err != nil {
		t.Fatal(err)
	}
	der[0] ^= 1
	if desired.Policy.Certificates[0].DER[0] == der[0] {
		t.Fatal("certificate aliases caller memory")
	}
	der[0] ^= 1
	for _, policy := range []kylok.NetworkPolicy{
		{PrimaryID: "missing", Profiles: []kylok.WifiProfile{profile}},
		{PrimaryID: "office", Profiles: []kylok.WifiProfile{profile, profile}},
		{Profiles: []kylok.WifiProfile{{ID: "bad", SSID: string([]byte{255}), Security: "OPEN"}}},
		{PrimaryID: "office", Profiles: []kylok.WifiProfile{{ID: "office", SSID: "Office", Security: "OPEN", SecretRef: "not-allowed"}}},
		{Certificates: []kylok.CACertificate{{ID: "root", DER: []byte("not a CA")}}},
	} {
		if _, err := ports.NormalizeNetworkPolicy(policy); !errors.Is(err, kylok.ErrInvalidArgument) {
			t.Fatal(policy, err)
		}
	}
	protected := valid
	protected.Profiles = []kylok.WifiProfile{{ID: "office", SSID: "Office", Security: "WPA2_PSK", SecretRef: "key-v1"}}
	if _, err := s.SetNetworkPolicy(ctx, "scope", id, "vault-required", protected); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	if old, err := s.SetNetworkPolicy(ctx, "scope", id, "ca", valid); err != nil || old.Revision != desired.Revision {
		t.Fatal("expired receipt unavailable", old, err)
	}
	if _, err := s.SetNetworkPolicy(ctx, "scope", id, "expired", valid); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expired new CA accepted", err)
	}
	if released, err := s.SetNetworkPolicy(ctx, "scope", id, "release", kylok.NetworkPolicy{}); err != nil || released.Revision != 2 {
		t.Fatal(released, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.GetNetworkPolicy(cancelled, "scope", id); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
