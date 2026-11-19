// Package pgtest provides a shared real-Postgres test harness usable from any
// package in the module (core/migrate, cmd/gofastr, …) without importing
// framework/internal/testdb, which is import-restricted to the framework tree.
//
// Resolution (memoised once per process):
//  1. TEST_POSTGRES_DSN env var — CI's `pgvector/pgvector:pg16` service sets
//     it; locally, `make postgres-up` starts the docker-compose service (same
//     image) and `make test-pg` exports the matching DSN.
//  2. unset → t.Skip locally; under CI (PGTEST_REQUIRED=1 or
//     GITHUB_ACTIONS=true) escalate to t.Fatal so the ~30 real-PG suites can't
//     silently degrade to SQLite-only behind a green build.
//
// This used to spawn postgres:16-alpine through testcontainers-go when the env
// var was unset. Convenient, but testcontainers had to be a require in the root
// go.mod, and Go hands a module's requirements to everything that imports it —
// so every application built on GoFastr resolved the Docker client stack to run
// `go mod tidy` on a hello-world. A build tag would not have helped: tidy walks
// every build configuration. The env var was already the preferred path and the
// documented one for CI, so the container branch was removed rather than hidden.
//
// Each DB(t) hands back a connection scoped to a unique schema (search_path),
// so concurrent tests don't collide, with cleanup registered on t.
package pgtest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/dsnredact"
)

var (
	once       sync.Once
	baseDSN    string
	resolveErr error
	using      string
	schemaSeq  atomic.Int64
	logged     atomic.Bool
)

// ErrNoPostgres is what every caller sees when TEST_POSTGRES_DSN is unset. It
// names the two ways to supply one rather than reporting a bare absence.
var ErrNoPostgres = errors.New(
	"TEST_POSTGRES_DSN is not set — start one with `make postgres-up` (docker compose) " +
		"or point at an existing server, e.g. " +
		"TEST_POSTGRES_DSN='postgres://test:test@localhost:5432/pgtest?sslmode=disable'")

// ResolveEnv returns the trimmed TEST_POSTGRES_DSN and "env" as its
// source, or an error naming how to supply one. Memoised across the
// whole test process — the one shared read for every Postgres test
// harness in the module (this package, framework/internal/testdb and
// the battery/auth fixtures). It resolves a string only: whether a
// server answers is each entry point's ping to establish.
func ResolveEnv() (dsn string, source string, err error) {
	once.Do(func() {
		if dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN")); dsn != "" {
			baseDSN, using = dsn, "env"
			return
		}
		resolveErr = ErrNoPostgres
	})
	return baseDSN, using, resolveErr
}

// resolve returns the resolved base DSN, or an error saying none was supplied.
func resolve() (string, error) {
	dsn, _, err := ResolveEnv()
	return dsn, err
}

// required reports whether a reachable Postgres is mandatory for this run.
// True when PGTEST_REQUIRED is set to any non-empty value (the explicit opt-in
// the CI workflow sets) or when running under GitHub Actions (GITHUB_ACTIONS).
// When true the harness escalates the no-Postgres t.Skip to a t.Fatal so the
// ~30 real-PG suites cannot silently degrade to SQLite-only behind a green
// build. Local dev (neither set) keeps the cheap skip — no Docker, no failure.
func required() bool {
	if strings.TrimSpace(os.Getenv("PGTEST_REQUIRED")) != "" {
		return true
	}
	return os.Getenv("GITHUB_ACTIONS") == "true"
}

// BaseDSN returns the resolved base DSN (the maintenance/default
// database), skipping the test when TEST_POSTGRES_DSN is unset and
// failing it under required mode (see required); this is the single
// choke point every pgtest entry point (DB, FreshDatabaseDSN,
// UnusedDSN) flows through. Use BaseDSN directly for
// EnsureDatabase / CLI round-trips that need a raw connection string.
// It resolves the string only and never connects: a set-but-unreachable
// server surfaces at the caller's ping — fatal in DB and
// FreshDatabaseDSN, invisible to UnusedDSN, which connects only in its
// returned cleanup.
func BaseDSN(t *testing.T) string {
	t.Helper()
	dsn, err := resolve()
	if err != nil {
		if required() {
			t.Fatalf("pgtest: Postgres is required (PGTEST_REQUIRED/GITHUB_ACTIONS) but unreachable — real-PG suites would otherwise silently skip. resolve error: %v", err)
		}
		t.Skipf("Postgres unavailable: %v", err)
	}
	if logged.CompareAndSwap(false, true) {
		t.Logf("pgtest using %s", using)
	}
	return dsn
}

// RewriteDBName returns dsn pointed at database name, dropping any
// dbname/database query selector first: lib/pq takes the database from
// the URL path and then adds the query options independently, so a
// retained selector keeps selecting the original database. This is the
// one rewrite every database-carving test path shares (this package's
// FreshDatabaseDSN/UnusedDSN and framework/testkit).
func RewriteDBName(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return "", fmt.Errorf("pgtest.RewriteDBName: parse: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("pgtest.RewriteDBName: expected postgres scheme, got %q", u.Scheme)
	}
	u.Path = "/" + name
	q := u.Query()
	q.Del("dbname")
	q.Del("database")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// skipOrFail gives the URL-only database-carving helpers BaseDSN's skip policy.
func skipOrFail(t *testing.T, format string, args ...any) {
	t.Helper()
	if required() {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

// DB returns a *sql.DB scoped to a fresh, uniquely-named schema (via
// search_path) on the shared Postgres. An unset TEST_POSTGRES_DSN follows
// BaseDSN's skip policy. An invalid DSN or unreachable server fails the test.
func DB(t *testing.T) *sql.DB {
	t.Helper()
	base := BaseDSN(t)
	schema := fmt.Sprintf("pgt_%d_%d", os.Getpid(), schemaSeq.Add(1))
	dsn, err := DSNWithSearchPath(base, schema)
	if err != nil {
		t.Fatalf("pgtest: invalid TEST_POSTGRES_DSN: %v", err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	db.SetMaxOpenConns(1) // advisory-lock correctness on one session
	if err := PingRetry(db, 30, 2*time.Second, 200*time.Millisecond); err != nil {
		db.Close()
		t.Fatalf("ping pg: %v", err)
	}
	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		db.Close()
		t.Fatalf("create schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		// best-effort: test cleanup must not replace the original assertion.
		_, _ = db.Exec("DROP SCHEMA " + schema + " CASCADE")
		db.Close()
	})
	return db
}

// SideSchema creates a sibling schema beside db's current one — for
// objects a test must create OUTSIDE its search_path — and drops it
// (CASCADE) on cleanup. suffix distinguishes concurrent siblings.
func SideSchema(t *testing.T, db *sql.DB, suffix string) string {
	t.Helper()
	var cur string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&cur); err != nil {
		t.Fatal(err)
	}
	s := cur + suffix
	if _, err := db.Exec("CREATE SCHEMA " + s); err != nil {
		t.Fatalf("create schema %s: %v", s, err)
	}
	// best-effort: test cleanup must not replace the original assertion.
	t.Cleanup(func() { _, _ = db.Exec("DROP SCHEMA " + s + " CASCADE") })
	return s
}

// Replica opens a second pool on the schema db (from DB) is scoped to,
// the way another replica reaches the same tables. It is closed on
// t.Cleanup, before db's schema is dropped.
func Replica(t *testing.T, db *sql.DB) *sql.DB {
	t.Helper()
	var schema string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	dsn, err := DSNWithSearchPath(BaseDSN(t), schema)
	if err != nil {
		t.Fatal(err)
	}
	other, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	return other
}

// FreshDatabaseDSN creates a uniquely-named database on the shared
// Postgres and returns a URL DSN pointing at it, dropped on t.Cleanup.
// Use for CLI / tooling tests that connect by URL string and expect to
// own the database (vs DB(t), which schema-scopes a shared one). It
// skips when TEST_POSTGRES_DSN is unset or not URL-form (failing
// instead under required mode) and fails the test on the admin ping or
// CREATE DATABASE when the server is set but unreachable.
func FreshDatabaseDSN(t *testing.T) string {
	t.Helper()
	base := BaseDSN(t)
	if u, err := url.Parse(base); err != nil || u.Scheme == "" {
		skipOrFail(t, "pgtest.FreshDatabaseDSN needs a URL-form base DSN, got %q", RedactDSN(base))
	}
	admin, err := sql.Open("postgres", base)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	if err := PingRetry(admin, 30, 2*time.Second, 200*time.Millisecond); err != nil {
		admin.Close()
		t.Fatalf("ping admin: %v", err)
	}
	name := fmt.Sprintf("clitest_%d_%d", os.Getpid(), schemaSeq.Add(1))
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		admin.Close()
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() {
		// best-effort: cleanup only; the test already completed its contract.
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
		admin.Close()
	})
	dsn, err := RewriteDBName(base, name)
	if err != nil {
		skipOrFail(t, "pgtest.FreshDatabaseDSN needs a URL-form base DSN, got %q (%v)", RedactDSN(base), err)
	}
	return dsn
}

// UnusedDSN returns a URL DSN pointing at a uniquely-named database that
// does NOT yet exist, plus a cleanup that drops it if something created
// it. Use to test database-creation paths (EnsureDatabase / migrate up
// --create-db). It skips when TEST_POSTGRES_DSN is unset or not
// URL-form (failing instead under required mode) and never connects
// before the test body runs: an unreachable server surfaces in the
// caller, and only the returned cleanup opens a connection.
func UnusedDSN(t *testing.T) (string, func()) {
	t.Helper()
	base := BaseDSN(t)
	if u, err := url.Parse(base); err != nil || u.Scheme == "" {
		skipOrFail(t, "pgtest.UnusedDSN needs a URL-form base DSN, got %q", RedactDSN(base))
	}
	name := fmt.Sprintf("created_%d_%d", os.Getpid(), schemaSeq.Add(1))
	dsn, err := RewriteDBName(base, name)
	if err != nil {
		skipOrFail(t, "pgtest.UnusedDSN needs a URL-form base DSN, got %q (%v)", RedactDSN(base), err)
	}
	drop := func() {
		admin, err := sql.Open("postgres", base)
		if err != nil {
			return
		}
		defer admin.Close()
		// best-effort: returned cleanup cannot report after its caller exits.
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
	}
	return dsn, drop
}

// RedactDSN masks credentials in a DSN for safe logging. Delegates to
// internal/dsnredact — the one canonical redactor — so this twin cannot
// drift again: it returned key=value DSNs verbatim, password included,
// into the log output of DB / FreshDatabaseDSN / UnusedDSN.
func RedactDSN(dsn string) string {
	return dsnredact.Redact(dsn)
}

// PingRetry pings db up to attempts times, with a timeout per ping and an
// interval between attempts, returning the last error until a ping succeeds.
func PingRetry(db *sql.DB, attempts int, timeout, interval time.Duration) error {
	return pingRetry(db, timeout, interval, func(attempt int) bool { return attempt < attempts })
}

// PingRetryFor starts pings for budget, with a timeout per ping and an
// interval between attempts. A ping in progress keeps its own timeout.
func PingRetryFor(db *sql.DB, budget, timeout, interval time.Duration) error {
	deadline := time.Now().Add(budget)
	return pingRetry(db, timeout, interval, func(int) bool { return time.Now().Before(deadline) })
}

func pingRetry(db *sql.DB, timeout, interval time.Duration, canTry func(int) bool) error {
	var err error
	for attempt := 0; canTry(attempt); attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		err = db.PingContext(ctx)
		cancel()
		if err == nil {
			return nil
		}
		time.Sleep(interval)
	}
	return err
}
