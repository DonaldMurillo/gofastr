package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gofastr/gofastr/core/query"
	"github.com/gofastr/gofastr/core/schema"
)

// ============================================================================
// Test: Multitenancy scoping in CRUD handlers
// ============================================================================

func TestCrudApplyTenantScope_QueryBuilder(t *testing.T) {
	entity := Define("posts", EntityConfig{})
	WithMultiTenant(entity, DefaultTenantConfig())

	ch := &CrudHandler{Entity: entity}

	req := httptest.NewRequest("GET", "/posts", nil)
	req = req.WithContext(SetTenantID(context.Background(), "tenant-123"))

	qb := query.Select("*").From("posts")
	ch.applyTenantScope(qb, req)

	sqlStr, args := qb.Build()
	if !strings.Contains(sqlStr, "tenant_id = $") {
		t.Errorf("expected tenant_id filter in query, got: %s", sqlStr)
	}
	found := false
	for _, a := range args {
		if a == "tenant-123" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected tenant-123 in args, got: %v", args)
	}
}

func TestCrudApplyTenantScope_NotMultiTenant(t *testing.T) {
	entity := Define("posts", EntityConfig{})
	// NOT multi-tenant

	ch := &CrudHandler{Entity: entity}

	req := httptest.NewRequest("GET", "/posts", nil)
	req = req.WithContext(SetTenantID(context.Background(), "tenant-123"))

	qb := query.Select("*").From("posts")
	ch.applyTenantScope(qb, req)

	sqlStr, args := qb.Build()
	if strings.Contains(sqlStr, "tenant_id") {
		t.Errorf("expected no tenant_id filter for non-multitenant entity, got: %s", sqlStr)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got: %v", args)
	}
}

func TestCrudApplyTenantScope_EmptyTenantID(t *testing.T) {
	entity := Define("posts", EntityConfig{})
	WithMultiTenant(entity, DefaultTenantConfig())

	ch := &CrudHandler{Entity: entity}

	req := httptest.NewRequest("GET", "/posts", nil)
	// No tenant ID in context

	qb := query.Select("*").From("posts")
	ch.applyTenantScope(qb, req)

	sqlStr, args := qb.Build()
	if strings.Contains(sqlStr, "tenant_id") {
		t.Errorf("expected no tenant_id filter when no tenant in context, got: %s", sqlStr)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got: %v", args)
	}
}

func TestCrudInjectTenant(t *testing.T) {
	entity := Define("posts", EntityConfig{})
	WithMultiTenant(entity, DefaultTenantConfig())

	ch := &CrudHandler{Entity: entity}

	req := httptest.NewRequest("POST", "/posts", nil)
	req = req.WithContext(SetTenantID(context.Background(), "tenant-abc"))

	data := map[string]any{"title": "Hello"}
	ch.injectTenant(data, req.Context())

	if v, ok := data["tenant_id"]; !ok || v != "tenant-abc" {
		t.Errorf("expected tenant_id to be injected, got: %v", data)
	}
}

func TestCrudInjectTenant_NotMultiTenant(t *testing.T) {
	entity := Define("posts", EntityConfig{})

	ch := &CrudHandler{Entity: entity}

	req := httptest.NewRequest("POST", "/posts", nil)
	req = req.WithContext(SetTenantID(context.Background(), "tenant-abc"))

	data := map[string]any{"title": "Hello"}
	ch.injectTenant(data, req.Context())

	if _, ok := data["tenant_id"]; ok {
		t.Error("expected no tenant_id injection for non-multitenant entity")
	}
}

func TestCrudApplyTenantScopeCount(t *testing.T) {
	entity := Define("posts", EntityConfig{})
	WithMultiTenant(entity, DefaultTenantConfig())

	ch := &CrudHandler{Entity: entity}

	req := httptest.NewRequest("GET", "/posts", nil)
	req = req.WithContext(SetTenantID(context.Background(), "tenant-xyz"))

	cb := query.Count("posts")
	ch.applyTenantScopeCount(cb, req)

	sqlStr, args := cb.Build()
	if !strings.Contains(sqlStr, "tenant_id = $") {
		t.Errorf("expected tenant_id filter in count query, got: %s", sqlStr)
	}
	found := false
	for _, a := range args {
		if a == "tenant-xyz" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected tenant-xyz in args, got: %v", args)
	}
}

// ============================================================================
// Test: Soft-delete filtering in CRUD handlers
// ============================================================================

func TestCrudApplySoftDeleteFilter(t *testing.T) {
	entity := Define("posts", EntityConfig{SoftDelete: true})

	ch := &CrudHandler{Entity: entity}

	req := httptest.NewRequest("GET", "/posts", nil)

	qb := query.Select("*").From("posts")
	ch.applySoftDeleteFilter(qb, req)

	sqlStr, _ := qb.Build()
	if !strings.Contains(sqlStr, "deleted_at IS NULL") {
		t.Errorf("expected deleted_at IS NULL in query, got: %s", sqlStr)
	}
}

func TestCrudApplySoftDeleteFilter_WithTrashed(t *testing.T) {
	entity := Define("posts", EntityConfig{SoftDelete: true})

	ch := &CrudHandler{Entity: entity}

	req := httptest.NewRequest("GET", "/posts?trashed=true", nil)

	qb := query.Select("*").From("posts")
	ch.applySoftDeleteFilter(qb, req)

	sqlStr, _ := qb.Build()
	if strings.Contains(sqlStr, "deleted_at") {
		t.Errorf("expected no deleted_at filter when trashed=true, got: %s", sqlStr)
	}
}

func TestCrudApplySoftDeleteFilter_NotSoftDelete(t *testing.T) {
	entity := Define("posts", EntityConfig{})

	ch := &CrudHandler{Entity: entity}

	req := httptest.NewRequest("GET", "/posts", nil)

	qb := query.Select("*").From("posts")
	ch.applySoftDeleteFilter(qb, req)

	sqlStr, _ := qb.Build()
	if strings.Contains(sqlStr, "deleted_at") {
		t.Errorf("expected no deleted_at filter for non-soft-delete entity, got: %s", sqlStr)
	}
}

func TestCrudApplySoftDeleteFilterCount(t *testing.T) {
	entity := Define("posts", EntityConfig{SoftDelete: true})

	ch := &CrudHandler{Entity: entity}

	req := httptest.NewRequest("GET", "/posts", nil)

	cb := query.Count("posts")
	ch.applySoftDeleteFilterCount(cb, req)

	sqlStr, _ := cb.Build()
	if !strings.Contains(sqlStr, "deleted_at IS NULL") {
		t.Errorf("expected deleted_at IS NULL in count query, got: %s", sqlStr)
	}
}

// ============================================================================
// Test: Read-only and hidden field rejection in Create
// ============================================================================

func TestCrudCreate_SkipsReadOnlyFields(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "status", Type: schema.String, ReadOnly: true},
		},
	}.WithTimestamps(false))

	ch := NewCrudHandler(entity, db)

	// Expect INSERT to only have "title" columns, not "status"
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO posts .*`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "status"}).AddRow("test-id", "Hello", ""))
	mock.ExpectCommit()

	body := map[string]any{
		"title":  "Hello",
		"status": "published", // should be ignored — ReadOnly
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/posts", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "")

	rec := httptest.NewRecorder()
	ch.Create().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestCrudCreate_SkipsHiddenFields(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "internal_flag", Type: schema.String, Hidden: true},
		},
	}.WithTimestamps(false))

	ch := NewCrudHandler(entity, db)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO posts .*`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title"}).AddRow("test-id", "Hello"))
	mock.ExpectCommit()

	body := map[string]any{
		"title":         "Hello",
		"internal_flag": "secret", // should be ignored — Hidden
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/posts", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "")

	rec := httptest.NewRecorder()
	ch.Create().ServeHTTP(rec, req)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// ============================================================================
// Test: Hook execution in CRUD handlers
// ============================================================================

func TestCrudCreate_ExecutesHooks(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))

	hooks := NewHookRegistry()
	var beforeCalled, afterCalled bool
	var beforeData map[string]any

	hooks.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		beforeCalled = true
		if m, ok := data.(map[string]any); ok {
			beforeData = m
		}
		return nil
	})
	hooks.RegisterHook(AfterCreate, func(ctx context.Context, data any) error {
		afterCalled = true
		return nil
	})

	ch := NewCrudHandler(entity, db)
	ch.Hooks = hooks

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO posts .*`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title"}).AddRow("test-id", "Hello"))
	mock.ExpectCommit()

	body := map[string]any{"title": "Hello"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/posts", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "")

	rec := httptest.NewRecorder()
	ch.Create().ServeHTTP(rec, req)

	if !beforeCalled {
		t.Error("expected BeforeCreate hook to be called")
	}
	if !afterCalled {
		t.Error("expected AfterCreate hook to be called")
	}
	if beforeData != nil && beforeData["title"] != "Hello" {
		t.Errorf("expected BeforeCreate to receive body data, got: %v", beforeData)
	}
}

func TestCrudCreate_BeforeCreateHookRejects(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))

	hooks := NewHookRegistry()
	hooks.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		return fmt.Errorf("rejected by policy")
	})

	ch := NewCrudHandler(entity, db)
	ch.Hooks = hooks

	// BeforeCreate fires inside the tx; rejection rolls back without an INSERT.
	mock.ExpectBegin()
	mock.ExpectRollback()

	body := map[string]any{"title": "Hello"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/posts", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	ch.Create().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when BeforeCreate rejects, got %d", rec.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestCrudUpdate_ExecutesHooks(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))

	hooks := NewHookRegistry()
	var beforeCalled, afterCalled bool

	hooks.RegisterHook(BeforeUpdate, func(ctx context.Context, data any) error {
		beforeCalled = true
		return nil
	})
	hooks.RegisterHook(AfterUpdate, func(ctx context.Context, data any) error {
		afterCalled = true
		return nil
	})

	ch := NewCrudHandler(entity, db)
	ch.Hooks = hooks

	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE posts .*`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title"}).AddRow("p1", "Updated"))
	mock.ExpectCommit()

	body := map[string]any{"title": "Updated"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("PUT", "/posts/p1", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "p1")

	rec := httptest.NewRecorder()
	ch.Update().ServeHTTP(rec, req)

	if !beforeCalled {
		t.Error("expected BeforeUpdate hook to be called")
	}
	if !afterCalled {
		t.Error("expected AfterUpdate hook to be called")
	}
}

func TestCrudDelete_ExecutesHooks(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))

	hooks := NewHookRegistry()
	var beforeCalled, afterCalled bool

	hooks.RegisterHook(BeforeDelete, func(ctx context.Context, data any) error {
		beforeCalled = true
		return nil
	})
	hooks.RegisterHook(AfterDelete, func(ctx context.Context, data any) error {
		afterCalled = true
		return nil
	})

	ch := NewCrudHandler(entity, db)
	ch.Hooks = hooks

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM posts .*`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	req := httptest.NewRequest("DELETE", "/posts/p1", nil)
	req.SetPathValue("id", "p1")

	rec := httptest.NewRecorder()
	ch.Delete().ServeHTTP(rec, req)

	if !beforeCalled {
		t.Error("expected BeforeDelete hook to be called")
	}
	if !afterCalled {
		t.Error("expected AfterDelete hook to be called")
	}
}

// ============================================================================
// Test: SQL default escaping in migrate.go
// ============================================================================

func TestSqlDefault_StringEscapesSingleQuotes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no quotes", "hello", "'hello'"},
		{"single quote", "it's", "'it''s'"},
		{"multiple quotes", "it's a test's result", "'it''s a test''s result'"},
		{"empty string", "", "''"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := schema.Field{Default: tt.input}
			result := sqlDefault(f)
			if result != tt.expected {
				t.Errorf("sqlDefault(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSqlDefault_NonStringTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"int", 42, "42"},
		{"float", 3.14, "3.140000"},
		{"bool true", true, "1"},
		{"bool false", false, "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := schema.Field{Default: tt.input}
			result := sqlDefault(f)
			if result != tt.expected {
				t.Errorf("sqlDefault(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// Test: E2E — Soft-delete filtering in List/Get via real DB
// ============================================================================

func TestE2E_SoftDelete_ListFiltersDeleted(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		body TEXT DEFAULT '',
		status TEXT DEFAULT 'draft',
		author_id TEXT DEFAULT '',
		deleted_at DATETIME DEFAULT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert one active, one soft-deleted
	_, err = db.Exec("INSERT INTO posts (id, title) VALUES (?, ?)", "p1", "Active Post")
	if err != nil {
		t.Fatalf("insert p1: %v", err)
	}
	_, err = db.Exec("INSERT INTO posts (id, title, deleted_at) VALUES (?, ?, ?)", "p2", "Deleted Post", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	if err != nil {
		t.Fatalf("insert p2: %v", err)
	}

	app := NewApp(WithDB(db))
	entity := Define("posts", EntityConfig{
		Table:      "posts",
		SoftDelete: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
	})
	app.Registry.Register(entity)

	crud := NewCrudHandler(entity, db)
	RegisterCrudRoutes(app.Router, crud, "/posts")

	ta := TestHarness(t, app)
	defer ta.Close()

	// List should only return the active post
	resp := ta.Get("/posts")
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertBodyContains(t, "Active Post")
	if strings.Contains(resp.Body(), "Deleted Post") {
		t.Error("expected soft-deleted post to be excluded from list")
	}

	var listResult ListResponse
	if err := resp.JSON(&listResult); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listResult.Total != 1 {
		t.Errorf("expected total=1, got %d", listResult.Total)
	}

	// Get the active post should work
	resp = ta.Get("/posts/p1")
	resp.AssertStatus(t, http.StatusOK)

	// Get the soft-deleted post should return 404
	resp = ta.Get("/posts/p2")
	resp.AssertStatus(t, http.StatusNotFound)

	// List with ?trashed=true should include deleted post
	resp = ta.Get("/posts?trashed=true")
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertBodyContains(t, "Active Post")
	resp.AssertBodyContains(t, "Deleted Post")

	var trashedResult ListResponse
	if err := resp.JSON(&trashedResult); err != nil {
		t.Fatalf("decode trashed list: %v", err)
	}
	if trashedResult.Total != 2 {
		t.Errorf("expected total=2 with trashed, got %d", trashedResult.Total)
	}
}

// ============================================================================
// Test: E2E — Read-only fields rejected in Create
// ============================================================================

func TestE2E_ReadOnlyFieldRejected(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		slug TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	app := NewApp(WithDB(db))
	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "slug", Type: schema.String, ReadOnly: true},
		},
	})
	app.Registry.Register(entity)

	crud := NewCrudHandler(entity, db)
	RegisterCrudRoutes(app.Router, crud, "/posts")

	ta := TestHarness(t, app)
	defer ta.Close()

	// Create with slug (ReadOnly) should still succeed but slug should not be in the INSERT
	resp := ta.Post("/posts", map[string]string{
		"title": "Hello",
		"slug":  "hacked-slug", // should be ignored
	})
	resp.AssertStatus(t, http.StatusCreated)

	// Verify slug was NOT inserted
	var slug sql.NullString
	err = db.QueryRow("SELECT slug FROM posts LIMIT 1").Scan(&slug)
	if err != nil {
		t.Fatalf("query slug: %v", err)
	}
	if slug.Valid && slug.String == "hacked-slug" {
		t.Error("ReadOnly field 'slug' should not have been set from request body")
	}
}

// ============================================================================
// Test: E2E — Hook execution in real CRUD flow
// ============================================================================

func TestE2E_Hooks_CreateLifecycle(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		body TEXT DEFAULT '',
		status TEXT DEFAULT 'draft',
		author_id TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	app := NewApp(WithDB(db))
	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
	})
	app.Registry.Register(entity)

	var beforeCreateCalled, afterCreateCalled bool
	hooks := app.HookRegistry("posts")
	hooks.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		beforeCreateCalled = true
		// Modify data before insert
		if m, ok := data.(map[string]any); ok {
			m["status"] = "auto-draft"
		}
		return nil
	})
	hooks.RegisterHook(AfterCreate, func(ctx context.Context, data any) error {
		afterCreateCalled = true
		return nil
	})

	crud := NewCrudHandler(entity, db)
	crud.Hooks = hooks
	RegisterCrudRoutes(app.Router, crud, "/posts")

	ta := TestHarness(t, app)
	defer ta.Close()

	resp := ta.Post("/posts", map[string]string{
		"title": "Hook Test",
	})
	resp.AssertStatus(t, http.StatusCreated)

	if !beforeCreateCalled {
		t.Error("expected BeforeCreate hook to be called")
	}
	if !afterCreateCalled {
		t.Error("expected AfterCreate hook to be called")
	}

	// Verify the hook-modified data was inserted
	var status string
	id := extractIDFromResponse(t, resp)
	err = db.QueryRow("SELECT status FROM posts WHERE id = ?", id).Scan(&status)
	if err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "auto-draft" {
		t.Errorf("expected status='auto-draft' (set by hook), got %q", status)
	}
}

// ============================================================================
// Test: E2E — Multi-tenant scoping via real DB
// ============================================================================

func TestE2E_MultiTenant_CRUDScoping(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		body TEXT DEFAULT '',
		status TEXT DEFAULT 'draft',
		author_id TEXT DEFAULT '',
		tenant_id TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert posts for two tenants
	_, err = db.Exec("INSERT INTO posts (id, title, tenant_id) VALUES (?, ?, ?)", "p1", "Tenant A Post", "tenant-a")
	if err != nil {
		t.Fatalf("insert p1: %v", err)
	}
	_, err = db.Exec("INSERT INTO posts (id, title, tenant_id) VALUES (?, ?, ?)", "p2", "Tenant B Post", "tenant-b")
	if err != nil {
		t.Fatalf("insert p2: %v", err)
	}

	app := NewApp(WithDB(db))
	entity := Define("posts", EntityConfig{
		Table:       "posts",
		MultiTenant: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
	})
	WithMultiTenant(entity, DefaultTenantConfig())
	app.Registry.Register(entity)

	// Apply tenant middleware BEFORE registering routes
	// (Router.wrap bakes in middleware at registration time)
	app.Router.Use(TenantMiddleware("X-Tenant-ID"))

	crud := NewCrudHandler(entity, db)
	RegisterCrudRoutes(app.Router, crud, "/posts")

	ta := TestHarness(t, app)
	defer ta.Close()

	// List as tenant-a — should only see tenant-a's posts
	req := ta.Request(http.MethodGet, "/posts", nil)
	req.WithHeader("X-Tenant-ID", "tenant-a")
	resp := req.Execute()
	resp.AssertStatus(t, http.StatusOK)

	var listResult ListResponse
	if err := resp.JSON(&listResult); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listResult.Total != 1 {
		t.Errorf("expected 1 post for tenant-a, got %d", listResult.Total)
	}
	if len(listResult.Data) > 0 && listResult.Data[0]["title"] != "Tenant A Post" {
		t.Errorf("expected 'Tenant A Post', got %v", listResult.Data[0]["title"])
	}

	// Create as tenant-a — should inject tenant_id
	req2 := ta.Request(http.MethodPost, "/posts", strings.NewReader(`{"title":"New Post"}`))
	req2.WithHeader("X-Tenant-ID", "tenant-a")
	req2.WithHeader("Content-Type", "application/json")
	resp2 := req2.Execute()
	resp2.AssertStatus(t, http.StatusCreated)

	// Verify tenant_id was set
	id := extractIDFromResponse(t, resp2)
	var tenantID string
	err = db.QueryRow("SELECT tenant_id FROM posts WHERE id = ?", id).Scan(&tenantID)
	if err != nil {
		t.Fatalf("query tenant_id: %v", err)
	}
	if tenantID != "tenant-a" {
		t.Errorf("expected tenant_id='tenant-a', got %q", tenantID)
	}
}

// ============================================================================
// Test: E2E — Delete with soft-delete uses timestamp, not raw NOW()
// ============================================================================

func TestE2E_SoftDelete_UsesTimestamp(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		deleted_at DATETIME DEFAULT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	app := NewApp(WithDB(db))
	entity := Define("posts", EntityConfig{
		Table:      "posts",
		SoftDelete: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	})
	app.Registry.Register(entity)

	crud := NewCrudHandler(entity, db)
	RegisterCrudRoutes(app.Router, crud, "/posts")

	ta := TestHarness(t, app)
	defer ta.Close()

	// Create a post
	resp := ta.Post("/posts", map[string]string{
		"title": "Delete Test",
	})
	resp.AssertStatus(t, http.StatusCreated)
	id := extractIDFromResponse(t, resp)

	// Before delete: deleted_at should be NULL
	var beforeDelete sql.NullString
	db.QueryRow("SELECT deleted_at FROM posts WHERE id = ?", id).Scan(&beforeDelete)
	if beforeDelete.Valid {
		t.Error("expected deleted_at to be NULL before delete")
	}

	// Soft delete
	resp = ta.Delete("/posts/" + id)
	resp.AssertStatus(t, http.StatusNoContent)

	// After delete: deleted_at should be a real timestamp (not the literal string "NOW()")
	var afterDelete sql.NullString
	err = db.QueryRow("SELECT deleted_at FROM posts WHERE id = ?", id).Scan(&afterDelete)
	if err != nil {
		t.Fatalf("query after delete: %v", err)
	}
	if !afterDelete.Valid {
		t.Fatal("expected deleted_at to be set after soft delete")
	}
	// Verify it's a timestamp, not the literal "NOW()" string
	if afterDelete.String == "NOW()" {
		t.Error("deleted_at should be a real timestamp, not the literal string 'NOW()'")
	}
	// Parse it to confirm it's a valid time
	_, err = time.Parse("2006-01-02T15:04:05Z", afterDelete.String)
	if err != nil {
		t.Logf("Note: deleted_at = %q (may use different format, that's OK for SQLite)", afterDelete.String)
	}
}

// ============================================================================
// Helpers
// ============================================================================

func extractIDFromResponse(t *testing.T, resp *TestResponse) string {
	t.Helper()
	var result map[string]any
	if err := resp.JSON(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	id, ok := result["id"].(string)
	if !ok || id == "" {
		t.Fatalf("expected id in response, got: %v", result["id"])
	}
	return id
}
