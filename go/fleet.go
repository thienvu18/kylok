// SPDX-License-Identifier: LGPL-3.0-only
package kylok

import (
	"context"
	"slices"
	"sort"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

// Group is an opaque scoped membership set, capped at 100 devices; it carries no business metadata.
type Group = ports.Group

// FleetRequest selects one group or an explicit device set with an absolute deployment window.
// Request ID is the scope-local retry key; membership freezes on creation.
type FleetRequest = ports.FleetRequest

// FleetItem describes submission progress; use OperationID to read the device's observed execution result.
type FleetItem = ports.FleetItem

// FleetAssignment is a detached durable fan-out checkpoint, not a claim that devices applied its APK.
type FleetAssignment = ports.FleetAssignment

// GroupPage contains a live ascending-ID membership page and a scope-bound continuation.
type GroupPage struct {
	Items      []Group
	NextCursor string
}

// FleetPage contains durable assignment checkpoints; hosts scan these to resume incomplete work after restart.
type FleetPage struct {
	Items      []FleetAssignment
	NextCursor string
}

// Validate and canonicalize the target set without mutating a host-owned slice.
func validFleetTargets(ids []string) ([]string, error) {
	if len(ids) > ports.MaximumFleetDevices {
		return nil, ErrInvalidArgument
	}
	result := slices.Clone(ids)
	sort.Strings(result)
	for i, id := range result {
		if !devicePattern.MatchString(id) || i > 0 && result[i-1] == id {
			return nil, ErrInvalidArgument
		}
	}
	return result, nil
}

// ReplaceGroup replaces the complete membership using optimistic concurrency (zero creates).
// An identical retry with the same expected revision returns the committed set; foreign/revoked members fail.
// Existing fleet assignments keep their frozen targets. An empty set clears membership.
// Usage: group, err := sdk.ReplaceGroup(ctx, scope, "lobby", []string{deviceID}, 0).
func (s *SDK) ReplaceGroup(ctx context.Context, scope, id string, devices []string, expectedRevision uint64) (Group, error) {
	if err := s.checkOpen(ctx); err != nil {
		return Group{}, err
	}
	if !idPattern.MatchString(scope) || !idPattern.MatchString(id) || expectedRevision >= 1<<62 {
		return Group{}, ErrInvalidArgument
	}
	ids, err := validFleetTargets(devices)
	if err != nil {
		return Group{}, err
	}
	return s.store.ReplaceGroup(ctx, Group{ScopeID: scope, ID: id, DeviceIDs: ids}, expectedRevision)
}

// GetGroup reads membership in one authorized scope; foreign and absent IDs both return ErrNotFound.
// Usage: group, err := sdk.GetGroup(ctx, scope, "lobby").
func (s *SDK) GetGroup(ctx context.Context, scope, id string) (Group, error) {
	if err := s.checkOpen(ctx); err != nil {
		return Group{}, err
	}
	if !idPattern.MatchString(scope) || !idPattern.MatchString(id) {
		return Group{}, ErrInvalidArgument
	}
	return s.store.GetGroup(ctx, scope, id)
}

// QueryGroups reads a bounded live membership page. Hosts authorize the scope on every call.
// Usage: page, err := sdk.QueryGroups(ctx, scope, PageRequest{Limit:20}).
func (s *SDK) QueryGroups(ctx context.Context, scope string, page PageRequest) (GroupPage, error) {
	if err := s.checkOpen(ctx); err != nil {
		return GroupPage{}, err
	}
	if !idPattern.MatchString(scope) {
		return GroupPage{}, ErrInvalidArgument
	}
	binding := cursorBinding("groups", scope, "", "", "")
	after, limit, err := readKeysetPage(page, binding, idPattern)
	if err != nil {
		return GroupPage{}, err
	}
	items, err := s.store.ListGroups(ctx, scope, after, limit+1)
	if err != nil {
		return GroupPage{}, err
	}
	result := GroupPage{Items: items}
	if len(items) > limit {
		result.NextCursor = nextPage(binding, items[limit-1].ID)
		result.Items = items[:limit]
	}
	return result, nil
}

// ScheduleApplication creates one durable install with an absolute [notBefore, deadline) window (at most seven days).
// The same key binds artifact and window exactly. The latest committed assignment wins per package;
// cancellation/supersession cannot undo an already delivered platform effect. Expired retries still read the original.
// Usage: op, err := sdk.ScheduleApplication(ctx, scope, deviceID, digest, "nightly", start, start.Add(time.Hour)).
func (s *SDK) ScheduleApplication(ctx context.Context, scope, device, digest, key string, notBefore, deadline time.Time) (Operation, error) {
	if err := s.checkOpen(ctx); err != nil {
		return Operation{}, err
	}
	notBefore, deadline = notBefore.UTC().Truncate(time.Millisecond), deadline.UTC().Truncate(time.Millisecond)
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(device) || !digestPattern.MatchString(digest) || !idPattern.MatchString(key) || notBefore.UnixMilli() <= 0 || !notBefore.Before(deadline) || deadline.Sub(notBefore) > 7*24*time.Hour {
		return Operation{}, ErrInvalidArgument
	}
	id, err := secret()
	if err != nil {
		return Operation{}, err
	}
	op, err := s.store.Assign(ctx, ports.Assignment{ScopeID: scope, DeviceID: device, ArtifactSHA256: digest, IdempotencyKey: key, OperationID: id, Now: s.clock().UTC(), NotBefore: notBefore, Deadline: deadline})
	return publicOperation(op), err
}

// CreateFleetAssignment freezes up to 100 targets without submitting device work. Set GroupID or DeviceIDs, never both.
// Reusing ID requires the same selector, APK and absolute window. Group edits do not expand retries.
// Hosts drive AdvanceFleetAssignment and retain a scan/checkpoint for their authorized scopes.
// Usage: batch, err := sdk.CreateFleetAssignment(ctx, FleetRequest{ScopeID:scope, ID:"nightly", GroupID:"lobby", ArtifactSHA256:digest, NotBefore:start, Deadline:start.Add(time.Hour)}).
func (s *SDK) CreateFleetAssignment(ctx context.Context, request FleetRequest) (FleetAssignment, error) {
	if err := s.checkOpen(ctx); err != nil {
		return FleetAssignment{}, err
	}
	if !idPattern.MatchString(request.ScopeID) || !idPattern.MatchString(request.ID) || !digestPattern.MatchString(request.ArtifactSHA256) || request.GroupID != "" && !idPattern.MatchString(request.GroupID) {
		return FleetAssignment{}, ErrInvalidArgument
	}
	ids, err := validFleetTargets(request.DeviceIDs)
	if err != nil {
		return FleetAssignment{}, err
	}
	request.DeviceIDs = ids
	request.NotBefore = request.NotBefore.UTC().Truncate(time.Millisecond)
	request.Deadline = request.Deadline.UTC().Truncate(time.Millisecond)
	return s.store.CreateFleetAssignment(ctx, request, s.clock().UTC())
}

// GetFleetAssignment reads frozen targets and per-device submission results, without advancing work.
// Usage: batch, err := sdk.GetFleetAssignment(ctx, scope, "nightly").
func (s *SDK) GetFleetAssignment(ctx context.Context, scope, id string) (FleetAssignment, error) {
	if err := s.checkOpen(ctx); err != nil {
		return FleetAssignment{}, err
	}
	if !idPattern.MatchString(scope) || !idPattern.MatchString(id) {
		return FleetAssignment{}, ErrInvalidArgument
	}
	return s.store.GetFleetAssignment(ctx, scope, id)
}

// QueryFleetAssignments scans durable checkpoints so host workers can resume pending fan-out after restart.
// Usage: page, err := sdk.QueryFleetAssignments(ctx, scope, PageRequest{Limit:20}).
func (s *SDK) QueryFleetAssignments(ctx context.Context, scope string, page PageRequest) (FleetPage, error) {
	if err := s.checkOpen(ctx); err != nil {
		return FleetPage{}, err
	}
	if !idPattern.MatchString(scope) {
		return FleetPage{}, ErrInvalidArgument
	}
	binding := cursorBinding("fleet", scope, "", "", "")
	after, limit, err := readKeysetPage(page, binding, idPattern)
	if err != nil {
		return FleetPage{}, err
	}
	items, err := s.store.ListFleetAssignments(ctx, scope, after, limit+1)
	if err != nil {
		return FleetPage{}, err
	}
	result := FleetPage{Items: items}
	if len(items) > limit {
		result.NextCursor = nextPage(binding, items[limit-1].Request.ID)
		result.Items = items[:limit]
	}
	return result, nil
}

// AdvanceFleetAssignment atomically submits at most limit pending targets (1–100); permanent per-device errors are retained.
// Infrastructure/context failures roll back the chunk. Concurrent workers and lost responses cannot create duplicate work.
// Complete means fan-out finished; read each OperationID for installation/compliance results.
// Usage: batch, err := sdk.AdvanceFleetAssignment(ctx, scope, "nightly", 20).
func (s *SDK) AdvanceFleetAssignment(ctx context.Context, scope, id string, limit int) (FleetAssignment, error) {
	if err := s.checkOpen(ctx); err != nil {
		return FleetAssignment{}, err
	}
	if !idPattern.MatchString(scope) || !idPattern.MatchString(id) || limit < 1 || limit > ports.MaximumFleetDevices {
		return FleetAssignment{}, ErrInvalidArgument
	}
	return s.store.AdvanceFleetAssignment(ctx, scope, id, s.clock().UTC(), limit)
}

// CancelFleetAssignment cancels pending targets and stops future dispatch for active submitted operations.
// Completed/superseded operations are preserved; already delivered Android effects may still complete.
// Usage: batch, err := sdk.CancelFleetAssignment(ctx, scope, "nightly").
func (s *SDK) CancelFleetAssignment(ctx context.Context, scope, id string) (FleetAssignment, error) {
	if err := s.checkOpen(ctx); err != nil {
		return FleetAssignment{}, err
	}
	if !idPattern.MatchString(scope) || !idPattern.MatchString(id) {
		return FleetAssignment{}, ErrInvalidArgument
	}
	return s.store.CancelFleetAssignment(ctx, scope, id, s.clock().UTC())
}

// CancelOperation stops future dispatch/download and signals the agent to abandon uncommitted local work.
// CANCELLED does not prove that a delivered installation stopped. Desired intent stays recorded;
// no implicit uninstall/rollback or drift retry follows. Reassign with a new key to resume.
// Usage: op, err := sdk.CancelOperation(ctx, scope, deviceID, operationID).
func (s *SDK) CancelOperation(ctx context.Context, scope, device, id string) (Operation, error) {
	if err := s.checkOpen(ctx); err != nil {
		return Operation{}, err
	}
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(device) || !devicePattern.MatchString(id) {
		return Operation{}, ErrInvalidArgument
	}
	op, err := s.store.CancelOperation(ctx, scope, device, id, s.clock().UTC())
	return publicOperation(op), err
}
