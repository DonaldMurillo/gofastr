package crud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

func TestAfterCreateHookError(t *testing.T) {
	ch, _ := covNotesHandler(t)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error {
		return errors.New("after-create boom")
	})
	if _, err := ch.CreateOne(context.Background(), map[string]any{"title": "x"}); err == nil {
		t.Error("AfterCreate hook error should propagate")
	}
}

func TestBeforeUpdateHookError(t *testing.T) {
	ch, _ := covNotesHandler(t)
	created, _ := ch.CreateOne(context.Background(), map[string]any{"title": "x"})
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeUpdate, func(ctx context.Context, data any) error {
		return errors.New("before-update boom")
	})
	if _, err := ch.UpdateOne(context.Background(), created["id"].(string), map[string]any{"title": "y"}); err == nil {
		t.Error("BeforeUpdate hook error should propagate")
	}
}

func TestAfterUpdateHookError(t *testing.T) {
	ch, _ := covNotesHandler(t)
	created, _ := ch.CreateOne(context.Background(), map[string]any{"title": "x"})
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterUpdate, func(ctx context.Context, data any) error {
		return errors.New("after-update boom")
	})
	if _, err := ch.UpdateOne(context.Background(), created["id"].(string), map[string]any{"title": "y"}); err == nil {
		t.Error("AfterUpdate hook error should propagate")
	}
}

func TestBeforeDeleteHookError(t *testing.T) {
	ch, _ := covNotesHandler(t)
	created, _ := ch.CreateOne(context.Background(), map[string]any{"title": "x"})
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeDelete, func(ctx context.Context, data any) error {
		return errors.New("before-delete boom")
	})
	if err := ch.DeleteOne(context.Background(), created["id"].(string)); err == nil {
		t.Error("BeforeDelete hook error should propagate")
	}
}

func TestAfterDeleteHookError(t *testing.T) {
	ch, _ := covNotesHandler(t)
	created, _ := ch.CreateOne(context.Background(), map[string]any{"title": "x"})
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterDelete, func(ctx context.Context, data any) error {
		return errors.New("after-delete boom")
	})
	if err := ch.DeleteOne(context.Background(), created["id"].(string)); err == nil {
		t.Error("AfterDelete hook error should propagate")
	}
}

func TestUpsert_WithHooksDefaultsAutoFields(t *testing.T) {
	db := setupDB(t, `CREATE TABLE up (id TEXT PRIMARY KEY, title TEXT, status TEXT, code TEXT)`)
	ent := entity.Define("up", entity.EntityConfig{
		Name: "up", Table: "up",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "status", Type: schema.String, Default: "draft"},
			{Name: "code", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	beforeFired, afterFired := false, false
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeCreate, func(ctx context.Context, data any) error { beforeFired = true; return nil })
	ch.Hooks.RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error { afterFired = true; return nil })

	res, err := ch.UpsertOne(context.Background(), map[string]any{"id": "u1", "title": "t"})
	if err != nil {
		t.Fatalf("UpsertOne: %v", err)
	}
	if !beforeFired || !afterFired {
		t.Errorf("upsert hooks: before=%v after=%v", beforeFired, afterFired)
	}
	if res["status"] != "draft" {
		t.Errorf("default not applied: %v", res["status"])
	}
	if res["code"] == nil || res["code"] == "" {
		t.Errorf("auto code not generated: %v", res["code"])
	}
}

func TestUpsert_BeforeCreateHookError(t *testing.T) {
	ch, _ := covNotesHandler(t)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeCreate, func(ctx context.Context, data any) error {
		return errors.New("upsert before boom")
	})
	var bhe *beforeHookError
	if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "x", "title": "t"}); !errors.As(err, &bhe) {
		t.Errorf("upsert before-create hook err = %v, want beforeHookError", err)
	}
}

func TestUpsert_ValidationError(t *testing.T) {
	ch, _ := covNotesHandler(t) // title required
	var ve *validationError
	if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "x"}); !errors.As(err, &ve) {
		t.Errorf("upsert validation err = %v, want validationError", err)
	}
}

func TestUpsert_MultiTenantStamps(t *testing.T) {
	db := setupDB(t, `CREATE TABLE tup (id TEXT PRIMARY KEY, tenant_id TEXT, title TEXT)`)
	ent := entity.Define("tup", entity.EntityConfig{
		Name: "tup", Table: "tup", MultiTenant: true,
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ctx := tenant.SetTenantID(context.Background(), "T9")
	if _, err := ch.UpsertOne(ctx, map[string]any{"id": "x", "title": "t"}); err != nil {
		t.Fatalf("multitenant upsert: %v", err)
	}
	var tid string
	_ = db.QueryRow("SELECT tenant_id FROM tup WHERE id='x'").Scan(&tid)
	if tid != "T9" {
		t.Errorf("tenant not stamped on upsert: %q", tid)
	}
}

func TestCursor_WithNestedFilter(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	// Set a cursor field so cursor mode kicks in alongside a nested filter.
	ch.Entity.Config.CursorField = "id"
	req := httptest.NewRequest("GET", "/posts?cursor=&author.name=alice", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cursor+nested-filter = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestStream_WithNestedFilter(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	req := httptest.NewRequest("GET", "/posts?stream=true&author.name=alice", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stream+nested-filter = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestCursor_IncludeDBError(t *testing.T) {
	ch, _ := covMissingTargetWorld(t)
	ch.Entity.Config.CursorField = "id"
	req := httptest.NewRequest("GET", "/eposts?cursor=&include=comments", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("cursor include DB error = %d, want 500", rec.Code)
	}
}

func TestEventStream_FilterDropsNonMatching(t *testing.T) {
	// Drive the real EventStream handler: subscribe as alice on an owner
	// entity, emit a bob event + an alice event, and assert only alice's
	// event is written to the SSE body. This exercises the entity/owner
	// filter drop branches inside EventStream.
	installOwnerExtractor(t)
	ch, _ := covOwnerNotesHandler(t)
	bus := event.NewEventBus()
	ch.Events = bus

	ctx, cancel := context.WithCancel(ctxWithUser("alice"))
	req := httptest.NewRequest("GET", "/onotes/_events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { ch.EventStream()(rec, req); close(done) }()

	time.Sleep(50 * time.Millisecond)
	// Wrong-entity event (dropped by entity filter).
	bus.EmitAsync(context.Background(), event.Event{Type: event.EntityCreated, Data: map[string]any{
		eventKeyEntity: "other", eventKeyOwnerID: "alice",
	}})
	// Bob's event (dropped by owner filter).
	ch.EmitEvent(ctxWithUser("bob"), event.EntityCreated, map[string]any{"id": "n2", "user_id": "bob"})
	// Alice's event (delivered).
	ch.EmitEvent(ctxWithUser("alice"), event.EntityCreated, map[string]any{"id": "n1", "user_id": "alice"})
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, "n1") {
		t.Errorf("alice's event not delivered: %s", body)
	}
	if strings.Contains(body, "n2") {
		t.Errorf("bob's event leaked to alice's SSE stream: %s", body)
	}
}
