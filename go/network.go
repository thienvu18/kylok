// SPDX-License-Identifier: LGPL-3.0-only
package kylok

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	v1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/ports"
)

// ErrNetworkSecretUnavailable hides provider diagnostics so credential values cannot leak through SDK errors.
var ErrNetworkSecretUnavailable = errors.New("network secret unavailable")

// NetworkPolicy owns managed saved Wi-Fi profiles and private CAs; an empty document releases managed resources safely.
type NetworkPolicy = ports.NetworkPolicy

// WifiProfile uses a scoped immutable SecretRef; no management read contains a delivered password.
type WifiProfile = ports.WifiProfile

// CACertificate carries a CA's DER bytes, without its private key.
type CACertificate = ports.CACertificate

// DesiredNetworkPolicy binds normalized references and CA ownership to a desired revision.
type DesiredNetworkPolicy = ports.DesiredNetworkPolicy

// NetworkReport separates authenticated route verification from association/setter results.
type NetworkReport = ports.NetworkReport

// NetworkStatus keeps desired and reported state distinct and excludes resolved network secrets.
type NetworkStatus = ports.NetworkStatus

// NetworkSecretProvider is a host-owned immutable scoped vault contract; rotate using a new reference version.
type NetworkSecretProvider = ports.NetworkSecretProvider

// SetNetworkPolicy replaces bounded managed networks/trust. Hosts supply immutable passphrase versions through NetworkSecrets.
// Android requires permissions, an independently verified saved recovery network and ownership readback before mutation.
// A failed migration/release retains the working route; a new request key creates the host's retry revision.
// Usage: desired, err := sdk.SetNetworkPolicy(ctx, scope, deviceID, "network-v2", kylok.NetworkPolicy{PrimaryID:"office", Profiles:[]kylok.WifiProfile{{ID:"office",SSID:"Office",Security:"WPA2_PSK",SecretRef:"office-key-v2"}}}).
func (s *SDK) SetNetworkPolicy(ctx context.Context, scope, device, key string, input NetworkPolicy) (DesiredNetworkPolicy, error) {
	if err := s.checkOpen(ctx); err != nil {
		return DesiredNetworkPolicy{}, err
	}
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(device) || !idPattern.MatchString(key) {
		return DesiredNetworkPolicy{}, ErrInvalidArgument
	}
	policy, err := ports.NormalizeNetworkPolicy(input)
	if err != nil {
		return DesiredNetworkPolicy{}, err
	}
	target, err := s.store.GetDevice(ctx, scope, device)
	if err != nil {
		return DesiredNetworkPolicy{}, err
	}
	if target.State == "REVOKED" {
		return DesiredNetworkPolicy{}, ErrRevoked
	}
	previous, err := s.store.GetNetworkRequest(ctx, scope, device, key)
	if err == nil {
		before, _ := json.Marshal(previous.Policy)
		after, _ := json.Marshal(policy)
		if !bytes.Equal(before, after) {
			return DesiredNetworkPolicy{}, ErrConflict
		}
		return previous, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return DesiredNetworkPolicy{}, err
	}
	// Reject obvious expired/future CA submissions before they can affect installed trust.
	for _, certificate := range policy.Certificates {
		cert, err := x509.ParseCertificate(certificate.DER)
		if err != nil || s.clock().Before(cert.NotBefore) || !s.clock().Before(cert.NotAfter) {
			return DesiredNetworkPolicy{}, invalidArgument("CA must be valid at submission")
		}
	}
	for _, profile := range policy.Profiles {
		if profile.Security != "OPEN" {
			if _, err = s.networkSecret(ctx, scope, profile); err != nil {
				return DesiredNetworkPolicy{}, err
			}
		}
	}
	return s.store.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: scope, DeviceID: device, IdempotencyKey: key, Policy: policy})
}

// GetNetworkPolicy returns reference-only desired/report/compliance state in the authorized scope.
// Usage: status, err := sdk.GetNetworkPolicy(ctx, scope, deviceID).
func (s *SDK) GetNetworkPolicy(ctx context.Context, scope, device string) (NetworkStatus, error) {
	if err := s.checkOpen(ctx); err != nil {
		return NetworkStatus{}, err
	}
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(device) {
		return NetworkStatus{}, ErrInvalidArgument
	}
	return s.store.GetNetworkPolicy(ctx, scope, device)
}

// Resolve immutable scoped credentials; malformed or unavailable secrets fail without a fallback or value-bearing error.
func (s *SDK) networkSecret(ctx context.Context, scope string, profile WifiProfile) ([]byte, error) {
	if profile.Security == "OPEN" {
		return nil, nil
	}
	if s.networkSecrets == nil {
		return nil, invalidArgument("NetworkSecrets provider required for protected Wi-Fi")
	}
	value, err := s.networkSecrets.ReadNetworkSecret(ctx, scope, profile.SecretRef)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrNetworkSecretUnavailable
	}
	minimum := 1
	if profile.Security == "WPA2_PSK" {
		minimum = 8
	}
	_, hexError := hex.DecodeString(string(value))
	hexPSK := profile.Security == "WPA2_PSK" && len(value) == 64 && hexError == nil
	valid := len(value) >= minimum && len(value) <= 63 && utf8.Valid(value)
	for _, b := range value {
		if b < 32 || b == 127 {
			valid = false
		}
		if profile.Security == "WPA2_PSK" && b > 126 {
			valid = false
		}
	}
	if !valid && !hexPSK {
		return nil, invalidArgument("invalid Wi-Fi secret format")
	}
	return value, nil
}

// Resolve passwords only after authenticated store.Sync; its cached snapshot stores references, never credentials.
func (s *SDK) networkMessage(ctx context.Context, scope string, desired *DesiredNetworkPolicy) (*v1.DesiredNetworkPolicy, error) {
	if desired == nil {
		return nil, nil
	}
	policy := &v1.NetworkPolicy{PrimaryId: desired.Policy.PrimaryID}
	for _, profile := range desired.Policy.Profiles {
		passphrase, err := s.networkSecret(ctx, scope, profile)
		if err != nil {
			return nil, err
		}
		policy.Profiles = append(policy.Profiles, &v1.WifiProfile{Id: profile.ID, Ssid: profile.SSID, Security: profile.Security, Passphrase: passphrase, Hidden: profile.Hidden})
	}
	for _, certificate := range desired.Policy.Certificates {
		policy.Certificates = append(policy.Certificates, &v1.CACertificate{Id: certificate.ID, Der: certificate.DER})
	}
	return &v1.DesiredNetworkPolicy{Revision: desired.Revision, Policy: policy}, nil
}

// Authenticate a nonce-bound connectivity check without changing sync sequence or exposing other management routes.
func (s *SDK) networkProbe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	credential, ok := bearer(r.Header.Get("Authorization"))
	id, nonce := r.Header.Get("X-Kylok-Device-ID"), r.Header.Get("X-Kylok-Probe")
	if !ok || !devicePattern.MatchString(id) || len(nonce) != 43 || !devicePattern.MatchString(nonce) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if err := s.checkOpen(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if _, err := s.store.Authenticate(r.Context(), id, hash(credential)); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.Header().Set("X-Kylok-Probe", strings.Clone(nonce))
	w.WriteHeader(http.StatusNoContent)
}
