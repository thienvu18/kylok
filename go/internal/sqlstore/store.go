// SPDX-License-Identifier: LGPL-3.0-only
// Package sqlstore preserves shared transaction semantics across supported SQL dialects.
package sqlstore

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

type Store struct{ db *database }

// Open verifies or explicitly initializes the current schema on a supplied pool; it never closes the pool.
// Usage: store, err := sqlstore.Open(ctx, db, "postgres", "kylok", true).
func Open(ctx context.Context, pool *sql.DB, dialect, schema string, initialize bool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("database pool required")
	}
	if dialect != "sqlite" && dialect != "postgres" {
		return nil, errors.New("unsupported SQL dialect")
	}
	if dialect == "postgres" && !schemaPattern.MatchString(schema) {
		return nil, errors.New("schema must be a lowercase SQL identifier of 1–63 characters")
	}
	s := &Store{db: &database{pool: pool, dialect: dialect, schema: schema}}
	if err := s.migrate(ctx, initialize); err != nil {
		return nil, err
	}
	return s, nil
}

// Initialize the current development schema or reject an incomplete existing schema without repair.
func (s *Store) migrate(ctx context.Context, initialize bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if s.db.dialect == "postgres" {
		if initialize {
			if _, err = tx.Tx.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS "`+s.db.schema+`"`); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_version(version BIGINT NOT NULL)"); err != nil {
				return err
			}
		}
		err = tx.QueryRowContext(ctx, "SELECT version FROM schema_version").Scan(&version)
		if errors.Is(err, sql.ErrNoRows) && initialize {
			version = 0
		} else if err != nil {
			return err
		}
	} else if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != 0 && version != 1 {
		return fmt.Errorf("unsupported database version %d", version)
	}
	if version == 0 {
		if !initialize {
			return errors.New("schema initialization requires explicit opt-in")
		}
		schemaSQL := `
CREATE TABLE enrollments(token_hash BLOB PRIMARY KEY,scope_id TEXT NOT NULL,expires_at INTEGER NOT NULL,artifact_sha256 TEXT NOT NULL,correlation_id TEXT NOT NULL DEFAULT '',attempt_id TEXT,key_hash BLOB,device_id TEXT,receipt BLOB,recover_until INTEGER);
CREATE UNIQUE INDEX enrollments_correlation ON enrollments(scope_id,correlation_id) WHERE correlation_id<>'';
CREATE TABLE devices(id TEXT PRIMARY KEY,scope_id TEXT NOT NULL,state TEXT NOT NULL,correlation_id TEXT NOT NULL DEFAULT '',inventory BLOB NOT NULL,credential_hash BLOB NOT NULL,created_at INTEGER NOT NULL,last_seen_at INTEGER NOT NULL,desired_revision INTEGER NOT NULL DEFAULT 0,reported_revision INTEGER NOT NULL DEFAULT 0,sync_sequence INTEGER NOT NULL DEFAULT 0,sync_hash BLOB,sync_snapshot BLOB);
CREATE INDEX devices_scope_id ON devices(scope_id,id);
CREATE TABLE artifacts(scope_id TEXT NOT NULL,digest TEXT NOT NULL,data BLOB NOT NULL,PRIMARY KEY(scope_id,digest));
CREATE TABLE operations(id TEXT PRIMARY KEY,ordinal INTEGER NOT NULL UNIQUE,device_id TEXT NOT NULL,package_name TEXT NOT NULL,artifact_sha256 TEXT NOT NULL,state TEXT NOT NULL,revision INTEGER NOT NULL,idempotency_key TEXT NOT NULL,data BLOB NOT NULL,UNIQUE(device_id,idempotency_key));
CREATE INDEX operations_device ON operations(device_id,state,revision);
CREATE TABLE desired_apps(device_id TEXT NOT NULL,package_name TEXT NOT NULL,artifact_sha256 TEXT NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(device_id,package_name));
CREATE TABLE reports(device_id TEXT NOT NULL,report_id TEXT NOT NULL,body_hash BLOB NOT NULL,receipt BLOB NOT NULL,PRIMARY KEY(device_id,report_id));
CREATE TABLE policies(device_id TEXT PRIMARY KEY,data BLOB NOT NULL,reported BLOB);
CREATE TABLE policy_requests(device_id TEXT NOT NULL,idempotency_key TEXT NOT NULL,body BLOB NOT NULL,response BLOB NOT NULL,PRIMARY KEY(device_id,idempotency_key));
CREATE TABLE change_heads(scope_id TEXT PRIMARY KEY,sequence INTEGER NOT NULL,floor INTEGER NOT NULL);
CREATE TABLE changes(scope_id TEXT NOT NULL,sequence INTEGER NOT NULL,kind TEXT NOT NULL,resource_id TEXT NOT NULL,PRIMARY KEY(scope_id,sequence));
CREATE TABLE projection_scans(id TEXT PRIMARY KEY,ordinal INTEGER NOT NULL UNIQUE,scope_id TEXT NOT NULL,watermark INTEGER NOT NULL,expires_at INTEGER NOT NULL);
CREATE INDEX projection_expiry ON projection_scans(scope_id,expires_at,id);
CREATE TABLE projection_items(snapshot_id TEXT NOT NULL,item_index INTEGER NOT NULL,data BLOB NOT NULL,PRIMARY KEY(snapshot_id,item_index));
PRAGMA user_version=1;`
		if s.db.dialect == "postgres" {
			schemaSQL = strings.ReplaceAll(schemaSQL, " BLOB", " BYTEA")
			schemaSQL = strings.ReplaceAll(schemaSQL, " INTEGER", " BIGINT")
			schemaSQL = strings.ReplaceAll(schemaSQL, "PRAGMA user_version=1;", "INSERT INTO schema_version(version) VALUES(1);")
		}
		_, err = tx.ExecContext(ctx, schemaSQL)
		if err != nil {
			return err
		}
	}
	// user_version alone does not prove that the database still has the schema.
	// Resolve every required column before returning a ready store, without
	// scanning device data or attempting to repair an existing damaged database.
	for _, query := range []string{
		"SELECT token_hash,scope_id,expires_at,artifact_sha256,correlation_id,attempt_id,key_hash,device_id,receipt,recover_until FROM enrollments LIMIT 0",
		"SELECT " + deviceColumns + " FROM devices LIMIT 0",
		"SELECT sync_sequence,sync_hash,sync_snapshot FROM devices LIMIT 0",
		"SELECT scope_id,digest,data FROM artifacts LIMIT 0",
		"SELECT id,ordinal,device_id,package_name,artifact_sha256,state,revision,idempotency_key,data FROM operations LIMIT 0",
		"SELECT device_id,package_name,artifact_sha256,revision FROM desired_apps LIMIT 0",
		"SELECT device_id,report_id,body_hash,receipt FROM reports LIMIT 0",
		"SELECT device_id,data,reported FROM policies LIMIT 0",
		"SELECT device_id,idempotency_key,body,response FROM policy_requests LIMIT 0",
		"SELECT scope_id,sequence,floor FROM change_heads LIMIT 0",
		"SELECT scope_id,sequence,kind,resource_id FROM changes LIMIT 0",
		"SELECT id,ordinal,scope_id,watermark,expires_at FROM projection_scans LIMIT 0",
		"SELECT snapshot_id,item_index,data FROM projection_items LIMIT 0",
	} {
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return fmt.Errorf("invalid development schema: %w", err)
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateEnrollment persists only a token verifier, scope, expiry and immutable bootstrap binding.
// Usage: err := store.CreateEnrollment(ctx, enrollment).
func (s *Store) CreateEnrollment(ctx context.Context, e ports.Enrollment) error {
	result, err := s.db.ExecContext(ctx, "INSERT INTO enrollments(token_hash,scope_id,expires_at,artifact_sha256,correlation_id) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING", e.TokenHash, e.ScopeID, e.ExpiresAt.UnixMilli(), e.ArtifactSHA256, e.CorrelationID)
	if err == nil {
		n, countErr := result.RowsAffected()
		if countErr != nil {
			return countErr
		}
		if n == 0 {
			return ports.ErrConflict
		}
	}
	return err
}

// Enrollment loads the token verifier's original scope, expiry and bootstrap identity.
// Usage: enrollment, err := store.Enrollment(ctx, tokenHash).
func (s *Store) Enrollment(ctx context.Context, hash []byte) (ports.Enrollment, error) {
	var e ports.Enrollment
	var expiry int64
	err := s.db.QueryRowContext(ctx, "SELECT token_hash,scope_id,expires_at,artifact_sha256,correlation_id FROM enrollments WHERE token_hash=?", hash).Scan(&e.TokenHash, &e.ScopeID, &expiry, &e.ArtifactSHA256, &e.CorrelationID)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ports.ErrNotFound
	}
	e.ExpiresAt = time.UnixMilli(expiry).UTC()
	return e, err
}

// Consume serializes competing claims and atomically stores identity, credential verifier and encrypted recovery receipt.
// Usage: receipt, err := store.Consume(ctx, attempt).
func (s *Store) Consume(ctx context.Context, a ports.Attempt) ([]byte, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var scope, correlation string
	var expiry int64
	var id, deviceID sql.NullString
	var recoverUntil sql.NullInt64
	var key, receipt []byte
	err = tx.QueryRowContext(ctx, `SELECT scope_id,correlation_id,expires_at,attempt_id,key_hash,device_id,receipt,recover_until FROM enrollments WHERE token_hash=?`, a.TokenHash).Scan(&scope, &correlation, &expiry, &id, &key, &deviceID, &receipt, &recoverUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if id.Valid {
		if id.String != a.ID || subtle.ConstantTimeCompare(key, a.KeyHash) != 1 {
			return nil, ports.ErrConsumed
		}
		var state string
		if err = tx.QueryRowContext(ctx, "SELECT state FROM devices WHERE id=?", deviceID.String).Scan(&state); err != nil {
			return nil, err
		}
		if state == "REVOKED" {
			return nil, ports.ErrRevoked
		}
		if a.Now.UnixMilli() >= recoverUntil.Int64 {
			return nil, ports.ErrRecoveryExpired
		}
		return receipt, nil
	}
	if a.Now.UnixMilli() >= expiry {
		return nil, ports.ErrExpired
	}
	if a.Device.ScopeID != scope || len(a.Receipt) == 0 || len(a.CredentialHash) != 32 || len(a.KeyHash) != 32 || a.ID == "" || !a.RecoverUntil.After(a.Now) {
		return nil, errors.New("invalid atomic enrollment candidate")
	}
	inventory, err := json.Marshal(a.Device.Inventory)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO devices(id,scope_id,state,correlation_id,inventory,credential_hash,created_at,last_seen_at) VALUES(?,?,?,?,?,?,?,?)`, a.Device.ID, scope, "ENROLLED", correlation, inventory, a.CredentialHash, a.Now.UnixMilli(), int64(0))
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE enrollments SET attempt_id=?,key_hash=?,device_id=?,receipt=?,recover_until=? WHERE token_hash=?`, a.ID, a.KeyHash, a.Device.ID, a.Receipt, a.RecoverUntil.UnixMilli(), a.TokenHash)
	if err != nil {
		return nil, err
	}
	if err = recordChange(ctx, tx, scope, "device_changed", a.Device.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return append([]byte(nil), a.Receipt...), nil
}

type scanner interface{ Scan(...any) error }

// Decode persisted device columns and normalize timestamps while preserving not-found semantics.
func scanDevice(row scanner) (ports.Device, []byte, error) {
	var d ports.Device
	var inventory, hash []byte
	var created, seen int64
	err := row.Scan(&d.ID, &d.ScopeID, &d.State, &d.CorrelationID, &inventory, &hash, &created, &seen, &d.DesiredRevision, &d.ReportedRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return d, nil, ports.ErrNotFound
	}
	if err != nil {
		return d, nil, err
	}
	if err = json.Unmarshal(inventory, &d.Inventory); err != nil {
		return d, nil, err
	}
	d.CreatedAt = time.UnixMilli(created).UTC()
	if seen != 0 {
		d.LastSeenAt = time.UnixMilli(seen).UTC()
	}
	return d, hash, nil
}

const deviceColumns = "id,scope_id,state,correlation_id,inventory,credential_hash,created_at,last_seen_at,desired_revision,reported_revision"

// Authenticate checks the credential verifier before rejecting revoked identities.
// Usage: device, err := store.Authenticate(ctx, deviceID, credentialHash).
func (s *Store) Authenticate(ctx context.Context, id string, hash []byte) (ports.Device, error) {
	d, actual, err := scanDevice(s.db.QueryRowContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE id=?", id))
	if errors.Is(err, ports.ErrNotFound) {
		return d, ports.ErrUnauthorized
	}
	if err != nil {
		return d, err
	}
	if subtle.ConstantTimeCompare(actual, hash) != 1 {
		return ports.Device{}, ports.ErrUnauthorized
	}
	if d.State == "REVOKED" {
		return ports.Device{}, ports.ErrRevoked
	}
	return d, nil
}

// ListDevices returns only the authorized scope's stable device cursor page.
// Usage: devices, err := store.ListDevices(ctx, scope, "", 100).
func (s *Store) ListDevices(ctx context.Context, scope, after string, limit int) ([]ports.Device, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE scope_id=? AND id>? ORDER BY id LIMIT ?", scope, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := []ports.Device{}
	for rows.Next() {
		d, _, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// Revoke serializes revocation with enrollment recovery and sync; it does not reset Android.
// Usage: err := store.Revoke(ctx, scope, deviceID).
func (s *Store) Revoke(ctx context.Context, scope, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err = tx.QueryRowContext(ctx, "SELECT state FROM devices WHERE scope_id=? AND id=?", scope, id).Scan(&state); errors.Is(err, sql.ErrNoRows) {
		return ports.ErrNotFound
	} else if err != nil {
		return err
	}
	if state == "REVOKED" {
		return nil
	}
	if _, err = tx.ExecContext(ctx, "UPDATE devices SET state='REVOKED' WHERE scope_id=? AND id=?", scope, id); err != nil {
		return err
	}
	if err = recordChange(ctx, tx, scope, "device_changed", id); err != nil {
		return err
	}
	return tx.Commit()
}
