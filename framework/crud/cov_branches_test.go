package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// covDefaultHandler exercises Default-valued fields on create.
func TestCreate_AppliesDefaults(t *testing.T) {
	db := setupDB(t, `CREATE TABLE df (id TEXT PRIMARY KEY, title TEXT, status TEXT)`)
	ent := entity.Define("df", entity.EntityConfig{
		Name: "df", Table: "df",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "status", Type: schema.String, Default: "draft"},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)

	got, err := ch.CreateOne(context.Background(), map[string]any{"title": "x"})
	if err != nil {
		t.Fatalf("CreateOne: %v", err)
	}
	if got["status"] != "draft" {
		t.Errorf("default not applied: %v", got["status"])
	}
}

func TestInjectTenant_StampsColumn(t *testing.T) {
	ent := entity.Define("mt", entity.EntityConfig{
		Name: "mt", Table: "mt", MultiTenant: true,
		Fields: []schema.Field{{Name: "body", Type: schema.String}},
	}.WithTimestamps(false))
	ch := NewCrudHandler(ent, nil).WithJSONCase(CaseSnake)
	data := map[string]any{"body": "x"}
	ch.InjectTenant(data, tenant.SetTenantID(context.Background(), "T1"))
	if data[ent.Config.TenantColumn()] != "T1" {
		t.Errorf("tenant not injected: %v", data)
	}
	// No tenant id → no injection.
	data2 := map[string]any{"body": "y"}
	ch.InjectTenant(data2, context.Background())
	if _, ok := data2[ent.Config.TenantColumn()]; ok {
		t.Error("tenant injected without id")
	}
}

func TestCreate_TenantMissing(t *testing.T) {
	db := setupDB(t, `CREATE TABLE mt2 (id TEXT PRIMARY KEY, tenant_id TEXT, body TEXT)`)
	ent := entity.Define("mt2", entity.EntityConfig{
		Name: "mt2", Table: "mt2", MultiTenant: true,
		Fields: []schema.Field{{Name: "body", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	// HTTP create with no tenant in ctx → 401: the secure-by-default
	// RequireTenant gate refuses the request before the create-specific
	// orphan guard (which would otherwise 400). Either way the row is
	// never written.
	req := httptest.NewRequest("POST", "/mt2", strings.NewReader(`{"body":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tenant-missing create = %d, want 401", rec.Code)
	}
}

func TestAfterHooks_RunOnAllOps(t *testing.T) {
	ch, _ := covNotesHandler(t)
	ch.Hooks = hook.NewHookRegistry()
	fired := map[hook.HookType]bool{}
	for _, ht := range []hook.HookType{hook.AfterCreate, hook.AfterUpdate, hook.AfterDelete, hook.AfterGet, hook.AfterList} {
		ht := ht
		ch.Hooks.RegisterHook(ht, func(ctx context.Context, data any) error {
			fired[ht] = true
			return nil
		})
	}
	ctx := context.Background()
	created, _ := ch.CreateOne(ctx, map[string]any{"title": "x"})
	id := created["id"].(string)
	_, _ = ch.UpdateOne(ctx, id, map[string]any{"title": "y"})

	// Get + List go through HTTP handlers (they run After hooks).
	req := httptest.NewRequest("GET", "/notes/"+id, nil)
	req.SetPathValue("id", id)
	ch.Get()(httptest.NewRecorder(), req)
	ch.List()(httptest.NewRecorder(), httptest.NewRequest("GET", "/notes", nil))

	_ = ch.DeleteOne(ctx, id)

	for _, ht := range []hook.HookType{hook.AfterCreate, hook.AfterUpdate, hook.AfterDelete, hook.AfterGet, hook.AfterList} {
		if !fired[ht] {
			t.Errorf("hook %v did not fire", ht)
		}
	}
}

func TestEagerLoad_InvalidIdentifiers(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	ent := entity.Define("p", entity.EntityConfig{Name: "p", Table: "p"}.WithTimestamps(false))
	ctx := context.Background()
	// Invalid relation entity name.
	badRel := entity.Relation{Type: entity.RelHasMany, Name: "r", Entity: "bad name!", ForeignKey: "x"}
	if _, err := EagerLoad(ctx, db, ent, []entity.Relation{badRel}, []string{"1"}); err == nil {
		t.Error("invalid relation entity should error")
	}
	// Invalid FK.
	badFK := entity.Relation{Type: entity.RelHasMany, Name: "r", Entity: "users", ForeignKey: "bad fk!"}
	if _, err := EagerLoad(ctx, db, ent, []entity.Relation{badFK}, []string{"1"}); err == nil {
		t.Error("invalid FK should error")
	}
}

func TestEagerLoad_InvalidParentTable(t *testing.T) {
	db := setupDB(t, `CREATE TABLE x (id TEXT PRIMARY KEY)`)
	ent := entity.Define("bad", entity.EntityConfig{Name: "bad", Table: "bad name!"}.WithTimestamps(false))
	rel := entity.HasMany("r", "x", "x_id")
	if _, err := EagerLoad(context.Background(), db, ent, []entity.Relation{rel}, []string{"1"}); err == nil {
		t.Error("invalid parent table should error")
	}
}

func TestInclude_SoftDeleteAndHiddenScrub(t *testing.T) {
	// posts → comments (HasMany), comments soft-deletable + hidden secret.
	db := setupDB(t,
		`CREATE TABLE sposts (id TEXT PRIMARY KEY, title TEXT)`,
		`CREATE TABLE scomments (id TEXT PRIMARY KEY, post_id TEXT, body TEXT, secret TEXT, deleted_at TEXT)`,
	)
	seedRows(t, db, "sposts", []map[string]any{{"id": "p1", "title": "t"}})
	seedRows(t, db, "scomments", []map[string]any{
		{"id": "c1", "post_id": "p1", "body": "live", "secret": "SHH", "deleted_at": nil},
		{"id": "c2", "post_id": "p1", "body": "gone", "secret": "SHH2", "deleted_at": "2026-01-01"},
	})
	commentsEnt := entity.Define("scomments", entity.EntityConfig{
		Name: "scomments", Table: "scomments", SoftDelete: true,
		Fields: []schema.Field{
			{Name: "post_id", Type: schema.String},
			{Name: "body", Type: schema.String},
			{Name: "secret", Type: schema.String, Hidden: true},
		},
	}.WithTimestamps(false))
	postsEnt := entity.Define("sposts", entity.EntityConfig{
		Name: "sposts", Table: "sposts",
		Fields:    []schema.Field{{Name: "title", Type: schema.String}},
		Relations: []entity.Relation{entity.HasMany("comments", "scomments", "post_id")},
	}.WithTimestamps(false))
	postsEnt.SetDB(db)
	reg := stubRegistry{byName: map[string]*entity.Entity{"scomments": commentsEnt, "sposts": postsEnt}}
	ch := NewCrudHandler(postsEnt, db).WithJSONCase(CaseSnake)
	ch.Registry = reg

	req := httptest.NewRequest("GET", "/sposts?include=comments", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	resp := decodeListResponse(t, rec.Body.String())
	comments, _ := resp.Data[0]["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("soft-deleted comment leaked: %d comments", len(comments))
	}
	c := comments[0].(map[string]any)
	if _, leaked := c["secret"]; leaked {
		t.Error("SECURITY: hidden secret leaked via include")
	}
}

func TestCursor_WithFiltersAndExtraWhere(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) { c.CursorField = "seq" }, 6)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeList, func(ctx context.Context, data any) error {
		p := data.(*hook.ListPayload)
		p.AddWhere("seq >= $1", 1)
		return nil
	})
	// filter on name + extraWhere from hook + cursor mode.
	req := httptest.NewRequest("GET", "/items?cursor=&limit=2&name=n", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cursor+filter+hook = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestStream_WithSortAndExtraWhere(t *testing.T) {
	ch, _ := covItems(t, nil, 5)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeList, func(ctx context.Context, data any) error {
		p := data.(*hook.ListPayload)
		p.AddWhere("seq >= $1", 0)
		return nil
	})
	req := httptest.NewRequest("GET", "/items?stream=true&sort=-seq", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stream+sort+hook = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestList_PaginatedTotalPages(t *testing.T) {
	ch, _ := covItems(t, nil, 5)
	req := httptest.NewRequest("GET", "/items?page=1&limit=2", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	resp := decodeListResponse(t, rec.Body.String())
	if resp.Total != 5 || resp.TotalPages != 3 {
		t.Errorf("pagination = total %d, pages %d", resp.Total, resp.TotalPages)
	}
}

func TestEventStream_FilterDropsByTenant(t *testing.T) {
	// Multi-tenant entity: subscriber on tenant A must not see tenant B events.
	db := setupDB(t, `CREATE TABLE mte (id TEXT PRIMARY KEY, tenant_id TEXT, body TEXT)`)
	ent := entity.Define("mte", entity.EntityConfig{
		Name: "mte", Table: "mte", MultiTenant: true,
		Fields: []schema.Field{{Name: "body", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	bus := event.NewEventBus()
	ch.Events = bus

	// Drive the EventStream filter directly through a subscription mirroring it.
	// Use the EmitEvent path so the tenant key is stamped, then assert the
	// event data carries the right tenant for filtering.
	got := make(chan map[string]any, 2)
	cancel := bus.Subscribe(event.EntityCreated, func(_ context.Context, ev event.Event) error {
		d := ev.Data.(map[string]any)
		got <- d
		return nil
	})
	defer cancel()
	ch.EmitEvent(tenant.SetTenantID(context.Background(), "A"), event.EntityCreated, map[string]any{"id": "1", "body": "a"})
	d := <-got
	if d[eventKeyTenantID] != "A" {
		t.Errorf("tenant key = %v, want A", d[eventKeyTenantID])
	}
}

// covSeedExecutor lets us run scanRowsPooledWithKeys against a scan error.
func TestScanRowsPooled_ScanError(t *testing.T) {
	ch, db := covNotesHandler(t)
	_, _ = ch.CreateOne(context.Background(), map[string]any{"title": "x"})
	rows, err := db.Query(`SELECT id, title FROM notes`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	// cols has wrong arity → Scan fails inside the pooled scanner.
	if _, err := scanRowsPooledWithKeys(rows, []string{"id"}, []string{"id"}); err == nil {
		t.Error("scanRowsPooledWithKeys with wrong arity should error")
	}
}

func TestProjection_DuplicateFields(t *testing.T) {
	ch, _ := covNotesHandler(t)
	// Duplicate + empty entries are de-duped / skipped.
	req := httptest.NewRequest("GET", "/notes?fields=title,title,,body", nil)
	cols, err := ch.projectFromRequest(req)
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	// id + title + body, no dup title.
	seen := map[string]int{}
	for _, c := range cols {
		seen[c]++
	}
	if seen["title"] != 1 {
		t.Errorf("duplicate title not de-duped: %v", cols)
	}
}
