// SPDX-License-Identifier: LGPL-3.0-only
package portstest

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

// Changes verifies transactional invalidations, scoped replay, immutable snapshots and explicit expiry/eviction.
// Usage: portstest.Changes(t, factory); run unchanged for both durable SQL adapters.
func Changes(t *testing.T, factory func(*testing.T) ports.StateStore) {
	t.Helper()
	t.Run("change-feed-rollback-snapshot-replay", func(t *testing.T) {
		ctx := context.Background()
		s := factory(t)
		now := time.Now().UTC().Truncate(time.Millisecond)
		token := sha256.Sum256([]byte("feed-token"))
		key := sha256.Sum256([]byte("feed-key"))
		credential := sha256.Sum256([]byte("feed-credential"))
		if err := s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: "scope", TokenHash: token[:], ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		attempt := ports.Attempt{TokenHash: token[:], ID: "attempt", KeyHash: key[:], Device: ports.Device{ID: "device", ScopeID: "scope"}, CredentialHash: credential[:], Receipt: []byte("encrypted"), Now: now, RecoverUntil: now.Add(time.Hour)}
		if _, err := s.Consume(ctx, attempt); err != nil {
			t.Fatal(err)
		}
		artifact := ports.Artifact{ScopeID: "scope", SHA256: "digest", PackageName: "app.example", VersionCode: 1, SigningCertificates: []string{"signer"}}
		if err := s.RegisterArtifact(ctx, artifact); err != nil {
			t.Fatal(err)
		}
		first, err := s.ListChanges(ctx, "scope", 0, 100)
		if err != nil || len(first.Items) != 2 || first.Next != 2 {
			t.Fatal(first, err)
		}
		replay, err := s.ListChanges(ctx, "scope", 0, 100)
		if err != nil || !reflect.DeepEqual(first, replay) {
			t.Fatal("replay changed", first, replay, err)
		}
		// Duplicate creation/delivery does not generate another logical change.
		s.Consume(ctx, attempt)
		s.RegisterArtifact(ctx, artifact)
		duplicate, err := s.ListChanges(ctx, "scope", first.Next, 100)
		if err != nil || len(duplicate.Items) != 0 {
			t.Fatal(duplicate, err)
		}
		foreign, err := s.ListChanges(ctx, "other", 0, 100)
		if err != nil || len(foreign.Items) != 0 || foreign.Head != 0 {
			t.Fatal("scope leak", foreign, err)
		}
		snapshot, err := s.ReadProjection(ctx, "scope", "", 0, 1, now)
		if err != nil || !snapshot.More || len(snapshot.Items) != 1 || snapshot.Items[0].Device == nil || snapshot.Watermark != 2 {
			t.Fatal(snapshot, err)
		}
		immutable, err := s.ReadProjection(ctx, "scope", snapshot.SnapshotID, 0, 1, now)
		if err != nil || !reflect.DeepEqual(immutable, snapshot) {
			t.Fatal("snapshot replay", immutable, err)
		}
		if _, err = s.ReadProjection(ctx, "other", snapshot.SnapshotID, 1, 1, now); !errors.Is(err, ports.ErrCursorExpired) {
			t.Fatal("foreign scan readable", err)
		}
		assignment := ports.Assignment{ScopeID: "scope", DeviceID: "device", ArtifactSHA256: "digest", IdempotencyKey: "assign", OperationID: "operation", Now: now, Deadline: now.Add(time.Hour)}
		op, err := s.Assign(ctx, assignment)
		if err != nil {
			t.Fatal(err)
		}
		input := ports.SyncInput{DeviceID: "device", CredentialHash: credential[:], Sequence: 1, Now: now, Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"install_apk"}}}
		leased, err := s.Sync(ctx, input)
		if err != nil || len(leased.Operations) != 1 {
			t.Fatal(leased, err)
		}
		before, err := s.ListChanges(ctx, "scope", 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		report := ports.OperationReport{ID: "report", OperationID: op.ID, LeaseID: leased.Operations[0].LeaseID, Sequence: 1, State: "SUCCEEDED", Observed: &ports.InstalledApplication{PackageName: "app.example", VersionCode: 1, SigningCertificates: []string{"signer"}}}
		conflict := report
		conflict.ErrorCode = "different"
		input.Sequence = 2
		input.ReportedRevision = 1
		input.Reports = []ports.OperationReport{report, conflict}
		if _, err = s.Sync(ctx, input); !errors.Is(err, ports.ErrConflict) {
			t.Fatal("expected transactional rollback", err)
		}
		after, err := s.ListChanges(ctx, "scope", 0, 100)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("rolled-back effect entered feed", before, after, err)
		}
		current, err := s.GetDeviceState(ctx, "scope", "device")
		if err != nil || current.Operations[0].State != "RUNNING" || len(current.Desired) != 1 {
			t.Fatal(current, err)
		}
		input.Reports = []ports.OperationReport{report}
		if _, err = s.Sync(ctx, input); err != nil {
			t.Fatal(err)
		}
		committed, err := s.ListChanges(ctx, "scope", after.Next, 100)
		if err != nil || len(committed.Items) != 1 || committed.Items[0].Kind != "device_changed" {
			t.Fatal(committed, err)
		}
		s.Sync(ctx, input)
		again, err := s.ListChanges(ctx, "scope", committed.Next, 100)
		if err != nil || len(again.Items) != 0 {
			t.Fatal("sync replay entered log", again, err)
		}
		// Continue the saved snapshot after mutations: old device state and catalog remain consistent with watermark.
		old, err := s.ReadProjection(ctx, "scope", snapshot.SnapshotID, 0, 100, now)
		if err != nil || len(old.Items) != 2 || old.Items[0].Device.Device.State != "ENROLLED" || len(old.Items[0].Device.Desired) != 0 || old.Watermark != snapshot.Watermark {
			t.Fatal("mutable snapshot", old, err)
		}
		gap, err := s.ListChanges(ctx, "scope", snapshot.Watermark, 100)
		if err != nil || len(gap.Items) < 3 {
			t.Fatal("snapshot handoff missed changes", gap, err)
		}
		// Unreferenced catalog deletion must be a replayable tombstone.
		unreferenced := artifact
		unreferenced.SHA256 = "unreferenced"
		if err = s.RegisterArtifact(ctx, unreferenced); err != nil {
			t.Fatal(err)
		}
		if err = s.RemoveArtifact(ctx, "scope", unreferenced.SHA256); err != nil {
			t.Fatal(err)
		}
		tail, err := s.ListChanges(ctx, "scope", gap.Next, 100)
		if err != nil || len(tail.Items) != 2 || tail.Items[1].Kind != "application_removed" {
			t.Fatal(tail, err)
		}
		if _, err = s.ListChanges(ctx, "scope", tail.Head+1, 1); !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatal(err)
		}
		if _, err = s.ReadProjection(ctx, "scope", snapshot.SnapshotID, 0, 1, now.Add(ports.SnapshotLifetime)); !errors.Is(err, ports.ErrCursorExpired) {
			t.Fatal("expired snapshot accepted", err)
		}
		for range ports.SnapshotLimit {
			if _, err = s.ReadProjection(ctx, "scope", "", 0, 1, now); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = s.ReadProjection(ctx, "scope", snapshot.SnapshotID, 0, 1, now); !errors.Is(err, ports.ErrCursorExpired) {
			t.Fatal("evicted snapshot accepted", err)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err = s.ListChanges(cancelled, "scope", 0, 1); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}
