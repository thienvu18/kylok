// SPDX-License-Identifier: LGPL-3.0-only
package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

// Check verifies database connectivity without creating a transaction or changing state.
// Usage: err := store.Check(ctx).
func (s *Store) Check(ctx context.Context) error { return s.db.PingContext(ctx) }

// GetDevice hides absent/foreign identities behind the same scoped not-found contract.
// Usage: device, err := store.GetDevice(ctx, scope, deviceID).
func (s *Store) GetDevice(ctx context.Context, scope, id string) (ports.Device, error) {
	value, _, err := scanDevice(s.db.QueryRowContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE scope_id=? AND id=?", scope, id))
	return value, err
}

// QueryDevices evaluates exact filters and keyset bounds in one database statement.
// Usage: values, err := store.QueryDevices(ctx, ports.DeviceQuery{ScopeID: scope, Limit: 20}).
func (s *Store) QueryDevices(ctx context.Context, input ports.DeviceQuery) ([]ports.Device, error) {
	if input.ScopeID == "" || input.Limit < 1 || input.Limit > 101 {
		return nil, ports.ErrInvalidArgument
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE scope_id=? AND id>? AND (?='' OR state=?) AND (?='' OR correlation_id=?) ORDER BY id LIMIT ?", input.ScopeID, input.After, input.State, input.State, input.CorrelationID, input.CorrelationID, input.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ports.Device{}
	for rows.Next() {
		value, _, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// GetOperation verifies ownership and evaluates deadline expiry before reading a detached result.
// Usage: operation, err := store.GetOperation(ctx, scope, deviceID, operationID, now).
func (s *Store) GetOperation(ctx context.Context, scope, device, id string, now time.Time) (ports.Operation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ports.Operation{}, err
	}
	defer tx.Rollback()
	var found string
	if err = tx.QueryRowContext(ctx, "SELECT id FROM devices WHERE scope_id=? AND id=?", scope, device).Scan(&found); errors.Is(err, sql.ErrNoRows) {
		return ports.Operation{}, ports.ErrNotFound
	} else if err != nil {
		return ports.Operation{}, err
	}
	if err = expire(ctx, tx, device, now); err != nil {
		return ports.Operation{}, err
	}
	value, err := operation(ctx, tx, id)
	if err != nil {
		return ports.Operation{}, err
	}
	if value.DeviceID != device {
		return ports.Operation{}, ports.ErrNotFound
	}
	return value, tx.Commit()
}

// QueryOperations serializes expiry with scoped, filtered keyset reads.
// Usage: values, err := store.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: deviceID, Limit: 20, Now: now}).
func (s *Store) QueryOperations(ctx context.Context, input ports.OperationQuery) ([]ports.Operation, error) {
	if input.ScopeID == "" || input.DeviceID == "" || input.Limit < 1 || input.Limit > 101 {
		return nil, ports.ErrInvalidArgument
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var found string
	if err = tx.QueryRowContext(ctx, "SELECT id FROM devices WHERE scope_id=? AND id=?", input.ScopeID, input.DeviceID).Scan(&found); errors.Is(err, sql.ErrNoRows) {
		return nil, ports.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if err = expire(ctx, tx, input.DeviceID, input.Now); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT data FROM operations WHERE device_id=? AND id>? AND (?='' OR state=?) ORDER BY id LIMIT ?", input.DeviceID, input.After, input.State, input.State, input.Limit)
	if err != nil {
		return nil, err
	}
	result := []ports.Operation{}
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			rows.Close()
			return nil, err
		}
		var value ports.Operation
		if err = json.Unmarshal(data, &value); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, value)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return result, tx.Commit()
}
