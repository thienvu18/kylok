// SPDX-License-Identifier: LGPL-3.0-only
package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/thienvu18/kylok/go/ports"
)

// Join current desired app revisions to scoped verified metadata within a consistent transaction.
func desiredApplications(ctx context.Context, tx *sql.Tx, d ports.Device) ([]ports.DesiredApplication, error) {
	rows, err := tx.QueryContext(ctx, `SELECT a.data,w.revision FROM desired_apps w LEFT JOIN artifacts a ON a.scope_id=? AND a.digest=w.artifact_sha256 WHERE w.device_id=? ORDER BY w.package_name`, d.ScopeID, d.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var apps []ports.DesiredApplication
	for rows.Next() {
		var data []byte
		var app ports.DesiredApplication
		if err = rows.Scan(&data, &app.Revision); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &app.Artifact); err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	return apps, rows.Err()
}

// SetPolicy atomically validates required assignments and binds a normalized replacement to its request key.
// Usage: desired, err := store.SetPolicy(ctx, assignment).
func (s *Store) SetPolicy(ctx context.Context, input ports.PolicyAssignment) (ports.DesiredPolicy, error) {
	var empty ports.DesiredPolicy
	p, err := ports.NormalizePolicy(input.Policy)
	if err != nil {
		return empty, err
	}
	body, err := json.Marshal(p)
	if err != nil {
		return empty, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	d, _, err := scanDevice(tx.QueryRowContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE scope_id=? AND id=?", input.ScopeID, input.DeviceID))
	if err != nil {
		return empty, err
	}
	if d.State == "REVOKED" {
		return empty, ports.ErrRevoked
	}
	var previous, response []byte
	err = tx.QueryRowContext(ctx, "SELECT body,response FROM policy_requests WHERE device_id=? AND idempotency_key=?", d.ID, input.IdempotencyKey).Scan(&previous, &response)
	if err == nil {
		if !bytes.Equal(previous, body) {
			return empty, ports.ErrConflict
		}
		err = json.Unmarshal(response, &empty)
		return empty, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	apps, err := desiredApplications(ctx, tx, d)
	if err != nil {
		return empty, err
	}
	for _, name := range p.RequiredPackages {
		if !slices.ContainsFunc(apps, func(a ports.DesiredApplication) bool { return a.Artifact.PackageName == name }) {
			return empty, ports.ErrNotFound
		}
	}
	want := ports.DesiredPolicy{Revision: d.DesiredRevision + 1, Policy: p}
	response, err = json.Marshal(want)
	if err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO policies(device_id,data) VALUES(?,?) ON CONFLICT(device_id) DO UPDATE SET data=excluded.data,reported=NULL`, d.ID, response); err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO policy_requests(device_id,idempotency_key,body,response) VALUES(?,?,?,?)", d.ID, input.IdempotencyKey, body, response); err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE devices SET desired_revision=? WHERE id=?", want.Revision, d.ID); err != nil {
		return empty, err
	}
	return want, tx.Commit()
}

// Accept only current-snapshot observations and derive per-rule compliance independently of assignment.
func policyStatus(ctx context.Context, tx *sql.Tx, d ports.Device, apps []ports.DesiredApplication, incoming *ports.PolicyReport) (ports.PolicyStatus, error) {
	var status ports.PolicyStatus
	var data, reported []byte
	err := tx.QueryRowContext(ctx, "SELECT data,reported FROM policies WHERE device_id=?", d.ID).Scan(&data, &reported)
	if errors.Is(err, sql.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	if err = json.Unmarshal(data, &status.Desired); err != nil {
		return status, err
	}
	if len(reported) > 0 {
		if err = json.Unmarshal(reported, &status.Reported); err != nil {
			return status, err
		}
	}
	expected := ports.PolicyExpectations(status.Desired.Policy, apps)
	if incoming != nil && incoming.Revision == status.Desired.Revision && incoming.SnapshotRevision <= d.DesiredRevision && incoming.SnapshotRevision >= incoming.Revision {
		for _, r := range incoming.Results {
			if _, ok := expected[r.RuleID]; !ok {
				return status, ports.ErrConflict
			}
		}
		if status.Reported == nil || incoming.SnapshotRevision >= status.Reported.SnapshotRevision {
			status.Reported = incoming
			encoded, err := json.Marshal(incoming)
			if err != nil {
				return status, err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE policies SET reported=? WHERE device_id=?", encoded, d.ID); err != nil {
				return status, err
			}
		}
	}
	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		r := ports.PolicyResult{RuleID: key, State: "PENDING", Reason: "NOT_OBSERVED"}
		if !slices.Contains(d.Inventory.Capabilities, ports.PolicyCapability(key)) {
			r.State, r.Reason = "UNSUPPORTED", "CAPABILITY_MISSING"
		}
		if status.Reported != nil && status.Reported.Revision == status.Desired.Revision && status.Reported.SnapshotRevision == d.DesiredRevision {
			for _, observed := range status.Reported.Results {
				if observed.RuleID != key {
					continue
				}
				r = observed
				if r.State == "APPLIED" && r.ObservedValue != expected[key] {
					r.State, r.Reason = "FAILED", "OBSERVATION_MISMATCH"
				}
			}
		}
		status.Compliance = append(status.Compliance, r)
	}
	missing := map[string]bool{}
	for _, name := range status.Desired.Policy.RequiredPackages {
		matches := false
		for _, want := range apps {
			if want.Artifact.PackageName == name {
				for _, actual := range d.Inventory.Applications {
					if sameInstalled(&actual, want.Artifact) {
						matches = true
					}
				}
			}
		}
		missing[name] = !matches
	}
	for i := range status.Compliance {
		r := &status.Compliance[i]
		if d.State == "REVOKED" {
			r.State, r.Reason = "PENDING", "DEVICE_REVOKED"
			continue
		}
		if r.State != "APPLIED" {
			continue
		}
		if strings.HasPrefix(r.RuleID, "required:") && missing[strings.TrimPrefix(r.RuleID, "required:")] {
			r.State, r.Reason = "PENDING", "INVENTORY_MISMATCH"
		}
		if r.RuleID == "lock_task" {
			for _, absent := range missing {
				if absent {
					r.State, r.Reason = "PENDING", "REQUIRED_APPS_PENDING"
				}
			}
		}
		for name, absent := range missing {
			if absent && strings.HasPrefix(r.RuleID, "permission:"+name+":") {
				r.State, r.Reason = "PENDING", "REQUIRED_APP_PENDING"
			}
		}
	}
	return status, nil
}

// GetPolicy returns one consistent scoped desired/reported/compliance view without mutating policy.
// Usage: status, err := store.GetPolicy(ctx, scope, deviceID).
func (s *Store) GetPolicy(ctx context.Context, scope, device string) (ports.PolicyStatus, error) {
	var empty ports.PolicyStatus
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	d, _, err := scanDevice(tx.QueryRowContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE scope_id=? AND id=?", scope, device))
	if err != nil {
		return empty, err
	}
	apps, err := desiredApplications(ctx, tx, d)
	if err != nil {
		return empty, err
	}
	return policyStatus(ctx, tx, d, apps, nil)
}
