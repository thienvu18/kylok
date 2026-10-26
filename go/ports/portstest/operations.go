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

// Operations verifies storage-level atomic reports, replay, assignments and retention.
// Usage: portstest.Operations(t, factory).
func Operations(t *testing.T, factory func(*testing.T) ports.StateStore) {
	t.Helper()
	t.Run("sync-assignment-rollback-retention", func(t *testing.T) {
		ctx := context.Background()
		s := factory(t)
		now := time.Now().UTC().Truncate(time.Millisecond)
		token := sha256.Sum256([]byte("token"))
		key := sha256.Sum256([]byte("key"))
		credential := sha256.Sum256([]byte("credential"))
		if err := s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: token[:], ScopeID: "scope", ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Consume(ctx, ports.Attempt{TokenHash: token[:], ID: "attempt", KeyHash: key[:], Device: ports.Device{ID: "device", ScopeID: "scope"}, CredentialHash: credential[:], Receipt: []byte("encrypted"), Now: now, RecoverUntil: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		artifact := ports.Artifact{ScopeID: "scope", SHA256: "digest", PackageName: "io.test", VersionCode: 2, MinSDK: 33, SizeBytes: 100, SigningCertificates: []string{"certificate"}}
		if err := s.RegisterArtifact(ctx, artifact); err != nil {
			t.Fatal(err)
		}
		catalog, err := s.ListArtifacts(ctx, "scope", "", 1)
		if err != nil || len(catalog) != 1 || catalog[0].SHA256 != artifact.SHA256 {
			t.Fatal("registered catalog entry missing", catalog, err)
		}
		for _, query := range [][2]string{{"other", ""}, {"scope", artifact.SHA256}} {
			catalog, err = s.ListArtifacts(ctx, query[0], query[1], 1)
			if err != nil || len(catalog) != 0 {
				t.Fatal("catalog leaked scope or repeated cursor", catalog, err)
			}
		}
		altered := artifact
		altered.VersionCode = 3
		if err := s.RegisterArtifact(ctx, altered); !errors.Is(err, ports.ErrConflict) {
			t.Fatal("mutable catalog", err)
		}
		assignment := ports.Assignment{ScopeID: "scope", DeviceID: "device", ArtifactSHA256: "digest", IdempotencyKey: "same", OperationID: "operation", Now: now, Deadline: now.Add(time.Hour)}
		op, err := s.Assign(ctx, assignment)
		if err != nil {
			t.Fatal(err)
		}
		again, err := s.Assign(ctx, assignment)
		if err != nil || again.ID != op.ID || again.Revision != 1 {
			t.Fatal("duplicate assignment", again, err)
		}
		input := ports.SyncInput{DeviceID: "device", CredentialHash: credential[:], Sequence: 1, Now: now, Inventory: ports.Inventory{APILevel: 35, DeviceOwner: true, Capabilities: []string{"install_apk"}}}
		snapshot, err := s.Sync(ctx, input)
		if err != nil || len(snapshot.Operations) != 1 {
			t.Fatal("sync", snapshot, err)
		}
		lease := snapshot.Operations[0].LeaseID
		input.Sequence = 2
		input.ReportedRevision = 1
		report := ports.OperationReport{ID: "receipt", OperationID: op.ID, LeaseID: lease, Sequence: 1, State: "SUCCEEDED", Observed: &ports.InstalledApplication{PackageName: "io.test", VersionCode: 2, SigningCertificates: []string{"certificate"}}}
		conflict := report
		conflict.ErrorCode = "different-payload"
		input.Reports = []ports.OperationReport{report, conflict}
		if _, err := s.Sync(ctx, input); !errors.Is(err, ports.ErrConflict) {
			t.Fatal("conflicting report", err)
		}
		ops, err := s.ListOperations(ctx, "scope", "device", "", 100, now)
		if err != nil || ops[0].State != "RUNNING" {
			t.Fatal("failed sync partially committed", ops, err)
		}
		// Retrying the same sequence with the valid body succeeds after rollback.
		input.Reports = []ports.OperationReport{report}
		completed, err := s.Sync(ctx, input)
		if err != nil || !completed.Receipts[0].Accepted {
			t.Fatal("completion", completed, err)
		}
		replay, err := s.Sync(ctx, input)
		if err != nil || replay.Sequence != 2 || len(replay.Receipts) != 1 {
			t.Fatal("lost response", replay, err)
		}
		input.Inventory.Model = "conflict"
		if _, err := s.Sync(ctx, input); !errors.Is(err, ports.ErrConflict) {
			t.Fatal("sync binding", err)
		}
		if err := s.RemoveArtifact(ctx, "scope", "digest"); !errors.Is(err, ports.ErrReferenced) {
			t.Fatal("desired reference removed", err)
		}
		if _, err := s.Artifact(ctx, "other", "digest"); !errors.Is(err, ports.ErrNotFound) {
			t.Fatal("scope leak", err)
		}
	})
}
