// SPDX-License-Identifier: LGPL-3.0-only
package postgres_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/thienvu18/kylok/go/adapters/postgres"
	"github.com/thienvu18/kylok/go/ports"
	"github.com/thienvu18/kylok/go/ports/portstest"
)

var schemas atomic.Uint64

func pool(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("KYLOK_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("run make test-postgres for real backend tests")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(12)
	t.Cleanup(func() { db.Close() })
	if err = db.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func schema(t *testing.T, db *sql.DB) string {
	t.Helper()
	name := fmt.Sprintf("kylok_test_%d_%d", os.Getpid(), schemas.Add(1))
	t.Cleanup(func() {
		if _, err := db.Exec(`DROP SCHEMA IF EXISTS "` + name + `" CASCADE`); err != nil {
			t.Error(err)
		}
	})
	return name
}

func store(t *testing.T, db *sql.DB, name string, initialize bool) *postgres.Store {
	t.Helper()
	result, err := postgres.Open(context.Background(), db, postgres.Options{Schema: name, Initialize: initialize})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestRealBackendConformance(t *testing.T) {
	db := pool(t)
	factory := func(t *testing.T) ports.StateStore { return store(t, db, schema(t, db), true) }
	portstest.State(t, factory)
	portstest.Operations(t, factory)
	portstest.Policies(t, factory)
	portstest.ManagedConfigurations(t, factory)
	portstest.Management(t, factory)
	portstest.Changes(t, factory)
}

func TestSchemaOwnershipAndValidation(t *testing.T) {
	db := pool(t)
	name := schema(t, db)
	ctx := context.Background()
	for _, input := range []postgres.Options{{Schema: name}, {Schema: "unsafe;drop"}, {Schema: strings.Repeat("s", 64)}} {
		if _, err := postgres.Open(ctx, db, input); err == nil {
			t.Fatal("accepted absent/invalid schema", input)
		}
		if err := db.PingContext(ctx); err != nil {
			t.Fatal("constructor closed supplied pool", err)
		}
	}
	store(t, db, name, true)
	store(t, db, name, false)
	for _, damage := range []string{`ALTER TABLE "` + name + `".devices DROP COLUMN credential_hash`, `UPDATE "` + name + `".schema_version SET version=999`} {
		if _, err := db.Exec(damage); err != nil {
			t.Fatal(err)
		}
		if _, err := postgres.Open(ctx, db, postgres.Options{Schema: name}); err == nil {
			t.Fatal("accepted damaged schema")
		}
		if err := db.PingContext(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConcurrentInstancesClaimsSyncAssignmentsAndRevocation(t *testing.T) {
	db := pool(t)
	other := pool(t)
	name := schema(t, db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	a := store(t, db, name, true)
	b := store(t, other, name, false)
	token := sha256.Sum256([]byte("token"))
	key := sha256.Sum256([]byte("key"))
	credential := sha256.Sum256([]byte("credential"))
	if err := a.CreateEnrollment(ctx, ports.Enrollment{TokenHash: token[:], ScopeID: "scope", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	attempt := ports.Attempt{TokenHash: token[:], ID: "one", KeyHash: key[:], Device: ports.Device{ID: "device", ScopeID: "scope"}, CredentialHash: credential[:], Receipt: []byte("encrypted"), Now: now, RecoverUntil: now.Add(time.Hour)}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := a
			if i%2 != 0 {
				s = b
			}
			candidate := attempt
			if i%3 == 0 {
				candidate.ID = "competitor"
			}
			receipt, err := s.Consume(ctx, candidate)
			if err != nil && !errors.Is(err, ports.ErrConsumed) {
				t.Error(err)
			}
			if err == nil && !bytes.Equal(receipt, attempt.Receipt) {
				t.Error("receipt mismatch")
			}
		}(i)
	}
	wg.Wait()
	devices, err := a.ListDevices(ctx, "scope", "", 100)
	if err != nil || len(devices) != 1 {
		t.Fatal(devices, err)
	}
	artifact := ports.Artifact{ScopeID: "scope", SHA256: "digest", PackageName: "app.example", VersionCode: 1, SigningCertificates: []string{"signer"}}
	if err = a.RegisterArtifact(ctx, artifact); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := a
			if i%2 != 0 {
				s = b
			}
			_, err := s.Assign(ctx, ports.Assignment{ScopeID: "scope", DeviceID: "device", ArtifactSHA256: "digest", IdempotencyKey: "request", OperationID: fmt.Sprint("operation", i), Now: now, Deadline: now.Add(time.Hour)})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	operations, err := a.ListOperations(ctx, "scope", "device", "", 100, now)
	if err != nil || len(operations) != 1 {
		t.Fatal(operations, err)
	}
	input := ports.SyncInput{DeviceID: "device", CredentialHash: credential[:], Sequence: 1, Now: now, Inventory: ports.Inventory{DeviceOwner: true, Capabilities: []string{"install_apk"}}}
	results := make(chan ports.Snapshot, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := a
			if i%2 != 0 {
				s = b
			}
			result, err := s.Sync(ctx, input)
			if err != nil {
				t.Error(err)
				return
			}
			results <- result
		}(i)
	}
	wg.Wait()
	close(results)
	lease := ""
	for result := range results {
		if len(result.Operations) != 1 {
			t.Fatal(result)
		}
		if lease == "" {
			lease = result.Operations[0].LeaseID
		}
		if lease != result.Operations[0].LeaseID || result.Operations[0].Attempts != 1 {
			t.Fatal("duplicate lease/effect", result)
		}
	}
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				if err := a.Revoke(ctx, "scope", "device"); err != nil {
					t.Error(err)
				}
			} else {
				_, err := b.Sync(ctx, input)
				if err != nil && !errors.Is(err, ports.ErrRevoked) {
					t.Error(err)
				}
			}
		}(i)
	}
	wg.Wait()
	if _, err = b.Sync(ctx, input); !errors.Is(err, ports.ErrRevoked) {
		t.Fatal("revocation lost", err)
	}
	if _, err = b.Consume(ctx, attempt); !errors.Is(err, ports.ErrRevoked) && !errors.Is(err, ports.ErrConsumed) {
		t.Fatal("revoked recovery", err)
	}
}

func TestConnectionCrashRollbackAndServerRestart(t *testing.T) {
	db := pool(t)
	name := schema(t, db)
	ctx := context.Background()
	s := store(t, db, name, true)
	// Kill a transaction after an uncommitted mutation; the committed SDK state must survive unchanged.
	now := time.Now().UTC().Truncate(time.Millisecond)
	token := sha256.Sum256([]byte("restart-token"))
	if err := s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: token[:], ScopeID: "scope", ExpiresAt: now.Add(time.Hour), CorrelationID: "durable"}); err != nil {
		t.Fatal(err)
	}
	key := sha256.Sum256([]byte("restart-key"))
	credential := sha256.Sum256([]byte("restart-credential"))
	if _, err := s.Consume(ctx, ports.Attempt{TokenHash: token[:], ID: "restart-attempt", KeyHash: key[:], Device: ports.Device{ID: "restart-device", ScopeID: "scope"}, CredentialHash: credential[:], Receipt: []byte("encrypted"), Now: now, RecoverUntil: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterArtifact(ctx, ports.Artifact{ScopeID: "scope", SHA256: "snapshot-artifact"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.ReadProjection(ctx, "scope", "", 0, 1, now)
	if err != nil || !snapshot.More {
		t.Fatal(snapshot, err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var pid int
	if err = tx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE "`+name+`".enrollments SET correlation_id='uncommitted'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "SELECT pg_terminate_backend($1)", pid); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err == nil {
		t.Fatal("killed transaction committed")
	}
	value, err := s.Enrollment(ctx, token[:])
	if err != nil || value.CorrelationID != "durable" {
		t.Fatal(value, err)
	}
	container := os.Getenv("KYLOK_POSTGRES_CONTAINER")
	if container == "" {
		t.Fatal("restart test requires the fixture-owned PostgreSQL container")
	}
	if output, err := exec.Command("docker", "restart", "--signal", "SIGKILL", container).CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		check, cancel := context.WithTimeout(ctx, time.Second)
		err = db.PingContext(check)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	recovered := store(t, db, name, false)
	value, err = recovered.Enrollment(ctx, token[:])
	if err != nil || value.CorrelationID != "durable" {
		t.Fatal("crash lost committed state", value, err)
	}
	continued, err := recovered.ReadProjection(ctx, "scope", snapshot.SnapshotID, snapshot.After, 100, now)
	if err != nil || len(continued.Items) != 1 || continued.Watermark != snapshot.Watermark {
		t.Fatal("snapshot lost across server restart", continued, err)
	}
	changes, err := recovered.ListChanges(ctx, "scope", 0, 100)
	if err != nil || changes.Head != 2 || len(changes.Items) != 2 {
		t.Fatal("change checkpoint lost across server restart", changes, err)
	}
}

func TestRuntimePrincipalNeedsNoSchemaCreationPrivileges(t *testing.T) {
	admin := pool(t)
	name := schema(t, admin)
	ctx := context.Background()
	store(t, admin, name, true)
	role := name + "_runtime"
	if _, err := admin.Exec(`CREATE ROLE "` + role + `" NOLOGIN`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec(`DROP OWNED BY "` + role + `"`)
		if _, err := admin.Exec(`DROP ROLE "` + role + `"`); err != nil {
			t.Error(err)
		}
	})
	if _, err := admin.Exec(`GRANT USAGE ON SCHEMA "` + name + `" TO "` + role + `"; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA "` + name + `" TO "` + role + `"`); err != nil {
		t.Fatal(err)
	}
	config, err := pgx.ParseConfig(os.Getenv("KYLOK_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["role"] = role
	runtime := stdlib.OpenDB(*config)
	t.Cleanup(func() { runtime.Close() })
	s := store(t, runtime, name, false)
	token := sha256.Sum256([]byte("runtime-token"))
	if err = s.CreateEnrollment(ctx, ports.Enrollment{TokenHash: token[:], ScopeID: "scope", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.Exec(`CREATE TABLE "` + name + `".forbidden(id BIGINT)`); err == nil {
		t.Fatal("runtime principal unexpectedly has CREATE privilege")
	}
	if err = runtime.PingContext(ctx); err != nil {
		t.Fatal("adapter closed runtime pool", err)
	}
}
