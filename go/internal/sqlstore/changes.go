// SPDX-License-Identifier: LGPL-3.0-only
package sqlstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

// Record invalidations atomically with effects. A transaction deduplicates device changes,
// retaining one notification for the final aggregate rather than transient lease/checkpoint updates.
func recordChange(ctx context.Context, tx *transaction, scope, kind, id string) error {
	if scope == "" {
		return nil
	} // Empty scope is immutable public bootstrap catalog, not a management scope.
	key := scope + "\x00" + kind + "\x00" + id
	if tx.changes == nil {
		tx.changes = map[string]bool{}
	}
	if tx.changes[key] {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO change_heads(scope_id,sequence,floor) VALUES(?,0,0) ON CONFLICT DO NOTHING", scope); err != nil {
		return err
	}
	var head uint64
	if err := tx.QueryRowContext(ctx, "SELECT sequence FROM change_heads WHERE scope_id=?", scope).Scan(&head); err != nil {
		return err
	}
	head++
	if _, err := tx.ExecContext(ctx, "INSERT INTO changes(scope_id,sequence,kind,resource_id) VALUES(?,?,?,?)", scope, head, kind, id); err != nil {
		return err
	}
	floor := uint64(0)
	if head > ports.ChangeRetention {
		floor = head - ports.ChangeRetention
	}
	if _, err := tx.ExecContext(ctx, "UPDATE change_heads SET sequence=?,floor=? WHERE scope_id=?", head, floor, scope); err != nil {
		return err
	}
	if floor > 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM changes WHERE scope_id=? AND sequence<=?", scope, floor); err != nil {
			return err
		}
	}
	tx.changes[key] = true
	return nil
}

// Resolve a device's authoritative scope inside the mutation transaction before recording its final aggregate.
func deviceChanged(ctx context.Context, tx *transaction, id string) error {
	var scope string
	if err := tx.QueryRowContext(ctx, "SELECT scope_id FROM devices WHERE id=?", id).Scan(&scope); err != nil {
		return err
	}
	return recordChange(ctx, tx, scope, "device_changed", id)
}

// Read a scope head/floor within a serialized transaction, including an empty scope history.
func changeHead(ctx context.Context, tx *transaction, scope string) (uint64, uint64, error) {
	var head, floor uint64
	err := tx.QueryRowContext(ctx, "SELECT sequence,floor FROM change_heads WHERE scope_id=?", scope).Scan(&head, &floor)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	return head, floor, err
}

// ListChanges reads committed scoped invalidations in sequence order and explicitly rejects gaps.
// Usage: page, err := store.ListChanges(ctx, scope, lastSequence, 100).
func (s *Store) ListChanges(ctx context.Context, scope string, after uint64, limit int) (ports.ChangesPage, error) {
	if scope == "" || limit < 1 || limit > 100 {
		return ports.ChangesPage{}, ports.ErrInvalidArgument
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ports.ChangesPage{}, err
	}
	defer tx.Rollback()
	head, floor, err := changeHead(ctx, tx, scope)
	if err != nil {
		return ports.ChangesPage{}, err
	}
	if after < floor {
		return ports.ChangesPage{}, ports.ErrCursorExpired
	}
	if after > head {
		return ports.ChangesPage{}, ports.ErrInvalidArgument
	}
	rows, err := tx.QueryContext(ctx, "SELECT sequence,kind,resource_id FROM changes WHERE scope_id=? AND sequence>? ORDER BY sequence LIMIT ?", scope, after, limit)
	if err != nil {
		return ports.ChangesPage{}, err
	}
	result := ports.ChangesPage{Items: []ports.Change{}, Next: after, Head: head}
	for rows.Next() {
		var item ports.Change
		if err = rows.Scan(&item.Sequence, &item.Kind, &item.ResourceID); err != nil {
			rows.Close()
			return ports.ChangesPage{}, err
		}
		result.Items = append(result.Items, item)
		result.Next = item.Sequence
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return ports.ChangesPage{}, err
	}
	return result, tx.Commit()
}

// Read a device's desired/observed state consistently; lease/checkpoint details are stripped by the public facade.
func readDeviceState(ctx context.Context, tx *transaction, scope, id string) (ports.DeviceState, error) {
	var result ports.DeviceState
	device, _, err := scanDevice(tx.QueryRowContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE scope_id=? AND id=?", scope, id))
	if err != nil {
		return result, err
	}
	result.Network, err = networkStatus(ctx, tx, device, nil)
	if err != nil {
		return result, err
	}
	result.Device = device
	if result.Desired, err = desiredApplications(ctx, tx, device); err != nil {
		return result, err
	}
	if result.Policy, err = policyStatus(ctx, tx, device, result.Desired, nil); err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT data FROM operations WHERE device_id=? ORDER BY id", id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var data []byte
		var op ports.Operation
		if err = rows.Scan(&data); err == nil {
			err = json.Unmarshal(data, &op)
		}
		if err != nil {
			rows.Close()
			return ports.DeviceState{}, err
		}
		result.Operations = append(result.Operations, op)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return result, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT data FROM commands WHERE device_id=? ORDER BY id", id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var data []byte
		var command ports.Command
		if err = rows.Scan(&data); err == nil {
			err = json.Unmarshal(data, &command)
		}
		if err != nil {
			rows.Close()
			return result, err
		}
		result.Commands = append(result.Commands, command)
	}
	return result, errors.Join(rows.Err(), rows.Close())
}

// GetDeviceState returns one scoped transactionally consistent aggregate without expiring or changing work.
// Usage: state, err := store.GetDeviceState(ctx, scope, deviceID).
func (s *Store) GetDeviceState(ctx context.Context, scope, id string) (ports.DeviceState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ports.DeviceState{}, err
	}
	defer tx.Rollback()
	result, err := readDeviceState(ctx, tx, scope, id)
	if err != nil {
		return ports.DeviceState{}, err
	}
	return result, tx.Commit()
}

// Delete saved scan rows before deleting their scope/expiry envelope; no external provider owns these records.
func removeScan(ctx context.Context, tx *transaction, id string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM projection_items WHERE snapshot_id=?", id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM projection_scans WHERE id=?", id)
	return err
}

// Materialize stable projection records and their watermark under the same state serialization lock.
// Scans are capped per scope and expire; an evicted/expired continuation requires a new scan.
func createScan(ctx context.Context, tx *transaction, scope string, now time.Time) (string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id,expires_at FROM projection_scans WHERE scope_id=? ORDER BY ordinal", scope)
	if err != nil {
		return "", err
	}
	var retained []string
	var expired []string
	for rows.Next() {
		var id string
		var expires int64
		if err = rows.Scan(&id, &expires); err != nil {
			rows.Close()
			return "", err
		}
		if expires <= now.UnixMilli() {
			expired = append(expired, id)
		} else {
			retained = append(retained, id)
		}
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return "", err
	}
	for _, id := range expired {
		if err = removeScan(ctx, tx, id); err != nil {
			return "", err
		}
	}
	for len(retained) >= ports.SnapshotLimit {
		if err = removeScan(ctx, tx, retained[0]); err != nil {
			return "", err
		}
		retained = retained[1:]
	}
	random := make([]byte, 32)
	if _, err = rand.Read(random); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(random)
	head, _, err := changeHead(ctx, tx, scope)
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO projection_scans(id,ordinal,scope_id,watermark,expires_at) VALUES(?,(SELECT COALESCE(MAX(ordinal),0)+1 FROM projection_scans),?,?,?)", id, scope, head, now.Add(ports.SnapshotLifetime).UnixMilli()); err != nil {
		return "", err
	}
	rows, err = tx.QueryContext(ctx, "SELECT id FROM devices WHERE scope_id=? ORDER BY id", scope)
	if err != nil {
		return "", err
	}
	var devices []string
	for rows.Next() {
		var device string
		if err = rows.Scan(&device); err != nil {
			rows.Close()
			return "", err
		}
		devices = append(devices, device)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return "", err
	}
	index := 0
	for _, device := range devices {
		state, err := readDeviceState(ctx, tx, scope, device)
		if err != nil {
			return "", err
		}
		if err = saveProjection(ctx, tx, id, index, ports.ProjectionItem{Device: &state}); err != nil {
			return "", err
		}
		index++
	}
	rows, err = tx.QueryContext(ctx, "SELECT data FROM artifacts WHERE scope_id=? ORDER BY digest", scope)
	if err != nil {
		return "", err
	}
	var applications []ports.Artifact
	for rows.Next() {
		var data []byte
		var artifact ports.Artifact
		if err = rows.Scan(&data); err == nil {
			err = json.Unmarshal(data, &artifact)
		}
		if err != nil {
			rows.Close()
			return "", err
		}
		applications = append(applications, artifact)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return "", err
	}
	for _, artifact := range applications {
		if err = saveProjection(ctx, tx, id, index, ports.ProjectionItem{Application: &artifact}); err != nil {
			return "", err
		}
		index++
	}
	// Freeze group and fleet resources under the same watermark as devices/catalog metadata.
	for _, query := range []string{
		"SELECT data FROM fleet_groups WHERE scope_id=? ORDER BY id",
		"SELECT data FROM fleet_assignments WHERE scope_id=? ORDER BY id",
	} {
		rows, err := tx.QueryContext(ctx, query, scope)
		if err != nil {
			return "", err
		}
		var items []ports.ProjectionItem
		for rows.Next() {
			var data []byte
			item := ports.ProjectionItem{}
			if err = rows.Scan(&data); err != nil {
				rows.Close()
				return "", err
			}
			if query == "SELECT data FROM fleet_groups WHERE scope_id=? ORDER BY id" {
				item.Group = &ports.Group{}
				err = json.Unmarshal(data, item.Group)
			} else {
				item.Fleet = &ports.FleetAssignment{}
				err = json.Unmarshal(data, item.Fleet)
			}
			if err != nil {
				rows.Close()
				return "", err
			}
			items = append(items, item)
		}
		if err = errors.Join(rows.Err(), rows.Close()); err != nil {
			return "", err
		}
		for _, item := range items {
			if err = saveProjection(ctx, tx, id, index, item); err != nil {
				return "", err
			}
			index++
		}
	}
	return id, nil
}

// Save one detached immutable scan record inside the transaction that selected its watermark.
func saveProjection(ctx context.Context, tx *transaction, id string, index int, item ports.ProjectionItem) error {
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO projection_items(snapshot_id,item_index,data) VALUES(?,?,?)", id, index, data)
	return err
}

// ReadProjection returns a persisted consistent snapshot page across reconnect/restart.
// It does not change observed/desired state. Expired or evicted scans return ErrCursorExpired.
// Usage: page, err := store.ReadProjection(ctx, scope, "", 0, 100, now).
func (s *Store) ReadProjection(ctx context.Context, scope, id string, after, limit int, now time.Time) (ports.ProjectionPage, error) {
	if scope == "" || after < 0 || limit < 1 || limit > 100 || id == "" && after != 0 {
		return ports.ProjectionPage{}, ports.ErrInvalidArgument
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ports.ProjectionPage{}, err
	}
	defer tx.Rollback()
	if id == "" {
		id, err = createScan(ctx, tx, scope, now)
		if err != nil {
			return ports.ProjectionPage{}, err
		}
	}
	result := ports.ProjectionPage{SnapshotID: id, After: after, Items: []ports.ProjectionItem{}}
	var expires int64
	if err = tx.QueryRowContext(ctx, "SELECT watermark,expires_at FROM projection_scans WHERE id=? AND scope_id=?", id, scope).Scan(&result.Watermark, &expires); errors.Is(err, sql.ErrNoRows) {
		return ports.ProjectionPage{}, ports.ErrCursorExpired
	} else if err != nil {
		return ports.ProjectionPage{}, err
	}
	result.ExpiresAt = time.UnixMilli(expires).UTC()
	if !now.Before(result.ExpiresAt) {
		return ports.ProjectionPage{}, ports.ErrCursorExpired
	}
	rows, err := tx.QueryContext(ctx, "SELECT item_index,data FROM projection_items WHERE snapshot_id=? AND item_index>=? ORDER BY item_index LIMIT ?", id, after, limit+1)
	if err != nil {
		return ports.ProjectionPage{}, err
	}
	for rows.Next() {
		var index int
		var data []byte
		var item ports.ProjectionItem
		if err = rows.Scan(&index, &data); err == nil {
			err = json.Unmarshal(data, &item)
		}
		if err != nil {
			rows.Close()
			return ports.ProjectionPage{}, err
		}
		if len(result.Items) == limit {
			result.More = true
			break
		}
		result.Items = append(result.Items, item)
		result.After = index + 1
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return ports.ProjectionPage{}, err
	}
	return result, tx.Commit()
}
