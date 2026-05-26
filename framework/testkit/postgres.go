// Package testkit provides PUBLIC test helpers for host apps that use
// the GoFastr framework. The framework-internal counterparts live in
// framework/internal/testdb and are intentionally unexported so outside
// callers can't accidentally couple to their schema-based isolation
// strategy.
//
// The most common helper is NewIsolatedDB — it carves a fresh, named
// Postgres database for the duration of a single test and drops it on
// t.Cleanup. Use it from host apps' integration tests that want true
// per-test isolation against a real cluster.
package testkit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// NewIsolatedDB carves a fresh Postgres database on the cluster that
// adminDSN points at, runs the supplied migrate callback against it,
// and returns a connection to the carved DB. The DB is dropped (and
// any lingering backends terminated) on t.Cleanup.
//
// adminDSN must be a connection string with permission to create and
// drop databases (typically a superuser connecting to a maintenance DB
// like `postgres`). The helper hard-fails on missing or unusable DSNs
// — by design, never t.Skip. Tests that hand-roll t.Skip when a DB is
// missing pass without proving anything; this helper inverts that.
//
// migrate is called exactly once with the connection to the new DB
// before the helper returns. Pass nil to skip schema setup.
func NewIsolatedDB(t *testing.T, adminDSN string, migrate func(*sql.DB) error) *sql.DB {
	t.Helper()
	db, _ := NewIsolatedDBWithName(t, adminDSN, migrate)
	return db
}

// NewIsolatedDBWithName is NewIsolatedDB but also returns the carved
// database name. Useful in tests that want to verify the DB exists
// independently of the returned connection.
func NewIsolatedDBWithName(t *testing.T, adminDSN string, migrate func(*sql.DB) error) (*sql.DB, string) {
	t.Helper()
	if err := ValidateAdminDSN(adminDSN); err != nil {
		t.Fatalf("testkit: %v", err)
	}

	admin, err := sql.Open("postgres", adminDSN)
	if err != nil {
		t.Fatalf("testkit: open admin DSN: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	admin.SetMaxOpenConns(2)

	if err := pingWithRetry(admin); err != nil {
		t.Fatalf("testkit: cannot reach Postgres via adminDSN: %v", err)
	}

	dbName := newUniqueDBName(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE "`+dbName+`"`); err != nil {
		t.Fatalf("testkit: CREATE DATABASE %q: %v", dbName, err)
	}

	carvedDSN, err := rewriteDBName(adminDSN, dbName)
	if err != nil {
		dropCarvedDB(t, admin, dbName, err)
		t.Fatalf("testkit: rewrite carved DSN: %v", err)
	}
	carved, err := sql.Open("postgres", carvedDSN)
	if err != nil {
		dropCarvedDB(t, admin, dbName, err)
		t.Fatalf("testkit: open carved DB %q: %v", dbName, err)
	}
	if err := pingWithRetry(carved); err != nil {
		_ = carved.Close()
		dropCarvedDB(t, admin, dbName, err)
		t.Fatalf("testkit: ping carved DB %q: %v", dbName, err)
	}

	t.Cleanup(func() {
		_ = carved.Close()
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		// Terminate any lingering connections before drop — Postgres
		// rejects DROP DATABASE while sessions remain open.
		_, _ = admin.ExecContext(dropCtx,
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, dbName)
		if _, derr := admin.ExecContext(dropCtx, `DROP DATABASE IF EXISTS "`+dbName+`"`); derr != nil {
			// Surface drop failures via t.Errorf — silently swallowing
			// them turns every flaky drop into a leaked database that
			// accumulates across test runs.
			t.Errorf("testkit: leaked DB %q (drop failed): %v", dbName, derr)
		}
	})

	if migrate != nil {
		if err := migrate(carved); err != nil {
			t.Fatalf("testkit: migrate callback on %q: %v", dbName, err)
		}
	}
	return carved, dbName
}

// ValidateAdminDSN returns an error if the DSN is empty or obviously
// malformed. Exposed so tests can check the failure mode directly.
func ValidateAdminDSN(dsn string) error {
	if strings.TrimSpace(dsn) == "" {
		return fmt.Errorf("admin DSN is empty — set WTF_TEST_DATABASE_URL or pass a usable DSN")
	}
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return fmt.Errorf("admin DSN must use postgres:// scheme, got %q", redact(dsn))
	}
	return nil
}

func pingWithRetry(db *sql.DB) error {
	deadline := time.Now().Add(3 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		err := db.PingContext(ctx)
		cancel()
		if err == nil {
			return nil
		}
		last = err
		time.Sleep(100 * time.Millisecond)
	}
	return last
}

// newUniqueDBName returns a lowercase Postgres identifier safe to use
// as a database name. The test name supplies a debugging-friendly
// prefix; an 8-byte random suffix avoids collisions across parallel
// tests in the same process.
func newUniqueDBName(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("testkit: rand: %v", err)
	}
	suffix := hex.EncodeToString(buf)
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return -1
	}, t.Name())
	// Postgres identifiers cap at 63 bytes; keep prefix short.
	if len(clean) > 40 {
		clean = clean[:40]
	}
	return "ftest_" + clean + "_" + suffix
}

// rewriteDBName changes the database path component of a Postgres DSN
// while preserving everything else (user, password, host, options).
// Hard-fails on parse error or non-postgres scheme so the caller never
// connects to the wrong database — a silent fallthrough would point
// the carved connection at the admin DSN's database, where any
// migrations or fixture writes would land on the operator's
// maintenance DB.
func rewriteDBName(dsn, dbName string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("rewriteDBName: parse: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("rewriteDBName: expected postgres scheme, got %q in %s", u.Scheme, redact(dsn))
	}
	u.Path = "/" + dbName
	return u.String(), nil
}

// RewriteDBNameForTest exposes rewriteDBName to the external test
// package so the parse-failure contract can be asserted directly.
// Not intended for production callers.
func RewriteDBNameForTest(dsn, dbName string) (string, error) {
	return rewriteDBName(dsn, dbName)
}

// dropCarvedDB best-effort terminates lingering backends and drops the
// carved DB during an early-error path. Logs failures via t.Errorf so
// leaks are visible — but uses t.Errorf rather than t.Fatalf because
// the caller is about to t.Fatalf with the underlying err anyway.
func dropCarvedDB(t *testing.T, admin *sql.DB, dbName string, _ error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = admin.ExecContext(ctx,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, dbName)
	if _, derr := admin.ExecContext(ctx, `DROP DATABASE IF EXISTS "`+dbName+`"`); derr != nil {
		t.Errorf("testkit: leaked DB %q during error path (drop failed): %v", dbName, derr)
	}
}

func redact(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "<unparseable>"
	}
	if u.User != nil {
		u.User = url.UserPassword(u.User.Username(), "REDACTED")
	}
	return u.String()
}
