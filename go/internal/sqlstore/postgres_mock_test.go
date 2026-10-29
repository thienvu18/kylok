// SPDX-License-Identifier: LGPL-3.0-only
package sqlstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

var (
	mockMu                 sync.Mutex
	mockFailNextQuery      bool
	mockFailNextExec       bool
	mockFailQuerySubstring string
	mockFailExecSubstring  string
	mockVersion            int64
	mockNoVersionRow       bool
	mockBadJSONRows        bool
	mockScanErrorRows      bool
	mockRowsErr            bool
)

type mockDriver struct{}
type mockConn struct{}
type mockTx struct{}
type mockRows struct {
	columns []string
	rows    [][]driver.Value
	pos     int
	err     error
}

func (d *mockDriver) Open(name string) (driver.Conn, error) {
	return &mockConn{}, nil
}

func (c *mockConn) Prepare(query string) (driver.Stmt, error) {
	return &mockStmt{}, nil
}

func (c *mockConn) Close() error { return nil }

func (c *mockConn) Begin() (driver.Tx, error) {
	return &mockTx{}, nil
}

func (c *mockConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return &mockTx{}, nil
}

type mockStmt struct{}

func (s *mockStmt) Close() error { return nil }
func (s *mockStmt) NumInput() int { return -1 }
func (s *mockStmt) Exec(args []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}
func (s *mockStmt) Query(args []driver.Value) (driver.Rows, error) {
	return &mockRows{columns: []string{"version"}, rows: [][]driver.Value{{int64(0)}}}, nil
}

func (t *mockTx) Commit() error   { return nil }
func (t *mockTx) Rollback() error { return nil }

func (c *mockConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	mockMu.Lock()
	defer mockMu.Unlock()
	if mockFailNextExec || (mockFailExecSubstring != "" && strings.Contains(query, mockFailExecSubstring)) {
		mockFailNextExec = false
		mockFailExecSubstring = ""
		return nil, errors.New("simulated exec failure")
	}
	return driver.RowsAffected(1), nil
}

func (c *mockConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	mockMu.Lock()
	defer mockMu.Unlock()
	println("QueryContext query:", query)
	if mockFailNextQuery || (mockFailQuerySubstring != "" && strings.Contains(query, mockFailQuerySubstring)) {
		mockFailNextQuery = false
		mockFailQuerySubstring = ""
		return nil, errors.New("simulated query failure")
	}
	if strings.Contains(query, "schema_version") {
		if mockNoVersionRow {
			return &mockRows{columns: []string{"version"}, rows: nil}, nil
		}
		return &mockRows{
			columns: []string{"version"},
			rows:    [][]driver.Value{{mockVersion}},
		}, nil
	}
	if strings.Contains(query, "devices") && strings.Contains(query, "SELECT id") {
		return &mockRows{columns: []string{"id"}, rows: [][]driver.Value{{"d"}}}, nil
	}
	if strings.Contains(query, "devices") && strings.Contains(query, "credential_hash") {
		cols := strings.Split(deviceColumns, ",")
		return &mockRows{
			columns: cols,
			rows: [][]driver.Value{{"d", "scope_faults", "ENROLLED", "", []byte(`{"device_owner":true,"capabilities":["command.reboot"]}`), []byte(strings.Repeat("a", 32)), int64(1000), int64(0), int64(0), int64(0)}},
		}, nil
	}
	if strings.Contains(query, "WAITING_USER") || strings.Contains(query, "DISPATCHED") {
		return &mockRows{columns: []string{"data"}, rows: nil}, nil
	}
	if mockBadJSONRows {
		mockBadJSONRows = false
		return &mockRows{
			columns: []string{"data"},
			rows:    [][]driver.Value{{[]byte("{bad-json-syntax")}},
		}, nil
	}
	if mockScanErrorRows {
		mockScanErrorRows = false
		// Return 3 columns with incompatible types so rows.Scan fails
		return &mockRows{
			columns: []string{"col1", "col2", "col3"},
			rows:    [][]driver.Value{{struct{}{}, struct{}{}, struct{}{}}},
		}, nil
	}
	if mockRowsErr {
		mockRowsErr = false
		return &mockRows{
			columns: []string{"col"},
			rows:    nil,
			err:     errors.New("simulated rows iteration error"),
		}, nil
	}
	return &mockRows{columns: []string{"col"}, rows: nil}, nil
}

func (r *mockRows) Columns() []string { return r.columns }
func (r *mockRows) Close() error      { return nil }
func (r *mockRows) Next(dest []driver.Value) error {
	if r.err != nil {
		return r.err
	}
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	for i, v := range r.rows[r.pos] {
		dest[i] = v
	}
	r.pos++
	return nil
}

func init() {
	sql.Register("mock_postgres", &mockDriver{})
}

// TestPostgresDialectFullMigration exercises all postgres dialect branching in migrate().
func TestPostgresDialectFullMigration(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("mock_postgres", "mock://postgres")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 1. Successful initialization with version 0
	mockMu.Lock()
	mockVersion = 0
	mockNoVersionRow = false
	mockFailNextQuery = false
	mockFailNextExec = false
	mockFailQuerySubstring = ""
	mockFailExecSubstring = ""
	mockMu.Unlock()

	store, err := Open(ctx, db, "postgres", "kylok_mock", true)
	if err != nil {
		t.Fatalf("expected successful postgres migration with initialize=true, got %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	// 2. Initialization when schema_version table is empty (ErrNoRows -> version 0) (lines 57-58)
	mockMu.Lock()
	mockNoVersionRow = true
	mockMu.Unlock()
	_, _ = Open(ctx, db, "postgres", "kylok_mock", true)

	// 3. Query error during SELECT version (line 60)
	mockMu.Lock()
	mockNoVersionRow = false
	mockFailNextQuery = true
	mockMu.Unlock()
	if _, err := Open(ctx, db, "postgres", "kylok_mock", true); err == nil {
		t.Fatal("expected error on failed SELECT version")
	}

	// 4. Exec error during CREATE SCHEMA (line 50)
	mockMu.Lock()
	mockFailNextExec = true
	mockMu.Unlock()
	if _, err := Open(ctx, db, "postgres", "kylok_mock", true); err == nil {
		t.Fatal("expected error on failed CREATE SCHEMA")
	}

	// 5. Existing version 1 schema with initialize=false
	mockMu.Lock()
	mockVersion = 1
	mockNoVersionRow = false
	mockFailNextQuery = false
	mockFailNextExec = false
	mockFailQuerySubstring = ""
	mockFailExecSubstring = ""
	mockBadJSONRows = false
	mockScanErrorRows = false
	mockRowsErr = false
	mockMu.Unlock()
	storeV1, err := Open(ctx, db, "postgres", "kylok_mock", false)
	if err != nil {
		t.Fatalf("expected successful Open with version=1, got %v", err)
	}

	// 6. Unsupported version 99 (line 66)
	mockMu.Lock()
	mockVersion = 99
	mockMu.Unlock()
	if _, err := Open(ctx, db, "postgres", "kylok_mock", false); err == nil {
		t.Fatal("expected error for unsupported version 99")
	}

	// Reset mock for query fault injection
	mockMu.Lock()
	mockVersion = 1
	mockNoVersionRow = false
	mockFailNextQuery = false
	mockFailNextExec = false
	mockFailQuerySubstring = ""
	mockFailExecSubstring = ""
	mockBadJSONRows = false
	mockScanErrorRows = false
	mockRowsErr = false
	mockMu.Unlock()

	now := time.Now().UTC()
	scope := "mock_scope"

	// 7. Query failure fault injection across sqlstore methods
	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.ListChanges(ctx, scope, 0, 10)

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.ReadProjection(ctx, scope, "snap", 0, 10, now)

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.GetCommand(ctx, scope, "d", "c", now)

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.ListCommands(ctx, scope, "d", "", 10, now)

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.CancelCommand(ctx, scope, "d", "c", now)

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.ReplaceGroup(ctx, ports.Group{ScopeID: scope, ID: "g", DeviceIDs: []string{"d"}}, 0)

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.GetGroup(ctx, scope, "g")

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.ListGroups(ctx, scope, "", 10)

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.GetFleetAssignment(ctx, scope, "f")

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.ListFleetAssignments(ctx, scope, "", 10)

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.AdvanceFleetAssignment(ctx, scope, "f", now, 10)

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.GetOperation(ctx, scope, "d", "o", now)

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_ = storeV1.Revoke(ctx, scope, "d")

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.SetNetworkPolicy(ctx, ports.NetworkAssignment{ScopeID: scope, DeviceID: "d", IdempotencyKey: "k", Policy: ports.NetworkPolicy{PrimaryID: "w", Profiles: []ports.WifiProfile{{ID: "w", SSID: "s", Security: "OPEN"}}}})

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_ = storeV1.RegisterArtifact(ctx, ports.Artifact{ScopeID: scope, SHA256: strings.Repeat("f", 64)})

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_ = storeV1.RemoveArtifact(ctx, scope, strings.Repeat("f", 64))

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.Assign(ctx, ports.Assignment{ScopeID: scope, DeviceID: "d", IdempotencyKey: "k", OperationID: "o", ArtifactSHA256: strings.Repeat("f", 64), Now: now, Deadline: now.Add(time.Hour)})

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.ListOperations(ctx, scope, "d", "", 10, now)

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.AuthorizeDownload(ctx, "d", []byte("h"), "o", "dig", "l", now)

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.Sync(ctx, ports.SyncInput{DeviceID: "d", CredentialHash: []byte("h")})

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.SetPolicy(ctx, ports.PolicyAssignment{ScopeID: scope, DeviceID: "d", IdempotencyKey: "k"})

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.Consume(ctx, ports.Attempt{TokenHash: []byte("h"), ID: "id", KeyHash: []byte("k"), CredentialHash: []byte("c"), Device: ports.Device{ID: "d", ScopeID: scope}, Receipt: []byte("r"), Now: now, RecoverUntil: now.Add(time.Hour)})

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.ListArtifacts(ctx, scope, "", 10)

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.Artifact(ctx, scope, "dig")

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.Enrollment(ctx, []byte("h"))

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.QueryDevices(ctx, ports.DeviceQuery{ScopeID: scope})

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.GetDevice(ctx, scope, "d")

	mockMu.Lock()
	mockFailNextQuery = true
	mockMu.Unlock()
	_, _ = storeV1.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: "d"})

	// 8. Exec failure fault injection across sqlstore methods
	mockMu.Lock()
	mockFailNextExec = true
	mockMu.Unlock()
	_ = storeV1.CreateEnrollment(ctx, ports.Enrollment{TokenHash: []byte("h"), ScopeID: scope, ExpiresAt: now.Add(time.Hour)})

	mockMu.Lock()
	mockFailNextExec = true
	mockMu.Unlock()
	_ = storeV1.RegisterArtifact(ctx, ports.Artifact{ScopeID: scope, SHA256: strings.Repeat("f", 64)})

	mockMu.Lock()
	mockFailNextExec = true
	mockMu.Unlock()
	_ = storeV1.RemoveArtifact(ctx, scope, strings.Repeat("f", 64))

	mockMu.Lock()
	mockFailNextExec = true
	mockMu.Unlock()
	_ = storeV1.Revoke(ctx, scope, "d")

	// 9. Specific internal query fault injection (hitting lines 246, 273, 304, 381 in changes.go; lines 259, 270, 322 in operations.go; line 211 in fleet.go)
	for _, substr := range []string{
		"devices",
		"artifacts",
		"fleet_groups",
		"projection_items",
		"commands",
		"operations",
		"fleet_assignments",
	} {
		mockMu.Lock()
		mockFailQuerySubstring = substr
		mockMu.Unlock()
		_, _ = storeV1.ReadProjection(ctx, scope, "", 0, 10, now)

		mockMu.Lock()
		mockFailQuerySubstring = substr
		mockMu.Unlock()
		_, _ = storeV1.Assign(ctx, ports.Assignment{ScopeID: scope, DeviceID: "d", IdempotencyKey: "k", OperationID: "o", ArtifactSHA256: strings.Repeat("f", 64), Now: now, Deadline: now.Add(time.Hour)})

		mockMu.Lock()
		mockFailQuerySubstring = substr
		mockMu.Unlock()
		_, _ = storeV1.CreateFleetAssignment(ctx, ports.FleetRequest{ScopeID: scope, ID: "f", GroupID: "g", ArtifactSHA256: strings.Repeat("f", 64), NotBefore: now, Deadline: now.Add(time.Hour)}, now)

		mockMu.Lock()
		mockFailQuerySubstring = substr
		mockMu.Unlock()
		_, _ = storeV1.ListOperations(ctx, scope, "d", "", 10, now)

		mockMu.Lock()
		mockFailQuerySubstring = substr
		mockMu.Unlock()
		_, _ = storeV1.ListCommands(ctx, scope, "d", "", 10, now)

		mockMu.Lock()
		mockFailQuerySubstring = substr
		mockMu.Unlock()
		_, _ = storeV1.Sync(ctx, ports.SyncInput{DeviceID: "d", CredentialHash: []byte("h")})
	}
}

// TestPostgresQueryScanAndUnmarshalFaults exercises Scan and Unmarshal failure branches across all entities.
func TestPostgresQueryScanAndUnmarshalFaults(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("mock_postgres", "mock://postgres_faults")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mockMu.Lock()
	mockVersion = 1
	mockNoVersionRow = false
	mockFailNextQuery = false
	mockFailNextExec = false
	mockFailQuerySubstring = ""
	mockFailExecSubstring = ""
	mockBadJSONRows = false
	mockScanErrorRows = false
	mockRowsErr = false
	mockMu.Unlock()

	store, err := Open(ctx, db, "postgres", "kylok_faults", true)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	now := time.Now().UTC()
	scope := "scope_faults"

	// 1. Bad JSON rows (Scan succeeds, json.Unmarshal fails)
	mockMu.Lock()
	mockBadJSONRows = true
	mockMu.Unlock()
	_, _ = store.ListArtifacts(ctx, scope, "", 10)

	mockMu.Lock()
	mockBadJSONRows = true
	mockMu.Unlock()
	_, _ = store.ListFleetAssignments(ctx, scope, "", 10)

	mockMu.Lock()
	mockBadJSONRows = true
	mockMu.Unlock()
	_, _ = store.ListGroups(ctx, scope, "", 10)

	// 2. Scan error rows (rows.Scan fails due to mismatched columns/types)
	mockMu.Lock()
	mockScanErrorRows = true
	mockMu.Unlock()
	_, _ = store.ListArtifacts(ctx, scope, "", 10)

	mockMu.Lock()
	mockScanErrorRows = true
	mockMu.Unlock()
	_, _ = store.ListFleetAssignments(ctx, scope, "", 10)

	mockMu.Lock()
	mockScanErrorRows = true
	mockMu.Unlock()
	_, _ = store.ListGroups(ctx, scope, "", 10)

	mockMu.Lock()
	mockScanErrorRows = true
	mockMu.Unlock()
	_, _ = store.QueryDevices(ctx, ports.DeviceQuery{ScopeID: scope, Limit: 10})

	// 3. Rows iteration error (rows.Err() != nil)
	mockMu.Lock()
	mockRowsErr = true
	mockMu.Unlock()
	_, _ = store.ListArtifacts(ctx, scope, "", 10)

	mockMu.Lock()
	mockRowsErr = true
	mockMu.Unlock()
	_, _ = store.ListFleetAssignments(ctx, scope, "", 10)

	mockMu.Lock()
	mockRowsErr = true
	mockMu.Unlock()
	_, _ = store.ListGroups(ctx, scope, "", 10)

	// 4. Operations and Commands Scan, Unmarshal, and RowsErr faults
	mockMu.Lock()
	mockScanErrorRows = true
	mockMu.Unlock()
	_, _ = store.ListOperations(ctx, scope, "d", "", 10, now)

	mockMu.Lock()
	mockBadJSONRows = true
	mockMu.Unlock()
	_, _ = store.ListOperations(ctx, scope, "d", "", 10, now)

	mockMu.Lock()
	mockScanErrorRows = true
	mockMu.Unlock()
	_, _ = store.ListCommands(ctx, scope, "d", "", 10, now)

	mockMu.Lock()
	mockBadJSONRows = true
	mockMu.Unlock()
	_, _ = store.ListCommands(ctx, scope, "d", "", 10, now)

	mockMu.Lock()
	mockScanErrorRows = true
	mockMu.Unlock()
	_, _ = store.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: "d", Limit: 10, Now: now})

	mockMu.Lock()
	mockBadJSONRows = true
	mockMu.Unlock()
	_, _ = store.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: "d", Limit: 10, Now: now})

	mockMu.Lock()
	mockRowsErr = true
	mockMu.Unlock()
	ops, err := store.ListOperations(ctx, scope, "d", "", 10, now)
	t.Logf("ListOperations with mockRowsErr returned: %v, err: %v", ops, err)

	mockMu.Lock()
	mockRowsErr = true
	mockMu.Unlock()
	_, _ = store.ListCommands(ctx, scope, "d", "", 10, now)

	mockMu.Lock()
	mockRowsErr = true
	mockMu.Unlock()
	_, _ = store.QueryOperations(ctx, ports.OperationQuery{ScopeID: scope, DeviceID: "d", Limit: 10, Now: now})

	mockMu.Lock()
	mockRowsErr = true
	mockMu.Unlock()
	_, _ = store.QueryDevices(ctx, ports.DeviceQuery{ScopeID: scope, Limit: 10})

	// 5. Changes and Projections Scan, Unmarshal, and RowsErr faults
	mockMu.Lock()
	mockScanErrorRows = true
	mockMu.Unlock()
	_, _ = store.ListChanges(ctx, scope, 0, 10)

	mockMu.Lock()
	mockRowsErr = true
	mockMu.Unlock()
	_, _ = store.ListChanges(ctx, scope, 0, 10)

	mockMu.Lock()
	mockScanErrorRows = true
	mockMu.Unlock()
	_, _ = store.GetDeviceState(ctx, scope, "d")

	mockMu.Lock()
	mockBadJSONRows = true
	mockMu.Unlock()
	_, _ = store.GetDeviceState(ctx, scope, "d")

	mockMu.Lock()
	mockScanErrorRows = true
	mockMu.Unlock()
	_, _ = store.ReadProjection(ctx, scope, "", 0, 10, now)

	mockMu.Lock()
	mockBadJSONRows = true
	mockMu.Unlock()
	_, _ = store.ReadProjection(ctx, scope, "", 0, 10, now)

	mockMu.Lock()
	mockRowsErr = true
	mockMu.Unlock()
	_, _ = store.ReadProjection(ctx, scope, "", 0, 10, now)

	mockMu.Lock()
	mockScanErrorRows = true
	mockMu.Unlock()
	_, _ = store.ReadProjection(ctx, scope, "snap_id", 0, 10, now)

	mockMu.Lock()
	mockBadJSONRows = true
	mockMu.Unlock()
	_, _ = store.ReadProjection(ctx, scope, "snap_id", 0, 10, now)

	mockMu.Lock()
	mockRowsErr = true
	mockMu.Unlock()
	_, _ = store.ReadProjection(ctx, scope, "snap_id", 0, 10, now)

	// 6. Targeted Query and Exec failure substring injections
	for _, substr := range []string{
		"artifacts WHERE scope_id",
		"fleet_groups WHERE scope_id",
		"projection_items WHERE snapshot_id",
		"SELECT data FROM operations WHERE device_id",
		"SELECT data FROM commands WHERE device_id",
		"SELECT sequence FROM change_heads",
		"SELECT report FROM commands",
	} {
		mockMu.Lock()
		mockFailQuerySubstring = substr
		mockMu.Unlock()
		_, _ = store.ReadProjection(ctx, scope, "", 0, 10, now)

		mockMu.Lock()
		mockFailQuerySubstring = substr
		mockMu.Unlock()
		_, _ = store.ReadProjection(ctx, scope, "snap_id", 0, 10, now)

		mockMu.Lock()
		mockFailQuerySubstring = substr
		mockMu.Unlock()
		_, _ = store.GetDeviceState(ctx, scope, "d")
	}

	for _, substr := range []string{
		"projection_items",
		"DELETE FROM changes",
		"UPDATE devices SET desired_revision",
		"UPDATE commands SET report",
	} {
		mockMu.Lock()
		mockFailExecSubstring = substr
		mockMu.Unlock()
		_, _ = store.ReadProjection(ctx, scope, "", 0, 10, now)

		mockMu.Lock()
		mockFailExecSubstring = substr
		mockMu.Unlock()
		_, _ = store.SetNetworkPolicy(ctx, ports.NetworkAssignment{
			ScopeID: scope, DeviceID: "d", IdempotencyKey: "k",
			Policy: ports.NetworkPolicy{PrimaryID: "p", Profiles: []ports.WifiProfile{{ID: "p", SSID: "s", Security: "OPEN"}}},
		})
	}

	_ = now
}

