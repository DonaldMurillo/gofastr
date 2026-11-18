// Package testdb gives framework tests a fresh database per test on each dialect: Open, ForEachDialect.
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

	"github.com/DonaldMurillo/gofastr/internal/dsnredact"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
	_ "github.com/lib/pq"

	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

var (
	pgOnce    sync.Once
	pgBaseDSN string
	pgErr     error
	pgUsing   string
	pgLogged  atomic.Bool
)

var errNoPostgres = errors.New(
	"TEST_POSTGRES_DSN is not set: start one with `make postgres-up` (docker compose) " +
		"or point at an existing server, e.g. " +
		"TEST_POSTGRES_DSN='postgres://test:test@localhost:5432/framework_test?sslmode=disable'")

// ResolvePostgresOnce returns TEST_POSTGRES_DSN, or an error saying how to supply one; memoised per process.
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

type dsnPair struct{ key, raw string }

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
		i++
		for i < len(dsn) && isSpace(dsn[i]) {
			i++
		}
		if i < len(dsn) && dsn[i] == '\'' {
			i++
			for {
				if i >= len(dsn) {
					return nil, fmt.Errorf("dsn: unterminated quoted value for key %q", key)
				}
				if dsn[i] == '\\' && i+1 < len(dsn) {
					i += 2
					continue
				}
				if dsn[i] == '\'' {
					i++
					break
				}
				i++
			}
		} else {
			for i < len(dsn) && !isSpace(dsn[i]) {
				if dsn[i] == '\\' && i+1 < len(dsn) {
					i += 2
					continue
				}
				i++
			}
		}
		out = append(out, dsnPair{key: key, raw: dsn[start:i]})
	}
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

// WaitPGReady pings db until it answers, for about five seconds.
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

// RedactDSN strips credentials from a DSN for logs, with internal/dsnredact.
func RedactDSN(dsn string) string {
	return dsnredact.Redact(dsn)
}
