// SPDX-License-Identifier: LGPL-3.0-only
package kylok_test

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thienvu18/kylok/go"
	"github.com/thienvu18/kylok/go/internal/testapk"
)

func TestFleetPublicPagesRestartAndScheduling(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	s, options := setup(t, func() time.Time { return now })
	client, server := start(t, s)
	id, credential := enrolled(t, s, client, "scope")
	foreign, _ := enrolled(t, s, client, "foreign")
	artifact, err := s.RegisterApplication(ctx, "scope", testapk.Make(t, testapk.Manifest))
	if err != nil {
		t.Fatal(err)
	}
	group, err := s.ReplaceGroup(ctx, "scope", "a", []string{id}, 0)
	if err != nil || group.Revision != 1 {
		t.Fatal(group, err)
	}
	if _, err = s.ReplaceGroup(ctx, "scope", "b", nil, 0); err != nil {
		t.Fatal(err)
	}
	page, err := s.QueryGroups(ctx, "scope", kylok.PageRequest{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "a" || page.NextCursor == "" {
		t.Fatal(page, err)
	}
	if next, err := s.QueryGroups(ctx, "scope", kylok.PageRequest{Limit: 1, Cursor: page.NextCursor}); err != nil || len(next.Items) != 1 || next.Items[0].ID != "b" || next.NextCursor != "" {
		t.Fatal(next, err)
	}
	if _, err := s.QueryGroups(ctx, "foreign", kylok.PageRequest{Cursor: page.NextCursor}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("foreign cursor", err)
	}
	request := kylok.FleetRequest{ScopeID: "scope", ID: "batch-a", GroupID: "a", ArtifactSHA256: artifact.SHA256, NotBefore: now.Add(time.Hour), Deadline: now.Add(2 * time.Hour)}
	if _, err = s.CreateFleetAssignment(ctx, request); err != nil {
		t.Fatal(err)
	}
	request.ID = "batch-b"
	if _, err = s.CreateFleetAssignment(ctx, request); err != nil {
		t.Fatal(err)
	}
	batches, err := s.QueryFleetAssignments(ctx, "scope", kylok.PageRequest{Limit: 1})
	if err != nil || len(batches.Items) != 1 || batches.NextCursor == "" {
		t.Fatal(batches, err)
	}
	if next, err := s.QueryFleetAssignments(ctx, "scope", kylok.PageRequest{Limit: 1, Cursor: batches.NextCursor}); err != nil || next.Items[0].Request.ID != "batch-b" || next.NextCursor != "" {
		t.Fatal(next, err)
	}
	if _, err := s.QueryFleetAssignments(ctx, "scope", kylok.PageRequest{Cursor: page.NextCursor}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("cross resource cursor", err)
	}
	if _, err = s.ReplaceGroup(ctx, "scope", "bad", []string{foreign}, 0); !errors.Is(err, kylok.ErrNotFound) {
		t.Fatal("foreign target", err)
	}
	if _, err = s.ReplaceGroup(ctx, "scope", "bad", []string{id, id}, 0); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("duplicate target", err)
	}
	for _, bad := range []kylok.FleetRequest{
		{ScopeID: "scope", ID: "both", GroupID: "a", DeviceIDs: []string{id}, ArtifactSHA256: artifact.SHA256, NotBefore: now, Deadline: now.Add(time.Hour)},
		{ScopeID: "scope", ID: "neither", ArtifactSHA256: artifact.SHA256, NotBefore: now, Deadline: now.Add(time.Hour)},
		{ScopeID: "scope", ID: "late", GroupID: "a", ArtifactSHA256: artifact.SHA256, NotBefore: now.Add(-time.Hour), Deadline: now},
	} {
		if _, err = s.CreateFleetAssignment(ctx, bad); !errors.Is(err, kylok.ErrInvalidArgument) {
			t.Fatal(bad.ID, err)
		}
	}
	server.Close()
	s.Close()
	restarted, err := kylok.New(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	client, _ = start(t, restarted)
	batch, err := restarted.AdvanceFleetAssignment(ctx, "scope", "batch-a", 20)
	if err != nil || !batch.Complete || batch.Items[0].State != "ASSIGNED" {
		t.Fatal(batch, err)
	}
	if before := exchange(t, client, id, credential, 1, 0); len(before.Operations) != 0 || before.Compliance[0].Reason != "SCHEDULED" {
		t.Fatal(before)
	}
	opID := batch.Items[0].OperationID
	// A direct assignment committed after fan-out supersedes it. Retrying the batch must not restore older intent.
	direct, err := restarted.AssignApplication(ctx, "scope", id, artifact.SHA256, "direct", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.AdvanceFleetAssignment(ctx, "scope", "batch-a", 20); err != nil {
		t.Fatal(err)
	}
	if old, err := restarted.GetOperation(ctx, "scope", id, opID); err != nil || old.State != "SUPERSEDED" {
		t.Fatal(old, err)
	}
	if active := exchange(t, client, id, credential, 2, 1); len(active.Operations) != 1 || active.Operations[0].Id != direct.ID {
		t.Fatal(active)
	}
	var wait sync.WaitGroup
	for i := 0; i < 4; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := restarted.CancelOperation(ctx, "scope", id, direct.ID); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	if cancelled, err := restarted.QueryOperations(ctx, "scope", id, kylok.OperationFilter{State: "CANCELLED"}, kylok.PageRequest{}); err != nil || len(cancelled.Items) != 1 {
		t.Fatal(cancelled, err)
	}
	scheduled, err := restarted.ScheduleApplication(ctx, "scope", id, artifact.SHA256, "absolute", now.Add(2*time.Hour), now.Add(3*time.Hour))
	if err != nil || !scheduled.NotBefore.Equal(now.Add(2*time.Hour)) {
		t.Fatal(scheduled, err)
	}
	if _, err = restarted.ScheduleApplication(ctx, "scope", id, artifact.SHA256, "absolute", now.Add(2*time.Hour), now.Add(4*time.Hour)); !errors.Is(err, kylok.ErrConflict) {
		t.Fatal("window retry conflict", err)
	}
	if _, err = restarted.ScheduleApplication(ctx, "scope", id, artifact.SHA256, "absolute", time.Time{}, now.Add(time.Hour)); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("missing start", err)
	}
	if _, err = restarted.CancelOperation(ctx, "scope", id, strings.Repeat("x", 43)); !errors.Is(err, kylok.ErrNotFound) {
		t.Fatal(err)
	}
	now = now.Add(4 * time.Hour)
	if original, err := restarted.ScheduleApplication(ctx, "scope", id, artifact.SHA256, "absolute", scheduled.NotBefore, scheduled.Deadline); err != nil || original.ID != scheduled.ID {
		t.Fatal("expired retry", original, err)
	}
}

func TestCommandPublicAPIAndSanitizedProjection(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	s, _ := setup(t, func() time.Time { return now })
	client, _ := start(t, s)
	id, credential := enrolled(t, s, client, "scope")
	if _, err := s.SubmitCommand(ctx, "scope", id, kylok.CommandLock, "lock", time.Hour); !errors.Is(err, kylok.ErrUnsupported) {
		t.Fatal(err)
	}
	request := syncRequest(id, credential)
	request.Msg.Sequence = 1
	request.Msg.Inventory.Capabilities = []string{"command.lock", "command.reboot"}
	if _, err := client.Sync(ctx, request); err != nil {
		t.Fatal(err)
	}
	command, err := s.SubmitCommand(ctx, "scope", id, kylok.CommandLock, "lock", time.Hour)
	if err != nil || command.State != "QUEUED" {
		t.Fatal(command, err)
	}
	second, err := s.SubmitCommand(ctx, "scope", id, kylok.CommandReboot, "reboot", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	request.Msg.Sequence = 2
	response, err := client.Sync(ctx, request)
	if err != nil || len(response.Msg.Commands) != 1 || response.Msg.Commands[0].Id != command.ID {
		t.Fatal(response, err)
	}
	state, err := s.GetDeviceState(ctx, "scope", id)
	if err != nil || len(state.Commands) != 2 {
		t.Fatal(state, err)
	}
	data, err := json.Marshal(state)
	if err != nil || strings.Contains(string(data), response.Msg.Commands[0].DispatchToken) || strings.Contains(string(data), "dispatch_token") {
		t.Fatal("dispatch credential exposed", err)
	}
	first, err := s.QueryCommands(ctx, "scope", id, kylok.PageRequest{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	next, err := s.QueryCommands(ctx, "scope", id, kylok.PageRequest{Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == first.Items[0].ID || next.NextCursor != "" {
		t.Fatal(next, err)
	}
	if stopped, err := s.CancelCommand(ctx, "scope", id, command.ID); err != nil || stopped.State != "UNCERTAIN" {
		t.Fatal(stopped, err)
	}
	if stopped, err := s.CancelCommand(ctx, "scope", id, second.ID); err != nil || stopped.State != "CANCELLED" {
		t.Fatal(stopped, err)
	}
	if _, err := s.GetCommand(ctx, "other", id, command.ID); !errors.Is(err, kylok.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.SubmitCommand(ctx, "scope", id, kylok.CommandKind("WIPE"), "bad", time.Hour); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal(err)
	}
}

func TestFleetAndGroupOperations(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	s, _ := setup(t, func() time.Time { return now })
	client, _ := start(t, s)
	devID, _ := enrolled(t, s, client, "scope")
	artifact, err := s.RegisterApplication(ctx, "scope", testapk.Make(t, testapk.Manifest))
	if err != nil {
		t.Fatal(err)
	}

	// Replace and GetGroup
	group, err := s.ReplaceGroup(ctx, "scope", "engineers", []string{devID}, 0)
	if err != nil {
		t.Fatal(err)
	}
	gotGroup, err := s.GetGroup(ctx, "scope", "engineers")
	if err != nil || gotGroup.ID != group.ID || gotGroup.Revision != group.Revision {
		t.Fatalf("unexpected group: %+v, err: %v", gotGroup, err)
	}

	// GetGroup validation & not found
	if _, err := s.GetGroup(ctx, "invalid scope!", "engineers"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid scope")
	}
	if _, err := s.GetGroup(ctx, "scope", "invalid group!"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid group ID")
	}
	if _, err := s.GetGroup(ctx, "other", "engineers"); !errors.Is(err, kylok.ErrNotFound) {
		t.Fatal("expected ErrNotFound for wrong scope")
	}

	// Create, Get and Cancel FleetAssignment
	req := kylok.FleetRequest{
		ScopeID:        "scope",
		ID:             "batch-test",
		GroupID:        "engineers",
		ArtifactSHA256: artifact.SHA256,
		NotBefore:      now,
		Deadline:       now.Add(time.Hour),
	}
	batch, err := s.CreateFleetAssignment(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	gotBatch, err := s.GetFleetAssignment(ctx, "scope", "batch-test")
	if err != nil || gotBatch.Request.ID != batch.Request.ID {
		t.Fatalf("unexpected batch: %+v, err: %v", gotBatch, err)
	}

	// GetFleetAssignment validation & not found
	if _, err := s.GetFleetAssignment(ctx, "invalid scope!", "batch-test"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid scope")
	}
	if _, err := s.GetFleetAssignment(ctx, "scope", "invalid batch!"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid batch ID")
	}
	if _, err := s.GetFleetAssignment(ctx, "other", "batch-test"); !errors.Is(err, kylok.ErrNotFound) {
		t.Fatal("expected ErrNotFound for wrong scope")
	}

	// Advance with invalid limit
	if _, err := s.AdvanceFleetAssignment(ctx, "scope", "batch-test", 0); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit 0")
	}
	if _, err := s.AdvanceFleetAssignment(ctx, "scope", "batch-test", 101); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit > 100")
	}

	// CancelFleetAssignment
	cancelledBatch, err := s.CancelFleetAssignment(ctx, "scope", "batch-test")
	if err != nil || !cancelledBatch.Cancelled {
		t.Fatalf("unexpected cancel result: %+v, err: %v", cancelledBatch, err)
	}
	if _, err := s.CancelFleetAssignment(ctx, "invalid scope", "batch-test"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid scope in CancelFleetAssignment")
	}
	if _, err := s.CancelFleetAssignment(ctx, "scope", "invalid batch!"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid ID in CancelFleetAssignment")
	}

	// ScheduleApplication tests
	startWindow := now.Add(10 * time.Minute)
	op, err := s.ScheduleApplication(ctx, "scope", devID, artifact.SHA256, "sched-op-1", startWindow, startWindow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if op.State != "QUEUED" {
		t.Fatalf("unexpected op state: %s", op.State)
	}

	// CancelOperation
	cancelledOp, err := s.CancelOperation(ctx, "scope", devID, op.ID)
	if err != nil || cancelledOp.State != "CANCELLED" {
		t.Fatalf("unexpected cancel op result: %+v, err: %v", cancelledOp, err)
	}
	if _, err := s.CancelOperation(ctx, "invalid scope!", devID, op.ID); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid scope")
	}
	if _, err := s.CancelOperation(ctx, "scope", "invalid-dev", op.ID); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid dev")
	}
	if _, err := s.CancelOperation(ctx, "scope", devID, "invalid-op"); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid op ID")
	}

	// ScheduleApplication invalid arguments
	if _, err := s.ScheduleApplication(ctx, "invalid scope", devID, artifact.SHA256, "k", startWindow, startWindow.Add(time.Hour)); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := s.ScheduleApplication(ctx, "scope", "invalid dev", artifact.SHA256, "k", startWindow, startWindow.Add(time.Hour)); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := s.ScheduleApplication(ctx, "scope", devID, "invalid digest", "k", startWindow, startWindow.Add(time.Hour)); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := s.ScheduleApplication(ctx, "scope", devID, artifact.SHA256, "invalid key!", startWindow, startWindow.Add(time.Hour)); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := s.ScheduleApplication(ctx, "scope", devID, artifact.SHA256, "k", startWindow.Add(time.Hour), startWindow); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument when start after deadline")
	}
	if _, err := s.ScheduleApplication(ctx, "scope", devID, artifact.SHA256, "k", startWindow, startWindow.Add(8*24*time.Hour)); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument when window > 7 days")
	}

	// CreateFleetAssignment invalid requests
	if _, err := s.CreateFleetAssignment(ctx, kylok.FleetRequest{ScopeID: "invalid scope"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := s.CreateFleetAssignment(ctx, kylok.FleetRequest{ScopeID: "scope", ID: "invalid id!"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := s.CreateFleetAssignment(ctx, kylok.FleetRequest{ScopeID: "scope", ID: "ok", ArtifactSHA256: "bad-digest"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := s.CreateFleetAssignment(ctx, kylok.FleetRequest{ScopeID: "scope", ID: "ok", ArtifactSHA256: artifact.SHA256, GroupID: "bad group!"}); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument")
	}
	if _, err := s.CreateFleetAssignment(ctx, kylok.FleetRequest{ScopeID: "scope", ID: "ok", ArtifactSHA256: artifact.SHA256, GroupID: "grp", DeviceIDs: []string{devID}}); err == nil {
		t.Fatal("expected error when both GroupID and DeviceIDs set")
	}
	if _, err := s.CreateFleetAssignment(ctx, kylok.FleetRequest{ScopeID: "scope", ID: "ok", ArtifactSHA256: artifact.SHA256}); err == nil {
		t.Fatal("expected error when neither GroupID nor DeviceIDs set")
	}
}
