// SPDX-License-Identifier: LGPL-3.0-only
package sqlstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/thienvu18/kylok/go/ports"
)

// Read and optionally persist bounded network observations against the exact current desired revision.
func networkStatus(ctx context.Context, tx *transaction, device ports.Device, report *ports.NetworkReport) (ports.NetworkStatus, error) {
	status := ports.NetworkStatus{State: "UNMANAGED"}
	var data, reported []byte
	err := tx.QueryRowContext(ctx, "SELECT data,reported FROM network_policies WHERE device_id=?", device.ID).Scan(&data, &reported)
	if errors.Is(err, sql.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	status.Desired = &ports.DesiredNetworkPolicy{}
	if err = json.Unmarshal(data, status.Desired); err != nil {
		return status, err
	}
	if report != nil {
		if report.Revision > status.Desired.Revision || report.Revision == 0 || len(report.Reason) > 128 || len(report.ActiveProfileID) > 128 || !slices.Contains([]string{"APPLIED", "PENDING", "FAILED", "UNSUPPORTED"}, report.State) {
			return status, ports.ErrInvalidArgument
		}
		if report.Revision == status.Desired.Revision {
			if report.State == "APPLIED" && (!report.ConnectivityVerified || !report.CleanupComplete || report.ActiveProfileID != status.Desired.Policy.PrimaryID) {
				return status, ports.ErrInvalidArgument
			}
			reported, err = json.Marshal(report)
			if err != nil {
				return status, err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE network_policies SET reported=? WHERE device_id=?", reported, device.ID); err != nil {
				return status, err
			}
		}
	}
	if len(reported) > 0 {
		status.Reported = &ports.NetworkReport{}
		if err = json.Unmarshal(reported, status.Reported); err != nil {
			return status, err
		}
	}
	status.State, status.Reason = "PENDING", "NOT_OBSERVED"
	if !device.Inventory.DeviceOwner || !slices.Contains(device.Inventory.Capabilities, "network.lifecycle") {
		status.State, status.Reason = "UNSUPPORTED", "CAPABILITY_UNAVAILABLE"
		return status, nil
	}
	if status.Reported != nil && status.Reported.Revision == status.Desired.Revision {
		status.State, status.Reason = status.Reported.State, status.Reported.Reason
	}
	return status, nil
}

// SetNetworkPolicy binds a normalized reference-only document and advances desired revision in the same transaction.
// Usage: desired, err := store.SetNetworkPolicy(ctx, assignment).
func (s *Store) SetNetworkPolicy(ctx context.Context, a ports.NetworkAssignment) (ports.DesiredNetworkPolicy, error) {
	empty := ports.DesiredNetworkPolicy{}
	if a.ScopeID == "" || a.DeviceID == "" || a.IdempotencyKey == "" {
		return empty, ports.ErrInvalidArgument
	}
	policy, err := ports.NormalizeNetworkPolicy(a.Policy)
	if err != nil {
		return empty, err
	}
	body, err := json.Marshal(policy)
	if err != nil {
		return empty, err
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
	var previous, response []byte
	err = tx.QueryRowContext(ctx, "SELECT body,response FROM network_requests WHERE device_id=? AND idempotency_key=?", a.DeviceID, a.IdempotencyKey).Scan(&previous, &response)
	if err == nil {
		if !bytes.Equal(previous, body) {
			return empty, ports.ErrConflict
		}
		var desired ports.DesiredNetworkPolicy
		err = json.Unmarshal(response, &desired)
		return desired, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	desired := ports.DesiredNetworkPolicy{Revision: device.DesiredRevision + 1, Policy: policy}
	response, err = json.Marshal(desired)
	if err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO network_policies(device_id,data) VALUES(?,?) ON CONFLICT(device_id) DO UPDATE SET data=excluded.data", a.DeviceID, response); err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO network_requests(device_id,idempotency_key,body,response) VALUES(?,?,?,?)", a.DeviceID, a.IdempotencyKey, body, response); err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE devices SET desired_revision=? WHERE id=?", desired.Revision, a.DeviceID); err != nil {
		return empty, err
	}
	if err = deviceChanged(ctx, tx, a.DeviceID); err != nil {
		return empty, err
	}
	return desired, tx.Commit()
}

// GetNetworkPolicy returns scoped consistent desired/report/compliance without resolving secrets or triggering effects.
// Usage: status, err := store.GetNetworkPolicy(ctx, scope, deviceID).
func (s *Store) GetNetworkPolicy(ctx context.Context, scope, device string) (ports.NetworkStatus, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ports.NetworkStatus{}, err
	}
	defer tx.Rollback()
	value, err := commandDevice(ctx, tx, scope, device)
	if err != nil {
		return ports.NetworkStatus{}, err
	}
	status, err := networkStatus(ctx, tx, value, nil)
	if err != nil {
		return status, err
	}
	return status, tx.Commit()
}

// GetNetworkRequest reads a detached retry receipt only after checking device/scope ownership.
// Usage: desired, err := store.GetNetworkRequest(ctx, scope, deviceID, requestKey).
func (s *Store) GetNetworkRequest(ctx context.Context, scope, device, key string) (ports.DesiredNetworkPolicy, error) {
	var result ports.DesiredNetworkPolicy
	var response []byte
	err := s.db.QueryRowContext(ctx, "SELECT response FROM network_requests n JOIN devices d ON d.id=n.device_id WHERE d.scope_id=? AND d.id=? AND n.idempotency_key=?", scope, device, key).Scan(&response)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ports.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(response, &result)
	return result, err
}
