package crud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

func batchReqBN(method, body string) *http.Request {
	r := withTestUser(httptest.NewRequest(method, "/bn/_batch", strings.NewReader(body)), "u1")
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestBatchUpdate_EmptyAndTooBig(t *testing.T) {
	ch, _ := covBatchHandler(t)
	rec := httptest.NewRecorder()
	ch.BatchUpdate()(rec, batchReqBN("PATCH", `{"items":[]}`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty update batch = %d, want 400", rec.Code)
	}
	var sb strings.Builder
	sb.WriteString(`{"items":[`)
	for i := 0; i <= MaxBatchSize; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"id":"x","title":"y"}`)
	}
	sb.WriteString(`]}`)
	rec = httptest.NewRecorder()
	ch.BatchUpdate()(rec, batchReqBN("PATCH", sb.String()))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("oversized update batch = %d, want 400", rec.Code)
	}
}

func TestBatchUpdate_NonStringID(t *testing.T) {
	ch, _ := covBatchHandler(t)
	// numeric id → fmt.Sprintf fallback path; row won't exist → rollback 400.
	rec := httptest.NewRecorder()
	ch.BatchUpdate()(rec, batchReqBN("PATCH", `{"items":[{"id":123,"title":"y"}]}`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("numeric-id update batch = %d, want 400 (no such row)", rec.Code)
	}
}

func TestBatchDelete_TooBig(t *testing.T) {
	ch, _ := covBatchHandler(t)
	var sb strings.Builder
	sb.WriteString(`{"ids":[`)
	for i := 0; i <= MaxBatchSize; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`"x"`)
	}
	sb.WriteString(`]}`)
	rec := httptest.NewRecorder()
	ch.BatchDelete()(rec, batchReqBN("DELETE", sb.String()))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("oversized delete batch = %d, want 400", rec.Code)
	}
}

func TestInProcessBatch_RollbackErrors(t *testing.T) {
	ch, _ := covNotesHandler(t)
	ctx := context.Background()
	// BatchUpdateMany with a missing id → doUpdate errNotFound → rollback.
	if _, err := ch.BatchUpdateMany(ctx, []string{"ghost"}, []map[string]any{{"title": "x"}}); err == nil {
		t.Error("BatchUpdateMany on missing row should error")
	}
	// BatchDeleteMany with a missing id → doDelete errNotFound → rollback.
	if _, err := ch.BatchDeleteMany(ctx, []string{"ghost"}); err == nil {
		t.Error("BatchDeleteMany on missing row should error")
	}
}

func TestInProcess_TenantGuards(t *testing.T) {
	db := setupDB(t, `CREATE TABLE tg (id TEXT PRIMARY KEY, tenant_id TEXT, body TEXT)`)
	ent := entity.Define("tg", entity.EntityConfig{
		Name: "tg", Table: "tg", MultiTenant: true,
		Fields: []schema.Field{{Name: "body", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ctx := context.Background() // no tenant
	if _, err := ch.CreateOne(ctx, map[string]any{"body": "x"}); err == nil {
		t.Error("CreateOne without tenant should error")
	}
	if _, err := ch.BatchCreateMany(ctx, []map[string]any{{"body": "x"}}); err == nil {
		t.Error("BatchCreateMany without tenant should error")
	}
}

func TestHTTPCreate_MultiTenantStampsColumn(t *testing.T) {
	db := setupDB(t, `CREATE TABLE htc (id TEXT PRIMARY KEY, tenant_id TEXT, body TEXT)`)
	ent := entity.Define("htc", entity.EntityConfig{
		Name: "htc", Table: "htc", MultiTenant: true,
		Fields: []schema.Field{{Name: "body", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	// In-process create with a tenant in ctx exercises the cols/vals tenant append.
	if _, err := ch.CreateOne(setTenant(t, "T1"), map[string]any{"body": "x"}); err != nil {
		t.Fatalf("multitenant create: %v", err)
	}
	var tid string
	_ = db.QueryRow("SELECT tenant_id FROM htc LIMIT 1").Scan(&tid)
	if tid != "T1" {
		t.Errorf("tenant column not stamped: %q", tid)
	}
}

func TestDoUpdate_ValidatesMediaURL(t *testing.T) {
	ch, _ := covUploadHandler(t)
	created, _ := ch.CreateOne(context.Background(), map[string]any{"caption": "c", "photo": "https://ok/a.png"})
	// Update with an unsafe media URL → validation error.
	req := withTestUser(httptest.NewRequest("PUT", "/media/"+created["id"].(string),
		strings.NewReader(`{"photo":"javascript:alert(1)"}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", created["id"].(string))
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update unsafe media = %d, want 400", rec.Code)
	}
}

func TestLLMMD_NotesCombinations(t *testing.T) {
	ent := entity.Define("combo", entity.EntityConfig{
		Name: "combo", Table: "combo",
		Fields: []schema.Field{
			// unique + default → notes prefix branch (line 65).
			{Name: "code", Type: schema.String, Unique: true, Default: "AUTO"},
			// unique + values → notes prefix branch (line 71).
			{Name: "kind", Type: schema.Enum, Unique: true, Values: []string{"a", "b"}},
		},
	}.WithTimestamps(false))
	md := EntityLLMMD(ent)
	if !strings.Contains(md, "unique, default: AUTO") {
		t.Errorf("expected combined unique+default notes; md=%s", md)
	}
	if !strings.Contains(md, "unique, values: a|b") {
		t.Errorf("expected combined unique+values notes")
	}
}

func TestMCP_RegisterToolDuplicateError(t *testing.T) {
	ent, db, r := covSimpleEntity(t)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	RegisterCrudRoutes(r, ch, "/widgets")
	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatalf("first register: %v", err)
	}
	// Second registration of the same entity → duplicate tool name error.
	if err := RegisterEntityMCPTools(srv, ch, r); err == nil {
		t.Error("duplicate MCP tool registration should error")
	}
}

func TestTypedQuery_DeleteAllSoftDelete(t *testing.T) {
	db := setupDB(t, `CREATE TABLE sd (id TEXT PRIMARY KEY, title TEXT, deleted_at TEXT)`)
	ent := entity.Define("sd", entity.EntityConfig{
		Name: "sd", Table: "sd", SoftDelete: true,
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	_, _ = ch.CreateOne(context.Background(), map[string]any{"title": "a"})
	type row struct {
		ID string `json:"id"`
	}
	n, err := NewTypedQuery[row](ch).Where(entity.NewStringColumn("title").Eq("a")).DeleteAll(context.Background())
	if err != nil {
		t.Fatalf("soft DeleteAll: %v", err)
	}
	if n != 1 {
		t.Errorf("soft DeleteAll = %d, want 1", n)
	}
	// Row still present but soft-deleted.
	var del any
	_ = db.QueryRow("SELECT deleted_at FROM sd LIMIT 1").Scan(&del)
	if del == nil {
		t.Error("row not soft-deleted")
	}
}

func TestTypedQuery_FindIncludeError(t *testing.T) {
	ch, _ := covMissingTargetWorld(t)
	type post struct {
		ID string `json:"id"`
	}
	if _, err := NewTypedQuery[post](ch).Include("comments").Find(context.Background()); err == nil {
		t.Error("Find with broken include should error")
	}
}

func TestDecodeCursorAny_FieldNotInSet(t *testing.T) {
	// A composite cursor whose decoded field isn't in the expected set.
	ch, _ := covItems(t, func(c *entity.EntityConfig) { c.CursorFields = []string{"seq", "id"} }, 4)
	// Build a forward page to get a real composite cursor, then ask the
	// decoder to validate it against a DIFFERENT field set.
	req := withTestUser(httptest.NewRequest("GET", "/items?cursor=&limit=2", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	var page struct {
		Cursor string `json:"cursor"`
	}
	_ = decodeJSON(rec.Body.String(), &page)
	if page.Cursor == "" {
		t.Skip("no cursor produced")
	}
	if _, err := decodeCursorAny(page.Cursor, []string{"seq", "other"}); err == nil {
		t.Error("cursor with field not in expected set should error")
	}
}

// helpers
func setTenant(t *testing.T, id string) context.Context {
	t.Helper()
	return tenant.SetTenantID(context.Background(), id)
}

func decodeJSON(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}

var _ = filter.OpEq
