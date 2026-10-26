// SPDX-License-Identifier: LGPL-3.0-only
// Package portstest exposes transactional persistence conformance checks to adapter authors.
package portstest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

// State runs against a fresh durable store. Callers should separately prove
// process-restart persistence, resource ownership and backend-specific failures.
// Usage: portstest.State(t, factory).
func State(t *testing.T, factory func(*testing.T) ports.StateStore) {
	t.Helper()
	t.Run("atomic-consume-recovery-scope-revoke", func(t *testing.T) {
		ctx := context.Background()
		s := factory(t)
		now := time.Now().UTC().Truncate(time.Millisecond)
		token := sha256.Sum256([]byte("token"))
		key := sha256.Sum256([]byte("key"))
		credential := sha256.Sum256([]byte("credential"))
		e := ports.Enrollment{TokenHash: token[:], ScopeID: "site", ExpiresAt: now.Add(time.Minute), ArtifactSHA256: "apk"}
		if err := s.CreateEnrollment(ctx, e); err != nil {
			t.Fatal(err)
		}
		a := ports.Attempt{TokenHash: token[:], ID: "original-attempt", KeyHash: key[:], Device: ports.Device{ID: "device", ScopeID: "site", Inventory: ports.Inventory{APILevel: 35}}, CredentialHash: credential[:], Receipt: []byte("opaque-encrypted-receipt"), Now: now, RecoverUntil: now.Add(time.Hour)}
		var wg sync.WaitGroup
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				receipt, err := s.Consume(ctx, a)
				if err != nil || !bytes.Equal(receipt, a.Receipt) {
					t.Error("consume", err)
				}
			}()
		}
		wg.Wait()
		list, err := s.ListDevices(ctx, "site", "", 100)
		if err != nil || len(list) != 1 {
			t.Fatal("atomic rows", list, err)
		}
		list, err = s.ListDevices(ctx, "other", "", 100)
		if err != nil || len(list) != 0 {
			t.Fatal("scope leak", list, err)
		}
		wrong := a
		wrong.ID = "other-attempt"
		if _, err = s.Consume(ctx, wrong); !errors.Is(err, ports.ErrConsumed) {
			t.Fatal("ID binding", err)
		}
		wrong = a
		wrong.KeyHash = make([]byte, 32)
		if _, err = s.Consume(ctx, wrong); !errors.Is(err, ports.ErrConsumed) {
			t.Fatal("key binding", err)
		}
		a.Now = now.Add(2 * time.Minute)
		if _, err = s.Consume(ctx, a); err != nil {
			t.Fatal("bound recovery after token expiry", err)
		}
		if _, err = s.Sync(ctx, ports.SyncInput{DeviceID: "device", CredentialHash: make([]byte, 32), Sequence: 1, Now: now}); !errors.Is(err, ports.ErrUnauthorized) {
			t.Fatal("bad credential", err)
		}
		if _, err = s.Sync(ctx, ports.SyncInput{DeviceID: "device", CredentialHash: credential[:], Inventory: ports.Inventory{APILevel: 35, DeviceOwner: true}, Sequence: 1, Now: now}); err != nil {
			t.Fatal(err)
		}
		if err = s.Revoke(ctx, "other", "device"); !errors.Is(err, ports.ErrNotFound) {
			t.Fatal("cross-scope revoke", err)
		}
		if err = s.Revoke(ctx, "site", "device"); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Authenticate(ctx, "device", credential[:]); !errors.Is(err, ports.ErrRevoked) {
			t.Fatal("auth after revoke", err)
		}
		if _, err = s.Sync(ctx, ports.SyncInput{DeviceID: "device", CredentialHash: credential[:], Sequence: 2, Now: now}); !errors.Is(err, ports.ErrRevoked) {
			t.Fatal("sync after revoke", err)
		}
		if _, err = s.Consume(ctx, a); !errors.Is(err, ports.ErrRevoked) {
			t.Fatal("recovery after revoke", err)
		}
	})
	t.Run("expiry-boundaries", func(t *testing.T) {
		ctx := context.Background()
		s := factory(t)
		now := time.Now().UTC().Truncate(time.Millisecond)
		token := sha256.Sum256([]byte("expired"))
		e := ports.Enrollment{TokenHash: token[:], ScopeID: "site", ExpiresAt: now, ArtifactSHA256: "apk"}
		if err := s.CreateEnrollment(ctx, e); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Consume(ctx, ports.Attempt{TokenHash: token[:], Now: now}); !errors.Is(err, ports.ErrExpired) {
			t.Fatal("expiry inclusive", err)
		}
		if _, err := s.Enrollment(ctx, []byte("unknown")); !errors.Is(err, ports.ErrNotFound) {
			t.Fatal(err)
		}
	})
}
