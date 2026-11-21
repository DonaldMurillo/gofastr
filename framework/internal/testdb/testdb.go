// Package testdb gives framework tests a fresh database per test on each dialect: Open, ForEachDialect.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/dsnredact"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

var pgLogged atomic.Bool

// ResolvePostgresOnce returns TEST_POSTGRES_DSN through the shared
// pgtest resolution (memoised per process), or pgtest's error naming
// how to supply one.
func ResolvePostgresOnce() (string, error) {
	dsn, _, err := pgtest.ResolveEnv()
	return dsn, err
}

// Dialects is the canonical list of dialects ForEachDialect iterates.
var Dialects = []migrate.Dialect{migrate.DialectSQLite, migrate.DialectPostgres}

// Open returns a fresh database for dialect, closed and dropped at cleanup: SQLite in memory, Postgres a new schema (skipped with no server).
func Open(t *testing.T, dialect migrate.Dialect) *sql.DB {
	t.Helper()
	switch dialect {
	case migrate.DialectSQLite:
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
			t.Fatalf("pragma fk: %v", err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { db.Close() })
		return db
	case migrate.DialectPostgres:
		base, source, err := pgtest.ResolveEnv()
		if err != nil {
			t.Skipf("Postgres unavailable: %v", err)
		}
		schemaName := NewSchemaName(t)
		scoped, err := pgtest.DSNWithSearchPath(base, schemaName)
		if err != nil {
			t.Fatalf("scope dsn to %s: %v", schemaName, err)
		}
		if !pgLogged.Swap(true) {
			t.Logf("Postgres tests using %s: %s", source, RedactDSN(base))
		}
		db, err := sql.Open("postgres", base)
		if err != nil {
			t.Fatalf("open pg: %v", err)
		}
		db.SetMaxOpenConns(1)
		if err := WaitPGReady(db); err != nil {
			t.Fatalf("ping pg: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), "CREATE SCHEMA "+schemaName); err != nil {
			t.Fatalf("create schema %s: %v", schemaName, err)
		}
		scopedDB, err := sql.Open("postgres", scoped)
		if err != nil {
			t.Fatalf("open pg (scoped): %v", err)
		}
		scopedDB.SetMaxOpenConns(1)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			scopedDB.Close()
			if _, err := db.ExecContext(ctx, "DROP SCHEMA "+schemaName+" CASCADE"); err != nil {
				t.Errorf("drop test schema %s: %v", schemaName, err)
			}
			db.Close()
		})
		return scopedDB
	}
	t.Fatalf("unknown dialect: %s", dialect)
	return nil
}

// ForEachDialect runs fn as a subtest on each of Dialects, with a database from Open.
func ForEachDialect(t *testing.T, fn func(t *testing.T, db *sql.DB, dialect migrate.Dialect)) {
	t.Helper()
	for _, dialect := range Dialects {
		d := dialect
		t.Run(string(d), func(t *testing.T) {
			db := Open(t, d)
			fn(t, db, d)
		})
	}
}

var schemaCounter atomic.Uint64

// NewSchemaName returns a unique [a-z0-9_] schema name of at most 63 bytes from the test name, pid and a counter.
func NewSchemaName(t *testing.T) string {
	id := schemaCounter.Add(1)
	pid := os.Getpid()
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, t.Name())
	if len(clean) > 30 {
		clean = clean[:30]
	}
	return fmt.Sprintf("t_%s_%d_%d", clean, pid, id)
}

// WaitPGReady pings db until it answers, for about five seconds, through
// the shared pgtest retry loop.
func WaitPGReady(db *sql.DB) error {
	if err := pgtest.PingRetry(db, 25, 500*time.Millisecond, 200*time.Millisecond); err != nil {
		return fmt.Errorf("Postgres did not become ready within ~5s")
	}
	return nil
}

// RedactDSN strips credentials from a DSN for logs, with internal/dsnredact.
func RedactDSN(dsn string) string {
	return dsnredact.Redact(dsn)
}
