// SPDX-License-Identifier: LGPL-3.0-only
package portstest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

// Commands verifies capability gates, stable delivery, report binding, honest uncertainty and scope isolation.
// Usage: portstest.Commands(t, factory).
func Commands(t *testing.T, factory func(*testing.T) ports.StateStore) {
	t.Helper()
	t.Run("commands-capability-observation-and-replay", func(t *testing.T) {
		ctx := context.Background()
		s := factory(t)
		now, credential := fleetFixture(t, s)
		a := ports.CommandAssignment{ScopeID: "scope", DeviceID: "device-a", Kind: "REBOOT", IdempotencyKey: "reboot", ID: "command-a", Now: now, Deadline: now.Add(time.Hour)}
		if _, err := s.SubmitCommand(ctx, a); !errors.Is(err, ports.ErrUnsupported) {
			t.Fatal("missing capability accepted", err)
		}
		input := ports.SyncInput{DeviceID: "device-a", CredentialHash: credential, Sequence: 1, Now: now, Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}}}
		if _, err := s.Sync(ctx, input); err != nil {
			t.Fatal(err)
		}
		command, err := s.SubmitCommand(ctx, a)
		if err != nil || command.State != "QUEUED" {
			t.Fatal(command, err)
		}
		a.ID = "unused"
		a.Deadline = a.Deadline.Add(time.Hour)
		if retry, err := s.SubmitCommand(ctx, a); err != nil || retry.ID != command.ID || !retry.Deadline.Equal(command.Deadline) {
			t.Fatal("request duplicate", retry, err)
		}
		conflict := a
		conflict.Kind = "LOCK"
		if _, err := s.SubmitCommand(ctx, conflict); !errors.Is(err, ports.ErrConflict) {
			t.Fatal(err)
		}
		input.Sequence++
		dispatched, err := s.Sync(ctx, input)
		if err != nil || len(dispatched.Commands) != 1 || dispatched.Commands[0].DispatchToken == "" {
			t.Fatal(dispatched, err)
		}
		token := dispatched.Commands[0].DispatchToken
		input.Sequence++
		repeated, err := s.Sync(ctx, input)
		if err != nil || len(repeated.Commands) != 1 || repeated.Commands[0].DispatchToken != token {
			t.Fatal("dispatch replaced", repeated, err)
		}
		report := ports.CommandReport{CommandID: command.ID, DispatchToken: token, State: "SUCCEEDED", Observation: &ports.CommandObservation{BootBefore: 7, BootAfter: 7}}
		input.Sequence++
		input.CommandReports = []ports.CommandReport{report}
		if _, err := s.Sync(ctx, input); !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatal("reconnection accepted as reboot", err)
		}
		report.Observation = &ports.CommandObservation{BootBefore: 7, BootAfter: 8}
		input.CommandReports = []ports.CommandReport{report}
		done, err := s.Sync(ctx, input)
		if err != nil || len(done.Commands) != 0 || len(done.CommandReceipts) != 1 {
			t.Fatal(done, err)
		}
		original, err := s.GetCommand(ctx, "scope", "device-a", command.ID, now)
		if err != nil || original.State != "SUCCEEDED" || original.Observation.BootAfter != 8 {
			t.Fatal(original, err)
		}
		input.Sequence++
		if again, err := s.Sync(ctx, input); err != nil || len(again.CommandReceipts) != 1 || len(again.Commands) != 0 {
			t.Fatal(again, err)
		}
		changed := report
		changed.ErrorCode = "changed"
		input.Sequence++
		input.CommandReports = []ports.CommandReport{changed}
		if _, err := s.Sync(ctx, input); !errors.Is(err, ports.ErrConflict) {
			t.Fatal("report rebound", err)
		}
		for _, query := range [][2]string{{"foreign", "device-a"}, {"scope", "device-b"}} {
			if _, err := s.GetCommand(ctx, query[0], query[1], command.ID, now); !errors.Is(err, ports.ErrNotFound) {
				t.Fatal("command scope leak", err)
			}
			if _, err := s.CancelCommand(ctx, query[0], query[1], command.ID, now); !errors.Is(err, ports.ErrNotFound) {
				t.Fatal("foreign command cancellation", err)
			}
		}
		state, err := s.GetDeviceState(ctx, "scope", "device-a")
		if err != nil || len(state.Commands) != 1 || state.Commands[0].State != "SUCCEEDED" {
			t.Fatal("projection lost command", state, err)
		}
	})
	t.Run("commands-cancellation-deadlines-and-lock-readback", func(t *testing.T) {
		ctx := context.Background()
		s := factory(t)
		now, credential := fleetFixture(t, s)
		input := ports.SyncInput{DeviceID: "device-a", CredentialHash: credential, Sequence: 1, Now: now, Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}}}
		if _, err := s.Sync(ctx, input); err != nil {
			t.Fatal(err)
		}
		assignment := ports.CommandAssignment{ScopeID: "scope", DeviceID: "device-a", Kind: "LOCK", IdempotencyKey: "queued", ID: "queued", Now: now, Deadline: now.Add(time.Hour)}
		if _, err := s.SubmitCommand(ctx, assignment); err != nil {
			t.Fatal(err)
		}
		if cancelled, err := s.CancelCommand(ctx, "scope", "device-a", "queued", now); err != nil || cancelled.State != "CANCELLED" {
			t.Fatal(cancelled, err)
		}
		assignment.ID = "lock"
		assignment.IdempotencyKey = "lock"
		if _, err := s.SubmitCommand(ctx, assignment); err != nil {
			t.Fatal(err)
		}
		input.Sequence++
		active, err := s.Sync(ctx, input)
		if err != nil || len(active.Commands) != 1 || active.Commands[0].ID != "lock" {
			t.Fatal(active, err)
		}
		if uncertain, err := s.CancelCommand(ctx, "scope", "device-a", "lock", now); err != nil || uncertain.State != "UNCERTAIN" {
			t.Fatal(uncertain, err)
		}
		input.Sequence++
		input.CommandReports = []ports.CommandReport{{CommandID: "lock", DispatchToken: active.Commands[0].DispatchToken, State: "SUCCEEDED", Observation: &ports.CommandObservation{Locked: true, Secure: false}}}
		if _, err := s.Sync(ctx, input); !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatal("unsecured lock accepted", err)
		}
		input.CommandReports[0].Observation.Secure = true
		if observed, err := s.Sync(ctx, input); err != nil || len(observed.Commands) != 0 {
			t.Fatal(observed, err)
		}
		if done, err := s.GetCommand(ctx, "scope", "device-a", "lock", now); err != nil || done.State != "SUCCEEDED" {
			t.Fatal("late observation could not resolve uncertainty", done, err)
		}
		assignment.ID = "expiry"
		assignment.IdempotencyKey = "expiry"
		if _, err := s.SubmitCommand(ctx, assignment); err != nil {
			t.Fatal(err)
		}
		input.Sequence++
		input.CommandReports = nil
		if active, err = s.Sync(ctx, input); err != nil || len(active.Commands) != 1 {
			t.Fatal(active, err)
		}
		if expired, err := s.GetCommand(ctx, "scope", "device-a", "expiry", assignment.Deadline); err != nil || expired.State != "UNCERTAIN" {
			t.Fatal("delivered deadline claimed failure", expired, err)
		}
		input.Sequence++
		input.Now = assignment.Deadline
		if stopped, err := s.Sync(ctx, input); err != nil || len(stopped.Commands) != 0 {
			t.Fatal("expired command redispatched", stopped, err)
		}
		assignment.ID = "undispatched"
		assignment.IdempotencyKey = "undispatched"
		assignment.Now = input.Now
		assignment.Deadline = input.Now.Add(time.Hour)
		if _, err := s.SubmitCommand(ctx, assignment); err != nil {
			t.Fatal(err)
		}
		if expired, err := s.GetCommand(ctx, "scope", "device-a", "undispatched", assignment.Deadline); err != nil || expired.State != "FAILED" {
			t.Fatal(expired, err)
		}
		history, err := s.ListCommands(ctx, "scope", "device-a", "", 100, assignment.Deadline)
		if err != nil || len(history) != 4 {
			t.Fatal(history, err)
		}
	})
}
