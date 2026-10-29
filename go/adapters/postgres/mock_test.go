// SPDX-License-Identifier: LGPL-3.0-only
package postgres_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"testing"

	"github.com/thienvu18/kylok/go/adapters/postgres"
)

type mockDriver struct{}
type mockConn struct{}
type mockTx struct{}
type mockRows struct {
	columns []string
	rows    [][]driver.Value
	pos     int
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
	return driver.RowsAffected(1), nil
}

func (c *mockConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if query == "SELECT version FROM schema_version" {
		return &mockRows{
			columns: []string{"version"},
			rows:    [][]driver.Value{{int64(0)}},
		}, nil
	}
	return &mockRows{columns: []string{"col"}, rows: nil}, nil
}

func (r *mockRows) Columns() []string { return r.columns }
func (r *mockRows) Close() error      { return nil }
func (r *mockRows) Next(dest []driver.Value) error {
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
	sql.Register("mock_postgres_adapter", &mockDriver{})
}

// TestPostgresOpenWithMockDriver verifies the successful return of *Store from Open.
func TestPostgresOpenWithMockDriver(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("mock_postgres_adapter", "mock://postgres")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store, err := postgres.Open(ctx, db, postgres.Options{Initialize: true})
	if err != nil {
		t.Fatalf("expected successful Open with mock driver, got %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil Store")
	}
}
