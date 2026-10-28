// SPDX-License-Identifier: LGPL-3.0-only
package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/thienvu18/kylok/go/adapters/sqlite"
	"github.com/thienvu18/kylok/go/ports"
	"github.com/thienvu18/kylok/go/ports/portstest"
)

func TestOpenRejectsIncompleteExistingSchema(t *testing.T) {
	for _, tc := range []struct{ name, damage string }{
		{"missing enrollment table", "DROP TABLE enrollments"},
		{"missing device table", "DROP TABLE devices"},
		{"missing enrollment binding", "ALTER TABLE enrollments DROP COLUMN key_hash"},
		{"missing credential verifier", "ALTER TABLE devices DROP COLUMN credential_hash"},
		{"missing policy table", "DROP TABLE policies"},
		{"missing policy request binding", "DROP TABLE policy_requests"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			store, err := sqlite.Open(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(tc.damage); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = sqlite.Open(context.Background(), path)
			if err == nil {
				store.Close()
				t.Fatal("accepted an incomplete version 1 schema")
			}
			// A failed constructor must release its transaction/connection and
			// leave the damaged state in place for explicit operator recovery.
			db, err = sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec("BEGIN IMMEDIATE; ROLLBACK"); err != nil {
				t.Fatal("constructor leaked its write lock:", err)
			}
		})
	}
}

func TestOpenRejectsUnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version=2"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if store, err := sqlite.Open(context.Background(), path); err == nil {
		store.Close()
		t.Fatal("accepted a newer unsupported database version")
	}
}

func TestConformance(t *testing.T) {
	factory := func(t *testing.T) ports.StateStore {
		s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	portstest.State(t, factory)
	portstest.Operations(t, factory)
	portstest.Policies(t, factory)
	portstest.ManagedConfigurations(t, factory)
	portstest.Management(t, factory)
	portstest.Changes(t, factory)
	portstest.Fleet(t, factory)
	portstest.Commands(t, factory)
	portstest.Network(t, factory)
}
