// Package pgtest provides a shared real-Postgres test harness usable from any
// package in the module (core/migrate, cmd/gofastr, …) without importing
// framework/internal/testdb, which is import-restricted to the framework tree.
//
// Resolution order (memoised once per process):
//  1. TEST_POSTGRES_DSN env var — point CI at an existing server.
//  2. testcontainers postgres:16-alpine — spun up on demand (needs Docker).
//  3. neither reachable → tests call t.Skip via DB/DSN.
//
// Each DB(t) hands back a connection scoped to a unique schema (search_path),
// so concurrent tests don't collide, with cleanup registered on t.
package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

var (
	once     sync.Once
	baseDSN  string
	resolveErr error
	using    string
	schemaSeq atomic.Int64
	logged   atomic.Bool
)

// resolve returns a base DSN to a working Postgres, or an error describing why
// one isn't reachable. Memoised across the whole test process.
func resolve() (string, error) {
	once.Do(func() {
		if dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN")); dsn != "" {
			baseDSN, using = dsn, "env"
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		c, err := postgres.Run(ctx, "postgres:16-alpine",
			postgres.WithDatabase("pgtest"),
			postgres.WithUsername("test"),
			postgres.WithPassword("test"),
		)
		if err != nil {
			resolveErr = fmt.Errorf("testcontainers postgres: %w", err)
			return
		}
		dsn, err := c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			resolveErr = err
			return
		}
		baseDSN, using = dsn, "container"
		// The container is intentionally not terminated: it is shared across
		// every test in the process and reaped when the test binary exits.
	})
	return baseDSN, resolveErr
}

// BaseDSN returns the resolved base DSN (the maintenance/default database), or
// skips the test if Postgres is unreachable. Use for EnsureDatabase / CLI
// round-trips that need a raw connection string.
func BaseDSN(t *testing.T) string {
	t.Helper()
	dsn, err := resolve()
	if err != nil {
		t.Skipf("Postgres unavailable: %v", err)
	}
	if logged.CompareAndSwap(false, true) {
		t.Logf("pgtest using %s", using)
	}
	return dsn
}

// DB returns a *sql.DB scoped to a fresh, uniquely-named schema (via
// search_path) on the shared Postgres, or skips if Postgres is unreachable.
// The schema and connection are dropped/closed on t.Cleanup.
func DB(t *testing.T) *sql.DB {
	t.Helper()
	base := BaseDSN(t)
	schema := fmt.Sprintf("pgt_%d_%d", os.Getpid(), schemaSeq.Add(1))
	// Set search_path via the connection-string `options` so it applies to
	// EVERY pooled connection — including ones the pool opens after a
	// ctx-cancel poisons the pinned conn. A session-level `SET search_path`
	// would be lost on that recycle, silently moving later queries to the
	// default schema.
	dsn, err := dsnWithSearchPath(base, schema)
	if err != nil {
		t.Skipf("pgtest.DB needs a URL-form base DSN, got %q (%v)", RedactDSN(base), err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	db.SetMaxOpenConns(1) // advisory-lock correctness on one session
	if err := ping(db); err != nil {
		db.Close()
		t.Fatalf("ping pg: %v", err)
	}
	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		db.Close()
		t.Fatalf("create schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP SCHEMA " + schema + " CASCADE")
		db.Close()
	})
	return db
}

func dsnWithSearchPath(base, schema string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" {
		return "", fmt.Errorf("not URL-form")
	}
	q := u.Query()
	q.Set("options", "-c search_path="+schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// FreshDatabaseDSN creates a uniquely-named database on the shared Postgres
// and returns a URL DSN pointing at it, dropped on t.Cleanup. Use for CLI /
// tooling tests that connect by URL string and expect to own the database
// (vs DB(t), which schema-scopes a shared one). Skips if Postgres is
// unreachable, or if the base DSN isn't URL-form.
func FreshDatabaseDSN(t *testing.T) string {
	t.Helper()
	base := BaseDSN(t)
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" {
		t.Skipf("pgtest.FreshDatabaseDSN needs a URL-form base DSN, got %q", RedactDSN(base))
	}
	admin, err := sql.Open("postgres", base)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	if err := ping(admin); err != nil {
		admin.Close()
		t.Fatalf("ping admin: %v", err)
	}
	name := fmt.Sprintf("clitest_%d_%d", os.Getpid(), schemaSeq.Add(1))
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		admin.Close()
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
		admin.Close()
	})
	u.Path = "/" + name
	return u.String()
}

// UnusedDSN returns a URL DSN pointing at a uniquely-named database that does
// NOT yet exist, plus a cleanup that drops it if something created it. Use to
// test database-creation paths (EnsureDatabase / migrate up --create-db).
// Skips if Postgres is unreachable or the base DSN isn't URL-form.
func UnusedDSN(t *testing.T) (string, func()) {
	t.Helper()
	base := BaseDSN(t)
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" {
		t.Skipf("pgtest.UnusedDSN needs a URL-form base DSN, got %q", RedactDSN(base))
	}
	name := fmt.Sprintf("created_%d_%d", os.Getpid(), schemaSeq.Add(1))
	u.Path = "/" + name
	drop := func() {
		admin, err := sql.Open("postgres", base)
		if err != nil {
			return
		}
		defer admin.Close()
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
	}
	return u.String(), drop
}

// RedactDSN masks the password in a DSN for safe logging.
func RedactDSN(dsn string) string {
	if u, err := url.Parse(dsn); err == nil && u.User != nil {
		if _, ok := u.User.Password(); ok {
			u.User = url.UserPassword(u.User.Username(), "xxx")
			return u.String()
		}
	}
	return dsn
}

func ping(db *sql.DB) error {
	var err error
	for i := 0; i < 30; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = db.PingContext(ctx)
		cancel()
		if err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return err
}
