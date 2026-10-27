// SPDX-License-Identifier: LGPL-3.0-only
package sqlstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

// Canonicalize a bounded target set without retaining or modifying caller-owned slices.
func fleetTargets(ids []string) ([]string, error) {
	if len(ids) > ports.MaximumFleetDevices {
		return nil, ports.ErrInvalidArgument
	}
	ids = slices.Clone(ids)
	sort.Strings(ids)
	for i, id := range ids {
		if id == "" || i > 0 && ids[i-1] == id {
			return nil, ports.ErrInvalidArgument
		}
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}

// Read one opaque group without treating an absent scope as global access.
func readGroup(ctx context.Context, q queryer, scope, id string) (ports.Group, error) {
	var result ports.Group
	var data []byte
	err := q.QueryRowContext(ctx, "SELECT data FROM fleet_groups WHERE scope_id=? AND id=?", scope, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ports.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}

// ReplaceGroup validates scope ownership and atomically compares membership revision; exact retries are harmless.
// Usage: group, err := store.ReplaceGroup(ctx, desiredGroup, expectedRevision).
func (s *Store) ReplaceGroup(ctx context.Context, group ports.Group, expected uint64) (ports.Group, error) {
	if group.ScopeID == "" || group.ID == "" || expected >= 1<<62 {
		return ports.Group{}, ports.ErrInvalidArgument
	}
	ids, err := fleetTargets(group.DeviceIDs)
	if err != nil {
		return ports.Group{}, err
	}
	group.DeviceIDs = ids
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ports.Group{}, err
	}
	defer tx.Rollback()
	previous, err := readGroup(ctx, tx, group.ScopeID, group.ID)
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return ports.Group{}, err
	}
	if previous.Revision == expected+1 && slices.Equal(previous.DeviceIDs, ids) {
		return previous, nil
	}
	if previous.Revision != expected {
		return ports.Group{}, ports.ErrConflict
	}
	for _, id := range ids {
		var state string
		err = tx.QueryRowContext(ctx, "SELECT state FROM devices WHERE scope_id=? AND id=?", group.ScopeID, id).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return ports.Group{}, ports.ErrNotFound
		}
		if err != nil {
			return ports.Group{}, err
		}
		if state == "REVOKED" {
			return ports.Group{}, ports.ErrRevoked
		}
	}
	group.Revision = expected + 1
	data, err := json.Marshal(group)
	if err != nil {
		return ports.Group{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO fleet_groups(scope_id,id,data) VALUES(?,?,?) ON CONFLICT(scope_id,id) DO UPDATE SET data=excluded.data", group.ScopeID, group.ID, data); err != nil {
		return ports.Group{}, err
	}
	if err = recordChange(ctx, tx, group.ScopeID, "group_changed", group.ID); err != nil {
		return ports.Group{}, err
	}
	return group, tx.Commit()
}

// GetGroup returns detached scope-local membership, including a deliberately empty group.
// Usage: group, err := store.GetGroup(ctx, scope, groupID).
func (s *Store) GetGroup(ctx context.Context, scope, id string) (ports.Group, error) {
	return readGroup(ctx, s.db, scope, id)
}

// ListGroups reads a bounded live group page ordered by opaque group ID.
// Usage: groups, err := store.ListGroups(ctx, scope, afterID, 20).
func (s *Store) ListGroups(ctx context.Context, scope, after string, limit int) ([]ports.Group, error) {
	if scope == "" || limit < 1 || limit > 101 {
		return nil, ports.ErrInvalidArgument
	}
	rows, err := s.db.QueryContext(ctx, "SELECT data FROM fleet_groups WHERE scope_id=? AND id>? ORDER BY id LIMIT ?", scope, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ports.Group{}
	for rows.Next() {
		var data []byte
		var group ports.Group
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &group); err != nil {
			return nil, err
		}
		result = append(result, group)
	}
	return result, rows.Err()
}

// Load a fan-out checkpoint in the caller's serialization boundary.
func readFleet(ctx context.Context, q queryer, scope, id string) (ports.FleetAssignment, error) {
	var result ports.FleetAssignment
	var data []byte
	err := q.QueryRowContext(ctx, "SELECT data FROM fleet_assignments WHERE scope_id=? AND id=?", scope, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ports.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}

// Commit the batch checkpoint and its scoped invalidation with all per-device writes in this transaction.
func saveFleet(ctx context.Context, tx *transaction, batch ports.FleetAssignment) error {
	data, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO fleet_assignments(scope_id,id,artifact_sha256,pending,data) VALUES(?,?,?,?,?) ON CONFLICT(scope_id,id) DO UPDATE SET pending=excluded.pending,data=excluded.data", batch.Request.ScopeID, batch.Request.ID, batch.Request.ArtifactSHA256, fleetPending(batch), data); err != nil {
		return err
	}
	return recordChange(ctx, tx, batch.Request.ScopeID, "fleet_changed", batch.Request.ID)
}

// Keep the artifact referenced until every frozen target was submitted or explicitly stopped.
func fleetPending(batch ports.FleetAssignment) int {
	if batch.Complete || batch.Cancelled {
		return 0
	}
	return 1
}

// CreateFleetAssignment freezes targets once, binding the original selector/window before any fan-out.
// Usage: batch, err := store.CreateFleetAssignment(ctx, request, now).
func (s *Store) CreateFleetAssignment(ctx context.Context, request ports.FleetRequest, now time.Time) (ports.FleetAssignment, error) {
	empty := ports.FleetAssignment{}
	ids, err := fleetTargets(request.DeviceIDs)
	if err != nil {
		return empty, err
	}
	request.DeviceIDs = ids
	if request.ScopeID == "" || request.ID == "" || request.ArtifactSHA256 == "" || (request.GroupID == "") == (len(ids) == 0) || request.NotBefore.UnixMilli() <= 0 || !request.NotBefore.Before(request.Deadline) || request.Deadline.Sub(request.NotBefore) > 7*24*time.Hour {
		return empty, ports.ErrInvalidArgument
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	previous, err := readFleet(ctx, tx, request.ScopeID, request.ID)
	if err == nil {
		before, _ := json.Marshal(previous.Request)
		after, _ := json.Marshal(request)
		if !bytes.Equal(before, after) {
			return empty, ports.ErrConflict
		}
		return previous, nil
	}
	if !errors.Is(err, ports.ErrNotFound) {
		return empty, err
	}
	if !now.Before(request.Deadline) {
		return empty, ports.ErrInvalidArgument
	}
	if _, err = artifact(ctx, tx, request.ScopeID, request.ArtifactSHA256); err != nil {
		return empty, err
	}
	batch := ports.FleetAssignment{Request: request, Items: []ports.FleetItem{}}
	if request.GroupID != "" {
		group, err := readGroup(ctx, tx, request.ScopeID, request.GroupID)
		if err != nil {
			return empty, err
		}
		ids, batch.GroupRevision = group.DeviceIDs, group.Revision
	}
	for _, id := range ids {
		batch.Items = append(batch.Items, ports.FleetItem{DeviceID: id, State: "PENDING"})
	}
	batch.Complete = len(batch.Items) == 0
	if err = saveFleet(ctx, tx, batch); err != nil {
		return empty, err
	}
	return batch, tx.Commit()
}

// GetFleetAssignment returns detached frozen targets and submission progress in one authorized scope.
// Usage: batch, err := store.GetFleetAssignment(ctx, scope, batchID).
func (s *Store) GetFleetAssignment(ctx context.Context, scope, id string) (ports.FleetAssignment, error) {
	return readFleet(ctx, s.db, scope, id)
}

// ListFleetAssignments lets a restarted host find incomplete batches through a bounded live page.
// Usage: batches, err := store.ListFleetAssignments(ctx, scope, afterID, 20).
func (s *Store) ListFleetAssignments(ctx context.Context, scope, after string, limit int) ([]ports.FleetAssignment, error) {
	if scope == "" || limit < 1 || limit > 101 {
		return nil, ports.ErrInvalidArgument
	}
	rows, err := s.db.QueryContext(ctx, "SELECT data FROM fleet_assignments WHERE scope_id=? AND id>? ORDER BY id LIMIT ?", scope, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ports.FleetAssignment{}
	for rows.Next() {
		var data []byte
		var batch ports.FleetAssignment
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &batch); err != nil {
			return nil, err
		}
		result = append(result, batch)
	}
	return result, rows.Err()
}

// Derive a collision-resistant device request key from its frozen batch identity; no user key namespace is reserved.
func fleetKey(scope, batch, device string) string {
	data, _ := json.Marshal([]string{"kylok.fleet", scope, batch, device})
	sum := sha256.Sum256(data)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// AdvanceFleetAssignment commits a bounded submission chunk and its progress atomically; infrastructure errors roll it back.
// Usage: batch, err := store.AdvanceFleetAssignment(ctx, scope, batchID, now, 20).
func (s *Store) AdvanceFleetAssignment(ctx context.Context, scope, id string, now time.Time, limit int) (ports.FleetAssignment, error) {
	empty := ports.FleetAssignment{}
	if limit < 1 || limit > ports.MaximumFleetDevices {
		return empty, ports.ErrInvalidArgument
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	batch, err := readFleet(ctx, tx, scope, id)
	if err != nil {
		return empty, err
	}
	if batch.Complete || batch.Cancelled {
		return batch, nil
	}
	count := 0
	for i := range batch.Items {
		item := &batch.Items[i]
		if item.State != "PENDING" || count >= limit {
			continue
		}
		count++
		if !now.Before(batch.Request.Deadline) {
			item.State, item.ErrorCode = "FAILED", "DEADLINE_EXCEEDED"
			continue
		}
		// The same device gets the same key across restarts, concurrent workers and lost responses.
		key := fleetKey(scope, id, item.DeviceID)
		op, err := assign(ctx, tx, ports.Assignment{ScopeID: scope, DeviceID: item.DeviceID, ArtifactSHA256: batch.Request.ArtifactSHA256, IdempotencyKey: "fleet-" + key, OperationID: key, Now: now, NotBefore: batch.Request.NotBefore, Deadline: batch.Request.Deadline})
		switch {
		case err == nil:
			item.State, item.OperationID = "ASSIGNED", op.ID
		case errors.Is(err, ports.ErrNotFound):
			item.State, item.ErrorCode = "FAILED", "NOT_FOUND"
		case errors.Is(err, ports.ErrRevoked):
			item.State, item.ErrorCode = "FAILED", "DEVICE_REVOKED"
		case errors.Is(err, ports.ErrConflict):
			item.State, item.ErrorCode = "FAILED", "CONFLICT"
		case errors.Is(err, ports.ErrInvalidArgument):
			item.State, item.ErrorCode = "FAILED", "INVALID_ARGUMENT"
		default:
			return empty, err
		}
	}
	batch.Complete = true
	for _, item := range batch.Items {
		if item.State == "PENDING" {
			batch.Complete = false
		}
	}
	if err = saveFleet(ctx, tx, batch); err != nil {
		return empty, err
	}
	return batch, tx.Commit()
}

// Stop active dispatch while retaining immutable desired intent and any already observed completion.
func cancelOperation(ctx context.Context, tx *transaction, scope, device, id string, now time.Time) (ports.Operation, error) {
	var found string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM devices WHERE scope_id=? AND id=?", scope, device).Scan(&found); errors.Is(err, sql.ErrNoRows) {
		return ports.Operation{}, ports.ErrNotFound
	} else if err != nil {
		return ports.Operation{}, err
	}
	if err := expire(ctx, tx, device, now); err != nil {
		return ports.Operation{}, err
	}
	op, err := operation(ctx, tx, id)
	if err != nil {
		return ports.Operation{}, err
	}
	if op.DeviceID != device {
		return ports.Operation{}, ports.ErrNotFound
	}
	if op.State == "QUEUED" || op.State == "RUNNING" || op.State == "WAITING_USER" {
		op.State, op.ErrorCode = "CANCELLED", "CANCELLED_BY_HOST"
		op.ErrorMessage = "Future dispatch stopped; a delivered installation may still complete"
		if err = saveOperation(ctx, tx, op); err != nil {
			return ports.Operation{}, err
		}
	}
	return op, nil
}

// CancelOperation stops dispatch and future downloads; it cannot revoke a platform effect already delivered.
// Usage: operation, err := store.CancelOperation(ctx, scope, deviceID, operationID, now).
func (s *Store) CancelOperation(ctx context.Context, scope, device, id string, now time.Time) (ports.Operation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ports.Operation{}, err
	}
	defer tx.Rollback()
	op, err := cancelOperation(ctx, tx, scope, device, id, now)
	if err != nil {
		return ports.Operation{}, err
	}
	return op, tx.Commit()
}

// CancelFleetAssignment serializes with fan-out, stopping unsubmitted targets and active submitted operations only.
// Usage: batch, err := store.CancelFleetAssignment(ctx, scope, batchID, now).
func (s *Store) CancelFleetAssignment(ctx context.Context, scope, id string, now time.Time) (ports.FleetAssignment, error) {
	empty := ports.FleetAssignment{}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	batch, err := readFleet(ctx, tx, scope, id)
	if err != nil {
		return empty, err
	}
	if batch.Cancelled {
		return batch, nil
	}
	for i := range batch.Items {
		item := &batch.Items[i]
		if item.State == "PENDING" {
			item.State = "CANCELLED"
		}
		if item.State == "ASSIGNED" {
			if _, err = cancelOperation(ctx, tx, scope, item.DeviceID, item.OperationID, now); err != nil {
				return empty, err
			}
		}
	}
	batch.Cancelled, batch.Complete = true, true
	if err = saveFleet(ctx, tx, batch); err != nil {
		return empty, err
	}
	return batch, tx.Commit()
}
