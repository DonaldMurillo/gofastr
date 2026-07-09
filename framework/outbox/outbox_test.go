package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// openOutbox returns a fresh in-memory SQLite + Outbox pair. Cleanup is
// registered via t.Cleanup. SQLite with MaxOpenConns(1) serialises
// writers on the single in-memory page — the same setup battery/queue's
// tests use.
func openOutbox(t *testing.T, opts ...Option) (*sql.DB, *Outbox) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	o, err := New(db, opts...)
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	return db, o
}

// mustList is a fail-fast List for tests that don't care about the error.
func mustList(t *testing.T, o *Outbox, status string, limit int) []Row {
	t.Helper()
	rows, err := o.List(context.Background(), status, limit)
	if err != nil {
		t.Fatalf("list(%q): %v", status, err)
	}
	return rows
}

// findRow returns the row with id from a List result, or fails.
func findRow(t *testing.T, rows []Row, id string) Row {
	t.Helper()
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("row %s not found in %d rows", id, len(rows))
	return Row{}
}

// ---------------------------------------------------------------------------
// Append: commit persists, rollback drops.
// ---------------------------------------------------------------------------

func TestAppend_CommitPersists(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	id, err := o.Append(ctx, tx, "user.created", map[string]any{"id": 42, "name": "ada"})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if len(id) != 32 {
		t.Fatalf("id len = %d, want 32 (16-byte hex)", len(id))
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	rows := mustList(t, o, "pending", 0)
	if len(rows) != 1 {
		t.Fatalf("got %d pending rows, want 1", len(rows))
	}
	r := findRow(t, rows, id)
	if r.Type != "user.created" {
		t.Errorf("type = %q", r.Type)
	}
	if r.Status != "pending" {
		t.Errorf("status = %q", r.Status)
	}
	if r.Attempts != 0 {
		t.Errorf("attempts = %d, want 0", r.Attempts)
	}
	if r.DispatchedAt != nil {
		t.Error("DispatchedAt should be nil for a pending row")
	}
	if !strings.Contains(string(r.Payload), `"ada"`) {
		t.Errorf("payload = %s, want JSON containing name", r.Payload)
	}
	if !strings.Contains(string(r.Payload), `"id":42`) {
		t.Errorf("payload = %s, want JSON containing id", r.Payload)
	}
}

func TestAppend_RollbackDrops(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := o.Append(ctx, tx, "x", "data"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if rows := mustList(t, o, "", 0); len(rows) != 0 {
		t.Fatalf("got %d rows after rollback, want 0", len(rows))
	}
}

func TestAppend_EmptyDataBecomesNull(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "e", nil)
	tx.Commit()

	r := findRow(t, mustList(t, o, "pending", 0), id)
	if string(r.Payload) != "null" {
		t.Errorf("payload = %q, want \"null\"", string(r.Payload))
	}
}

// ---------------------------------------------------------------------------
// New: invalid table name → error (no SQL executed with the bad name).
// ---------------------------------------------------------------------------

func TestNew_InvalidTable(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = New(db, WithTable("bad name; drop"))
	if err == nil {
		t.Fatal("expected error for invalid table name, got nil")
	}
}

func TestNew_CustomTable(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := New(db, WithTable("my_outbox")); err != nil {
		t.Fatalf("New with custom table: %v", err)
	}
	// Table + index exist.
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='my_outbox'`).Scan(&n); err != nil {
		t.Fatalf("query master: %v", err)
	}
	if n != 1 {
		t.Errorf("custom table not created (count=%d)", n)
	}
}

// WithoutEnsureTable must skip the boot CREATE TABLE, leaving construction
// clean but the table absent until the operator's own migration creates it.
func TestNew_WithoutEnsureTable(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	o, err := New(db, WithoutEnsureTable())
	if err != nil {
		t.Fatalf("New(WithoutEnsureTable): %v", err)
	}
	// The table must NOT have been created.
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='event_outbox'`).Scan(&n); err != nil {
		t.Fatalf("query master: %v", err)
	}
	if n != 0 {
		t.Fatalf("event_outbox created despite WithoutEnsureTable (count=%d)", n)
	}
	// Append must then fail fast (no table) rather than silently swallow.
	tx, _ := db.BeginTx(context.Background(), nil)
	defer tx.Rollback()
	if _, err := o.Append(context.Background(), tx, "t", map[string]any{"k": "v"}); err == nil {
		t.Fatal("Append succeeded with no table; want a failure")
	}
}

// ---------------------------------------------------------------------------
// List: filters by status; empty status returns all.
// ---------------------------------------------------------------------------

func TestList_FilterByStatus(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	var ids [3]string
	for i := range ids {
		tx, _ := db.BeginTx(ctx, nil)
		id, _ := o.Append(ctx, tx, "t", i)
		tx.Commit()
		ids[i] = id
	}
	exec(t, db, o, "UPDATE %s SET status='dispatched' WHERE id=?", ids[0])
	exec(t, db, o, "UPDATE %s SET status='dead' WHERE id=?", ids[1])

	if n := len(mustList(t, o, "pending", 0)); n != 1 {
		t.Errorf("pending = %d, want 1", n)
	}
	if n := len(mustList(t, o, "dispatched", 0)); n != 1 {
		t.Errorf("dispatched = %d, want 1", n)
	}
	if n := len(mustList(t, o, "dead", 0)); n != 1 {
		t.Errorf("dead = %d, want 1", n)
	}
	if n := len(mustList(t, o, "", 0)); n != 3 {
		t.Errorf("all = %d, want 3", n)
	}
}

// ---------------------------------------------------------------------------
// Replay: dead → pending; pending/dispatched/unknown → no-op.
// ---------------------------------------------------------------------------

func TestReplay_DeadRow(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()
	exec(t, db, o, "UPDATE %s SET status='dead', attempts=5, last_error='boom' WHERE id=?", id)

	if err := o.Replay(ctx, id); err != nil {
		t.Fatalf("replay: %v", err)
	}
	r := findRow(t, mustList(t, o, "pending", 0), id)
	if r.Attempts != 0 {
		t.Errorf("attempts = %d, want 0 after replay", r.Attempts)
	}
	if r.LastError != "" {
		t.Errorf("last_error = %q, want cleared", r.LastError)
	}
}

func TestReplay_PendingIsNoop(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()

	if err := o.Replay(ctx, id); err != nil {
		t.Fatalf("replay pending: %v", err)
	}
	// Still exactly one pending row (untouched).
	if n := len(mustList(t, o, "pending", 0)); n != 1 {
		t.Errorf("pending rows = %d, want 1 (replay must be a no-op)", n)
	}
}

func TestReplay_DispatchedIsNoop(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()
	exec(t, db, o, "UPDATE %s SET status='dispatched' WHERE id=?", id)

	if err := o.Replay(ctx, id); err != nil {
		t.Fatalf("replay dispatched: %v", err)
	}
	if n := len(mustList(t, o, "dispatched", 0)); n != 1 {
		t.Errorf("dispatched = %d, want 1", n)
	}
	if n := len(mustList(t, o, "pending", 0)); n != 0 {
		t.Errorf("pending = %d, want 0 (dispatched row must not resurrect)", n)
	}
}

func TestReplay_UnknownIsNoop(t *testing.T) {
	_, o := openOutbox(t)
	if err := o.Replay(context.Background(), "does-not-exist"); err != nil {
		t.Fatalf("replay unknown: %v", err)
	}
}

// ---------------------------------------------------------------------------
// backoffFor: exponential growth capped at max.
// ---------------------------------------------------------------------------

func TestBackoffFor(t *testing.T) {
	_, o := openOutbox(t)
	o.backoffBase = time.Second
	o.backoffMax = 8 * time.Second

	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{1, time.Second},      // base * 2^0
		{2, 2 * time.Second},  // base * 2^1
		{3, 4 * time.Second},  // base * 2^2
		{4, 8 * time.Second},  // base * 2^3 = cap
		{5, 8 * time.Second},  // capped
		{20, 8 * time.Second}, // still capped
	}
	for _, c := range cases {
		if got := o.backoffFor(c.attempts); got != c.want {
			t.Errorf("backoffFor(%d) = %v, want %v", c.attempts, got, c.want)
		}
	}
}

// exec runs a one-shot SQL statement against the outbox table from a test.
func exec(t *testing.T, db *sql.DB, o *Outbox, tmpl, id string) {
	t.Helper()
	if _, err := db.Exec(fmt.Sprintf(tmpl, o.qt()), id); err != nil {
		t.Fatalf("exec: %v", err)
	}
}
