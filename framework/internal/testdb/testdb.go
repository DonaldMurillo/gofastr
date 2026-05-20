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

	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Postgres connection resolution
//
// Order:
//  1. TEST_POSTGRES_DSN env var      — fastest, you wire your local PG.
//  2. testcontainers-go (Docker)     — auto-spawn an ephemeral container.
//  3. t.Skip                         — neither available; SQLite-only.
//
// Resolution is memoised across all tests in the process.

var (
	pgOnce     sync.Once
	pgBaseDSN  string
	pgErr      error
	pgUsing    string
	pgLogged   atomic.Bool
	pgContaine *tcpostgres.PostgresContainer //nolint:unused // kept alive for process lifetime
)

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
