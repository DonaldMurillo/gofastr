package auth

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/migrate"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// probeConnector opens in-memory SQLite connections whose dialect probe
// (SELECT version()) it counts and can answer as Postgres or fail as a
// dropped connection. Every other statement reaches SQLite.
type probeConnector struct {
	base   driver.Driver
	probes atomic.Int32
	mode   atomic.Value // "", "postgres" or "fail"
}

func newProbeDB(t *testing.T) (*sql.DB, *probeConnector) {
	t.Helper()
	open, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	c := &probeConnector{base: open.Driver()}
	_ = open.Close()
	c.mode.Store("")
	db := sql.OpenDB(c)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db, c
}

func (c *probeConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.base.Open(":memory:")
	if err != nil {
		return nil, err
	}
	return probeConn{conn, c}, nil
}

func (c *probeConnector) Driver() driver.Driver { return c.base }

type probeConn struct {
	driver.Conn
	c *probeConnector
}

func (p probeConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "version()") {
		p.c.probes.Add(1)
		switch p.c.mode.Load() {
		case "postgres":
			return &versionRow{v: "PostgreSQL 16.0"}, nil
		case "fail":
			return nil, errors.New("read: connection reset by peer")
		}
	}
	if qc, ok := p.Conn.(driver.QueryerContext); ok {
		return qc.QueryContext(ctx, q, args)
	}
	return nil, driver.ErrSkip
}

func (p probeConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if ec, ok := p.Conn.(driver.ExecerContext); ok {
		return ec.ExecContext(ctx, q, args)
	}
	return nil, driver.ErrSkip
}

type versionRow struct {
	v    string
	done bool
}

func (r *versionRow) Columns() []string { return []string{"version"} }
func (r *versionRow) Close() error      { return nil }
func (r *versionRow) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.v
	return nil
}

// Every auth SQL store probed the dialect with SELECT version() on each
// use: six times per user-store boot and once per OAuth unlink. A store
// resolves it once and reuses the answer.
func TestStoreProbesDialectOnce(t *testing.T) {
	db, c := newProbeDB(t)
	ctx := context.Background()
	us := NewEntityUserStore(db, "users")
	if err := us.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := us.CreateUser(ctx, "a@x.test", "hash", []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if _, err := us.UnlinkOAuthGuarded(ctx, u.GetID(), "github"); err != nil {
			t.Fatal(err)
		}
	}
	if n := c.probes.Load(); n != 1 {
		t.Fatalf("user store probed the dialect %d times over boot and 10 unlinks, want 1", n)
	}
	ss := NewEntitySessionStore(db, "sessions")
	if err := ss.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ss.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if n := c.probes.Load(); n != 2 {
		t.Fatalf("session store probed %d times over two boots, want 1", n-1)
	}
}

// A per-call probe that failed mid-request read as SQLite, so a Postgres
// store took the SQLite unlink path (no row lock) for that one call. Once
// resolved, a store keeps its dialect.
func TestFailedProbeKeepsDialect(t *testing.T) {
	db, c := newProbeDB(t)
	us := NewEntityUserStore(db, "users")
	c.mode.Store("postgres")
	if d := us.sqlDialect(); d != migrate.DialectPostgres {
		t.Fatalf("first resolution = %s, want postgres", d)
	}
	c.mode.Store("fail")
	if d := us.sqlDialect(); d != migrate.DialectPostgres {
		t.Fatalf("after a failing probe = %s, want postgres kept", d)
	}
	if n := c.probes.Load(); n != 1 {
		t.Fatalf("probes = %d, want 1", n)
	}
}

// A database the probe cannot reach reads as SQLite, as before, and that
// guess is not kept: the next call probes again and finds Postgres.
func TestFailedFirstProbeIsNotKept(t *testing.T) {
	db, c := newProbeDB(t)
	us := NewEntityUserStore(db, "users")
	c.mode.Store("fail")
	if d := us.sqlDialect(); d != migrate.DialectSQLite {
		t.Fatalf("unreachable probe = %s, want the sqlite fallback", d)
	}
	c.mode.Store("postgres")
	if d := us.sqlDialect(); d != migrate.DialectPostgres {
		t.Fatalf("after the database answered = %s, want postgres", d)
	}
}

// The stores the probe-count test does not drive (2FA, passkey, magic
// link, API token, service account, OAuth token) go through the same
// cache: none probes per call.
func TestStoresUseCachedDialect(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "dialect.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "migrate.DetectDialect(") {
			t.Errorf("%s probes the dialect per call; use the store's storeDialect", f)
		}
	}
}
