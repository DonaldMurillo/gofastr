package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

func setupOwnerCreateInProcHandler(t *testing.T) *CrudHandler {
	t.Helper()
	db := setupDB(t, `CREATE TABLE notes (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		title TEXT
	)`)
	ent := entity.Define("notes", entity.EntityConfig{Table: "notes",
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		}, Scope: &entity.ScopeConfig{OwnerField: "user_id"},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
}

func setupTenantInProcHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE docs (
		id TEXT PRIMARY KEY,
		tenant_id TEXT,
		title TEXT
	)`)
	ent := entity.Define("docs", entity.EntityConfig{Table: "docs",
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		}, Scope: &entity.ScopeConfig{MultiTenant: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "docs", []map[string]any{
		{"id": "doc-a", "tenant_id": "tenant-a", "title": "Alpha"},
		{"id": "doc-b", "tenant_id": "tenant-b", "title": "Beta"},
	})
	return ch, db
}

// TestCrud_AnonymousOwnerCreateRejected pins the contract that in-proc
// Create / BatchCreate against an OwnerField entity refuses anonymous
// callers. The HTTP path has middleware-level enforcement; in-process
// callers (typed repos, jobs, seed scripts) bypass that path and would
// otherwise write orphan rows.
func TestCrud_AnonymousOwnerCreateRejected(t *testing.T) {
	installOwnerExtractor(t)
	ch := setupOwnerCreateInProcHandler(t)

	if _, err := ch.CreateOne(context.Background(), map[string]any{"title": "hi"}); !errors.Is(err, errOwnerRequired) {
		t.Fatalf("CreateOne err=%v, want errOwnerRequired", err)
	}
	if _, err := ch.BatchCreateMany(context.Background(), []map[string]any{{"title": "hi"}}); !errors.Is(err, errOwnerRequired) {
		t.Fatalf("BatchCreateMany err=%v, want errOwnerRequired", err)
	}
}

// TestCrud_MissingTenantContextRejects pins fail-closed behaviour on
// every in-proc CRUD method touching a MultiTenant entity. The HTTP
// path uses tenant middleware to refuse; in-proc callers bypass that
// path and ApplyTenantScope alone is fail-OPEN (no tenant ⇒ no WHERE).
func TestCrud_MissingTenantContextRejects(t *testing.T) {
	ch, db := setupTenantInProcHandler(t)
	ctx := context.Background()

	if _, err := ch.GetOne(ctx, "doc-a", nil); err == nil {
		t.Fatalf("GetOne without tenant context returned no error")
	}
	if rows, err := ch.ListAll(ctx, ListOptions{}); err == nil && len(rows) > 0 {
		t.Fatalf("ListAll without tenant context returned rows: %+v", rows)
	}
	if n, err := ch.CountAll(ctx, ListOptions{}); err == nil && n != 0 {
		t.Fatalf("CountAll without tenant context returned %d", n)
	}
	if _, err := ch.UpdateOne(ctx, "doc-a", map[string]any{"title": "tampered"}); err == nil {
		t.Fatalf("UpdateOne without tenant context returned no error")
	}
	if err := ch.DeleteOne(ctx, "doc-a"); err == nil {
		t.Fatalf("DeleteOne without tenant context returned no error")
	}
	if _, err := ch.BatchUpdateMany(ctx, []string{"doc-a"}, []map[string]any{{"title": "x"}}); err == nil {
		t.Fatalf("BatchUpdateMany without tenant context returned no error")
	}
	if _, err := ch.BatchDeleteMany(ctx, []string{"doc-a"}); err == nil {
		t.Fatalf("BatchDeleteMany without tenant context returned no error")
	}

	// Sanity: the rows themselves were not mutated.
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM docs").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("rejected operations still mutated rows; count=%d", n)
	}
}

// setupOwnerReadInProcHandler builds an OwnerField entity seeded with two
// owners' rows, used to assert in-proc read/update/delete fail closed
// without an owner in context.
func setupOwnerReadInProcHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE onotes (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		title TEXT
	)`)
	ent := entity.Define("onotes", entity.EntityConfig{Table: "onotes",
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		}, Scope: &entity.ScopeConfig{OwnerField: "user_id"},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "onotes", []map[string]any{
		{"id": "note-a", "user_id": "alice", "title": "Alpha"},
		{"id": "note-b", "user_id": "bob", "title": "Beta"},
	})
	return ch, db
}

// TestCrud_MissingOwnerContextRejects pins fail-closed behaviour on every
// in-proc read/update/delete method touching an OwnerField entity. The
// HTTP twins enforce RequireOwner; the in-proc methods previously called
// only ApplyOwnerScope, which is fail-OPEN (no owner ⇒ no WHERE), so an
// anonymous context spanned every owner's rows.
func TestCrud_MissingOwnerContextRejects(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerReadInProcHandler(t)
	ctx := context.Background() // no owner in context

	if _, err := ch.GetOne(ctx, "note-b", nil); !errors.Is(err, errOwnerRequired) {
		t.Fatalf("GetOne err=%v, want errOwnerRequired (anonymous read of another owner's row)", err)
	}
	if rows, err := ch.ListAll(ctx, ListOptions{}); !errors.Is(err, errOwnerRequired) {
		t.Fatalf("ListAll err=%v rows=%+v, want errOwnerRequired (anonymous list spans all owners)", err, rows)
	}
	if n, err := ch.CountAll(ctx, ListOptions{}); !errors.Is(err, errOwnerRequired) {
		t.Fatalf("CountAll err=%v n=%d, want errOwnerRequired", err, n)
	}
	if _, err := ch.UpdateOne(ctx, "note-b", map[string]any{"title": "pwned"}); !errors.Is(err, errOwnerRequired) {
		t.Fatalf("UpdateOne err=%v, want errOwnerRequired", err)
	}
	if err := ch.DeleteOne(ctx, "note-b"); !errors.Is(err, errOwnerRequired) {
		t.Fatalf("DeleteOne err=%v, want errOwnerRequired", err)
	}
	if _, err := ch.BatchUpdateMany(ctx, []string{"note-b"}, []map[string]any{{"title": "x"}}); !errors.Is(err, errOwnerRequired) {
		t.Fatalf("BatchUpdateMany err=%v, want errOwnerRequired", err)
	}
	if _, err := ch.BatchDeleteMany(ctx, []string{"note-b"}); !errors.Is(err, errOwnerRequired) {
		t.Fatalf("BatchDeleteMany err=%v, want errOwnerRequired", err)
	}

	// Sanity: no rows mutated/removed by the rejected operations.
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM onotes").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("rejected operations still mutated rows; count=%d", n)
	}
	var title string
	if err := db.QueryRow("SELECT title FROM onotes WHERE id = $1", "note-b").Scan(&title); err != nil {
		t.Fatalf("read title: %v", err)
	}
	if title != "Beta" {
		t.Fatalf("bob's row was mutated by anonymous in-proc update; title=%q", title)
	}
}

// TestParseScopedFilters_CapsInListSize keeps a defensible cap on
// `field_in=a|b|...` so a single request can't blow up a JOIN with
// thousands of bind parameters.
func TestParseScopedFilters_CapsInListSize(t *testing.T) {
	values := ""
	for i := range maxScopedINEntries + 1 {
		if i > 0 {
			values += "|"
		}
		values += "v"
	}
	if _, err := parseScopedFilters("id_in="+values, nil, "comments"); err == nil {
		t.Fatalf("parseScopedFilters accepted IN list of %d entries (cap %d)", maxScopedINEntries+1, maxScopedINEntries)
	}
}

// TestAmbientTxRollbackWithholdsEvents pins that no lifecycle event is
// published for a write whose ambient transaction rolled back. The HTTP
// response lane of this property is pinned by the scrubRolledBackData
// tests (batch_error_leak_security_test.go); the durable outbox lane rolls
// back with the tx by construction (StageEvent writes in-tx). This test
// covers the remaining lane: the live bus feeding SSE EventStream and
// On/Subscribe handlers.
//
// Composition follows the documented pattern (docs/content/
// hooks-and-transactions.md): CRUD operations inside one ambient tx whose
// owner (the caller) commits or rolls back. inTx's ambient branch
// (tx.go) joins the outer tx and returns pre-commit, so EmitEvent must
// not fire until that outer tx has actually committed.
func TestAmbientTxRollbackWithholdsEvents(t *testing.T) {
	ch, dbc := covNotesHandler(t)
	bus := event.NewEventBus()
	ch.Events = bus

	got := make(chan event.Event, 8)
	cancel := bus.Subscribe(event.EntityCreated, func(_ context.Context, ev event.Event) error {
		got <- ev
		return nil
	})
	defer cancel()

	tx, err := dbc.Begin()
	if err != nil {
		t.Fatal(err)
	}
	ctx := db.WithTx(context.Background(), tx)

	// First write succeeds inside the ambient tx.
	if _, err := ch.CreateOne(ctx, map[string]any{"title": "phantom"}); err != nil {
		t.Fatalf("first CreateOne: %v", err)
	}
	// Second write fails validation (required title missing) → the caller
	// rolls the whole ambient tx back.
	if _, err := ch.CreateOne(ctx, map[string]any{"body": "no title"}); err == nil {
		t.Fatal("expected second CreateOne to fail on missing required title")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := dbc.QueryRow("SELECT COUNT(*) FROM notes").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rollback failed: %d rows persisted", n)
	}

	select {
	case ev := <-got:
		t.Fatalf("SECURITY: [event-integrity] EntityCreated delivered on the live bus for a write the ambient transaction rolled back: %v. Property: no lifecycle event is published for a write that did not commit. Attack impact: SSE subscribers and On/Subscribe handlers observe a phantom row (full record payload) that never existed. Fix: commit-gate EmitEvent — when db.TxFromContext finds an uncommitted ambient tx, enqueue the emission on a post-commit callback queue drained by the tx owner instead of firing immediately.", ev.Data)
	case <-time.After(time.Second):
		// Fixed behaviour: nothing published for an uncommitted write.
	}
}

// TestAmbientTxRollbackWithholdsUpdateEvents is the UPDATE arm of the same
// property, and it is a distinct case: a rolled-back create leaves no row,
// so row presence alone answers "did this land?", but a rolled-back update
// leaves the row exactly as present as a committed one. Gating on presence
// would publish an EntityUpdated carrying values the database never held.
func TestAmbientTxRollbackWithholdsUpdateEvents(t *testing.T) {
	ch, dbc := covNotesHandler(t)
	bus := event.NewEventBus()

	// Seed a committed row OUTSIDE the ambient tx, so the update below has
	// something that survives the rollback.
	created, err := ch.CreateOne(context.Background(), map[string]any{"title": "original"})
	if err != nil {
		t.Fatalf("seed CreateOne: %v", err)
	}
	id := created[ch.convertKey(ch.PrimaryKey)]

	// Subscribe only now, so the seed's own event is not in the channel.
	ch.Events = bus
	got := make(chan event.Event, 8)
	cancel := bus.Subscribe(event.EntityUpdated, func(_ context.Context, ev event.Event) error {
		got <- ev
		return nil
	})
	defer cancel()

	tx, err := dbc.Begin()
	if err != nil {
		t.Fatal(err)
	}
	ctx := db.WithTx(context.Background(), tx)
	if _, err := ch.UpdateOne(ctx, fmt.Sprint(id), map[string]any{"title": "phantom-edit"}); err != nil {
		t.Fatalf("UpdateOne: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	var title string
	if err := dbc.QueryRow("SELECT title FROM notes WHERE id = ?", id).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "original" {
		t.Fatalf("rollback failed: title = %q, want %q", title, "original")
	}

	select {
	case ev := <-got:
		t.Fatalf("SECURITY: [event-integrity] EntityUpdated delivered for an edit the ambient transaction rolled back: %v. The row survives a rolled-back update, so its presence cannot be the gate; the emitted values must be matched against what the database actually holds.", ev.Data)
	case <-time.After(time.Second):
		// Fixed behaviour: nothing published for an uncommitted edit.
	}
}

// TestAmbientTxCommitEmitsEvents is the happy-path pin for the same
// property: a write whose ambient transaction COMMITS publishes its
// lifecycle event exactly once, and the row is durable.
func TestAmbientTxCommitEmitsEvents(t *testing.T) {
	ch, dbc := covNotesHandler(t)
	bus := event.NewEventBus()
	ch.Events = bus

	got := make(chan event.Event, 8)
	cancel := bus.Subscribe(event.EntityCreated, func(_ context.Context, ev event.Event) error {
		got <- ev
		return nil
	})
	defer cancel()

	tx, err := dbc.Begin()
	if err != nil {
		t.Fatal(err)
	}
	ctx := db.WithTx(context.Background(), tx)
	if _, err := ch.CreateOne(ctx, map[string]any{"title": "durable"}); err != nil {
		t.Fatalf("CreateOne: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := dbc.QueryRow("SELECT COUNT(*) FROM notes").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("committed rows = %d, want 1", n)
	}

	select {
	case ev := <-got:
		data, ok := ev.Data.(map[string]any)
		if !ok || data[eventKeyEntity] != "notes" {
			t.Errorf("EntityCreated payload malformed: %v", ev.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("EntityCreated not delivered after ambient tx commit")
	}
	select {
	case ev := <-got:
		t.Errorf("duplicate EntityCreated delivered: %v", ev.Data)
	case <-time.After(200 * time.Millisecond):
		// Exactly once, as documented.
	}
}

// TestAmbientTxQueuePublishesOnDrainOnly pins the commit-queue path the
// framework-owned tx wrappers use (db.WithTxQueue): the emission for a
// write joined to the tx is staged on the queue — NOT probed against the
// live *sql.Tx, which raced the owner's own statements on the
// transaction's single connection (#353) — and fires exactly when the
// owner drains after commit.
func TestAmbientTxQueuePublishesOnDrainOnly(t *testing.T) {
	ch, dbc := covNotesHandler(t)
	bus := event.NewEventBus()
	ch.Events = bus

	got := make(chan event.Event, 8)
	cancel := bus.Subscribe(event.EntityCreated, func(_ context.Context, ev event.Event) error {
		got <- ev
		return nil
	})
	defer cancel()

	tx, err := dbc.Begin()
	if err != nil {
		t.Fatal(err)
	}
	ctx, queue := db.WithTxQueue(context.Background(), tx)
	if _, err := ch.CreateOne(ctx, map[string]any{"title": "queued"}); err != nil {
		t.Fatalf("CreateOne: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Committed but not yet drained: the emission must still be held.
	select {
	case ev := <-got:
		t.Fatalf("EntityCreated published before the tx owner drained the queue: %v", ev.Data)
	case <-time.After(300 * time.Millisecond):
	}

	queue.RunAfterCommit()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("EntityCreated not delivered after the queue drained")
	}
}

// TestAmbientFallbackEmitsWithoutLiveTx pins #367: the two
// emitAfterAmbientTx fallback branches (handler bound to a tx; record
// with no extractable primary key) publish immediately, and EmitAsync
// hands the context to a goroutine per subscriber. That context must NOT
// still carry the live *sql.Tx — a subscriber following the documented
// TxFromContext pattern would otherwise run statements on the
// transaction's single connection beside its owner, the exact race the
// commit queue removed from the framework paths.
func TestAmbientFallbackEmitsWithoutLiveTx(t *testing.T) {
	ch, dbc := covNotesHandler(t)
	bus := event.NewEventBus()
	ch.Events = bus

	sawTx := make(chan bool, 8)
	cancel := bus.Subscribe(event.EntityCreated, func(ctx context.Context, _ event.Event) error {
		_, ok := db.TxFromContext(ctx)
		sawTx <- ok
		return nil
	})
	defer cancel()

	tx, err := dbc.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ctx := db.WithTx(context.Background(), tx)

	// Branch 1: the handler itself is tx-bound, so there is no base
	// connection to confirm against and the emission fires immediately.
	txCh := *ch
	txCh.DB = tx
	txCh.EmitEvent(ctx, event.EntityCreated, map[string]any{"id": "r1", "title": "x"})

	// Branch 2: no extractable primary key, same immediate publish.
	ch.EmitEvent(ctx, event.EntityCreated, map[string]any{"title": "no id here"})

	for i := 0; i < 2; i++ {
		select {
		case ok := <-sawTx:
			if ok {
				t.Fatal("SECURITY: [#367] a live *sql.Tx reached an async bus subscriber's context — statements from the subscriber goroutine interleave with the transaction owner's on one connection")
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("fallback emission %d never reached the subscriber", i+1)
		}
	}
}

// TestInTxRollsBackWhenFnPanics pins the deferred rollback in crud's inTx:
// a panic inside fn must not leak the transaction's pooled connection.
// (Hook panics are recovered into errors by the hook registry, so this
// drives inTx directly — the defer guards whatever future code panics
// inside the closure.) App.InTx has carried the same guard, and the same
// test, since it was written; crud's inTx did not.
func TestInTxRollsBackWhenFnPanics(t *testing.T) {
	ch, dbc := covNotesHandler(t)
	dbc.SetMaxOpenConns(1)

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic did not propagate out of inTx")
			}
		}()
		_ = ch.inTx(context.Background(), func(context.Context, *CrudHandler) error {
			panic("boom inside the transaction")
		})
	}()

	// Before the deferred rollback, the abandoned tx held the pool's only
	// connection forever and this query blocked until the deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var n int
	if err := dbc.QueryRowContext(ctx, "SELECT COUNT(*) FROM notes").Scan(&n); err != nil {
		t.Fatalf("connection still held after a panicking fn: %v", err)
	}
}
