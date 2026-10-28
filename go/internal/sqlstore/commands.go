// SPDX-License-Identifier: LGPL-3.0-only
package sqlstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

// Bound outstanding one-shot actions independently of installation operation limits.
const maximumActiveCommands = 16

// Load the original command checkpoint without exposing another device's existence to public callers.
func readCommand(ctx context.Context, q queryer, id string) (ports.Command, error) {
	var result ports.Command
	var data []byte
	err := q.QueryRowContext(ctx, "SELECT data FROM commands WHERE id=?", id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ports.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}

// Keep the indexed lifecycle and durable result aligned in the same device-change transaction.
func saveCommand(ctx context.Context, tx *transaction, command ports.Command) error {
	data, err := json.Marshal(command)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE commands SET state=?,data=? WHERE id=?", command.State, data, command.ID); err != nil {
		return err
	}
	return deviceChanged(ctx, tx, command.DeviceID)
}

// Read active commands in host submission order; Android receives at most one one-shot action per sync.
func activeCommands(ctx context.Context, tx *transaction, device string) ([]ports.Command, error) {
	rows, err := tx.QueryContext(ctx, "SELECT data FROM commands WHERE device_id=? AND state IN ('QUEUED','DISPATCHED') ORDER BY ordinal", device)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ports.Command{}
	for rows.Next() {
		var data []byte
		var command ports.Command
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &command); err != nil {
			return nil, err
		}
		result = append(result, command)
	}
	return result, rows.Err()
}

// Expiry cannot prove whether a dispatched destructive effect happened; never requeue such work.
func expireCommands(ctx context.Context, tx *transaction, device string, now time.Time) error {
	commands, err := activeCommands(ctx, tx, device)
	if err != nil {
		return err
	}
	for _, command := range commands {
		if now.Before(command.Deadline) {
			continue
		}
		command.ErrorCode = "DEADLINE_EXCEEDED"
		if command.State == "DISPATCHED" {
			command.State = "UNCERTAIN"
		} else {
			command.State = "FAILED"
		}
		if err = saveCommand(ctx, tx, command); err != nil {
			return err
		}
	}
	return nil
}

// Resolve scoped ownership before observing command history or applying cancellation.
func commandDevice(ctx context.Context, tx *transaction, scope, device string) (ports.Device, error) {
	value, _, err := scanDevice(tx.QueryRowContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE scope_id=? AND id=?", scope, device))
	return value, err
}

// SubmitCommand atomically binds a host retry key and capability-gates a bounded one-shot action queue.
// Usage: command, err := store.SubmitCommand(ctx, assignment).
func (s *Store) SubmitCommand(ctx context.Context, a ports.CommandAssignment) (ports.Command, error) {
	empty := ports.Command{}
	if a.ScopeID == "" || a.DeviceID == "" || a.ID == "" || a.IdempotencyKey == "" || a.Kind != "REBOOT" && a.Kind != "LOCK" {
		return empty, ports.ErrInvalidArgument
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	device, err := commandDevice(ctx, tx, a.ScopeID, a.DeviceID)
	if err != nil {
		return empty, err
	}
	if device.State == "REVOKED" {
		return empty, ports.ErrRevoked
	}
	var id string
	err = tx.QueryRowContext(ctx, "SELECT id FROM commands WHERE device_id=? AND idempotency_key=?", a.DeviceID, a.IdempotencyKey).Scan(&id)
	if err == nil {
		command, err := readCommand(ctx, tx, id)
		if err != nil {
			return empty, err
		}
		if command.Kind != a.Kind {
			return empty, ports.ErrConflict
		}
		return command, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	capability := "command.lock"
	if a.Kind == "REBOOT" {
		capability = "command.reboot"
	}
	if !device.Inventory.DeviceOwner || !slices.Contains(device.Inventory.Capabilities, capability) {
		return empty, ports.ErrUnsupported
	}
	if !a.Now.Before(a.Deadline) || a.Deadline.Sub(a.Now) > 7*24*time.Hour {
		return empty, ports.ErrInvalidArgument
	}
	if err = expireCommands(ctx, tx, a.DeviceID, a.Now); err != nil {
		return empty, err
	}
	commands, err := activeCommands(ctx, tx, a.DeviceID)
	if err != nil {
		return empty, err
	}
	if len(commands) >= maximumActiveCommands {
		return empty, ports.ErrConflict
	}
	command := ports.Command{ID: a.ID, DeviceID: a.DeviceID, Kind: a.Kind, State: "QUEUED", Deadline: a.Deadline}
	data, err := json.Marshal(command)
	if err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO commands(id,ordinal,device_id,idempotency_key,state,data) VALUES(?,(SELECT COALESCE(MAX(ordinal),0)+1 FROM commands),?,?,?,?)", a.ID, a.DeviceID, a.IdempotencyKey, command.State, data); err != nil {
		return empty, err
	}
	if err = deviceChanged(ctx, tx, a.DeviceID); err != nil {
		return empty, err
	}
	return command, tx.Commit()
}

// GetCommand reads a scoped result after classifying deadline expiry without replaying an action.
// Usage: command, err := store.GetCommand(ctx, scope, deviceID, commandID, now).
func (s *Store) GetCommand(ctx context.Context, scope, device, id string, now time.Time) (ports.Command, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ports.Command{}, err
	}
	defer tx.Rollback()
	if _, err = commandDevice(ctx, tx, scope, device); err != nil {
		return ports.Command{}, err
	}
	if err = expireCommands(ctx, tx, device, now); err != nil {
		return ports.Command{}, err
	}
	result, err := readCommand(ctx, tx, id)
	if err != nil {
		return ports.Command{}, err
	}
	if result.DeviceID != device {
		return ports.Command{}, ports.ErrNotFound
	}
	return result, tx.Commit()
}

// ListCommands returns one authorized device's history; reads classify but never replay expired actions.
// Usage: commands, err := store.ListCommands(ctx, scope, deviceID, afterID, 20, now).
func (s *Store) ListCommands(ctx context.Context, scope, device, after string, limit int, now time.Time) ([]ports.Command, error) {
	if limit < 1 || limit > 101 {
		return nil, ports.ErrInvalidArgument
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = commandDevice(ctx, tx, scope, device); err != nil {
		return nil, err
	}
	if err = expireCommands(ctx, tx, device, now); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT data FROM commands WHERE device_id=? AND id>? ORDER BY id LIMIT ?", device, after, limit)
	if err != nil {
		return nil, err
	}
	result := []ports.Command{}
	for rows.Next() {
		var data []byte
		var command ports.Command
		if err = rows.Scan(&data); err == nil {
			err = json.Unmarshal(data, &command)
		}
		if err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, command)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return result, tx.Commit()
}

// CancelCommand stops undispatched work; already committed delivery remains uncertain until platform observation.
// Usage: command, err := store.CancelCommand(ctx, scope, deviceID, commandID, now).
func (s *Store) CancelCommand(ctx context.Context, scope, device, id string, now time.Time) (ports.Command, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ports.Command{}, err
	}
	defer tx.Rollback()
	if _, err = commandDevice(ctx, tx, scope, device); err != nil {
		return ports.Command{}, err
	}
	if err = expireCommands(ctx, tx, device, now); err != nil {
		return ports.Command{}, err
	}
	result, err := readCommand(ctx, tx, id)
	if err != nil {
		return ports.Command{}, err
	}
	if result.DeviceID != device {
		return ports.Command{}, ports.ErrNotFound
	}
	switch result.State {
	case "QUEUED":
		result.State, result.ErrorCode = "CANCELLED", "CANCELLED_BY_HOST"
	case "DISPATCHED":
		result.State, result.ErrorCode = "UNCERTAIN", "CANCELLED_AFTER_DISPATCH"
	default:
		return result, nil
	}
	if err = saveCommand(ctx, tx, result); err != nil {
		return ports.Command{}, err
	}
	return result, tx.Commit()
}

// Validate an observed result and bind its exact bytes once; late readback can resolve an uncertain dispatched action.
func applyCommandReport(ctx context.Context, tx *transaction, device string, report ports.CommandReport) error {
	command, err := readCommand(ctx, tx, report.CommandID)
	if err != nil {
		return err
	}
	if command.DeviceID != device {
		return ports.ErrNotFound
	}
	if command.DispatchToken == "" || command.DispatchToken != report.DispatchToken {
		return ports.ErrConflict
	}
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	var previous []byte
	if err = tx.QueryRowContext(ctx, "SELECT report FROM commands WHERE id=?", command.ID).Scan(&previous); err != nil {
		return err
	}
	if len(previous) > 0 {
		if !bytes.Equal(previous, data) {
			return ports.ErrConflict
		}
		return nil
	}
	if report.State != "SUCCEEDED" && report.State != "FAILED" && report.State != "UNCERTAIN" {
		return ports.ErrInvalidArgument
	}
	if report.State == "SUCCEEDED" {
		observed := report.Observation
		if observed == nil || command.Kind == "REBOOT" && (observed.BootBefore < 0 || observed.BootAfter <= observed.BootBefore) || command.Kind == "LOCK" && (!observed.Secure || !observed.Locked) {
			return ports.ErrInvalidArgument
		}
	}
	command.State, command.ErrorCode, command.Observation = report.State, report.ErrorCode, report.Observation
	if err = saveCommand(ctx, tx, command); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE commands SET report=? WHERE id=?", data, command.ID)
	return err
}

// Dispatch one stable action token and redeliver that same token until reported or expired; never create a new attempt.
func dispatchCommand(ctx context.Context, tx *transaction, device string, inventory ports.Inventory) ([]ports.Command, error) {
	commands, err := activeCommands(ctx, tx, device)
	if err != nil || len(commands) == 0 {
		return nil, err
	}
	command := commands[0]
	if command.State == "QUEUED" {
		capability := "command.lock"
		if command.Kind == "REBOOT" {
			capability = "command.reboot"
		}
		if !inventory.DeviceOwner || !slices.Contains(inventory.Capabilities, capability) {
			command.State, command.ErrorCode = "FAILED", "CAPABILITY_UNAVAILABLE"
			return nil, saveCommand(ctx, tx, command)
		}
		nonce := make([]byte, 24)
		if _, err = rand.Read(nonce); err != nil {
			return nil, err
		}
		command.State, command.DispatchToken = "DISPATCHED", hex.EncodeToString(nonce)
		if err = saveCommand(ctx, tx, command); err != nil {
			return nil, err
		}
	}
	return []ports.Command{command}, nil
}
