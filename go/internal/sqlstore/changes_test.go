// SPDX-License-Identifier: LGPL-3.0-only
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/thienvu18/kylok/go/ports"
	_ "modernc.org/sqlite"
)

func TestRetentionBoundaryAndRollback(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	s, err := Open(ctx, db, "sqlite", "", true)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// A single bulk transaction fills the real retention limit cheaply. The helper's
	// deduplication key varies so all invalidations represent distinct resources.
	for i := 0; i < ports.ChangeRetention+2; i++ {
		if err = recordChange(ctx, tx, "scope", "device_changed", string(rune(i))); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ListChanges(ctx, "scope", 1, 100); !errors.Is(err, ports.ErrCursorExpired) {
		t.Fatal("silent retention gap", err)
	}
	page, err := s.ListChanges(ctx, "scope", 2, 100)
	if err != nil || page.Items[0].Sequence != 3 || page.Head != ports.ChangeRetention+2 {
		t.Fatal(page, err)
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM changes WHERE scope_id='scope'").Scan(&count); err != nil || count != ports.ChangeRetention {
		t.Fatal(count, err)
	}
	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = recordChange(ctx, tx, "scope", "device_changed", "rolled-back"); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	result, err := s.ListChanges(ctx, "scope", page.Head, 100)
	if err != nil || result.Next != page.Head || len(result.Items) != 0 {
		t.Fatal(result, err)
	}
}
