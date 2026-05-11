package framework

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// ----------------------------------------------------------------------------
// Postgres connection resolution
//
// Order:
//  1. TEST_POSTGRES_DSN env var      — fastest, you wire your local PG.
//  2. testcontainers-go (Docker)     — auto-spawn an ephemeral container.
//  3. t.Skip                         — neither available; SQLite-only.
//
// The chosen path is logged once per test process so it's clear which one
// the run is using.
// ----------------------------------------------------------------------------

var (
	pgOnce     sync.Once
	pgBaseDSN  string
	pgErr      error
	pgUsing    string // "env" or "container" — for diagnostic output
	pgLogged   atomic.Bool
	pgContaine *tcpostgres.PostgresContainer // kept alive for process lifetime
)

// resolvePostgresOnce returns a base DSN to a working Postgres or an error
// describing why one isn't reachable. Resolution is memoised across all tests
// in the process.
func resolvePostgresOnce() (string, error) {
	pgOnce.Do(func() {
		if dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN")); dsn != "" {
			pgBaseDSN = dsn
			pgUsing = "env"
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("framework_test"),
			tcpostgres.WithUsername("test"),
			tcpostgres.WithPassword("test"),
		)
		if err != nil {
			pgErr = fmt.Errorf("testcontainers Postgres: %w", err)
			return
		}
		dsn, err := c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			pgErr = err
			return
		}
		pgBaseDSN = dsn
		pgUsing = "container"
		pgContaine = c
	})
	return pgBaseDSN, pgErr
}

// ----------------------------------------------------------------------------
// Per-test database
// ----------------------------------------------------------------------------

// Dialects is the canonical list of dialects that forEachDialect iterates.
var Dialects = []Dialect{DialectSQLite, DialectPostgres}

// openTestDB returns a fresh database for the given dialect. For SQLite this
// is an in-memory database with foreign keys enabled. For Postgres it
// connects to the shared resolved instance, creates a unique per-test schema
// to isolate state, and sets the connection's search_path to that schema.
//
// Cleanup (DROP SCHEMA, db.Close) is registered via t.Cleanup.
func openTestDB(t *testing.T, dialect Dialect) *sql.DB {
	t.Helper()
	switch dialect {
	case DialectSQLite:
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
			t.Fatalf("pragma fk: %v", err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	case DialectPostgres:
		base, err := resolvePostgresOnce()
		if err != nil {
			t.Skipf("Postgres unavailable: %v", err)
		}
		if !pgLogged.Swap(true) {
			t.Logf("Postgres tests using %s: %s", pgUsing, redactDSN(base))
		}
		db, err := sql.Open("postgres", base)
		if err != nil {
			t.Fatalf("open pg: %v", err)
		}
		// Single connection per test keeps schema search_path consistent across
		// queries — pooled connections on Postgres can otherwise pick a fresh
		// session that doesn't share the SET search_path.
		db.SetMaxOpenConns(1)

		// Postgres in containers logs "ready to accept connections" before it
		// has actually bound, then restarts once on first init. Ping with a
		// short backoff so the first real query doesn't hit a reset.
		if err := waitPGReady(db); err != nil {
			t.Fatalf("ping pg: %v", err)
		}

		schemaName := newSchemaName(t)
		if _, err := db.ExecContext(context.Background(), "CREATE SCHEMA "+schemaName); err != nil {
			t.Fatalf("create schema %s: %v", schemaName, err)
		}
		if _, err := db.ExecContext(context.Background(), "SET search_path TO "+schemaName); err != nil {
			t.Fatalf("set search_path: %v", err)
		}
		t.Cleanup(func() {
			// Best-effort cleanup; leftover schemas are dropped on container
			// teardown anyway.
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

// forEachDialect runs fn against every dialect in Dialects as a t.Run subtest.
// Postgres subtests are skipped (not failed) when no PG is reachable.
func forEachDialect(t *testing.T, fn func(t *testing.T, db *sql.DB, dialect Dialect)) {
	t.Helper()
	for _, dialect := range Dialects {
		d := dialect
		t.Run(string(d), func(t *testing.T) {
			db := openTestDB(t, d)
			fn(t, db, d)
		})
	}
}

// ----------------------------------------------------------------------------
// Schema name generation
// ----------------------------------------------------------------------------

var schemaCounter atomic.Uint64

// newSchemaName produces a unique, lowercase, identifier-safe schema name
// from the test's name plus a process-local counter. Postgres identifiers
// have a 63-byte cap; we truncate aggressively.
func newSchemaName(t *testing.T) string {
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

// waitPGReady pings the database with linear backoff until it answers or the
// total deadline expires. Bounded to ~5s of waiting so a misconfigured
// connection still fails fast.
func waitPGReady(db *sql.DB) error {
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

// redactDSN strips the password from a Postgres URL for log output.
func redactDSN(dsn string) string {
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
