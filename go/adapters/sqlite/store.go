// SPDX-License-Identifier: LGPL-3.0-only
// Package sqlite implements Kylok's durable single-node state store.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/thienvu18/kylok/go/internal/sqlstore"
	"github.com/thienvu18/kylok/go/ports"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
)

// Store owns the SQLite pool and promotes the shared transactional state contract.
type Store struct {
	ports.StateStore
	pool *sql.DB
}

// Open requires a durable path. It never substitutes a temporary or memory DB.
// Usage: store, err := sqlite.Open(ctx, "/var/lib/kylok/state.sqlite").
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" || path == ":memory:" {
		return nil, errors.New("SQLite requires a durable file path")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// Ensure database and WAL inherit private directory/file permissions.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	u := &url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", u.String()+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(FULL)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	core, err := sqlstore.Open(ctx, db, "sqlite", "", true)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate SQLite: %w", err)
	}
	return &Store{StateStore: core, pool: db}, nil
}

// Close releases the SQLite-owned pool after the host drains SDK users.
// Usage: defer store.Close().
func (s *Store) Close() error { return s.pool.Close() }
