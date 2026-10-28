// SPDX-License-Identifier: LGPL-3.0-only
package sqlstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"regexp"
	"strconv"
	"strings"
)

var schemaPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
var tableNames = map[string]bool{"network_policies": true, "network_requests": true, "commands": true, "fleet_groups": true, "fleet_assignments": true, "change_heads": true, "changes": true, "projection_scans": true, "projection_items": true, "schema_version": true, "enrollments": true, "devices": true, "artifacts": true, "operations": true, "desired_apps": true, "reports": true, "policies": true, "policy_requests": true}

type database struct {
	pool            *sql.DB
	dialect, schema string
}
type transaction struct {
	*sql.Tx
	database *database
	changes  map[string]bool
}

// Translate only static SQL identifiers/placeholders outside quoted literals. Values always remain bound parameters.
func (d *database) bind(query string) string {
	if d.dialect != "postgres" {
		return query
	}
	var out strings.Builder
	parameter := 0
	for index := 0; index < len(query); {
		c := query[index]
		if c == '\'' || c == '"' {
			quote := c
			out.WriteByte(c)
			index++
			for index < len(query) {
				ch := query[index]
				out.WriteByte(ch)
				index++
				if ch == quote {
					if index < len(query) && query[index] == quote {
						out.WriteByte(query[index])
						index++
					} else {
						break
					}
				}
			}
		} else if c == '?' {
			parameter++
			out.WriteByte('$')
			out.WriteString(strconv.Itoa(parameter))
			index++
		} else if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
			start := index
			index++
			for index < len(query) {
				ch := query[index]
				if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_') {
					break
				}
				index++
			}
			word := query[start:index]
			if tableNames[word] {
				out.WriteByte('"')
				out.WriteString(d.schema)
				out.WriteString(`".`)
			}
			out.WriteString(word)
		} else {
			out.WriteByte(c)
			index++
		}
	}
	return out.String()
}

// BeginTx serializes PostgreSQL control-plane effects per schema before the first state read.
// A transaction-level lock is released on rollback, commit, connection loss or server restart.
// Read committed ensures reads after lock acquisition see the previous holder's committed state.
// Usage: tx, err := database.BeginTx(ctx, nil); defer tx.Rollback().
func (d *database) BeginTx(ctx context.Context, options *sql.TxOptions) (*transaction, error) {
	if d.dialect == "postgres" {
		options = &sql.TxOptions{Isolation: sql.LevelReadCommitted}
	}
	tx, err := d.pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	if d.dialect == "postgres" {
		sum := sha256.Sum256([]byte("kylok.state/" + d.schema))
		key := int64(binary.BigEndian.Uint64(sum[:8]))
		if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", key); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	return &transaction{Tx: tx, database: d}, nil
}

// ExecContext wraps standalone writes in the same transaction/lock discipline as multi-statement effects.
// Usage: result, err := database.ExecContext(ctx, query, args...).
func (d *database) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return result, tx.Commit()
}

// QueryRowContext performs a single committed observation without opening an ambient host transaction.
// Usage: err := database.QueryRowContext(ctx, query, args...).Scan(&value).
func (d *database) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.pool.QueryRowContext(ctx, d.bind(query), args...)
}

// QueryContext reads one statement's committed rows; the caller closes them.
// Usage: rows, err := database.QueryContext(ctx, query, args...).
func (d *database) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.pool.QueryContext(ctx, d.bind(query), args...)
}

// PingContext checks the supplied pool without changing its settings or ownership.
// Usage: err := database.PingContext(ctx).
func (d *database) PingContext(ctx context.Context) error { return d.pool.PingContext(ctx) }

// ExecContext binds one statement to this SDK-owned state transaction.
// Usage: result, err := tx.ExecContext(ctx, query, args...).
func (t *transaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, t.database.bind(query), args...)
}

// QueryRowContext reads within the transaction's serialization boundary.
// Usage: err := tx.QueryRowContext(ctx, query, args...).Scan(&value).
func (t *transaction) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.Tx.QueryRowContext(ctx, t.database.bind(query), args...)
}

// QueryContext reads rows within this transaction; close them before committing.
// Usage: rows, err := tx.QueryContext(ctx, query, args...).
func (t *transaction) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(ctx, t.database.bind(query), args...)
}
