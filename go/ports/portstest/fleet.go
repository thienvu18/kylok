// SPDX-License-Identifier: LGPL-3.0-only
package portstest

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

// Prepare independent scoped identities without depending on a facade or transport implementation.
func fleetFixture(t *testing.T, s ports.StateStore) (time.Time, []byte) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	credential := sha256.Sum256([]byte("fleet credential"))
	key := sha256.Sum256([]byte("fleet key"))
	for _, target := range []struct{ scope, id string }{{"scope", "device-a"}, {"scope", "device-b"}, {"scope", "device-c"}, {"foreign", "device-foreign"}} {
		token := sha256.Sum256([]byte(target.id))
		if err := s.CreateEnrollment(context.Background(), ports.Enrollment{TokenHash: token[:], ScopeID: target.scope, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Consume(context.Background(), ports.Attempt{TokenHash: token[:], ID: target.id, KeyHash: key[:], Device: ports.Device{ID: target.id, ScopeID: target.scope}, CredentialHash: credential[:], Receipt: []byte("encrypted"), Now: now, RecoverUntil: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RegisterArtifact(context.Background(), ports.Artifact{ScopeID: "scope", SHA256: "digest", PackageName: "io.test.app", VersionCode: 2, SigningCertificates: []string{"signer"}}); err != nil {
		t.Fatal(err)
	}
	return now, credential[:]
}

// Fleet verifies frozen membership, bounded/restartable fan-out, windows, cancellation, isolation and replay on every provider.
// Usage: portstest.Fleet(t, factory).
func Fleet(t *testing.T, factory func(*testing.T) ports.StateStore) {
	t.Helper()
	t.Run("fleet-frozen-membership-partial-results", func(t *testing.T) {
		ctx := context.Background()
		s := factory(t)
		now, _ := fleetFixture(t, s)
		group := ports.Group{ScopeID: "scope", ID: "lobby", DeviceIDs: []string{"device-b", "device-a"}}
		saved, err := s.ReplaceGroup(ctx, group, 0)
		if err != nil || saved.Revision != 1 || saved.DeviceIDs[0] != "device-a" {
			t.Fatal(saved, err)
		}
		group.DeviceIDs[0] = "mutated"
		if again, err := s.GetGroup(ctx, "scope", "lobby"); err != nil || again.DeviceIDs[1] != "device-b" {
			t.Fatal("aliased group", again, err)
		}
		group.DeviceIDs = []string{"device-a", "device-b"}
		if retry, err := s.ReplaceGroup(ctx, group, 0); err != nil || retry.Revision != 1 {
			t.Fatal("group retry", retry, err)
		}
		foreign := group
		foreign.DeviceIDs = []string{"device-foreign"}
		if _, err := s.ReplaceGroup(ctx, foreign, 1); !errors.Is(err, ports.ErrNotFound) {
			t.Fatal("foreign group member", err)
		}
		request := ports.FleetRequest{ScopeID: "scope", ID: "batch", GroupID: "lobby", ArtifactSHA256: "digest", NotBefore: now.Add(time.Hour), Deadline: now.Add(2 * time.Hour)}
		batch, err := s.CreateFleetAssignment(ctx, request, now)
		if err != nil || len(batch.Items) != 2 || batch.GroupRevision != 1 || batch.Complete {
			t.Fatal(batch, err)
		}
		if err = s.RemoveArtifact(ctx, "scope", "digest"); !errors.Is(err, ports.ErrReferenced) {
			t.Fatal("pending batch lost artifact", err)
		}
		group.DeviceIDs = []string{"device-c"}
		if _, err = s.ReplaceGroup(ctx, group, 1); err != nil {
			t.Fatal(err)
		}
		if _, err = s.ReplaceGroup(ctx, group, 0); !errors.Is(err, ports.ErrConflict) {
			t.Fatal("lost group update", err)
		}
		if retry, err := s.CreateFleetAssignment(ctx, request, now); err != nil || len(retry.Items) != 2 || retry.GroupRevision != 1 {
			t.Fatal("retry expanded membership", retry, err)
		}
		changed := request
		changed.Deadline = changed.Deadline.Add(time.Second)
		if _, err = s.CreateFleetAssignment(ctx, changed, now); !errors.Is(err, ports.ErrConflict) {
			t.Fatal("window conflict", err)
		}
		first, err := s.AdvanceFleetAssignment(ctx, "scope", "batch", now, 1)
		if err != nil || first.Complete || first.Items[0].State != "ASSIGNED" || first.Items[1].State != "PENDING" {
			t.Fatal(first, err)
		}
		if err := s.Revoke(ctx, "scope", "device-b"); err != nil {
			t.Fatal(err)
		}
		done, err := s.AdvanceFleetAssignment(ctx, "scope", "batch", now, 1)
		if err != nil || !done.Complete || done.Items[0].OperationID != first.Items[0].OperationID || done.Items[1].ErrorCode != "DEVICE_REVOKED" {
			t.Fatal(done, err)
		}
		if retry, err := s.AdvanceFleetAssignment(ctx, "scope", "batch", now, 100); err != nil || retry.Items[0].OperationID != first.Items[0].OperationID {
			t.Fatal(retry, err)
		}
		for _, id := range []string{"device-b", "device-c"} {
			ops, err := s.ListOperations(ctx, "scope", id, "", 100, now)
			if err != nil || len(ops) != 0 {
				t.Fatal("fanout reached wrong member", id, ops, err)
			}
		}
		if _, err := s.GetFleetAssignment(ctx, "foreign", "batch"); !errors.Is(err, ports.ErrNotFound) {
			t.Fatal("batch scope leak", err)
		}
		if _, err := s.CancelFleetAssignment(ctx, "foreign", "batch", now); !errors.Is(err, ports.ErrNotFound) {
			t.Fatal("foreign cancellation", err)
		}
		if _, err := s.GetGroup(ctx, "foreign", "lobby"); !errors.Is(err, ports.ErrNotFound) {
			t.Fatal("group scope leak", err)
		}
		feed, err := s.ListChanges(ctx, "scope", 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		kinds := map[string]bool{}
		for _, change := range feed.Items {
			kinds[change.Kind] = true
		}
		if !kinds["group_changed"] || !kinds["fleet_changed"] {
			t.Fatal("missing durable notifications", feed)
		}
		projection, err := s.ReadProjection(ctx, "scope", "", 0, 100, now)
		if err != nil {
			t.Fatal(err)
		}
		groups, batches := 0, 0
		for _, item := range projection.Items {
			if item.Group != nil {
				groups++
				if item.Group.DeviceIDs[0] != "device-c" {
					t.Fatal(item)
				}
			}
			if item.Fleet != nil {
				batches++
				if !item.Fleet.Complete {
					t.Fatal(item)
				}
			}
		}
		if groups != 1 || batches != 1 {
			t.Fatal("projection omitted fleet state", projection)
		}
	})
	t.Run("fleet-windows-cancel-and-stale-delivery", func(t *testing.T) {
		ctx := context.Background()
		s := factory(t)
		now, credential := fleetFixture(t, s)
		request := ports.FleetRequest{ScopeID: "scope", ID: "window", DeviceIDs: []string{"device-a", "device-b"}, ArtifactSHA256: "digest", NotBefore: now.Add(time.Hour), Deadline: now.Add(2 * time.Hour)}
		if _, err := s.CreateFleetAssignment(ctx, request, now); err != nil {
			t.Fatal(err)
		}
		batch, err := s.AdvanceFleetAssignment(ctx, "scope", "window", now, 1)
		if err != nil {
			t.Fatal(err)
		}
		opID := batch.Items[0].OperationID
		in := ports.SyncInput{DeviceID: "device-a", CredentialHash: credential, Sequence: 1, Now: now, Inventory: ports.Inventory{Capabilities: []string{"install_apk"}}}
		before, err := s.Sync(ctx, in)
		if err != nil || len(before.Operations) != 0 || before.Compliance[0].Reason != "SCHEDULED" {
			t.Fatal(before, err)
		}
		in.Sequence++
		in.ReportedRevision = 1
		in.Now = request.NotBefore
		active, err := s.Sync(ctx, in)
		if err != nil || len(active.Operations) != 1 || active.Operations[0].Attempts != 1 {
			t.Fatal(active, err)
		}
		leased := active.Operations[0]
		if _, err := s.AuthorizeDownload(ctx, "device-a", credential, opID, "digest", leased.LeaseID, now); !errors.Is(err, ports.ErrUnauthorized) {
			t.Fatal("clock rollback downloaded early", err)
		}
		cancelled, err := s.CancelFleetAssignment(ctx, "scope", "window", in.Now)
		if err != nil || !cancelled.Cancelled || !cancelled.Complete || cancelled.Items[1].State != "CANCELLED" {
			t.Fatal(cancelled, err)
		}
		if _, err := s.AuthorizeDownload(ctx, "device-a", credential, opID, "digest", leased.LeaseID, in.Now); !errors.Is(err, ports.ErrUnauthorized) {
			t.Fatal("cancelled download", err)
		}
		// An exact lost-response replay remains immutable; cancellation arrives on the next sequence.
		replay, err := s.Sync(ctx, in)
		if err != nil || len(replay.Operations) != 1 || replay.Operations[0].LeaseID != leased.LeaseID {
			t.Fatal(replay, err)
		}
		in.Sequence++
		in.RetainedOperationIDs = []string{opID}
		in.Reports = []ports.OperationReport{{ID: "late", OperationID: opID, LeaseID: leased.LeaseID, Sequence: 1, State: "SUCCEEDED", Observed: &ports.InstalledApplication{PackageName: "io.test.app", VersionCode: 2, SigningCertificates: []string{"signer"}}}}
		stopped, err := s.Sync(ctx, in)
		if err != nil || len(stopped.Operations) != 0 || len(stopped.StoppedOperationIDs) != 1 || stopped.StoppedOperationIDs[0] != opID {
			t.Fatal(stopped, err)
		}
		op, err := s.GetOperation(ctx, "scope", "device-a", opID, in.Now)
		if err != nil || op.State != "CANCELLED" || op.Observed != nil {
			t.Fatal("late report overwrote cancellation", op, err)
		}
		// Repeated inventory mismatch cannot silently resume cancelled desired intent.
		in.Sequence++
		in.Reports = nil
		in.Now = in.Now.Add(time.Minute)
		if next, err := s.Sync(ctx, in); err != nil || len(next.Operations) != 0 {
			t.Fatal(next, err)
		}
		if retry, err := s.AdvanceFleetAssignment(ctx, "scope", "window", now, 100); err != nil || !retry.Cancelled {
			t.Fatal(retry, err)
		}
	})
	t.Run("fleet-concurrent-workers-and-expiry", func(t *testing.T) {
		ctx := context.Background()
		s := factory(t)
		now, _ := fleetFixture(t, s)
		request := ports.FleetRequest{ScopeID: "scope", ID: "concurrent", DeviceIDs: []string{"device-a", "device-b", "missing"}, ArtifactSHA256: "digest", NotBefore: now, Deadline: now.Add(time.Hour)}
		if _, err := s.CreateFleetAssignment(ctx, request, now); err != nil {
			t.Fatal(err)
		}
		var wait sync.WaitGroup
		for i := 0; i < 8; i++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				if _, err := s.AdvanceFleetAssignment(ctx, "scope", "concurrent", now, 1); err != nil {
					t.Error(err)
				}
			}()
		}
		wait.Wait()
		batch, err := s.GetFleetAssignment(ctx, "scope", "concurrent")
		if err != nil || !batch.Complete || batch.Items[2].ErrorCode != "NOT_FOUND" {
			t.Fatal(batch, err)
		}
		for _, id := range []string{"device-a", "device-b"} {
			ops, err := s.ListOperations(ctx, "scope", id, "", 100, now)
			if err != nil || len(ops) != 1 || ops[0].Revision != 1 {
				t.Fatal("duplicate fanout", ops, err)
			}
		}
		request.ID = "expired"
		request.DeviceIDs = []string{"device-c"}
		if _, err := s.CreateFleetAssignment(ctx, request, now); err != nil {
			t.Fatal(err)
		}
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := s.AdvanceFleetAssignment(cancelledCtx, "scope", "expired", now, 1); !errors.Is(err, context.Canceled) {
			t.Fatal("ignored context", err)
		}
		if expired, err := s.AdvanceFleetAssignment(ctx, "scope", "expired", request.Deadline, 100); err != nil || !expired.Complete || expired.Items[0].ErrorCode != "DEADLINE_EXCEEDED" {
			t.Fatal(expired, err)
		}
		if retry, err := s.CreateFleetAssignment(ctx, request, request.Deadline.Add(time.Hour)); err != nil || !retry.Complete {
			t.Fatal("expired retry lost checkpoint", retry, err)
		}
		for _, count := range []int{0, 101} {
			if _, err := s.AdvanceFleetAssignment(ctx, "scope", "concurrent", now, count); !errors.Is(err, ports.ErrInvalidArgument) {
				t.Fatal(fmt.Sprint(count), err)
			}
		}
	})
}
