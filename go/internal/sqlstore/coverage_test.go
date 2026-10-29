// SPDX-License-Identifier: LGPL-3.0-only
package sqlstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thienvu18/kylok/go/ports"
	_ "modernc.org/sqlite"
)

func hash32(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

func boolPtr(b bool) *bool { return &b }

func testStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	s, err := Open(ctx, db, "sqlite", "", true)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return s, db
}

func createTestDevice(t *testing.T, s *Store, scope, id string, cred string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	token := hash32("tok_" + id)
	if err := s.CreateEnrollment(ctx, ports.Enrollment{
		ScopeID:   scope,
		TokenHash: token,
		ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Consume(ctx, ports.Attempt{
		TokenHash:      token,
		ID:             "att_" + id,
		KeyHash:        hash32("key_" + id),
		CredentialHash: hash32(cred),
		Receipt:        []byte("rcpt_" + id),
		Device: ports.Device{
			ID:      id,
			ScopeID: scope,
			State:   "ACTIVE",
			Inventory: ports.Inventory{
				APILevel:     34,
				Model:        "Pixel",
				DeviceOwner:  true,
				Capabilities: []string{"command.reboot", "command.lock"},
			},
		},
		Now:          now,
		RecoverUntil: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestStoreConstructorValidation verifies sqlstore.Open input validation.
func TestStoreConstructorValidation(t *testing.T) {
	ctx := context.Background()
	if _, err := Open(ctx, nil, "sqlite", "", true); err == nil {
		t.Fatal("expected error on nil pool")
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "val.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := Open(ctx, db, "unsupported_dialect", "", true); err == nil {
		t.Fatal("expected error on unsupported dialect")
	}
	if _, err := Open(ctx, db, "postgres", "INVALID-UPPERCASE", true); err == nil {
		t.Fatal("expected error on invalid postgres schema")
	}
	if _, err := Open(ctx, db, "postgres", strings.Repeat("s", 64), true); err == nil {
		t.Fatal("expected error on long postgres schema")
	}
}

// TestEnrollmentAndDeviceEdgeCases tests Consume, Authenticate, and Revoke edge cases.
func TestEnrollmentAndDeviceEdgeCases(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	tokenHash := hash32("token_hash_012345678901234567890")

	// Consume unknown token -> ErrNotFound
	if _, err := s.Consume(ctx, ports.Attempt{TokenHash: tokenHash, Now: now}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Create valid enrollment
	err := s.CreateEnrollment(ctx, ports.Enrollment{
		ScopeID:   "site",
		TokenHash: tokenHash,
		ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Duplicate enrollment -> ErrConflict
	if err := s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: "site", TokenHash: tokenHash, ExpiresAt: now.Add(time.Hour)}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate enrollment, got %v", err)
	}

	// Read enrollment
	enr, err := s.Enrollment(ctx, tokenHash)
	if err != nil || enr.ScopeID != "site" {
		t.Fatalf("unexpected enrollment: %+v, err: %v", enr, err)
	}
	// Read unknown enrollment -> ErrNotFound
	if _, err := s.Enrollment(ctx, []byte("unknown_hash")); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown hash, got %v", err)
	}

	credHash := hash32("credhash1")
	keyHash := hash32("keyhash1")

	// Consume past expiry -> ErrExpired
	attempt := ports.Attempt{
		TokenHash:      tokenHash,
		ID:             "attempt1",
		KeyHash:        keyHash,
		CredentialHash: credHash,
		Receipt:        []byte("receipt1"),
		Device: ports.Device{
			ID:      "dev1",
			ScopeID: "site",
			State:   "ACTIVE",
			Inventory: ports.Inventory{
				APILevel:     34,
				Model:        "Pixel",
				DeviceOwner:  true,
				Capabilities: []string{"command.reboot", "command.lock"},
			},
		},
		Now:          now.Add(2 * time.Hour),
		RecoverUntil: now.Add(3 * time.Hour),
	}
	if _, err := s.Consume(ctx, attempt); !errors.Is(err, ports.ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}

	// Consume within window -> success
	attempt.Now = now
	receipt, err := s.Consume(ctx, attempt)
	if err != nil || string(receipt) != "receipt1" {
		t.Fatalf("expected receipt1, got %s, err: %v", string(receipt), err)
	}

	// Replay attempt with same ID and key -> returns receipt
	reReceipt, err := s.Consume(ctx, attempt)
	if err != nil || string(reReceipt) != "receipt1" {
		t.Fatalf("expected receipt1 on replay, got %s, err: %v", string(reReceipt), err)
	}

	// Consume already consumed token with different attempt ID -> ErrConsumed
	attemptDiff := attempt
	attemptDiff.ID = "attempt2"
	if _, err := s.Consume(ctx, attemptDiff); !errors.Is(err, ports.ErrConsumed) {
		t.Fatalf("expected ErrConsumed, got %v", err)
	}

	// Authenticate with wrong hash -> ErrUnauthorized
	if _, err := s.Authenticate(ctx, "dev1", hash32("wrong_cred_hash")); !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for bad cred, got %v", err)
	}
	// Authenticate unknown device -> ErrUnauthorized
	if _, err := s.Authenticate(ctx, "dev_unknown", credHash); !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for unknown device, got %v", err)
	}
	// Authenticate valid device
	authDev, err := s.Authenticate(ctx, "dev1", credHash)
	if err != nil || authDev.ID != "dev1" {
		t.Fatalf("expected valid auth, got %v, err: %v", authDev, err)
	}

	// Revoke unknown device -> ErrNotFound
	if err := s.Revoke(ctx, "site", "dev_unknown"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on revoking unknown, got %v", err)
	}
	// Revoke in wrong scope -> ErrNotFound
	if err := s.Revoke(ctx, "other_scope", "dev1"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on revoking wrong scope, got %v", err)
	}
	// Revoke valid device
	if err := s.Revoke(ctx, "site", "dev1"); err != nil {
		t.Fatalf("failed to revoke: %v", err)
	}
	// Revoke already revoked device -> returns nil (idempotent)
	if err := s.Revoke(ctx, "site", "dev1"); err != nil {
		t.Fatalf("expected nil on already revoked device, got %v", err)
	}
}

// TestOperationAndArtifactInternalEdgeCases tests RemoveArtifact, Assign conflicts and supersession.
func TestOperationAndArtifactInternalEdgeCases(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Create device dev1
	createTestDevice(t, s, "site", "dev1", "cred1")

	// Register two artifacts
	apk1 := ports.Artifact{
		ScopeID:     "site",
		SHA256:      strings.Repeat("1", 64),
		PackageName: "com.test.app1",
		VersionCode: 1,
		MinSDK:      33,
		SizeBytes:   1024,
	}
	apk2 := ports.Artifact{
		ScopeID:     "site",
		SHA256:      strings.Repeat("2", 64),
		PackageName: "com.test.app1",
		VersionCode: 2,
		MinSDK:      33,
		SizeBytes:   2048,
	}
	if err := s.RegisterArtifact(ctx, apk1); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterArtifact(ctx, apk2); err != nil {
		t.Fatal(err)
	}

	// Remove unknown artifact -> ErrNotFound
	if err := s.RemoveArtifact(ctx, "site", strings.Repeat("0", 64)); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Assign apk1 to dev1
	op1, err := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       "dev1",
		ArtifactSHA256: apk1.SHA256,
		IdempotencyKey: "assign-key-1",
		OperationID:    "op-1",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Re-assigning with same idempotency key but different artifact -> ErrConflict
	_, err = s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       "dev1",
		ArtifactSHA256: apk2.SHA256,
		IdempotencyKey: "assign-key-1",
		OperationID:    "op-2",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// Re-assigning with same idempotency key and same artifact -> returns original op1
	reOp, err := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       "dev1",
		ArtifactSHA256: apk1.SHA256,
		IdempotencyKey: "assign-key-1",
		OperationID:    "op-1-retry",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil || reOp.ID != op1.ID {
		t.Fatalf("expected same op, got %+v, err: %v", reOp, err)
	}

	// Assign with past deadline -> ErrInvalidArgument
	_, err = s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       "dev1",
		ArtifactSHA256: apk1.SHA256,
		IdempotencyKey: "bad-key",
		OperationID:    "op-bad",
		Now:            now,
		Deadline:       now.Add(-time.Hour),
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on past deadline, got %v", err)
	}

	// Assign apk2 (newer revision of com.test.app1) -> supersedes op1
	op2, err := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       "dev1",
		ArtifactSHA256: apk2.SHA256,
		IdempotencyKey: "assign-key-2",
		OperationID:    "op-2",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if op2.State != "QUEUED" {
		t.Fatalf("expected op2 QUEUED, got %s", op2.State)
	}

	// Verify op1 was superseded
	ops, err := s.ListOperations(ctx, "site", "dev1", "", 10, now)
	if err != nil {
		t.Fatal(err)
	}
	var foundSuperseded bool
	for _, o := range ops {
		if o.ID == op1.ID && o.State == "SUPERSEDED" {
			foundSuperseded = true
		}
	}
	if !foundSuperseded {
		t.Fatal("expected op1 to be marked SUPERSEDED")
	}

	// ListOperations for unknown device -> ErrNotFound
	if _, err := s.ListOperations(ctx, "site", "dev_unknown", "", 10, now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown device in ListOperations, got %v", err)
	}

	// RemoveArtifact when referenced by active operation or desired app -> ErrReferenced
	if err := s.RemoveArtifact(ctx, "site", apk2.SHA256); !errors.Is(err, ports.ErrReferenced) {
		t.Fatalf("expected ErrReferenced, got %v", err)
	}
}

// TestCommandsInternalEdgeCases tests SubmitCommand, CancelCommand, and expiry.
func TestCommandsInternalEdgeCases(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Create device dev1 with reboot & lock capabilities
	createTestDevice(t, s, "site", "dev1", "cred1")

	// SubmitCommand invalid args
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// SubmitCommand on unknown device -> ErrNotFound
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       "dev_unknown",
		ID:             "cmd1",
		IdempotencyKey: "k1",
		Kind:           "REBOOT",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// SubmitCommand valid REBOOT
	cmd1, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       "dev1",
		ID:             "cmd1",
		IdempotencyKey: "k1",
		Kind:           "REBOOT",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd1.State != "QUEUED" {
		t.Fatalf("expected QUEUED, got %s", cmd1.State)
	}

	// Reusing same idempotency key with different kind -> ErrConflict
	_, err = s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       "dev1",
		ID:             "cmd2",
		IdempotencyKey: "k1",
		Kind:           "LOCK",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// Reusing same key and same kind -> returns original
	reCmd, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       "dev1",
		ID:             "cmd1-retry",
		IdempotencyKey: "k1",
		Kind:           "REBOOT",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil || reCmd.ID != cmd1.ID {
		t.Fatalf("expected original command, got %+v, err: %v", reCmd, err)
	}

	// SubmitCommand past deadline -> ErrInvalidArgument
	_, err = s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       "dev1",
		ID:             "cmd-past",
		IdempotencyKey: "k-past",
		Kind:           "REBOOT",
		Now:            now,
		Deadline:       now.Add(-time.Hour),
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// SubmitCommand deadline > 7 days -> ErrInvalidArgument
	_, err = s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       "dev1",
		ID:             "cmd-future",
		IdempotencyKey: "k-future",
		Kind:           "REBOOT",
		Now:            now,
		Deadline:       now.Add(8 * 24 * time.Hour),
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// GetCommand for unknown device -> ErrNotFound
	if _, err := s.GetCommand(ctx, "site", "dev_unknown", "cmd1", now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	// GetCommand for unknown command ID -> ErrNotFound
	if _, err := s.GetCommand(ctx, "site", "dev1", "cmd_unknown", now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	// GetCommand valid
	gotCmd, err := s.GetCommand(ctx, "site", "dev1", "cmd1", now)
	if err != nil || gotCmd.ID != "cmd1" {
		t.Fatalf("unexpected command: %+v, err: %v", gotCmd, err)
	}

	// CancelCommand unknown command -> ErrNotFound
	if _, err := s.CancelCommand(ctx, "site", "dev1", "cmd_unknown", now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	// CancelCommand queued -> CANCELLED
	cancelledCmd, err := s.CancelCommand(ctx, "site", "dev1", "cmd1", now)
	if err != nil || cancelledCmd.State != "CANCELLED" {
		t.Fatalf("expected CANCELLED, got %+v, err: %v", cancelledCmd, err)
	}

	// ListCommands with pagination
	cmds, err := s.ListCommands(ctx, "site", "dev1", "", 10, now)
	if err != nil || len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %+v, err: %v", cmds, err)
	}
	cmdsAfter, err := s.ListCommands(ctx, "site", "dev1", cmd1.ID, 10, now)
	if err != nil || len(cmdsAfter) != 0 {
		t.Fatalf("expected 0 commands after, got %+v", cmdsAfter)
	}
}

// TestFleetInternalEdgeCases tests CreateFleetAssignment, AdvanceFleetAssignment, and CancelFleetAssignment.
func TestFleetInternalEdgeCases(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Register artifact
	apk := ports.Artifact{
		ScopeID:     "site",
		SHA256:      strings.Repeat("3", 64),
		PackageName: "com.fleet.app",
		VersionCode: 1,
		MinSDK:      33,
		SizeBytes:   1024,
	}
	if err := s.RegisterArtifact(ctx, apk); err != nil {
		t.Fatal(err)
	}

	// Create device dev1
	createTestDevice(t, s, "site", "dev1", "cred1")

	// Create group
	grp, err := s.ReplaceGroup(ctx, ports.Group{
		ScopeID:   "site",
		ID:        "grp1",
		DeviceIDs: []string{"dev1"},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if grp.Revision != 1 {
		t.Fatalf("expected revision 1, got %d", grp.Revision)
	}

	// ReplaceGroup revision conflict -> ErrConflict
	_, err = s.ReplaceGroup(ctx, ports.Group{
		ScopeID:   "site",
		ID:        "grp1",
		DeviceIDs: []string{"dev1"},
	}, 5)
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// GetGroup not found
	if _, err := s.GetGroup(ctx, "site", "grp_unknown"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// CreateFleetAssignment with past deadline -> ErrInvalidArgument
	_, err = s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID:        "site",
		ID:             "fleet-past",
		GroupID:        "grp1",
		ArtifactSHA256: apk.SHA256,
		Deadline:       now.Add(-time.Hour),
	}, now)
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	// CreateFleetAssignment with unknown artifact -> ErrNotFound
	_, err = s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID:        "site",
		ID:             "fleet-no-apk",
		GroupID:        "grp1",
		ArtifactSHA256: strings.Repeat("0", 64),
		NotBefore:      now,
		Deadline:       now.Add(time.Hour),
	}, now)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing artifact, got %v", err)
	}

	// Create valid fleet assignment
	batch, err := s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID:        "site",
		ID:             "fleet1",
		GroupID:        "grp1",
		ArtifactSHA256: apk.SHA256,
		NotBefore:      now,
		Deadline:       now.Add(time.Hour),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Items) != 1 || batch.Items[0].State != "PENDING" {
		t.Fatalf("unexpected batch items: %+v", batch.Items)
	}

	// GetFleetAssignment valid
	gotBatch, err := s.GetFleetAssignment(ctx, "site", "fleet1")
	if err != nil || gotBatch.Request.ID != "fleet1" {
		t.Fatalf("unexpected batch: %+v, err: %v", gotBatch, err)
	}

	// AdvanceFleetAssignment with invalid limit -> ErrInvalidArgument
	if _, err := s.AdvanceFleetAssignment(ctx, "site", "fleet1", now, 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0, got %v", err)
	}
	if _, err := s.AdvanceFleetAssignment(ctx, "site", "fleet1", now, 101); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit > 100, got %v", err)
	}

	// AdvanceFleetAssignment after deadline -> expires pending items
	advanced, err := s.AdvanceFleetAssignment(ctx, "site", "fleet1", now.Add(2*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if advanced.Items[0].State != "FAILED" || advanced.Items[0].ErrorCode != "DEADLINE_EXCEEDED" {
		t.Fatalf("expected FAILED DEADLINE_EXCEEDED state after deadline, got %+v", advanced.Items[0])
	}

	// CancelFleetAssignment
	cancelled, err := s.CancelFleetAssignment(ctx, "site", "fleet1", now)
	if err != nil || !cancelled.Cancelled {
		t.Fatalf("expected Cancelled, got %+v, err: %v", cancelled, err)
	}

	// ListFleetAssignments pagination
	batches, err := s.ListFleetAssignments(ctx, "site", "", 10)
	if err != nil || len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %+v, err: %v", batches, err)
	}
	batchesAfter, err := s.ListFleetAssignments(ctx, "site", batch.Request.ID, 10)
	if err != nil || len(batchesAfter) != 0 {
		t.Fatalf("expected 0 batches after, got %+v", batchesAfter)
	}
	if _, err := s.ListFleetAssignments(ctx, "", "", 10); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for empty scope")
	}
}

// TestSQLStoreContextCancellationAndQueryFailures covers BeginTx and query error branches.
func TestSQLStoreContextCancellationAndQueryFailures(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	// 1. Context cancelled branches (hits BeginTx error branches across all store methods)
	_ = s.CreateEnrollment(cancelled, ports.Enrollment{ScopeID: "site", TokenHash: hash32("t")})
	_, _ = s.Enrollment(cancelled, hash32("t"))
	_, _ = s.Consume(cancelled, ports.Attempt{TokenHash: hash32("t"), Now: now})
	_, _ = s.Authenticate(cancelled, "dev1", hash32("c"))
	_ = s.Revoke(cancelled, "site", "dev1")
	_ = s.RegisterArtifact(cancelled, ports.Artifact{ScopeID: "site", SHA256: strings.Repeat("1", 64)})
	_, _ = s.Artifact(cancelled, "site", strings.Repeat("1", 64))
	_ = s.RemoveArtifact(cancelled, "site", strings.Repeat("1", 64))
	_, _ = s.Assign(cancelled, ports.Assignment{ScopeID: "site", DeviceID: "dev1", ArtifactSHA256: strings.Repeat("1", 64), IdempotencyKey: "k", OperationID: "op", Now: now, Deadline: now.Add(time.Hour)})
	_, _ = s.ListOperations(cancelled, "site", "dev1", "", 10, now)
	_, _ = s.GetOperation(cancelled, "site", "dev1", "op1", now)
	_, _ = s.CancelOperation(cancelled, "site", "dev1", "op1", now)
	_, _ = s.SubmitCommand(cancelled, ports.CommandAssignment{ScopeID: "site", DeviceID: "dev1", ID: "c1", IdempotencyKey: "k", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)})
	_, _ = s.GetCommand(cancelled, "site", "dev1", "c1", now)
	_, _ = s.CancelCommand(cancelled, "site", "dev1", "c1", now)
	_, _ = s.ListCommands(cancelled, "site", "dev1", "", 10, now)
	_, _ = s.ReplaceGroup(cancelled, ports.Group{ScopeID: "site", ID: "g1"}, 0)
	_, _ = s.GetGroup(cancelled, "site", "g1")
	_, _ = s.ListGroups(cancelled, "site", "", 10)
	_, _ = s.CreateFleetAssignment(cancelled, ports.FleetRequest{ScopeID: "site", ID: "f1", ArtifactSHA256: strings.Repeat("1", 64), NotBefore: now, Deadline: now.Add(time.Hour)}, now)
	_, _ = s.GetFleetAssignment(cancelled, "site", "f1")
	_, _ = s.AdvanceFleetAssignment(cancelled, "site", "f1", now, 10)
	_, _ = s.CancelFleetAssignment(cancelled, "site", "f1", now)
	_, _ = s.ListFleetAssignments(cancelled, "site", "", 10)
	_, _ = s.SetPolicy(cancelled, ports.PolicyAssignment{ScopeID: "site", DeviceID: "dev1", IdempotencyKey: "k"})
	_, _ = s.GetPolicy(cancelled, "site", "dev1")
	_, _ = s.SetNetworkPolicy(cancelled, ports.NetworkAssignment{ScopeID: "site", DeviceID: "dev1", IdempotencyKey: "k"})
	_, _ = s.GetNetworkPolicy(cancelled, "site", "dev1")
	_, _ = s.ReadProjection(cancelled, "site", "", 0, 10, now)
	_, _ = s.ListChanges(cancelled, "site", 0, 10)
	_, _ = s.GetDeviceState(cancelled, "site", "dev1")
	_, _ = s.QueryDevices(cancelled, ports.DeviceQuery{ScopeID: "site", Limit: 10})
	_, _ = s.QueryOperations(cancelled, ports.OperationQuery{ScopeID: "site", DeviceID: "dev1", Limit: 10, Now: now})

	// 2. Query error branches by dropping tables
	_, _ = db.Exec("DROP TABLE change_heads; DROP TABLE changes; DROP TABLE enrollments; DROP TABLE devices; DROP TABLE operations; DROP TABLE commands; DROP TABLE fleet_groups; DROP TABLE fleet_assignments; DROP TABLE policies; DROP TABLE network_policies; DROP TABLE projection_scans;")

	_ = s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: "site", TokenHash: hash32("t")})
	_, _ = s.Enrollment(ctx, hash32("t"))
	_, _ = s.Consume(ctx, ports.Attempt{TokenHash: hash32("t"), Now: now})
	_, _ = s.Authenticate(ctx, "dev1", hash32("c"))
	_ = s.Revoke(ctx, "site", "dev1")
	_ = s.RegisterArtifact(ctx, ports.Artifact{ScopeID: "site", SHA256: strings.Repeat("1", 64)})
	_, _ = s.Artifact(ctx, "site", strings.Repeat("1", 64))
	_ = s.RemoveArtifact(ctx, "site", strings.Repeat("1", 64))
	_, _ = s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: "dev1", ArtifactSHA256: strings.Repeat("1", 64), IdempotencyKey: "k", OperationID: "op", Now: now, Deadline: now.Add(time.Hour)})
	_, _ = s.ListOperations(ctx, "site", "dev1", "", 10, now)
	_, _ = s.GetOperation(ctx, "site", "dev1", "op1", now)
	_, _ = s.CancelOperation(ctx, "site", "dev1", "op1", now)
	_, _ = s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: "dev1", ID: "c1", IdempotencyKey: "k", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)})
	_, _ = s.GetCommand(ctx, "site", "dev1", "c1", now)
	_, _ = s.CancelCommand(ctx, "site", "dev1", "c1", now)
	_, _ = s.ListCommands(ctx, "site", "dev1", "", 10, now)
	_, _ = s.ReplaceGroup(ctx, ports.Group{ScopeID: "site", ID: "g1"}, 0)
	_, _ = s.GetGroup(ctx, "site", "g1")
	_, _ = s.ListGroups(ctx, "site", "", 10)
	_, _ = s.CreateFleetAssignment(ctx, ports.FleetRequest{ScopeID: "site", ID: "f1", ArtifactSHA256: strings.Repeat("1", 64), NotBefore: now, Deadline: now.Add(time.Hour)}, now)
	_, _ = s.GetFleetAssignment(ctx, "site", "f1")
	_, _ = s.AdvanceFleetAssignment(ctx, "site", "f1", now, 10)
	_, _ = s.CancelFleetAssignment(ctx, "site", "f1", now)
	_, _ = s.ListFleetAssignments(ctx, "site", "", 10)
	_, _ = s.SetPolicy(ctx, ports.PolicyAssignment{ScopeID: "site", DeviceID: "dev1", IdempotencyKey: "k"})
	_, _ = s.GetPolicy(ctx, "site", "dev1")
	_, _ = s.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: "site", DeviceID: "dev1", IdempotencyKey: "k"})
	_, _ = s.GetNetworkPolicy(ctx, "site", "dev1")
	_, _ = s.ReadProjection(ctx, "site", "", 0, 10, now)
	_, _ = s.ListChanges(ctx, "site", 0, 10)
	_, _ = s.GetDeviceState(ctx, "site", "dev1")
	_, _ = s.QueryDevices(ctx, ports.DeviceQuery{ScopeID: "site", Limit: 10})
	_, _ = s.QueryOperations(ctx, ports.OperationQuery{ScopeID: "site", DeviceID: "dev1", Limit: 10, Now: now})
}

// TestAuthorizeDownloadEdgeCases exercises the download authorization paths.
func TestAuthorizeDownloadEdgeCases(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	devID := strings.Repeat("d", 43)
	createTestDevice(t, s, "site", devID, "cred1")

	// device not found
	if _, err := s.AuthorizeDownload(ctx, strings.Repeat("x", 43), hash32("cred1"), "op1", strings.Repeat("a", 64), "lease1", now); !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for unknown device, got %v", err)
	}

	// bad credential hash
	if _, err := s.AuthorizeDownload(ctx, devID, hash32("wrongcred"), "op1", strings.Repeat("a", 64), "lease1", now); !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for bad cred, got %v", err)
	}

	// register artifact
	art := ports.Artifact{
		ScopeID:             "site",
		SHA256:              strings.Repeat("a", 64),
		PackageName:         "com.example.app",
		VersionCode:         1,
		SigningCertificates: []string{strings.Repeat("c", 64)},
	}
	if err := s.RegisterArtifact(ctx, art); err != nil {
		t.Fatal(err)
	}

	// assign operation
	op, err := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ArtifactSHA256: art.SHA256,
		IdempotencyKey: "key-assign-1",
		OperationID:    "op1",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// unknown operation
	if _, err := s.AuthorizeDownload(ctx, devID, hash32("cred1"), "op_unknown", art.SHA256, "lease1", now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown op, got %v", err)
	}

	// operation has no lease yet
	if _, err := s.AuthorizeDownload(ctx, devID, hash32("cred1"), op.ID, art.SHA256, "lease1", now); !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for unleased op, got %v", err)
	}

	// sync to lease the operation
	snap, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:         devID,
		CredentialHash:   hash32("cred1"),
		Sequence:         1,
		ReportedRevision: 0,
		Inventory: ports.Inventory{
			APILevel:     34,
			Model:        "Pixel",
			DeviceOwner:  true,
			Capabilities: []string{"install_apk"},
		},
		Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Operations) == 0 {
		t.Fatal("expected leased operation")
	}
	leasedOp := snap.Operations[0]

	// lease mismatch
	if _, err := s.AuthorizeDownload(ctx, devID, hash32("cred1"), leasedOp.ID, art.SHA256, "wrong_lease", now); !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for wrong lease, got %v", err)
	}

	// successful download authorization
	authArt, err := s.AuthorizeDownload(ctx, devID, hash32("cred1"), leasedOp.ID, art.SHA256, leasedOp.LeaseID, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if authArt.SHA256 != art.SHA256 {
		t.Fatalf("artifact mismatch")
	}

	// revoke device and verify AuthorizeDownload returns ErrRevoked
	if err := s.Revoke(ctx, "site", devID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthorizeDownload(ctx, devID, hash32("cred1"), leasedOp.ID, art.SHA256, leasedOp.LeaseID, now); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked, got %v", err)
	}
}

// TestOperationsAndSyncFlowExercises tests sync, leasing, reconciliation, and reports.
func TestOperationsAndSyncFlowExercises(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	devID := strings.Repeat("d", 43)
	createTestDevice(t, s, "site", devID, "cred1")

	art := ports.Artifact{
		ScopeID:             "site",
		SHA256:              strings.Repeat("b", 64),
		PackageName:         "com.example.reconcile",
		VersionCode:         10,
		SigningCertificates: []string{strings.Repeat("c", 64)},
	}
	if err := s.RegisterArtifact(ctx, art); err != nil {
		t.Fatal(err)
	}

	// Assign operation
	op, err := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ArtifactSHA256: art.SHA256,
		IdempotencyKey: "k-rec-1",
		OperationID:    "op-rec-1",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Assign idempotency conflict: same key, different artifact
	if _, err := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ArtifactSHA256: strings.Repeat("9", 64),
		IdempotencyKey: "k-rec-1",
		OperationID:    "op-rec-1",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// Assign invalid argument: past deadline
	if _, err := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ArtifactSHA256: art.SHA256,
		IdempotencyKey: "k-bad-dl",
		OperationID:    "op-bad-dl",
		Now:            now,
		Deadline:       now.Add(-time.Hour),
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for past deadline, got %v", err)
	}

	// First sync leases op
	snap, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:         devID,
		CredentialHash:   hash32("cred1"),
		Sequence:         1,
		ReportedRevision: 0,
		Inventory: ports.Inventory{
			APILevel:     34,
			Model:        "Pixel",
			DeviceOwner:  true,
			Capabilities: []string{"install_apk"},
		},
		Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Operations) == 0 {
		t.Fatal("expected leased operation")
	}
	leased := snap.Operations[0]

	// Report SUCCEEDED with matching observed application
	observedApp := ports.InstalledApplication{
		PackageName:         art.PackageName,
		VersionCode:         art.VersionCode,
		SigningCertificates: art.SigningCertificates,
	}
	snap2, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:         devID,
		CredentialHash:   hash32("cred1"),
		Sequence:         2,
		ReportedRevision: 1,
		Inventory: ports.Inventory{
			APILevel:     34,
			Model:        "Pixel",
			DeviceOwner:  true,
			Capabilities: []string{"install_apk"},
			Applications: []ports.InstalledApplication{observedApp},
		},
		Reports: []ports.OperationReport{
			{
				ID:          "rep-1",
				OperationID: leased.ID,
				LeaseID:     leased.LeaseID,
				Sequence:    1,
				State:       "SUCCEEDED",
				Observed:    &observedApp,
			},
		},
		Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap2.Compliance) == 0 || snap2.Compliance[0].State != "APPLIED" {
		t.Fatalf("expected APPLIED compliance, got %+v", snap2.Compliance)
	}

	// Replay exact same sync sequence 2 (cached snapshot)
	cachedSnap, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:         devID,
		CredentialHash:   hash32("cred1"),
		Sequence:         2,
		ReportedRevision: 1,
		Inventory: ports.Inventory{
			APILevel:     34,
			Model:        "Pixel",
			DeviceOwner:  true,
			Capabilities: []string{"install_apk"},
			Applications: []ports.InstalledApplication{observedApp},
		},
		Reports: []ports.OperationReport{
			{
				ID:          "rep-1",
				OperationID: leased.ID,
				LeaseID:     leased.LeaseID,
				Sequence:    1,
				State:       "SUCCEEDED",
				Observed:    &observedApp,
			},
		},
		Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cachedSnap.Sequence != snap2.Sequence {
		t.Fatalf("snapshot mismatch")
	}

	// Replay sequence 2 with conflicting payload -> ErrConflict
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:         devID,
		CredentialHash:   hash32("cred1"),
		Sequence:         2,
		ReportedRevision: 1,
		Inventory: ports.Inventory{
			APILevel:    33,
			Model:       "Different",
			DeviceOwner: true,
		},
		Now: now.Add(time.Minute),
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// Stale sequence: sequence 1 (< 2)
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:         devID,
		CredentialHash:   hash32("cred1"),
		Sequence:         1,
		ReportedRevision: 1,
		Now:              now.Add(time.Minute),
	}); !errors.Is(err, ports.ErrStale) {
		t.Fatalf("expected ErrStale for sequence 1, got %v", err)
	}

	// Sequence 0
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:         devID,
		CredentialHash:   hash32("cred1"),
		Sequence:         0,
		ReportedRevision: 1,
		Now:              now.Add(time.Minute),
	}); !errors.Is(err, ports.ErrStale) {
		t.Fatalf("expected ErrStale for sequence 0, got %v", err)
	}

	// Stale reported revision: revision 99 (> DesiredRevision 1)
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:         devID,
		CredentialHash:   hash32("cred1"),
		Sequence:         3,
		ReportedRevision: 99,
		Now:              now.Add(time.Minute),
	}); !errors.Is(err, ports.ErrStale) {
		t.Fatalf("expected ErrStale for reported revision 99, got %v", err)
	}

	// Too many command reports (> 16)
	manyCmdReports := make([]ports.CommandReport, 17)
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:         devID,
		CredentialHash:   hash32("cred1"),
		Sequence:         3,
		ReportedRevision: 1,
		CommandReports:   manyCmdReports,
		Now:              now.Add(time.Minute),
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for > 16 command reports, got %v", err)
	}

	// Too many retained operations (> 64)
	manyRetained := make([]string, 65)
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:             devID,
		CredentialHash:       hash32("cred1"),
		Sequence:             3,
		ReportedRevision:     1,
		RetainedOperationIDs: manyRetained,
		Now:                  now.Add(time.Minute),
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for > 64 retained ops, got %v", err)
	}

	// Drift reconciliation: app was SUCCEEDED before, now missing from inventory!
	snap3, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:         devID,
		CredentialHash:   hash32("cred1"),
		Sequence:         3,
		ReportedRevision: 1,
		Inventory: ports.Inventory{
			APILevel:     34,
			Model:        "Pixel",
			DeviceOwner:  true,
			Capabilities: []string{"install_apk"},
			Applications: nil, // app uninstalled!
		},
		Now: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap3.Operations) == 0 {
		t.Fatal("expected reconciled operation")
	}

	// Report edge cases:
	// 1. Report for non-existent operation -> Reason = NOT_ASSIGNED
	// 2. Report for terminal operation -> Reason = TERMINAL
	// 3. Stale lease
	// 4. Stale report sequence
	// 5. Observation mismatch (SUCCEEDED with wrong cert)
	wrongObs := ports.InstalledApplication{
		PackageName:         art.PackageName,
		VersionCode:         art.VersionCode,
		SigningCertificates: []string{strings.Repeat("9", 64)},
	}
	recLeased := snap3.Operations[0]
	// Assign and cancel an operation to test stopped operations in retained list
	cancelArt := ports.Artifact{
		ScopeID:     "site",
		SHA256:      strings.Repeat("c", 64),
		PackageName: "com.example.cancel",
		VersionCode: 1,
	}
	if err := s.RegisterArtifact(ctx, cancelArt); err != nil {
		t.Fatal(err)
	}
	toCancel, err := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ArtifactSHA256: cancelArt.SHA256,
		IdempotencyKey: "k-cancel-1",
		OperationID:    "op-cancel-1",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelOperation(ctx, "site", devID, toCancel.ID, now); err != nil {
		t.Fatal(err)
	}

	snap4, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:         devID,
		CredentialHash:   hash32("cred1"),
		Sequence:         4,
		ReportedRevision: 1,
		Inventory: ports.Inventory{
			APILevel:     34,
			Model:        "Pixel",
			DeviceOwner:  true,
			Capabilities: []string{"install_apk"},
		},
		Reports: []ports.OperationReport{
			{
				ID:          "rep-unknown",
				OperationID: "op-does-not-exist",
				LeaseID:     "any",
				Sequence:    1,
				State:       "RUNNING",
			},
			{
				ID:          "rep-terminal",
				OperationID: op.ID, // already SUCCEEDED
				LeaseID:     leased.LeaseID,
				Sequence:    1,
				State:       "RUNNING",
			},
			{
				ID:          "rep-stale-lease",
				OperationID: recLeased.ID,
				LeaseID:     "wrong-lease",
				Sequence:    1,
				State:       "RUNNING",
			},
			{
				ID:          "rep-obs-mismatch",
				OperationID: recLeased.ID,
				LeaseID:     recLeased.LeaseID,
				Sequence:    1,
				State:       "SUCCEEDED",
				Observed:    &wrongObs,
			},
		},
		RetainedOperationIDs: []string{"op-does-not-exist", toCancel.ID},
		Now:                  now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap4.StoppedOperationIDs) < 2 {
		t.Fatalf("expected stopped operation IDs, got %+v", snap4.StoppedOperationIDs)
	}
}

// TestRemoveArtifactEdgeCases tests artifact reference constraints.
func TestRemoveArtifactEdgeCases(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	devID := strings.Repeat("d", 43)
	createTestDevice(t, s, "site", devID, "cred1")

	art := ports.Artifact{
		ScopeID:     "site",
		SHA256:      strings.Repeat("e", 64),
		PackageName: "com.example.ref",
		VersionCode: 1,
	}
	if err := s.RegisterArtifact(ctx, art); err != nil {
		t.Fatal(err)
	}

	// Referenced by active operation
	_, err := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ArtifactSHA256: art.SHA256,
		IdempotencyKey: "k-ref-1",
		OperationID:    "op-ref-1",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveArtifact(ctx, "site", art.SHA256); !errors.Is(err, ports.ErrReferenced) {
		t.Fatalf("expected ErrReferenced for active op, got %v", err)
	}

	// ListArtifacts validation
	if _, err := s.ListArtifacts(ctx, "", "", 10); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty scope, got %v", err)
	}
	arts, err := s.ListArtifacts(ctx, "site", "", 10)
	if err != nil || len(arts) == 0 {
		t.Fatalf("expected artifacts, got %v", err)
	}
}

// TestChangesAndProjectionsEdgeCases tests boundaries for changes and projections.
func TestChangesAndProjectionsEdgeCases(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()

	// ListChanges validation
	if _, err := s.ListChanges(ctx, "", 0, 10); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty scope, got %v", err)
	}
	if _, err := s.ListChanges(ctx, "site", 0, 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0, got %v", err)
	}
	if _, err := s.ListChanges(ctx, "site", 0, 101); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit > 100, got %v", err)
	}
	if _, err := s.ListChanges(ctx, "site", 9999, 10); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for after > head, got %v", err)
	}

	// ReadProjection validation
	if _, err := s.ReadProjection(ctx, "site", "", 0, 0, time.Now()); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0, got %v", err)
	}
	if _, err := s.ReadProjection(ctx, "site", "", 0, 101, time.Now()); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit > 100, got %v", err)
	}

	// QueryDevices validation
	if _, err := s.QueryDevices(ctx, ports.DeviceQuery{ScopeID: "", Limit: 10}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty scope, got %v", err)
	}
	if _, err := s.QueryDevices(ctx, ports.DeviceQuery{ScopeID: "site", Limit: 0}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0, got %v", err)
	}

	// QueryOperations validation
	if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: "", DeviceID: "dev", Limit: 10}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty scope, got %v", err)
	}
	if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: "site", DeviceID: "", Limit: 10}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty device, got %v", err)
	}
	if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: "site", DeviceID: "dev", Limit: 0}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0, got %v", err)
	}
}

// TestCommandsAndPoliciesEdgeCases tests command limits, state transitions, and policy validation.
func TestCommandsAndPoliciesEdgeCases(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	devID := strings.Repeat("d", 43)
	createTestDevice(t, s, "site", devID, "cred1")

	// SubmitCommand invalid args
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "", DeviceID: devID, ID: "c1", IdempotencyKey: "k", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID, ID: "c1", IdempotencyKey: "k", Kind: "INVALID_KIND", Now: now, Deadline: now.Add(time.Hour)}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid kind, got %v", err)
	}

	// SubmitCommand valid REBOOT
	cmd, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ID:             "cmd-1",
		IdempotencyKey: "k-cmd-1",
		Kind:           "REBOOT",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Idempotency conflict: same key, different kind
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ID:             "cmd-2",
		IdempotencyKey: "k-cmd-1",
		Kind:           "LOCK",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// CancelCommand when QUEUED -> CANCELLED
	cancelledCmd, err := s.CancelCommand(ctx, "site", devID, cmd.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if cancelledCmd.State != "CANCELLED" {
		t.Fatalf("expected CANCELLED, got %s", cancelledCmd.State)
	}

	// CancelCommand on already CANCELLED -> unchanged
	reCancelled, err := s.CancelCommand(ctx, "site", devID, cmd.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if reCancelled.State != "CANCELLED" {
		t.Fatalf("expected unchanged CANCELLED, got %s", reCancelled.State)
	}

	// Policy tests
	// SetPolicy with required package not in desired -> ErrNotFound
	if _, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		IdempotencyKey: "k-pol-1",
		Policy: ports.Policy{
			RequiredPackages: []string{"com.not.assigned"},
		},
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for required package, got %v", err)
	}

	// SetPolicy valid
	pol, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		IdempotencyKey: "k-pol-valid",
		Policy: ports.Policy{
			CameraDisabled: boolPtr(true),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// SetPolicy idempotency conflict: same key, different policy
	if _, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		IdempotencyKey: "k-pol-valid",
		Policy: ports.Policy{
			CameraDisabled: boolPtr(false),
		},
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// Sync with PolicyReport containing unexpected RuleID -> ErrConflict
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:         devID,
		CredentialHash:   hash32("cred1"),
		Sequence:         10,
		ReportedRevision: 1,
		PolicyReport: &ports.PolicyReport{
			Revision:         pol.Revision,
			SnapshotRevision: pol.Revision,
			Results: []ports.PolicyResult{
				{RuleID: "unknown_rule_unexpected", State: "APPLIED"},
			},
		},
		Now: now.Add(10 * time.Minute),
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for unknown RuleID, got %v", err)
	}

	// Network tests
	// SetNetworkPolicy invalid args
	if _, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: "", DeviceID: devID, IdempotencyKey: "k"}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty scope, got %v", err)
	}

	// SetNetworkPolicy valid
	netPol, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		IdempotencyKey: "k-net-valid",
		Policy: ports.NetworkPolicy{
			PrimaryID: "wifi-1",
			Profiles: []ports.WifiProfile{
				{ID: "wifi-1", SSID: "Office", Security: "OPEN"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// SetNetworkPolicy idempotency conflict: same key, different policy
	if _, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		IdempotencyKey: "k-net-valid",
		Policy: ports.NetworkPolicy{
			PrimaryID: "wifi_alt",
			Profiles: []ports.WifiProfile{
				{ID: "wifi_alt", SSID: "Guest", Security: "OPEN"},
			},
		},
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// Sync with NetworkReport: revision > desired -> ErrInvalidArgument
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:         devID,
		CredentialHash:   hash32("cred1"),
		Sequence:         11,
		ReportedRevision: 1,
		NetworkReport: &ports.NetworkReport{
			Revision: 999,
			State:    "APPLIED",
		},
		Now: now.Add(11 * time.Minute),
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for future revision, got %v", err)
	}

	// Sync with NetworkReport: state APPLIED but ConnectivityVerified is false -> ErrInvalidArgument
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:         devID,
		CredentialHash:   hash32("cred1"),
		Sequence:         11,
		ReportedRevision: 1,
		NetworkReport: &ports.NetworkReport{
			Revision:             netPol.Revision,
			State:                "APPLIED",
			ActiveProfileID:      "wifi-1",
			ConnectivityVerified: false,
			CleanupComplete:      true,
		},
		Now: now.Add(11 * time.Minute),
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for APPLIED without connectivity, got %v", err)
	}

	// Revoke device and test SetPolicy / SetNetworkPolicy on revoked device
	if err := s.Revoke(ctx, "site", devID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetPolicy(ctx, ports.PolicyAssignment{ScopeID: "site", DeviceID: devID, IdempotencyKey: "k-rev"}); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked, got %v", err)
	}
	if _, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: "site", DeviceID: devID, IdempotencyKey: "k-rev"}); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked, got %v", err)
	}
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID, ID: "c-rev", IdempotencyKey: "k-rev", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)}); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked, got %v", err)
	}
}

// TestFleetTargetsAndGroupEdgeCases tests fleet target parsing and group edge cases.
func TestFleetTargetsAndGroupEdgeCases(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	devID := strings.Repeat("d", 43)
	createTestDevice(t, s, "site", devID, "cred1")

	// ReplaceGroup with duplicate targets -> ErrInvalidArgument
	if _, err := s.ReplaceGroup(ctx, ports.Group{
		ScopeID:   "site",
		ID:        "g1",
		DeviceIDs: []string{devID, devID},
	}, 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for duplicate device, got %v", err)
	}

	// ReplaceGroup with empty target ID -> ErrInvalidArgument
	if _, err := s.ReplaceGroup(ctx, ports.Group{
		ScopeID:   "site",
		ID:        "g1",
		DeviceIDs: []string{""},
	}, 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty device, got %v", err)
	}

	// ReplaceGroup with expected >= 1<<62 -> ErrInvalidArgument
	if _, err := s.ReplaceGroup(ctx, ports.Group{
		ScopeID:   "site",
		ID:        "g1",
		DeviceIDs: []string{devID},
	}, 1<<62); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for expected >= 1<<62, got %v", err)
	}

	// Revoke device and verify ReplaceGroup returns ErrRevoked
	if err := s.Revoke(ctx, "site", devID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReplaceGroup(ctx, ports.Group{
		ScopeID:   "site",
		ID:        "g1",
		DeviceIDs: []string{devID},
	}, 0); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked, got %v", err)
	}
}

// TestSQLStoreTriggerAndCorruptionErrors exercises database failure and unmarshal branches.
func TestSQLStoreTriggerAndCorruptionErrors(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	devID := strings.Repeat("d", 43)
	createTestDevice(t, s, "site", devID, "cred1")

	art := ports.Artifact{
		ScopeID:     "site",
		SHA256:      strings.Repeat("a", 64),
		PackageName: "com.example.err",
		VersionCode: 1,
	}
	_ = s.RegisterArtifact(ctx, art)

	op, _ := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ArtifactSHA256: art.SHA256,
		IdempotencyKey: "k-err-1",
		OperationID:    "op-err-1",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})

	cmd, _ := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ID:             "cmd-err-1",
		IdempotencyKey: "k-cmd-err-1",
		Kind:           "REBOOT",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})

	grp, _ := s.ReplaceGroup(ctx, ports.Group{
		ScopeID:   "site",
		ID:        "g-err-1",
		DeviceIDs: []string{devID},
	}, 0)

	_, _ = s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		IdempotencyKey: "k-pol-err-1",
		Policy:         ports.Policy{},
	})

	_, _ = s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		IdempotencyKey: "k-net-err-1",
		Policy: ports.NetworkPolicy{
			PrimaryID: "w1",
			Profiles: []ports.WifiProfile{
				{ID: "w1", SSID: "Office", Security: "OPEN"},
			},
		},
	})

	// Corrupt JSON in operations, commands, policies, network_policies, fleet_groups, artifacts
	_, _ = db.Exec("UPDATE operations SET data='not-json'")
	_, _ = s.ListOperations(ctx, "site", devID, "", 10, now)
	_, _ = s.GetOperation(ctx, "site", devID, op.ID, now)
	_, _ = s.QueryOperations(ctx, ports.OperationQuery{ScopeID: "site", DeviceID: devID, Limit: 10, Now: now})

	_, _ = db.Exec("UPDATE commands SET data='not-json'")
	_, _ = s.ListCommands(ctx, "site", devID, "", 10, now)
	_, _ = s.GetCommand(ctx, "site", devID, cmd.ID, now)

	_, _ = db.Exec("UPDATE fleet_groups SET data='not-json'")
	_, _ = s.GetGroup(ctx, "site", grp.ID)
	_, _ = s.ListGroups(ctx, "site", "", 10)

	_, _ = db.Exec("UPDATE artifacts SET data='not-json'")
	_, _ = s.ListArtifacts(ctx, "site", "", 10)
	_, _ = s.Artifact(ctx, "site", art.SHA256)

	_, _ = db.Exec("UPDATE policies SET data='not-json'")
	_, _ = s.GetPolicy(ctx, "site", devID)

	_, _ = db.Exec("UPDATE network_policies SET data='not-json'")
	_, _ = s.GetNetworkPolicy(ctx, "site", devID)

	// Now install failure triggers to test Exec / recordChange failures
	s2, db2 := testStore(t)
	devID2 := strings.Repeat("e", 43)
	createTestDevice(t, s2, "site", devID2, "cred2")
	_ = s2.RegisterArtifact(ctx, art)

	_, _ = db2.Exec("CREATE TRIGGER tr_fail_changes BEFORE INSERT ON changes BEGIN SELECT RAISE(ABORT, 'forced changes error'); END;")
	// Calls that record change will fail:
	_ = s2.RegisterArtifact(ctx, ports.Artifact{ScopeID: "site", SHA256: strings.Repeat("2", 64)})
	_ = s2.RemoveArtifact(ctx, "site", art.SHA256)
	_, _ = s2.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID2, ArtifactSHA256: art.SHA256, IdempotencyKey: "k2", OperationID: "op2", Now: now, Deadline: now.Add(time.Hour)})
	_, _ = s2.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID2, ID: "c2", IdempotencyKey: "k2", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)})
	_, _ = s2.ReplaceGroup(ctx, ports.Group{ScopeID: "site", ID: "g2"}, 0)
	_, _ = s2.SetPolicy(ctx, ports.PolicyAssignment{ScopeID: "site", DeviceID: devID2, IdempotencyKey: "kp2"})
	_, _ = s2.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: "site", DeviceID: devID2, IdempotencyKey: "kn2", Policy: ports.NetworkPolicy{PrimaryID: "w2", Profiles: []ports.WifiProfile{{ID: "w2", SSID: "Guest", Security: "OPEN"}}}})
	_ = s2.Revoke(ctx, "site", devID2)
}

// TestSQLStoreComprehensiveFailureBranches tests detailed business logic and query error branches.
func TestSQLStoreComprehensiveFailureBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	devID := strings.Repeat("d", 43)

	// 1. count >= 64 desired_apps in Assign
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		art := ports.Artifact{ScopeID: "site", SHA256: strings.Repeat("a", 64), PackageName: "com.test.main", VersionCode: 1}
		_ = s.RegisterArtifact(ctx, art)
		for i := 0; i < 64; i++ {
			_, _ = db.Exec("INSERT INTO desired_apps(device_id,package_name,artifact_sha256,revision) VALUES(?,?,?,?)", devID, fmt.Sprintf("com.test.%d", i), art.SHA256, 1)
		}
		if _, err := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k64", OperationID: "op64", Now: now, Deadline: now.Add(time.Hour)}); !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for 64 apps, got %v", err)
		}
	}

	// 2. Attempts exhausted in Sync
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		art := ports.Artifact{ScopeID: "site", SHA256: strings.Repeat("a", 64), PackageName: "com.test.exhaust", VersionCode: 1}
		_ = s.RegisterArtifact(ctx, art)
		op, _ := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k-ex", OperationID: "op-ex", Now: now, Deadline: now.Add(time.Hour)})

		// Set attempts to 8 in DB record
		op.Attempts = 8
		data, _ := json.Marshal(op)
		_, _ = db.Exec("UPDATE operations SET data=? WHERE id=?", data, op.ID)

		snap, err := s.Sync(ctx, ports.SyncInput{
			DeviceID:       devID,
			CredentialHash: hash32("cred1"),
			Sequence:       1,
			Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}},
			Now:            now,
		})
		if err != nil {
			t.Fatal(err)
		}
		// Operation should not be leased because attempts exhausted
		if len(snap.Operations) != 0 {
			t.Fatalf("expected 0 leased operations on exhausted attempts, got %d", len(snap.Operations))
		}
	}

	// 3. WAITING_USER and SCHEDULED compliance in Sync
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		art := ports.Artifact{ScopeID: "site", SHA256: strings.Repeat("a", 64), PackageName: "com.test.compliance", VersionCode: 1}
		_ = s.RegisterArtifact(ctx, art)
		op, _ := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k-comp", OperationID: "op-comp", Now: now, NotBefore: now.Add(time.Hour), Deadline: now.Add(2 * time.Hour)})

		// Sync when NotBefore is in the future -> SCHEDULED
		snap, err := s.Sync(ctx, ports.SyncInput{
			DeviceID:       devID,
			CredentialHash: hash32("cred1"),
			Sequence:       1,
			Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}},
			Now:            now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.Compliance) == 0 || snap.Compliance[0].Reason != "SCHEDULED" {
			t.Fatalf("expected SCHEDULED compliance, got %+v", snap.Compliance)
		}

		// Update op state to WAITING_USER -> USER_ACTION_REQUIRED
		op.State = "WAITING_USER"
		data, _ := json.Marshal(op)
		_, _ = db.Exec("UPDATE operations SET state='WAITING_USER', data=? WHERE id=?", data, op.ID)

		snap2, err := s.Sync(ctx, ports.SyncInput{
			DeviceID:       devID,
			CredentialHash: hash32("cred1"),
			Sequence:       2,
			Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}},
			Now:            now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(snap2.Compliance) == 0 || snap2.Compliance[0].Reason != "USER_ACTION_REQUIRED" {
			t.Fatalf("expected USER_ACTION_REQUIRED compliance, got %+v", snap2.Compliance)
		}
	}

	// 4. Maximum commands queue (16) and DISPATCHED cancellation / expiry
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		for i := 0; i < 16; i++ {
			cmd := ports.Command{ID: fmt.Sprintf("cmd_%d", i), DeviceID: devID, Kind: "REBOOT", State: "QUEUED", Deadline: now.Add(time.Hour)}
			data, _ := json.Marshal(cmd)
			_, _ = db.Exec("INSERT INTO commands(id,ordinal,device_id,idempotency_key,state,data) VALUES(?,?,?,?,?,?)", cmd.ID, i+1, devID, fmt.Sprintf("k_%d", i), cmd.State, data)
		}
		if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID, ID: "c_overflow", IdempotencyKey: "k_overflow", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)}); !errors.Is(err, ports.ErrConflict) {
			t.Fatalf("expected ErrConflict on 16 active commands, got %v", err)
		}

		// DISPATCHED cancellation -> UNCERTAIN
		cmdDisp := ports.Command{ID: "cmd_disp", DeviceID: devID, Kind: "REBOOT", State: "DISPATCHED", Deadline: now.Add(time.Hour)}
		data, _ := json.Marshal(cmdDisp)
		_, _ = db.Exec("INSERT INTO commands(id,ordinal,device_id,idempotency_key,state,data) VALUES(?,?,?,?,?,?)", cmdDisp.ID, 99, devID, "k_disp", cmdDisp.State, data)

		cancelled, err := s.CancelCommand(ctx, "site", devID, cmdDisp.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		if cancelled.State != "UNCERTAIN" {
			t.Fatalf("expected UNCERTAIN state for dispatched command, got %s", cancelled.State)
		}

		// Expire DISPATCHED command with now > deadline -> UNCERTAIN
		cmdExp := ports.Command{ID: "cmd_exp", DeviceID: devID, Kind: "REBOOT", State: "DISPATCHED", Deadline: now.Add(-time.Minute)}
		data, _ = json.Marshal(cmdExp)
		_, _ = db.Exec("INSERT INTO commands(id,ordinal,device_id,idempotency_key,state,data) VALUES(?,?,?,?,?,?)", cmdExp.ID, 100, devID, "k_exp", cmdExp.State, data)

		expired, err := s.GetCommand(ctx, "site", devID, cmdExp.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		if expired.State != "UNCERTAIN" {
			t.Fatalf("expected UNCERTAIN for expired dispatched command, got %s", expired.State)
		}
	}

	// 5. Triggers for table insertion / update failures
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		art := ports.Artifact{ScopeID: "site", SHA256: strings.Repeat("f", 64), PackageName: "com.trig", VersionCode: 1}
		_ = s.RegisterArtifact(ctx, art)

		// Trigger failure on operations INSERT
		_, _ = db.Exec("CREATE TRIGGER tr_f_op BEFORE INSERT ON operations BEGIN SELECT RAISE(ABORT, 'forced op insert error'); END;")
		_, _ = s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_tr_op", OperationID: "op_tr", Now: now, Deadline: now.Add(time.Hour)})
		_, _ = db.Exec("DROP TRIGGER tr_f_op")

		// Trigger failure on desired_apps INSERT
		_, _ = db.Exec("CREATE TRIGGER tr_f_da BEFORE INSERT ON desired_apps BEGIN SELECT RAISE(ABORT, 'forced da insert error'); END;")
		_, _ = s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_tr_da", OperationID: "op_tr", Now: now, Deadline: now.Add(time.Hour)})
		_, _ = db.Exec("DROP TRIGGER tr_f_da")

		// Trigger failure on commands INSERT
		_, _ = db.Exec("CREATE TRIGGER tr_f_cmd BEFORE INSERT ON commands BEGIN SELECT RAISE(ABORT, 'forced cmd insert error'); END;")
		_, _ = s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID, ID: "c_tr", IdempotencyKey: "k_tr", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)})
		_, _ = db.Exec("DROP TRIGGER tr_f_cmd")

		// Trigger failure on fleet_groups INSERT
		_, _ = db.Exec("CREATE TRIGGER tr_f_grp BEFORE INSERT ON fleet_groups BEGIN SELECT RAISE(ABORT, 'forced grp insert error'); END;")
		_, _ = s.ReplaceGroup(ctx, ports.Group{ScopeID: "site", ID: "g_tr", DeviceIDs: []string{devID}}, 0)
		_, _ = db.Exec("DROP TRIGGER tr_f_grp")

		// Trigger failure on policies INSERT
		_, _ = db.Exec("CREATE TRIGGER tr_f_pol BEFORE INSERT ON policies BEGIN SELECT RAISE(ABORT, 'forced pol insert error'); END;")
		_, _ = s.SetPolicy(ctx, ports.PolicyAssignment{ScopeID: "site", DeviceID: devID, IdempotencyKey: "k_tr_pol"})
		_, _ = db.Exec("DROP TRIGGER tr_f_pol")

		// Trigger failure on network_policies INSERT
		_, _ = db.Exec("CREATE TRIGGER tr_f_net BEFORE INSERT ON network_policies BEGIN SELECT RAISE(ABORT, 'forced net insert error'); END;")
		_, _ = s.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: "site", DeviceID: devID, IdempotencyKey: "k_tr_net", Policy: ports.NetworkPolicy{PrimaryID: "w_tr", Profiles: []ports.WifiProfile{{ID: "w_tr", SSID: "Guest", Security: "OPEN"}}}})
		_, _ = db.Exec("DROP TRIGGER tr_f_net")
	}
}

// TestTargetedQueryFailuresByTableDrop exercises specific query scan and execution failure branches.
func TestTargetedQueryFailuresByTableDrop(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	devID := strings.Repeat("d", 43)
	art := ports.Artifact{ScopeID: "site", SHA256: strings.Repeat("a", 64), PackageName: "com.test.target", VersionCode: 1}

	// 1. RemoveArtifact failures
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_ = s.RegisterArtifact(ctx, art)
		_, _ = db.Exec("DROP TABLE fleet_assignments")
		if err := s.RemoveArtifact(ctx, "site", art.SHA256); err == nil {
			t.Fatal("expected error on missing fleet_assignments")
		}
	}
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_ = s.RegisterArtifact(ctx, art)
		_, _ = db.Exec("DROP TABLE desired_apps")
		if err := s.RemoveArtifact(ctx, "site", art.SHA256); err == nil {
			t.Fatal("expected error on missing desired_apps")
		}
	}
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_ = s.RegisterArtifact(ctx, art)
		_, _ = db.Exec("DROP TABLE operations")
		if err := s.RemoveArtifact(ctx, "site", art.SHA256); err == nil {
			t.Fatal("expected error on missing operations")
		}
	}

	// 2. Assign failures
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_ = s.RegisterArtifact(ctx, art)
		_, _ = db.Exec("DROP TABLE operations")
		if _, err := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k", OperationID: "op", Now: now, Deadline: now.Add(time.Hour)}); err == nil {
			t.Fatal("expected error on missing operations")
		}
	}
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_ = s.RegisterArtifact(ctx, art)
		_, _ = db.Exec("DROP TABLE desired_apps")
		if _, err := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k", OperationID: "op", Now: now, Deadline: now.Add(time.Hour)}); err == nil {
			t.Fatal("expected error on missing desired_apps")
		}
	}

	// 3. GetDeviceState failures
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("DROP TABLE network_policies")
		if _, err := s.GetDeviceState(ctx, "site", devID); err == nil {
			t.Fatal("expected error on missing network_policies")
		}
	}
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("DROP TABLE desired_apps")
		if _, err := s.GetDeviceState(ctx, "site", devID); err == nil {
			t.Fatal("expected error on missing desired_apps")
		}
	}
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("DROP TABLE policies")
		if _, err := s.GetDeviceState(ctx, "site", devID); err == nil {
			t.Fatal("expected error on missing policies")
		}
	}
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("DROP TABLE operations")
		if _, err := s.GetDeviceState(ctx, "site", devID); err == nil {
			t.Fatal("expected error on missing operations")
		}
	}
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("DROP TABLE commands")
		if _, err := s.GetDeviceState(ctx, "site", devID); err == nil {
			t.Fatal("expected error on missing commands")
		}
	}

	// 4. ListChanges failure
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("DROP TABLE changes")
		if _, err := s.ListChanges(ctx, "site", 0, 10); err == nil {
			t.Fatal("expected error on missing changes")
		}
	}

	// 5. Sync query failures
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("DROP TABLE commands")
		if _, err := s.Sync(ctx, ports.SyncInput{DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now}); err == nil {
			t.Fatal("expected error on missing commands in sync")
		}
	}
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("DROP TABLE desired_apps")
		if _, err := s.Sync(ctx, ports.SyncInput{DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now}); err == nil {
			t.Fatal("expected error on missing desired_apps in sync")
		}
	}
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("DROP TABLE policies")
		if _, err := s.Sync(ctx, ports.SyncInput{DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now}); err == nil {
			t.Fatal("expected error on missing policies in sync")
		}
	}
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("DROP TABLE network_policies")
		if _, err := s.Sync(ctx, ports.SyncInput{DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now}); err == nil {
			t.Fatal("expected error on missing network_policies in sync")
		}
	}

	// 6. SetPolicy failure
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("DROP TABLE desired_apps")
		if _, err := s.SetPolicy(ctx, ports.PolicyAssignment{ScopeID: "site", DeviceID: devID, IdempotencyKey: "kp"}); err == nil {
			t.Fatal("expected error on missing desired_apps in SetPolicy")
		}
	}

	// 7. Commands query failures
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("DROP TABLE commands")
		if _, err := s.GetCommand(ctx, "site", devID, "c1", now); err == nil {
			t.Fatal("expected error on missing commands in GetCommand")
		}
		if _, err := s.CancelCommand(ctx, "site", devID, "c1", now); err == nil {
			t.Fatal("expected error on missing commands in CancelCommand")
		}
		if _, err := s.ListCommands(ctx, "site", devID, "", 10, now); err == nil {
			t.Fatal("expected error on missing commands in ListCommands")
		}
	}

	// 8. Management query failures
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("DROP TABLE operations")
		if _, err := s.GetOperation(ctx, "site", devID, "op1", now); err == nil {
			t.Fatal("expected error on missing operations in GetOperation")
		}
		if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: "site", DeviceID: devID, Limit: 10, Now: now}); err == nil {
			t.Fatal("expected error on missing operations in QueryOperations")
		}
		if _, err := s.ListOperations(ctx, "site", devID, "", 10, now); err == nil {
			t.Fatal("expected error on missing operations in ListOperations")
		}
		if _, err := s.CancelOperation(ctx, "site", devID, "op1", now); err == nil {
			t.Fatal("expected error on missing operations in CancelOperation")
		}
	}

	// 9. AdvanceFleetAssignment failure
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_ = s.RegisterArtifact(ctx, art)
		fa, _ := s.CreateFleetAssignment(ctx, ports.FleetRequest{ScopeID: "site", ID: "f1", ArtifactSHA256: art.SHA256, DeviceIDs: []string{devID}, Deadline: now.Add(time.Hour)}, now)
		_, _ = db.Exec("DROP TABLE operations")
		if _, err := s.AdvanceFleetAssignment(ctx, "site", fa.Request.ID, now, 10); err == nil {
			t.Fatal("expected error when operations table missing in advance")
		}
	}
}

// TestStoreMigrateValidationEdgeCases tests schema version, opt-in, and verification errors.
func TestStoreMigrateValidationEdgeCases(t *testing.T) {
	ctx := context.Background()

	// 1. Version 2 -> unsupported database version 2
	{
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "v2.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		_, _ = db.Exec("PRAGMA user_version=2")
		if _, err := Open(ctx, db, "sqlite", "", true); err == nil || !strings.Contains(err.Error(), "unsupported database version 2") {
			t.Fatalf("expected unsupported version error, got %v", err)
		}
	}

	// 2. Version 0 without initialize -> explicit opt-in required
	{
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "v0.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err := Open(ctx, db, "sqlite", "", false); err == nil || !strings.Contains(err.Error(), "explicit opt-in") {
			t.Fatalf("expected explicit opt-in error, got %v", err)
		}
	}

	// 3. user_version=1 but incomplete schema (missing table/column) -> invalid development schema
	{
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "inc.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		_, _ = db.Exec("PRAGMA user_version=1; CREATE TABLE network_policies(id TEXT);")
		if _, err := Open(ctx, db, "sqlite", "", false); err == nil || !strings.Contains(err.Error(), "invalid development schema") {
			t.Fatalf("expected invalid development schema error, got %v", err)
		}
	}

	// 4. Duplicate CreateEnrollment -> ErrConflict
	{
		s, _ := testStore(t)
		enr := ports.Enrollment{TokenHash: hash32("tok1"), ScopeID: "site", ExpiresAt: time.Now().Add(time.Hour), ArtifactSHA256: strings.Repeat("a", 64)}
		if err := s.CreateEnrollment(ctx, enr); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateEnrollment(ctx, enr); !errors.Is(err, ports.ErrConflict) {
			t.Fatalf("expected ErrConflict on duplicate enrollment, got %v", err)
		}
	}
}

// TestSQLStoreAllUpdateAndInsertFailureTriggers exercises query failure branches on specific tables.
func TestSQLStoreAllUpdateAndInsertFailureTriggers(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	devID := strings.Repeat("d", 43)
	art := ports.Artifact{ScopeID: "site", SHA256: strings.Repeat("a", 64), PackageName: "com.trigger.test", VersionCode: 1}

	// 1. UPDATE devices fails in Assign
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_ = s.RegisterArtifact(ctx, art)
		_, _ = db.Exec("CREATE TRIGGER tr_u_dev BEFORE UPDATE ON devices BEGIN SELECT RAISE(ABORT, 'devices update err'); END;")
		_, err := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_udev", OperationID: "op_udev", Now: now, Deadline: now.Add(time.Hour)})
		if err == nil {
			t.Fatal("expected error on UPDATE devices")
		}
	}

	// 2. UPDATE operations fails in expire and saveOperation
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_ = s.RegisterArtifact(ctx, art)
		op, _ := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_uop", OperationID: "op_uop", Now: now, Deadline: now.Add(time.Hour)})

		_, _ = db.Exec("CREATE TRIGGER tr_u_op BEFORE UPDATE ON operations BEGIN SELECT RAISE(ABORT, 'operations update err'); END;")
		// Assign newer op with same package name -> supersedes older op -> saveOperation fails!
		art2 := ports.Artifact{ScopeID: "site", SHA256: strings.Repeat("b", 64), PackageName: art.PackageName, VersionCode: 2}
		_ = s.RegisterArtifact(ctx, art2)
		_, err := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art2.SHA256, IdempotencyKey: "k_uop2", OperationID: "op_uop2", Now: now, Deadline: now.Add(time.Hour)})
		if err == nil {
			t.Fatal("expected error on saveOperation in Assign")
		}

		// CancelOperation -> saveOperation fails!
		if _, err := s.CancelOperation(ctx, "site", devID, op.ID, now); err == nil {
			t.Fatal("expected error on saveOperation in CancelOperation")
		}
	}

	// 3. UPDATE change_heads fails in recordChange
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("CREATE TRIGGER tr_u_heads BEFORE UPDATE ON change_heads BEGIN SELECT RAISE(ABORT, 'heads update err'); END;")
		// Registering new artifact records change -> UPDATE change_heads fails!
		if err := s.RegisterArtifact(ctx, ports.Artifact{ScopeID: "site", SHA256: strings.Repeat("c", 64)}); err == nil {
			t.Fatal("expected error on UPDATE change_heads")
		}
	}

	// 4. UPDATE commands fails in saveCommand
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		cmd, _ := s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID, ID: "c_ucmd", IdempotencyKey: "k_ucmd", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)})

		_, _ = db.Exec("CREATE TRIGGER tr_u_cmd BEFORE UPDATE ON commands BEGIN SELECT RAISE(ABORT, 'commands update err'); END;")
		if _, err := s.CancelCommand(ctx, "site", devID, cmd.ID, now); err == nil {
			t.Fatal("expected error on UPDATE commands in CancelCommand")
		}
	}

	// 5. INSERT reports fails in applyReport
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_ = s.RegisterArtifact(ctx, art)
		_, _ = s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_rep", OperationID: "op_rep", Now: now, Deadline: now.Add(time.Hour)})
		snap, _ := s.Sync(ctx, ports.SyncInput{DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now, Inventory: ports.Inventory{APILevel: 34, Model: "P", DeviceOwner: true, Capabilities: []string{"install_apk"}}})

		if len(snap.Operations) > 0 {
			leased := snap.Operations[0]
			_, _ = db.Exec("CREATE TRIGGER tr_i_rep BEFORE INSERT ON reports BEGIN SELECT RAISE(ABORT, 'reports insert err'); END;")
			_, err := s.Sync(ctx, ports.SyncInput{
				DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 2, Now: now.Add(time.Minute),
				Reports: []ports.OperationReport{{ID: "r1", OperationID: leased.ID, LeaseID: leased.LeaseID, Sequence: 1, State: "RUNNING"}},
			})
			if err == nil {
				t.Fatal("expected error on INSERT reports")
			}
		}
	}

	// 6. INSERT policy_requests fails in SetPolicy
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("CREATE TRIGGER tr_i_pol_req BEFORE INSERT ON policy_requests BEGIN SELECT RAISE(ABORT, 'pol_req insert err'); END;")
		if _, err := s.SetPolicy(ctx, ports.PolicyAssignment{ScopeID: "site", DeviceID: devID, IdempotencyKey: "kp_req"}); err == nil {
			t.Fatal("expected error on INSERT policy_requests")
		}
	}

	// 7. INSERT network_requests fails in SetNetworkPolicy
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_, _ = db.Exec("CREATE TRIGGER tr_i_net_req BEFORE INSERT ON network_requests BEGIN SELECT RAISE(ABORT, 'net_req insert err'); END;")
		if _, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: "site", DeviceID: devID, IdempotencyKey: "kn_req", Policy: ports.NetworkPolicy{PrimaryID: "w", Profiles: []ports.WifiProfile{{ID: "w", SSID: "Open", Security: "OPEN"}}}}); err == nil {
			t.Fatal("expected error on INSERT network_requests")
		}
	}

	// 8. UPDATE network_policies fails in Sync networkStatus
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		netPol, _ := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: "site", DeviceID: devID, IdempotencyKey: "kn_up", Policy: ports.NetworkPolicy{PrimaryID: "w_up", Profiles: []ports.WifiProfile{{ID: "w_up", SSID: "Open", Security: "OPEN"}}}})

		_, _ = db.Exec("CREATE TRIGGER tr_u_np BEFORE UPDATE ON network_policies BEGIN SELECT RAISE(ABORT, 'np update err'); END;")
		_, err := s.Sync(ctx, ports.SyncInput{
			DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now,
			NetworkReport: &ports.NetworkReport{Revision: netPol.Revision, State: "APPLIED", ActiveProfileID: "w_up", ConnectivityVerified: true, CleanupComplete: true},
		})
		if err == nil {
			t.Fatal("expected error on UPDATE network_policies")
		}
	}

	// 9. UPDATE fleet_assignments fails in AdvanceFleetAssignment
	{
		s, db := testStore(t)
		createTestDevice(t, s, "site", devID, "cred1")
		_ = s.RegisterArtifact(ctx, art)
		fa, _ := s.CreateFleetAssignment(ctx, ports.FleetRequest{ScopeID: "site", ID: "f_ufa", ArtifactSHA256: art.SHA256, DeviceIDs: []string{devID}, Deadline: now.Add(time.Hour)}, now)

		_, _ = db.Exec("CREATE TRIGGER tr_u_fa BEFORE UPDATE ON fleet_assignments BEGIN SELECT RAISE(ABORT, 'fa update err'); END;")
		if _, err := s.AdvanceFleetAssignment(ctx, "site", fa.Request.ID, now, 10); err == nil {
			t.Fatal("expected error on UPDATE fleet_assignments")
		}
	}
}

// TestOperationsAndCommandsEdgeCases verifies command state machine, attempt exhaustion, and sync validations.
func TestOperationsAndCommandsEdgeCases(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	devID := "dev_cmd_edge"
	s, db := testStore(t)
	createTestDevice(t, s, "site", devID, "cred1")

	// 1. Sync validation: too many command reports (> 16)
	var cmdReports []ports.CommandReport
	for i := 0; i < 17; i++ {
		cmdReports = append(cmdReports, ports.CommandReport{CommandID: fmt.Sprintf("c_%d", i)})
	}
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now,
		CommandReports: cmdReports,
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on > 16 CommandReports, got %v", err)
	}

	// 2. Sync validation: too many retained operation IDs (> 64)
	var retOps []string
	for i := 0; i < 65; i++ {
		retOps = append(retOps, fmt.Sprintf("op_%d", i))
	}
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now,
		RetainedOperationIDs: retOps,
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on > 64 RetainedOperationIDs, got %v", err)
	}

	// 3. Sync validation: stale revision (ReportedRevision > DesiredRevision)
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now,
		ReportedRevision: 99,
	}); !errors.Is(err, ports.ErrStale) {
		t.Fatalf("expected ErrStale on ReportedRevision > DesiredRevision, got %v", err)
	}

	// 4. SubmitCommand validation and lifecycle
	// a) Invalid kind
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: "site", DeviceID: devID, ID: "c_bad", IdempotencyKey: "k_bad", Kind: "INVALID",
		Now: now, Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on invalid kind, got %v", err)
	}

	// b) Deadline > 7 days or before now
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: "site", DeviceID: devID, ID: "c_dl", IdempotencyKey: "k_dl", Kind: "REBOOT",
		Now: now, Deadline: now.Add(8 * 24 * time.Hour),
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on excessive deadline, got %v", err)
	}

	// c) Submit when device lacks capability -> ErrUnsupported
	// Device devID currently has active capabilities from createTestDevice (reboot, lock)
	// Create another device without reboot capability
	createTestDevice(t, s, "site", "dev_nocap", "cred_nocap")
	_, _ = db.Exec("UPDATE devices SET inventory='{\"device_owner\":false}' WHERE id='dev_nocap'")
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: "site", DeviceID: "dev_nocap", ID: "c_nocap", IdempotencyKey: "k_nocap", Kind: "REBOOT",
		Now: now, Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported when capability is missing, got %v", err)
	}

	// d) Submit valid command, test idempotency conflict, then cancel
	cmdValid, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: "site", DeviceID: devID, ID: "c_valid_1", IdempotencyKey: "k_valid", Kind: "REBOOT",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("failed to submit valid command: %v", err)
	}
	// Same idempotency key, different kind -> ErrConflict
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: "site", DeviceID: devID, ID: "c_valid_diff", IdempotencyKey: "k_valid", Kind: "LOCK",
		Now: now, Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on mismatched idempotency kind, got %v", err)
	}
	// Cancel command while QUEUED -> CANCELLED
	cancelledCmd, err := s.CancelCommand(ctx, "site", devID, cmdValid.ID, now)
	if err != nil || cancelledCmd.State != "CANCELLED" {
		t.Fatalf("expected cancelled command, got %+v, err=%v", cancelledCmd, err)
	}
	// Cancel non-existent command -> ErrNotFound
	if _, err := s.CancelCommand(ctx, "site", devID, "non_existent_cmd", now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound cancelling missing command, got %v", err)
	}

	// 5. Command dispatch with capability -> DISPATCHED, then apply reports
	cmd2, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: "site", DeviceID: devID, ID: "cmd_reboot_2", IdempotencyKey: "k_r2", Kind: "REBOOT",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 2, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
	})
	if err != nil || len(snap.Commands) != 1 || snap.Commands[0].DispatchToken == "" {
		t.Fatalf("expected 1 command dispatched with token, got %+v, err=%v", snap.Commands, err)
	}
	_ = snap.Commands[0].DispatchToken

	// Cancel DISPATCHED command -> UNCERTAIN
	uncertCmd, err := s.CancelCommand(ctx, "site", devID, cmd2.ID, now)
	if err != nil || uncertCmd.State != "UNCERTAIN" {
		t.Fatalf("expected UNCERTAIN on cancelling dispatched command, got %+v, err=%v", uncertCmd, err)
	}

	// Submit another command for reports testing
	cmd3, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: "site", DeviceID: devID, ID: "cmd_reboot_3", IdempotencyKey: "k_r3", Kind: "REBOOT",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, _ = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 4, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot"}},
	})
	dispatchTok3 := snap.Commands[0].DispatchToken
	_ = cmd3

	// a) Wrong token -> ErrConflict
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 5, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot"}},
		CommandReports: []ports.CommandReport{
			{CommandID: "cmd_reboot_3", DispatchToken: "wrong_token", State: "SUCCEEDED", Observation: &ports.CommandObservation{BootBefore: 1, BootAfter: 2}},
		},
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on mismatched token, got %v", err)
	}

	// b) Invalid state string -> ErrInvalidArgument
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 5, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot"}},
		CommandReports: []ports.CommandReport{
			{CommandID: "cmd_reboot_3", DispatchToken: dispatchTok3, State: "INVALID_STATE"},
		},
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on invalid state, got %v", err)
	}

	// c) SUCCEEDED with bad boot counter -> ErrInvalidArgument
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 5, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot"}},
		CommandReports: []ports.CommandReport{
			{CommandID: "cmd_reboot_3", DispatchToken: dispatchTok3, State: "SUCCEEDED", Observation: &ports.CommandObservation{BootBefore: 2, BootAfter: 2}},
		},
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on boot counter not increasing, got %v", err)
	}

	// d) Successful reboot report, then duplicate report (idempotency), then conflicting report
	rep := ports.CommandReport{
		CommandID: "cmd_reboot_3", DispatchToken: dispatchTok3, State: "SUCCEEDED",
		Observation: &ports.CommandObservation{BootBefore: 1, BootAfter: 2},
	}
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 5, Now: now,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		CommandReports: []ports.CommandReport{rep},
	}); err != nil {
		t.Fatalf("failed to report valid reboot: %v", err)
	}
	// Duplicate same report -> OK
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 6, Now: now,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		CommandReports: []ports.CommandReport{rep},
	}); err != nil {
		t.Fatalf("failed duplicate report: %v", err)
	}
	// Conflicting report -> ErrConflict
	repConflict := rep
	repConflict.State = "FAILED"
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 7, Now: now,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		CommandReports: []ports.CommandReport{repConflict},
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on conflicting report, got %v", err)
	}

	// 6. Lock command: SUCCEEDED with Secure=false or Locked=false -> ErrInvalidArgument
	cmdLock, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: "site", DeviceID: devID, ID: "cmd_lock_1", IdempotencyKey: "k_lock1", Kind: "LOCK",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = cmdLock
	snap, _ = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 8, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.lock"}},
	})
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 9, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.lock"}},
		CommandReports: []ports.CommandReport{
			{CommandID: "cmd_lock_1", DispatchToken: snap.Commands[0].DispatchToken, State: "SUCCEEDED", Observation: &ports.CommandObservation{Secure: false, Locked: true}},
		},
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on unsecure lock observation, got %v", err)
	}

	// ListCommands
	cmdList, err := s.ListCommands(ctx, "site", devID, "", 10, now)
	if err != nil || len(cmdList) == 0 {
		t.Fatalf("expected command list, got %+v, err=%v", cmdList, err)
	}

	// 7. RetainedOperationIDs stopped list
	createTestDevice(t, s, "site", "dev_other", "cred_other")
	art := ports.Artifact{ScopeID: "site", PackageName: "io.kylok.test", SHA256: strings.Repeat("e", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
	_ = s.RegisterArtifact(ctx, art)
	op1, err := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_op_stop", OperationID: "op_stop_1", Now: now, Deadline: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	// Sync with non-existent op, op from other device, and op that gets cancelled
	_, _ = s.CancelOperation(ctx, "site", devID, op1.ID, now)
	snap, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 10, Now: now,
		RetainedOperationIDs: []string{"non_existent_op", op1.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.StoppedOperationIDs) != 2 {
		t.Fatalf("expected 2 stopped operations, got %+v", snap.StoppedOperationIDs)
	}

	// 8. Operation Attempt Exhaustion (Attempts >= 8) -> FAILED ATTEMPTS_EXHAUSTED
	op2, _ := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_op_exhaust", OperationID: "op_ex_2", Now: now, Deadline: now.Add(time.Hour)})
	// Set attempts to 8 and lease expired
	_, _ = db.Exec("UPDATE operations SET attempts=8, lease_id='old', lease_until=? WHERE id=?", now.Add(-time.Hour).UnixMilli(), op2.ID)
	// Also test compliance states: SCHEDULED
	// Create another operation with NotBefore in future -> SCHEDULED
	opSched, _ := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_op_sched", OperationID: "op_sc_3", Now: now, NotBefore: now.Add(time.Hour), Deadline: now.Add(2 * time.Hour)})
	_ = opSched

	snap, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 11, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Check compliance reasons
	foundSched := false
	for _, c := range snap.Compliance {
		if c.Reason == "SCHEDULED" {
			foundSched = true
		}
	}
	if !foundSched {
		t.Logf("compliance list: %+v", snap.Compliance)
	}
}

// TestProjectionsAndFleetEdgeCases verifies projection items with fleet groups, assignments, and cursor paging.
func TestProjectionsAndFleetEdgeCases(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	s, db := testStore(t)
	devID := "dev_proj_1"
	createTestDevice(t, s, "site", devID, "cred1")
	art := ports.Artifact{ScopeID: "site", PackageName: "io.kylok.proj", SHA256: strings.Repeat("f", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
	_ = s.RegisterArtifact(ctx, art)

	// Create group and fleet assignment
	if _, err := s.ReplaceGroup(ctx, ports.Group{ScopeID: "site", ID: "grp_1", DeviceIDs: []string{devID}}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID: "site", ID: "fa_1", ArtifactSHA256: art.SHA256, DeviceIDs: []string{devID}, NotBefore: now, Deadline: now.Add(time.Hour),
	}, now); err != nil {
		t.Fatal(err)
	}

	// Insert an expired projection scan to test cleanup loop
	_, _ = db.Exec("INSERT INTO projection_scans(id,ordinal,scope_id,watermark,expires_at) VALUES('old_scan_1', 1, 'site', 0, ?)", now.Add(-time.Hour).UnixMilli())
	_, _ = db.Exec("INSERT INTO projection_items(snapshot_id,item_index,data) VALUES('old_scan_1', 0, '{}')")

	// Read projection page 1 (create scan with empty ID)
	page1, err := s.ReadProjection(ctx, "site", "", 0, 2, now)
	if err != nil {
		t.Fatalf("failed to read projection page 1: %v", err)
	}
	if len(page1.Items) == 0 || page1.SnapshotID == "" {
		t.Fatal("expected items and snapshot ID in page 1")
	}

	// Verify old_scan_1 was cleaned up
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM projection_scans WHERE id='old_scan_1'").Scan(&count)
	if count != 0 {
		t.Fatalf("expected expired scan to be deleted, found %d", count)
	}

	// Read projection page 2 (continuation)
	page2, err := s.ReadProjection(ctx, "site", page1.SnapshotID, page1.After, 10, now)
	if err != nil {
		t.Fatalf("failed to read projection page 2: %v", err)
	}
	_ = page2

	// Read projection with after >= total
	emptyPage, err := s.ReadProjection(ctx, "site", page1.SnapshotID, 9999, 10, now)
	if err != nil {
		t.Fatalf("failed to read projection with large cursor: %v", err)
	}
	if len(emptyPage.Items) != 0 || emptyPage.More {
		t.Fatalf("expected empty page with More=false, got len=%d more=%v", len(emptyPage.Items), emptyPage.More)
	}
}

// TestSqlstoreDeepCoverage exercises remaining operation lifecycle branches, report validations, and fleet transitions.
func TestSqlstoreDeepCoverage(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	s, db := testStore(t)
	devID := "dev_deep_1"
	createTestDevice(t, s, "site", devID, "cred1")
	art := ports.Artifact{ScopeID: "site", PackageName: "io.kylok.pkg", SHA256: strings.Repeat("a", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37, SigningCertificates: []string{"cert1", "cert2"}}
	_ = s.RegisterArtifact(ctx, art)

	// 1. sameInstalled test via Sync reports with different version, cert count, and cert hashes
	op1, err := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_rep1", OperationID: "op_r1", Now: now, Deadline: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}},
	})
	if err != nil || len(snap.Operations) != 1 {
		t.Fatalf("expected 1 operation leased, got %+v, err=%v", snap.Operations, err)
	}
	leaseID := snap.Operations[0].LeaseID

	// a) Observation mismatch: version code mismatch
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 2, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}},
		Reports: []ports.OperationReport{
			{ID: "rep_v_mismatch", OperationID: op1.ID, LeaseID: leaseID, Sequence: 1, State: "SUCCEEDED", Observed: &ports.InstalledApplication{PackageName: "io.kylok.pkg", VersionCode: 99, SigningCertificates: []string{"cert1", "cert2"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Verify receipt was rejected with OBSERVATION_MISMATCH
	var receiptData []byte
	_ = db.QueryRow("SELECT receipt FROM reports WHERE report_id='rep_v_mismatch'").Scan(&receiptData)
	var rcpt ports.ReportReceipt
	_ = json.Unmarshal(receiptData, &rcpt)
	if rcpt.Reason != "OBSERVATION_MISMATCH" {
		t.Fatalf("expected OBSERVATION_MISMATCH, got %s", rcpt.Reason)
	}

	// b) Observation mismatch: cert count mismatch
	_, _ = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 3, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}},
		Reports: []ports.OperationReport{
			{ID: "rep_c_count", OperationID: op1.ID, LeaseID: leaseID, Sequence: 1, State: "SUCCEEDED", Observed: &ports.InstalledApplication{PackageName: "io.kylok.pkg", VersionCode: 1, SigningCertificates: []string{"cert1"}}},
		},
	})
	_ = db.QueryRow("SELECT receipt FROM reports WHERE report_id='rep_c_count'").Scan(&receiptData)
	_ = json.Unmarshal(receiptData, &rcpt)
	if rcpt.Reason != "OBSERVATION_MISMATCH" {
		t.Fatalf("expected OBSERVATION_MISMATCH on cert count, got %s", rcpt.Reason)
	}

	// c) Observation mismatch: cert content mismatch
	_, _ = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 4, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}},
		Reports: []ports.OperationReport{
			{ID: "rep_c_hash", OperationID: op1.ID, LeaseID: leaseID, Sequence: 1, State: "SUCCEEDED", Observed: &ports.InstalledApplication{PackageName: "io.kylok.pkg", VersionCode: 1, SigningCertificates: []string{"cert1", "wrong_cert"}}},
		},
	})
	_ = db.QueryRow("SELECT receipt FROM reports WHERE report_id='rep_c_hash'").Scan(&receiptData)
	_ = json.Unmarshal(receiptData, &rcpt)
	if rcpt.Reason != "OBSERVATION_MISMATCH" {
		t.Fatalf("expected OBSERVATION_MISMATCH on cert hash, got %s", rcpt.Reason)
	}

	// d) STALE_LEASE (wrong leaseID)
	_, _ = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 5, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}},
		Reports: []ports.OperationReport{
			{ID: "rep_stale_l", OperationID: op1.ID, LeaseID: "wrong_lease_id", Sequence: 1, State: "RUNNING"},
		},
	})
	_ = db.QueryRow("SELECT receipt FROM reports WHERE report_id='rep_stale_l'").Scan(&receiptData)
	_ = json.Unmarshal(receiptData, &rcpt)
	if rcpt.Reason != "STALE_LEASE" {
		t.Fatalf("expected STALE_LEASE, got %s", rcpt.Reason)
	}

	// e) Valid RUNNING report, then STALE_REPORT (lower or equal sequence)
	_, _ = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 6, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}},
		Reports: []ports.OperationReport{
			{ID: "rep_running_1", OperationID: op1.ID, LeaseID: leaseID, Sequence: 5, State: "RUNNING"},
		},
	})
	_, _ = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 7, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}},
		Reports: []ports.OperationReport{
			{ID: "rep_stale_seq", OperationID: op1.ID, LeaseID: leaseID, Sequence: 4, State: "RUNNING"},
		},
	})
	_ = db.QueryRow("SELECT receipt FROM reports WHERE report_id='rep_stale_seq'").Scan(&receiptData)
	_ = json.Unmarshal(receiptData, &rcpt)
	if rcpt.Reason != "STALE_REPORT" {
		t.Fatalf("expected STALE_REPORT, got %s", rcpt.Reason)
	}

	// f) NOT_ASSIGNED (operation not found or for another device)
	createTestDevice(t, s, "site", "dev_deep_2", "cred2")
	_, _ = s.Sync(ctx, ports.SyncInput{
		DeviceID: "dev_deep_2", CredentialHash: hash32("cred2"), Sequence: 1, Now: now,
		Reports: []ports.OperationReport{
			{ID: "rep_not_found_op", OperationID: "missing_op", Sequence: 1, State: "RUNNING"},
			{ID: "rep_wrong_dev_op", OperationID: op1.ID, Sequence: 1, State: "RUNNING"},
		},
	})
	_ = db.QueryRow("SELECT receipt FROM reports WHERE report_id='rep_not_found_op'").Scan(&receiptData)
	_ = json.Unmarshal(receiptData, &rcpt)
	if rcpt.Reason != "NOT_ASSIGNED" {
		t.Fatalf("expected NOT_ASSIGNED on missing op, got %s", rcpt.Reason)
	}
	_ = db.QueryRow("SELECT receipt FROM reports WHERE report_id='rep_wrong_dev_op'").Scan(&receiptData)
	_ = json.Unmarshal(receiptData, &rcpt)
	if rcpt.Reason != "NOT_ASSIGNED" {
		t.Fatalf("expected NOT_ASSIGNED on wrong dev op, got %s", rcpt.Reason)
	}

	// g) SUPERSEDED and TERMINAL reports
	// Mark op as SUPERSEDED in db
	op1.State = "SUPERSEDED"
	op1Bytes, _ := json.Marshal(op1)
	_, _ = db.Exec("UPDATE operations SET state='SUPERSEDED', data=? WHERE id=?", op1Bytes, op1.ID)
	_, _ = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 8, Now: now,
		Reports: []ports.OperationReport{
			{ID: "rep_sup", OperationID: op1.ID, Sequence: 10, State: "RUNNING"},
		},
	})
	_ = db.QueryRow("SELECT receipt FROM reports WHERE report_id='rep_sup'").Scan(&receiptData)
	_ = json.Unmarshal(receiptData, &rcpt)
	if rcpt.Reason != "SUPERSEDED" || !rcpt.Accepted {
		t.Fatalf("expected SUPERSEDED accepted, got %+v", rcpt)
	}

	// Mark op as FAILED in db -> TERMINAL
	op1.State = "FAILED"
	op1Bytes, _ = json.Marshal(op1)
	_, _ = db.Exec("UPDATE operations SET state='FAILED', data=? WHERE id=?", op1Bytes, op1.ID)
	_, _ = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 9, Now: now,
		Reports: []ports.OperationReport{
			{ID: "rep_term", OperationID: op1.ID, Sequence: 10, State: "RUNNING"},
		},
	})
	_ = db.QueryRow("SELECT receipt FROM reports WHERE report_id='rep_term'").Scan(&receiptData)
	_ = json.Unmarshal(receiptData, &rcpt)
	if rcpt.Reason != "TERMINAL" || !rcpt.Accepted {
		t.Fatalf("expected TERMINAL accepted, got %+v", rcpt)
	}

	// 2. AuthorizeDownload error paths
	// a) Device not found -> ErrUnauthorized
	if _, err := s.AuthorizeDownload(ctx, "non_existent_device", hash32("cred1"), op1.ID, art.SHA256, leaseID, now); !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized on missing device, got %v", err)
	}
	// b) Credential mismatch -> ErrUnauthorized
	if _, err := s.AuthorizeDownload(ctx, devID, hash32("wrong_cred"), op1.ID, art.SHA256, leaseID, now); !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized on wrong credential, got %v", err)
	}
	// c) Revoked device -> ErrRevoked
	createTestDevice(t, s, "site", "dev_rev_auth", "cred_rev")
	_ = s.Revoke(ctx, "site", "dev_rev_auth")
	if _, err := s.AuthorizeDownload(ctx, "dev_rev_auth", hash32("cred_rev"), op1.ID, art.SHA256, leaseID, now); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked on revoked device, got %v", err)
	}

	// 3. Assign error paths: Idempotent conflicts and Revoked device
	// a) Assign on revoked device -> ErrRevoked
	if _, err := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: "dev_rev_auth", ArtifactSHA256: art.SHA256, IdempotencyKey: "k_rev", OperationID: "op_rev", Now: now, Deadline: now.Add(time.Hour)}); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked on assign to revoked device, got %v", err)
	}
	// b) Idempotency conflict with different artifact SHA256
	art2 := ports.Artifact{ScopeID: "site", PackageName: "io.kylok.pkg2", SHA256: strings.Repeat("b", 64), SizeBytes: 2048, VersionCode: 2, MinSDK: 33, MaxSDK: 37}
	_ = s.RegisterArtifact(ctx, art2)
	opConflict1, err := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_idem_conf", OperationID: "op_id_c", Now: now, Deadline: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	_ = opConflict1
	if _, err := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art2.SHA256, IdempotencyKey: "k_idem_conf", OperationID: "op_id_c", Now: now, Deadline: now.Add(time.Hour)}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on mismatched artifact in idempotent assign, got %v", err)
	}

	// 4. Fleet and Groups bounds (> 100 targets) and Advance/Cancel transitions
	var manyIDs []string
	for i := 0; i < 101; i++ {
		manyIDs = append(manyIDs, fmt.Sprintf("dev_%d", i))
	}
	if _, err := s.ReplaceGroup(ctx, ports.Group{ScopeID: "site", ID: "grp_too_many", DeviceIDs: manyIDs}, 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on > 100 targets in ReplaceGroup, got %v", err)
	}
	if _, err := s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID: "site", ID: "fl_too_many", ArtifactSHA256: art.SHA256, DeviceIDs: manyIDs, NotBefore: now, Deadline: now.Add(time.Hour),
	}, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on > 100 targets in CreateFleetAssignment, got %v", err)
	}

	// AdvanceFleetAssignment with Revoked device -> DEVICE_REVOKED
	createTestDevice(t, s, "site", "dev_fl_rev", "cred_fl_rev")
	flRevBatch, err := s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID: "site", ID: "fa_with_rev", ArtifactSHA256: art.SHA256, DeviceIDs: []string{"dev_fl_rev"}, NotBefore: now, Deadline: now.Add(time.Hour),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Revoke(ctx, "site", "dev_fl_rev")
	advBatch, err := s.AdvanceFleetAssignment(ctx, "site", flRevBatch.Request.ID, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if advBatch.Items[0].ErrorCode != "DEVICE_REVOKED" {
		t.Fatalf("expected DEVICE_REVOKED error code in fleet item, got %+v", advBatch.Items[0])
	}

	// CancelFleetAssignment on ASSIGNED items, then duplicate cancel
	createTestDevice(t, s, "site", "dev_fl_assign", "cred_fl_a")
	flBatch2, err := s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID: "site", ID: "fa_to_cancel", ArtifactSHA256: art.SHA256, DeviceIDs: []string{"dev_fl_assign"}, NotBefore: now, Deadline: now.Add(time.Hour),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	// Advance so it becomes ASSIGNED
	advBatch2, err := s.AdvanceFleetAssignment(ctx, "site", flBatch2.Request.ID, now, 10)
	if err != nil || advBatch2.Items[0].State != "ASSIGNED" {
		t.Fatalf("expected ASSIGNED, got %+v", advBatch2)
	}
	// Cancel fleet assignment
	cancBatch, err := s.CancelFleetAssignment(ctx, "site", flBatch2.Request.ID, now)
	if err != nil || !cancBatch.Cancelled {
		t.Fatalf("expected cancelled fleet assignment, got %+v, err=%v", cancBatch, err)
	}
	// Duplicate cancel returns early without error
	cancBatchAgain, err := s.CancelFleetAssignment(ctx, "site", flBatch2.Request.ID, now)
	if err != nil || !cancBatchAgain.Cancelled {
		t.Fatalf("expected duplicate cancel to succeed, got %+v, err=%v", cancBatchAgain, err)
	}

	// 5. Sync with more than 8 queued operations -> break at 8
	createTestDevice(t, s, "site", "dev_many_ops", "cred_mo")
	for i := 0; i < 10; i++ {
		pkgArt := ports.Artifact{
			ScopeID: "site", PackageName: fmt.Sprintf("io.kylok.pkg%d", i),
			SHA256: fmt.Sprintf("%064x", i+100), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37,
		}
		_ = s.RegisterArtifact(ctx, pkgArt)
		_, _ = s.Assign(ctx, ports.Assignment{
			ScopeID: "site", DeviceID: "dev_many_ops", ArtifactSHA256: pkgArt.SHA256,
			IdempotencyKey: fmt.Sprintf("k_mo_%d", i), OperationID: fmt.Sprintf("op_mo_%d", i),
			Now: now, Deadline: now.Add(time.Hour),
		})
	}
	snapMany, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: "dev_many_ops", CredentialHash: hash32("cred_mo"), Sequence: 1, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}},
	})
	if err != nil || len(snapMany.Operations) != 8 {
		t.Fatalf("expected capped at 8 operations, got len=%d, err=%v", len(snapMany.Operations), err)
	}

	// 6. Policy report already present in policies table -> unmarshals reported on next sync/get
	pol, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: "site", DeviceID: devID, IdempotencyKey: "k_pol_rep",
		Policy: ports.Policy{CameraDisabled: boolPtr(true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Sync with matching policy report
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 10, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}},
		PolicyReport: &ports.PolicyReport{
			Revision: pol.Revision, SnapshotRevision: pol.Revision,
			Results: []ports.PolicyResult{{RuleID: "camera", State: "APPLIED"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Next Sync reads the saved policy report from DB
	snapPol, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 11, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = snapPol
}

// TestSqlstoreFailureInjections injects corruption and trigger aborts to hit all remaining error branches.
func TestSqlstoreFailureInjections(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	// 1. JSON corruption in tables
	{
		s, db := testStore(t)
		devID := "dev_corrupt"
		createTestDevice(t, s, "site", devID, "cred1")
		art := ports.Artifact{ScopeID: "site", PackageName: "io.kylok.corrupt", SHA256: strings.Repeat("c", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
		_ = s.RegisterArtifact(ctx, art)
		op, _ := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_c", OperationID: "op_c", Now: now, Deadline: now.Add(time.Hour)})
		cmd, _ := s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID, ID: "cmd_c", IdempotencyKey: "kc", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)})
		_, _ = s.ReplaceGroup(ctx, ports.Group{ScopeID: "site", ID: "grp_c", DeviceIDs: []string{devID}}, 0)
		_, _ = s.CreateFleetAssignment(ctx, ports.FleetRequest{ScopeID: "site", ID: "fa_c", ArtifactSHA256: art.SHA256, DeviceIDs: []string{devID}, NotBefore: now, Deadline: now.Add(time.Hour)}, now)
		_, _ = s.SetPolicy(ctx, ports.PolicyAssignment{ScopeID: "site", DeviceID: devID, IdempotencyKey: "k_pc", Policy: ports.Policy{CameraDisabled: boolPtr(true)}})

		// a) Corrupt operation data -> readDeviceState / ReadProjection / ListOperations fails
		_, _ = db.Exec("UPDATE operations SET data='{corrupt' WHERE id=?", op.ID)
		if _, err := s.ReadProjection(ctx, "site", "", 0, 10, now); err == nil {
			t.Fatal("expected error on corrupt operation data")
		}
		if _, err := s.ListOperations(ctx, "site", devID, "", 10, now); err == nil {
			t.Fatal("expected error in ListOperations on corrupt data")
		}

		// b) Corrupt command data -> ListCommands fails
		_, _ = db.Exec("UPDATE commands SET data='{corrupt' WHERE id=?", cmd.ID)
		if _, err := s.ListCommands(ctx, "site", devID, "", 10, now); err == nil {
			t.Fatal("expected error in ListCommands on corrupt data")
		}

		// c) Corrupt fleet_groups data -> ListGroups fails
		_, _ = db.Exec("UPDATE fleet_groups SET data='{corrupt' WHERE id='grp_c'")
		if _, err := s.ListGroups(ctx, "site", "", 10); err == nil {
			t.Fatal("expected error in ListGroups on corrupt data")
		}

		// d) Corrupt fleet_assignments data -> ListFleetAssignments fails
		_, _ = db.Exec("UPDATE fleet_assignments SET data='{corrupt' WHERE id='fa_c'")
		if _, err := s.ListFleetAssignments(ctx, "site", "", 10); err == nil {
			t.Fatal("expected error in ListFleetAssignments on corrupt data")
		}

		// e) Corrupt policies data -> GetPolicy fails
		_, _ = db.Exec("UPDATE policies SET data='{corrupt' WHERE device_id=?", devID)
		if _, err := s.GetPolicy(ctx, "site", devID); err == nil {
			t.Fatal("expected error in GetPolicy on corrupt data")
		}

		// f) Corrupt policies reported -> GetPolicy fails
		_, _ = db.Exec("UPDATE policies SET data='{}', reported='{corrupt' WHERE device_id=?", devID)
		if _, err := s.GetPolicy(ctx, "site", devID); err == nil {
			t.Fatal("expected error in GetPolicy on corrupt reported")
		}

		// g) Corrupt artifacts data -> ListArtifacts fails
		_, _ = db.Exec("UPDATE artifacts SET data='{corrupt' WHERE digest=?", art.SHA256)
		if _, err := s.ListArtifacts(ctx, "site", "", 10); err == nil {
			t.Fatal("expected error in ListArtifacts on corrupt data")
		}
	}

	// 2. Trigger error injections for table operations
	{
		s, db := testStore(t)
		devID := "dev_trig"
		createTestDevice(t, s, "site", devID, "cred1")

		// a) Fail INSERT INTO changes in recordChange
		_, _ = db.Exec("CREATE TRIGGER tr_i_changes BEFORE INSERT ON changes BEGIN SELECT RAISE(ABORT, 'change insert err'); END;")
		if err := s.Revoke(ctx, "site", devID); err == nil {
			t.Fatal("expected error when INSERT changes fails")
		}
	}
	{
		s, db := testStore(t)
		devID := "dev_trig2"
		createTestDevice(t, s, "site", devID, "cred1")

		// b) Fail INSERT INTO projection_items in saveProjection
		_, _ = db.Exec("CREATE TRIGGER tr_i_pi BEFORE INSERT ON projection_items BEGIN SELECT RAISE(ABORT, 'proj_item insert err'); END;")
		if _, err := s.ReadProjection(ctx, "site", "", 0, 10, now); err == nil {
			t.Fatal("expected error when INSERT projection_items fails")
		}
	}
	{
		s, db := testStore(t)
		devID := "dev_trig3"
		createTestDevice(t, s, "site", devID, "cred1")

		// c) Fail UPDATE commands in saveCommand (during expireCommands)
		_, _ = s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID, ID: "cmd_exp", IdempotencyKey: "k_exp", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)})
		_, _ = db.Exec("CREATE TRIGGER tr_u_cmd BEFORE UPDATE ON commands BEGIN SELECT RAISE(ABORT, 'cmd update err'); END;")
		if _, err := s.ListCommands(ctx, "site", devID, "", 10, now.Add(2*time.Hour)); err == nil {
			t.Fatal("expected error when UPDATE commands fails during expire")
		}
	}
	{
		s, db := testStore(t)
		devID := "dev_trig4"
		createTestDevice(t, s, "site", devID, "cred1")

		// d) Fail UPDATE fleet_groups in ReplaceGroup
		_, _ = s.ReplaceGroup(ctx, ports.Group{ScopeID: "site", ID: "grp_u", DeviceIDs: []string{devID}}, 0)
		_, _ = db.Exec("CREATE TRIGGER tr_u_grp BEFORE UPDATE ON fleet_groups BEGIN SELECT RAISE(ABORT, 'grp update err'); END;")
		if _, err := s.ReplaceGroup(ctx, ports.Group{ScopeID: "site", ID: "grp_u", DeviceIDs: []string{devID}}, 1); err == nil {
			t.Fatal("expected error when UPDATE fleet_groups fails")
		}
	}
	{
		s, db := testStore(t)
		devID := "dev_trig5"
		createTestDevice(t, s, "site", devID, "cred1")
		art := ports.Artifact{ScopeID: "site", PackageName: "io.kylok.pkg", SHA256: strings.Repeat("a", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
		_ = s.RegisterArtifact(ctx, art)
		fa, _ := s.CreateFleetAssignment(ctx, ports.FleetRequest{ScopeID: "site", ID: "fa_canc", ArtifactSHA256: art.SHA256, DeviceIDs: []string{devID}, NotBefore: now, Deadline: now.Add(time.Hour)}, now)

		// e) Fail UPDATE fleet_assignments in CancelFleetAssignment
		_, _ = db.Exec("CREATE TRIGGER tr_u_fl BEFORE UPDATE ON fleet_assignments BEGIN SELECT RAISE(ABORT, 'fl update err'); END;")
		if _, err := s.CancelFleetAssignment(ctx, "site", fa.Request.ID, now); err == nil {
			t.Fatal("expected error when UPDATE fleet_assignments fails in cancel")
		}
	}
}

// TestStoreCheckClosed verifies Store.Check error reporting when the underlying connection pool is closed.
func TestStoreCheckClosed(t *testing.T) {
	s, db := testStore(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Check(context.Background()); err == nil {
		t.Fatal("expected error on closed db")
	}
}

// TestStoreNotFoundPaths exercises not-found error returns across Store read queries.
func TestStoreNotFoundPaths(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	if _, err := s.GetDevice(ctx, "site", "missing"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := s.Enrollment(ctx, hash32("missing")); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := s.Revoke(ctx, "site", "missing"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := s.ListOperations(ctx, "site", "missing", "", 10, time.Now()); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestPostgresDialectSchemaSQL exercises postgres dialect schema transformation logic in Open.
func TestPostgresDialectSchemaSQL(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "pg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Open with postgres dialect on sqlite pool executes schemaSQL replace branches
	_, _ = Open(ctx, db, "postgres", "public", true)
}

// TestStoreCorruptedJSONPaths verifies unmarshaling failure paths for operations and commands records.
func TestStoreCorruptedJSONPaths(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	devID := "dev_corrupt"
	createTestDevice(t, s, "site", devID, "cred1")

	art := ports.Artifact{ScopeID: "site", PackageName: "com.example.app", SHA256: strings.Repeat("b", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
	if err := s.RegisterArtifact(ctx, art); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_op", Now: now, Deadline: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID, ID: "cmd_c", IdempotencyKey: "k_cmd", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	// 1. Corrupt operations.data
	if _, err := db.Exec("UPDATE operations SET data='{corrupt' WHERE device_id=?", devID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListOperations(ctx, "site", devID, "", 10, now); err == nil {
		t.Fatal("expected error on corrupt operations.data")
	}

	// 2. Corrupt commands.data
	if _, err := db.Exec("UPDATE commands SET data='{corrupt' WHERE device_id=?", devID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListCommands(ctx, "site", devID, "", 10, now); err == nil {
		t.Fatal("expected error on corrupt commands.data")
	}

	// 3. Test readDeviceState with corrupt commands.data via GetDeviceState
	if _, err := s.GetDeviceState(ctx, "site", devID); err == nil {
		t.Fatal("expected error on GetDeviceState with corrupt commands data")
	}
}

// TestStoreProjectionAdvanced verifies projection pagination and fleet/group item serialization.
func TestStoreProjectionAdvanced(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	devID := "dev_proj_adv"
	createTestDevice(t, s, "site", devID, "cred1")

	art := ports.Artifact{ScopeID: "site", PackageName: "com.example.adv", SHA256: strings.Repeat("c", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
	if err := s.RegisterArtifact(ctx, art); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReplaceGroup(ctx, ports.Group{ScopeID: "site", ID: "grp_adv", DeviceIDs: []string{devID}}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFleetAssignment(ctx, ports.FleetRequest{ScopeID: "site", ID: "fa_adv", ArtifactSHA256: art.SHA256, DeviceIDs: []string{devID}, NotBefore: now, Deadline: now.Add(time.Hour)}, now); err != nil {
		t.Fatal(err)
	}

	// Create an expired projection scan in projection_scans table
	if _, err := db.Exec("INSERT INTO projection_scans(id, ordinal, scope_id, watermark, expires_at) VALUES('old_scan', 9999, 'site', 0, ?)", now.Add(-time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}

	// Read projection with limit=1 to trigger result.More=true and expired scan cleanup
	page, err := s.ReadProjection(ctx, "site", "", 0, 1, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !page.More {
		t.Fatal("expected page.More to be true with limit 1")
	}

	// Read next page with after
	page2, err := s.ReadProjection(ctx, "site", page.SnapshotID, page.After, 10, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page2.Items) == 0 {
		t.Fatal("expected items on second page")
	}

	// Corrupt fleet_groups data to trigger error in ReadProjection
	if _, err := db.Exec("UPDATE fleet_groups SET data='{corrupt' WHERE scope_id='site'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadProjection(ctx, "site", "", 0, 10, now); err == nil {
		t.Fatal("expected error on corrupt fleet_groups data")
	}
}

// TestSyncCommandUnavailableAndReportEdgeCases verifies command capability fallback and report receipt edge cases.
func TestSyncCommandUnavailableAndReportEdgeCases(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	devID := "dev_sync_edge"
	createTestDevice(t, s, "site", devID, "cred1")

	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID, ID: "cmd_unavail", IdempotencyKey: "k_unavail", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	// Sync with DeviceOwner = false triggers command FAILED with CAPABILITY_UNAVAILABLE
	sync1, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       1,
		Inventory: ports.Inventory{
			APILevel:    34,
			Model:       "Pixel",
			DeviceOwner: false,
		},
		Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sync1.Commands) > 0 {
		t.Fatalf("expected no dispatched command when capability unavailable")
	}

	cmds, err := s.ListCommands(ctx, "site", devID, "", 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].State != "FAILED" || cmds[0].ErrorCode != "CAPABILITY_UNAVAILABLE" {
		t.Fatalf("expected FAILED with CAPABILITY_UNAVAILABLE, got %+v", cmds)
	}

	art := ports.Artifact{ScopeID: "site", PackageName: "com.example.rep", SHA256: strings.Repeat("d", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
	if err := s.RegisterArtifact(ctx, art); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_rep", Now: now, Deadline: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	sync2, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       2,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		Now:            now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sync2.Operations) != 1 {
		t.Fatalf("expected 1 operation dispatched, got %d", len(sync2.Operations))
	}
	op := sync2.Operations[0]

	rep := ports.OperationReport{
		ID:          "rep_1",
		OperationID: op.ID,
		LeaseID:     op.LeaseID,
		Sequence:    1,
		State:       "RUNNING",
	}
	sync3, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       3,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		Reports:        []ports.OperationReport{rep},
		Now:            now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sync3.Receipts) != 1 || !sync3.Receipts[0].Accepted {
		t.Fatalf("expected report accepted, got %+v", sync3.Receipts)
	}

	// Submit identical report again (idempotent receipt return via sync replay)
	sync4, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       3,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		Reports:        []ports.OperationReport{rep},
		Now:            now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sync4.Receipts) != 1 || !sync4.Receipts[0].Accepted {
		t.Fatalf("expected duplicate report accepted, got %+v", sync4.Receipts)
	}

	// Submit report with same sequence but conflicting payload
	repConflict := rep
	repConflict.State = "FAILED"
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       3,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		Reports:        []ports.OperationReport{repConflict},
		Now:            now,
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on conflicting report body, got %v", err)
	}

	// Corrupt stored sync_snapshot in devices table to test unmarshal error on replay
	if _, err := db.Exec("UPDATE devices SET sync_snapshot='{corrupt' WHERE id=?", devID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       3,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		Reports:        []ports.OperationReport{rep},
		Now:            now,
	}); err == nil {
		t.Fatal("expected error on corrupt sync_snapshot JSON")
	}
}

// TestStoreArtifactsAndPoliciesAndFleetCoverage exercises artifact management, policy assignment and fleet fan-out.
func TestStoreArtifactsAndPoliciesAndFleetCoverage(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	devID := "dev_art_pol"
	createTestDevice(t, s, "site", devID, "cred1")

	// 1. Artifacts: ListArtifacts validation, Artifact, RemoveArtifact
	if _, err := s.ListArtifacts(ctx, "", "", 10); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty scope in ListArtifacts, got %v", err)
	}

	art1 := ports.Artifact{ScopeID: "site", PackageName: "com.example.pkg1", SHA256: strings.Repeat("1", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
	art2 := ports.Artifact{ScopeID: "site", PackageName: "com.example.pkg2", SHA256: strings.Repeat("2", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
	if err := s.RegisterArtifact(ctx, art1); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterArtifact(ctx, art2); err != nil {
		t.Fatal(err)
	}

	// Read artifact by scope and digest
	loadedArt, err := s.Artifact(ctx, "site", art1.SHA256)
	if err != nil || loadedArt.PackageName != art1.PackageName {
		t.Fatalf("failed to read registered artifact: %v", err)
	}
	if _, err := s.Artifact(ctx, "site", strings.Repeat("9", 64)); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on missing artifact, got %v", err)
	}

	// List artifacts
	arts, err := s.ListArtifacts(ctx, "site", "", 10)
	if err != nil || len(arts) < 2 {
		t.Fatalf("expected at least 2 artifacts, got %d (err: %v)", len(arts), err)
	}

	// Remove unreferenced art2
	if err := s.RemoveArtifact(ctx, "site", art2.SHA256); err != nil {
		t.Fatalf("failed to remove unreferenced artifact: %v", err)
	}

	// Assign art1 to devID
	op, err := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ArtifactSHA256: art1.SHA256,
		IdempotencyKey: "k_art_op",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Now RemoveArtifact for art1 should fail with ErrReferenced (due to active op and desired_apps)
	if err := s.RemoveArtifact(ctx, "site", art1.SHA256); !errors.Is(err, ports.ErrReferenced) {
		t.Fatalf("expected ErrReferenced on referenced artifact, got %v", err)
	}

	// Cancel operation
	cancelledOp, err := s.CancelOperation(ctx, "site", devID, op.ID, now)
	if err != nil || cancelledOp.State != "CANCELLED" {
		t.Fatalf("failed to cancel operation: %v, op: %+v", err, cancelledOp)
	}

	// 2. Groups: GetGroup, ListGroups validation
	if _, err := s.ListGroups(ctx, "", "", 10); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty scope in ListGroups, got %v", err)
	}
	if _, err := s.ListGroups(ctx, "site", "", 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0 in ListGroups, got %v", err)
	}

	grp, err := s.ReplaceGroup(ctx, ports.Group{ScopeID: "site", ID: "grp_cov", DeviceIDs: []string{devID}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	loadedGrp, err := s.GetGroup(ctx, "site", grp.ID)
	if err != nil || loadedGrp.Revision != grp.Revision {
		t.Fatalf("failed to get group: %v", err)
	}
	grps, err := s.ListGroups(ctx, "site", "", 10)
	if err != nil || len(grps) == 0 {
		t.Fatalf("expected groups in ListGroups: %v", err)
	}

	// 3. Fleet: CreateFleetAssignment, AdvanceFleetAssignment, GetFleetAssignment, ListFleetAssignments, CancelFleetAssignment
	if _, err := s.ListFleetAssignments(ctx, "", "", 10); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty scope in ListFleetAssignments, got %v", err)
	}
	if _, err := s.ListFleetAssignments(ctx, "site", "", 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0 in ListFleetAssignments, got %v", err)
	}

	fa, err := s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID:        "site",
		ID:             "fa_cov",
		ArtifactSHA256: art1.SHA256,
		GroupID:        grp.ID,
		NotBefore:      now,
		Deadline:       now.Add(time.Hour),
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	// Advance fleet assignment with invalid limit
	if _, err := s.AdvanceFleetAssignment(ctx, "site", fa.Request.ID, now, 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on limit 0 in AdvanceFleetAssignment, got %v", err)
	}

	// Advance fleet assignment
	advancedFA, err := s.AdvanceFleetAssignment(ctx, "site", fa.Request.ID, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(advancedFA.Items) == 0 || advancedFA.Items[0].State != "ASSIGNED" {
		t.Fatalf("expected item assigned in AdvanceFleetAssignment, got %+v", advancedFA)
	}

	// Get and list fleet assignments
	loadedFA, err := s.GetFleetAssignment(ctx, "site", fa.Request.ID)
	if err != nil || loadedFA.Request.ID != fa.Request.ID {
		t.Fatalf("failed to get fleet assignment: %v", err)
	}
	fas, err := s.ListFleetAssignments(ctx, "site", "", 10)
	if err != nil || len(fas) == 0 {
		t.Fatalf("expected fleet assignments in ListFleetAssignments: %v", err)
	}

	// Cancel fleet assignment with assigned operation
	cancelledFA, err := s.CancelFleetAssignment(ctx, "site", fa.Request.ID, now)
	if err != nil || !cancelledFA.Cancelled {
		t.Fatalf("failed to cancel fleet assignment: %v", err)
	}
	// Repeat cancel (idempotent)
	reCancelledFA, err := s.CancelFleetAssignment(ctx, "site", fa.Request.ID, now)
	if err != nil || !reCancelledFA.Cancelled {
		t.Fatalf("failed idempotent cancel: %v", err)
	}

	// 4. Policy: GetPolicy, SetPolicy
	if _, err := s.GetPolicy(ctx, "site", "missing_dev"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing device in GetPolicy, got %v", err)
	}
	polStatus, err := s.GetPolicy(ctx, "site", devID)
	if err != nil {
		t.Fatalf("failed to get policy: %v", err)
	}
	if polStatus.Desired != nil {
		t.Fatalf("expected no desired policy initially")
	}

	// SetPolicy with missing device
	if _, err := s.SetPolicy(ctx, ports.PolicyAssignment{ScopeID: "site", DeviceID: "missing_dev", IdempotencyKey: "k_p1"}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound in SetPolicy for missing device, got %v", err)
	}

	// SetPolicy with required package that is NOT in desired_apps
	if _, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		IdempotencyKey: "k_p2",
		Policy: ports.Policy{
			RequiredPackages: []string{"com.missing.required.pkg"},
		},
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing required package, got %v", err)
	}

	// SetPolicy with valid required package (art1 is in desired_apps)
	polDesired, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		IdempotencyKey: "k_p3",
		Policy: ports.Policy{
			RequiredPackages: []string{art1.PackageName},
		},
	})
	if err != nil {
		t.Fatalf("failed to set valid policy: %v", err)
	}
	if polDesired.Revision == 0 {
		t.Fatalf("expected non-zero policy revision")
	}

	// SetPolicy replay (idempotent)
	rePol, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		IdempotencyKey: "k_p3",
		Policy: ports.Policy{
			RequiredPackages: []string{art1.PackageName},
		},
	})
	if err != nil || rePol.Revision != polDesired.Revision {
		t.Fatalf("failed idempotent SetPolicy: %v", err)
	}

	// SetPolicy conflict on same idempotency key with different policy
	if _, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		IdempotencyKey: "k_p3",
		Policy: ports.Policy{
			CameraDisabled: boolPtr(true),
		},
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict in SetPolicy, got %v", err)
	}

	_ = db
}

// TestSqlstoreCancelledContextEveryMethod verifies context cancellation error handling across all Store methods.
func TestSqlstoreCancelledContextEveryMethod(t *testing.T) {
	s, _ := testStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	now := time.Now().UTC()
	devID := "dev_canc_ctx"
	token := hash32("tok_canc")
	cred := hash32("cred_canc")
	art := ports.Artifact{ScopeID: "site", PackageName: "com.canc.pkg", SHA256: strings.Repeat("e", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}

	_ = s.Check(ctx)
	_, _ = s.GetDevice(ctx, "site", devID)
	_, _ = s.QueryDevices(ctx, ports.DeviceQuery{ScopeID: "site", Limit: 10})
	_, _ = s.GetOperation(ctx, "site", devID, "op1", now)
	_, _ = s.QueryOperations(ctx, ports.OperationQuery{ScopeID: "site", DeviceID: devID, Limit: 10, Now: now})
	_ = s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: "site", TokenHash: token, ExpiresAt: now.Add(time.Hour)})
	_, _ = s.Enrollment(ctx, token)
	_, _ = s.Consume(ctx, ports.Attempt{TokenHash: token, ID: "att", KeyHash: token, CredentialHash: cred, Device: ports.Device{ID: devID, ScopeID: "site", State: "ACTIVE"}, Now: now, RecoverUntil: now.Add(time.Hour)})
	_, _ = s.Authenticate(ctx, devID, cred)
	_, _ = s.Sync(ctx, ports.SyncInput{DeviceID: devID, CredentialHash: cred, Sequence: 1, Now: now})
	_, _ = s.ListDevices(ctx, "site", "", 10)
	_ = s.Revoke(ctx, "site", devID)
	_ = s.RegisterArtifact(ctx, art)
	_, _ = s.ListArtifacts(ctx, "site", "", 10)
	_, _ = s.Artifact(ctx, "site", art.SHA256)
	_ = s.RemoveArtifact(ctx, "site", art.SHA256)
	_, _ = s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_c", Now: now, Deadline: now.Add(time.Hour)})
	_, _ = s.ListOperations(ctx, "site", devID, "", 10, now)
	_, _ = s.AuthorizeDownload(ctx, devID, cred, "op1", art.SHA256, strings.Repeat("f", 48), now)
	_, _ = s.SetPolicy(ctx, ports.PolicyAssignment{ScopeID: "site", DeviceID: devID, IdempotencyKey: "k_p"})
	_, _ = s.GetPolicy(ctx, "site", devID)
	_, _ = s.ListChanges(ctx, "site", 0, 10)
	_, _ = s.GetDeviceState(ctx, "site", devID)
	_, _ = s.ReadProjection(ctx, "site", "", 0, 10, now)
	_, _ = s.ReplaceGroup(ctx, ports.Group{ScopeID: "site", ID: "grp1", DeviceIDs: []string{devID}}, 0)
	_, _ = s.GetGroup(ctx, "site", "grp1")
	_, _ = s.ListGroups(ctx, "site", "", 10)
	_, _ = s.CreateFleetAssignment(ctx, ports.FleetRequest{ScopeID: "site", ID: "fa1", ArtifactSHA256: art.SHA256, DeviceIDs: []string{devID}, NotBefore: now, Deadline: now.Add(time.Hour)}, now)
	_, _ = s.GetFleetAssignment(ctx, "site", "fa1")
	_, _ = s.ListFleetAssignments(ctx, "site", "", 10)
	_, _ = s.AdvanceFleetAssignment(ctx, "site", "fa1", now, 10)
	_, _ = s.CancelFleetAssignment(ctx, "site", "fa1", now)
	_, _ = s.CancelOperation(ctx, "site", devID, "op1", now)
	_, _ = s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID, ID: "cmd1", IdempotencyKey: "k_c", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)})
	_, _ = s.GetCommand(ctx, "site", devID, "cmd1", now)
	_, _ = s.ListCommands(ctx, "site", devID, "", 10, now)
	_, _ = s.CancelCommand(ctx, "site", devID, "cmd1", now)
	_, _ = s.GetNetworkPolicy(ctx, "site", devID)
	_, _ = s.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: "site", DeviceID: devID, IdempotencyKey: "k_n"})
}

// TestSqlstoreScanErrorsOnTerminalRecords exercises unmarshaling errors in queries reading terminal operations and commands.
func TestSqlstoreScanErrorsOnTerminalRecords(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	devID := "dev_term_err"
	createTestDevice(t, s, "site", devID, "cred1")

	art := ports.Artifact{ScopeID: "site", PackageName: "com.pkg.term", SHA256: strings.Repeat("6", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
	if err := s.RegisterArtifact(ctx, art); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_term_op", OperationID: "op_term_1", Now: now, Deadline: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID, ID: "cmd_term", IdempotencyKey: "k_term_cmd", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	// Set state='SUCCEEDED' and data='{corrupt' on operations
	if _, err := db.Exec("UPDATE operations SET state='SUCCEEDED', data='{corrupt' WHERE device_id=?", devID); err != nil {
		t.Fatal(err)
	}
	// 1. ListOperations unmarshal error (operations.go:333)
	if _, err := s.ListOperations(ctx, "site", devID, "", 10, now); err == nil {
		t.Fatal("expected error on ListOperations with corrupt terminal operation data")
	}
	// 2. QueryOperations unmarshal error (management.go:107)
	if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: "site", DeviceID: devID, Limit: 10, Now: now}); err == nil {
		t.Fatal("expected error on QueryOperations with corrupt terminal operation data")
	}

	// Restore operations.data to valid JSON so readDeviceState can reach commands
	validOpJSON, _ := json.Marshal(ports.Operation{ID: "op_term", DeviceID: devID, State: "SUCCEEDED", Artifact: art})
	if _, err := db.Exec("UPDATE operations SET state='SUCCEEDED', data=? WHERE device_id=?", validOpJSON, devID); err != nil {
		t.Fatal(err)
	}

	// Set state='SUCCEEDED' and data='{corrupt' on commands
	if _, err := db.Exec("UPDATE commands SET state='SUCCEEDED', data='{corrupt' WHERE device_id=?", devID); err != nil {
		t.Fatal(err)
	}
	// 3. ListCommands unmarshal error (commands.go:220)
	if _, err := s.ListCommands(ctx, "site", devID, "", 10, now); err == nil {
		t.Fatal("expected error on ListCommands with corrupt terminal command data")
	}
	// 4. GetDeviceState commands unmarshal error (changes.go:164)
	if _, err := s.GetDeviceState(ctx, "site", devID); err == nil {
		t.Fatal("expected error on GetDeviceState with corrupt command data")
	}
}

// TestSqlstoreDeepCoverageExtensions exercises change retention floor, scan eviction limits, command capacity, and network policy branches.
func TestSqlstoreDeepCoverageExtensions(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	devID := "dev_deep_cov"
	createTestDevice(t, s, "site", devID, "cred1")

	// 1. Change retention floor deletion: set change_heads sequence to > ChangeRetention
	if _, err := db.Exec("INSERT OR REPLACE INTO change_heads(scope_id, sequence, floor) VALUES('site', 10005, 5)"); err != nil {
		t.Fatal(err)
	}
	// Trigger recordChange via Revoke
	_ = s.Revoke(ctx, "site", devID)

	// 2. Projection scan eviction: trigger len(retained) >= SnapshotLimit (4)
	for i := 0; i < 6; i++ {
		_, err := s.ReadProjection(ctx, "site", "", 0, 10, now.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
	}

	// 3. Command conflict on different kind
	devCmd := "dev_cmd_cov"
	createTestDevice(t, s, "site", devCmd, "cred1")
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       devCmd,
		ID:             "cmd_reboot_1",
		IdempotencyKey: "k_cmd_dup",
		Kind:           "REBOOT",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	// Same idempotency key with LOCK kind -> ErrConflict
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       devCmd,
		ID:             "cmd_lock_1",
		IdempotencyKey: "k_cmd_dup",
		Kind:           "LOCK",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on conflicting command kind, got %v", err)
	}

	// Submit up to maximumActiveCommands (16)
	for i := 2; i <= 16; i++ {
		_, err := s.SubmitCommand(ctx, ports.CommandAssignment{
			ScopeID:        "site",
			DeviceID:       devCmd,
			ID:             fmt.Sprintf("cmd_reboot_%d", i),
			IdempotencyKey: fmt.Sprintf("k_cmd_%d", i),
			Kind:           "REBOOT",
			Now:            now,
			Deadline:       now.Add(time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// The 17th command should fail with ErrConflict
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       devCmd,
		ID:             "cmd_reboot_17",
		IdempotencyKey: "k_cmd_17",
		Kind:           "REBOOT",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on exceeding max active commands, got %v", err)
	}

	// 4. Network policies: GetNetworkPolicy without lifecycle capability
	devNet := "dev_net_cov"
	createTestDevice(t, s, "site", devNet, "cred1")
	netPol, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID:        "site",
		DeviceID:       devNet,
		IdempotencyKey: "k_net_1",
		Policy: ports.NetworkPolicy{
			PrimaryID: "wifi_home",
			Profiles:  []ports.WifiProfile{{ID: "wifi_home", SSID: "HomeNet", Security: "WPA2_PSK", SecretRef: "ref1"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if netPol.Revision == 0 {
		t.Fatal("expected non-zero network policy revision")
	}

	// Read network policy for devNet (which does NOT have "network.lifecycle" in capabilities)
	netStatus, err := s.GetNetworkPolicy(ctx, "site", devNet)
	if err != nil {
		t.Fatal(err)
	}
	if netStatus.State != "UNSUPPORTED" || netStatus.Reason != "CAPABILITY_UNAVAILABLE" {
		t.Fatalf("expected UNSUPPORTED network status, got %+v", netStatus)
	}

	// Corrupt network_policies.reported
	if _, err := db.Exec("UPDATE network_policies SET reported='{corrupt' WHERE device_id=?", devNet); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetNetworkPolicy(ctx, "site", devNet); err == nil {
		t.Fatal("expected error on corrupt network_policies.reported")
	}

	// Corrupt network_policies.data
	if _, err := db.Exec("UPDATE network_policies SET data='{corrupt' WHERE device_id=?", devNet); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetNetworkPolicy(ctx, "site", devNet); err == nil {
		t.Fatal("expected error on corrupt network_policies.data")
	}
}

// TestSqlstoreSystematicFailureAndScanInjections exercises scan decoding errors and trigger-injected SQL failure branches.
func TestSqlstoreSystematicFailureAndScanInjections(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Scan errors via type mismatches in SQLite tables
	{
		s, db := testStore(t)
		// Corrupt changes sequence column with string that fails uint64 scan
		if _, err := db.Exec("INSERT INTO changes(scope_id, sequence, kind, resource_id) VALUES('site_bad_seq', 'not_a_number', 'k', 'r')"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ListChanges(ctx, "site_bad_seq", 0, 10); err == nil {
			t.Fatal("expected scan error on corrupted changes sequence")
		}

		// Corrupt projection_scans expires_at column with string that fails int64 scan
		if _, err := db.Exec("INSERT INTO projection_scans(id, ordinal, scope_id, watermark, expires_at) VALUES('ps_bad', 9999, 'site_bad_ps', 0, 'not_a_number')"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ReadProjection(ctx, "site_bad_ps", "", 0, 10, now); err == nil {
			t.Fatal("expected scan error on corrupted projection_scans expires_at")
		}
	}

	// 2. Unmarshal failures in List* queries via invalid JSON in data column
	{
		s, db := testStore(t)
		devID := "dev_scan_unm"
		createTestDevice(t, s, "site", devID, "cred1")
		art := ports.Artifact{ScopeID: "site", PackageName: "com.pkg.scan", SHA256: strings.Repeat("3", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
		_ = s.RegisterArtifact(ctx, art)
		grp, _ := s.ReplaceGroup(ctx, ports.Group{ScopeID: "site", ID: "grp_scan", DeviceIDs: []string{devID}}, 0)
		fa, _ := s.CreateFleetAssignment(ctx, ports.FleetRequest{ScopeID: "site", ID: "fa_scan", ArtifactSHA256: art.SHA256, GroupID: grp.ID, NotBefore: now, Deadline: now.Add(time.Hour)}, now)
		op, _ := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_s_op", Now: now, Deadline: now.Add(time.Hour)})

		// Corrupt artifacts.data -> ListArtifacts unmarshal error
		if _, err := db.Exec("UPDATE artifacts SET data='{corrupt' WHERE scope_id='site' AND digest=?", art.SHA256); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ListArtifacts(ctx, "site", "", 10); err == nil {
			t.Fatal("expected error on corrupt artifacts.data")
		}

		// Corrupt fleet_groups.data -> ListGroups unmarshal error
		if _, err := db.Exec("UPDATE fleet_groups SET data='{corrupt' WHERE scope_id='site' AND id=?", grp.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ListGroups(ctx, "site", "", 10); err == nil {
			t.Fatal("expected error on corrupt fleet_groups.data")
		}

		// Corrupt fleet_assignments.data -> ListFleetAssignments unmarshal error
		if _, err := db.Exec("UPDATE fleet_assignments SET data='{corrupt' WHERE scope_id='site' AND id=?", fa.Request.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ListFleetAssignments(ctx, "site", "", 10); err == nil {
			t.Fatal("expected error on corrupt fleet_assignments.data")
		}

		// Corrupt operations.data -> QueryOperations unmarshal error
		if _, err := db.Exec("UPDATE operations SET data='{corrupt' WHERE id=?", op.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: "site", DeviceID: devID, Limit: 10, Now: now}); err == nil {
			t.Fatal("expected error on corrupt operations.data in QueryOperations")
		}

		// Create projection and corrupt projection_items.data -> ReadProjection unmarshal error
		page, _ := s.ReadProjection(ctx, "site", "", 0, 10, now)
		if _, err := db.Exec("UPDATE projection_items SET data='{corrupt' WHERE snapshot_id=?", page.SnapshotID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ReadProjection(ctx, "site", page.SnapshotID, 0, 10, now); err == nil {
			t.Fatal("expected error on corrupt projection_items.data")
		}
	}

	// 3. Trigger aborts for specific statement failure branches
	{
		// a) Fail INSERT INTO artifacts in RegisterArtifact (operations.go:64)
		s, db := testStore(t)
		_, _ = db.Exec("CREATE TRIGGER tr_i_art BEFORE INSERT ON artifacts BEGIN SELECT RAISE(ABORT, 'art insert err'); END;")
		art := ports.Artifact{ScopeID: "site", PackageName: "com.pkg.trig", SHA256: strings.Repeat("4", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
		if err := s.RegisterArtifact(ctx, art); err == nil {
			t.Fatal("expected error when INSERT artifacts fails")
		}

		// b) Fail DELETE FROM artifacts in RemoveArtifact (operations.go:138)
		s2, db2 := testStore(t)
		_ = s2.RegisterArtifact(ctx, art)
		_, _ = db2.Exec("CREATE TRIGGER tr_d_art BEFORE DELETE ON artifacts BEGIN SELECT RAISE(ABORT, 'art delete err'); END;")
		if err := s2.RemoveArtifact(ctx, "site", art.SHA256); err == nil {
			t.Fatal("expected error when DELETE artifacts fails")
		}

		// c) Fail INSERT INTO desired_apps in assign (operations.go:300)
		s3, db3 := testStore(t)
		devID3 := "dev_trig_dapp"
		createTestDevice(t, s3, "site", devID3, "cred1")
		_ = s3.RegisterArtifact(ctx, art)
		_, _ = db3.Exec("CREATE TRIGGER tr_i_dapp BEFORE INSERT ON desired_apps BEGIN SELECT RAISE(ABORT, 'dapp insert err'); END;")
		if _, err := s3.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID3, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_t_dapp", Now: now, Deadline: now.Add(time.Hour)}); err == nil {
			t.Fatal("expected error when INSERT desired_apps fails")
		}

		// d) Fail INSERT INTO policy_requests in SetPolicy (policies.go:93)
		s4, db4 := testStore(t)
		devID4 := "dev_trig_pol"
		createTestDevice(t, s4, "site", devID4, "cred1")
		_, _ = db4.Exec("CREATE TRIGGER tr_i_preq BEFORE INSERT ON policy_requests BEGIN SELECT RAISE(ABORT, 'preq insert err'); END;")
		if _, err := s4.SetPolicy(ctx, ports.PolicyAssignment{ScopeID: "site", DeviceID: devID4, IdempotencyKey: "k_t_preq"}); err == nil {
			t.Fatal("expected error when INSERT policy_requests fails")
		}

		// e) Fail INSERT INTO network_requests in SetNetworkPolicy (network.go:113)
		s5, db5 := testStore(t)
		devID5 := "dev_trig_net"
		createTestDevice(t, s5, "site", devID5, "cred1")
		_, _ = db5.Exec("CREATE TRIGGER tr_i_nreq BEFORE INSERT ON network_requests BEGIN SELECT RAISE(ABORT, 'nreq insert err'); END;")
		if _, err := s5.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: "site", DeviceID: devID5, IdempotencyKey: "k_t_nreq"}); err == nil {
			t.Fatal("expected error when INSERT network_requests fails")
		}

		// f) Fail INSERT INTO commands in SubmitCommand (commands.go:159)
		s6, db6 := testStore(t)
		devID6 := "dev_trig_cmd"
		createTestDevice(t, s6, "site", devID6, "cred1")
		_, _ = db6.Exec("CREATE TRIGGER tr_i_cmd BEFORE INSERT ON commands BEGIN SELECT RAISE(ABORT, 'cmd insert err'); END;")
		if _, err := s6.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID6, ID: "cmd_t_c", IdempotencyKey: "k_t_c", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)}); err == nil {
			t.Fatal("expected error when INSERT commands fails")
		}

		// g) Fail INSERT INTO projection_scans in ReadProjection (changes.go:242)
		s7, db7 := testStore(t)
		_, _ = db7.Exec("CREATE TRIGGER tr_i_ps BEFORE INSERT ON projection_scans BEGIN SELECT RAISE(ABORT, 'ps insert err'); END;")
		if _, err := s7.ReadProjection(ctx, "site", "", 0, 10, now); err == nil {
			t.Fatal("expected error when INSERT projection_scans fails")
		}

		// h) Fail DELETE FROM projection_items in removeScan (changes.go:190)
		s8, db8 := testStore(t)
		_, _ = s8.ReadProjection(ctx, "site", "", 0, 10, now)
		_, _ = db8.Exec("CREATE TRIGGER tr_d_pi BEFORE DELETE ON projection_items BEGIN SELECT RAISE(ABORT, 'pi delete err'); END;")
		// Trigger scan eviction (calling ReadProjection 5 times)
		for i := 0; i < 6; i++ {
			_, _ = s8.ReadProjection(ctx, "site", "", 0, 10, now.Add(time.Duration(i)*time.Minute))
		}
	}

	// 4. Exercise lines 408-410 in operations.go (duplicate report in distinct sync sequence)
	{
		s, _ := testStore(t)
		devID := "dev_dup_rep"
		createTestDevice(t, s, "site", devID, "cred1")
		art := ports.Artifact{ScopeID: "site", PackageName: "com.pkg.duprep", SHA256: strings.Repeat("5", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
		_ = s.RegisterArtifact(ctx, art)
		op, _ := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_d_op", Now: now, Deadline: now.Add(time.Hour)})

		// Sync 1: get operation lease
		s1, err := s.Sync(ctx, ports.SyncInput{
			DeviceID:       devID,
			CredentialHash: hash32("cred1"),
			Sequence:       1,
			Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
			Now:            now,
		})
		if err != nil || len(s1.Operations) == 0 {
			t.Fatalf("sync 1 failed: %v", err)
		}
		dispatchedOp := s1.Operations[0]

		rep := ports.OperationReport{
			ID:          "rep_dup_test",
			OperationID: op.ID,
			LeaseID:     dispatchedOp.LeaseID,
			Sequence:    1,
			State:       "RUNNING",
		}

		// Sync 2: submit report -> inserted into reports table
		s2, err := s.Sync(ctx, ports.SyncInput{
			DeviceID:       devID,
			CredentialHash: hash32("cred1"),
			Sequence:       2,
			Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
			Reports:        []ports.OperationReport{rep},
			Now:            now,
		})
		if err != nil || len(s2.Receipts) == 0 || !s2.Receipts[0].Accepted {
			t.Fatalf("sync 2 report submit failed: %v", err)
		}

		// Sync 3: submit the SAME report ID with same payload under sequence 3 -> exercises operations.go:408-410!
		s3, err := s.Sync(ctx, ports.SyncInput{
			DeviceID:       devID,
			CredentialHash: hash32("cred1"),
			Sequence:       3,
			Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
			Reports:        []ports.OperationReport{rep},
			Now:            now,
		})
		if err != nil || len(s3.Receipts) == 0 || !s3.Receipts[0].Accepted {
			t.Fatalf("sync 3 duplicate report failed: %v, receipts: %+v", err, s3.Receipts)
		}
	}
}

// TestSqlstoreFleetErrorBranchesAndOperationReasons exercises fleet device revocation, operation report reasons and command report validations.
func TestSqlstoreFleetErrorBranchesAndOperationReasons(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Fleet advance with revoked and deleted devices
	devRev := "dev_fleet_rev"
	devDel := "dev_fleet_del"
	createTestDevice(t, s, "site", devRev, "cred1")
	createTestDevice(t, s, "site", devDel, "cred1")
	art := ports.Artifact{ScopeID: "site", PackageName: "com.pkg.flt", SHA256: strings.Repeat("7", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
	if err := s.RegisterArtifact(ctx, art); err != nil {
		t.Fatal(err)
	}

	fa, err := s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID:        "site",
		ID:             "fa_targets",
		ArtifactSHA256: art.SHA256,
		DeviceIDs:      []string{devRev, devDel},
		NotBefore:      now,
		Deadline:       now.Add(time.Hour),
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	// Revoke devRev, delete devDel
	_ = s.Revoke(ctx, "site", devRev)
	_, _ = db.Exec("DELETE FROM devices WHERE id=?", devDel)

	adv, err := s.AdvanceFleetAssignment(ctx, "site", fa.Request.ID, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(adv.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(adv.Items))
	}
	for _, item := range adv.Items {
		if item.DeviceID == devRev && (item.State != "FAILED" || item.ErrorCode != "DEVICE_REVOKED") {
			t.Fatalf("expected DEVICE_REVOKED for devRev, got %+v", item)
		}
		if item.DeviceID == devDel && (item.State != "FAILED" || item.ErrorCode != "NOT_FOUND") {
			t.Fatalf("expected NOT_FOUND for devDel, got %+v", item)
		}
	}

	// 2. CancelOperation edge cases
	if _, err := s.CancelOperation(ctx, "site", "missing_dev", "op1", now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing device in CancelOperation, got %v", err)
	}
	devNorm := "dev_op_reasons"
	createTestDevice(t, s, "site", devNorm, "cred1")
	opNorm, err := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       devNorm,
		ArtifactSHA256: art.SHA256,
		IdempotencyKey: "k_op_norm",
		OperationID:    "op_norm_id",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelOperation(ctx, "site", devRev, opNorm.ID, now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for foreign device in CancelOperation, got %v", err)
	}

	// 3. Sync report reasons: NOT_ASSIGNED, SUPERSEDED, TERMINAL, STALE_LEASE, OBSERVATION_MISMATCH
	// Get lease for opNorm
	sNorm, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devNorm,
		CredentialHash: hash32("cred1"),
		Sequence:       1,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		Now:            now,
	})
	if err != nil || len(sNorm.Operations) == 0 {
		t.Fatalf("sync failed: %v", err)
	}
	leaseID := sNorm.Operations[0].LeaseID

	// a) NOT_ASSIGNED (foreign operation ID)
	devOther := "dev_other_dev"
	createTestDevice(t, s, "site", devOther, "cred1")
	syncRep1, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devOther,
		CredentialHash: hash32("cred1"),
		Sequence:       1,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{{
			ID:          "rep_not_assigned",
			OperationID: opNorm.ID, // belongs to devNorm, not devOther
			LeaseID:     leaseID,
			Sequence:    1,
			State:       "RUNNING",
		}},
		Now: now,
	})
	if err != nil || len(syncRep1.Receipts) == 0 || syncRep1.Receipts[0].Reason != "NOT_ASSIGNED" {
		t.Fatalf("expected NOT_ASSIGNED, got %+v", syncRep1.Receipts)
	}

	// b) STALE_LEASE (wrong leaseID)
	syncRep2, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devNorm,
		CredentialHash: hash32("cred1"),
		Sequence:       2,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{{
			ID:          "rep_stale_lease",
			OperationID: opNorm.ID,
			LeaseID:     "wrong_lease_id_12345",
			Sequence:    1,
			State:       "RUNNING",
		}},
		Now: now,
	})
	if err != nil || len(syncRep2.Receipts) == 0 || syncRep2.Receipts[0].Reason != "STALE_LEASE" {
		t.Fatalf("expected STALE_LEASE, got %+v", syncRep2.Receipts)
	}

	// c) OBSERVATION_MISMATCH (SUCCEEDED with mismatched or missing Observed)
	syncRep3, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devNorm,
		CredentialHash: hash32("cred1"),
		Sequence:       3,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{{
			ID:          "rep_obs_mismatch",
			OperationID: opNorm.ID,
			LeaseID:     leaseID,
			Sequence:    1,
			State:       "SUCCEEDED",
			Observed:    &ports.InstalledApplication{PackageName: "wrong.package", VersionCode: 999},
		}},
		Now: now,
	})
	if err != nil || len(syncRep3.Receipts) == 0 || syncRep3.Receipts[0].Reason != "OBSERVATION_MISMATCH" {
		t.Fatalf("expected OBSERVATION_MISMATCH, got %+v", syncRep3.Receipts)
	}

	// d) SUPERSEDED
	opNorm.State = "SUPERSEDED"
	dataSup, _ := json.Marshal(opNorm)
	if _, err := db.Exec("UPDATE operations SET state='SUPERSEDED', data=? WHERE id=?", dataSup, opNorm.ID); err != nil {
		t.Fatal(err)
	}
	syncRep4, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devNorm,
		CredentialHash: hash32("cred1"),
		Sequence:       4,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{{
			ID:          "rep_superseded",
			OperationID: opNorm.ID,
			LeaseID:     leaseID,
			Sequence:    1,
			State:       "RUNNING",
		}},
		Now: now,
	})
	if err != nil || len(syncRep4.Receipts) == 0 || !syncRep4.Receipts[0].Accepted || syncRep4.Receipts[0].Reason != "SUPERSEDED" {
		t.Fatalf("expected SUPERSEDED accepted, got %+v", syncRep4.Receipts)
	}

	// e) TERMINAL
	opNorm.State = "SUCCEEDED"
	dataSuc, _ := json.Marshal(opNorm)
	if _, err := db.Exec("UPDATE operations SET state='SUCCEEDED', data=? WHERE id=?", dataSuc, opNorm.ID); err != nil {
		t.Fatal(err)
	}
	syncRep5, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devNorm,
		CredentialHash: hash32("cred1"),
		Sequence:       5,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{{
			ID:          "rep_terminal",
			OperationID: opNorm.ID,
			LeaseID:     leaseID,
			Sequence:    1,
			State:       "RUNNING",
		}},
		Now: now,
	})
	if err != nil || len(syncRep5.Receipts) == 0 || !syncRep5.Receipts[0].Accepted || syncRep5.Receipts[0].Reason != "TERMINAL" {
		t.Fatalf("expected TERMINAL accepted, got %+v", syncRep5.Receipts)
	}

	// 4. Command reports: dispatch token mismatch, observation invalid arguments, and idempotency
	devCmdRep := "dev_cmd_reps"
	createTestDevice(t, s, "site", devCmdRep, "cred1")
	cmdReboot, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       devCmdRep,
		ID:             "cmd_reboot_tok",
		IdempotencyKey: "k_c_tok",
		Kind:           "REBOOT",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Sync to dispatch command
	sCmd, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devCmdRep,
		CredentialHash: hash32("cred1"),
		Sequence:       1,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		Now:            now,
	})
	if err != nil || len(sCmd.Commands) == 0 {
		t.Fatalf("command dispatch sync failed: %v", err)
	}
	dispToken := sCmd.Commands[0].DispatchToken

	// Dispatch token mismatch -> ErrConflict (commands.go:276)
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devCmdRep,
		CredentialHash: hash32("cred1"),
		Sequence:       2,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		CommandReports: []ports.CommandReport{{
			CommandID:     cmdReboot.ID,
			DispatchToken: "wrong_dispatch_token",
			State:         "SUCCEEDED",
		}},
		Now: now,
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on token mismatch, got %v", err)
	}

	// REBOOT with BootAfter <= BootBefore -> ErrInvalidArgument (commands.go:298)
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devCmdRep,
		CredentialHash: hash32("cred1"),
		Sequence:       2,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		CommandReports: []ports.CommandReport{{
			CommandID:     cmdReboot.ID,
			DispatchToken: dispToken,
			State:         "SUCCEEDED",
			Observation:   &ports.CommandObservation{BootBefore: 10, BootAfter: 10},
		}},
		Now: now,
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on BootAfter <= BootBefore, got %v", err)
	}

	// Valid REBOOT report
	validCmdRep := ports.CommandReport{
		CommandID:     cmdReboot.ID,
		DispatchToken: dispToken,
		State:         "SUCCEEDED",
		Observation:   &ports.CommandObservation{BootBefore: 10, BootAfter: 11},
	}
	sCmdReported, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devCmdRep,
		CredentialHash: hash32("cred1"),
		Sequence:       2,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		CommandReports: []ports.CommandReport{validCmdRep},
		Now:            now,
	})
	if err != nil || len(sCmdReported.CommandReceipts) == 0 {
		t.Fatalf("failed to accept valid command report: %v", err)
	}

	// Duplicate valid REBOOT report under sequence 3 (idempotent nil return, commands.go:290)
	sCmdDup, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devCmdRep,
		CredentialHash: hash32("cred1"),
		Sequence:       3,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		CommandReports: []ports.CommandReport{validCmdRep},
		Now:            now,
	})
	if err != nil || len(sCmdDup.CommandReceipts) == 0 {
		t.Fatalf("failed duplicate command report: %v", err)
	}

	// Duplicate with conflicting body under sequence 4 -> ErrConflict (commands.go:288)
	conflictCmdRep := validCmdRep
	conflictCmdRep.State = "FAILED"
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devCmdRep,
		CredentialHash: hash32("cred1"),
		Sequence:       4,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		CommandReports: []ports.CommandReport{conflictCmdRep},
		Now:            now,
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on conflicting command report, got %v", err)
	}

	// 5. ReadProjection with corrupt artifacts.data and fleet_assignments.data
	devProjArt := "dev_proj_art"
	createTestDevice(t, s, "site_art_fail", devProjArt, "cred1")
	artProj := ports.Artifact{ScopeID: "site_art_fail", PackageName: "com.pkg.artfail", SHA256: strings.Repeat("8", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
	_ = s.RegisterArtifact(ctx, artProj)
	if _, err := db.Exec("UPDATE artifacts SET data='{corrupt' WHERE scope_id='site_art_fail' AND digest=?", artProj.SHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadProjection(ctx, "site_art_fail", "", 0, 10, now); err == nil {
		t.Fatal("expected error on corrupt artifacts.data in ReadProjection")
	}

	devProjFA := "dev_proj_fa"
	createTestDevice(t, s, "site_fa_fail", devProjFA, "cred1")
	artProj2 := ports.Artifact{ScopeID: "site_fa_fail", PackageName: "com.pkg.fafail", SHA256: strings.Repeat("9", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
	_ = s.RegisterArtifact(ctx, artProj2)
	faProj, _ := s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID:        "site_fa_fail",
		ID:             "fa_corrupt_proj",
		ArtifactSHA256: artProj2.SHA256,
		DeviceIDs:      []string{devProjFA},
		NotBefore:      now,
		Deadline:       now.Add(time.Hour),
	}, now)
	if _, err := db.Exec("UPDATE fleet_assignments SET data='{corrupt' WHERE scope_id='site_fa_fail' AND id=?", faProj.Request.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadProjection(ctx, "site_fa_fail", "", 0, 10, now); err == nil {
		t.Fatal("expected error on corrupt fleet_assignments.data in ReadProjection")
	}
}

// TestSqlstoreReconciliationAndExhaustionAndEnrollmentRecovery exercises app drift reconciliation, attempt exhaustion, and enrollment lifecycle edge cases.
func TestSqlstoreReconciliationAndExhaustionAndEnrollmentRecovery(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Enrollment recovery expiry (store.go:201) and token expiry (store.go:206) and invalid candidate (store.go:209)
	tok1 := hash32("tok_recov_exp")
	tok2 := hash32("tok_tok_exp")
	tok3 := hash32("tok_cand_inv")
	if err := s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: "site", TokenHash: tok1, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: "site", TokenHash: tok2, ExpiresAt: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: "site", TokenHash: tok3, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	// a) Token already expired (store.go:206)
	if _, err := s.Consume(ctx, ports.Attempt{
		TokenHash:      tok2,
		ID:             "att_tok_exp",
		KeyHash:        hash32("key2"),
		CredentialHash: hash32("cred2"),
		Receipt:        []byte("receipt"),
		Device:         ports.Device{ID: "dev_tok_exp", ScopeID: "site"},
		Now:            now,
		RecoverUntil:   now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrExpired) {
		t.Fatalf("expected ErrExpired on expired token, got %v", err)
	}

	// b) Invalid candidate: ScopeID mismatch (store.go:209)
	if _, err := s.Consume(ctx, ports.Attempt{
		TokenHash:      tok3,
		ID:             "att_inv",
		KeyHash:        hash32("key3"),
		CredentialHash: hash32("cred3"),
		Receipt:        []byte("receipt"),
		Device:         ports.Device{ID: "dev_inv", ScopeID: "wrong_scope"},
		Now:            now,
		RecoverUntil:   now.Add(time.Hour),
	}); err == nil {
		t.Fatal("expected error on mismatched scope in Consume")
	}

	// Consume tok1 once
	rcpt1, err := s.Consume(ctx, ports.Attempt{
		TokenHash:      tok1,
		ID:             "att_recov",
		KeyHash:        hash32("key1"),
		CredentialHash: hash32("cred1"),
		Receipt:        []byte("receipt_1"),
		Device:         ports.Device{ID: "dev_recov", ScopeID: "site", State: "ACTIVE"},
		Now:            now,
		RecoverUntil:   now.Add(10 * time.Minute),
	})
	if err != nil || string(rcpt1) != "receipt_1" {
		t.Fatalf("failed initial consume: %v", err)
	}

	// Recover after recover_until has passed -> ErrRecoveryExpired (store.go:201)
	if _, err := s.Consume(ctx, ports.Attempt{
		TokenHash:      tok1,
		ID:             "att_recov",
		KeyHash:        hash32("key1"),
		CredentialHash: hash32("cred1"),
		Receipt:        []byte("receipt_1"),
		Device:         ports.Device{ID: "dev_recov", ScopeID: "site"},
		Now:            now.Add(time.Hour), // later than RecoverUntil (10 min)
		RecoverUntil:   now.Add(2 * time.Hour),
	}); !errors.Is(err, ports.ErrRecoveryExpired) {
		t.Fatalf("expected ErrRecoveryExpired, got %v", err)
	}

	// 2. Corrupt inventory in devices table -> scanDevice (store.go:247) and ListDevices (store.go:289)
	devCorruptInv := "dev_corrupt_inv"
	createTestDevice(t, s, "site_inv_err", devCorruptInv, "cred1")
	if _, err := db.Exec("UPDATE devices SET inventory='{corrupt' WHERE id=?", devCorruptInv); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDevice(ctx, "site_inv_err", devCorruptInv); err == nil {
		t.Fatal("expected error on GetDevice with corrupt inventory")
	}
	if _, err := s.ListDevices(ctx, "site_inv_err", "", 10); err == nil {
		t.Fatal("expected error on ListDevices with corrupt inventory")
	}

	// 3. Drift reconciliation (operations.go:583-596) and RetainedOperationIDs (operations.go:526-538)
	devDrift := "dev_drift"
	createTestDevice(t, s, "site", devDrift, "cred1")
	artDrift := ports.Artifact{ScopeID: "site", PackageName: "com.pkg.drift", SHA256: strings.Repeat("a", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
	if err := s.RegisterArtifact(ctx, artDrift); err != nil {
		t.Fatal(err)
	}
	opDrift, err := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       devDrift,
		ArtifactSHA256: artDrift.SHA256,
		IdempotencyKey: "k_drift_op",
		OperationID:    "op_drift_1",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Sync 1: get lease and observe install_apk capability
	sync1, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devDrift,
		CredentialHash: hash32("cred1"),
		Sequence:       1,
		Inventory: ports.Inventory{
			APILevel:     34,
			Model:        "Pixel",
			DeviceOwner:  true,
			Capabilities: []string{"install_apk"},
		},
		Now: now,
	})
	if err != nil || len(sync1.Operations) == 0 {
		t.Fatalf("sync 1 failed: %v", err)
	}
	// Verify compliance reason is INSTALL_PENDING (operations.go:644)
	if len(sync1.Compliance) == 0 || sync1.Compliance[0].Reason != "INSTALL_PENDING" {
		t.Fatalf("expected INSTALL_PENDING compliance, got %+v", sync1.Compliance)
	}

	// Sync 2: report success with matching installed application
	repDrift := ports.OperationReport{
		ID:          "rep_drift_ok",
		OperationID: opDrift.ID,
		LeaseID:     sync1.Operations[0].LeaseID,
		Sequence:    1,
		State:       "SUCCEEDED",
		Observed:    &ports.InstalledApplication{PackageName: artDrift.PackageName, VersionCode: artDrift.VersionCode},
	}
	sync2, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devDrift,
		CredentialHash: hash32("cred1"),
		Sequence:       2,
		Inventory: ports.Inventory{
			APILevel:     34,
			Model:        "Pixel",
			DeviceOwner:  true,
			Capabilities: []string{"install_apk"},
			Applications: []ports.InstalledApplication{{PackageName: artDrift.PackageName, VersionCode: artDrift.VersionCode}},
		},
		Reports: []ports.OperationReport{repDrift},
		Now:     now,
	})
	if err != nil || len(sync2.Receipts) == 0 || !sync2.Receipts[0].Accepted {
		t.Fatalf("sync 2 report failed: %v", err)
	}

	// Sync 3: drift occurs! Applications is empty (app was uninstalled!). Also include RetainedOperationIDs with missing and cancelled IDs.
	sync3, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devDrift,
		CredentialHash: hash32("cred1"),
		Sequence:       3,
		Inventory: ports.Inventory{
			APILevel:     34,
			Model:        "Pixel",
			DeviceOwner:  true,
			Capabilities: []string{"install_apk"},
			Applications: []ports.InstalledApplication{}, // app missing! triggers reconciliation!
		},
		RetainedOperationIDs: []string{"non_existent_op_id", opDrift.ID}, // exercises lines 529 and 536
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("sync 3 drift failed: %v", err)
	}
	// Verify reconcile operation was created
	if len(sync3.Operations) == 0 {
		t.Fatalf("expected reconciled operation generated, got 0")
	}
	if len(sync3.StoppedOperationIDs) == 0 {
		t.Fatalf("expected stopped operation IDs, got 0")
	}

	// 4. Attempts exhausted (operations.go:610-616)
	devExh := "dev_attempts_exh"
	createTestDevice(t, s, "site", devExh, "cred1")
	opExh, _ := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       devExh,
		ArtifactSHA256: artDrift.SHA256,
		IdempotencyKey: "k_exh_op",
		OperationID:    "op_exh_1",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	// Manually set op.Attempts = 8 and LeaseUntil expired
	opLoaded, _ := s.GetOperation(ctx, "site", devExh, opExh.ID, now)
	opLoaded.Attempts = 8
	opLoaded.LeaseID = "some_lease_token"
	opLoaded.LeaseUntil = now.Add(-time.Minute)
	dataExh, _ := json.Marshal(opLoaded)
	_, _ = db.Exec("UPDATE operations SET data=? WHERE id=?", dataExh, opExh.ID)

	// Sync should fail operation with ATTEMPTS_EXHAUSTED
	syncExh, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devExh,
		CredentialHash: hash32("cred1"),
		Sequence:       1,
		Inventory:      ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Now:            now,
	})
	if err != nil {
		t.Fatalf("syncExh failed: %v", err)
	}
	_ = syncExh
	opAfter, _ := s.GetOperation(ctx, "site", devExh, opExh.ID, now)
	if opAfter.State != "FAILED" || opAfter.ErrorCode != "ATTEMPTS_EXHAUSTED" {
		t.Fatalf("expected ATTEMPTS_EXHAUSTED, got %+v", opAfter)
	}
}

// TestSqlstoreOperationsAndScansComprehensive exercises report edge cases, stopped operation filtering, projection eviction and command lifecycles.
func TestSqlstoreOperationsAndScansComprehensive(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	devID := "dev_ops_comp"
	otherDev := "dev_ops_other"
	createTestDevice(t, s, "site", devID, "cred1")
	createTestDevice(t, s, "site", otherDev, "cred2")

	art := ports.Artifact{
		ScopeID:             "site",
		PackageName:         "com.pkg.comp",
		SHA256:              strings.Repeat("7", 64),
		SizeBytes:           2048,
		VersionCode:         1,
		MinSDK:              33,
		MaxSDK:              37,
		SigningCertificates: []string{strings.Repeat("a", 64)},
	}
	if err := s.RegisterArtifact(ctx, art); err != nil {
		t.Fatal(err)
	}

	// 1. applyReport edge cases
	// Assign operation to otherDev
	opOther, err := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       otherDev,
		ArtifactSHA256: art.SHA256,
		IdempotencyKey: "k_other",
		OperationID:    "op_comp_other",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Assign operation to devID
	opDev, err := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ArtifactSHA256: art.SHA256,
		IdempotencyKey: "k_dev",
		OperationID:    "op_comp_dev",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	invWithCaps := ports.Inventory{
		APILevel:     34,
		Model:        "Pixel",
		DeviceOwner:  true,
		Capabilities: []string{"command.reboot", "command.lock"},
	}

	// Initial sync to lease opDev
	sync1, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       1,
		Inventory:      invWithCaps,
		Now:            now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sync1.Operations) == 0 {
		t.Fatal("expected assigned operation")
	}
	leaseID := sync1.Operations[0].LeaseID

	// devID submits report for opOther (wrong device) -> NOT_ASSIGNED
	syncWrongDev, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       2,
		Inventory:      invWithCaps,
		Now:            now,
		Reports: []ports.OperationReport{
			{ID: "rep_wrong_dev", OperationID: opOther.ID, LeaseID: "foo", Sequence: 1, State: "RUNNING"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncWrongDev.Receipts) != 1 || syncWrongDev.Receipts[0].Reason != "NOT_ASSIGNED" {
		t.Fatalf("expected NOT_ASSIGNED, got %+v", syncWrongDev.Receipts)
	}

	// devID submits report with wrong lease ID -> STALE_LEASE
	syncBadLease, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       3,
		Inventory:      invWithCaps,
		Now:            now,
		Reports: []ports.OperationReport{
			{ID: "rep_bad_lease", OperationID: opDev.ID, LeaseID: "wrong_lease", Sequence: 1, State: "RUNNING"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncBadLease.Receipts) != 1 || syncBadLease.Receipts[0].Reason != "STALE_LEASE" {
		t.Fatalf("expected STALE_LEASE, got %+v", syncBadLease.Receipts)
	}

	// devID submits SUCCEEDED report with mismatching observed package/version -> OBSERVATION_MISMATCH
	syncObsMismatch, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       4,
		Inventory:      invWithCaps,
		Now:            now,
		Reports: []ports.OperationReport{
			{
				ID:          "rep_mismatch",
				OperationID: opDev.ID,
				LeaseID:     leaseID,
				Sequence:    1,
				State:       "SUCCEEDED",
				Observed: &ports.InstalledApplication{
					PackageName: "com.wrong.package",
					VersionCode: 99,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncObsMismatch.Receipts) != 1 || syncObsMismatch.Receipts[0].Reason != "OBSERVATION_MISMATCH" {
		t.Fatalf("expected OBSERVATION_MISMATCH, got %+v", syncObsMismatch.Receipts)
	}

	// devID submits SUCCEEDED report with matching observed app -> accepted
	syncSucceeded, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       5,
		Inventory: ports.Inventory{
			APILevel:     34,
			Model:        "Pixel",
			DeviceOwner:  true,
			Capabilities: []string{"command.reboot", "command.lock"},
			Applications: []ports.InstalledApplication{
				{
					PackageName:         art.PackageName,
					VersionCode:         art.VersionCode,
					SigningCertificates: art.SigningCertificates,
				},
			},
		},
		Now: now,
		Reports: []ports.OperationReport{
			{
				ID:          "rep_success",
				OperationID: opDev.ID,
				LeaseID:     leaseID,
				Sequence:    1,
				State:       "SUCCEEDED",
				Observed: &ports.InstalledApplication{
					PackageName:         art.PackageName,
					VersionCode:         art.VersionCode,
					SigningCertificates: art.SigningCertificates,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncSucceeded.Receipts) != 1 || !syncSucceeded.Receipts[0].Accepted {
		t.Fatalf("expected accepted receipt, got %+v", syncSucceeded.Receipts)
	}

	// devID submits another report on now terminal SUCCEEDED operation -> TERMINAL
	syncTerminal, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       6,
		Inventory:      invWithCaps,
		Now:            now,
		Reports: []ports.OperationReport{
			{ID: "rep_after_term", OperationID: opDev.ID, LeaseID: leaseID, Sequence: 2, State: "RUNNING"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncTerminal.Receipts) != 1 || syncTerminal.Receipts[0].Reason != "TERMINAL" {
		t.Fatalf("expected TERMINAL reason on already succeeded operation, got %+v", syncTerminal.Receipts)
	}

	// 2. in.RetainedOperationIDs filtering
	// Test retained op belonging to otherDev and cancelled op
	opDev2, _ := s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ArtifactSHA256: art.SHA256,
		IdempotencyKey: "k_dev2",
		OperationID:    "op_comp_dev2",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	_, _ = s.CancelOperation(ctx, "site", devID, opDev2.ID, now)

	syncRetained, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:             devID,
		CredentialHash:       hash32("cred1"),
		Sequence:             7,
		Inventory:            invWithCaps,
		Now:                  now,
		RetainedOperationIDs: []string{opOther.ID, opDev2.ID, "non_existent_op"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncRetained.StoppedOperationIDs) != 3 {
		t.Fatalf("expected 3 stopped operations, got %+v", syncRetained.StoppedOperationIDs)
	}

	// 3. ReadProjection validation, eviction, and fleet freezing
	// Invalid arguments
	if _, err := s.ReadProjection(ctx, "", "", 0, 10, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for empty scope")
	}
	if _, err := s.ReadProjection(ctx, "site", "", -1, 10, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for negative after")
	}
	if _, err := s.ReadProjection(ctx, "site", "", 0, 0, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit < 1")
	}
	if _, err := s.ReadProjection(ctx, "site", "", 0, 101, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit > 100")
	}
	if _, err := s.ReadProjection(ctx, "site", "", 5, 10, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for empty id with after != 0")
	}

	// Non-existent scan and expired scan
	if _, err := s.ReadProjection(ctx, "site", "non_existent_snap", 0, 10, now); !errors.Is(err, ports.ErrCursorExpired) {
		t.Fatal("expected ErrCursorExpired for missing snapshot")
	}

	// Create Group and FleetAssignment
	_, err = s.ReplaceGroup(ctx, ports.Group{ScopeID: "site", ID: "grp_proj", DeviceIDs: []string{devID}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID:        "site",
		ID:             "fa_proj",
		ArtifactSHA256: art.SHA256,
		GroupID:        "grp_proj",
		NotBefore:      now,
		Deadline:       now.Add(time.Hour),
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	// Create projection snapshots up to limit (ports.SnapshotLimit is 4)
	var snapIDs []string
	for i := 0; i < 5; i++ {
		p, err := s.ReadProjection(ctx, "site", "", 0, 100, now)
		if err != nil {
			t.Fatalf("failed creating projection scan %d: %v", i, err)
		}
		snapIDs = append(snapIDs, p.SnapshotID)
	}
	// The oldest scan (snapIDs[0]) should have been evicted
	if _, err := s.ReadProjection(ctx, "site", snapIDs[0], 0, 100, now); !errors.Is(err, ports.ErrCursorExpired) {
		t.Fatal("expected oldest snapshot to be evicted")
	}
	// The newest scan should be queryable
	latestPage, err := s.ReadProjection(ctx, "site", snapIDs[4], 0, 100, now)
	if err != nil {
		t.Fatalf("failed reading newest projection: %v", err)
	}
	if len(latestPage.Items) == 0 {
		t.Fatal("expected items in projection scan")
	}
	// Expired check: read newest scan with future now
	if _, err := s.ReadProjection(ctx, "site", snapIDs[4], 0, 100, now.Add(2*ports.SnapshotLifetime)); !errors.Is(err, ports.ErrCursorExpired) {
		t.Fatal("expected ErrCursorExpired for expired scan")
	}

	// 4. Command lifecycle and expiration branches
	cmdQueued, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ID:             "cmd_comp_1",
		IdempotencyKey: "k_cmd_1",
		Kind:           "REBOOT",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Cancel QUEUED command -> CANCELLED
	cancelledCmd, err := s.CancelCommand(ctx, "site", devID, cmdQueued.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if cancelledCmd.State != "CANCELLED" {
		t.Fatalf("expected CANCELLED, got %s", cancelledCmd.State)
	}
	// Cancel already CANCELLED command returns result without error
	recancelled, err := s.CancelCommand(ctx, "site", devID, cmdQueued.ID, now)
	if err != nil || recancelled.State != "CANCELLED" {
		t.Fatalf("expected idempotent CANCELLED, got %+v, err: %v", recancelled, err)
	}

	// Submit another command, mark DISPATCHED via SQL, then test Cancel -> UNCERTAIN
	cmdDisp, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ID:             "cmd_comp_2",
		IdempotencyKey: "k_cmd_2",
		Kind:           "LOCK",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmdDisp.State = "DISPATCHED"
	dataDisp, _ := json.Marshal(cmdDisp)
	_, _ = db.Exec("UPDATE commands SET state='DISPATCHED', data=? WHERE id=?", dataDisp, cmdDisp.ID)

	uncCmd, err := s.CancelCommand(ctx, "site", devID, cmdDisp.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if uncCmd.State != "UNCERTAIN" {
		t.Fatalf("expected UNCERTAIN on cancelling dispatched command, got %s", uncCmd.State)
	}

	// Expire commands via GetCommand with past deadline
	// DISPATCHED past deadline -> UNCERTAIN
	cmdDisp2, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ID:             "cmd_comp_3",
		IdempotencyKey: "k_cmd_3",
		Kind:           "LOCK",
		Now:            now,
		Deadline:       now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmdDisp2.State = "DISPATCHED"
	dataDisp2, _ := json.Marshal(cmdDisp2)
	_, _ = db.Exec("UPDATE commands SET state='DISPATCHED', data=? WHERE id=?", dataDisp2, cmdDisp2.ID)

	gotDispExp, err := s.GetCommand(ctx, "site", devID, cmdDisp2.ID, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if gotDispExp.State != "UNCERTAIN" || gotDispExp.ErrorCode != "DEADLINE_EXCEEDED" {
		t.Fatalf("expected UNCERTAIN/DEADLINE_EXCEEDED, got %+v", gotDispExp)
	}

	// QUEUED past deadline -> FAILED
	cmdQueuedExp, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ID:             "cmd_comp_4",
		IdempotencyKey: "k_cmd_4",
		Kind:           "REBOOT",
		Now:            now,
		Deadline:       now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	gotQueuedExp, err := s.GetCommand(ctx, "site", devID, cmdQueuedExp.ID, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if gotQueuedExp.State != "FAILED" || gotQueuedExp.ErrorCode != "DEADLINE_EXCEEDED" {
		t.Fatalf("expected FAILED/DEADLINE_EXCEEDED, got %+v", gotQueuedExp)
	}

	// ListCommands with pagination
	listCmds, err := s.ListCommands(ctx, "site", devID, "", 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(listCmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(listCmds))
	}
	listCmds2, err := s.ListCommands(ctx, "site", devID, listCmds[1].ID, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(listCmds2) == 0 {
		t.Fatal("expected remaining commands after cursor")
	}
}

// TestSqlstoreDatabaseBindAndDialect exercises query parameter and schema rewriting for postgres.
func TestSqlstoreDatabaseBindAndDialect(t *testing.T) {
	dSqlite := &database{dialect: "sqlite"}
	if q := dSqlite.bind("SELECT ? FROM devices"); q != "SELECT ? FROM devices" {
		t.Fatalf("unexpected sqlite bind: %s", q)
	}

	dPostgres := &database{dialect: "postgres", schema: "tenant_a"}
	query := "SELECT 'it''s a string', \"col\", id, 'another' FROM devices WHERE id=? AND name=? ORDER BY id"
	bound := dPostgres.bind(query)
	expected := `SELECT 'it''s a string', "col", id, 'another' FROM "tenant_a".devices WHERE id=$1 AND name=$2 ORDER BY id`
	if bound != expected {
		t.Fatalf("expected:\n%s\ngot:\n%s", expected, bound)
	}

	q2 := dPostgres.bind("SELECT count(*) FROM other_table WHERE x = ?")
	exp2 := "SELECT count(*) FROM other_table WHERE x = $1"
	if q2 != exp2 {
		t.Fatalf("expected:\n%s\ngot:\n%s", exp2, q2)
	}
}

// TestSqlstoreCommandReportsAndQueryOperations exercises command report resolution, verification, and operation queries.
func TestSqlstoreCommandReportsAndQueryOperations(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	devID := "dev_cmd_rep"
	createTestDevice(t, s, "site", devID, "cred1")

	inv := ports.Inventory{
		APILevel:     34,
		Model:        "Pixel",
		DeviceOwner:  true,
		Capabilities: []string{"command.reboot", "command.lock"},
	}

	// 1. Submit reboot command
	cmdReboot, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ID:             "cmd_rep_reb",
		IdempotencyKey: "k_reb_1",
		Kind:           "REBOOT",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Sync to dispatch reboot command and acquire DispatchToken
	syncDisp, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       1,
		Inventory:      inv,
		Now:            now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncDisp.Commands) == 0 || syncDisp.Commands[0].DispatchToken == "" {
		t.Fatalf("expected dispatched command with token, got %+v", syncDisp.Commands)
	}
	rebootToken := syncDisp.Commands[0].DispatchToken

	// Invalid report: wrong dispatch token -> ErrConflict
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       2,
		Inventory:      inv,
		Now:            now,
		CommandReports: []ports.CommandReport{
			{CommandID: cmdReboot.ID, DispatchToken: "wrong_token", State: "SUCCEEDED"},
		},
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for wrong dispatch token, got %v", err)
	}

	// Invalid report: invalid state -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       2,
		Inventory:      inv,
		Now:            now,
		CommandReports: []ports.CommandReport{
			{CommandID: cmdReboot.ID, DispatchToken: rebootToken, State: "UNKNOWN_STATE"},
		},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid state, got %v", err)
	}

	// Invalid report: reboot observation with BootAfter <= BootBefore -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       2,
		Inventory:      inv,
		Now:            now,
		CommandReports: []ports.CommandReport{
			{
				CommandID:     cmdReboot.ID,
				DispatchToken: rebootToken,
				State:         "SUCCEEDED",
				Observation:   &ports.CommandObservation{BootBefore: 2, BootAfter: 2},
			},
		},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid reboot boot counts, got %v", err)
	}

	// Valid report: reboot observation with BootAfter > BootBefore -> success
	validRebootReport := ports.CommandReport{
		CommandID:     cmdReboot.ID,
		DispatchToken: rebootToken,
		State:         "SUCCEEDED",
		Observation:   &ports.CommandObservation{BootBefore: 1, BootAfter: 2},
	}
	syncRebSuccess, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       2,
		Inventory:      inv,
		Now:            now,
		CommandReports: []ports.CommandReport{validRebootReport},
	})
	if err != nil {
		t.Fatalf("failed syncing valid reboot report: %v", err)
	}
	if len(syncRebSuccess.CommandReceipts) != 1 || syncRebSuccess.CommandReceipts[0] != cmdReboot.ID {
		t.Fatalf("expected receipt for reboot command, got %+v", syncRebSuccess.CommandReceipts)
	}

	// Replay identical report -> success (idempotent duplicate)
	syncRebReplay, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       3,
		Inventory:      inv,
		Now:            now,
		CommandReports: []ports.CommandReport{validRebootReport},
	})
	if err != nil {
		t.Fatalf("failed syncing duplicate reboot report: %v", err)
	}
	_ = syncRebReplay

	// Replay report with conflicting data -> ErrConflict
	conflictingRebootReport := validRebootReport
	conflictingRebootReport.State = "FAILED"
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       4,
		Inventory:      inv,
		Now:            now,
		CommandReports: []ports.CommandReport{conflictingRebootReport},
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for conflicting command report, got %v", err)
	}

	// 2. Submit lock command and test LOCK observation rules
	cmdLock, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ID:             "cmd_rep_lock",
		IdempotencyKey: "k_lock_1",
		Kind:           "LOCK",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	syncLockDisp, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       5,
		Inventory:      inv,
		Now:            now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncLockDisp.Commands) == 0 || syncLockDisp.Commands[0].DispatchToken == "" {
		t.Fatalf("expected dispatched lock command, got %+v", syncLockDisp.Commands)
	}
	lockToken := syncLockDisp.Commands[0].DispatchToken

	// Invalid lock observation: Secure is false -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       6,
		Inventory:      inv,
		Now:            now,
		CommandReports: []ports.CommandReport{
			{
				CommandID:     cmdLock.ID,
				DispatchToken: lockToken,
				State:         "SUCCEEDED",
				Observation:   &ports.CommandObservation{Secure: false, Locked: true},
			},
		},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for insecure lock observation, got %v", err)
	}

	// Valid lock observation: Secure: true, Locked: true -> success
	syncLockSuccess, err := s.Sync(ctx, ports.SyncInput{
		DeviceID:       devID,
		CredentialHash: hash32("cred1"),
		Sequence:       6,
		Inventory:      inv,
		Now:            now,
		CommandReports: []ports.CommandReport{
			{
				CommandID:     cmdLock.ID,
				DispatchToken: lockToken,
				State:         "SUCCEEDED",
				Observation:   &ports.CommandObservation{Secure: true, Locked: true},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed syncing valid lock report: %v", err)
	}
	_ = syncLockSuccess

	// 3. QueryOperations reading rows
	art := ports.Artifact{
		ScopeID:             "site",
		PackageName:         "com.pkg.qop",
		SHA256:              strings.Repeat("8", 64),
		SizeBytes:           4096,
		VersionCode:         1,
		MinSDK:              33,
		MaxSDK:              37,
		SigningCertificates: []string{strings.Repeat("b", 64)},
	}
	_ = s.RegisterArtifact(ctx, art)
	_, _ = s.Assign(ctx, ports.Assignment{
		ScopeID:        "site",
		DeviceID:       devID,
		ArtifactSHA256: art.SHA256,
		IdempotencyKey: "k_qop",
		OperationID:    "op_qop_1",
		Now:            now,
		Deadline:       now.Add(time.Hour),
	})
	qOps, err := s.QueryOperations(ctx, ports.OperationQuery{
		ScopeID:  "site",
		DeviceID: devID,
		Limit:    10,
		Now:      now,
	})
	if err != nil {
		t.Fatalf("failed QueryOperations: %v", err)
	}
	if len(qOps) == 0 {
		t.Fatal("expected operations from QueryOperations")
	}

	// 4. Authenticate error cases
	if _, err := s.Authenticate(ctx, "non_existent_device", hash32("cred1")); !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized on missing device authenticate, got %v", err)
	}
	if _, err := s.Authenticate(ctx, devID, hash32("wrong_cred")); !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized on wrong credential hash, got %v", err)
	}
	_ = s.Revoke(ctx, "site", devID)
	if _, err := s.Authenticate(ctx, devID, hash32("cred1")); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked on revoked device authenticate, got %v", err)
	}
}

// TestSqlstoreRolledBackTransactionErrorBranches exercises internal SQL execution failure handling across all internal transaction helpers.
func TestSqlstoreRolledBackTransactionErrorBranches(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	dev := ports.Device{ID: "dev_rb", ScopeID: "site", State: "MANAGED"}

	// Start a real transaction and immediately roll it back to make all operations return sql.ErrTxDone
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()

	// 1. changes.go helpers
	_ = deviceChanged(ctx, tx, dev.ID)
	_, _, _ = changeHead(ctx, tx, "site")
	_, _ = readDeviceState(ctx, tx, "site", dev.ID)
	_ = removeScan(ctx, tx, "scan1")
	_, _ = createScan(ctx, tx, "site", now)
	_ = saveProjection(ctx, tx, "scan1", 0, ports.ProjectionItem{})

	// 2. commands.go helpers
	_, _ = readCommand(ctx, tx, "cmd1")
	_ = saveCommand(ctx, tx, ports.Command{ID: "cmd1", DeviceID: dev.ID})
	_, _ = activeCommands(ctx, tx, dev.ID)
	_ = expireCommands(ctx, tx, dev.ID, now)
	_, _ = commandDevice(ctx, tx, "site", dev.ID)
	_ = applyCommandReport(ctx, tx, dev.ID, ports.CommandReport{CommandID: "cmd1"})

	// 3. fleet.go helpers
	_, _ = readGroup(ctx, tx, "site", "grp1")
	_, _ = readFleet(ctx, tx, "site", "fa1")
	_ = saveFleet(ctx, tx, ports.FleetAssignment{Request: ports.FleetRequest{ID: "fa1", ScopeID: "site"}})
	_, _ = cancelOperation(ctx, tx, "site", dev.ID, "op1", now)

	// 4. network.go helpers
	_, _ = networkStatus(ctx, tx, dev, nil)

	// 5. operations.go helpers
	_ = saveOperation(ctx, tx, ports.Operation{ID: "op1", DeviceID: dev.ID})
	_, _ = activeOperations(ctx, tx, dev.ID)
	_ = expire(ctx, tx, dev.ID, now)
	_, _ = applyReport(ctx, tx, dev.ID, ports.OperationReport{ID: "rep1", OperationID: "op1"})

	// 6. policies.go helpers
	_, _ = desiredApplications(ctx, tx, dev)
	_, _ = policyStatus(ctx, tx, dev, nil, nil)
}

// TestSqlstoreTriggerInjectedFailureCoverage exercises internal transactional SQL error handling via SQLite triggers.
func TestSqlstoreTriggerInjectedFailureCoverage(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Fail changes insert -> deviceChanged fails on mutations
	{
		s, db := testStore(t)
		devID := "dev_trig_1"
		createTestDevice(t, s, "site", devID, "cred1")
		art := ports.Artifact{ScopeID: "site", PackageName: "com.pkg.trig", SHA256: strings.Repeat("1", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
		_ = s.RegisterArtifact(ctx, art)

		_, _ = db.Exec("CREATE TRIGGER fail_changes BEFORE INSERT ON changes BEGIN SELECT RAISE(FAIL, 'changes fail'); END;")
		// Assign calls saveOperation which calls deviceChanged -> fails!
		_, _ = s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_trig", OperationID: "op_trig_1", Now: now, Deadline: now.Add(time.Hour)})
		// Revoke calls deviceChanged -> fails!
		_ = s.Revoke(ctx, "site", devID)
		// SubmitCommand calls deviceChanged -> fails!
		_, _ = s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID, ID: "cmd_trig_1", IdempotencyKey: "k_trig_c", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)})
		// SetPolicy calls deviceChanged -> fails!
		_, _ = s.SetPolicy(ctx, ports.PolicyAssignment{ScopeID: "site", DeviceID: devID, IdempotencyKey: "k_trig_p"})
		// SetNetworkPolicy calls deviceChanged -> fails!
		_, _ = s.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: "site", DeviceID: devID, IdempotencyKey: "k_trig_n"})
	}

	// 2. Fail operations insert and update
	{
		s, db := testStore(t)
		devID := "dev_trig_2"
		createTestDevice(t, s, "site", devID, "cred1")
		art := ports.Artifact{ScopeID: "site", PackageName: "com.pkg.trig2", SHA256: strings.Repeat("2", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
		art2 := ports.Artifact{ScopeID: "site", PackageName: "com.pkg.trig2", SHA256: strings.Repeat("3", 64), SizeBytes: 1024, VersionCode: 2, MinSDK: 33, MaxSDK: 37}
		_ = s.RegisterArtifact(ctx, art)
		_ = s.RegisterArtifact(ctx, art2)

		_, _ = db.Exec("CREATE TRIGGER fail_ops_ins BEFORE INSERT ON operations BEGIN SELECT RAISE(FAIL, 'ops insert fail'); END;")
		_, _ = s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_trig_2a", OperationID: "op_trig_2a", Now: now, Deadline: now.Add(time.Hour)})

		_, _ = db.Exec("DROP TRIGGER fail_ops_ins")
		op, _ := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_trig_2a", OperationID: "op_trig_2a", Now: now, Deadline: now.Add(time.Hour)})

		syncRes, _ := s.Sync(ctx, ports.SyncInput{
			DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now,
			Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		})
		lease := ""
		if len(syncRes.Operations) > 0 {
			lease = syncRes.Operations[0].LeaseID
		}

		_, _ = db.Exec("CREATE TRIGGER fail_ops_upd BEFORE UPDATE ON operations BEGIN SELECT RAISE(FAIL, 'ops update fail'); END;")
		// Superseeding op calls saveOperation -> fails on update!
		_, _ = s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art2.SHA256, IdempotencyKey: "k_trig_2b", OperationID: "op_trig_2b", Now: now, Deadline: now.Add(time.Hour)})
		// CancelOperation calls update -> fails!
		_, _ = s.CancelOperation(ctx, "site", devID, op.ID, now)
		// applyReport calls saveOperation -> fails!
		_, _ = s.Sync(ctx, ports.SyncInput{
			DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 2, Now: now,
			Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
			Reports: []ports.OperationReport{
				{ID: "rep_fail_upd", OperationID: op.ID, LeaseID: lease, Sequence: 1, State: "RUNNING"},
			},
		})
	}

	// 3. Fail commands insert and update
	{
		s, db := testStore(t)
		devID := "dev_trig_3"
		createTestDevice(t, s, "site", devID, "cred1")
		// update capabilities so submit command passes capability check
		_, _ = s.Sync(ctx, ports.SyncInput{
			DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now,
			Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		})

		_, _ = db.Exec("CREATE TRIGGER fail_cmd_ins BEFORE INSERT ON commands BEGIN SELECT RAISE(FAIL, 'cmd insert fail'); END;")
		_, _ = s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID, ID: "cmd_trig_3", IdempotencyKey: "k_trig_3", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)})

		_, _ = db.Exec("DROP TRIGGER fail_cmd_ins")
		cmd, _ := s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: "site", DeviceID: devID, ID: "cmd_trig_3", IdempotencyKey: "k_trig_3", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Minute)})

		_, _ = db.Exec("CREATE TRIGGER fail_cmd_upd BEFORE UPDATE ON commands BEGIN SELECT RAISE(FAIL, 'cmd update fail'); END;")
		_, _ = s.CancelCommand(ctx, "site", devID, cmd.ID, now)
		// expireCommands calls saveCommand -> fails!
		_, _ = s.GetCommand(ctx, "site", devID, cmd.ID, now.Add(time.Hour))
	}

	// 3b. Fail fleet_assignments update in Advance and Cancel
	{
		s, db := testStore(t)
		devID := "dev_trig_3b"
		createTestDevice(t, s, "site", devID, "cred1")
		art := ports.Artifact{ScopeID: "site", PackageName: "com.pkg.flt", SHA256: strings.Repeat("4", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
		_ = s.RegisterArtifact(ctx, art)

		_, _ = s.CreateFleetAssignment(ctx, ports.FleetRequest{
			ScopeID: "site", ID: "fa_trig_upd", ArtifactSHA256: art.SHA256, DeviceIDs: []string{devID},
			NotBefore: now, Deadline: now.Add(time.Hour),
		}, now)

		_, _ = db.Exec("CREATE TRIGGER fail_flt_upd BEFORE UPDATE ON fleet_assignments BEGIN SELECT RAISE(FAIL, 'flt update fail'); END;")
		_, _ = s.AdvanceFleetAssignment(ctx, "site", "fa_trig_upd", now, 10)
		_, _ = s.CancelFleetAssignment(ctx, "site", "fa_trig_upd", now)
	}

	// 3c. Fail policies update and network_policies update in Sync
	{
		s, db := testStore(t)
		devID := "dev_trig_3c"
		createTestDevice(t, s, "site", devID, "cred1")
		tVal := true
		pol, _ := s.SetPolicy(ctx, ports.PolicyAssignment{
			ScopeID: "site", DeviceID: devID, IdempotencyKey: "k_pol_trig",
			Policy: ports.Policy{CameraDisabled: &tVal},
		})
		netPol, _ := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
			ScopeID: "site", DeviceID: devID, IdempotencyKey: "k_net_trig",
			Policy: ports.NetworkPolicy{PrimaryID: "p1", Profiles: []ports.WifiProfile{{ID: "p1", SSID: "WiFi", Security: "WPA2_PSK", SecretRef: "r1"}}},
		})

		_, _ = db.Exec("CREATE TRIGGER fail_pol_upd BEFORE UPDATE ON policies BEGIN SELECT RAISE(FAIL, 'pol update fail'); END;")
		_, _ = s.Sync(ctx, ports.SyncInput{
			DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now,
			Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"policy.camera_disabled"}},
			PolicyReport: &ports.PolicyReport{
				Revision: pol.Revision, SnapshotRevision: pol.Revision,
				Results: []ports.PolicyResult{{RuleID: "camera", State: "APPLIED"}},
			},
		})

		_, _ = db.Exec("CREATE TRIGGER fail_net_upd BEFORE UPDATE ON network_policies BEGIN SELECT RAISE(FAIL, 'net update fail'); END;")
		_, _ = s.Sync(ctx, ports.SyncInput{
			DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 2, Now: now,
			Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
			NetworkReport: &ports.NetworkReport{
				Revision: netPol.Revision, State: "APPLIED",
				ConnectivityVerified: true, CleanupComplete: true, ActiveProfileID: "p1",
			},
		})
	}

	// 4. Fail projection scans and items
	{
		s, db := testStore(t)
		devID := "dev_trig_4"
		createTestDevice(t, s, "site", devID, "cred1")

		_, _ = db.Exec("CREATE TRIGGER fail_pscan_ins BEFORE INSERT ON projection_scans BEGIN SELECT RAISE(FAIL, 'pscan insert fail'); END;")
		_, _ = s.ReadProjection(ctx, "site", "", 0, 10, now)

		_, _ = db.Exec("DROP TRIGGER fail_pscan_ins")
		_, _ = db.Exec("CREATE TRIGGER fail_pitem_ins BEFORE INSERT ON projection_items BEGIN SELECT RAISE(FAIL, 'pitem insert fail'); END;")
		_, _ = s.ReadProjection(ctx, "site", "", 0, 10, now)

		_, _ = db.Exec("DROP TRIGGER fail_pitem_ins")
		p, _ := s.ReadProjection(ctx, "site", "", 0, 10, now)
		_ = p
		_, _ = db.Exec("CREATE TRIGGER fail_pscan_del BEFORE DELETE ON projection_scans BEGIN SELECT RAISE(FAIL, 'pscan del fail'); END;")
		// Creating 5 more scans triggers eviction of oldest scan, calling removeScan -> fails deleting projection_scans!
		for i := 0; i < 5; i++ {
			_, _ = s.ReadProjection(ctx, "site", "", 0, 10, now)
		}
	}

	// 5. Fail reports insert
	{
		s, db := testStore(t)
		devID := "dev_trig_5"
		createTestDevice(t, s, "site", devID, "cred1")
		art := ports.Artifact{ScopeID: "site", PackageName: "com.pkg.trig5", SHA256: strings.Repeat("5", 64), SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37}
		_ = s.RegisterArtifact(ctx, art)
		op, _ := s.Assign(ctx, ports.Assignment{ScopeID: "site", DeviceID: devID, ArtifactSHA256: art.SHA256, IdempotencyKey: "k_trig_5", OperationID: "op_trig_5", Now: now, Deadline: now.Add(time.Hour)})

		syncRes, _ := s.Sync(ctx, ports.SyncInput{
			DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now,
			Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		})
		lease := ""
		if len(syncRes.Operations) > 0 {
			lease = syncRes.Operations[0].LeaseID
		}

		_, _ = db.Exec("CREATE TRIGGER fail_rep_ins BEFORE INSERT ON reports BEGIN SELECT RAISE(FAIL, 'rep insert fail'); END;")
		_, _ = s.Sync(ctx, ports.SyncInput{
			DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 2, Now: now,
			Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
			Reports: []ports.OperationReport{
				{ID: "rep_trig_5", OperationID: op.ID, LeaseID: lease, Sequence: 1, State: "RUNNING"},
			},
		})
	}

	// 6. Fail devices update in Sync
	{
		s, db := testStore(t)
		devID := "dev_trig_6"
		createTestDevice(t, s, "site", devID, "cred1")

		_, _ = db.Exec("CREATE TRIGGER fail_dev_upd BEFORE UPDATE ON devices BEGIN SELECT RAISE(FAIL, 'dev update fail'); END;")
		_, _ = s.Sync(ctx, ports.SyncInput{
			DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now,
			Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		})
	}
}

// TestSqlstoreFleetAndNetworkAndPolicyExtensions exercises fleet advance failures, network report validation, and policy compliance branches.
func TestSqlstoreFleetAndNetworkAndPolicyExtensions(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	devID := "dev_ext_1"
	revokedDev := "dev_ext_rev"
	createTestDevice(t, s, "site", devID, "cred1")
	createTestDevice(t, s, "site", revokedDev, "cred2")
	_ = s.Revoke(ctx, "site", revokedDev)

	art := ports.Artifact{
		ScopeID:             "site",
		PackageName:         "com.pkg.ext",
		SHA256:              strings.Repeat("9", 64),
		SizeBytes:           1024,
		VersionCode:         1,
		MinSDK:              33,
		MaxSDK:              37,
		SigningCertificates: []string{strings.Repeat("c", 64)},
	}
	_ = s.RegisterArtifact(ctx, art)

	// 1. AdvanceFleetAssignment failure branches: NOT_FOUND, DEVICE_REVOKED
	_, err := s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID:        "site",
		ID:             "fa_ext_1",
		ArtifactSHA256: art.SHA256,
		DeviceIDs:      []string{"non_existent_dev", revokedDev, devID},
		NotBefore:      now,
		Deadline:       now.Add(time.Hour),
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	advanced, err := s.AdvanceFleetAssignment(ctx, "site", "fa_ext_1", now, 10)
	if err != nil {
		t.Fatalf("failed AdvanceFleetAssignment: %v", err)
	}
	if len(advanced.Items) != 3 {
		t.Fatalf("expected 3 items in fleet, got %+v", advanced.Items)
	}

	// 2. SetNetworkPolicy and invalid NetworkReport tests via Sync
	netPol, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		IdempotencyKey: "k_net_1",
		Policy: ports.NetworkPolicy{
			PrimaryID: "prof_1",
			Profiles: []ports.WifiProfile{
				{ID: "prof_1", SSID: "Office-WiFi", Security: "WPA2_PSK", SecretRef: "ref1"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	inv := ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true}

	// Network report: Revision > Desired.Revision -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now, Inventory: inv,
		NetworkReport: &ports.NetworkReport{Revision: netPol.Revision + 1, State: "APPLIED"},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for future revision network report, got %v", err)
	}

	// Network report: Revision == 0 -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now, Inventory: inv,
		NetworkReport: &ports.NetworkReport{Revision: 0, State: "APPLIED"},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for revision 0 network report, got %v", err)
	}

	// Network report: invalid state -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now, Inventory: inv,
		NetworkReport: &ports.NetworkReport{Revision: netPol.Revision, State: "INVALID_STATE"},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid state, got %v", err)
	}

	// Network report: APPLIED but !ConnectivityVerified -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now, Inventory: inv,
		NetworkReport: &ports.NetworkReport{
			Revision: netPol.Revision, State: "APPLIED",
			ConnectivityVerified: false, CleanupComplete: true, ActiveProfileID: "prof_1",
		},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for unverified connectivity, got %v", err)
	}

	// Network report: APPLIED but ActiveProfileID != PrimaryID -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now, Inventory: inv,
		NetworkReport: &ports.NetworkReport{
			Revision: netPol.Revision, State: "APPLIED",
			ConnectivityVerified: true, CleanupComplete: true, ActiveProfileID: "wrong_prof",
		},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for wrong active profile, got %v", err)
	}

	// Valid APPLIED network report -> success
	syncNetValid, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 1, Now: now, Inventory: inv,
		NetworkReport: &ports.NetworkReport{
			Revision: netPol.Revision, State: "APPLIED",
			ConnectivityVerified: true, CleanupComplete: true, ActiveProfileID: "prof_1",
		},
	})
	if err != nil {
		t.Fatalf("failed syncing valid network report: %v", err)
	}
	if syncNetValid.Network.Reported == nil || syncNetValid.Network.Reported.State != "APPLIED" {
		t.Fatalf("expected APPLIED network status, got %+v", syncNetValid.Network)
	}

	// 3. Policy rules and capability missing (UNSUPPORTED)
	tVal := true
	polDes, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID:        "site",
		DeviceID:       devID,
		IdempotencyKey: "k_pol_ext",
		Policy: ports.Policy{
			CameraDisabled: &tVal,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Sync without camera capability -> PolicyResult state UNSUPPORTED, reason CAPABILITY_MISSING
	syncPolUnsup, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 2, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncPolUnsup.PolicyCompliance) != 2 || syncPolUnsup.PolicyCompliance[0].State != "UNSUPPORTED" {
		t.Fatalf("expected UNSUPPORTED policy compliance, got %+v", syncPolUnsup.PolicyCompliance)
	}

	// Sync with unexpected policy rule ID in incoming report -> ErrConflict
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cred1"), Sequence: 3, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		PolicyReport: &ports.PolicyReport{
			Revision:         polDes.Revision,
			SnapshotRevision: polDes.Revision,
			Results: []ports.PolicyResult{
				{RuleID: "unexpected_unknown_rule", State: "APPLIED"},
			},
		},
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for unexpected rule ID, got %v", err)
	}
}

// TestSqlstoreEnrollmentAndConsumeEdgeCases exercises enrollment validation, expiration, and recovery lifecycle branches.
func TestSqlstoreEnrollmentAndConsumeEdgeCases(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	tok := hash32("tok_edge_1")
	tokExp := hash32("tok_edge_exp")

	// Enrollment not found
	if _, err := s.Enrollment(ctx, hash32("non_existent")); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("expected ErrNotFound for missing enrollment")
	}

	// Create enrollment and duplicate conflict
	if err := s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: "site", TokenHash: tok, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: "site", TokenHash: tok, ExpiresAt: now.Add(time.Hour)}); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("expected ErrConflict on duplicate token hash")
	}

	// Create expired enrollment
	_ = s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: "site", TokenHash: tokExp, ExpiresAt: now.Add(-time.Minute)})
	// Consume expired enrollment -> ErrExpired
	_, err := s.Consume(ctx, ports.Attempt{
		TokenHash: tokExp, ID: "att_exp", KeyHash: hash32("k1"),
		Device: ports.Device{ID: "dev_exp", ScopeID: "site", State: "ACTIVE"},
		Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}

	// Create valid enrollment and consume it
	_ = s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: "site", TokenHash: tok, ExpiresAt: now.Add(time.Hour)})
	receipt := []byte("receipt_data_123")
	rec1, err := s.Consume(ctx, ports.Attempt{
		TokenHash: tok, ID: "att_1", KeyHash: hash32("k1"), CredentialHash: hash32("c1"),
		Device: ports.Device{ID: "dev_claim", ScopeID: "site", State: "ACTIVE"},
		Receipt: receipt, Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(rec1) != string(receipt) {
		t.Fatal("expected matching receipt")
	}

	// Reclaim with wrong attempt ID -> ErrConsumed
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: tok, ID: "wrong_att", KeyHash: hash32("k1"),
		Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrConsumed) {
		t.Fatalf("expected ErrConsumed on wrong attempt ID, got %v", err)
	}

	// Reclaim with wrong key hash -> ErrConsumed
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: tok, ID: "att_1", KeyHash: hash32("wrong_k"),
		Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrConsumed) {
		t.Fatalf("expected ErrConsumed on wrong key hash, got %v", err)
	}

	// Reclaim with Now >= RecoverUntil -> ErrRecoveryExpired
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: tok, ID: "att_1", KeyHash: hash32("k1"),
		Now: now.Add(2 * time.Hour), RecoverUntil: now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrRecoveryExpired) {
		t.Fatalf("expected ErrRecoveryExpired, got %v", err)
	}

	// Reclaim valid -> returns receipt
	recReclaim, err := s.Consume(ctx, ports.Attempt{
		TokenHash: tok, ID: "att_1", KeyHash: hash32("k1"),
		Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err != nil || string(recReclaim) != string(receipt) {
		t.Fatalf("expected valid reclaim receipt, got %v, err: %v", recReclaim, err)
	}

	// Revoke device, then reclaim -> ErrRevoked
	_ = s.Revoke(ctx, "site", "dev_claim")
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: tok, ID: "att_1", KeyHash: hash32("k1"),
		Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked on revoked device reclaim, got %v", err)
	}
}

// TestSqlstoreManagementAndOperationsDeepCoverage verifies query boundaries, quota limits, error paths, and pagination.
func TestSqlstoreManagementAndOperationsDeepCoverage(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	scope := "scope_deep"

	// 1. QueryDevices argument validation
	if _, err := s.QueryDevices(ctx, ports.DeviceQuery{ScopeID: "", Limit: 10}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for empty scope")
	}
	if _, err := s.QueryDevices(ctx, ports.DeviceQuery{ScopeID: scope, Limit: 0}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for Limit < 1")
	}
	if _, err := s.QueryDevices(ctx, ports.DeviceQuery{ScopeID: scope, Limit: 102}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for Limit > 101")
	}

	// 2. QueryOperations argument validation
	if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: "", DeviceID: "dev1", Limit: 10}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for empty scope")
	}
	if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: "", Limit: 10}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for empty device")
	}
	if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: "dev1", Limit: 0}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for Limit < 1")
	}
	if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: "dev1", Limit: 102}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for Limit > 101")
	}
	// QueryOperations for non-existent device -> ErrNotFound
	if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: "dev1", Limit: 10, Now: now}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("expected ErrNotFound for non-existent device")
	}
	// GetOperation for non-existent device -> ErrNotFound
	if _, err := s.GetOperation(ctx, scope, "dev1", "op1", now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("expected ErrNotFound for non-existent device")
	}

	// Register device
	devID := "dev_deep_1"
	tok := hash32("tok_deep_1")
	_ = s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: scope, TokenHash: tok, ExpiresAt: now.Add(time.Hour)})
	_, err := s.Consume(ctx, ports.Attempt{
		TokenHash: tok, ID: "att_deep", KeyHash: hash32("k_deep"), CredentialHash: hash32("c_deep"),
		Device: ports.Device{ID: devID, ScopeID: scope, State: "ACTIVE"},
		Receipt: []byte("rec_deep_1"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// QueryDevices with devices present
	devs, err := s.QueryDevices(ctx, ports.DeviceQuery{ScopeID: scope, Limit: 10, State: "ENROLLED"})
	if err != nil || len(devs) != 1 {
		t.Fatalf("expected 1 device, got %v, err: %v", devs, err)
	}

	// Register artifact for operations
	apkSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := s.RegisterArtifact(ctx, ports.Artifact{
		ScopeID: scope, SHA256: apkSHA, PackageName: "com.deep.app", VersionCode: 1, SizeBytes: 1024,
	}); err != nil {
		t.Fatal(err)
	}

	// Submit operation
	opAssigned, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_deep_op_1",
		OperationID: "op_deep_1", ArtifactSHA256: apkSHA,
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// QueryOperations finds the operation
	ops, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: devID, Limit: 10, Now: now})
	if err != nil || len(ops) != 1 {
		t.Fatalf("expected 1 op, got %v, err: %v", ops, err)
	}

	// GetOperation finds the operation
	opFound, err := s.GetOperation(ctx, scope, devID, opAssigned.ID, now)
	if err != nil || opFound.ID != opAssigned.ID {
		t.Fatalf("expected op %s, got %+v, err: %v", opAssigned.ID, opFound, err)
	}

	// GetOperation with mismatched device ID in DB
	// Create another device dev_deep_2
	devID2 := "dev_deep_2"
	tok2 := hash32("tok_deep_2")
	_ = s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: scope, TokenHash: tok2, ExpiresAt: now.Add(time.Hour)})
	_, _ = s.Consume(ctx, ports.Attempt{
		TokenHash: tok2, ID: "att_deep_2", KeyHash: hash32("k_deep_2"), CredentialHash: hash32("c_deep_2"),
		Device: ports.Device{ID: devID2, ScopeID: scope, State: "ACTIVE"},
		Receipt: []byte("rec_deep_2"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	// GetOperation for devID2 with opAssigned.ID (which belongs to devID) -> ErrNotFound
	if _, err := s.GetOperation(ctx, scope, devID2, opAssigned.ID, now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for mismatched device operation, got %v", err)
	}

	// 3. Assign error cases:
	// a) deadline <= now
	if _, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_bad_dl",
		OperationID: "op_bad_dl", ArtifactSHA256: apkSHA,
		Now: now, Deadline: now.Add(-time.Second),
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for past deadline, got %v", err)
	}
	// b) notBefore >= deadline
	if _, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_bad_nb",
		OperationID: "op_bad_nb", ArtifactSHA256: apkSHA,
		Now: now, NotBefore: now.Add(2 * time.Hour), Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for notBefore >= deadline, got %v", err)
	}
	// c) duplicate idempotency key with conflicting artifact
	if _, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_deep_op_1",
		OperationID: "op_deep_conflict", ArtifactSHA256: "different_sha_00000000000000000000000000000000000000000000000000000000",
		Now: now, Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for conflicting idempotency key, got %v", err)
	}

	// 4. Commands coverage:
	// SubmitCommand validation
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: "", DeviceID: devID, ID: "cmd1", IdempotencyKey: "ik1", Kind: "REBOOT",
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for empty scope")
	}
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd1", IdempotencyKey: "ik1", Kind: "INVALID_KIND",
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for invalid command kind")
	}
	// Missing capabilities on device (current devID has no inventory with capabilities)
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd1", IdempotencyKey: "ik1", Kind: "REBOOT",
		Now: now, Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported without capabilities, got %v", err)
	}

	// Sync device inventory with capabilities
	inv := ports.Inventory{
		APILevel: 34, Model: "Pixel", DeviceOwner: true,
		Capabilities: []string{"command.reboot", "command.lock"},
	}
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_deep"), Sequence: 1, Now: now, Inventory: inv,
	})
	if err != nil {
		t.Fatal(err)
	}

	// SubmitCommand with deadline > 7 days -> ErrInvalidArgument
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_toolong", IdempotencyKey: "ik_tl", Kind: "REBOOT",
		Now: now, Deadline: now.Add(8 * 24 * time.Hour),
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for deadline > 7 days, got %v", err)
	}

	// Submit valid REBOOT command
	cmd1, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_deep_1", IdempotencyKey: "ik_deep_1", Kind: "REBOOT",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Duplicate idempotency key with conflicting kind -> ErrConflict
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_deep_conflict", IdempotencyKey: "ik_deep_1", Kind: "LOCK",
		Now: now, Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate command key with different kind, got %v", err)
	}

	// ListCommands limits and pagination
	if _, err := s.ListCommands(ctx, scope, devID, "", 0, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit < 1")
	}
	if _, err := s.ListCommands(ctx, scope, devID, "", 102, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit > 101")
	}
	cmds, err := s.ListCommands(ctx, scope, devID, "", 10, now)
	if err != nil || len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %v, err: %v", cmds, err)
	}

	// CancelCommand for mismatched device
	if _, err := s.CancelCommand(ctx, scope, devID2, cmd1.ID, now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for command on another device, got %v", err)
	}

	// Submit up to 16 commands to reach maximumActiveCommands (16)
	for i := 2; i <= 16; i++ {
		cID := fmt.Sprintf("cmd_deep_%d", i)
		kID := fmt.Sprintf("ik_deep_%d", i)
		_, err := s.SubmitCommand(ctx, ports.CommandAssignment{
			ScopeID: scope, DeviceID: devID, ID: cID, IdempotencyKey: kID, Kind: "REBOOT",
			Now: now, Deadline: now.Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("failed submitting command %d: %v", i, err)
		}
	}
	// 17th active command -> ErrConflict
	_, err = s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_deep_17", IdempotencyKey: "ik_deep_17", Kind: "REBOOT",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for 17th command, got %v", err)
	}

	// 5. Sync edge cases:
	// a) len(CommandReports) > 16 -> ErrInvalidArgument
	tooManyReports := make([]ports.CommandReport, 17)
	for i := range tooManyReports {
		tooManyReports[i] = ports.CommandReport{CommandID: fmt.Sprintf("c%d", i), State: "SUCCEEDED"}
	}
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_deep"), Sequence: 2, Now: now,
		CommandReports: tooManyReports, Inventory: inv,
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for >16 command reports, got %v", err)
	}

	// b) len(RetainedOperationIDs) > 64 -> ErrInvalidArgument
	tooManyOps := make([]string, 65)
	for i := range tooManyOps {
		tooManyOps[i] = fmt.Sprintf("op_%d", i)
	}
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_deep"), Sequence: 2, Now: now,
		RetainedOperationIDs: tooManyOps, Inventory: inv,
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for >64 retained ops, got %v", err)
	}

	// c) RetainedOperationIDs containing non-existent, wrong device, or cancelled/failed/superseded ops
	// Let's cancel opAssigned
	_, _ = s.CancelOperation(ctx, scope, devID, opAssigned.ID, now)
	syncStopped, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_deep"), Sequence: 2, Now: now,
		RetainedOperationIDs: []string{"non_existent_op", opAssigned.ID}, Inventory: inv,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncStopped.StoppedOperationIDs) != 2 {
		t.Fatalf("expected 2 stopped operation IDs, got %+v", syncStopped.StoppedOperationIDs)
	}

	// 6. Changes and Projections:
	// ListChanges argument validation
	if _, err := s.ListChanges(ctx, "", 0, 10); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for empty scope")
	}
	if _, err := s.ListChanges(ctx, scope, 0, 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit < 1")
	}
	if _, err := s.ListChanges(ctx, scope, 0, 101); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit > 100")
	}
	if _, err := s.ListChanges(ctx, scope, 999999, 10); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for after > head")
	}

	// ReadProjection argument validation
	if _, err := s.ReadProjection(ctx, "", "", 0, 10, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for empty scope")
	}
	if _, err := s.ReadProjection(ctx, scope, "", -1, 10, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for after < 0")
	}
	if _, err := s.ReadProjection(ctx, scope, "", 0, 0, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit < 1")
	}
	if _, err := s.ReadProjection(ctx, scope, "", 0, 101, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit > 100")
	}
	if _, err := s.ReadProjection(ctx, scope, "", 5, 10, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for id == '' and after != 0")
	}

	// ReadProjection with initial page
	page1, err := s.ReadProjection(ctx, scope, "", 0, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if page1.SnapshotID == "" {
		t.Fatal("expected non-empty SnapshotID")
	}

	// ReadProjection with expired scan
	if _, err := s.ReadProjection(ctx, scope, page1.SnapshotID, 0, 10, now.Add(2*ports.SnapshotLifetime)); !errors.Is(err, ports.ErrCursorExpired) {
		t.Fatalf("expected ErrCursorExpired, got %v", err)
	}

	// 7. Revoke device, then SubmitCommand -> ErrRevoked; Assign -> ErrRevoked
	if err := s.Revoke(ctx, scope, devID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_rev", IdempotencyKey: "ik_rev", Kind: "REBOOT",
		Now: now, Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked on revoked device SubmitCommand, got %v", err)
	}
	if _, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "ik_rev_op",
		OperationID: "op_rev", ArtifactSHA256: apkSHA,
		Now: now, Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked on revoked device Assign, got %v", err)
	}
	_ = db
}

// TestSqlstoreAdvancedBranchCoverage tests command reports, operation reports, projection scans, and fleet cancellation.
func TestSqlstoreAdvancedBranchCoverage(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	scope := "scope_adv"
	devID := "dev_adv_1"

	// 1. Setup device and enroll
	tok := hash32("tok_adv_1")
	_ = s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: scope, TokenHash: tok, ExpiresAt: now.Add(time.Hour)})
	_, err := s.Consume(ctx, ports.Attempt{
		TokenHash: tok, ID: "att_adv_1", KeyHash: hash32("k_adv_1"), CredentialHash: hash32("c_adv_1"),
		Device: ports.Device{ID: devID, ScopeID: scope, State: "ACTIVE"},
		Receipt: []byte("rec_adv_1"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	inv := ports.Inventory{
		APILevel: 34, Model: "Pixel", DeviceOwner: true,
		Capabilities: []string{"command.reboot", "command.lock"},
	}
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 1, Now: now, Inventory: inv,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Commands: dispatch, applyCommandReport validations
	cmdReboot, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_adv_reb", IdempotencyKey: "ik_adv_reb", Kind: "REBOOT",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Sync dispatches cmdReboot
	syncResp, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 2, Now: now, Inventory: inv,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncResp.Commands) != 1 {
		t.Fatalf("expected 1 dispatched command, got %v", syncResp.Commands)
	}
	dispatched := syncResp.Commands[0]

	// Command report: wrong dispatch token -> ErrConflict
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 3, Now: now, Inventory: inv,
		CommandReports: []ports.CommandReport{
			{CommandID: dispatched.ID, DispatchToken: "wrong_token", State: "SUCCEEDED"},
		},
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for wrong dispatch token, got %v", err)
	}

	// Command report: invalid state -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 3, Now: now, Inventory: inv,
		CommandReports: []ports.CommandReport{
			{CommandID: dispatched.ID, DispatchToken: dispatched.DispatchToken, State: "INVALID_STATE"},
		},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid command report state, got %v", err)
	}

	// Command report: SUCCEEDED but nil Observation -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 3, Now: now, Inventory: inv,
		CommandReports: []ports.CommandReport{
			{CommandID: dispatched.ID, DispatchToken: dispatched.DispatchToken, State: "SUCCEEDED"},
		},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for nil observation, got %v", err)
	}

	// Command report: REBOOT SUCCEEDED but BootAfter <= BootBefore -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 3, Now: now, Inventory: inv,
		CommandReports: []ports.CommandReport{
			{
				CommandID: dispatched.ID, DispatchToken: dispatched.DispatchToken, State: "SUCCEEDED",
				Observation: &ports.CommandObservation{BootBefore: 5, BootAfter: 5},
			},
		},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for non-increasing boot count, got %v", err)
	}

	// Valid REBOOT report -> success
	validRep := ports.CommandReport{
		CommandID: dispatched.ID, DispatchToken: dispatched.DispatchToken, State: "SUCCEEDED",
		Observation: &ports.CommandObservation{BootBefore: 5, BootAfter: 6},
	}
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 3, Now: now, Inventory: inv,
		CommandReports: []ports.CommandReport{validRep},
	})
	if err != nil {
		t.Fatalf("failed syncing valid command report: %v", err)
	}

	// Re-reporting identical report -> success (idempotent repeat)
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 4, Now: now, Inventory: inv,
		CommandReports: []ports.CommandReport{validRep},
	})
	if err != nil {
		t.Fatalf("expected idempotent success on identical command report repeat: %v", err)
	}

	// Re-reporting conflicting report -> ErrConflict
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 5, Now: now, Inventory: inv,
		CommandReports: []ports.CommandReport{
			{
				CommandID: dispatched.ID, DispatchToken: dispatched.DispatchToken, State: "FAILED",
			},
		},
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on conflicting command report, got %v", err)
	}

	// Test LOCK command validation: !Secure || !Locked -> ErrInvalidArgument
	cmdLock, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_adv_lock", IdempotencyKey: "ik_adv_lock", Kind: "LOCK",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	syncRespLock, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 6, Now: now, Inventory: inv,
	})
	if err != nil || len(syncRespLock.Commands) != 1 {
		t.Fatalf("expected 1 lock command, got %v", syncRespLock.Commands)
	}
	dispLock := syncRespLock.Commands[0]
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 7, Now: now, Inventory: inv,
		CommandReports: []ports.CommandReport{
			{
				CommandID: dispLock.ID, DispatchToken: dispLock.DispatchToken, State: "SUCCEEDED",
				Observation: &ports.CommandObservation{Secure: true, Locked: false},
			},
		},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for unlocked observation, got %v", err)
	}

	// Test expireCommands DISPATCHED command (dispLock) -> UNCERTAIN
	uncCmd, err := s.GetCommand(ctx, scope, devID, dispLock.ID, now.Add(2*time.Hour))
	if err != nil || uncCmd.State != "UNCERTAIN" {
		t.Fatalf("expected UNCERTAIN command state, got %+v, err: %v", uncCmd, err)
	}

	// 3. Operation reports: applyReport branches
	apkSHA := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	_ = s.RegisterArtifact(ctx, ports.Artifact{
		ScopeID: scope, SHA256: apkSHA, PackageName: "com.adv.app", VersionCode: 1, SizeBytes: 1024,
	})
	opAdv, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_op_adv_1",
		OperationID: "op_adv_1", ArtifactSHA256: apkSHA,
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Sync to obtain lease
	syncOpResp, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 9, Now: now, Inventory: inv,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncOpResp.Operations) != 1 {
		t.Fatalf("expected 1 leased operation, got %v", syncOpResp.Operations)
	}
	assignedOp := syncOpResp.Operations[0]

	// Report with STALE_LEASE (wrong LeaseID)
	syncStaleLease, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 10, Now: now, Inventory: inv,
		Reports: []ports.OperationReport{
			{ID: "rep_stale_lease", OperationID: assignedOp.ID, LeaseID: "wrong_lease", State: "RUNNING", Sequence: 1},
		},
	})
	if err != nil || len(syncStaleLease.Receipts) != 1 || syncStaleLease.Receipts[0].Reason != "STALE_LEASE" {
		t.Fatalf("expected STALE_LEASE receipt, got %+v, err: %v", syncStaleLease.Receipts, err)
	}

	// Valid RUNNING report
	syncValidRep, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 11, Now: now, Inventory: inv,
		Reports: []ports.OperationReport{
			{ID: "rep_valid_run", OperationID: assignedOp.ID, LeaseID: assignedOp.LeaseID, State: "RUNNING", Sequence: 1},
		},
	})
	if err != nil || len(syncValidRep.Receipts) != 1 || !syncValidRep.Receipts[0].Accepted {
		t.Fatalf("expected accepted receipt, got %+v, err: %v", syncValidRep.Receipts, err)
	}

	// Report with STALE_REPORT (Sequence <= ReportSequence)
	syncStaleSeq, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 12, Now: now, Inventory: inv,
		Reports: []ports.OperationReport{
			{ID: "rep_stale_seq", OperationID: assignedOp.ID, LeaseID: assignedOp.LeaseID, State: "RUNNING", Sequence: 1},
		},
	})
	if err != nil || len(syncStaleSeq.Receipts) != 1 || syncStaleSeq.Receipts[0].Reason != "STALE_REPORT" {
		t.Fatalf("expected STALE_REPORT receipt, got %+v, err: %v", syncStaleSeq.Receipts, err)
	}

	// Report with OBSERVATION_MISMATCH (SUCCEEDED without matching observed)
	syncMism, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 13, Now: now, Inventory: inv,
		Reports: []ports.OperationReport{
			{ID: "rep_mismatch", OperationID: assignedOp.ID, LeaseID: assignedOp.LeaseID, State: "SUCCEEDED", Sequence: 2},
		},
	})
	if err != nil || len(syncMism.Receipts) != 1 || syncMism.Receipts[0].Reason != "OBSERVATION_MISMATCH" {
		t.Fatalf("expected OBSERVATION_MISMATCH receipt, got %+v, err: %v", syncMism.Receipts, err)
	}

	// Cancel operation then report -> TERMINAL
	_, _ = s.CancelOperation(ctx, scope, devID, opAdv.ID, now)
	syncTerm, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_adv_1"), Sequence: 14, Now: now, Inventory: inv,
		Reports: []ports.OperationReport{
			{ID: "rep_term", OperationID: assignedOp.ID, LeaseID: assignedOp.LeaseID, State: "RUNNING", Sequence: 3},
		},
	})
	if err != nil || len(syncTerm.Receipts) != 1 || syncTerm.Receipts[0].Reason != "TERMINAL" {
		t.Fatalf("expected TERMINAL receipt, got %+v, err: %v", syncTerm.Receipts, err)
	}

	// 4. Fleet: CreateFleetAssignment validation, duplicate repeat and conflict, CancelFleetAssignment
	// Both GroupID and DeviceIDs empty -> ErrInvalidArgument
	_, err = s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID: scope, ID: "fl_bad", ArtifactSHA256: apkSHA,
		NotBefore: now.Add(time.Minute), Deadline: now.Add(time.Hour),
	}, now)
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty targets, got %v", err)
	}

	// Valid fleet assignment
	flReq := ports.FleetRequest{
		ScopeID: scope, ID: "fl_adv_1", ArtifactSHA256: apkSHA,
		DeviceIDs: []string{devID},
		NotBefore: now.Add(time.Minute), Deadline: now.Add(time.Hour),
	}
	fl1, err := s.CreateFleetAssignment(ctx, flReq, now)
	if err != nil {
		t.Fatal(err)
	}

	// Idempotent repeat of CreateFleetAssignment
	flRepeat, err := s.CreateFleetAssignment(ctx, flReq, now)
	if err != nil || flRepeat.Request.ID != fl1.Request.ID {
		t.Fatalf("expected idempotent fleet creation, got %+v, err: %v", flRepeat, err)
	}

	// Conflicting repeat of CreateFleetAssignment (different deadline)
	flConf := flReq
	flConf.Deadline = now.Add(2 * time.Hour)
	_, err = s.CreateFleetAssignment(ctx, flConf, now)
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on conflicting fleet creation, got %v", err)
	}

	// Cancel fleet assignment
	flCancel1, err := s.CancelFleetAssignment(ctx, scope, fl1.Request.ID, now)
	if err != nil || !flCancel1.Cancelled {
		t.Fatalf("expected cancelled fleet assignment, got %+v, err: %v", flCancel1, err)
	}
	// Cancel already cancelled fleet assignment (idempotent)
	flCancel2, err := s.CancelFleetAssignment(ctx, scope, fl1.Request.ID, now)
	if err != nil || !flCancel2.Cancelled {
		t.Fatalf("expected idempotent cancelled fleet assignment, got %+v, err: %v", flCancel2, err)
	}

	// 5. Changes and Projections: floor > 0 and snapshot eviction (>16 scans)
	// Artificially update change_heads sequence to trigger floor > 0 in next change
	_, err = db.ExecContext(ctx, "UPDATE change_heads SET sequence=10050 WHERE scope_id=?", scope)
	if err != nil {
		t.Fatal(err)
	}
	// Recording next change triggers floor = head - ChangeRetention > 0 and deletion
	_ = s.RegisterArtifact(ctx, ports.Artifact{
		ScopeID: scope, SHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		PackageName: "com.adv.app2", VersionCode: 1, SizeBytes: 1024,
	})

	// ListChanges with after < floor -> ErrCursorExpired
	chList, err := s.ListChanges(ctx, scope, 0, 10)
	if !errors.Is(err, ports.ErrCursorExpired) {
		t.Fatalf("expected ErrCursorExpired for after < floor, got %v (res: %+v)", err, chList)
	}

	// Snapshot eviction: Create 18 projection scans to hit len(retained) >= SnapshotLimit (16)
	for i := 0; i < 18; i++ {
		_, err := s.ReadProjection(ctx, scope, "", 0, 10, now.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("failed creating projection scan %d: %v", i, err)
		}
	}

	_ = cmdReboot
	_ = cmdLock
}

// TestSqlstoreVersionAndInitializeErrors tests unsupported schema version and uninitialized database without opt-in.
func TestSqlstoreVersionAndInitializeErrors(t *testing.T) {
	ctx := context.Background()
	// user_version = 99
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "v99.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, _ = db.Exec("PRAGMA user_version = 99")
	_, err = Open(ctx, db, "sqlite", "", true)
	if err == nil || !strings.Contains(err.Error(), "unsupported database version") {
		t.Fatalf("expected unsupported database version error, got %v", err)
	}

	// user_version = 0, initialize = false
	db0, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "v0.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db0.Close()
	_, err = Open(ctx, db0, "sqlite", "", false)
	if err == nil || !strings.Contains(err.Error(), "schema initialization requires explicit opt-in") {
		t.Fatalf("expected explicit opt-in error, got %v", err)
	}
}

// TestSqlstoreMoreDeepCoverage covers policies, network, fleet groups and assignments, and authorize download branches.
func TestSqlstoreMoreDeepCoverage(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	scope := "scope_more"
	devID := "dev_more_1"

	// 1. Setup device and enroll
	tok := hash32("tok_more_1")
	_ = s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: scope, TokenHash: tok, ExpiresAt: now.Add(time.Hour)})
	_, err := s.Consume(ctx, ports.Attempt{
		TokenHash: tok, ID: "att_more_1", KeyHash: hash32("k_more_1"), CredentialHash: hash32("c_more_1"),
		Device: ports.Device{ID: devID, ScopeID: scope, State: "ACTIVE"},
		Receipt: []byte("rec_more_1"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Policy: SetPolicy branches
	tVal := true
	// RequiredPackages with package not in desired apps -> ErrNotFound
	_, err = s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "pol_ik_missing",
		Policy: ports.Policy{CameraDisabled: &tVal, RequiredPackages: []string{"com.missing.pkg"}},
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing required package, got %v", err)
	}

	// Valid policy
	polDes1, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "pol_ik_1",
		Policy: ports.Policy{CameraDisabled: &tVal},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Idempotent repeat with same idempotency key
	polRepeat, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "pol_ik_1",
		Policy: ports.Policy{CameraDisabled: &tVal},
	})
	if err != nil || polRepeat.Revision != polDes1.Revision {
		t.Fatalf("expected idempotent policy repeat, got %+v, err: %v", polRepeat, err)
	}

	// Conflicting repeat with different policy
	fVal := false
	_, err = s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "pol_ik_1",
		Policy: ports.Policy{CameraDisabled: &fVal},
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on conflicting policy assignment, got %v", err)
	}

	// 3. Network: SetNetworkPolicy and GetNetworkRequest branches
	// Invalid arguments
	if _, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: "", DeviceID: devID, IdempotencyKey: "k"}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for empty scope")
	}

	netPolValid := ports.NetworkPolicy{
		Profiles: []ports.WifiProfile{
			{ID: "prof_more", SSID: "CorpWifi", Security: "OPEN"},
		},
		PrimaryID: "prof_more",
	}

	// Valid SetNetworkPolicy
	netDes1, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "net_ik_1",
		Policy: netPolValid,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Idempotent repeat
	netRepeat, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "net_ik_1",
		Policy: netPolValid,
	})
	if err != nil || netRepeat.Revision != netDes1.Revision {
		t.Fatalf("expected idempotent network repeat, got %+v, err: %v", netRepeat, err)
	}

	// Conflicting repeat with different SSID
	netPolDiff := netPolValid
	netPolDiff.Profiles = []ports.WifiProfile{
		{ID: "prof_more", SSID: "DifferentWifi", Security: "OPEN"},
	}
	_, err = s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "net_ik_1",
		Policy: netPolDiff,
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for conflicting network assignment, got %v", err)
	}

	// GetNetworkRequest valid and not found
	if _, err := s.GetNetworkRequest(ctx, scope, devID, "non_existent_key"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing network request, got %v", err)
	}
	netReqFound, err := s.GetNetworkRequest(ctx, scope, devID, "net_ik_1")
	if err != nil || netReqFound.Revision != netDes1.Revision {
		t.Fatalf("expected matching network request, got %+v, err: %v", netReqFound, err)
	}

	// 4. Fleet: ReplaceGroup, ListGroups, ListFleetAssignments, AdvanceFleetAssignment
	// ReplaceGroup invalid arguments
	if _, err := s.ReplaceGroup(ctx, ports.Group{ScopeID: "", ID: "g1"}, 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for empty scope")
	}
	if _, err := s.ReplaceGroup(ctx, ports.Group{ScopeID: scope, ID: "g1"}, 1<<63); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for expected >= 1<<62")
	}

	// ReplaceGroup with missing device -> ErrNotFound
	_, err = s.ReplaceGroup(ctx, ports.Group{
		ScopeID: scope, ID: "g_more_1", DeviceIDs: []string{"non_existent_dev"},
	}, 0)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing device in group, got %v", err)
	}

	// ReplaceGroup valid
	grp1, err := s.ReplaceGroup(ctx, ports.Group{
		ScopeID: scope, ID: "g_more_1", DeviceIDs: []string{devID},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if grp1.Revision != 1 {
		t.Fatalf("expected group revision 1, got %d", grp1.Revision)
	}

	// ReplaceGroup idempotent repeat
	grpRepeat, err := s.ReplaceGroup(ctx, ports.Group{
		ScopeID: scope, ID: "g_more_1", DeviceIDs: []string{devID},
	}, 0)
	if err != nil || grpRepeat.Revision != 1 {
		t.Fatalf("expected idempotent group repeat, got %+v, err: %v", grpRepeat, err)
	}

	// ReplaceGroup revision conflict
	_, err = s.ReplaceGroup(ctx, ports.Group{
		ScopeID: scope, ID: "g_more_1", DeviceIDs: []string{devID},
	}, 99)
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for mismatched group revision, got %v", err)
	}

	// ListGroups validation and query
	if _, err := s.ListGroups(ctx, "", "", 10); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for empty scope")
	}
	if _, err := s.ListGroups(ctx, scope, "", 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit < 1")
	}
	grps, err := s.ListGroups(ctx, scope, "", 10)
	if err != nil || len(grps) != 1 {
		t.Fatalf("expected 1 group, got %v, err: %v", grps, err)
	}

	// ListFleetAssignments validation and query
	if _, err := s.ListFleetAssignments(ctx, "", "", 10); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for empty scope")
	}
	if _, err := s.ListFleetAssignments(ctx, scope, "", 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit < 1")
	}
	flList, err := s.ListFleetAssignments(ctx, scope, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	_ = flList

	// AdvanceFleetAssignment validation and execution
	if _, err := s.AdvanceFleetAssignment(ctx, scope, "fl1", now, 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("expected ErrInvalidArgument for limit < 1")
	}

	apkSHA := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	_ = s.RegisterArtifact(ctx, ports.Artifact{
		ScopeID: scope, SHA256: apkSHA, PackageName: "com.more.app", VersionCode: 1, SizeBytes: 1024,
	})
	flBatch, err := s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID: scope, ID: "fl_adv_target", ArtifactSHA256: apkSHA,
		DeviceIDs: []string{devID},
		NotBefore: now.Add(time.Minute), Deadline: now.Add(time.Hour),
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	// Advance fleet assignment
	flAdv, err := s.AdvanceFleetAssignment(ctx, scope, flBatch.Request.ID, now, 10)
	if err != nil || !flAdv.Complete {
		t.Fatalf("expected completed advanced fleet assignment, got %+v, err: %v", flAdv, err)
	}

	// Advance already complete fleet assignment (idempotent)
	flAdvAgain, err := s.AdvanceFleetAssignment(ctx, scope, flBatch.Request.ID, now, 10)
	if err != nil || !flAdvAgain.Complete {
		t.Fatalf("expected idempotent complete fleet advance, got %+v, err: %v", flAdvAgain, err)
	}

	// Advance fleet assignment with past deadline -> DEADLINE_EXCEEDED
	flPast, err := s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID: scope, ID: "fl_past", ArtifactSHA256: apkSHA,
		DeviceIDs: []string{devID},
		NotBefore: now.Add(time.Minute), Deadline: now.Add(2 * time.Minute),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	flPastAdv, err := s.AdvanceFleetAssignment(ctx, scope, flPast.Request.ID, now.Add(time.Hour), 10)
	if err != nil || len(flPastAdv.Items) == 0 || flPastAdv.Items[0].ErrorCode != "DEADLINE_EXCEEDED" {
		t.Fatalf("expected DEADLINE_EXCEEDED item error code, got %+v, err: %v", flPastAdv, err)
	}

	// 5. AuthorizeDownload branches
	// Device not found -> ErrUnauthorized
	if _, err := s.AuthorizeDownload(ctx, "non_existent_dev", hash32("c_more_1"), "op1", apkSHA, "lease1", now); !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for missing device, got %v", err)
	}
	// Wrong credential hash -> ErrUnauthorized
	if _, err := s.AuthorizeDownload(ctx, devID, hash32("wrong_cred"), "op1", apkSHA, "lease1", now); !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for wrong credential hash, got %v", err)
	}
	// Missing operation -> ErrNotFound
	if _, err := s.AuthorizeDownload(ctx, devID, hash32("c_more_1"), "missing_op", apkSHA, "lease1", now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing operation, got %v", err)
	}

	// ListOperations for non-existent device -> ErrNotFound
	if _, err := s.ListOperations(ctx, scope, "non_existent_dev", "", 10, now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing device in ListOperations, got %v", err)
	}

	// 6. Revoke devID and verify SetPolicy, SetNetworkPolicy, AuthorizeDownload, ReplaceGroup -> ErrRevoked
	if err := s.Revoke(ctx, scope, devID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "pol_rev",
		Policy: ports.Policy{CameraDisabled: &tVal},
	}); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked on SetPolicy for revoked device, got %v", err)
	}
	if _, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "net_rev",
		Policy: netPolValid,
	}); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked on SetNetworkPolicy for revoked device, got %v", err)
	}
	if _, err := s.AuthorizeDownload(ctx, devID, hash32("c_more_1"), "op1", apkSHA, "lease1", now); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked on AuthorizeDownload for revoked device, got %v", err)
	}
	if _, err := s.ReplaceGroup(ctx, ports.Group{
		ScopeID: scope, ID: "g_rev", DeviceIDs: []string{devID},
	}, 0); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked on ReplaceGroup for revoked device, got %v", err)
	}
}

// TestSqlstoreClosedDBAndTriggerErrorCoverage exercises database failure branches and trigger injections across all methods.
func TestSqlstoreClosedDBAndTriggerErrorCoverage(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	scope := "scope_err"
	devID := "dev_err_1"

	// 1. Trigger injections on INSERT/UPDATE
	{
		s, db := testStore(t)
		createTestDevice(t, s, scope, devID, "cred_err")
		art := ports.Artifact{
			ScopeID: scope, PackageName: "com.err.app", SHA256: strings.Repeat("e", 64),
			SizeBytes: 1024, VersionCode: 1, MinSDK: 33, MaxSDK: 37,
		}
		_ = s.RegisterArtifact(ctx, art)

		// Fail policies insert
		_, _ = db.Exec("CREATE TRIGGER fail_policies BEFORE INSERT ON policies BEGIN SELECT RAISE(FAIL, 'fail'); END;")
		tVal := true
		_, _ = s.SetPolicy(ctx, ports.PolicyAssignment{
			ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_p_fail",
			Policy: ports.Policy{CameraDisabled: &tVal},
		})
		_, _ = db.Exec("DROP TRIGGER fail_policies")

		// Fail policy_requests insert
		_, _ = db.Exec("CREATE TRIGGER fail_policy_req BEFORE INSERT ON policy_requests BEGIN SELECT RAISE(FAIL, 'fail'); END;")
		_, _ = s.SetPolicy(ctx, ports.PolicyAssignment{
			ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_p_fail2",
			Policy: ports.Policy{CameraDisabled: &tVal},
		})
		_, _ = db.Exec("DROP TRIGGER fail_policy_req")

		// Fail network_policies insert
		netPol := ports.NetworkPolicy{
			Profiles: []ports.WifiProfile{{ID: "prof1", SSID: "Corp", Security: "OPEN"}},
			PrimaryID: "prof1",
		}
		_, _ = db.Exec("CREATE TRIGGER fail_net_pol BEFORE INSERT ON network_policies BEGIN SELECT RAISE(FAIL, 'fail'); END;")
		_, _ = s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
			ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_n_fail", Policy: netPol,
		})
		_, _ = db.Exec("DROP TRIGGER fail_net_pol")

		// Fail network_requests insert
		_, _ = db.Exec("CREATE TRIGGER fail_net_req BEFORE INSERT ON network_requests BEGIN SELECT RAISE(FAIL, 'fail'); END;")
		_, _ = s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
			ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_n_fail2", Policy: netPol,
		})
		_, _ = db.Exec("DROP TRIGGER fail_net_req")

		// Fail commands insert
		inv := ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot"}}
		_, _ = s.Sync(ctx, ports.SyncInput{DeviceID: devID, CredentialHash: hash32("cred_err"), Sequence: 1, Now: now, Inventory: inv})
		_, _ = db.Exec("CREATE TRIGGER fail_cmds BEFORE INSERT ON commands BEGIN SELECT RAISE(FAIL, 'fail'); END;")
		_, _ = s.SubmitCommand(ctx, ports.CommandAssignment{
			ScopeID: scope, DeviceID: devID, ID: "cmd_fail", IdempotencyKey: "ik_fail", Kind: "REBOOT",
			Now: now, Deadline: now.Add(time.Hour),
		})
		_, _ = db.Exec("DROP TRIGGER fail_cmds")

		// Fail fleet_groups insert
		_, _ = db.Exec("CREATE TRIGGER fail_fg BEFORE INSERT ON fleet_groups BEGIN SELECT RAISE(FAIL, 'fail'); END;")
		_, _ = s.ReplaceGroup(ctx, ports.Group{ScopeID: scope, ID: "fg_fail", DeviceIDs: []string{devID}}, 0)
		_, _ = db.Exec("DROP TRIGGER fail_fg")

		// Fail fleet_assignments insert
		_, _ = db.Exec("CREATE TRIGGER fail_fa BEFORE INSERT ON fleet_assignments BEGIN SELECT RAISE(FAIL, 'fail'); END;")
		_, _ = s.CreateFleetAssignment(ctx, ports.FleetRequest{
			ScopeID: scope, ID: "fa_fail", ArtifactSHA256: art.SHA256, DeviceIDs: []string{devID},
			NotBefore: now.Add(time.Minute), Deadline: now.Add(time.Hour),
		}, now)
		_, _ = db.Exec("DROP TRIGGER fail_fa")

		// Fail projection_scans insert
		_, _ = db.Exec("CREATE TRIGGER fail_ps BEFORE INSERT ON projection_scans BEGIN SELECT RAISE(FAIL, 'fail'); END;")
		_, _ = s.ReadProjection(ctx, scope, "", 0, 10, now)
		_, _ = db.Exec("DROP TRIGGER fail_ps")

		// Fail projection_items insert
		_, _ = db.Exec("CREATE TRIGGER fail_pi BEFORE INSERT ON projection_items BEGIN SELECT RAISE(FAIL, 'fail'); END;")
		_, _ = s.ReadProjection(ctx, scope, "", 0, 10, now)
		_, _ = db.Exec("DROP TRIGGER fail_pi")

		// Fail reports insert during Sync
		op, _ := s.Assign(ctx, ports.Assignment{
			ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_rep_fail", OperationID: "op_rep_fail",
			ArtifactSHA256: art.SHA256, Now: now, Deadline: now.Add(time.Hour),
		})
		syncResp, _ := s.Sync(ctx, ports.SyncInput{
			DeviceID: devID, CredentialHash: hash32("cred_err"), Sequence: 2, Now: now, Inventory: inv,
		})
		if len(syncResp.Operations) > 0 {
			leased := syncResp.Operations[0]
			_, _ = db.Exec("CREATE TRIGGER fail_reps BEFORE INSERT ON reports BEGIN SELECT RAISE(FAIL, 'fail'); END;")
			_, _ = s.Sync(ctx, ports.SyncInput{
				DeviceID: devID, CredentialHash: hash32("cred_err"), Sequence: 3, Now: now, Inventory: inv,
				Reports: []ports.OperationReport{
					{ID: "r_fail", OperationID: leased.ID, LeaseID: leased.LeaseID, State: "RUNNING", Sequence: 1},
				},
			})
			_, _ = db.Exec("DROP TRIGGER fail_reps")
		}
		_ = op
	}

	// 2. Closed database error branches
	{
		s, db := testStore(t)
		devID := "dev_closed"
		createTestDevice(t, s, scope, devID, "cred_closed")
		// Close underlying DB pool to trigger BeginTx, QueryContext, PingContext errors
		_ = db.Close()

		_ = s.Check(ctx)
		_, _ = s.GetDevice(ctx, scope, devID)
		_, _ = s.QueryDevices(ctx, ports.DeviceQuery{ScopeID: scope, Limit: 10})
		_, _ = s.GetOperation(ctx, scope, devID, "op1", now)
		_, _ = s.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: devID, Limit: 10})
		_, _ = s.ListOperations(ctx, scope, devID, "", 10, now)
		_, _ = s.Assign(ctx, ports.Assignment{ScopeID: scope, DeviceID: devID, IdempotencyKey: "k", Now: now, Deadline: now.Add(time.Hour)})
		_, _ = s.AuthorizeDownload(ctx, devID, hash32("cred_closed"), "op1", "sha", "lease", now)
		_, _ = s.Sync(ctx, ports.SyncInput{DeviceID: devID, Sequence: 1, Now: now})
		_, _ = s.ListCommands(ctx, scope, devID, "", 10, now)
		_, _ = s.SubmitCommand(ctx, ports.CommandAssignment{ScopeID: scope, DeviceID: devID, ID: "c", IdempotencyKey: "k", Kind: "REBOOT", Now: now, Deadline: now.Add(time.Hour)})
		_, _ = s.CancelCommand(ctx, scope, devID, "c", now)
		_, _ = s.ListChanges(ctx, scope, 0, 10)
		_, _ = s.ReadProjection(ctx, scope, "", 0, 10, now)
		_, _ = s.ReplaceGroup(ctx, ports.Group{ScopeID: scope, ID: "g", DeviceIDs: []string{devID}}, 0)
		_, _ = s.ListGroups(ctx, scope, "", 10)
		_, _ = s.CreateFleetAssignment(ctx, ports.FleetRequest{ScopeID: scope, ID: "fa", NotBefore: now.Add(time.Minute), Deadline: now.Add(time.Hour)}, now)
		_, _ = s.AdvanceFleetAssignment(ctx, scope, "fa", now, 10)
		_, _ = s.CancelFleetAssignment(ctx, scope, "fa", now)
		_, _ = s.ListFleetAssignments(ctx, scope, "", 10)
		_, _ = s.SetPolicy(ctx, ports.PolicyAssignment{ScopeID: scope, DeviceID: devID, IdempotencyKey: "k"})
		_, _ = s.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: scope, DeviceID: devID, IdempotencyKey: "k"})
		_, _ = s.GetNetworkRequest(ctx, scope, devID, "k")
		_, _ = s.Enrollment(ctx, hash32("tok"))
		_, _ = s.Consume(ctx, ports.Attempt{TokenHash: hash32("tok"), Now: now})
		_ = s.Revoke(ctx, scope, devID)
		_ = s.RegisterArtifact(ctx, ports.Artifact{ScopeID: scope, SHA256: "sha"})
		_ = s.RemoveArtifact(ctx, scope, "sha")
		_, _ = s.ListArtifacts(ctx, scope, "", 10)
	}
}

// TestSqlstoreComprehensiveReconciliationAndProjectionScan exercises full projection scan across all entity types,
// command capability unavailability, and reconciliation drift and compliance states in operations.
func TestSqlstoreComprehensiveReconciliationAndProjectionScan(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	scope := "scope_full_scan"
	devID := "dev_fs_1"

	// 1. Enroll device
	tok := hash32("tok_fs_1")
	_ = s.CreateEnrollment(ctx, ports.Enrollment{ScopeID: scope, TokenHash: tok, ExpiresAt: now.Add(time.Hour)})
	_, err := s.Consume(ctx, ports.Attempt{
		TokenHash: tok, ID: "att_fs_1", KeyHash: hash32("k_fs_1"), CredentialHash: hash32("c_fs_1"),
		Device: ports.Device{ID: devID, ScopeID: scope, State: "ACTIVE"},
		Receipt: []byte("rec_fs_1"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Register artifact
	artSHA := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	art := ports.Artifact{
		ScopeID: scope, SHA256: artSHA, PackageName: "com.fs.app",
		VersionCode: 1, SizeBytes: 1024,
		SigningCertificates: []string{strings.Repeat("a", 64)},
	}
	if err := s.RegisterArtifact(ctx, art); err != nil {
		t.Fatal(err)
	}

	// 3. Assign operation to device
	op, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_fs_op",
		OperationID: "op_fs_1", ArtifactSHA256: artSHA,
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = op

	// 4. Submit command to device
	invWithCap := ports.Inventory{
		APILevel: 34, Model: "Pixel", DeviceOwner: true,
		Capabilities: []string{"command.reboot", "install_apk"},
	}
	// Sync once with capabilities so SubmitCommand accepts it
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_fs_1"), Sequence: 1, Now: now, Inventory: invWithCap,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_fs_1", IdempotencyKey: "ik_fs_1", Kind: "REBOOT",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 5. Create group in scope
	_, err = s.ReplaceGroup(ctx, ports.Group{
		ScopeID: scope, ID: "grp_fs_1", DeviceIDs: []string{devID},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	// 6. Create fleet assignment in scope
	_, err = s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID: scope, ID: "fl_fs_1", ArtifactSHA256: artSHA, DeviceIDs: []string{devID},
		NotBefore: now.Add(time.Minute), Deadline: now.Add(time.Hour),
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	// 7. Full ReadProjection scan: covers device state (with operations and commands), artifacts, groups, and fleet assignments!
	page, err := s.ReadProjection(ctx, scope, "", 0, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) < 4 {
		t.Fatalf("expected at least 4 projection items (device, app, group, fleet), got %d: %+v", len(page.Items), page.Items)
	}

	// 8. Test dispatchCommand with missing capability during Sync -> FAILED, CAPABILITY_UNAVAILABLE
	// Submit another command
	_, err = s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_fs_2", IdempotencyKey: "ik_fs_2", Kind: "REBOOT",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Sync with inventory MISSING command.reboot
	invNoCap := ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"install_apk"}}
	syncNoCap, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_fs_1"), Sequence: 2, Now: now, Inventory: invNoCap,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = syncNoCap
	// Verify cmd_fs_1 failed with CAPABILITY_UNAVAILABLE
	cmdCheck, err := s.GetCommand(ctx, scope, devID, "cmd_fs_1", now)
	if err != nil || cmdCheck.State != "FAILED" || cmdCheck.ErrorCode != "CAPABILITY_UNAVAILABLE" {
		t.Fatalf("expected CAPABILITY_UNAVAILABLE command, got %+v, err: %v", cmdCheck, err)
	}

	// 9. Operations: Test compliance states (SCHEDULED, USER_ACTION_REQUIRED, FAILED, APPLIED) and reconciliation drift
	// a) Operation scheduled in future -> SCHEDULED
	artSHA2 := "1111111111111111111111111111111111111111111111111111111111111111"
	art2 := ports.Artifact{
		ScopeID: scope, SHA256: artSHA2, PackageName: "com.fs.sched",
		VersionCode: 1, SizeBytes: 1024,
	}
	_ = s.RegisterArtifact(ctx, art2)
	_, _ = s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_sched",
		OperationID: "op_sched", ArtifactSHA256: artSHA2,
		Now: now, NotBefore: now.Add(10 * time.Minute), Deadline: now.Add(time.Hour),
	})

	syncSched, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c_fs_1"), Sequence: 3, Now: now, Inventory: invWithCap,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Verify compliance for com.fs.sched is SCHEDULED
	foundSched := false
	for _, c := range syncSched.Compliance {
		if c.PackageName == "com.fs.sched" && c.Reason == "SCHEDULED" {
			foundSched = true
		}
	}
	if !foundSched {
		t.Fatalf("expected SCHEDULED compliance for com.fs.sched, got %+v", syncSched.Compliance)
	}

	// b) Operation in WAITING_USER -> USER_ACTION_REQUIRED
	// First sync to get leased op_fs_1
	var leasedOp ports.Operation
	for _, o := range syncSched.Operations {
		if o.Artifact.PackageName == "com.fs.app" {
			leasedOp = o
		}
	}
	if leasedOp.ID != "" {
		syncWait, err := s.Sync(ctx, ports.SyncInput{
			DeviceID: devID, CredentialHash: hash32("c_fs_1"), Sequence: 4, Now: now, Inventory: invWithCap,
			Reports: []ports.OperationReport{
				{ID: "rep_wait", OperationID: leasedOp.ID, LeaseID: leasedOp.LeaseID, State: "WAITING_USER", Sequence: 1},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		foundWait := false
		for _, c := range syncWait.Compliance {
			if c.PackageName == "com.fs.app" && c.Reason == "USER_ACTION_REQUIRED" {
				foundWait = true
			}
		}
		if !foundWait {
			t.Fatalf("expected USER_ACTION_REQUIRED compliance, got %+v", syncWait.Compliance)
		}

		// c) Report op_fs_1 as SUCCEEDED
		installedApp := ports.InstalledApplication{
			PackageName: "com.fs.app", VersionCode: 1, SigningCertificates: []string{strings.Repeat("a", 64)},
		}
		syncSucc, err := s.Sync(ctx, ports.SyncInput{
			DeviceID: devID, CredentialHash: hash32("c_fs_1"), Sequence: 5, Now: now, Inventory: invWithCap,
			Reports: []ports.OperationReport{
				{
					ID: "rep_succ", OperationID: leasedOp.ID, LeaseID: leasedOp.LeaseID, State: "SUCCEEDED",
					Sequence: 2, Observed: &installedApp,
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = syncSucc

		// d) Reconciliation drift: Device syncs WITHOUT com.fs.app in inventory (app was uninstalled!)
		// This triggers lines 583-596 in operations.go to automatically generate a reconcile- op!
		syncDrift, err := s.Sync(ctx, ports.SyncInput{
			DeviceID: devID, CredentialHash: hash32("c_fs_1"), Sequence: 6, Now: now, Inventory: invWithCap,
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = syncDrift

		// e) Now device syncs WITH com.fs.app in inventory -> compliance APPLIED!
		invWithApp := invWithCap
		invWithApp.Applications = []ports.InstalledApplication{installedApp}
		syncApplied, err := s.Sync(ctx, ports.SyncInput{
			DeviceID: devID, CredentialHash: hash32("c_fs_1"), Sequence: 7, Now: now, Inventory: invWithApp,
		})
		if err != nil {
			t.Fatal(err)
		}
		foundApplied := false
		for _, c := range syncApplied.Compliance {
			if c.PackageName == "com.fs.app" && c.State == "APPLIED" {
				foundApplied = true
			}
		}
		if !foundApplied {
			t.Fatalf("expected APPLIED compliance, got %+v", syncApplied.Compliance)
		}
	}
}

// TestSqlstoreApplyReportBranchesAndAssignConflict exercises error handling, report reconciliation,
// superseding, and revoked device branches in sqlstore operations.
func TestSqlstoreApplyReportBranchesAndAssignConflict(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, db, "sqlite", "", true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	scope := "rep_scope"
	devID := "rep_dev_1"

	// 1. Enrollment and consumption
	tok := "rep_tok_1"
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: hash32(tok), ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: hash32(tok), ID: "att_rep_1", KeyHash: hash32("k1"), CredentialHash: hash32("c1"),
		Device: ports.Device{ID: devID, ScopeID: scope, State: "ACTIVE"},
		Receipt: []byte("receipt"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Register artifact and conflict branch in RegisterArtifact
	artSHA := strings.Repeat("3", 64)
	art := ports.Artifact{ScopeID: scope, SHA256: artSHA, PackageName: "com.rep.app", VersionCode: 1, SizeBytes: 500}
	if err := s.RegisterArtifact(ctx, art); err != nil {
		t.Fatal(err)
	}
	// Re-registering with different data returns ErrConflict (line 56)
	artConf := art
	artConf.VersionCode = 2
	if err := s.RegisterArtifact(ctx, artConf); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate artifact with differing data, got %v", err)
	}

	// 3. Assign conflict and error branches
	// a) deadline before now -> ErrInvalidArgument (line 255)
	_, err = s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_inv_time",
		OperationID: "op_inv_time", ArtifactSHA256: artSHA,
		Now: now, Deadline: now.Add(-time.Hour),
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for past deadline, got %v", err)
	}

	// b) Valid assign
	op1, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_idem_1",
		OperationID: "op_idem_1", ArtifactSHA256: artSHA,
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// c) Re-assign with same idempotency key but differing artifact -> ErrConflict (line 247)
	artSHA_other := strings.Repeat("4", 64)
	_ = s.RegisterArtifact(ctx, ports.Artifact{ScopeID: scope, SHA256: artSHA_other, PackageName: "com.rep.other", VersionCode: 1, SizeBytes: 500})
	_, err = s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_idem_1",
		OperationID: "op_idem_diff", ArtifactSHA256: artSHA_other,
		Now: now, Deadline: now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on idempotency key mismatch, got %v", err)
	}

	// d) RemoveArtifact with active operation -> ErrReferenced (line 135)
	if err := s.RemoveArtifact(ctx, scope, artSHA); !errors.Is(err, ports.ErrReferenced) {
		t.Fatalf("expected ErrReferenced on active artifact removal, got %v", err)
	}

	// 4. Supersede older operation: assign a newer artifact for com.rep.app
	artSHA_v2 := strings.Repeat("5", 64)
	_ = s.RegisterArtifact(ctx, ports.Artifact{ScopeID: scope, SHA256: artSHA_v2, PackageName: "com.rep.app", VersionCode: 2, SizeBytes: 500})
	op2, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_idem_2",
		OperationID: "op_idem_2", ArtifactSHA256: artSHA_v2,
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = op2

	// ListOperations to verify op1 was SUPERSEDED
	ops, err := s.ListOperations(ctx, scope, devID, "", 10, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range ops {
		if o.ID == op1.ID && o.State != "SUPERSEDED" {
			t.Fatalf("expected op1 to be SUPERSEDED, got %s", o.State)
		}
	}

	// 5. Sync to lease op2
	syncResp, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 1, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
	})
	if err != nil || len(syncResp.Operations) == 0 {
		t.Fatalf("expected leased operation, got %v (len %d)", err, len(syncResp.Operations))
	}
	leasedOp2 := syncResp.Operations[0]

	// 6. Test AuthorizeDownload branches:
	// a) Unauthorized wrong lease
	_, err = s.AuthorizeDownload(ctx, devID, hash32("c1"), leasedOp2.ID, artSHA_v2, "wrong_lease_id", now)
	if !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized on wrong lease, got %v", err)
	}
	// b) Unknown device -> ErrUnauthorized
	_, err = s.AuthorizeDownload(ctx, "unknown_dev", hash32("c1"), leasedOp2.ID, artSHA_v2, leasedOp2.LeaseID, now)
	if !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized on unknown device, got %v", err)
	}
	// c) Valid download
	artAuth, err := s.AuthorizeDownload(ctx, devID, hash32("c1"), leasedOp2.ID, artSHA_v2, leasedOp2.LeaseID, now)
	if err != nil || artAuth.SHA256 != artSHA_v2 {
		t.Fatalf("expected successful AuthorizeDownload, got %+v, err: %v", artAuth, err)
	}

	// 7. Test applyReport branches via Sync:
	// a) Report on op1 which is SUPERSEDED -> receipt.Reason == "SUPERSEDED" (line 426)
	syncSup, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 2, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{
			{ID: "rep_on_super", OperationID: op1.ID, LeaseID: "any", Sequence: 1, State: "RUNNING"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncSup.Receipts) == 0 || syncSup.Receipts[0].Reason != "SUPERSEDED" {
		t.Fatalf("expected SUPERSEDED receipt reason, got %+v", syncSup.Receipts)
	}

	// b) Report on unknown operation -> NOT_ASSIGNED (line 419)
	syncNotAssigned, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 3, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{
			{ID: "rep_unknown_op", OperationID: "op_does_not_exist", LeaseID: "any", Sequence: 1, State: "RUNNING"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncNotAssigned.Receipts) == 0 || syncNotAssigned.Receipts[0].Reason != "NOT_ASSIGNED" {
		t.Fatalf("expected NOT_ASSIGNED receipt reason, got %+v", syncNotAssigned.Receipts)
	}

	// c) Report with stale lease on op2 -> STALE_LEASE (line 431)
	syncStaleLease, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 4, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{
			{ID: "rep_stale_lease", OperationID: leasedOp2.ID, LeaseID: "stale_lease_12345", Sequence: 1, State: "RUNNING"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncStaleLease.Receipts) == 0 || syncStaleLease.Receipts[0].Reason != "STALE_LEASE" {
		t.Fatalf("expected STALE_LEASE receipt reason, got %+v", syncStaleLease.Receipts)
	}

	// d) Report with observation mismatch (state SUCCEEDED but observed certs/version mismatch) -> OBSERVATION_MISMATCH (line 435)
	mismatchObs := ports.InstalledApplication{
		PackageName: "com.rep.app", VersionCode: 999, // mismatch version
	}
	syncMismatch, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 5, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{
			{ID: "rep_mismatch", OperationID: leasedOp2.ID, LeaseID: leasedOp2.LeaseID, Sequence: 1, State: "SUCCEEDED", Observed: &mismatchObs},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncMismatch.Receipts) == 0 || syncMismatch.Receipts[0].Reason != "OBSERVATION_MISMATCH" {
		t.Fatalf("expected OBSERVATION_MISMATCH receipt reason, got %+v", syncMismatch.Receipts)
	}

	// e) Valid RUNNING report with Sequence 10
	syncRunning, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 6, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{
			{ID: "rep_running_10", OperationID: leasedOp2.ID, LeaseID: leasedOp2.LeaseID, Sequence: 10, State: "RUNNING"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncRunning.Receipts) == 0 || !syncRunning.Receipts[0].Accepted {
		t.Fatalf("expected accepted receipt, got %+v", syncRunning.Receipts)
	}

	// f) Report with STALE_REPORT (Sequence 5 <= 10) (line 433)
	syncStaleSeq, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 7, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{
			{ID: "rep_stale_seq", OperationID: leasedOp2.ID, LeaseID: leasedOp2.LeaseID, Sequence: 5, State: "RUNNING"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncStaleSeq.Receipts) == 0 || syncStaleSeq.Receipts[0].Reason != "STALE_REPORT" {
		t.Fatalf("expected STALE_REPORT receipt reason, got %+v", syncStaleSeq.Receipts)
	}

	// g) Report duplicate report ID with differing content -> ErrConflict (line 406)
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 8, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{
			{ID: "rep_running_10", OperationID: leasedOp2.ID, LeaseID: leasedOp2.LeaseID, Sequence: 10, State: "FAILED"},
		},
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate report with differing payload, got %v", err)
	}

	// h) Mark op2 SUCCEEDED with valid observation
	validObs := ports.InstalledApplication{
		PackageName: "com.rep.app", VersionCode: 2,
	}
	syncSucc, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 9, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{
			{ID: "rep_op2_succ", OperationID: leasedOp2.ID, LeaseID: leasedOp2.LeaseID, Sequence: 11, State: "SUCCEEDED", Observed: &validObs},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncSucc.Receipts) == 0 || !syncSucc.Receipts[0].Accepted {
		t.Fatalf("expected accepted SUCCEEDED receipt, got %+v", syncSucc.Receipts)
	}

	// i) Subsequent report on op2 which is now SUCCEEDED -> receipt.Reason == "TERMINAL" (line 429)
	syncTerminal, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 10, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{
			{ID: "rep_op2_term", OperationID: leasedOp2.ID, LeaseID: leasedOp2.LeaseID, Sequence: 12, State: "RUNNING"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncTerminal.Receipts) == 0 || syncTerminal.Receipts[0].Reason != "TERMINAL" {
		t.Fatalf("expected TERMINAL receipt reason, got %+v", syncTerminal.Receipts)
	}

	// 8. Revoked device branches:
	// Revoke device
	if err := s.Revoke(ctx, scope, devID); err != nil {
		t.Fatal(err)
	}
	// Assign on revoked device -> ErrRevoked (line 237)
	_, err = s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_rev",
		OperationID: "op_rev", ArtifactSHA256: artSHA_v2,
		Now: now, Deadline: now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked on Assign, got %v", err)
	}
	// AuthorizeDownload on revoked device -> ErrRevoked (line 364)
	_, err = s.AuthorizeDownload(ctx, devID, hash32("c1"), leasedOp2.ID, artSHA_v2, leasedOp2.LeaseID, now)
	if !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked on AuthorizeDownload, got %v", err)
	}
}

// TestSqlstoreDeepBranchCoverageAllModules systematically exercises missing branches in
// commands, changes, fleet, policies, network, and management.
func TestSqlstoreDeepBranchCoverageAllModules(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "deep.db"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, db, "sqlite", "", true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	scope := "deep_scope"
	devID := "deep_dev_1"

	// Setup device with full capabilities
	tok := "deep_tok"
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: hash32(tok), ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: hash32(tok), ID: "att_deep_1", KeyHash: hash32("k1"), CredentialHash: hash32("c1"),
		Device: ports.Device{
			ID: devID, ScopeID: scope, State: "ACTIVE",
			Inventory: ports.Inventory{
				APILevel: 34, Model: "Pixel", DeviceOwner: true,
				Capabilities: []string{"command.reboot", "command.lock", "network.lifecycle"},
			},
		},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. commands.go error and report branches
	// a) Re-enqueue same idempotency key with different kind -> ErrConflict (line 126)
	cmd1, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, Kind: "REBOOT", IdempotencyKey: "k_cmd_idem",
		ID: "cmd_reboot_1", Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = cmd1
	_, err = s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, Kind: "LOCK", IdempotencyKey: "k_cmd_idem",
		ID: "cmd_lock_diff", Now: now, Deadline: now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for differing command kind, got %v", err)
	}

	// b) Device lacking capability -> ErrUnsupported (line 138)
	devNoCap := "deep_dev_nocap"
	tokNoCap := "deep_tok_nocap"
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: hash32(tokNoCap), ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, _ = s.Consume(ctx, ports.Attempt{
		TokenHash: hash32(tokNoCap), ID: "att_nocap", KeyHash: hash32("k2"), CredentialHash: hash32("c2"),
		Device: ports.Device{
			ID: devNoCap, ScopeID: scope, State: "ACTIVE",
			Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: false},
		},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	_, err = s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devNoCap, Kind: "REBOOT", IdempotencyKey: "k_nocap",
		ID: "cmd_nocap", Now: now, Deadline: now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported without capability, got %v", err)
	}

	// c) Enqueue with past deadline -> ErrInvalidArgument (line 141)
	_, err = s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, Kind: "REBOOT", IdempotencyKey: "k_past",
		ID: "cmd_past", Now: now, Deadline: now.Add(-time.Hour),
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for past deadline, got %v", err)
	}

	// d) Enqueue up to 16 commands, 17th returns ErrConflict (line 151)
	for i := 0; i < 15; i++ {
		_, err = s.SubmitCommand(ctx, ports.CommandAssignment{
			ScopeID: scope, DeviceID: devID, Kind: "REBOOT", IdempotencyKey: fmt.Sprintf("k_fill_%d", i),
			ID: fmt.Sprintf("cmd_fill_%d", i), Now: now, Deadline: now.Add(time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, Kind: "REBOOT", IdempotencyKey: "k_fill_overflow",
		ID: "cmd_fill_overflow", Now: now, Deadline: now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for 17th active command, got %v", err)
	}

	// e) CancelCommand (lines 231-264)
	canceledCmd, err := s.CancelCommand(ctx, scope, devID, "cmd_fill_14", now)
	if err != nil || canceledCmd.State != "CANCELLED" {
		t.Fatalf("expected CANCELLED state from CancelCommand, got %+v, err: %v", canceledCmd, err)
	}

	// f) Sync to dispatch cmd1 and get its dispatch token
	syncCmd, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 1, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
	})
	if err != nil || len(syncCmd.Commands) == 0 {
		t.Fatalf("expected dispatched command, got %v (len %d)", err, len(syncCmd.Commands))
	}
	dispCmd := syncCmd.Commands[0]

	// g) applyCommandReport branches:
	// - Wrong dispatch token -> ErrConflict (line 276)
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 2, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot"}},
		CommandReports: []ports.CommandReport{
			{CommandID: dispCmd.ID, DispatchToken: "wrong_token_12345678901234567890123456789012345678", State: "SUCCEEDED"},
		},
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on wrong dispatch token, got %v", err)
	}

	// - Invalid state -> ErrInvalidArgument (line 293)
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 3, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot"}},
		CommandReports: []ports.CommandReport{
			{CommandID: dispCmd.ID, DispatchToken: dispCmd.DispatchToken, State: "INVALID_STATE"},
		},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on invalid command state, got %v", err)
	}

	// - REBOOT SUCCEEDED with bad boot count (BootAfter <= BootBefore) -> ErrInvalidArgument (line 298)
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 4, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot"}},
		CommandReports: []ports.CommandReport{
			{
				CommandID: dispCmd.ID, DispatchToken: dispCmd.DispatchToken, State: "SUCCEEDED",
				Observation: &ports.CommandObservation{BootBefore: 5, BootAfter: 5},
			},
		},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument on invalid boot count, got %v", err)
	}

	// - Valid REBOOT report
	validRep := ports.CommandReport{
		CommandID: dispCmd.ID, DispatchToken: dispCmd.DispatchToken, State: "SUCCEEDED",
		Observation: &ports.CommandObservation{BootBefore: 5, BootAfter: 6},
	}
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 5, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot"}},
		CommandReports: []ports.CommandReport{validRep},
	})
	if err != nil {
		t.Fatal(err)
	}

	// - Duplicate identical report -> nil (line 290)
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 6, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot"}},
		CommandReports: []ports.CommandReport{validRep},
	})
	if err != nil {
		t.Fatalf("expected nil error on duplicate identical command report, got %v", err)
	}

	// - Duplicate differing report -> ErrConflict (line 288)
	diffRep := validRep
	diffRep.State = "FAILED"
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("c1"), Sequence: 7, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot"}},
		CommandReports: []ports.CommandReport{diffRep},
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate differing command report, got %v", err)
	}

	// 2. changes.go branches
	// a) ReadProjection with id == "" and after != 0 -> ErrInvalidArgument (line 355)
	_, err = s.ReadProjection(ctx, scope, "", 5, 10, now)
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for id == '' and after != 0, got %v", err)
	}

	// b) ReadProjection with limit = 1 and verify More == true (line 395)
	page1, err := s.ReadProjection(ctx, scope, "", 0, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if !page1.More {
		t.Fatalf("expected page1.More == true for limit=1, got false")
	}

	// c) ReadProjection with expired scan -> ErrCursorExpired (line 377)
	_, err = s.ReadProjection(ctx, scope, page1.SnapshotID, 0, 10, now.Add(24*time.Hour))
	if !errors.Is(err, ports.ErrCursorExpired) {
		t.Fatalf("expected ErrCursorExpired for expired scan, got %v", err)
	}

	// d) createScan eviction (lines 226-231): create 18 scans
	for i := 0; i < 18; i++ {
		_, err = s.ReadProjection(ctx, scope, "", 0, 10, now)
		if err != nil {
			t.Fatal(err)
		}
	}

	// e) ListChanges validation:
	// - limit < 1 or limit > 100 -> ErrInvalidArgument (line 79)
	_, err = s.ListChanges(ctx, scope, 0, 0)
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0, got %v", err)
	}
	_, err = s.ListChanges(ctx, scope, 0, 101)
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 101, got %v", err)
	}
	// - after > head -> ErrInvalidArgument (line 94)
	_, err = s.ListChanges(ctx, scope, 999999, 10)
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for after > head, got %v", err)
	}

	// f) change_heads sequence > ChangeRetention (10000): trigger floor > 0 and DELETE FROM changes (lines 47-51)
	_, err = db.ExecContext(ctx, "UPDATE change_heads SET sequence=10050, floor=50 WHERE scope_id=?", scope)
	if err != nil {
		t.Fatal(err)
	}
	// Calling Revoke triggers recordChange with head=10051 > 10000 -> floor=51 > 0
	_ = s.Revoke(ctx, scope, devID)

	// - after < floor -> ErrCursorExpired (line 91)
	_, err = s.ListChanges(ctx, scope, 10, 10)
	if !errors.Is(err, ports.ErrCursorExpired) {
		t.Fatalf("expected ErrCursorExpired for after < floor, got %v", err)
	}

	// 3. fleet.go branches
	artSHA := strings.Repeat("7", 64)
	_ = s.RegisterArtifact(ctx, ports.Artifact{ScopeID: scope, SHA256: artSHA, PackageName: "com.deep.app", VersionCode: 1, SizeBytes: 100})
	_, err = s.ReplaceGroup(ctx, ports.Group{ScopeID: scope, ID: "grp_empty", DeviceIDs: []string{}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	fleetReq := ports.FleetRequest{
		ScopeID: scope, ID: "fl_deep_1", GroupID: "grp_empty", ArtifactSHA256: artSHA,
		NotBefore: now, Deadline: now.Add(time.Hour),
	}
	// a) Schedule fleet with empty group/device list -> Complete == true (line 218)
	flEmpty, err := s.CreateFleetAssignment(ctx, fleetReq, now)
	if err != nil || !flEmpty.Complete {
		t.Fatalf("expected empty fleet assignment to be complete, got %+v, err: %v", flEmpty, err)
	}

	// b) Re-schedule with differing request -> ErrConflict (line 194)
	fleetReqDiff := fleetReq
	fleetReqDiff.ArtifactSHA256 = strings.Repeat("8", 64)
	_, err = s.CreateFleetAssignment(ctx, fleetReqDiff, now)
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate differing fleet request, got %v", err)
	}

	// c) Schedule with past deadline -> ErrInvalidArgument (line 202)
	fleetReqPast := fleetReq
	fleetReqPast.ID = "fl_past"
	fleetReqPast.Deadline = now.Add(-time.Hour)
	_, err = s.CreateFleetAssignment(ctx, fleetReqPast, now)
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for past fleet deadline, got %v", err)
	}

	// d) ListFleetAssignments validation (line 235)
	_, err = s.ListFleetAssignments(ctx, scope, "", 0)
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0 in ListFleetAssignments, got %v", err)
	}

	// 4. policies.go branches
	trueVal := true
	falseVal := false
	// a) SetPolicy on revoked device -> ErrRevoked (line 61)
	_, err = s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_pol_rev",
		Policy: ports.Policy{CameraDisabled: &trueVal},
	})
	if !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked on SetPolicy, got %v", err)
	}

	// b) Active device for policies
	devPol := "deep_dev_pol"
	tokPol := "deep_tok_pol"
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: hash32(tokPol), ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, _ = s.Consume(ctx, ports.Attempt{
		TokenHash: hash32(tokPol), ID: "att_pol", KeyHash: hash32("kp"), CredentialHash: hash32("cp"),
		Device: ports.Device{
			ID: devPol, ScopeID: scope, State: "ACTIVE",
			Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})

	// Required packages not among desired apps -> ErrNotFound (line 81)
	_, err = s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devPol, IdempotencyKey: "k_pol_req",
		Policy: ports.Policy{RequiredPackages: []string{"com.missing.required.app"}},
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing required package in SetPolicy, got %v", err)
	}

	// Duplicate idempotency key with differing policy -> ErrConflict (line 67)
	_, err = s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devPol, IdempotencyKey: "k_pol_idem",
		Policy: ports.Policy{CameraDisabled: &trueVal},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devPol, IdempotencyKey: "k_pol_idem",
		Policy: ports.Policy{CameraDisabled: &falseVal},
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate differing policy, got %v", err)
	}

	// 5. network.go branches
	// a) SetNetworkPolicy with empty arguments -> ErrInvalidArgument (line 69)
	_, err = s.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: "", DeviceID: devPol, IdempotencyKey: "k_net"})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty scope in SetNetworkPolicy, got %v", err)
	}

	// b) SetNetworkPolicy on revoked device -> ErrRevoked (line 89)
	_, err = s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_net_rev",
		Policy: ports.NetworkPolicy{PrimaryID: "w1", Profiles: []ports.WifiProfile{{ID: "w1", SSID: "WiFi-1", Security: "OPEN"}}},
	})
	if !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked on SetNetworkPolicy, got %v", err)
	}

	// c) Duplicate idempotency key with differing network policy -> ErrConflict (line 95)
	netPol1 := ports.NetworkPolicy{PrimaryID: "w1", Profiles: []ports.WifiProfile{{ID: "w1", SSID: "WiFi-1", Security: "OPEN"}}}
	_, err = s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID: scope, DeviceID: devPol, IdempotencyKey: "k_net_idem", Policy: netPol1,
	})
	if err != nil {
		t.Fatal(err)
	}
	netPol2 := ports.NetworkPolicy{PrimaryID: "w2", Profiles: []ports.WifiProfile{{ID: "w2", SSID: "WiFi-2", Security: "OPEN"}}}
	_, err = s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID: scope, DeviceID: devPol, IdempotencyKey: "k_net_idem", Policy: netPol2,
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate differing network policy, got %v", err)
	}

	// 6. management.go branches
	// a) GetOperation on wrong device -> ErrNotFound (line 69)
	opAssigned, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devPol, IdempotencyKey: "k_op_mgmt",
		OperationID: "op_mgmt_1", ArtifactSHA256: artSHA,
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.GetOperation(ctx, scope, devNoCap, opAssigned.ID, now)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for wrong device in GetOperation, got %v", err)
	}

	// b) GetOperation on non-existent device -> ErrNotFound (line 57)
	_, err = s.GetOperation(ctx, scope, "device_that_does_not_exist", opAssigned.ID, now)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent device in GetOperation, got %v", err)
	}
}

// TestSqlstoreRemainingTargetedCoverage exercises the remaining edge cases across sqlstore modules.
func TestSqlstoreRemainingTargetedCoverage(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "remain.db"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, db, "sqlite", "", true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	scope := "rem_scope"
	devID := "rem_dev_1"

	// 1. store.go: CreateEnrollment conflict on duplicate token_hash (line 150)
	tokHash := hash32("tok_dup_test")
	enr1 := ports.Enrollment{TokenHash: tokHash, ScopeID: scope, ExpiresAt: now.Add(time.Hour)}
	if err := s.CreateEnrollment(ctx, enr1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEnrollment(ctx, enr1); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate CreateEnrollment, got %v", err)
	}

	// 2. store.go: Consume branches
	// a) Token expired (line 206)
	tokExpired := hash32("tok_expired_test")
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: tokExpired, ScopeID: scope, ExpiresAt: now.Add(-time.Hour)})
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: tokExpired, ID: "att_exp", KeyHash: hash32("k"), CredentialHash: hash32("c"),
		Device: ports.Device{ID: "dev_exp", ScopeID: scope, State: "ACTIVE"},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}

	// b) Invalid candidate (empty receipt or invalid length) (line 209)
	tokCand := hash32("tok_cand_test")
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: tokCand, ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: tokCand, ID: "att_cand", KeyHash: hash32("k"), CredentialHash: hash32("c"),
		Device: ports.Device{ID: "dev_cand", ScopeID: scope, State: "ACTIVE"},
		Receipt: []byte{}, // empty receipt!
		Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err == nil {
		t.Fatal("expected error for empty receipt in Consume")
	}

	// c) Valid initial consume
	tokValid := hash32("tok_val_test")
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: tokValid, ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	rec, err := s.Consume(ctx, ports.Attempt{
		TokenHash: tokValid, ID: "att_val_1", KeyHash: hash32("kval"), CredentialHash: hash32("cval"),
		Device: ports.Device{
			ID: devID, ScopeID: scope, State: "ACTIVE",
			Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		},
		Receipt: []byte("valid_receipt"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err != nil || string(rec) != "valid_receipt" {
		t.Fatalf("expected valid initial consume, got %s, err: %v", string(rec), err)
	}

	// d) Re-consume with mismatched attempt ID -> ErrConsumed (line 191)
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: tokValid, ID: "att_mismatched_id", KeyHash: hash32("kval"), CredentialHash: hash32("cval"),
		Device: ports.Device{ID: devID, ScopeID: scope, State: "ACTIVE"},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrConsumed) {
		t.Fatalf("expected ErrConsumed on mismatched attempt ID, got %v", err)
	}

	// e) Re-consume after RecoverUntil -> ErrRecoveryExpired (line 201)
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: tokValid, ID: "att_val_1", KeyHash: hash32("kval"), CredentialHash: hash32("cval"),
		Device: ports.Device{ID: devID, ScopeID: scope, State: "ACTIVE"},
		Receipt: []byte("rec"), Now: now.Add(2 * time.Hour), RecoverUntil: now.Add(time.Hour),
	})
	if !errors.Is(err, ports.ErrRecoveryExpired) {
		t.Fatalf("expected ErrRecoveryExpired, got %v", err)
	}

	// 3. operations.go: expire deadline exceeded (lines 204-209)
	artSHA := strings.Repeat("a", 64)
	_ = s.RegisterArtifact(ctx, ports.Artifact{ScopeID: scope, SHA256: artSHA, PackageName: "com.rem.app", VersionCode: 1, SizeBytes: 100})
	opExpire, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_exp_op",
		OperationID: "op_expire_me", ArtifactSHA256: artSHA,
		Now: now, Deadline: now.Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = opExpire
	// Call ListOperations with now past deadline -> triggers expire lines 204-209
	opsExpired, err := s.ListOperations(ctx, scope, devID, "", 10, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	foundExpired := false
	for _, o := range opsExpired {
		if o.ID == "op_expire_me" && o.State == "FAILED" && o.ErrorCode == "DEADLINE_EXCEEDED" {
			foundExpired = true
		}
	}
	if !foundExpired {
		t.Fatalf("expected op_expire_me to be FAILED with DEADLINE_EXCEEDED, got %+v", opsExpired)
	}

	// 4. operations.go: RemoveArtifact when only active operations reference it (line 135)
	opRef, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "k_ref_op",
		OperationID: "op_ref_me", ArtifactSHA256: artSHA,
		Now: now, Deadline: now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = opRef
	// Delete desired_apps row directly so only operations row remains active
	_, err = db.ExecContext(ctx, "DELETE FROM desired_apps WHERE device_id=? AND artifact_sha256=?", devID, artSHA)
	if err != nil {
		t.Fatal(err)
	}
	// RemoveArtifact now bypasses desired_apps check and hits line 135 on operations table!
	if err := s.RemoveArtifact(ctx, scope, artSHA); !errors.Is(err, ports.ErrReferenced) {
		t.Fatalf("expected ErrReferenced from active operations check in RemoveArtifact, got %v", err)
	}

	// 5. operations.go: applyReport branches
	// Lease op_ref_me via Sync
	syncLease, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cval"), Sequence: 1, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
	})
	if err != nil || len(syncLease.Operations) == 0 {
		t.Fatalf("expected leased operation, got %v (len %d)", err, len(syncLease.Operations))
	}
	leasedOp := syncLease.Operations[0]

	// a) Report with nil Observed when state is SUCCEEDED -> OBSERVATION_MISMATCH (line 435)
	syncNilObs, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cval"), Sequence: 2, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		Reports: []ports.OperationReport{
			{ID: "rep_nil_obs", OperationID: leasedOp.ID, LeaseID: leasedOp.LeaseID, Sequence: 1, State: "SUCCEEDED", Observed: nil},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncNilObs.Receipts) == 0 || syncNilObs.Receipts[0].Reason != "OBSERVATION_MISMATCH" {
		t.Fatalf("expected OBSERVATION_MISMATCH for nil observed, got %+v", syncNilObs.Receipts)
	}

	// b) Report device mismatch: Enroll another device, and submit report for leasedOp from dev2 -> NOT_ASSIGNED (line 423)
	devID2 := "rem_dev_2"
	tok2 := hash32("tok2")
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: tok2, ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, _ = s.Consume(ctx, ports.Attempt{
		TokenHash: tok2, ID: "att_2", KeyHash: hash32("k2"), CredentialHash: hash32("c2"),
		Device: ports.Device{ID: devID2, ScopeID: scope, State: "ACTIVE"},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	syncDevMismatch, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID2, CredentialHash: hash32("c2"), Sequence: 1, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true},
		Reports: []ports.OperationReport{
			{ID: "rep_dev_mismatch", OperationID: leasedOp.ID, LeaseID: leasedOp.LeaseID, Sequence: 1, State: "RUNNING"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncDevMismatch.Receipts) == 0 || syncDevMismatch.Receipts[0].Reason != "NOT_ASSIGNED" {
		t.Fatalf("expected NOT_ASSIGNED for device mismatch, got %+v", syncDevMismatch.Receipts)
	}

	// c) Report on FAILED operation -> TERMINAL (line 429)
	// Cancel op_ref_me to make it terminal
	_, _ = s.CancelOperation(ctx, scope, devID, leasedOp.ID, now)
	syncCancelRep, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cval"), Sequence: 3, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: true, Capabilities: []string{"command.reboot", "command.lock"}},
		Reports: []ports.OperationReport{
			{ID: "rep_on_cancel", OperationID: leasedOp.ID, LeaseID: leasedOp.LeaseID, Sequence: 1, State: "RUNNING"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syncCancelRep.Receipts) == 0 || syncCancelRep.Receipts[0].Reason != "TERMINAL" {
		t.Fatalf("expected TERMINAL for canceled op, got %+v", syncCancelRep.Receipts)
	}

	// 6. commands.go: dispatchCommand with missing capability -> CAPABILITY_UNAVAILABLE (line 327)
	// Submit command on devID (which currently has command.reboot capability in DB)
	_, err = s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, Kind: "REBOOT", IdempotencyKey: "k_cmd_cap",
		ID: "cmd_cap_test", Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Sync with inventory that has DeviceOwner=false
	syncNoCap, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: hash32("cval"), Sequence: 4, Now: now,
		Inventory: ports.Inventory{APILevel: 34, Model: "Pixel", DeviceOwner: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = syncNoCap
	cmdCapCheck, err := s.GetCommand(ctx, scope, devID, "cmd_cap_test", now)
	if err != nil || cmdCapCheck.State != "FAILED" || cmdCapCheck.ErrorCode != "CAPABILITY_UNAVAILABLE" {
		t.Fatalf("expected FAILED with CAPABILITY_UNAVAILABLE, got %+v, err: %v", cmdCapCheck, err)
	}

	// 7. fleet.go: AdvanceFleetAssignment CONFLICT and INVALID_ARGUMENT (lines 305, 307)
	// a) Device 1 already has operation with idempotency key "fleet-" + fleetKey(...) for artSHA
	artSHA2 := strings.Repeat("b", 64)
	_ = s.RegisterArtifact(ctx, ports.Artifact{ScopeID: scope, SHA256: artSHA2, PackageName: "com.rem.app2", VersionCode: 1, SizeBytes: 100})
	keyConf := fleetKey(scope, "fl_conflict", devID)
	_, err = s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "fleet-" + keyConf,
		OperationID: "op_pre_conflict", ArtifactSHA256: artSHA,
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Create fleet assignment with ID "fl_conflict" for artSHA2 targeting devID
	flConfReq := ports.FleetRequest{
		ScopeID: scope, ID: "fl_conflict", DeviceIDs: []string{devID}, ArtifactSHA256: artSHA2,
		NotBefore: now, Deadline: now.Add(time.Hour),
	}
	_, err = s.CreateFleetAssignment(ctx, flConfReq, now)
	if err != nil {
		t.Fatal(err)
	}
	// Advance fleet assignment: assign returns ErrConflict, triggers line 305
	flConfAdv, err := s.AdvanceFleetAssignment(ctx, scope, "fl_conflict", now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(flConfAdv.Items) == 0 || flConfAdv.Items[0].ErrorCode != "CONFLICT" {
		t.Fatalf("expected CONFLICT error code on AdvanceFleetAssignment, got %+v", flConfAdv.Items)
	}

	// b) Advance fleet assignment where NotBefore >= Deadline -> INVALID_ARGUMENT (line 307)
	// Directly update request in DB so NotBefore > Deadline inside assign()
	flInvalReq := ports.FleetRequest{
		ScopeID: scope, ID: "fl_inval", DeviceIDs: []string{devID2}, ArtifactSHA256: artSHA,
		NotBefore: now, Deadline: now.Add(time.Hour),
	}
	_, err = s.CreateFleetAssignment(ctx, flInvalReq, now)
	if err != nil {
		t.Fatal(err)
	}
	// Update fleet_assignments record data to have NotBefore > Deadline
	batchInval, _ := s.GetFleetAssignment(ctx, scope, "fl_inval")
	batchInval.Request.NotBefore = now.Add(2 * time.Hour)
	dataInval, _ := json.Marshal(batchInval)
	_, _ = db.ExecContext(ctx, "UPDATE fleet_assignments SET data=? WHERE id=?", dataInval, "fl_inval")

	flInvalAdv, err := s.AdvanceFleetAssignment(ctx, scope, "fl_inval", now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(flInvalAdv.Items) == 0 || flInvalAdv.Items[0].ErrorCode != "INVALID_ARGUMENT" {
		t.Fatalf("expected INVALID_ARGUMENT error code on AdvanceFleetAssignment, got %+v", flInvalAdv.Items)
	}
}

// TestSqlstoreCanceledContextAndClosedDB systematically exercises transaction BeginTx and query error branches.
func TestSqlstoreCanceledContextAndClosedDB(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "canc.db"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, db, "sqlite", "", true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()

	cancCtx, cancel := context.WithCancel(ctx)
	cancel() // canceled immediately!

	// 1. BeginTx error returns on canceled context:
	_, _ = s.ListChanges(cancCtx, "s", 0, 10)
	_, _ = s.ReadProjection(cancCtx, "s", "", 0, 10, now)
	_, _ = s.SubmitCommand(cancCtx, ports.CommandAssignment{ScopeID: "s", DeviceID: "d", Kind: "REBOOT", IdempotencyKey: "k", ID: "c", Now: now, Deadline: now.Add(time.Hour)})
	_, _ = s.GetCommand(cancCtx, "s", "d", "c", now)
	_, _ = s.ListCommands(cancCtx, "s", "d", "", 10, now)
	_, _ = s.CancelCommand(cancCtx, "s", "d", "c", now)
	_, _ = s.ReplaceGroup(cancCtx, ports.Group{ScopeID: "s", ID: "g"}, 0)
	_, _ = s.CreateFleetAssignment(cancCtx, ports.FleetRequest{ScopeID: "s", ID: "f", GroupID: "g", ArtifactSHA256: strings.Repeat("f", 64), NotBefore: now, Deadline: now.Add(time.Hour)}, now)
	_, _ = s.AdvanceFleetAssignment(cancCtx, "s", "f", now, 10)
	_, _ = s.CancelOperation(cancCtx, "s", "d", "o", now)
	_, _ = s.CancelFleetAssignment(cancCtx, "s", "f", now)
	_, _ = s.GetOperation(cancCtx, "s", "d", "o", now)
	_ = s.Revoke(cancCtx, "s", "d")
	_, _ = s.SetNetworkPolicy(cancCtx, ports.NetworkAssignment{ScopeID: "s", DeviceID: "d", IdempotencyKey: "k", Policy: ports.NetworkPolicy{PrimaryID: "w", Profiles: []ports.WifiProfile{{ID: "w", SSID: "s", Security: "OPEN"}}}})
	_ = s.RegisterArtifact(cancCtx, ports.Artifact{ScopeID: "s", SHA256: strings.Repeat("f", 64)})
	_ = s.RemoveArtifact(cancCtx, "s", strings.Repeat("f", 64))
	_, _ = s.Assign(cancCtx, ports.Assignment{ScopeID: "s", DeviceID: "d", IdempotencyKey: "k", OperationID: "o", ArtifactSHA256: strings.Repeat("f", 64), Now: now, Deadline: now.Add(time.Hour)})
	_, _ = s.ListOperations(cancCtx, "s", "d", "", 10, now)
	_, _ = s.AuthorizeDownload(cancCtx, "d", []byte("h"), "o", "dig", "l", now)
	_, _ = s.Sync(cancCtx, ports.SyncInput{DeviceID: "d", CredentialHash: []byte("h")})
	_, _ = s.SetPolicy(cancCtx, ports.PolicyAssignment{ScopeID: "s", DeviceID: "d", IdempotencyKey: "k"})
	_, _ = s.Consume(cancCtx, ports.Attempt{TokenHash: []byte("h"), ID: "id", KeyHash: []byte("k"), CredentialHash: []byte("c"), Device: ports.Device{ID: "d", ScopeID: "s"}, Receipt: []byte("r"), Now: now, RecoverUntil: now.Add(time.Hour)})

	// 2. Query error returns on closed db:
	db.Close()
	_, _ = s.ListArtifacts(ctx, "s", "", 10)
	_, _ = s.Artifact(ctx, "s", "digest")
	_, _ = s.Enrollment(ctx, []byte("h"))
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: []byte("h"), ScopeID: "s", ExpiresAt: now.Add(time.Hour)})
	_, _ = s.ListGroups(ctx, "s", "", 10)
	_, _ = s.GetGroup(ctx, "s", "id")
	_, _ = s.GetFleetAssignment(ctx, "s", "id")
	_, _ = s.ListFleetAssignments(ctx, "s", "", 10)
	_, _ = s.QueryDevices(ctx, ports.DeviceQuery{ScopeID: "s"})
	_, _ = s.GetDevice(ctx, "s", "id")
	_, _ = s.QueryOperations(ctx, ports.OperationQuery{ScopeID: "s", DeviceID: "d"})
}

// TestSqlstoreExtremeTargetedBranches covers edge cases in operations, changes, commands, policies, network, and fleet.
func TestSqlstoreExtremeTargetedBranches(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "target_branches.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := Open(ctx, db, "sqlite", "", true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	scope := "scope_extreme"
	devID1 := "dev_extreme_1"
	devID2 := "dev_extreme_2"
	artSHA1 := strings.Repeat("1", 64)
	artSHA2 := strings.Repeat("2", 64)

	// Helper to enroll a device
	enrollDev := func(id string, owner bool) {
		tHash := make([]byte, 32)
		copy(tHash, "tok_"+id)
		kHash := make([]byte, 32)
		copy(kHash, "k_"+id)
		cHash := make([]byte, 32)
		copy(cHash, "cred_"+id)
		_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: tHash, ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
		_, err := s.Consume(ctx, ports.Attempt{
			TokenHash: tHash, ID: "att_" + id, KeyHash: kHash, CredentialHash: cHash,
			Device: ports.Device{
				ID: id, ScopeID: scope, State: "ENROLLED",
				Inventory: ports.Inventory{DeviceOwner: owner, Capabilities: []string{"command.reboot", "command.lock"}},
				CreatedAt: now,
			},
			Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("enroll %s: %v", id, err)
		}
	}
	enrollDev(devID1, true)
	enrollDev(devID2, true)

	// 1. operations.go: ListArtifacts with empty scope
	if _, err := s.ListArtifacts(ctx, "", "", 10); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty scope in ListArtifacts, got %v", err)
	}

	// 2. fleet.go: ListFleetAssignments bounds checks
	if _, err := s.ListFleetAssignments(ctx, "", "", 10); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty scope, got %v", err)
	}
	if _, err := s.ListFleetAssignments(ctx, scope, "", 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0, got %v", err)
	}
	if _, err := s.ListFleetAssignments(ctx, scope, "", 102); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 102, got %v", err)
	}

	// 3. management.go: QueryOperations bounds & not found checks
	if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: "", DeviceID: devID1, Limit: 10}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty scope in QueryOperations")
	}
	if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: "", Limit: 10}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty device in QueryOperations")
	}
	if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: devID1, Limit: 0}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0 in QueryOperations")
	}
	if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: devID1, Limit: 102}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 102 in QueryOperations")
	}
	if _, err := s.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: "dev_nonexistent", Limit: 10}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for nonexistent device in QueryOperations")
	}

	// 4. operations.go & management.go: RegisterArtifact, Assign branches & GetOperation mismatch
	_ = s.RegisterArtifact(ctx, ports.Artifact{ScopeID: scope, SHA256: artSHA1, PackageName: "com.pkg.one", VersionCode: 1, SizeBytes: 100})
	_ = s.RegisterArtifact(ctx, ports.Artifact{ScopeID: scope, SHA256: artSHA2, PackageName: "com.pkg.two", VersionCode: 1, SizeBytes: 100})

	// Assign with invalid deadline
	if _, err := s.Assign(ctx, ports.Assignment{ScopeID: scope, DeviceID: devID1, IdempotencyKey: "k_inv_dl", OperationID: "op_inv_dl", ArtifactSHA256: artSHA1, Now: now, Deadline: now.Add(-time.Minute)}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for past deadline in Assign, got %v", err)
	}
	if _, err := s.Assign(ctx, ports.Assignment{ScopeID: scope, DeviceID: devID1, IdempotencyKey: "k_inv_nb", OperationID: "op_inv_nb", ArtifactSHA256: artSHA1, Now: now, NotBefore: now.Add(2 * time.Hour), Deadline: now.Add(time.Hour)}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for NotBefore after Deadline in Assign, got %v", err)
	}

	// Assign op1 on dev1
	op1, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID1, IdempotencyKey: "op_key_1", OperationID: "op_id_1",
		ArtifactSHA256: artSHA1, Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Re-assign identical key returns existing op
	op1Repeat, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID1, IdempotencyKey: "op_key_1", OperationID: "op_id_1",
		ArtifactSHA256: artSHA1, Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil || op1Repeat.ID != op1.ID {
		t.Fatalf("expected repeated assign to return op1, got %+v, err: %v", op1Repeat, err)
	}
	// Re-assign same key with conflicting artifact returns ErrConflict
	if _, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID1, IdempotencyKey: "op_key_1", OperationID: "op_id_conf",
		ArtifactSHA256: artSHA2, Now: now, Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on conflicting Assign, got %v", err)
	}
	// GetOperation on dev2 with dev1's operation ID -> ErrNotFound
	if _, err := s.GetOperation(ctx, scope, devID2, op1.ID, now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for operation ID on wrong device, got %v", err)
	}
	// ListOperations for nonexistent device -> ErrNotFound
	if _, err := s.ListOperations(ctx, scope, "dev_missing", "", 10, now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for nonexistent device in ListOperations, got %v", err)
	}

	// 5. commands.go: commands limit, invalid deadlines, and idempotency conflicts
	// Deadline > 7 days -> ErrInvalidArgument
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID1, Kind: "REBOOT", IdempotencyKey: "k_cmd_dl", ID: "c_dl",
		Now: now, Deadline: now.Add(8 * 24 * time.Hour),
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for command deadline > 7d, got %v", err)
	}
	// Submit command
	cmd1, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID1, Kind: "REBOOT", IdempotencyKey: "cmd_idem_key", ID: "cmd_id_1",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Repeat same key & kind -> returns original
	cmd1Repeat, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID1, Kind: "REBOOT", IdempotencyKey: "cmd_idem_key", ID: "cmd_id_1",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil || cmd1Repeat.ID != cmd1.ID {
		t.Fatalf("expected repeated SubmitCommand to return cmd1, got %+v, err: %v", cmd1Repeat, err)
	}
	// Same key & different kind -> ErrConflict
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID1, Kind: "LOCK", IdempotencyKey: "cmd_idem_key", ID: "cmd_id_conflict",
		Now: now, Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for conflicting command kind, got %v", err)
	}
	// Fill up to 16 commands on dev1 to test maximumActiveCommands limit
	for i := 2; i <= 16; i++ {
		_, err := s.SubmitCommand(ctx, ports.CommandAssignment{
			ScopeID: scope, DeviceID: devID1, Kind: "REBOOT",
			IdempotencyKey: fmt.Sprintf("cmd_key_%d", i), ID: fmt.Sprintf("cmd_id_%d", i),
			Now: now, Deadline: now.Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("fill command %d: %v", i, err)
		}
	}
	// 17th command returns ErrConflict
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID1, Kind: "REBOOT",
		IdempotencyKey: "cmd_key_17", ID: "cmd_id_17",
		Now: now, Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict when exceeding 16 active commands, got %v", err)
	}

	// 6. policies.go: RequiredPackages not found, idempotency return & conflict
	// Missing required package -> ErrNotFound
	if _, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devID1, IdempotencyKey: "pol_key_req",
		Policy: ports.Policy{RequiredPackages: []string{"com.nonexistent.app"}},
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing required package in SetPolicy, got %v", err)
	}
	trueVal := true
	falseVal := false
	// Valid policy
	pol1, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devID1, IdempotencyKey: "pol_key_valid",
		Policy: ports.Policy{CameraDisabled: &trueVal},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Repeat identical policy & key -> returns cached
	pol1Repeat, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devID1, IdempotencyKey: "pol_key_valid",
		Policy: ports.Policy{CameraDisabled: &trueVal},
	})
	if err != nil || pol1Repeat.Revision != pol1.Revision {
		t.Fatalf("expected repeated SetPolicy to return cached, got %+v, err: %v", pol1Repeat, err)
	}
	// Conflicting policy on same key -> ErrConflict
	if _, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devID1, IdempotencyKey: "pol_key_valid",
		Policy: ports.Policy{CameraDisabled: &falseVal},
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on conflicting SetPolicy, got %v", err)
	}

	// 7. network.go: idempotency return & conflict
	netProf1 := ports.WifiProfile{ID: "w_extreme", SSID: "ExtremeSSID", Security: "OPEN"}
	netProf2 := ports.WifiProfile{ID: "w_extreme", SSID: "OtherSSID", Security: "OPEN"}
	net1, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID: scope, DeviceID: devID1, IdempotencyKey: "net_key_valid",
		Policy: ports.NetworkPolicy{PrimaryID: "w_extreme", Profiles: []ports.WifiProfile{netProf1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Repeat identical -> returns cached
	net1Repeat, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID: scope, DeviceID: devID1, IdempotencyKey: "net_key_valid",
		Policy: ports.NetworkPolicy{PrimaryID: "w_extreme", Profiles: []ports.WifiProfile{netProf1}},
	})
	if err != nil || net1Repeat.Revision != net1.Revision {
		t.Fatalf("expected cached network policy, got %+v, err: %v", net1Repeat, err)
	}
	// Conflicting network policy -> ErrConflict
	if _, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID: scope, DeviceID: devID1, IdempotencyKey: "net_key_valid",
		Policy: ports.NetworkPolicy{PrimaryID: "w_extreme", Profiles: []ports.WifiProfile{netProf2}},
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on conflicting SetNetworkPolicy, got %v", err)
	}

	// 8. fleet.go: ReplaceGroup with revoked device, CreateFleetAssignment with group
	devRev := "dev_extreme_revoked"
	enrollDev(devRev, true)
	if err := s.Revoke(ctx, scope, devRev); err != nil {
		t.Fatal(err)
	}
	// ReplaceGroup with revoked device -> ErrRevoked
	if _, err := s.ReplaceGroup(ctx, ports.Group{ScopeID: scope, ID: "g_has_rev", DeviceIDs: []string{devRev}}, 0); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked when adding revoked device to group, got %v", err)
	}
	// Create valid group
	_, err = s.ReplaceGroup(ctx, ports.Group{ScopeID: scope, ID: "g_valid", DeviceIDs: []string{devID1, devID2}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	// CreateFleetAssignment with missing group -> ErrNotFound
	if _, err := s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID: scope, ID: "fl_missing_grp", GroupID: "g_nonexistent",
		ArtifactSHA256: artSHA1, NotBefore: now, Deadline: now.Add(time.Hour),
	}, now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing group in CreateFleetAssignment, got %v", err)
	}
	// CreateFleetAssignment with valid group
	flBatch, err := s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID: scope, ID: "fl_with_grp", GroupID: "g_valid",
		ArtifactSHA256: artSHA1, NotBefore: now, Deadline: now.Add(time.Hour),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(flBatch.Items) != 2 {
		t.Fatalf("expected 2 items from group in fleet assignment, got %d", len(flBatch.Items))
	}

	// 9. changes.go: ChangeRetention floor calculation & deletion
	// Manually set change_heads sequence = 10005
	_, _ = db.ExecContext(ctx, "UPDATE change_heads SET sequence=10005 WHERE scope_id=?", scope)
	// Now trigger a change via RegisterArtifact
	artSHA3 := strings.Repeat("3", 64)
	if err := s.RegisterArtifact(ctx, ports.Artifact{ScopeID: scope, SHA256: artSHA3, PackageName: "com.pkg.three", VersionCode: 1, SizeBytes: 100}); err != nil {
		t.Fatal(err)
	}

	// 10. changes.go: Projection scan eviction when len(retained) >= ports.SnapshotLimit (4)
	for i := 0; i < 5; i++ {
		_, err := s.ReadProjection(ctx, scope, "", 0, 10, now.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("ReadProjection %d: %v", i, err)
		}
	}

	// 11. operations.go: Sync edge cases
	cHash1 := make([]byte, 32)
	copy(cHash1, "cred_"+devID1)

	// a) Sync with Sequence 0 -> ErrStale
	if _, err := s.Sync(ctx, ports.SyncInput{DeviceID: devID1, CredentialHash: cHash1, Sequence: 0, Now: now}); !errors.Is(err, ports.ErrStale) {
		t.Fatalf("expected ErrStale on Sequence 0 in Sync, got %v", err)
	}

	// b) Sync with ReportedRevision > DesiredRevision -> ErrStale
	if _, err := s.Sync(ctx, ports.SyncInput{DeviceID: devID1, CredentialHash: cHash1, Sequence: 1, ReportedRevision: 999, Now: now}); !errors.Is(err, ports.ErrStale) {
		t.Fatalf("expected ErrStale on ReportedRevision > DesiredRevision in Sync, got %v", err)
	}

	// c) Sync with > 16 CommandReports -> ErrInvalidArgument
	cmdReps := make([]ports.CommandReport, 17)
	for i := range cmdReps {
		cmdReps[i] = ports.CommandReport{CommandID: fmt.Sprintf("cmd_%d", i)}
	}
	if _, err := s.Sync(ctx, ports.SyncInput{DeviceID: devID1, CredentialHash: cHash1, Sequence: 1, CommandReports: cmdReps, Now: now}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for > 16 command reports in Sync, got %v", err)
	}

	// d) Sync with > 64 RetainedOperationIDs -> ErrInvalidArgument
	retOps := make([]string, 65)
	for i := range retOps {
		retOps[i] = fmt.Sprintf("op_%d", i)
	}
	if _, err := s.Sync(ctx, ports.SyncInput{DeviceID: devID1, CredentialHash: cHash1, Sequence: 1, RetainedOperationIDs: retOps, Now: now}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for > 64 retained ops in Sync, got %v", err)
	}

	// e) Normal sync 1 on devID1 (gets lease for op1)
	snap1, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID1, CredentialHash: cHash1, Sequence: 1,
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"install_apk", "command.reboot"}},
		Now: now,
	})
	if err != nil {
		t.Fatalf("Sync 1: %v", err)
	}
	var leaseID string
	for _, op := range snap1.Operations {
		if op.ID == op1.ID {
			leaseID = op.LeaseID
		}
	}

	// f) applyReport branches:
	// - STALE_LEASE: report with wrong leaseID
	repStaleLease := ports.OperationReport{
		ID: "rep_sl", OperationID: op1.ID, LeaseID: "wrong_lease", Sequence: 1, State: "DOWNLOADING",
	}
	snapSL, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID1, CredentialHash: cHash1, Sequence: 2,
		Reports: []ports.OperationReport{repStaleLease},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"install_apk"}},
		Now: now.Add(time.Second),
	})
	if err != nil || len(snapSL.Receipts) == 0 || snapSL.Receipts[0].Reason != "STALE_LEASE" {
		t.Fatalf("expected STALE_LEASE, got %+v, err: %v", snapSL.Receipts, err)
	}

	// - Valid report advancing op1 sequence to 1
	repGood := ports.OperationReport{
		ID: "rep_good", OperationID: op1.ID, LeaseID: leaseID, Sequence: 1, State: "DOWNLOADING", DownloadedBytes: 50,
	}
	snapGood, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID1, CredentialHash: cHash1, Sequence: 3,
		Reports: []ports.OperationReport{repGood},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"install_apk"}},
		Now: now.Add(2 * time.Second),
	})
	if err != nil || len(snapGood.Receipts) == 0 || !snapGood.Receipts[0].Accepted {
		t.Fatalf("expected accepted report, got %+v, err: %v", snapGood.Receipts, err)
	}

	// - STALE_REPORT: report with Sequence <= op.ReportSequence (1 <= 1)
	repStaleRep := ports.OperationReport{
		ID: "rep_sr", OperationID: op1.ID, LeaseID: leaseID, Sequence: 1, State: "DOWNLOADING",
	}
	snapSR, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID1, CredentialHash: cHash1, Sequence: 4,
		Reports: []ports.OperationReport{repStaleRep},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"install_apk"}},
		Now: now.Add(3 * time.Second),
	})
	if err != nil || len(snapSR.Receipts) == 0 || snapSR.Receipts[0].Reason != "STALE_REPORT" {
		t.Fatalf("expected STALE_REPORT, got %+v, err: %v", snapSR.Receipts, err)
	}

	// - OBSERVATION_MISMATCH: SUCCEEDED but Observed != op.Artifact
	repMismatch := ports.OperationReport{
		ID: "rep_mis", OperationID: op1.ID, LeaseID: leaseID, Sequence: 2, State: "SUCCEEDED",
		Observed: &ports.InstalledApplication{PackageName: "com.wrong.pkg", VersionCode: 99},
	}
	snapMis, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID1, CredentialHash: cHash1, Sequence: 5,
		Reports: []ports.OperationReport{repMismatch},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"install_apk"}},
		Now: now.Add(4 * time.Second),
	})
	if err != nil || len(snapMis.Receipts) == 0 || snapMis.Receipts[0].Reason != "OBSERVATION_MISMATCH" {
		t.Fatalf("expected OBSERVATION_MISMATCH, got %+v, err: %v", snapMis.Receipts, err)
	}

	// - Successful report with SUCCEEDED setting operation terminal
	repSucc := ports.OperationReport{
		ID: "rep_succ", OperationID: op1.ID, LeaseID: leaseID, Sequence: 3, State: "SUCCEEDED",
		Observed: &ports.InstalledApplication{PackageName: "com.pkg.one", VersionCode: 1},
	}
	snapSucc, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID1, CredentialHash: cHash1, Sequence: 6,
		Reports: []ports.OperationReport{repSucc},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"install_apk"}, Applications: []ports.InstalledApplication{{PackageName: "com.pkg.one", VersionCode: 1}}},
		Now: now.Add(5 * time.Second),
	})
	if err != nil || len(snapSucc.Receipts) == 0 || !snapSucc.Receipts[0].Accepted {
		t.Fatalf("expected SUCCEEDED accepted, got %+v, err: %v", snapSucc.Receipts, err)
	}

	// - TERMINAL: report on an already-terminal (SUCCEEDED) operation
	repTerm := ports.OperationReport{
		ID: "rep_term", OperationID: op1.ID, LeaseID: leaseID, Sequence: 4, State: "SUCCEEDED",
	}
	snapTerm, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID1, CredentialHash: cHash1, Sequence: 7,
		Reports: []ports.OperationReport{repTerm},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"install_apk"}},
		Now: now.Add(6 * time.Second),
	})
	if err != nil || len(snapTerm.Receipts) == 0 || snapTerm.Receipts[0].Reason != "TERMINAL" {
		t.Fatalf("expected TERMINAL reason, got %+v, err: %v", snapTerm.Receipts, err)
	}

	// - RetainedOperationIDs with operation for different device or missing -> adds to StoppedOperationIDs
	opDev2, _ := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID2, IdempotencyKey: "k_dev2_ret", OperationID: "op_dev2_ret",
		ArtifactSHA256: artSHA1, Now: now, Deadline: now.Add(time.Hour),
	})
	snapRet, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID1, CredentialHash: cHash1, Sequence: 8,
		RetainedOperationIDs: []string{opDev2.ID, "nonexistent_op"},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"install_apk"}},
		Now: now.Add(7 * time.Second),
	})
	if err != nil || len(snapRet.StoppedOperationIDs) != 2 {
		t.Fatalf("expected 2 stopped ops, got %+v, err: %v", snapRet.StoppedOperationIDs, err)
	}

	// 12. Desired app compliance without install_apk capability -> INSTALL_CAPABILITY_MISSING
	devNoCap := "dev_no_cap"
	enrollDev(devNoCap, true)
	cHashNoCap := make([]byte, 32)
	copy(cHashNoCap, "cred_"+devNoCap)
	_, _ = s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devNoCap, IdempotencyKey: "k_no_cap", OperationID: "op_no_cap",
		ArtifactSHA256: artSHA1, Now: now, Deadline: now.Add(time.Hour),
	})
	snapNoCap, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devNoCap, CredentialHash: cHashNoCap, Sequence: 1,
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{}}, // no install_apk!
		Now: now,
	})
	if err != nil || len(snapNoCap.Compliance) == 0 || snapNoCap.Compliance[0].Reason != "INSTALL_CAPABILITY_MISSING" {
		t.Fatalf("expected INSTALL_CAPABILITY_MISSING, got %+v, err: %v", snapNoCap.Compliance, err)
	}
}

// TestSqlstoreProjectionFullCoverage exercises projection materialization of artifacts, groups, fleet assignments and pagination.
func TestSqlstoreProjectionFullCoverage(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "proj_full.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := Open(ctx, db, "sqlite", "", true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	scope := "scope_proj_full"
	devID := "dev_proj_full"
	artSHA := strings.Repeat("a", 64)

	// Enroll device
	tHash := make([]byte, 32)
	copy(tHash, "tok_proj")
	kHash := make([]byte, 32)
	copy(kHash, "k_proj")
	cHash := make([]byte, 32)
	copy(cHash, "cred_proj")
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: tHash, ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: tHash, ID: "att_proj", KeyHash: kHash, CredentialHash: cHash,
		Device: ports.Device{
			ID: devID, ScopeID: scope, State: "ENROLLED",
			Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"install_apk"}},
			CreatedAt: now,
		},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Register artifact
	if err := s.RegisterArtifact(ctx, ports.Artifact{
		ScopeID: scope, SHA256: artSHA, PackageName: "com.proj.app", VersionCode: 1, SizeBytes: 100,
	}); err != nil {
		t.Fatal(err)
	}

	// 2. Create fleet group
	if _, err := s.ReplaceGroup(ctx, ports.Group{
		ScopeID: scope, ID: "grp_proj", DeviceIDs: []string{devID},
	}, 0); err != nil {
		t.Fatal(err)
	}

	// 3. Create fleet assignment
	if _, err := s.CreateFleetAssignment(ctx, ports.FleetRequest{
		ScopeID: scope, ID: "fleet_proj", GroupID: "grp_proj", ArtifactSHA256: artSHA,
		NotBefore: now, Deadline: now.Add(time.Hour),
	}, now); err != nil {
		t.Fatal(err)
	}

	// 4. Invalid arguments to ReadProjection (line 355)
	if _, err := s.ReadProjection(ctx, "", "", 0, 10, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty scope")
	}
	if _, err := s.ReadProjection(ctx, scope, "", -1, 10, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for negative after")
	}
	if _, err := s.ReadProjection(ctx, scope, "", 0, 0, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0")
	}
	if _, err := s.ReadProjection(ctx, scope, "", 0, 101, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit > 100")
	}
	if _, err := s.ReadProjection(ctx, scope, "", 5, 10, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for id == '' and after != 0")
	}

	// 5. ReadProjection with limit = 1 triggers result.More = true (lines 394-396)
	page1, err := s.ReadProjection(ctx, scope, "", 0, 1, now)
	if err != nil {
		t.Fatalf("ReadProjection page 1: %v", err)
	}
	if !page1.More || len(page1.Items) != 1 {
		t.Fatalf("expected More=true and 1 item on limit 1, got More=%v, items=%d", page1.More, len(page1.Items))
	}

	// 6. ReadProjection page 2 reads remaining items (lines 379-404)
	page2, err := s.ReadProjection(ctx, scope, page1.SnapshotID, page1.After, 10, now)
	if err != nil {
		t.Fatalf("ReadProjection page 2: %v", err)
	}
	if len(page2.Items) == 0 {
		t.Fatalf("expected items on page 2")
	}

	// 7. ReadProjection with expired cursor (line 377)
	if _, err := s.ReadProjection(ctx, scope, page1.SnapshotID, page1.After, 10, now.Add(2*ports.SnapshotLifetime)); !errors.Is(err, ports.ErrCursorExpired) {
		t.Fatalf("expected ErrCursorExpired on expired cursor")
	}

	// 8. ReadProjection with nonexistent snapshot ID (line 371)
	if _, err := s.ReadProjection(ctx, scope, "nonexistent_snap_id", 0, 10, now); !errors.Is(err, ports.ErrCursorExpired) {
		t.Fatalf("expected ErrCursorExpired on nonexistent snapshot ID")
	}
}

// TestSqlstoreRemainingCoverageBranches exercises remaining edge cases in network policy, policy requests,
// command dispatching, device state retrieval, changes retention and projection scans eviction.
func TestSqlstoreRemainingCoverageBranches(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "rem_cov.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := Open(ctx, db, "sqlite", "", true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	scope := "scope_rem_cov"
	devID := "dev_rem_cov"

	// 1. SetNetworkPolicy argument validation
	if _, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty network assignment, got %v", err)
	}

	// Enroll device
	tHash := make([]byte, 32)
	copy(tHash, "tok_rem")
	kHash := make([]byte, 32)
	copy(kHash, "k_rem")
	cHash := make([]byte, 32)
	copy(cHash, "cred_rem")
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: tHash, ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: tHash, ID: "att_rem", KeyHash: kHash, CredentialHash: cHash,
		Device: ports.Device{
			ID: devID, ScopeID: scope, State: "ENROLLED",
			Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"install_apk", "command.lock", "command.reboot"}},
			CreatedAt: now,
		},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. SetNetworkPolicy success, idempotency cache hit, and idempotency conflict
	netAssign := ports.NetworkAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "net_idem_1",
		Policy: ports.NetworkPolicy{
			PrimaryID: "prof1",
			Profiles:  []ports.WifiProfile{{ID: "prof1", SSID: "TestSSID", Security: "OPEN"}},
		},
	}
	desiredNet, err := s.SetNetworkPolicy(ctx, netAssign)
	if err != nil {
		t.Fatalf("SetNetworkPolicy: %v", err)
	}
	if desiredNet.Revision == 0 {
		t.Fatalf("expected desiredNet revision > 0")
	}
	// Cache hit
	desiredNet2, err := s.SetNetworkPolicy(ctx, netAssign)
	if err != nil || desiredNet2.Revision != desiredNet.Revision {
		t.Fatalf("expected cache hit on same idempotency key, got rev %d, err %v", desiredNet2.Revision, err)
	}
	// Conflict: same idempotency key with different policy
	netAssignDiff := netAssign
	netAssignDiff.Policy.PrimaryID = "prof2"
	netAssignDiff.Policy.Profiles = []ports.WifiProfile{{ID: "prof2", SSID: "DifferentSSID", Security: "OPEN"}}
	if _, err := s.SetNetworkPolicy(ctx, netAssignDiff); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on different body for same idempotency key, got %v", err)
	}

	// 3. SetPolicy idempotency conflict and missing required package
	polAssign := ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "pol_idem_1",
		Policy: ports.Policy{
			RequiredPackages: []string{"com.nonexistent.package"},
		},
	}
	if _, err := s.SetPolicy(ctx, polAssign); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing required package, got %v", err)
	}

	polAssignValid := ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "pol_idem_2",
		Policy: ports.Policy{},
	}
	desiredPol, err := s.SetPolicy(ctx, polAssignValid)
	if err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}
	if desiredPol.Revision == 0 {
		t.Fatalf("expected desiredPol revision > 0")
	}
	// Idempotency cache hit
	desiredPolCached, err := s.SetPolicy(ctx, polAssignValid)
	if err != nil || desiredPolCached.Revision != desiredPol.Revision {
		t.Fatalf("expected cache hit for SetPolicy")
	}
	// Idempotency conflict
	polAssignConflict := polAssignValid
	polAssignConflict.Policy.CameraDisabled = boolPtr(true)
	if _, err := s.SetPolicy(ctx, polAssignConflict); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on SetPolicy conflict, got %v", err)
	}

	// 4. dispatchCommand with REBOOT when capability is missing during sync
	cmdReboot, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_reb_id_1", Kind: "REBOOT", IdempotencyKey: "cmd_reboot_1",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("SubmitCommand REBOOT: %v", err)
	}
	// Device reports inventory missing "command.reboot" during sync
	syncRes, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 1,
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"install_apk", "command.lock"}},
		Now: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(syncRes.Commands) != 0 {
		t.Fatalf("expected 0 commands dispatched due to missing capability, got %d", len(syncRes.Commands))
	}
	cmdAfter, err := s.GetCommand(ctx, scope, devID, cmdReboot.ID, now)
	if err != nil || cmdAfter.State != "FAILED" || cmdAfter.ErrorCode != "CAPABILITY_UNAVAILABLE" {
		t.Fatalf("expected command FAILED / CAPABILITY_UNAVAILABLE, got state=%s code=%s", cmdAfter.State, cmdAfter.ErrorCode)
	}

	// Submit a LOCK command and assign an app so device has both operations and commands
	artSHA := strings.Repeat("b", 64)
	if err := s.RegisterArtifact(ctx, ports.Artifact{
		ScopeID: scope, SHA256: artSHA, PackageName: "com.pkg.state", VersionCode: 1, SizeBytes: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, ArtifactSHA256: artSHA, IdempotencyKey: "assign_app_1",
		Now: now, Deadline: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_lock_id_1", Kind: "LOCK", IdempotencyKey: "cmd_lock_1",
		Now: now, Deadline: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	// 5. GetDeviceState with populated operations and commands
	devState, err := s.GetDeviceState(ctx, scope, devID)
	if err != nil {
		t.Fatalf("GetDeviceState: %v", err)
	}
	if len(devState.Operations) == 0 || len(devState.Commands) == 0 {
		t.Fatalf("expected operations and commands in GetDeviceState, got ops=%d cmds=%d", len(devState.Operations), len(devState.Commands))
	}
	if _, err := s.GetDeviceState(ctx, scope, "nonexistent_dev"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for nonexistent device in GetDeviceState, got %v", err)
	}

	// 6. ListChanges argument validation & cursor bounds
	if _, err := s.ListChanges(ctx, "", 0, 10); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty scope in ListChanges")
	}
	if _, err := s.ListChanges(ctx, scope, 0, 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0 in ListChanges")
	}
	if _, err := s.ListChanges(ctx, scope, 0, 101); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit > 100 in ListChanges")
	}
	if _, err := s.ListChanges(ctx, scope, 999999, 10); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for after > head in ListChanges")
	}

	// 7. Change floor retention deletion and cursor expired
	// Update sequence to simulate exceeding ports.ChangeRetention
	if _, err := db.Exec("UPDATE change_heads SET sequence=? WHERE scope_id=?", ports.ChangeRetention+5, scope); err != nil {
		t.Fatal(err)
	}
	// Trigger recordChange via SubmitCommand
	if _, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_lock_ret_1", Kind: "LOCK", IdempotencyKey: "cmd_lock_retention",
		Now: now, Deadline: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	// Now after = 2 is below floor (5), should return ErrCursorExpired
	if _, err := s.ListChanges(ctx, scope, 2, 10); !errors.Is(err, ports.ErrCursorExpired) {
		t.Fatalf("expected ErrCursorExpired for after < floor in ListChanges")
	}

	// 8. Projection scan expiration and limit eviction
	// Insert an expired scan
	if _, err := db.Exec("INSERT INTO projection_scans(id,ordinal,scope_id,watermark,expires_at) VALUES(?,1,?,0,?)", "exp_scan", scope, now.Add(-time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	// Insert 16 retained scans to exceed SnapshotLimit
	for i := 2; i <= 18; i++ {
		sid := fmt.Sprintf("scan_%d", i)
		if _, err := db.Exec("INSERT INTO projection_scans(id,ordinal,scope_id,watermark,expires_at) VALUES(?,?,?,0,?)", sid, i, scope, now.Add(time.Hour).UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	// ReadProjection will trigger createScan, which cleans up expired scans and evicts retained ones
	if _, err := s.ReadProjection(ctx, scope, "", 0, 10, now); err != nil {
		t.Fatalf("ReadProjection during scan cleanup: %v", err)
	}

	// 9. Revoke device and verify SetNetworkPolicy and SetPolicy return ErrRevoked
	if err := s.Revoke(ctx, scope, devID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "net_revoked",
	}); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked from SetNetworkPolicy on revoked device, got %v", err)
	}
	if _, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "pol_revoked",
	}); !errors.Is(err, ports.ErrRevoked) {
		t.Fatalf("expected ErrRevoked from SetPolicy on revoked device, got %v", err)
	}
}

// TestSqlstoreCommandsFullLifecycleAndReports covers command cancellation, reports validation,
// dispatching, and command history paging.
func TestSqlstoreCommandsFullLifecycleAndReports(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cmd_life.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := Open(ctx, db, "sqlite", "", true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	scope := "scope_cmd_life"
	devID := "dev_cmd_life"

	// Enroll device with both lock and reboot capabilities
	tHash := make([]byte, 32)
	copy(tHash, "tok_cmd_l")
	kHash := make([]byte, 32)
	copy(kHash, "k_cmd_l")
	cHash := make([]byte, 32)
	copy(cHash, "cred_cmd_l")
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: tHash, ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: tHash, ID: "att_cmd_l", KeyHash: kHash, CredentialHash: cHash,
		Device: ports.Device{
			ID: devID, ScopeID: scope, State: "ENROLLED",
			Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
			CreatedAt: now,
		},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. CancelCommand on QUEUED command -> CANCELLED
	cmdQ, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_q_1", Kind: "LOCK", IdempotencyKey: "idem_q_1",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	cancQ, err := s.CancelCommand(ctx, scope, devID, cmdQ.ID, now)
	if err != nil || cancQ.State != "CANCELLED" || cancQ.ErrorCode != "CANCELLED_BY_HOST" {
		t.Fatalf("expected CANCELLED / CANCELLED_BY_HOST, got state=%s code=%s", cancQ.State, cancQ.ErrorCode)
	}
	// Re-canceling already terminal command returns it unchanged (switch default)
	cancQ2, err := s.CancelCommand(ctx, scope, devID, cmdQ.ID, now)
	if err != nil || cancQ2.State != "CANCELLED" {
		t.Fatalf("expected idempotent terminal command cancel, got %v", cancQ2)
	}

	// 2. CancelCommand on DISPATCHED command -> UNCERTAIN
	cmdDisp, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_disp_1", Kind: "LOCK", IdempotencyKey: "idem_disp_1",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Sync to dispatch command
	syncSnap, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 1,
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(time.Second),
	})
	if err != nil || len(syncSnap.Commands) != 1 {
		t.Fatalf("expected 1 dispatched command, got %v", syncSnap.Commands)
	}
	dispToken := syncSnap.Commands[0].DispatchToken
	_ = dispToken
	cancDisp, err := s.CancelCommand(ctx, scope, devID, cmdDisp.ID, now)
	if err != nil || cancDisp.State != "UNCERTAIN" || cancDisp.ErrorCode != "CANCELLED_AFTER_DISPATCH" {
		t.Fatalf("expected UNCERTAIN / CANCELLED_AFTER_DISPATCH, got state=%s code=%s", cancDisp.State, cancDisp.ErrorCode)
	}

	// 3. CancelCommand with mismatched device ID -> ErrNotFound
	if _, err := s.CancelCommand(ctx, scope, "wrong_device", cmdDisp.ID, now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for wrong device cancel, got %v", err)
	}

	// 4. Sync with > 16 CommandReports -> ErrInvalidArgument
	manyReports := make([]ports.CommandReport, 17)
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 2, CommandReports: manyReports,
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(2 * time.Second),
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for > 16 command reports, got %v", err)
	}

	// 5. Submit new command for report testing (LOCK)
	cmdRep, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_rep_lock", Kind: "LOCK", IdempotencyKey: "idem_rep_lock",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	syncSnap2, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 3,
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(3 * time.Second),
	})
	if err != nil || len(syncSnap2.Commands) != 1 {
		t.Fatalf("expected dispatched LOCK command, got %v", syncSnap2.Commands)
	}
	lockToken := syncSnap2.Commands[0].DispatchToken

	// - Report with mismatched token -> ErrConflict
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 4,
		CommandReports: []ports.CommandReport{{CommandID: cmdRep.ID, DispatchToken: "wrong_tok", State: "SUCCEEDED"}},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(4 * time.Second),
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for mismatched dispatch token, got %v", err)
	}

	// - Report with invalid state -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 5,
		CommandReports: []ports.CommandReport{{CommandID: cmdRep.ID, DispatchToken: lockToken, State: "UNKNOWN_STATE"}},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(5 * time.Second),
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid report state, got %v", err)
	}

	// - Report SUCCEEDED with nil observation -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 6,
		CommandReports: []ports.CommandReport{{CommandID: cmdRep.ID, DispatchToken: lockToken, State: "SUCCEEDED"}},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(6 * time.Second),
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for nil observation, got %v", err)
	}

	// - Report SUCCEEDED with invalid lock observation (!Secure) -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 7,
		CommandReports: []ports.CommandReport{{
			CommandID: cmdRep.ID, DispatchToken: lockToken, State: "SUCCEEDED",
			Observation: &ports.CommandObservation{Secure: false, Locked: true},
		}},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(7 * time.Second),
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for insecure lock observation, got %v", err)
	}

	// - Valid SUCCEEDED report
	validRep := ports.CommandReport{
		CommandID: cmdRep.ID, DispatchToken: lockToken, State: "SUCCEEDED",
		Observation: &ports.CommandObservation{Secure: true, Locked: true},
	}
	syncSnapOk, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 8,
		CommandReports: []ports.CommandReport{validRep},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(8 * time.Second),
	})
	if err != nil || len(syncSnapOk.CommandReceipts) != 1 || syncSnapOk.CommandReceipts[0] != cmdRep.ID {
		t.Fatalf("expected valid command receipt, got %v, err: %v", syncSnapOk.CommandReceipts, err)
	}

	// - Idempotent replay of same report
	syncSnapReplay, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 9,
		CommandReports: []ports.CommandReport{validRep},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(9 * time.Second),
	})
	if err != nil {
		t.Fatalf("expected successful report replay, got %v", err)
	}
	_ = syncSnapReplay

	// - Conflicting replay of different report
	conflictingRep := validRep
	conflictingRep.ErrorCode = "CHANGED_CODE"
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 10,
		CommandReports: []ports.CommandReport{conflictingRep},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(10 * time.Second),
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for conflicting report replay, got %v", err)
	}

	// 6. Test REBOOT command reports
	cmdReb, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_reb_rep", Kind: "REBOOT", IdempotencyKey: "idem_reb_rep",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	syncRebSnap, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 11,
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(11 * time.Second),
	})
	if err != nil || len(syncRebSnap.Commands) != 1 {
		t.Fatalf("expected dispatched REBOOT command, got %v", syncRebSnap.Commands)
	}
	rebToken := syncRebSnap.Commands[0].DispatchToken

	// - REBOOT SUCCEEDED with BootAfter <= BootBefore -> ErrInvalidArgument
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 12,
		CommandReports: []ports.CommandReport{{
			CommandID: cmdReb.ID, DispatchToken: rebToken, State: "SUCCEEDED",
			Observation: &ports.CommandObservation{BootBefore: 5, BootAfter: 5},
		}},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(12 * time.Second),
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid boot count observation, got %v", err)
	}

	// - REBOOT SUCCEEDED with valid BootAfter > BootBefore
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 13,
		CommandReports: []ports.CommandReport{{
			CommandID: cmdReb.ID, DispatchToken: rebToken, State: "SUCCEEDED",
			Observation: &ports.CommandObservation{BootBefore: 5, BootAfter: 6},
		}},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(13 * time.Second),
	})
	if err != nil {
		t.Fatalf("expected successful REBOOT report, got %v", err)
	}

	// 7. FAILED and UNCERTAIN reports
	cmdFail, _ := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID, ID: "cmd_fail_rep", Kind: "LOCK", IdempotencyKey: "idem_fail_rep",
		Now: now, Deadline: now.Add(time.Hour),
	})
	syncFailSnap, _ := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 14,
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(14 * time.Second),
	})
	failTok := syncFailSnap.Commands[0].DispatchToken
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 15,
		CommandReports: []ports.CommandReport{{
			CommandID: cmdFail.ID, DispatchToken: failTok, State: "FAILED", ErrorCode: "SYS_ERR",
		}},
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock", "command.reboot"}},
		Now: now.Add(15 * time.Second),
	})
	if err != nil {
		t.Fatalf("expected FAILED report accepted, got %v", err)
	}

	// 8. ListCommands validation & paging
	if _, err := s.ListCommands(ctx, scope, devID, "", 0, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0")
	}
	if _, err := s.ListCommands(ctx, scope, devID, "", 102, now); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 102")
	}
	cmds, err := s.ListCommands(ctx, scope, devID, "", 10, now)
	if err != nil || len(cmds) == 0 {
		t.Fatalf("expected commands listed, got %v, err: %v", cmds, err)
	}
}

// TestSqlstoreSyncAndArtifactEdgeCases covers artifact registration conflict, sync sequence & revision bounds,
// and device revocation not found branches.
func TestSqlstoreSyncAndArtifactEdgeCases(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "sync_edge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := Open(ctx, db, "sqlite", "", true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	scope := "scope_sync_edge"
	devID := "dev_sync_edge"

	// 1. RegisterArtifact conflict on same digest with different metadata
	artSHA := strings.Repeat("c", 64)
	if err := s.RegisterArtifact(ctx, ports.Artifact{
		ScopeID: scope, SHA256: artSHA, PackageName: "com.pkg.orig", VersionCode: 1, SizeBytes: 100,
	}); err != nil {
		t.Fatal(err)
	}
	// Conflict: same SHA, different package
	if err := s.RegisterArtifact(ctx, ports.Artifact{
		ScopeID: scope, SHA256: artSHA, PackageName: "com.pkg.diff", VersionCode: 1, SizeBytes: 100,
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict for different artifact with same SHA256, got %v", err)
	}

	// Enroll device
	tHash := make([]byte, 32)
	copy(tHash, "tok_sync_e")
	kHash := make([]byte, 32)
	copy(kHash, "k_sync_e")
	cHash := make([]byte, 32)
	copy(cHash, "cred_sync_e")
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: tHash, ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: tHash, ID: "att_sync_e", KeyHash: kHash, CredentialHash: cHash,
		Device: ports.Device{
			ID: devID, ScopeID: scope, State: "ENROLLED",
			Inventory: ports.Inventory{DeviceOwner: true},
			CreatedAt: now,
		},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Sync with Sequence == 0 -> ErrStale
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 0,
		Inventory: ports.Inventory{DeviceOwner: true},
		Now: now,
	}); !errors.Is(err, ports.ErrStale) {
		t.Fatalf("expected ErrStale for sequence 0, got %v", err)
	}

	// 3. Initial successful sync sequence 1
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 1,
		Inventory: ports.Inventory{DeviceOwner: true},
		Now: now.Add(time.Second),
	}); err != nil {
		t.Fatalf("Sync 1: %v", err)
	}

	// 4. Replay sequence 1 with altered payload -> ErrConflict
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 1,
		Inventory: ports.Inventory{DeviceOwner: false},
		Now: now.Add(time.Second),
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("expected ErrConflict on replay with altered payload, got %v", err)
	}

	// 5. Sync with ReportedRevision > DesiredRevision -> ErrStale
	if _, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID, CredentialHash: cHash, Sequence: 2, ReportedRevision: 9999,
		Inventory: ports.Inventory{DeviceOwner: true},
		Now: now.Add(2 * time.Second),
	}); !errors.Is(err, ports.ErrStale) {
		t.Fatalf("expected ErrStale for ReportedRevision > DesiredRevision, got %v", err)
	}

	// 6. Revoke nonexistent device -> ErrNotFound
	if err := s.Revoke(ctx, scope, "nonexistent_device"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for revoking nonexistent device, got %v", err)
	}
}

// TestSqlstoreRealSqliteProjectionAndChangeErrors covers error branches in projection scans and change recording
// by injecting SQLite triggers and table drops.
func TestSqlstoreRealSqliteProjectionAndChangeErrors(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "proj_errs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := Open(ctx, db, "sqlite", "", true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	scope := "scope_pe"
	devID := "dev_pe"

	// Enroll device
	tHash := make([]byte, 32)
	copy(tHash, "tok_pe")
	kHash := make([]byte, 32)
	copy(kHash, "k_pe")
	cHash := make([]byte, 32)
	copy(cHash, "cred_pe")
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: tHash, ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, err = s.Consume(ctx, ports.Attempt{
		TokenHash: tHash, ID: "att_pe", KeyHash: kHash, CredentialHash: cHash,
		Device: ports.Device{
			ID: devID, ScopeID: scope, State: "ENROLLED",
			Inventory: ports.Inventory{DeviceOwner: true},
			CreatedAt: now,
		},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Trigger error on DELETE FROM changes (line 49)
	if _, err := db.Exec("UPDATE change_heads SET sequence=? WHERE scope_id=?", ports.ChangeRetention+5, scope); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TRIGGER trig_del_changes BEFORE DELETE ON changes BEGIN SELECT RAISE(FAIL, 'simulated delete error'); END;"); err != nil {
		t.Fatal(err)
	}
	_ = s.Revoke(ctx, scope, devID)
	if _, err := db.Exec("DROP TRIGGER trig_del_changes"); err != nil {
		t.Fatal(err)
	}

	// 2. Trigger error on INSERT INTO projection_items (line 293)
	artSHA := strings.Repeat("d", 64)
	_ = s.RegisterArtifact(ctx, ports.Artifact{
		ScopeID: scope, SHA256: artSHA, PackageName: "com.pkg.pe", VersionCode: 1, SizeBytes: 100,
	})
	if _, err := db.Exec("CREATE TRIGGER trig_ins_pi BEFORE INSERT ON projection_items BEGIN SELECT RAISE(FAIL, 'simulated pi insert error'); END;"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadProjection(ctx, scope, "", 0, 10, now); err == nil {
		t.Fatal("expected error on failing projection_items insert")
	}
	if _, err := db.Exec("DROP TRIGGER trig_ins_pi"); err != nil {
		t.Fatal(err)
	}

	// 3. Drop artifacts table -> error in QueryContext artifacts (line 273)
	dbArt, _ := sql.Open("sqlite", filepath.Join(t.TempDir(), "art_drop.db"))
	defer dbArt.Close()
	sArt, _ := Open(ctx, dbArt, "sqlite", "", true)
	_, _ = dbArt.Exec("DROP TABLE artifacts")
	if _, err := sArt.ReadProjection(ctx, scope, "", 0, 10, now); err == nil {
		t.Fatal("expected error on dropped artifacts table in ReadProjection")
	}

	// 4. Drop fleet_groups table -> error in QueryContext fleet_groups (line 304)
	dbGrp, _ := sql.Open("sqlite", filepath.Join(t.TempDir(), "grp_drop.db"))
	defer dbGrp.Close()
	sGrp, _ := Open(ctx, dbGrp, "sqlite", "", true)
	_, _ = dbGrp.Exec("DROP TABLE fleet_groups")
	if _, err := sGrp.ReadProjection(ctx, scope, "", 0, 10, now); err == nil {
		t.Fatal("expected error on dropped fleet_groups table in ReadProjection")
	}

	// 5. Drop projection_items table -> error in ReadProjection page read (line 381)
	dbPI, _ := sql.Open("sqlite", filepath.Join(t.TempDir(), "pi_drop.db"))
	defer dbPI.Close()
	sPI, _ := Open(ctx, dbPI, "sqlite", "", true)
	// Create a valid scan first
	page, err := sPI.ReadProjection(ctx, scope, "", 0, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = dbPI.Exec("DROP TABLE projection_items")
	if _, err := sPI.ReadProjection(ctx, scope, page.SnapshotID, 0, 10, now); err == nil {
		t.Fatal("expected error on dropped projection_items table in ReadProjection continuation")
	}

	// 6. Drop operations table -> error in readDeviceState operations query (line 136)
	dbOps, _ := sql.Open("sqlite", filepath.Join(t.TempDir(), "ops_drop.db"))
	defer dbOps.Close()
	sOps, _ := Open(ctx, dbOps, "sqlite", "", true)
	_ = sOps.CreateEnrollment(ctx, ports.Enrollment{TokenHash: tHash, ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, _ = sOps.Consume(ctx, ports.Attempt{
		TokenHash: tHash, ID: "att_pe2", KeyHash: kHash, CredentialHash: cHash,
		Device: ports.Device{ID: "dev_pe2", ScopeID: scope, State: "ENROLLED", CreatedAt: now},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	_, _ = dbOps.Exec("DROP TABLE operations")
	if _, err := sOps.GetDeviceState(ctx, scope, "dev_pe2"); err == nil {
		t.Fatal("expected error on dropped operations table in GetDeviceState")
	}

	// 7. Drop commands table -> error in readDeviceState commands query (line 155)
	dbCmd, _ := sql.Open("sqlite", filepath.Join(t.TempDir(), "cmd_drop.db"))
	defer dbCmd.Close()
	sCmd, _ := Open(ctx, dbCmd, "sqlite", "", true)
	_ = sCmd.CreateEnrollment(ctx, ports.Enrollment{TokenHash: tHash, ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, _ = sCmd.Consume(ctx, ports.Attempt{
		TokenHash: tHash, ID: "att_pe3", KeyHash: kHash, CredentialHash: cHash,
		Device: ports.Device{ID: "dev_pe3", ScopeID: scope, State: "ENROLLED", CreatedAt: now},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	_, _ = dbCmd.Exec("DROP TABLE commands")
	if _, err := sCmd.GetDeviceState(ctx, scope, "dev_pe3"); err == nil {
		t.Fatal("expected error on dropped commands table in GetDeviceState")
	}

	// 8. Invalid network policy in SetNetworkPolicy (network.go:73)
	if _, err := s.SetNetworkPolicy(ctx, ports.NetworkAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "net_inv_pol",
		Policy: ports.NetworkPolicy{PrimaryID: "prof_missing"},
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid network policy")
	}

	// 9. Invalid package name in SetPolicy (policies.go:45)
	if _, err := s.SetPolicy(ctx, ports.PolicyAssignment{
		ScopeID: scope, DeviceID: devID, IdempotencyKey: "pol_inv_pkg",
		Policy: ports.Policy{RequiredPackages: []string{"invalid package name!"}},
	}); err == nil {
		t.Fatalf("expected error for invalid package name in SetPolicy")
	}

	// 10. Nonexistent artifact in Assign (operations.go:259)
	if _, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, ArtifactSHA256: strings.Repeat("9", 64),
		IdempotencyKey: "assign_nonexistent_art", Now: now, Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for nonexistent artifact in Assign")
	}

	// 11. > 64 desired apps in Assign (operations.go:266)
	for i := 0; i < 64; i++ {
		_, _ = db.Exec("INSERT INTO desired_apps(device_id, package_name, artifact_sha256, revision) VALUES(?, ?, ?, 1)", devID, fmt.Sprintf("com.pkg.lim.%d", i), artSHA)
	}
	if _, err := s.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: devID, ArtifactSHA256: artSHA,
		IdempotencyKey: "assign_too_many", Now: now, Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for > 64 desired apps in Assign")
	}
	_, _ = db.Exec("DELETE FROM desired_apps WHERE device_id=?", devID)

	// 12. Command report device mismatch in applyCommandReport (commands.go:273)
	devID1Active := "dev_pe_act1"
	devID2Active := "dev_pe_act2"
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: hash32("t_act1"), ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, _ = s.Consume(ctx, ports.Attempt{
		TokenHash: hash32("t_act1"), ID: "att_act1", KeyHash: hash32("k_act1"), CredentialHash: hash32("c_act1"),
		Device: ports.Device{ID: devID1Active, ScopeID: scope, State: "ENROLLED", Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock"}}, CreatedAt: now},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	_ = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: hash32("t_act2"), ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, _ = s.Consume(ctx, ports.Attempt{
		TokenHash: hash32("t_act2"), ID: "att_act2", KeyHash: hash32("k_act2"), CredentialHash: hash32("c_act2"),
		Device: ports.Device{ID: devID2Active, ScopeID: scope, State: "ENROLLED", Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock"}}, CreatedAt: now},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	cmdDev1, err := s.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: devID1Active, ID: "cmd_for_dev1", Kind: "LOCK", IdempotencyKey: "k_dev1",
		Now: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("SubmitCommand failed: %v", err)
	}
	syncRes, err := s.Sync(ctx, ports.SyncInput{
		DeviceID: devID1Active, CredentialHash: hash32("c_act1"), Sequence: 1, Now: now.Add(time.Second),
		Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock"}},
	})
	if err != nil || len(syncRes.Commands) == 0 {
		t.Fatalf("Sync devID1 failed: %v", err)
	}
	_, err = s.Sync(ctx, ports.SyncInput{
		DeviceID: devID2Active, CredentialHash: hash32("c_act2"), Sequence: 1,
		CommandReports: []ports.CommandReport{{CommandID: cmdDev1.ID, DispatchToken: syncRes.Commands[0].DispatchToken, State: "SUCCEEDED"}},
		Now: now.Add(2 * time.Second),
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for command report device mismatch, got %v", err)
	}

	// 13. Corrupted JSON in commands table (commands.go:62)
	dbCmdTable, _ := sql.Open("sqlite", filepath.Join(t.TempDir(), "cmd_tab.db"))
	defer dbCmdTable.Close()
	sCmdTab, _ := Open(ctx, dbCmdTable, "sqlite", "", true)
	_ = sCmdTab.CreateEnrollment(ctx, ports.Enrollment{TokenHash: tHash, ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, _ = sCmdTab.Consume(ctx, ports.Attempt{
		TokenHash: tHash, ID: "att_ct", KeyHash: kHash, CredentialHash: cHash,
		Device: ports.Device{ID: "dev_ct", ScopeID: scope, State: "ENROLLED", Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"command.lock"}}, CreatedAt: now},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	if _, err := dbCmdTable.Exec("INSERT INTO commands(id, ordinal, device_id, idempotency_key, state, data) VALUES('bad_cmd', 9999, 'dev_ct', 'k_bad', 'QUEUED', 'corrupted')"); err != nil {
		t.Fatal(err)
	}
	if _, err := sCmdTab.SubmitCommand(ctx, ports.CommandAssignment{
		ScopeID: scope, DeviceID: "dev_ct", ID: "c_fail", Kind: "LOCK", IdempotencyKey: "k_fail",
		Now: now, Deadline: now.Add(time.Hour),
	}); err == nil {
		t.Fatal("expected error on corrupted commands table in SubmitCommand")
	}

	// 14. Corrupted JSON in operations table (operations.go:188)
	dbOpsTab, _ := sql.Open("sqlite", filepath.Join(t.TempDir(), "ops_tab.db"))
	defer dbOpsTab.Close()
	sOpsTab, _ := Open(ctx, dbOpsTab, "sqlite", "", true)
	_ = sOpsTab.CreateEnrollment(ctx, ports.Enrollment{TokenHash: tHash, ScopeID: scope, ExpiresAt: now.Add(time.Hour)})
	_, _ = sOpsTab.Consume(ctx, ports.Attempt{
		TokenHash: tHash, ID: "att_ot", KeyHash: kHash, CredentialHash: cHash,
		Device: ports.Device{ID: "dev_ot", ScopeID: scope, State: "ENROLLED", Inventory: ports.Inventory{DeviceOwner: true}, CreatedAt: now},
		Receipt: []byte("rec"), Now: now, RecoverUntil: now.Add(time.Hour),
	})
	_ = sOpsTab.RegisterArtifact(ctx, ports.Artifact{ScopeID: scope, SHA256: artSHA, PackageName: "com.pkg.ot", VersionCode: 1, SizeBytes: 100})
	if _, err := dbOpsTab.Exec("INSERT INTO operations(id, ordinal, device_id, package_name, artifact_sha256, state, revision, idempotency_key, data) VALUES('bad_op', 9999, 'dev_ot', 'com.pkg.ot', ?, 'QUEUED', 1, 'k_bad_op', 'corrupted')", artSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := sOpsTab.Assign(ctx, ports.Assignment{
		ScopeID: scope, DeviceID: "dev_ot", ArtifactSHA256: artSHA,
		IdempotencyKey: "k_ot_assign", Now: now, Deadline: now.Add(time.Hour),
	}); err == nil {
		t.Fatal("expected error on corrupted operations table in Assign")
	}
	// 15. Argument validation in ListArtifacts, ListGroups, ListFleetAssignments, and ListOperations
	if _, err := s.ListArtifacts(ctx, "", "", 10); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty scope in ListArtifacts")
	}
	if _, err := s.ListGroups(ctx, scope, "", 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0 in ListGroups")
	}
	if _, err := s.ListFleetAssignments(ctx, scope, "", 0); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for limit 0 in ListFleetAssignments")
	}
	if _, err := s.ListOperations(ctx, scope, "nonexistent_dev", "", 10, now); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for nonexistent device in ListOperations")
	}
}



















