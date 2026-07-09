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
	// Default to a zero handler grace so completion/abandonment/drop happen
	// promptly in unit tests; grace-specific tests pass their own
	// WithHandlerGrace after this to override.
	o, err := New(db, append([]Option{WithHandlerGrace(0)}, opts...)...)
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

// mustDeliveries is a fail-fast ListDeliveries for tests.
func mustDeliveries(t *testing.T, o *Outbox, rowID string) []Delivery {
	t.Helper()
	ds, err := o.ListDeliveries(context.Background(), rowID)
	if err != nil {
		t.Fatalf("list deliveries %s: %v", rowID, err)
	}
	return ds
}

// findDelivery returns the delivery for consumer from a ListDeliveries
// result, or fails.
func findDelivery(t *testing.T, ds []Delivery, consumer string) Delivery {
	t.Helper()
	for _, d := range ds {
		if d.Consumer == consumer {
			return d
		}
	}
	t.Fatalf("delivery for consumer %q not found in %d deliveries", consumer, len(ds))
	return Delivery{}
}

// insertDelivery seeds a fresh delivery row directly (created_at = now), for
// tests that construct non-happy-path state without driving the relay.
// status defaults to pending when empty.
func insertDelivery(t *testing.T, db *sql.DB, o *Outbox, rowID, consumer, status string, attempts int, lastErr string) {
	t.Helper()
	insertDeliveryAged(t, db, o, rowID, consumer, status, attempts, lastErr, 0)
}

// insertDeliveryAged is insertDelivery with the delivery's created_at set
// `age` in the past, so tests can exercise the abandonment grace.
func insertDeliveryAged(t *testing.T, db *sql.DB, o *Outbox, rowID, consumer, status string, attempts int, lastErr string, age time.Duration) {
	t.Helper()
	if status == "" {
		status = "pending"
	}
	if _, err := db.Exec(fmt.Sprintf(
		`INSERT INTO %s (row_id, consumer, status, attempts, last_error, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		o.qd()), rowID, consumer, status, attempts, lastErr, o.now().UTC().Add(-age)); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}
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
	// Parent + child delivery tables both exist.
	for _, want := range []string{"my_outbox", "my_outbox_delivery"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, want).Scan(&n); err != nil {
			t.Fatalf("query master: %v", err)
		}
		if n != 1 {
			t.Errorf("table %q not created (count=%d)", want, n)
		}
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
	// Neither table must have been created.
	for _, want := range []string{"event_outbox", "event_outbox_delivery"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, want).Scan(&n); err != nil {
			t.Fatalf("query master: %v", err)
		}
		if n != 0 {
			t.Fatalf("table %q created despite WithoutEnsureTable (count=%d)", want, n)
		}
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

	if n := len(mustList(t, o, "pending", 0)); n != 2 {
		t.Errorf("pending = %d, want 2", n)
	}
	if n := len(mustList(t, o, "dispatched", 0)); n != 1 {
		t.Errorf("dispatched = %d, want 1", n)
	}
	if n := len(mustList(t, o, "", 0)); n != 3 {
		t.Errorf("all = %d, want 3", n)
	}
}

// ---------------------------------------------------------------------------
// Replay (per-consumer): resets dead deliveries of a row + reopens parent.
// ---------------------------------------------------------------------------

func TestReplay_ResurrectsAllDeadDeliveries(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()
	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()
	// Two dead deliveries + one already-dispatched sibling.
	insertDelivery(t, db, o, id, "a", "dead", 5, "boom")
	insertDelivery(t, db, o, id, "b", "dead", 5, "boom2")
	insertDelivery(t, db, o, id, "c", "dispatched", 1, "")
	if _, err := db.Exec(fmt.Sprintf("UPDATE %s SET status='dispatched', dispatched_at=? WHERE id=?", o.qt()),
		time.Now().UTC(), id); err != nil {
		t.Fatalf("set dispatched: %v", err)
	}
	if err := o.Replay(ctx, id); err != nil {
		t.Fatalf("replay: %v", err)
	}
	ds := mustDeliveries(t, o, id)
	a := findDelivery(t, ds, "a")
	if a.Status != "pending" || a.Attempts != 0 || a.LastError != "" {
		t.Errorf("delivery a = %+v, want pending/0/empty", a)
	}
	b := findDelivery(t, ds, "b")
	if b.Status != "pending" {
		t.Errorf("delivery b status = %q, want pending", b.Status)
	}
	c := findDelivery(t, ds, "c")
	if c.Status != "dispatched" {
		t.Errorf("delivery c status = %q, want dispatched (untouched)", c.Status)
	}
	// Parent reopened to pending.
	r := findRow(t, mustList(t, o, "pending", 0), id)
	if r.Status != "pending" {
		t.Errorf("parent status = %q, want pending", r.Status)
	}
}

func TestReplay_NoDeadDeliveriesIsNoop(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()
	insertDelivery(t, db, o, id, "a", "dispatched", 1, "")
	exec(t, db, o, "UPDATE %s SET status='dispatched' WHERE id=?", id)

	if err := o.Replay(ctx, id); err != nil {
		t.Fatalf("replay: %v", err)
	}
	// Parent stays dispatched (no dead deliveries to resurrect).
	if n := len(mustList(t, o, "pending", 0)); n != 0 {
		t.Errorf("pending = %d, want 0 (replay no-op)", n)
	}
}

func TestReplayConsumer_ResurrectsOneDeadDelivery(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()
	insertDelivery(t, db, o, id, "a", "dead", 5, "boom")
	insertDelivery(t, db, o, id, "b", "dead", 5, "boom2")
	exec(t, db, o, "UPDATE %s SET status='dispatched' WHERE id=?", id)

	if err := o.ReplayConsumer(ctx, id, "a"); err != nil {
		t.Fatalf("replay consumer: %v", err)
	}
	ds := mustDeliveries(t, o, id)
	a := findDelivery(t, ds, "a")
	if a.Status != "pending" || a.Attempts != 0 {
		t.Errorf("delivery a = %+v, want pending/0", a)
	}
	b := findDelivery(t, ds, "b")
	if b.Status != "dead" {
		t.Errorf("delivery b status = %q, want dead (untouched)", b.Status)
	}
	r := findRow(t, mustList(t, o, "pending", 0), id)
	if r.Status != "pending" {
		t.Errorf("parent = %q, want pending (reopened)", r.Status)
	}
}

func TestReplayConsumer_UnknownIsNoop(t *testing.T) {
	db, o := openOutbox(t)
	tx, _ := db.BeginTx(context.Background(), nil)
	id, _ := o.Append(context.Background(), tx, "t", nil)
	tx.Commit()
	if err := o.ReplayConsumer(context.Background(), id, "ghost"); err != nil {
		t.Fatalf("replay unknown consumer: %v", err)
	}
	if n := len(mustDeliveries(t, o, id)); n != 0 {
		t.Errorf("deliveries = %d, want 0", n)
	}
}

// Replay resurrects an ABANDONED delivery (a removed-then-re-added consumer),
// not just dead ones.
func TestReplay_ResurrectsAbandoned(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()
	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()
	insertDelivery(t, db, o, id, "svc", "abandoned", 0, "no consumer handler within grace")
	exec(t, db, o, "UPDATE %s SET status='dispatched' WHERE id=?", id)

	if err := o.ReplayConsumer(ctx, id, "svc"); err != nil {
		t.Fatalf("replay consumer: %v", err)
	}
	if got := findDelivery(t, mustDeliveries(t, o, id), "svc").Status; got != "pending" {
		t.Errorf("abandoned delivery after replay = %q, want pending", got)
	}
	if got := findRow(t, mustList(t, o, "pending", 0), id).Status; got != "pending" {
		t.Errorf("parent after replay = %q, want pending (reopened)", got)
	}
}

// WithRetention: the relay purges fully-settled (dispatched) parents + their
// deliveries once older than the window; pending/dead rows are never purged.
func TestRetention_PurgesDispatchedOnly(t *testing.T) {
	db, o := openOutbox(t, WithRetention(time.Hour))
	ctx := context.Background()

	// An old dispatched parent (should purge) and an old pending one (must stay).
	old := o.now().UTC().Add(-2 * time.Hour)
	insertParent := func(id, status string) {
		t.Helper()
		if _, err := db.Exec(fmt.Sprintf(
			"INSERT INTO %s (id, type, payload, status, created_at) VALUES (?, 't', 'null', ?, ?)",
			o.qt()), id, status, old); err != nil {
			t.Fatalf("insert parent %s: %v", id, err)
		}
	}
	insertParent("done", "dispatched")
	insertDeliveryAged(t, db, o, "done", "c", "dispatched", 0, "", 2*time.Hour)
	insertParent("live", "pending")

	if err := o.purgeExpired(ctx); err != nil {
		t.Fatalf("purge: %v", err)
	}
	rows := mustList(t, o, "", 0)
	if findRowMaybe(rows, "done") != nil {
		t.Error("dispatched parent past retention was not purged")
	}
	if findRowMaybe(rows, "live") == nil {
		t.Error("pending parent was wrongly purged")
	}
	if n := len(mustDeliveries(t, o, "done")); n != 0 {
		t.Errorf("purged parent's deliveries remain: %d", n)
	}
}

// findRowMaybe returns a pointer to the row with id, or nil.
func findRowMaybe(rows []Row, id string) *Row {
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// backoffFor: exponential growth capped at max.
// ---------------------------------------------------------------------------

func TestBackoffFor(t *testing.T) {
	_, o := openOutbox(t)
	o.backoffBase = 10 * time.Millisecond
	o.backoffMax = 100 * time.Millisecond
	cases := []struct {
		att  int
		want time.Duration
	}{
		{1, 10 * time.Millisecond},
		{2, 20 * time.Millisecond},
		{3, 40 * time.Millisecond},
		{4, 80 * time.Millisecond},
		{5, 100 * time.Millisecond}, // capped
		{9, 100 * time.Millisecond}, // still capped
	}
	for _, c := range cases {
		if got := o.backoffFor(c.att); got != c.want {
			t.Errorf("backoffFor(%d) = %v, want %v", c.att, got, c.want)
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
