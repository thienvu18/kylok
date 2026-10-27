// SPDX-License-Identifier: LGPL-3.0-only
package sqlstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Read one immutable artifact record through the caller's current connection or transaction.
func artifact(ctx context.Context, q queryer, scope, digest string) (ports.Artifact, error) {
	var data []byte
	err := q.QueryRowContext(ctx, "SELECT data FROM artifacts WHERE scope_id=? AND digest=?", scope, digest).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.Artifact{}, ports.ErrNotFound
	}
	if err != nil {
		return ports.Artifact{}, err
	}
	var a ports.Artifact
	err = json.Unmarshal(data, &a)
	return a, err
}

// RegisterArtifact inserts scoped immutable metadata or accepts an identical repeat.
// Usage: err := store.RegisterArtifact(ctx, artifact).
func (s *Store) RegisterArtifact(ctx context.Context, a ports.Artifact) error {
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing []byte
	err = tx.QueryRowContext(ctx, "SELECT data FROM artifacts WHERE scope_id=? AND digest=?", a.ScopeID, a.SHA256).Scan(&existing)
	if err == nil {
		if !bytes.Equal(existing, data) {
			return ports.ErrConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO artifacts(scope_id,digest,data) VALUES(?,?,?)", a.ScopeID, a.SHA256, data); err != nil {
		return err
	}
	if err = recordChange(ctx, tx, a.ScopeID, "application_changed", a.SHA256); err != nil {
		return err
	}
	return tx.Commit()
}

// Artifact loads metadata only from the requested scope and digest.
// Usage: artifact, err := store.Artifact(ctx, scope, digest).
func (s *Store) Artifact(ctx context.Context, scope, digest string) (ports.Artifact, error) {
	return artifact(ctx, s.db, scope, digest)
}

// ListArtifacts returns a stable scoped catalog page without exposing bootstrap entries.
// Usage: artifacts, err := store.ListArtifacts(ctx, "site", "", 100).
func (s *Store) ListArtifacts(ctx context.Context, scope, after string, limit int) ([]ports.Artifact, error) {
	if scope == "" {
		return nil, ports.ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, "SELECT data FROM artifacts WHERE scope_id=? AND digest>? ORDER BY digest LIMIT ?", scope, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ports.Artifact{}
	for rows.Next() {
		var data []byte
		var a ports.Artifact
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &a); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// RemoveArtifact rejects desired/active references before removing scoped catalog metadata.
// Usage: err := store.RemoveArtifact(ctx, scope, digest).
func (s *Store) RemoveArtifact(ctx context.Context, scope, digest string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = artifact(ctx, tx, scope, digest); err != nil {
		return err
	}
	var pending int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM fleet_assignments WHERE scope_id=? AND artifact_sha256=? AND pending=1", scope, digest).Scan(&pending); err != nil {
		return err
	}
	if pending != 0 {
		return ports.ErrReferenced
	}
	var count int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM desired_apps a JOIN devices d ON d.id=a.device_id WHERE d.scope_id=? AND a.artifact_sha256=?`, scope, digest).Scan(&count)
	if err != nil {
		return err
	}
	if count != 0 {
		return ports.ErrReferenced
	}
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM operations o JOIN devices d ON d.id=o.device_id WHERE d.scope_id=? AND o.artifact_sha256=? AND o.state IN ('QUEUED','RUNNING','WAITING_USER')`, scope, digest).Scan(&count)
	if err != nil {
		return err
	}
	if count != 0 {
		return ports.ErrReferenced
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM artifacts WHERE scope_id=? AND digest=?", scope, digest); err != nil {
		return err
	}
	if err = recordChange(ctx, tx, scope, "application_removed", digest); err != nil {
		return err
	}
	return tx.Commit()
}

// Load a durable operation by globally unique ID inside the current transaction.
func operation(ctx context.Context, q queryer, id string) (ports.Operation, error) {
	var data []byte
	err := q.QueryRowContext(ctx, "SELECT data FROM operations WHERE id=?", id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.Operation{}, ports.ErrNotFound
	}
	if err != nil {
		return ports.Operation{}, err
	}
	var op ports.Operation
	err = json.Unmarshal(data, &op)
	return op, err
}

// Persist an operation and its indexed state together so queries agree with its JSON record.
func saveOperation(ctx context.Context, tx *transaction, op ports.Operation) error {
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE operations SET state=?,data=? WHERE id=?", op.State, data, op.ID)
	if err != nil {
		return err
	}
	return deviceChanged(ctx, tx, op.DeviceID)
}

// Read pending work in deterministic revision order before leasing or expiry decisions.
func activeOperations(ctx context.Context, tx *transaction, deviceID string) ([]ports.Operation, error) {
	rows, err := tx.QueryContext(ctx, "SELECT data FROM operations WHERE device_id=? AND state IN ('QUEUED','RUNNING','WAITING_USER') ORDER BY revision,id", deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ports.Operation
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var op ports.Operation
		if err = json.Unmarshal(data, &op); err != nil {
			return nil, err
		}
		result = append(result, op)
	}
	return result, rows.Err()
}

// Fail operations whose host-selected deadline passed without manufacturing an observed success.
func expire(ctx context.Context, tx *transaction, deviceID string, now time.Time) error {
	ops, err := activeOperations(ctx, tx, deviceID)
	if err != nil {
		return err
	}
	for _, op := range ops {
		if !now.Before(op.Deadline) {
			op.State = "FAILED"
			op.ErrorCode = "DEADLINE_EXCEEDED"
			op.ErrorMessage = "Device did not confirm the desired installation before its deadline"
			if err := saveOperation(ctx, tx, op); err != nil {
				return err
			}
		}
	}
	return nil
}

// Assign atomically advances desired app state, binds retries and supersedes older pending work.
// Usage: operation, err := store.Assign(ctx, assignment).
func (s *Store) Assign(ctx context.Context, a ports.Assignment) (ports.Operation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ports.Operation{}, err
	}
	defer tx.Rollback()
	op, err := assign(ctx, tx, a)
	if err != nil {
		return op, err
	}
	return op, tx.Commit()
}

// Apply one assignment within the caller's serialized transaction so fleet progress commits with its device effects.
func assign(ctx context.Context, tx *transaction, a ports.Assignment) (ports.Operation, error) {
	d, _, err := scanDevice(tx.QueryRowContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE scope_id=? AND id=?", a.ScopeID, a.DeviceID))
	if err != nil {
		return ports.Operation{}, err
	}
	if d.State == "REVOKED" {
		return ports.Operation{}, ports.ErrRevoked
	}
	var existing string
	err = tx.QueryRowContext(ctx, "SELECT id FROM operations WHERE device_id=? AND idempotency_key=?", a.DeviceID, a.IdempotencyKey).Scan(&existing)
	if err == nil {
		op, e := operation(ctx, tx, existing)
		if e != nil {
			return op, e
		}
		if op.Artifact.SHA256 != a.ArtifactSHA256 || !op.NotBefore.Equal(a.NotBefore) || !a.NotBefore.IsZero() && !op.Deadline.Equal(a.Deadline) {
			return op, ports.ErrConflict
		}
		return op, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ports.Operation{}, err
	}
	if !a.Now.Before(a.Deadline) || !a.NotBefore.IsZero() && !a.NotBefore.Before(a.Deadline) {
		return ports.Operation{}, ports.ErrInvalidArgument
	}
	apk, err := artifact(ctx, tx, a.ScopeID, a.ArtifactSHA256)
	if err != nil {
		return ports.Operation{}, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM desired_apps WHERE device_id=? AND package_name<>?", a.DeviceID, apk.PackageName).Scan(&count); err != nil {
		return ports.Operation{}, err
	}
	if count >= 64 {
		return ports.Operation{}, ports.ErrInvalidArgument
	}
	ops, err := activeOperations(ctx, tx, a.DeviceID)
	if err != nil {
		return ports.Operation{}, err
	}
	for _, op := range ops {
		if op.Artifact.PackageName == apk.PackageName {
			op.State = "SUPERSEDED"
			op.ErrorCode = "NEWER_DESIRED_REVISION"
			if err = saveOperation(ctx, tx, op); err != nil {
				return ports.Operation{}, err
			}
		}
	}
	op := ports.Operation{ID: a.OperationID, DeviceID: a.DeviceID, Artifact: apk, Revision: d.DesiredRevision + 1, State: "QUEUED", NotBefore: a.NotBefore, Deadline: a.Deadline}
	data, err := json.Marshal(op)
	if err != nil {
		return op, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO operations(id,ordinal,device_id,package_name,artifact_sha256,state,revision,idempotency_key,data) VALUES(?,(SELECT COALESCE(MAX(ordinal),0)+1 FROM operations),?,?,?,?,?,?,?)", op.ID, a.DeviceID, apk.PackageName, apk.SHA256, op.State, op.Revision, a.IdempotencyKey, data)
	if err != nil {
		return op, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO desired_apps(device_id,package_name,artifact_sha256,revision) VALUES(?,?,?,?) ON CONFLICT(device_id,package_name) DO UPDATE SET artifact_sha256=excluded.artifact_sha256,revision=excluded.revision`, a.DeviceID, apk.PackageName, apk.SHA256, op.Revision)
	if err != nil {
		return op, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE devices SET desired_revision=? WHERE id=?", op.Revision, a.DeviceID); err != nil {
		return op, err
	}
	if err = deviceChanged(ctx, tx, a.DeviceID); err != nil {
		return ports.Operation{}, err
	}
	return op, nil
}

// ListOperations expires overdue work and returns a scoped cursor page of durable results.
// Usage: operations, err := store.ListOperations(ctx, scope, deviceID, "", 100, now).
func (s *Store) ListOperations(ctx context.Context, scope, device, after string, limit int, now time.Time) ([]ports.Operation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	if err = tx.QueryRowContext(ctx, "SELECT id FROM devices WHERE scope_id=? AND id=?", scope, device).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return nil, ports.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if err = expire(ctx, tx, device, now); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT data FROM operations WHERE device_id=? AND id>? ORDER BY id LIMIT ?", device, after, limit)
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
		var op ports.Operation
		if err = json.Unmarshal(data, &op); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, op)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return nil, err
	}
	return result, tx.Commit()
}

// AuthorizeDownload checks credential, device, scope, active lease and deadline before exposing an artifact.
// Usage: artifact, err := store.AuthorizeDownload(ctx, deviceID, credentialHash, operationID, digest, leaseID, now).
func (s *Store) AuthorizeDownload(ctx context.Context, id string, hash []byte, operationID, digest, lease string, now time.Time) (ports.Artifact, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ports.Artifact{}, err
	}
	defer tx.Rollback()
	d, actual, err := scanDevice(tx.QueryRowContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE id=?", id))
	if errors.Is(err, ports.ErrNotFound) {
		return ports.Artifact{}, ports.ErrUnauthorized
	}
	if err != nil {
		return ports.Artifact{}, err
	}
	if subtle.ConstantTimeCompare(actual, hash) != 1 {
		return ports.Artifact{}, ports.ErrUnauthorized
	}
	if d.State == "REVOKED" {
		return ports.Artifact{}, ports.ErrRevoked
	}
	op, err := operation(ctx, tx, operationID)
	if err != nil {
		return ports.Artifact{}, ports.ErrNotFound
	}
	if op.DeviceID != id || op.Artifact.ScopeID != d.ScopeID || op.Artifact.SHA256 != digest || op.LeaseID != lease || lease == "" || now.Before(op.NotBefore) || !now.Before(op.Deadline) || !now.Before(op.LeaseUntil) || (op.State != "RUNNING" && op.State != "WAITING_USER") {
		return ports.Artifact{}, ports.ErrUnauthorized
	}
	return artifact(ctx, tx, d.ScopeID, digest)
}

// Compare exact package, version and signer sets instead of trusting an installer callback.
func sameInstalled(observed *ports.InstalledApplication, a ports.Artifact) bool {
	if observed == nil || observed.PackageName != a.PackageName || observed.VersionCode != a.VersionCode {
		return false
	}
	left, right := append([]string(nil), observed.SigningCertificates...), append([]string(nil), a.SigningCertificates...)
	sort.Strings(left)
	sort.Strings(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// Deduplicate and bind a report to its assigned operation/lease before accepting an observed outcome.
func applyReport(ctx context.Context, tx *transaction, device string, r ports.OperationReport) (ports.ReportReceipt, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return ports.ReportReceipt{}, err
	}
	sum := sha256.Sum256(data)
	var previous, encoded []byte
	err = tx.QueryRowContext(ctx, "SELECT body_hash,receipt FROM reports WHERE device_id=? AND report_id=?", device, r.ID).Scan(&previous, &encoded)
	if err == nil {
		if !bytes.Equal(previous, sum[:]) {
			return ports.ReportReceipt{}, ports.ErrConflict
		}
		var receipt ports.ReportReceipt
		err = json.Unmarshal(encoded, &receipt)
		return receipt, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ports.ReportReceipt{}, err
	}
	receipt := ports.ReportReceipt{ReportID: r.ID}
	op, err := operation(ctx, tx, r.OperationID)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		receipt.Reason = "NOT_ASSIGNED"
	case err != nil:
		return receipt, err
	case op.DeviceID != device:
		receipt.Reason = "NOT_ASSIGNED"
	case op.State == "SUPERSEDED":
		receipt.Accepted = true
		receipt.Reason = "SUPERSEDED"
	case op.State == "SUCCEEDED" || op.State == "FAILED" || op.State == "CANCELLED":
		receipt.Accepted = true
		receipt.Reason = "TERMINAL"
	case op.LeaseID == "" || op.LeaseID != r.LeaseID:
		receipt.Reason = "STALE_LEASE"
	case r.Sequence <= op.ReportSequence:
		receipt.Reason = "STALE_REPORT"
	case r.State == "SUCCEEDED" && !sameInstalled(r.Observed, op.Artifact):
		receipt.Reason = "OBSERVATION_MISMATCH"
	default:
		op.State, op.ReportSequence, op.DownloadedBytes = r.State, r.Sequence, r.DownloadedBytes
		op.ErrorCode, op.ErrorMessage, op.Observed = r.ErrorCode, r.ErrorMessage, r.Observed
		if err = saveOperation(ctx, tx, op); err != nil {
			return receipt, err
		}
		receipt.Accepted = true
	}
	encoded, err = json.Marshal(receipt)
	if err != nil {
		return receipt, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO reports(device_id,report_id,body_hash,receipt) VALUES(?,?,?,?)", device, r.ID, sum[:], encoded)
	return receipt, err
}

// Sync atomically authenticates, binds replay bytes, accepts observations and checkpoints the desired snapshot.
// Usage: snapshot, err := store.Sync(ctx, input).
func (s *Store) Sync(ctx context.Context, in ports.SyncInput) (ports.Snapshot, error) {
	empty := ports.Snapshot{}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	d, actual, err := scanDevice(tx.QueryRowContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE id=?", in.DeviceID))
	if errors.Is(err, ports.ErrNotFound) {
		return empty, ports.ErrUnauthorized
	}
	if err != nil {
		return empty, err
	}
	if subtle.ConstantTimeCompare(actual, in.CredentialHash) != 1 {
		return empty, ports.ErrUnauthorized
	}
	if d.State == "REVOKED" {
		return empty, ports.ErrRevoked
	}
	payload, _ := json.Marshal(struct {
		Sequence, Revision   uint64
		Inventory            ports.Inventory
		Reports              []ports.OperationReport
		PolicyReport         *ports.PolicyReport
		RetainedOperationIDs []string
		CommandReports       []ports.CommandReport
		NetworkReport        *ports.NetworkReport
	}{in.Sequence, in.ReportedRevision, in.Inventory, in.Reports, in.PolicyReport, in.RetainedOperationIDs, in.CommandReports, in.NetworkReport})
	sum := sha256.Sum256(payload)
	var sequence uint64
	var previous, cached []byte
	if err = tx.QueryRowContext(ctx, "SELECT sync_sequence,sync_hash,sync_snapshot FROM devices WHERE id=?", in.DeviceID).Scan(&sequence, &previous, &cached); err != nil {
		return empty, err
	}
	if in.Sequence == 0 || in.Sequence < sequence {
		return empty, ports.ErrStale
	}
	if in.Sequence == sequence {
		if !bytes.Equal(previous, sum[:]) {
			return empty, ports.ErrConflict
		}
		var snapshot ports.Snapshot
		err = json.Unmarshal(cached, &snapshot)
		return snapshot, err
	}
	if in.ReportedRevision < d.ReportedRevision || in.ReportedRevision > d.DesiredRevision {
		return empty, ports.ErrStale
	}
	if err = expire(ctx, tx, in.DeviceID, in.Now); err != nil {
		return empty, err
	}
	if err = expireCommands(ctx, tx, in.DeviceID, in.Now); err != nil {
		return empty, err
	}
	snapshot := ports.Snapshot{Sequence: in.Sequence}
	if len(in.CommandReports) > 16 {
		return empty, ports.ErrInvalidArgument
	}
	for _, report := range in.CommandReports {
		if err = applyCommandReport(ctx, tx, in.DeviceID, report); err != nil {
			return empty, err
		}
		snapshot.CommandReceipts = append(snapshot.CommandReceipts, report.CommandID)
	}
	snapshot.Commands, err = dispatchCommand(ctx, tx, in.DeviceID, in.Inventory)
	if err != nil {
		return empty, err
	}
	if len(in.RetainedOperationIDs) > 64 {
		return empty, ports.ErrInvalidArgument
	}
	for _, id := range in.RetainedOperationIDs {
		op, err := operation(ctx, tx, id)
		if errors.Is(err, ports.ErrNotFound) {
			snapshot.StoppedOperationIDs = append(snapshot.StoppedOperationIDs, id)
			continue
		}
		if err != nil {
			return empty, err
		}
		if op.DeviceID != in.DeviceID || op.State == "CANCELLED" || op.State == "FAILED" || op.State == "SUPERSEDED" {
			snapshot.StoppedOperationIDs = append(snapshot.StoppedOperationIDs, id)
		}
	}
	for _, report := range in.Reports {
		receipt, err := applyReport(ctx, tx, in.DeviceID, report)
		if err != nil {
			return empty, err
		}
		snapshot.Receipts = append(snapshot.Receipts, receipt)
	}
	snapshot.Desired, err = desiredApplications(ctx, tx, d)
	if err != nil {
		return empty, err
	}
	// Reconcile drift after a previously confirmed install. A success report in
	// this request can precede its inventory observation, so wait for the next sync.
	for _, desired := range snapshot.Desired {
		matches := false
		for _, app := range in.Inventory.Applications {
			if sameInstalled(&app, desired.Artifact) {
				matches = true
			}
		}
		if matches {
			continue
		}
		var data []byte
		err = tx.QueryRowContext(ctx, "SELECT data FROM operations WHERE device_id=? AND revision=? ORDER BY ordinal DESC LIMIT 1", in.DeviceID, desired.Revision).Scan(&data)
		if err != nil {
			return empty, err
		}
		var previous ports.Operation
		if err = json.Unmarshal(data, &previous); err != nil {
			return empty, err
		}
		if previous.State != "SUCCEEDED" {
			continue
		}
		justCompleted := false
		for _, report := range in.Reports {
			if report.OperationID == previous.ID && report.State == "SUCCEEDED" {
				justCompleted = true
			}
		}
		if justCompleted {
			continue
		}
		random := make([]byte, 32)
		if _, err = rand.Read(random); err != nil {
			return empty, err
		}
		id := base64.RawURLEncoding.EncodeToString(random)
		op := ports.Operation{ID: id, DeviceID: in.DeviceID, Artifact: desired.Artifact, Revision: desired.Revision, State: "QUEUED", Deadline: in.Now.Add(24 * time.Hour)}
		data, err = json.Marshal(op)
		if err != nil {
			return empty, err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO operations(id,ordinal,device_id,package_name,artifact_sha256,state,revision,idempotency_key,data) VALUES(?,(SELECT COALESCE(MAX(ordinal),0)+1 FROM operations),?,?,?,?,?,?,?)", id, in.DeviceID, desired.Artifact.PackageName, desired.Artifact.SHA256, op.State, op.Revision, "reconcile-"+id, data)
		if err != nil {
			return empty, err
		}
	}
	ops, err := activeOperations(ctx, tx, in.DeviceID)
	if err != nil {
		return empty, err
	}
	for _, op := range ops {
		if in.Now.Before(op.NotBefore) {
			continue
		}
		if len(snapshot.Operations) >= 8 {
			break
		}
		if op.LeaseID == "" || !in.Now.Before(op.LeaseUntil) {
			if op.Attempts >= 8 {
				op.State = "FAILED"
				op.ErrorCode = "ATTEMPTS_EXHAUSTED"
				if err = saveOperation(ctx, tx, op); err != nil {
					return empty, err
				}
				continue
			}
			nonce := make([]byte, 24)
			if _, err = rand.Read(nonce); err != nil {
				return empty, err
			}
			op.LeaseID = hex.EncodeToString(nonce)
			op.Attempts++
		}
		op.LeaseUntil = in.Now.Add(30 * time.Minute)
		if op.State == "QUEUED" {
			op.State = "RUNNING"
		}
		if err = saveOperation(ctx, tx, op); err != nil {
			return empty, err
		}
		snapshot.Operations = append(snapshot.Operations, op)
	}
	for _, want := range snapshot.Desired {
		compliance := ports.ApplicationCompliance{PackageName: want.Artifact.PackageName, Revision: want.Revision, State: "PENDING", Reason: "NOT_OBSERVED"}
		for _, app := range in.Inventory.Applications {
			if sameInstalled(&app, want.Artifact) {
				compliance.State, compliance.Reason = "APPLIED", ""
			}
		}
		if compliance.State != "APPLIED" {
			for _, cap := range in.Inventory.Capabilities {
				if cap == "install_apk" {
					compliance.Reason = "INSTALL_PENDING"
				}
			}
			if compliance.Reason == "NOT_OBSERVED" {
				compliance.State, compliance.Reason = "UNSUPPORTED", "INSTALL_CAPABILITY_MISSING"
			}
			var data []byte
			err = tx.QueryRowContext(ctx, "SELECT data FROM operations WHERE device_id=? AND revision=? ORDER BY ordinal DESC LIMIT 1", in.DeviceID, want.Revision).Scan(&data)
			if err != nil {
				return empty, err
			}
			var op ports.Operation
			if err = json.Unmarshal(data, &op); err != nil {
				return empty, err
			}
			if op.State == "FAILED" || op.State == "CANCELLED" {
				compliance.State, compliance.Reason = "FAILED", op.ErrorCode
			}
			if in.Now.Before(op.NotBefore) && op.State == "QUEUED" {
				compliance.State, compliance.Reason = "PENDING", "SCHEDULED"
			}
			if op.State == "WAITING_USER" {
				compliance.State, compliance.Reason = "PENDING", "USER_ACTION_REQUIRED"
			}
		}
		snapshot.Compliance = append(snapshot.Compliance, compliance)
	}
	d.State, d.Inventory, d.LastSeenAt, d.ReportedRevision = "MANAGED", in.Inventory, in.Now, in.ReportedRevision
	policy, err := policyStatus(ctx, tx, d, snapshot.Desired, in.PolicyReport)
	if err != nil {
		return empty, err
	}
	snapshot.Network, err = networkStatus(ctx, tx, d, in.NetworkReport)
	if err != nil {
		return empty, err
	}
	snapshot.DesiredPolicy, snapshot.PolicyCompliance = policy.Desired, policy.Compliance
	snapshot.Device = d
	inventory, err := json.Marshal(in.Inventory)
	if err != nil {
		return empty, err
	}
	cached, err = json.Marshal(snapshot)
	if err != nil {
		return empty, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE devices SET inventory=?,state='MANAGED',last_seen_at=?,reported_revision=?,sync_sequence=?,sync_hash=?,sync_snapshot=? WHERE id=?", inventory, in.Now.UnixMilli(), in.ReportedRevision, in.Sequence, sum[:], cached, in.DeviceID)
	if err != nil {
		return empty, err
	}
	if err = deviceChanged(ctx, tx, in.DeviceID); err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return snapshot, nil
}
