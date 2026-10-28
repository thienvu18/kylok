// SPDX-License-Identifier: LGPL-3.0-only
package portstest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

// Network verifies scoped reference-only intent, idempotency, observed connectivity and atomic rollback with command effects.
// Usage: portstest.Network(t, factory).
func Network(t *testing.T, factory func(*testing.T) ports.StateStore) {
	t.Helper()
	t.Run("network-references-observation-release-rollback", func(t *testing.T) {
		ctx := context.Background()
		s := factory(t)
		now, credential := fleetFixture(t, s)
		assignment := ports.NetworkAssignment{ScopeID: "scope", DeviceID: "device-a", IdempotencyKey: "network", Policy: ports.NetworkPolicy{PrimaryID: "office", Profiles: []ports.WifiProfile{{ID: "office", SSID: "Office", Security: "WPA2_PSK", SecretRef: "key-v1"}}}}
		desired, err := s.SetNetworkPolicy(ctx, assignment)
		if err != nil || desired.Revision != 1 {
			t.Fatal(desired, err)
		}
		if retry, err := s.SetNetworkPolicy(ctx, assignment); err != nil || retry.Revision != 1 {
			t.Fatal(retry, err)
		}
		changed := assignment
		changed.Policy.Profiles = []ports.WifiProfile{{ID: "office", SSID: "Office", Security: "WPA2_PSK", SecretRef: "key-v2"}}
		if _, err := s.SetNetworkPolicy(ctx, changed); !errors.Is(err, ports.ErrConflict) {
			t.Fatal("reference rebound", err)
		}
		for _, scope := range []string{"foreign", ""} {
			if _, err := s.GetNetworkPolicy(ctx, scope, "device-a"); !errors.Is(err, ports.ErrNotFound) {
				t.Fatal("network scope leak", err)
			}
			if _, err := s.GetNetworkRequest(ctx, scope, "device-a", "network"); !errors.Is(err, ports.ErrNotFound) {
				t.Fatal("receipt scope leak", err)
			}
		}
		unsupported, err := s.GetNetworkPolicy(ctx, "scope", "device-a")
		if err != nil || unsupported.State != "UNSUPPORTED" {
			t.Fatal(unsupported, err)
		}
		input := ports.SyncInput{DeviceID: "device-a", CredentialHash: credential, Sequence: 1, Now: now, Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"network.lifecycle", "command.reboot"}}}
		initial, err := s.Sync(ctx, input)
		if err != nil || initial.Network.State != "PENDING" || initial.Network.Desired.Revision != 1 {
			t.Fatal(initial, err)
		}
		if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "scope", DeviceID: "device-a", Kind: "REBOOT", ID: "command", IdempotencyKey: "command", Now: now, Deadline: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		input.Sequence++
		input.ReportedRevision = 1
		dispatched, err := s.Sync(ctx, input)
		if err != nil || len(dispatched.Commands) != 1 {
			t.Fatal(dispatched, err)
		}
		before, err := s.ListChanges(ctx, "scope", 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		input.Sequence++
		input.CommandReports = []ports.CommandReport{{CommandID: "command", DispatchToken: dispatched.Commands[0].DispatchToken, State: "SUCCEEDED", Observation: &ports.CommandObservation{BootBefore: 7, BootAfter: 8}}}
		input.NetworkReport = &ports.NetworkReport{Revision: 1, State: "APPLIED", ActiveProfileID: "office", ConnectivityVerified: false, CleanupComplete: true}
		if _, err := s.Sync(ctx, input); !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatal("setter accepted as network success", err)
		}
		command, err := s.GetCommand(ctx, "scope", "device-a", "command", now)
		if err != nil || command.State != "DISPATCHED" {
			t.Fatal("invalid network report partially committed command", command, err)
		}
		after, err := s.ListChanges(ctx, "scope", before.Next, 100)
		if err != nil || len(after.Items) != 0 {
			t.Fatal("rolled-back network report entered feed", after, err)
		}
		input.NetworkReport.ConnectivityVerified = true
		if applied, err := s.Sync(ctx, input); err != nil || applied.Network.State != "APPLIED" {
			t.Fatal(applied, err)
		}
		changed.IdempotencyKey = "rotation"
		next, err := s.SetNetworkPolicy(ctx, changed)
		if err != nil || next.Revision != 2 {
			t.Fatal(next, err)
		}
		status, err := s.GetNetworkPolicy(ctx, "scope", "device-a")
		if err != nil || status.State != "PENDING" || status.Reported.Revision != 1 {
			t.Fatal("stale report became current compliance", status, err)
		}
		input.Sequence++
		input.CommandReports = nil
		if stale, err := s.Sync(ctx, input); err != nil || stale.Network.State != "PENDING" {
			t.Fatal(stale, err)
		}
		input.Sequence++
		input.ReportedRevision = 2
		input.NetworkReport = &ports.NetworkReport{Revision: 2, State: "FAILED", Reason: "MANAGEMENT_UNREACHABLE"}
		if failed, err := s.Sync(ctx, input); err != nil || failed.Network.State != "FAILED" {
			t.Fatal(failed, err)
		}
		release, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: "scope", DeviceID: "device-a", IdempotencyKey: "release", Policy: ports.NetworkPolicy{}})
		if err != nil || release.Revision != 3 {
			t.Fatal(release, err)
		}
		input.Sequence++
		input.ReportedRevision = 3
		input.NetworkReport = &ports.NetworkReport{Revision: 3, State: "APPLIED", ConnectivityVerified: true, CleanupComplete: true}
		if released, err := s.Sync(ctx, input); err != nil || released.Network.State != "APPLIED" || len(released.Network.Desired.Policy.Profiles) != 0 {
			t.Fatal(released, err)
		}
		state, err := s.GetDeviceState(ctx, "scope", "device-a")
		if err != nil || state.Network.Desired.Revision != 3 || state.Network.Reported.Revision != 3 {
			t.Fatal("aggregate omitted network state", state, err)
		}
		if err := s.Revoke(ctx, "scope", "device-a"); err != nil {
			t.Fatal(err)
		}
		assignment.IdempotencyKey = "revoked"
		if _, err := s.SetNetworkPolicy(ctx, assignment); !errors.Is(err, ports.ErrRevoked) {
			t.Fatal(err)
		}
	})
}
