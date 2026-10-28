// SPDX-License-Identifier: LGPL-3.0-only
package ports

import (
	"context"
	"crypto/x509"
	"encoding/asn1"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// WifiProfile describes a managed saved network. Secrets are immutable host references, never passwords.
// Security is OPEN, WPA2_PSK or WPA3_SAE. SSID is literal UTF-8, limited to 32 bytes.
type WifiProfile struct {
	ID        string `json:"id"`
	SSID      string `json:"ssid"`
	Security  string `json:"security"`
	SecretRef string `json:"secret_ref,omitempty"`
	Hidden    bool   `json:"hidden"`
}

// CACertificate is one DER-encoded CA, without a private key. ID is an opaque ownership label.
type CACertificate struct {
	ID  string `json:"id"`
	DER []byte `json:"der"`
}

// NetworkPolicy is a full replacement of at most eight Wi-Fi profiles and eight private CAs.
// PrimaryID chooses the network requiring authenticated management connectivity. An empty document releases ownership.
// The Android agent preserves an independent saved recovery route before applying or releasing managed settings.
type NetworkPolicy struct {
	PrimaryID    string          `json:"primary_id"`
	Profiles     []WifiProfile   `json:"profiles"`
	Certificates []CACertificate `json:"certificates"`
}

// DesiredNetworkPolicy binds the normalized document to a shared device desired revision.
type DesiredNetworkPolicy struct {
	Revision uint64        `json:"revision"`
	Policy   NetworkPolicy `json:"policy"`
}

// NetworkReport distinguishes readback and authenticated route verification from setters and Wi-Fi association.
// APPLIED requires primary/recovery connectivity and completion of only the managed-resource cleanup.
type NetworkReport struct {
	Revision             uint64 `json:"revision"`
	State                string `json:"state"`
	Reason               string `json:"reason"`
	ActiveProfileID      string `json:"active_profile_id"`
	ConnectivityVerified bool   `json:"connectivity_verified"`
	CleanupComplete      bool   `json:"cleanup_complete"`
}

// NetworkStatus contains no delivered passwords. Reported state remains separate from current desired compliance.
type NetworkStatus struct {
	Desired  *DesiredNetworkPolicy `json:"desired,omitempty"`
	Reported *NetworkReport        `json:"reported,omitempty"`
	State    string                `json:"state"`
	Reason   string                `json:"reason"`
}

// NetworkAssignment atomically binds a complete normalized replacement to a scoped device/request key.
type NetworkAssignment struct {
	ScopeID, DeviceID, IdempotencyKey string
	Policy                            NetworkPolicy
}

// NetworkSecretProvider resolves immutable versioned references only within an explicitly authorized scope.
// References must retain identical bytes across retries/restarts. Rotate with a new reference and desired revision.
// The host owns persistence, encryption, access control and lifetime; failures never permit empty-secret fallback.
// Returned bytes belong to the caller. Do not log secret values or place them in public snapshots/events.
type NetworkSecretProvider interface {
	// ReadNetworkSecret returns the passphrase bound to an immutable scoped reference; nil/error fails closed.
	// Usage: secret, err := vault.ReadNetworkSecret(ctx, scope, "wifi-key-version-2").
	ReadNetworkSecret(context.Context, string, string) ([]byte, error)
}

// NetworkStore shares policy/secret-reference state and report invalidations with the serialized device store.
type NetworkStore interface {
	// GetNetworkRequest reads the original scoped replacement receipt without resolving secret references.
	// Usage: desired, err := store.GetNetworkRequest(ctx, scope, deviceID, requestKey).
	GetNetworkRequest(context.Context, string, string, string) (DesiredNetworkPolicy, error)
	// SetNetworkPolicy binds a normalized document/key, advances desired revision once and records a device change.
	// Usage: desired, err := store.SetNetworkPolicy(ctx, assignment).
	SetNetworkPolicy(context.Context, NetworkAssignment) (DesiredNetworkPolicy, error)
	// GetNetworkPolicy reads consistent desired/reported/compliance state in exactly one authorized scope.
	// Usage: status, err := store.GetNetworkPolicy(ctx, scope, deviceID).
	GetNetworkPolicy(context.Context, string, string) (NetworkStatus, error)
}

// Bound profile count and certificate allocation independently of transport's aggregate message limit.
const maximumNetworkEntries = 8
const maximumNetworkDocumentBytes = 200 * 1024
const maximumNetworkCertificateBytes = 16 * 1024

// NormalizeNetworkPolicy detaches, validates and sorts a bounded replacement before idempotency comparison.
// Certificate validity is checked again at delivery/application time; certificates must be complete CA DER objects.
// Usage: policy, err := ports.NormalizeNetworkPolicy(input).
func NormalizeNetworkPolicy(input NetworkPolicy) (NetworkPolicy, error) {
	for _, profile := range input.Profiles {
		if !utf8.ValidString(profile.SSID) {
			return NetworkPolicy{}, ErrInvalidArgument
		}
	}
	data, err := json.Marshal(input)
	if err != nil || len(data) > maximumNetworkDocumentBytes {
		return NetworkPolicy{}, ErrInvalidArgument
	}
	var p NetworkPolicy
	if err = json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	if len(p.Profiles) > maximumNetworkEntries || len(p.Certificates) > maximumNetworkEntries {
		return p, ErrInvalidArgument
	}
	if p.Profiles == nil {
		p.Profiles = []WifiProfile{}
	}
	if p.Certificates == nil {
		p.Certificates = []CACertificate{}
	}
	sort.Slice(p.Profiles, func(i, j int) bool { return p.Profiles[i].ID < p.Profiles[j].ID })
	sort.Slice(p.Certificates, func(i, j int) bool { return p.Certificates[i].ID < p.Certificates[j].ID })
	seenSSID := map[string]bool{}
	primary := false
	for i, profile := range p.Profiles {
		if !configurationKey.MatchString(profile.ID) || i > 0 && p.Profiles[i-1].ID == profile.ID || len(profile.SSID) < 1 || len(profile.SSID) > 32 || !utf8.ValidString(profile.SSID) || strings.ContainsRune(profile.SSID, 0) || seenSSID[profile.SSID] {
			return p, ErrInvalidArgument
		}
		if !slices.Contains([]string{"OPEN", "WPA2_PSK", "WPA3_SAE"}, profile.Security) || profile.Security == "OPEN" && profile.SecretRef != "" || profile.Security != "OPEN" && !configurationKey.MatchString(profile.SecretRef) {
			return p, ErrInvalidArgument
		}
		seenSSID[profile.SSID] = true
		primary = primary || profile.ID == p.PrimaryID
	}
	if len(p.Profiles) > 0 && !primary || len(p.Profiles) == 0 && p.PrimaryID != "" {
		return p, ErrInvalidArgument
	}
	seenCA := map[string]bool{}
	for i, certificate := range p.Certificates {
		if !configurationKey.MatchString(certificate.ID) || i > 0 && p.Certificates[i-1].ID == certificate.ID || len(certificate.DER) > maximumNetworkCertificateBytes {
			return p, ErrInvalidArgument
		}
		cert, err := x509.ParseCertificate(certificate.DER)
		if err != nil || !cert.IsCA || !cert.BasicConstraintsValid || seenCA[string(certificate.DER)] {
			return p, ErrInvalidArgument
		}
		// X.509 Key Usage is optional; an explicitly present extension must authorize CA signing.
		for _, extension := range cert.Extensions {
			if extension.Id.Equal(asn1.ObjectIdentifier{2, 5, 29, 15}) && cert.KeyUsage&x509.KeyUsageCertSign == 0 {
				return p, ErrInvalidArgument
			}
		}
		seenCA[string(certificate.DER)] = true
	}
	return p, nil
}
