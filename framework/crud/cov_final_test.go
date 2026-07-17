package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestValidationError_Error(t *testing.T) {
	ve := &ValidationError{fields: map[string][]string{"name": {"required"}}}
	if ve.Error() != "validation failed" {
		t.Errorf("ValidationError.Error() = %q", ve.Error())
	}
}

func TestTenantMissingError_Error(t *testing.T) {
	tme := &tenantMissingError{}
	if !strings.Contains(tme.Error(), "tenant") {
		t.Errorf("tenantMissingError.Error() = %q", tme.Error())
	}
}

func TestClassifyDoErr_AllBranches(t *testing.T) {
	ve := &ValidationError{fields: map[string][]string{"a": {"x"}}}
	if msg, fields := classifyDoErr(ve); msg != "validation failed" || fields == nil {
		t.Errorf("validation classify = %q, %v", msg, fields)
	}
	bhe := &beforeHookError{err: errors.New("hook said no")}
	if msg, _ := classifyDoErr(bhe); msg != "hook said no" {
		t.Errorf("hook classify = %q", msg)
	}
	if msg, _ := classifyDoErr(errNotFound); msg != "not found" {
		t.Errorf("notfound classify = %q", msg)
	}
	if msg, _ := classifyDoErr(errNoFieldsToUpdate); msg != "no fields to update" {
		t.Errorf("nofields classify = %q", msg)
	}
	if msg, _ := classifyDoErr(errors.New("driver boom")); msg != "internal error" {
		t.Errorf("generic classify = %q", msg)
	}
}

// covBatchHandler is the notes handler wired with batch routes.
func covBatchHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE bn (id TEXT PRIMARY KEY, title TEXT)`)
	ent := entity.Define("bn", entity.EntityConfig{
		Name: "bn", Table: "bn",
		Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

func batchReq(method, body string) *http.Request {
	r := withTestUser(httptest.NewRequest(method, "/bn/_batch", strings.NewReader(body)), "u1")
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestBatchCreate_HTTP(t *testing.T) {
	ch, _ := covBatchHandler(t)
	rec := httptest.NewRecorder()
	ch.BatchCreate()(rec, batchReq("POST", `{"items":[{"title":"a"},{"title":"b"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("batch create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp BatchResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Committed || len(resp.Results) != 2 {
		t.Errorf("batch create resp = %+v", resp)
	}
}

func TestBatchCreate_HTTP_RollbackScrubsData(t *testing.T) {
	ch, db := covBatchHandler(t)
	rec := httptest.NewRecorder()
	// second item missing required title → rollback.
	ch.BatchCreate()(rec, batchReq("POST", `{"items":[{"title":"a"},{"body":"x"}]}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("rollback batch = %d, want 400", rec.Code)
	}
	var resp BatchResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Committed {
		t.Error("rollback should report Committed=false")
	}
	for _, r := range resp.Results {
		if r.Data != nil {
			t.Errorf("rolled-back result still carries Data: %+v", r)
		}
	}
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM bn").Scan(&n)
	if n != 0 {
		t.Errorf("rollback failed: %d rows", n)
	}
}

func TestBatchCreate_EmptyAndTooBig(t *testing.T) {
	ch, _ := covBatchHandler(t)
	rec := httptest.NewRecorder()
	ch.BatchCreate()(rec, batchReq("POST", `{"items":[]}`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty batch = %d, want 400", rec.Code)
	}
	// Build > MaxBatchSize items.
	var sb strings.Builder
	sb.WriteString(`{"items":[`)
	for i := 0; i <= MaxBatchSize; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"title":"x"}`)
	}
	sb.WriteString(`]}`)
	rec = httptest.NewRecorder()
	ch.BatchCreate()(rec, batchReq("POST", sb.String()))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("oversized batch = %d, want 400", rec.Code)
	}
}

func TestBatchCreate_BadContentType(t *testing.T) {
	ch, _ := covBatchHandler(t)
	r := httptest.NewRequest("POST", "/bn/_batch", strings.NewReader(`{"items":[]}`))
	r.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	ch.BatchCreate()(rec, r)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("bad content-type batch = %d, want 415", rec.Code)
	}
}

func TestBatchUpdate_HTTP(t *testing.T) {
	ch, _ := covBatchHandler(t)
	// Seed two rows.
	c1, _ := ch.CreateOne(context.Background(), map[string]any{"title": "a"})
	c2, _ := ch.CreateOne(context.Background(), map[string]any{"title": "b"})
	rec := httptest.NewRecorder()
	body := `{"items":[{"id":"` + c1["id"].(string) + `","title":"A"},{"id":"` + c2["id"].(string) + `","title":"B"}]}`
	ch.BatchUpdate()(rec, batchReq("PATCH", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("batch update = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestBatchUpdate_MissingID(t *testing.T) {
	ch, _ := covBatchHandler(t)
	rec := httptest.NewRecorder()
	ch.BatchUpdate()(rec, batchReq("PATCH", `{"items":[{"title":"no id"}]}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("batch update missing id = %d, want 400", rec.Code)
	}
}

func TestBatchDelete_HTTP(t *testing.T) {
	ch, _ := covBatchHandler(t)
	c1, _ := ch.CreateOne(context.Background(), map[string]any{"title": "a"})
	rec := httptest.NewRecorder()
	ch.BatchDelete()(rec, batchReq("DELETE", `{"ids":["`+c1["id"].(string)+`"]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("batch delete = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestBatchDelete_EmptyAndMissing(t *testing.T) {
	ch, _ := covBatchHandler(t)
	rec := httptest.NewRecorder()
	ch.BatchDelete()(rec, batchReq("DELETE", `{"ids":[]}`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty delete batch = %d, want 400", rec.Code)
	}
	// Deleting a missing id → rollback 400.
	rec = httptest.NewRecorder()
	ch.BatchDelete()(rec, batchReq("DELETE", `{"ids":["ghost"]}`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing-id delete batch = %d, want 400", rec.Code)
	}
}

func TestBatch_BadJSON(t *testing.T) {
	ch, _ := covBatchHandler(t)
	for _, mk := range []func() http.HandlerFunc{ch.BatchCreate, ch.BatchUpdate, ch.BatchDelete} {
		rec := httptest.NewRecorder()
		method := "POST"
		mk()(rec, batchReq(method, `{not json`))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("bad json batch = %d, want 400", rec.Code)
		}
	}
}

// covNestedWorld registers users with a relation so author.<rel> nests.
func covNestedWorld(t *testing.T) (*CrudHandler, stubRegistry) {
	t.Helper()
	db := setupDB(t,
		`CREATE TABLE orgs (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE nusers (id TEXT PRIMARY KEY, name TEXT, org_id TEXT)`,
		`CREATE TABLE nposts (id TEXT PRIMARY KEY, title TEXT, author_id TEXT)`,
	)
	seedRows(t, db, "orgs", []map[string]any{{"id": "o1", "name": "Acme"}})
	seedRows(t, db, "nusers", []map[string]any{{"id": "u1", "name": "alice", "org_id": "o1"}})
	seedRows(t, db, "nposts", []map[string]any{{"id": "p1", "title": "first", "author_id": "u1"}})

	orgsEnt := entity.Define("orgs", entity.EntityConfig{
		Name: "orgs", Table: "orgs",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	usersEnt := entity.Define("nusers", entity.EntityConfig{
		Name: "nusers", Table: "nusers",
		Fields: []schema.Field{{Name: "name", Type: schema.String}, {Name: "org_id", Type: schema.String}},
		Relations: []entity.Relation{
			entity.BelongsTo("org", "orgs", "org_id"),
		},
	}.WithTimestamps(false))
	postsEnt := entity.Define("nposts", entity.EntityConfig{
		Name: "nposts", Table: "nposts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}, {Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "nusers", "author_id"),
		},
	}.WithTimestamps(false))
	postsEnt.SetDB(db)

	reg := stubRegistry{byName: map[string]*entity.Entity{
		"orgs": orgsEnt, "nusers": usersEnt, "nposts": postsEnt,
	}}
	ch := NewCrudHandler(postsEnt, db).WithJSONCase(CaseSnake)
	ch.Registry = reg
	return ch, reg
}

func TestNestedInclude_AuthorOrg(t *testing.T) {
	ch, _ := covNestedWorld(t)
	req := withTestUser(httptest.NewRequest("GET", "/nposts?include=author.org", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("nested include status = %d, body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeListResponse(t, rec.Body.String())
	if len(resp.Data) == 0 {
		t.Fatal("no posts")
	}
	author, _ := resp.Data[0]["author"].(map[string]any)
	if author == nil {
		t.Fatalf("author missing: %+v", resp.Data[0])
	}
	org, _ := author["org"].(map[string]any)
	if org == nil || org["name"] != "Acme" {
		t.Errorf("nested org not loaded: %+v", author)
	}
}

func TestTypedQuery_FindDBError(t *testing.T) {
	ch, db := covNotesHandler(t)
	db.Close() // force query error
	type n struct {
		ID string `json:"id"`
	}
	if _, err := NewTypedQuery[n](ch).Find(context.Background()); err == nil {
		t.Error("Find on closed DB should error")
	}
	if _, err := NewTypedQuery[n](ch).First(context.Background()); err == nil {
		t.Error("First on closed DB should error")
	}
	if _, err := NewTypedQuery[n](ch).Count(context.Background()); err == nil {
		t.Error("Count on closed DB should error")
	}
	if _, err := NewTypedQuery[n](ch).Exists(context.Background()); err == nil {
		t.Error("Exists on closed DB should error")
	}
	if _, err := NewTypedQuery[n](ch).UpdateAll(context.Background(), map[string]any{"title": "x"}); err == nil {
		t.Error("UpdateAll on closed DB should error")
	}
	if _, err := NewTypedQuery[n](ch).DeleteAll(context.Background()); err == nil {
		t.Error("DeleteAll on closed DB should error")
	}
}

func TestInProcess_DBErrors(t *testing.T) {
	ch, db := covNotesHandler(t)
	db.Close()
	ctx := context.Background()
	if _, err := ch.GetOne(ctx, "x", nil); err == nil {
		t.Error("GetOne on closed DB should error")
	}
	if _, err := ch.ListAll(ctx, ListOptions{}); err == nil {
		t.Error("ListAll on closed DB should error")
	}
	if _, err := ch.CountAll(ctx, ListOptions{}); err == nil {
		t.Error("CountAll on closed DB should error")
	}
	if _, err := ch.CreateOne(ctx, map[string]any{"title": "x"}); err == nil {
		t.Error("CreateOne on closed DB should error")
	}
}

func TestList_DBError(t *testing.T) {
	ch, db := covNotesHandler(t)
	db.Close()
	rec := httptest.NewRecorder()
	ch.List()(rec, withTestUser(httptest.NewRequest("GET", "/notes", nil), "u1"))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("List on closed DB = %d, want 500", rec.Code)
	}
}

func TestGet_DBError(t *testing.T) {
	ch, db := covNotesHandler(t)
	db.Close()
	req := withTestUser(httptest.NewRequest("GET", "/notes/x", nil), "u1")
	req.SetPathValue("id", "x")
	rec := httptest.NewRecorder()
	ch.Get()(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Get on closed DB = %d, want 500", rec.Code)
	}
}

func TestHookErrors_MapTo400(t *testing.T) {
	ch, _ := covNotesHandler(t)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeCreate, func(ctx context.Context, data any) error {
		return errors.New("rejected by hook")
	})
	req := withTestUser(httptest.NewRequest("POST", "/notes", strings.NewReader(`{"title":"x"}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("before-create hook reject = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "rejected by hook") {
		t.Errorf("body: %s", rec.Body.String())
	}
}

func TestBeforeListHookError_MapTo400(t *testing.T) {
	ch, _ := covNotesHandler(t)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeList, func(ctx context.Context, data any) error {
		return errors.New("list denied")
	})
	rec := httptest.NewRecorder()
	ch.List()(rec, withTestUser(httptest.NewRequest("GET", "/notes", nil), "u1"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("before-list hook reject = %d, want 400", rec.Code)
	}
}

func TestBeforeGetHookError_MapTo400(t *testing.T) {
	ch, _ := covNotesHandler(t)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeGet, func(ctx context.Context, data any) error {
		return errors.New("get denied")
	})
	req := withTestUser(httptest.NewRequest("GET", "/notes/x", nil), "u1")
	req.SetPathValue("id", "x")
	rec := httptest.NewRecorder()
	ch.Get()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("before-get hook reject = %d, want 400", rec.Code)
	}
}

func TestBodyTooLarge(t *testing.T) {
	ch, _ := covNotesHandler(t)
	big := `{"title":"` + strings.Repeat("x", int(MaxJSONBodyBytes)+100) + `"}`
	req := withTestUser(httptest.NewRequest("POST", "/notes", strings.NewReader(big)), "u1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize body = %d, want 413", rec.Code)
	}
}

func TestBatchCreate_BodyTooLarge(t *testing.T) {
	ch, _ := covBatchHandler(t)
	big := `{"items":[{"title":"` + strings.Repeat("x", int(MaxJSONBodyBytes)+100) + `"}]}`
	rec := httptest.NewRecorder()
	ch.BatchCreate()(rec, batchReq("POST", big))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize batch body = %d, want 413", rec.Code)
	}
}
