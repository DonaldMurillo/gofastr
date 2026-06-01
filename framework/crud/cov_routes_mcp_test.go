package crud

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
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
	ctx := context.Background()

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
