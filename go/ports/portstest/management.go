// SPDX-License-Identifier: LGPL-3.0-only
package portstest

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

// Management verifies correlation binding, scoped point/filter reads, expiry and cancellation.
// Usage: portstest.Management(t, factory); run this unchanged for every state adapter.
func Management(t *testing.T, factory func(*testing.T) ports.StateStore) {
	t.Helper()
	t.Run("management-correlation-scope-query-expiry", func(t *testing.T) {
		ctx := context.Background()
		store := factory(t)
		now := time.Now().UTC().Truncate(time.Millisecond)
		if err := store.Check(ctx); err != nil {
			t.Fatal(err)
		}
		token := sha256.Sum256([]byte("management-token"))
		key := sha256.Sum256([]byte("management-key"))
		credential := sha256.Sum256([]byte("management-credential"))
		enrollment := ports.Enrollment{TokenHash: token[:], ScopeID: "scope", CorrelationID: "host-intent", ExpiresAt: now.Add(time.Minute)}
		if err := store.CreateEnrollment(ctx, enrollment); err != nil {
			t.Fatal(err)
		}
		other := enrollment
		changed := sha256.Sum256([]byte("another-token"))
		other.TokenHash = changed[:]
		if err := store.CreateEnrollment(ctx, other); !errors.Is(err, ports.ErrConflict) {
			t.Fatal("duplicate correlation accepted", err)
		}
		other.ScopeID = "other"
		if err := store.CreateEnrollment(ctx, other); err != nil {
			t.Fatal("cross-scope correlation rejected", err)
		}
		attempt := ports.Attempt{TokenHash: token[:], ID: "attempt", KeyHash: key[:], Device: ports.Device{ID: "device", ScopeID: "scope", CorrelationID: "untrusted-candidate"}, CredentialHash: credential[:], Receipt: []byte("encrypted"), Now: now, RecoverUntil: now.Add(time.Hour)}
		if _, err := store.Consume(ctx, attempt); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Consume(ctx, attempt); err != nil {
			t.Fatal("retry lost identity", err)
		}
		value, err := store.GetDevice(ctx, "scope", "device")
		if err != nil || value.CorrelationID != "host-intent" {
			t.Fatal("correlation was not bound atomically", value, err)
		}
		for _, scope := range []string{"other", "missing"} {
			if _, err = store.GetDevice(ctx, scope, "device"); !errors.Is(err, ports.ErrNotFound) {
				t.Fatal("point read leaked scope", err)
			}
		}
		values, err := store.QueryDevices(ctx, ports.DeviceQuery{ScopeID: "scope", CorrelationID: "host-intent", State: "ENROLLED", Limit: 20})
		if err != nil || len(values) != 1 {
			t.Fatal(values, err)
		}
		for _, q := range []ports.DeviceQuery{{ScopeID: "other", Limit: 20}, {ScopeID: "scope", CorrelationID: "different", Limit: 20}, {ScopeID: "scope", State: "MANAGED", Limit: 20}, {ScopeID: "scope", After: "device", Limit: 20}} {
			values, err = store.QueryDevices(ctx, q)
			if err != nil || len(values) != 0 {
				t.Fatal("filter/cursor isolation", values, err)
			}
		}
		artifact := ports.Artifact{ScopeID: "scope", SHA256: "digest", PackageName: "app.example", VersionCode: 1, SigningCertificates: []string{"signer"}}
		if err = store.RegisterArtifact(ctx, artifact); err != nil {
			t.Fatal(err)
		}
		assigned, err := store.Assign(ctx, ports.Assignment{ScopeID: "scope", DeviceID: "device", ArtifactSHA256: "digest", IdempotencyKey: "assign", OperationID: "operation", Now: now, Deadline: now.Add(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.GetOperation(ctx, "other", "device", assigned.ID, now); !errors.Is(err, ports.ErrNotFound) {
			t.Fatal(err)
		}
		if _, err = store.GetOperation(ctx, "scope", "foreign", assigned.ID, now); !errors.Is(err, ports.ErrNotFound) {
			t.Fatal(err)
		}
		operations, err := store.QueryOperations(ctx, ports.OperationQuery{ScopeID: "scope", DeviceID: "device", State: "QUEUED", Limit: 20, Now: now})
		if err != nil || len(operations) != 1 {
			t.Fatal(operations, err)
		}
		expired, err := store.GetOperation(ctx, "scope", "device", assigned.ID, now.Add(2*time.Minute))
		if err != nil || expired.State != "FAILED" || expired.ErrorCode != "DEADLINE_EXCEEDED" {
			t.Fatal(expired, err)
		}
		operations, err = store.QueryOperations(ctx, ports.OperationQuery{ScopeID: "scope", DeviceID: "device", State: "QUEUED", Limit: 20, Now: now.Add(2 * time.Minute)})
		if err != nil || len(operations) != 0 {
			t.Fatal(operations, err)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if err = store.Check(cancelled); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err = store.GetDevice(cancelled, "scope", "device"); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err = store.QueryDevices(cancelled, ports.DeviceQuery{ScopeID: "scope", Limit: 20}); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}
