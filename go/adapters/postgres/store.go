// SPDX-License-Identifier: LGPL-3.0-only
// Package postgres implements optional durable state storage using a host-owned database/sql pool.
package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/thienvu18/kylok/go/internal/sqlstore"
	"github.com/thienvu18/kylok/go/ports"
)

// Options selects a dedicated existing schema or explicitly opts in to initializing it.
// Schema must be a lowercase SQL identifier, at most 63 characters; no search_path changes are made.
type Options struct {
	Schema string
	// Initialize requires CREATE on the database/schema and creates only Kylok-owned objects.
	// Leave false at runtime after initialization; missing/incomplete/wrong-version schemas fail closed.
	Initialize bool
}

// Store implements the concurrent transactional state contract. PostgreSQL effects
// serialize per schema across processes using a transaction-level advisory lock.
// The host owns the pool, driver, TLS, credentials, limits and shutdown. No Close method owns that pool.
type Store struct{ ports.StateStore }

// Open verifies a dedicated schema on a supplied pool, optionally initializing it atomically.
// It never closes or reconfigures the pool, including on failure. It does not join host transactions.
// Usage: store, err := postgres.Open(ctx, db, postgres.Options{Schema: "kylok", Initialize: true}).
func Open(ctx context.Context, db *sql.DB, options Options) (*Store, error) {
	if options.Schema == "" {
		options.Schema = "kylok"
	}
	core, err := sqlstore.Open(ctx, db, "postgres", options.Schema, options.Initialize)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL state: %w", err)
	}
	return &Store{StateStore: core}, nil
}
