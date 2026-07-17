package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestNormalizePath(t *testing.T) {
	if got := NormalizePath("/users///"); got != "/users" {
		t.Errorf("NormalizePath = %q", got)
	}
	if got := NormalizePath("/"); got != "/" {
		t.Errorf("root path = %q", got)
	}
}

// covSimpleEntity is an owner-free, single-table entity for route wiring.
func covSimpleEntity(t *testing.T) (*entity.Entity, *sql.DB, *router.Router) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE widgets (id TEXT PRIMARY KEY, name TEXT)`)
	ent := entity.Define("widgets", entity.EntityConfig{
		Name: "widgets", Table: "widgets",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	r := router.New()
	return ent, db, r
}

func TestRegisterCrudRoutes_WiresEndpoints(t *testing.T) {
	ent, db, r := covSimpleEntity(t)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	RegisterCrudRoutes(r, ch, "/widgets/")

	// LLM-md route requires auth; hit it authenticated.
	req := httptest.NewRequest("GET", "/widgets/llm.md", nil)
	req = req.WithContext(handler.SetUser(req.Context(), &testUser{id: "u1"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("llm.md status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "# widgets") {
		t.Error("llm.md body missing entity header")
	}
}

func TestPatchSparseUpdate(t *testing.T) {
	db := setupDB(t, `CREATE TABLE widgets (id TEXT PRIMARY KEY, name TEXT NOT NULL, note TEXT)`)
	if _, err := db.Exec(`INSERT INTO widgets (id, name, note) VALUES ('w1', 'Widget', 'old')`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("widgets", entity.EntityConfig{
		Name: "widgets", Table: "widgets",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "note", Type: schema.String},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	r := router.New()
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	var beforeBody map[string]any
	afterFired := false
	ch.Hooks.RegisterHook(hook.BeforeUpdate, func(_ context.Context, data any) error {
		beforeBody = data.(map[string]any)
		return nil
	})
	ch.Hooks.RegisterHook(hook.AfterUpdate, func(_ context.Context, _ any) error {
		afterFired = true
		return nil
	})
	RegisterCrudRoutes(r, ch, "/widgets")

	req := httptest.NewRequest(http.MethodPatch, "/widgets/w1", strings.NewReader(`{"note":"new"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var response struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data["name"] != "Widget" || response.Data["note"] != "new" {
		t.Fatalf("PATCH data = %#v", response.Data)
	}
	if len(beforeBody) != 1 || beforeBody["note"] != "new" || !afterFired {
		t.Fatalf("PATCH hooks: before=%#v after=%v", beforeBody, afterFired)
	}
}

func TestSingleResponsesWrapped(t *testing.T) {
	ent, db, r := covSimpleEntity(t)
	if _, err := db.Exec(`INSERT INTO widgets (id, name) VALUES ('w1', 'Widget')`); err != nil {
		t.Fatal(err)
	}
	RegisterCrudRoutes(r, NewCrudHandler(ent, db).WithJSONCase(CaseSnake), "/widgets")

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{name: "create", method: http.MethodPost, path: "/widgets", body: `{"name":"Created"}`, status: http.StatusCreated},
		{name: "get", method: http.MethodGet, path: "/widgets/w1", status: http.StatusOK},
		{name: "update", method: http.MethodPut, path: "/widgets/w1", body: `{"name":"Updated"}`, status: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.status, rec.Body.String())
			}
			var response struct {
				Data map[string]any `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Data == nil {
				t.Fatalf("response is not wrapped: %s", rec.Body.String())
			}
		})
	}
}

func TestRegisterCrudRoutes_ReadOnlyAndNoLLMMD(t *testing.T) {
	ent, db, r := covSimpleEntity(t)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	RegisterCrudRoutes(r, ch, "/widgets", CrudRouteOptions{ReadOnly: true, NoLLMMD: true})

	// POST should not be registered (read-only) → 405 or 404.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("POST", "/widgets", strings.NewReader("{}")))
	if rec.Code == http.StatusCreated {
		t.Error("read-only should not allow POST")
	}
	// llm.md disabled.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/widgets/llm.md", nil)
	req = req.WithContext(handler.SetUser(req.Context(), &testUser{id: "u1"}))
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("NoLLMMD should not register llm.md")
	}
}

func TestRegisterCrudRoutesFunc(t *testing.T) {
	ent, db, r := covSimpleEntity(t)
	ch := RegisterCrudRoutesFunc(r, ent, db, "/widgets")
	if ch == nil || ch.Entity != ent {
		t.Fatal("RegisterCrudRoutesFunc returned wrong handler")
	}
}

func TestLLMMDHandler_AuthGate(t *testing.T) {
	ent, _, _ := covSimpleEntity(t)
	h := LLMMDHandler(ent)
	// Anonymous → 401.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/widgets/llm.md", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon llm.md = %d, want 401", rec.Code)
	}
	// Authenticated → 200 + content-length set.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/widgets/llm.md", nil)
	req = req.WithContext(handler.SetUser(req.Context(), &testUser{id: "u1"}))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth llm.md = %d", rec.Code)
	}
	if rec.Header().Get("Content-Length") == "" {
		t.Error("missing Content-Length")
	}
}

func TestRegistryLLMMDHandler_AuthGate(t *testing.T) {
	reg := stubRegistry{byName: map[string]*entity.Entity{"posts": covRelEntity()}}
	h := RegistryLLMMDHandler(reg, "App")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/llm.md", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon registry llm.md = %d, want 401", rec.Code)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/llm.md", nil)
	req = req.WithContext(handler.SetUser(req.Context(), &testUser{id: "u1"}))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth registry llm.md = %d", rec.Code)
	}
}

func TestRegisterEntityMCPTools_NilGuards(t *testing.T) {
	srv := mcp.NewServer()
	ent, db, r := covSimpleEntity(t)
	ch := NewCrudHandler(ent, db)
	if err := RegisterEntityMCPTools(nil, ch, r); err == nil {
		t.Error("nil server should error")
	}
	if err := RegisterEntityMCPTools(srv, nil, r); err == nil {
		t.Error("nil crud should error")
	}
	if err := RegisterEntityMCPTools(srv, ch, nil); err == nil {
		t.Error("nil router should error")
	}
}

func TestMCPTools_DispatchThroughRouter(t *testing.T) {
	ent, db, r := covSimpleEntity(t)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	RegisterCrudRoutes(r, ch, "/widgets")
	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatalf("register mcp: %v", err)
	}
	// widgets has no OwnerField/Access/Public — MCP tools inherit the same
	// secure-by-default session gate as REST (issue #65), so this dispatch
	// test needs an authenticated caller. See
	// TestMCPTools_AnonymousCallsRejected for the negative case.
	ctx := handler.SetUser(context.Background(), &testUser{id: "u1"})

	// Create
	created, err := srv.CallTool(ctx, "widgets_create", map[string]any{"name": "gizmo"})
	if err != nil {
		t.Fatalf("widgets_create: %v", err)
	}
	cm := created.(map[string]any)
	id := cm["id"].(string)

	// Get
	got, err := srv.CallTool(ctx, "widgets_get", map[string]any{"id": id})
	if err != nil {
		t.Fatalf("widgets_get: %v", err)
	}
	if got.(map[string]any)["name"] != "gizmo" {
		t.Errorf("get mismatch: %v", got)
	}

	// List (with a filter param + paging)
	listed, err := srv.CallTool(ctx, "widgets_list", map[string]any{"name": "gizmo", "limit": 10, "page": 1})
	if err != nil {
		t.Fatalf("widgets_list: %v", err)
	}
	if _, ok := listed.(map[string]any)["data"]; !ok {
		t.Errorf("list missing data: %v", listed)
	}

	// Update
	upd, err := srv.CallTool(ctx, "widgets_update", map[string]any{"id": id, "name": "gizmo2"})
	if err != nil {
		t.Fatalf("widgets_update: %v", err)
	}
	if upd.(map[string]any)["name"] != "gizmo2" {
		t.Errorf("update mismatch: %v", upd)
	}

	// Delete
	del, err := srv.CallTool(ctx, "widgets_delete", map[string]any{"id": id})
	if err != nil {
		t.Fatalf("widgets_delete: %v", err)
	}
	if del.(map[string]any)["deleted"] != true {
		t.Errorf("delete result: %v", del)
	}
}

// TestMCPTools_AnonymousCallsRejected pins issue #65's MCP half: entity MCP
// tools dispatch through the same router + requireScope chain as REST, so an
// anonymous caller (no Cookie/Authorization on the inbound MCP request, no
// user in ctx) must be refused on every generated tool exactly like the
// REST routes are. No per-tool mcp.Gated wrapping is needed — the shared
// dispatch path (RegisterEntityMCPTools → runToolRequest → router.ServeHTTP)
// is the enforcement point, see mcp.Gated's doc comment.
func TestMCPTools_AnonymousCallsRejected(t *testing.T) {
	ent, db, r := covSimpleEntity(t)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	RegisterCrudRoutes(r, ch, "/widgets")
	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatalf("register mcp: %v", err)
	}
	ctx := context.Background() // no user, no inbound request — anonymous

	for _, call := range []struct {
		tool   string
		params map[string]any
	}{
		{"widgets_list", map[string]any{}},
		{"widgets_get", map[string]any{"id": "ghost"}},
		{"widgets_create", map[string]any{"name": "gizmo"}},
		{"widgets_update", map[string]any{"id": "ghost", "name": "gizmo2"}},
		{"widgets_delete", map[string]any{"id": "ghost"}},
	} {
		if _, err := srv.CallTool(ctx, call.tool, call.params); err == nil {
			t.Errorf("anonymous %s should be rejected, got no error", call.tool)
		} else if !strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "authentication required") {
			t.Errorf("anonymous %s error = %v, want a 401/authentication-required refusal", call.tool, err)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM widgets`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("anonymous widgets_create persisted a row: count=%d", n)
	}
}

func TestMCPTools_MissingIDParam(t *testing.T) {
	ent, db, r := covSimpleEntity(t)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	RegisterCrudRoutes(r, ch, "/widgets")
	srv := mcp.NewServer()
	_ = RegisterEntityMCPTools(srv, ch, r)

	if _, err := srv.CallTool(context.Background(), "widgets_get", map[string]any{}); err == nil {
		t.Error("widgets_get without id should error")
	}
	if _, err := srv.CallTool(context.Background(), "widgets_update", map[string]any{"name": "x"}); err == nil {
		t.Error("widgets_update without id should error")
	}
	if _, err := srv.CallTool(context.Background(), "widgets_delete", map[string]any{"id": ""}); err == nil {
		t.Error("widgets_delete with empty id should error")
	}
}

func TestMCPTools_GetNotFoundIsError(t *testing.T) {
	ent, db, r := covSimpleEntity(t)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	RegisterCrudRoutes(r, ch, "/widgets")
	srv := mcp.NewServer()
	_ = RegisterEntityMCPTools(srv, ch, r)
	if _, err := srv.CallTool(context.Background(), "widgets_get", map[string]any{"id": "missing"}); err == nil {
		t.Error("get of missing row should surface a 404 error")
	}
}
