// Package testdb provides shared per-test database helpers used by the
// framework's internal tests AND by framework_test (external) tests that
// can't access package-private helpers.
//
// Live in framework/internal/ so battery tests and downstream consumers
// can't depend on it, these helpers are tied to gofastr's test fixtures
// and shouldn't leak into product code.
package testdb

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

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
	_ "github.com/lib/pq"

	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Postgres connection resolution
//
// Order:
//  1. TEST_POSTGRES_DSN env var: CI's postgres service sets it; locally
//     `make postgres-up` starts the docker-compose service.
//  2. t.Skip: unset; SQLite-only. This package always skips; the fail-closed
//     canary for a broken CI Postgres lives in internal/pgtest, which
//     escalates to t.Fatal under PGTEST_REQUIRED/GITHUB_ACTIONS. One loud
//     canary is enough, and it fires on the same missing env var.
//
// Resolution is memoised across all tests in the process.
//
// A testcontainers-go branch used to sit between the two, spawning
// postgres:16-alpine on demand. It cost every downstream application the
// Docker client stack in its module graph, a require in this module's go.mod
// is inherited by everything that imports the framework, for a convenience
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
	"TEST_POSTGRES_DSN is not set: start one with `make postgres-up` (docker compose) " +
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
		// The schema binding travels in the DSN, not as `SET search_path` on
		// the open session. SetMaxOpenConns(1) bounds concurrency, not
		// connection identity: database/sql discards a connection whose
		// backend died and silently opens a replacement, and a replacement
		// starts at the default search_path. Every statement after that
		// point runs in "$user", public — so the test's tables are invisible
		// and it fails as `relation "…" does not exist`, pointing at the
		// schema rather than at the connection that was actually replaced.
		// lib/pq forwards unrecognised DSN parameters to the server as
		// startup options, so carrying it here applies it to every
		// connection the pool ever opens. Proven by killing the backend with
		// pg_terminate_backend: the session-level SET does not survive it and
		// the DSN parameter does (TestSearchPathSurvivesConnectionReplacement).
		scoped, err := withSearchPath(base, schemaName)
		if err != nil {
			t.Fatalf("scope dsn to %s: %v", schemaName, err)
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
			db.ExecContext(ctx, "DROP SCHEMA "+schemaName+" CASCADE")
			db.Close()
		})
		return scopedDB
	}
	t.Fatalf("unknown dialect: %s", dialect)
	return nil
}

// withSearchPath returns dsn with search_path set to schema, so every
// connection the pool opens from it starts already scoped.
//
// Both DSN spellings lib/pq accepts are handled: a URL
// (postgres://user@host/db?sslmode=disable) and keyword/value
// (host=… user=… dbname=…). An existing search_path is replaced rather than
// appended — two of them would leave which one wins up to the parser.
//
// schema comes from NewSchemaName, which emits only [a-z0-9_], so it needs
// no quoting here; anything else is refused rather than interpolated.
func withSearchPath(dsn, schema string) (string, error) {
	if schema == "" {
		return "", errors.New("empty schema name")
	}
	for _, r := range schema {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return "", fmt.Errorf("schema %q is not [a-z0-9_]; refusing to put it in a DSN", schema)
		}
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("parse dsn: %w", err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	// Keyword/value form. Drop any existing search_path pair, then append.
	pairs, err := splitKeywordValueDSN(dsn)
	if err != nil {
		return "", err
	}
	kept := make([]string, 0, len(pairs)+1)
	for _, p := range pairs {
		if p.key == "search_path" {
			continue
		}
		kept = append(kept, p.raw)
	}
	kept = append(kept, "search_path="+schema)
	return strings.Join(kept, " "), nil
}

// dsnPair is one keyword/value entry: its key, and the original text of the
// whole `key=value` so re-emitting a kept pair cannot change its meaning.
type dsnPair struct{ key, raw string }

// splitKeywordValueDSN tokenises a libpq keyword/value DSN.
//
// strings.Fields is wrong here, and quietly so. libpq values may be
// single-quoted and contain spaces, so `search_path='stale, public'` splits
// into `search_path='stale,` and `public'` — dropping the first leaves the
// second behind as a stray token, and a quoted value that merely CONTAINS
// "search_path=" loses its tail to the same filter. Either way the rewritten
// DSN is malformed, and lib/pq reports that rather than the schema.
//
// Follows libpq's fe-connect.c: whitespace around the key and the `=` is
// allowed; a value is either single-quoted with backslash escapes, or runs to
// the next whitespace. A DSN this cannot parse is an error, not a guess —
// silently reshaping a connection string is how the original bug read.
func splitKeywordValueDSN(dsn string) ([]dsnPair, error) {
	var out []dsnPair
	i := 0
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
	for {
		for i < len(dsn) && isSpace(dsn[i]) {
			i++
		}
		if i >= len(dsn) {
			return out, nil
		}
		start := i
		for i < len(dsn) && dsn[i] != '=' && !isSpace(dsn[i]) {
			i++
		}
		key := dsn[start:i]
		if key == "" {
			return nil, fmt.Errorf("dsn: empty key at offset %d", start)
		}
		for i < len(dsn) && isSpace(dsn[i]) {
			i++
		}
		if i >= len(dsn) || dsn[i] != '=' {
			return nil, fmt.Errorf("dsn: key %q has no value", key)
		}
		i++ // '='
		for i < len(dsn) && isSpace(dsn[i]) {
			i++
		}
		if i < len(dsn) && dsn[i] == '\'' {
			i++ // opening quote
			for {
				if i >= len(dsn) {
					return nil, fmt.Errorf("dsn: unterminated quoted value for key %q", key)
				}
				if dsn[i] == '\\' && i+1 < len(dsn) {
					i += 2
					continue
				}
				if dsn[i] == '\'' {
					i++ // closing quote
					break
				}
				i++
			}
		} else {
			for i < len(dsn) && !isSpace(dsn[i]) {
				i++
			}
		}
		out = append(out, dsnPair{key: key, raw: dsn[start:i]})
	}
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
// from the test's name, the process id, and a process-local counter. Postgres
// identifiers have a 63-byte cap; truncated aggressively.
//
// The pid is load-bearing, not decoration. The counter alone is process-local,
// which was sufficient only while every test process got its own ephemeral
// container: `go test -p 2` runs packages as separate processes, and against
// one shared Postgres, the CI service, or a local `make postgres-up`, two of
// them reach `t_<sametest>_1` and the second fails to create its schema.
// internal/pgtest has always included the pid for this reason.
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
	// 40 chars of test name + the pid and counter suffixes stay inside
	// Postgres's 63-byte identifier cap. Trimmed to 30 to make room for the
	// pid without silently truncating the discriminator instead of the name.
	if len(clean) > 30 {
		clean = clean[:30]
	}
	return fmt.Sprintf("t_%s_%d_%d", clean, pid, id)
}

// WaitPGReady pings the database with linear backoff until it answers or
// the total deadline expires. Bounded to ~5s.
func WaitPGReady(db *sql.DB) error {
	const maxAttempts = 25
	for range maxAttempts {
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
	user, _, ok := strings.CutLast(dsn[:at], ":")
	if !ok {
		return dsn
	}
	return user + ":****" + dsn[at:]
}
