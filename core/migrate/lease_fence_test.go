package migrate

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// openSQLiteFile opens a pool on a SQLite file, as one process would.
func openSQLiteFile(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// syncBuffer is a log sink the heartbeat goroutine can write to while
// the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureSlog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func leaseExpiry(t *testing.T, db *sql.DB, table string) (int64, bool) {
	t.Helper()
	var at int64
	err := db.QueryRow(`SELECT expires_at FROM ` + table + ` WHERE id = 1`).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return at, true
}

// A holder that stalled past its lease lost it to another process. Its
// heartbeat and release used to act on the row by id alone: the renewal
// extended the new holder's lease and the release deleted it, letting a
// third runner in beside the second. Both are fenced on the expiry the
// holder last wrote now, and the stale holder logs the loss.
func TestStaleLeaseCannotTouchNewer(t *testing.T) {
	logs := captureSlog(t)
	path := filepath.Join(t.TempDir(), "lease.db")
	dbA, dbB := openSQLiteFile(t, path), openSQLiteFile(t, path)
	ctx := context.Background()
	const table = "_t_lock"

	releaseA, err := AcquireSQLiteLease(ctx, dbA, table, 3*time.Second, "test")
	if err != nil {
		t.Fatal(err)
	}
	// A stalls: its lease runs out without a renewal.
	if _, err := dbB.Exec(`UPDATE ` + table + ` SET expires_at = 0`); err != nil {
		t.Fatal(err)
	}
	// B's lease is long, so its own heartbeat stays quiet in this test.
	releaseB, err := AcquireSQLiteLease(ctx, dbB, table, 30*time.Second, "test")
	if err != nil {
		t.Fatal(err)
	}
	held, _ := leaseExpiry(t, dbB, table)

	time.Sleep(1300 * time.Millisecond) // A's heartbeat ticks at lease/3
	if got, ok := leaseExpiry(t, dbB, table); !ok || got != held {
		t.Fatalf("after the stale holder's heartbeat the lease row = %d (present %v), want B's %d", got, ok, held)
	}
	releaseA()
	if got, ok := leaseExpiry(t, dbB, table); !ok || got != held {
		t.Fatalf("after the stale holder's release the lease row = %d (present %v), want B's %d", got, ok, held)
	}
	if n := strings.Count(logs.String(), "lease lost"); n != 1 {
		t.Fatalf("the stale holder logged the loss %d times, want once:\n%s", n, logs.String())
	}
	releaseB()
	if _, ok := leaseExpiry(t, dbB, table); ok {
		t.Fatal("the holder's own release left the row")
	}
}

// The release half on its own: a stale holder that never renewed after
// losing the lease releases straight into the new holder's row.
func TestStaleReleaseKeepsNewerRow(t *testing.T) {
	logs := captureSlog(t)
	path := filepath.Join(t.TempDir(), "release.db")
	dbA, dbB := openSQLiteFile(t, path), openSQLiteFile(t, path)
	ctx := context.Background()
	const table = "_t_lock"

	// Long leases: neither heartbeat ticks during the test.
	releaseA, err := AcquireSQLiteLease(ctx, dbA, table, 30*time.Second, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dbB.Exec(`UPDATE ` + table + ` SET expires_at = 0`); err != nil {
		t.Fatal(err)
	}
	// A real steal happens a lease after A's last write, so B's expiry is
	// later than A's fence; a longer lease stands in for that wait here.
	releaseB, err := AcquireSQLiteLease(ctx, dbB, table, 40*time.Second, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseB()
	held, _ := leaseExpiry(t, dbB, table)
	releaseA()
	if got, ok := leaseExpiry(t, dbB, table); !ok || got != held {
		t.Fatalf("after the stale holder's release the lease row = %d (present %v), want B's %d", got, ok, held)
	}
	if !strings.Contains(logs.String(), "lease lost") {
		t.Fatalf("the stale release did not log the loss:\n%s", logs.String())
	}
}

// On a one-connection pool the runner pins the only connection, and the
// heartbeat, which renewed through the pool, could not run until the
// whole run returned: a run longer than the lease lost it and a second
// process ran migrations beside it. The runner renews on its pinned
// connection between migrations.
func TestPoolOneRunKeepsLease(t *testing.T) {
	prevLease := migrateLease
	migrateLease = 3 * time.Second
	t.Cleanup(func() { migrateLease = prevLease })

	path := filepath.Join(t.TempDir(), "pool1.db")
	db, other := openSQLiteFile(t, path), openSQLiteFile(t, path)
	db.SetMaxOpenConns(1)
	m := New(db, WithDialect(DialectSQLite))
	for v := uint64(1); v <= 4; v++ {
		if err := m.Register(Migration{Version: v, Name: "t", Up: "CREATE TABLE t" + string(rune('0'+v)) + " (id INTEGER)"}); err != nil {
			t.Fatal(err)
		}
	}
	// Each migration stands for a second of work: the four-second run
	// outlasts the three-second lease. During the last one another
	// process tries for 400ms. The lease counts whole seconds, so a
	// renewal can expire up to a second early: the widest gap the steal
	// sees, 1s + 400ms + 1s of rounding, stays inside the lease, and a
	// run that never renews loses it every time.
	var stolen bool
	testHookAfterMigration = func(mig Migration) {
		time.Sleep(time.Second)
		if mig.Version != 4 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
		defer cancel()
		if release, err := AcquireSQLiteLease(ctx, other, "_gofastr_migrate_lock", 3*time.Second, "other"); err == nil {
			stolen = true
			release()
		}
	}
	t.Cleanup(func() { testHookAfterMigration = nil })
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if stolen {
		t.Fatal("another process took the migrate lease in the middle of a run on a one-connection pool")
	}
}

// A runner that finds its lease taken stops before its next migration:
// the new holder may be applying the same pending set.
func TestLostLeaseStopsRun(t *testing.T) {
	captureSlog(t)
	db := openSQLiteFile(t, filepath.Join(t.TempDir(), "lost.db"))
	m := New(db, WithDialect(DialectSQLite))
	// The first migration moves the lease row, as a process that took
	// the expired lease would.
	for _, mig := range []Migration{
		{Version: 1, Name: "steal", Up: "UPDATE _gofastr_migrate_lock SET expires_at = expires_at + 1000"},
		{Version: 2, Name: "after", Up: "CREATE TABLE after_loss (id INTEGER)"},
	} {
		if err := m.Register(mig); err != nil {
			t.Fatal(err)
		}
	}
	err := m.Up(context.Background())
	if err == nil || !strings.Contains(err.Error(), "lease lost") {
		t.Fatalf("Up = %v, want the lost lease", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'after_loss'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the runner applied a migration after losing its lease")
	}
}

// Down holds the same lease and stops the same way.
func TestLostLeaseStopsRollback(t *testing.T) {
	captureSlog(t)
	db := openSQLiteFile(t, filepath.Join(t.TempDir(), "lostdown.db"))
	m := New(db, WithDialect(DialectSQLite))
	for _, mig := range []Migration{
		{Version: 1, Name: "keep", Up: "CREATE TABLE kept (id INTEGER)", Down: "DROP TABLE kept"},
		{Version: 2, Name: "steal", Up: "CREATE TABLE t2 (id INTEGER)",
			Down: "UPDATE _gofastr_migrate_lock SET expires_at = expires_at + 1000"},
	} {
		if err := m.Register(mig); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := m.Down(context.Background(), 2)
	if err == nil || !strings.Contains(err.Error(), "lease lost") {
		t.Fatalf("Down = %v, want the lost lease", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'kept'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("the runner rolled back a migration after losing its lease")
	}
}
