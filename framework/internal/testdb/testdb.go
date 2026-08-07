// Package testdb provides shared per-test database helpers used by the
// framework's internal tests AND by framework_test (external) tests that
// can't access package-private helpers.
//
// Live in framework/internal/ so battery tests and downstream consumers
// can't depend on it — these helpers are tied to gofastr's test fixtures
// and shouldn't leak into product code.
package testdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
	_ "github.com/lib/pq"

	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Postgres connection resolution
//
// Order:
//  1. TEST_POSTGRES_DSN env var — CI's postgres service sets it; locally
//     `make postgres-up` starts the docker-compose service.
//  2. t.Skip — unset; SQLite-only. This package always skips; the fail-closed
//     canary for a broken CI Postgres lives in internal/pgtest, which
//     escalates to t.Fatal under PGTEST_REQUIRED/GITHUB_ACTIONS. One loud
//     canary is enough, and it fires on the same missing env var.
//
// Resolution is memoised across all tests in the process.
//
// A testcontainers-go branch used to sit between the two, spawning
// postgres:16-alpine on demand. It cost every downstream application the
// Docker client stack in its module graph — a require in this module's go.mod
// is inherited by everything that imports the framework — for a convenience
// only this repo's own tests used. See cmd/repolint's
// test-only-dep-in-consumer-graph rule, which now keeps it out.

var (
	pgOnce    sync.Once
	pgBaseDSN string
	pgErr     error
	pgUsing   string
	pgLogged  atomic.Bool
)

// errNoPostgres names the two ways to supply a server rather than reporting a
// bare absence.
var errNoPostgres = errors.New(
	"TEST_POSTGRES_DSN is not set — start one with `make postgres-up` (docker compose) " +
		"or point at an existing server, e.g. " +
		"TEST_POSTGRES_DSN='postgres://test:test@localhost:5432/framework_test?sslmode=disable'")

// ResolvePostgresOnce returns a base DSN to a working Postgres or an error
// describing why one isn't reachable. Resolution is memoised across all
// tests in the process.
func ResolvePostgresOnce() (string, error) {
	pgOnce.Do(func() {
		if dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN")); dsn != "" {
			pgBaseDSN = dsn
			pgUsing = "env"
			return
		}
		pgErr = errNoPostgres
	})
	return pgBaseDSN, pgErr
}

// Dialects is the canonical list of dialects ForEachDialect iterates.
var Dialects = []migrate.Dialect{migrate.DialectSQLite, migrate.DialectPostgres}

// Open returns a fresh database for the given dialect. SQLite is an
// in-memory database with foreign keys enabled. Postgres connects to the
// shared resolved instance, creates a unique per-test schema, and sets
// the connection's search_path to that schema. Cleanup is registered via
// t.Cleanup.
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
		// modernc.org/sqlite gives each :memory: connection its own database.
		// A single connection matches the old test-driver behavior and keeps
		// schema/data visible across sequential CRUD operations.
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { db.Close() })
		return db
	case migrate.DialectPostgres:
		base, err := ResolvePostgresOnce()
		if err != nil {
			t.Skipf("Postgres unavailable: %v", err)
		}
		if !pgLogged.Swap(true) {
			t.Logf("Postgres tests using %s: %s", pgUsing, RedactDSN(base))
		}
		db, err := sql.Open("postgres", base)
		if err != nil {
			t.Fatalf("open pg: %v", err)
		}
		db.SetMaxOpenConns(1)
		if err := WaitPGReady(db); err != nil {
			t.Fatalf("ping pg: %v", err)
		}
		schemaName := NewSchemaName(t)
		if _, err := db.ExecContext(context.Background(), "CREATE SCHEMA "+schemaName); err != nil {
			t.Fatalf("create schema %s: %v", schemaName, err)
		}
		if _, err := db.ExecContext(context.Background(), "SET search_path TO "+schemaName); err != nil {
			t.Fatalf("set search_path: %v", err)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			db.ExecContext(ctx, "DROP SCHEMA "+schemaName+" CASCADE")
			db.Close()
		})
		return db
	}
	t.Fatalf("unknown dialect: %s", dialect)
	return nil
}

// ForEachDialect runs fn against every dialect in Dialects as a t.Run
// subtest. Postgres subtests are skipped (not failed) when no PG is
// reachable.
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

// NewSchemaName produces a unique, lowercase, identifier-safe schema name
// from the test's name plus a process-local counter. Postgres identifiers
// have a 63-byte cap; truncated aggressively.
func NewSchemaName(t *testing.T) string {
	id := schemaCounter.Add(1)
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
	if len(clean) > 40 {
		clean = clean[:40]
	}
	return fmt.Sprintf("t_%s_%d", clean, id)
}

// WaitPGReady pings the database with linear backoff until it answers or
// the total deadline expires. Bounded to ~5s.
func WaitPGReady(db *sql.DB) error {
	const maxAttempts = 25
	for i := 0; i < maxAttempts; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		err := db.PingContext(ctx)
		cancel()
		if err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("Postgres did not become ready within ~5s")
}

// RedactDSN strips the password from a Postgres URL for log output.
func RedactDSN(dsn string) string {
	at := strings.Index(dsn, "@")
	if at < 0 {
		return dsn
	}
	colon := strings.LastIndex(dsn[:at], ":")
	if colon < 0 {
		return dsn
	}
	return dsn[:colon+1] + "****" + dsn[at:]
}
