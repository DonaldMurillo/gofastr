// Package testkit holds public test helpers for host apps' own tests.
package testkit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// NewIsolatedDB creates a fresh database on the Postgres cluster adminDSN
// (a postgres:// URL allowed to create databases) points at, runs migrate
// on it once unless nil, and drops it at cleanup. It fails the test, never
// skips it, when the DSN is missing or unusable.
func NewIsolatedDB(t *testing.T, adminDSN string, migrate func(*sql.DB) error) *sql.DB {
	t.Helper()
	db, _ := NewIsolatedDBWithName(t, adminDSN, migrate)
	return db
}

// NewIsolatedDBWithName is [NewIsolatedDB] that also returns the name.
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
		// best-effort: the checked DROP below reports any remaining leak.
		_, _ = admin.ExecContext(dropCtx,
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, dbName)
		if _, derr := admin.ExecContext(dropCtx, `DROP DATABASE IF EXISTS "`+dbName+`"`); derr != nil {
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

// ValidateAdminDSN errors on an empty DSN or one that is not postgres://.
func ValidateAdminDSN(dsn string) error {
	if strings.TrimSpace(dsn) == "" {
		return fmt.Errorf("admin DSN is empty: set WTF_TEST_DATABASE_URL or pass a usable DSN")
	}
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return fmt.Errorf("admin DSN must use postgres:// scheme, got scheme %q", dsnScheme(dsn))
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
	if len(clean) > 40 {
		clean = clean[:40]
	}
	return "ftest_" + clean + "_" + suffix
}

func rewriteDBName(dsn, dbName string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return "", fmt.Errorf("rewriteDBName: parse: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("rewriteDBName: expected postgres scheme, got %q", u.Scheme)
	}
	u.Path = "/" + dbName
	options := u.Query()
	options.Del("dbname")
	options.Del("database")
	u.RawQuery = options.Encode()
	return u.String(), nil
}

// RewriteDBNameForTest returns dsn pointed at dbName, for tests.
func RewriteDBNameForTest(dsn, dbName string) (string, error) {
	return rewriteDBName(dsn, dbName)
}

func dropCarvedDB(t *testing.T, admin *sql.DB, dbName string, _ error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// best-effort: the checked DROP below reports any remaining leak.
	_, _ = admin.ExecContext(ctx,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, dbName)
	if _, derr := admin.ExecContext(ctx, `DROP DATABASE IF EXISTS "`+dbName+`"`); derr != nil {
		t.Errorf("testkit: leaked DB %q during error path (drop failed): %v", dbName, derr)
	}
}

func dsnScheme(dsn string) string {
	scheme, _, ok := strings.Cut(dsn, "://")
	if !ok || scheme == "" || len(scheme) > 32 {
		return "none"
	}
	for i, c := range scheme {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.')) {
			return "none"
		}
	}
	return scheme
}
