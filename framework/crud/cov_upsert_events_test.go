package crud

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// covOwnerNotesHandler builds an owner-scoped notes table.
func covOwnerNotesHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	installOwnerExtractor(t)
	db := setupDB(t, `CREATE TABLE onotes (id TEXT PRIMARY KEY, user_id TEXT, title TEXT, deleted_at TEXT)`)
	ent := entity.Define("onotes", entity.EntityConfig{
		Name: "onotes", Table: "onotes", OwnerField: "user_id", SoftDelete: true,
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

func TestUpsert_RejectsAnonymousOnOwnerEntity(t *testing.T) {
	ch, _ := covOwnerNotesHandler(t)
	if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "n1", "title": "x"}); !errors.Is(err, errOwnerRequired) {
		t.Errorf("anonymous upsert err = %v, want errOwnerRequired", err)
	}
}

func TestUpsert_StampsOwnerAndStripsBodyOwner(t *testing.T) {
	ch, db := covOwnerNotesHandler(t)
	ctx := ctxWithUser("alice")
	// Body tries to forge user_id=bob; must be stripped & stamped to alice.
	res, err := ch.UpsertOne(ctx, map[string]any{"id": "n1", "title": "t", "user_id": "bob"})
	if err != nil {
		t.Fatalf("UpsertOne: %v", err)
	}
	if res["user_id"] != "alice" {
		t.Errorf("owner not stamped from ctx: %v", res["user_id"])
	}
	var uid string
	_ = db.QueryRow("SELECT user_id FROM onotes WHERE id='n1'").Scan(&uid)
	if uid != "alice" {
		t.Errorf("stored owner = %q, want alice", uid)
	}
}

func TestUpsert_RejectsForeignRowTakeover(t *testing.T) {
	ch, db := covOwnerNotesHandler(t)
	seedRows(t, db, "onotes", []map[string]any{{"id": "n1", "user_id": "bob", "title": "bob's"}})
	// Alice upserts n1 (owned by bob) → must fail closed.
	_, err := ch.UpsertOne(ctxWithUser("alice"), map[string]any{"id": "n1", "title": "hijack"})
	if !errors.Is(err, errUpsertForeignRow) {
		t.Fatalf("foreign-row upsert err = %v, want errUpsertForeignRow", err)
	}
	var title string
	_ = db.QueryRow("SELECT title FROM onotes WHERE id='n1'").Scan(&title)
	if title != "bob's" {
		t.Errorf("victim row mutated: %q", title)
	}
}

func TestUpsert_RejectsSoftDeletedResurrection(t *testing.T) {
	ch, db := covOwnerNotesHandler(t)
	seedRows(t, db, "onotes", []map[string]any{
		{"id": "n1", "user_id": "alice", "title": "gone", "deleted_at": "2026-01-01"},
	})
	_, err := ch.UpsertOne(ctxWithUser("alice"), map[string]any{"id": "n1", "title": "revive"})
	if !errors.Is(err, errSoftDeletedResurrection) {
		t.Fatalf("resurrection upsert err = %v, want errSoftDeletedResurrection", err)
	}
}

func TestUpsert_TenantMissing(t *testing.T) {
	db := setupDB(t, `CREATE TABLE tnotes (id TEXT PRIMARY KEY, tenant_id TEXT, title TEXT)`)
	ent := entity.Define("tnotes", entity.EntityConfig{
		Name: "tnotes", Table: "tnotes", MultiTenant: true,
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	var tme *tenantMissingError
	if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "n1", "title": "x"}); !errors.As(err, &tme) {
		t.Errorf("tenant-missing upsert err = %v, want tenantMissingError", err)
	}
}

func TestIsAutoField(t *testing.T) {
	ent := entity.Define("x", entity.EntityConfig{
		Name: "x", Table: "x",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	if !isAutoField(ent, "id") {
		t.Error("id should be auto field")
	}
	if isAutoField(ent, "name") {
		t.Error("name is not auto field")
	}
	if isAutoField(ent, "nonexistent") {
		t.Error("nonexistent is not auto field")
	}
}

func TestEmitEvent_NoBusIsNoop(t *testing.T) {
	ch, _ := covNotesHandler(t)
	// No Events set — must not panic.
	ch.EmitEvent(context.Background(), event.EntityCreated, map[string]any{"id": "1"})
}

func TestEmitEvent_StampsTenantAndOwner(t *testing.T) {
	installOwnerExtractor(t)
	db := setupDB(t, `CREATE TABLE ev (id TEXT PRIMARY KEY, user_id TEXT, tenant_id TEXT, body TEXT)`)
	ent := entity.Define("ev", entity.EntityConfig{
		Name: "ev", Table: "ev", OwnerField: "user_id", MultiTenant: true,
		Fields: []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "body", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	bus := event.NewEventBus()
	ch.Events = bus

	got := make(chan map[string]any, 1)
	cancel := bus.Subscribe(event.EntityCreated, func(_ context.Context, ev event.Event) error {
		got <- ev.Data.(map[string]any)
		return nil
	})
	defer cancel()

	ctx := tenant.SetTenantID(ctxWithUser("alice"), "tenantA")
	ch.EmitEvent(ctx, event.EntityCreated, map[string]any{"id": "1", "user_id": "alice"})

	select {
	case data := <-got:
		if data[eventKeyTenantID] != "tenantA" {
			t.Errorf("tenant not stamped: %v", data[eventKeyTenantID])
		}
		if data[eventKeyOwnerID] != "alice" {
			t.Errorf("owner not stamped: %v", data[eventKeyOwnerID])
		}
	case <-time.After(time.Second):
		t.Fatal("event not received")
	}
}

func TestEmitEvent_OwnerFromRecordFallback(t *testing.T) {
	// No owner in ctx (admin emitter) → owner extracted from record column.
	installOwnerExtractor(t)
	db := setupDB(t, `CREATE TABLE ev2 (id TEXT PRIMARY KEY, user_id TEXT, body TEXT)`)
	ent := entity.Define("ev2", entity.EntityConfig{
		Name: "ev2", Table: "ev2", OwnerField: "user_id",
		Fields: []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "body", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	bus := event.NewEventBus()
	ch.Events = bus

	got := make(chan map[string]any, 1)
	cancel := bus.Subscribe(event.EntityDeleted, func(_ context.Context, ev event.Event) error {
		got <- ev.Data.(map[string]any)
		return nil
	})
	defer cancel()

	ch.EmitEvent(context.Background(), event.EntityDeleted, map[string]any{"id": "1", "user_id": "carol"})
	select {
	case data := <-got:
		if data[eventKeyOwnerID] != "carol" {
			t.Errorf("owner-from-record fallback = %v, want carol", data[eventKeyOwnerID])
		}
	case <-time.After(time.Second):
		t.Fatal("event not received")
	}
}

func TestEventStream_NoBus(t *testing.T) {
	ch, _ := covNotesHandler(t)
	req := httptest.NewRequest("GET", "/notes/_events", nil)
	rec := httptest.NewRecorder()
	ch.EventStream()(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no-bus EventStream = %d, want 503", rec.Code)
	}
}

func TestEventStream_DeliversThenCancels(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := covOwnerNotesHandler(t)
	bus := event.NewEventBus()
	ch.Events = bus

	ctx, cancel := context.WithCancel(ctxWithUser("alice"))
	req := httptest.NewRequest("GET", "/onotes/_events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		ch.EventStream()(rec, req)
		close(done)
	}()

	// Give the subscription a moment, emit an alice event, then cancel.
	time.Sleep(50 * time.Millisecond)
	ch.EmitEvent(ctxWithUser("alice"), event.EntityCreated, map[string]any{"id": "n1", "user_id": "alice"})
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("EventStream did not return after context cancel")
	}
	body := rec.Body.String()
	if body == "" {
		t.Error("expected at least the subscribe comment in SSE body")
	}
}
