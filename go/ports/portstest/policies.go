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

// Policies checks scope, idempotent replacement, observed compliance, atomic
// rollback and revocation for custom providers as well as the default adapter.
// Usage: portstest.Policies(t, factory).
func Policies(t *testing.T, factory func(*testing.T) ports.StateStore) {
	t.Helper()
	t.Run("policy-replacement-observation-rollback", func(t *testing.T) {
		ctx := context.Background()
		s := factory(t)
		now := time.Now().UTC()
		token := sha256.Sum256([]byte("policy-token"))
		key := sha256.Sum256([]byte("key"))
		credential := sha256.Sum256([]byte("credential"))
		if err := s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: token[:], ScopeID: "site", ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Consume(ctx, ports.Attempt{TokenHash: token[:], ID: "attempt", KeyHash: key[:], Device: ports.Device{ID: "device", ScopeID: "site"}, CredentialHash: credential[:], Receipt: []byte("sealed"), Now: now, RecoverUntil: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		disabled := true
		a := ports.PolicyAssignment{ScopeID: "site", DeviceID: "device", IdempotencyKey: "request", Policy: ports.Policy{CameraDisabled: &disabled}}
		want, err := s.SetPolicy(ctx, a)
		if err != nil || want.Revision != 1 {
			t.Fatal(want, err)
		}
		again, err := s.SetPolicy(ctx, a)
		if err != nil || again.Revision != 1 {
			t.Fatal("duplicate", again, err)
		}
		a.ScopeID = "other"
		if _, err = s.SetPolicy(ctx, a); !errors.Is(err, ports.ErrNotFound) {
			t.Fatal("scope", err)
		}
		a.ScopeID = "site"
		a.Policy = ports.Policy{}
		if _, err = s.SetPolicy(ctx, a); !errors.Is(err, ports.ErrConflict) {
			t.Fatal("payload binding", err)
		}
		in := ports.SyncInput{DeviceID: "device", CredentialHash: credential[:], Sequence: 1, Now: now, ReportedRevision: 1,
			Inventory:    ports.Inventory{Capabilities: []string{"durable_sync", "policy.camera"}},
			PolicyReport: &ports.PolicyReport{Revision: 1, SnapshotRevision: 1, Results: []ports.PolicyResult{{RuleID: "unknown", State: "APPLIED", ObservedValue: "true"}}}}
		if _, err = s.Sync(ctx, in); !errors.Is(err, ports.ErrConflict) {
			t.Fatal("unknown policy result", err)
		}
		in.PolicyReport.Results = []ports.PolicyResult{{RuleID: "camera", State: "APPLIED", ObservedValue: "false"}, {RuleID: "cleanup", State: "APPLIED", ObservedValue: "complete"}}
		snapshot, err := s.Sync(ctx, in)
		if err != nil {
			t.Fatal("same sequence after rollback", err)
		}
		if snapshot.PolicyCompliance[0].State != "FAILED" || snapshot.PolicyCompliance[0].Reason != "OBSERVATION_MISMATCH" {
			t.Fatal(snapshot.PolicyCompliance)
		}
		in.Sequence = 2
		in.PolicyReport.Results[0].ObservedValue = "true"
		if _, err = s.Sync(ctx, in); err != nil {
			t.Fatal(err)
		}
		status, err := s.GetPolicy(ctx, "site", "device")
		if err != nil || status.Compliance[0].State != "APPLIED" {
			t.Fatal(status, err)
		}
		a.IdempotencyKey = "release"
		if want, err = s.SetPolicy(ctx, a); err != nil || want.Revision != 2 {
			t.Fatal(want, err)
		}
		status, err = s.GetPolicy(ctx, "site", "device")
		if err != nil || status.Reported != nil || len(status.Compliance) != 1 || status.Compliance[0].RuleID != "cleanup" || status.Compliance[0].State == "APPLIED" {
			t.Fatal("release unobserved", status, err)
		}
		if err = s.Revoke(ctx, "site", "device"); err != nil {
			t.Fatal(err)
		}
		a.IdempotencyKey = "revoked"
		if _, err = s.SetPolicy(ctx, a); !errors.Is(err, ports.ErrRevoked) {
			t.Fatal("revoked policy", err)
		}
	})
}
