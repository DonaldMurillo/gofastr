package framework

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ============================================================================
// EntityLLMMD generates comprehensive markdown for an entity
// ============================================================================

func TestEntityLLMMD_BasicEntity(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "published"}},
			{Name: "views", Type: schema.Int},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "users", "author_id"),
			entity.HasMany("comments", "comments", "post_id"),
		},
	})

	md := crud.EntityLLMMD(ent)

	// Must contain the entity name as header
	if !strings.Contains(md, "# posts") {
		t.Error("expected '# posts' header")
	}

	// Must contain all visible fields
	for _, name := range []string{"title", "body", "status", "views"} {
		if !strings.Contains(md, "`"+name+"`") {
			t.Errorf("expected field %q in markdown", name)
		}
	}

	// Must contain field types
	if !strings.Contains(md, "string") {
		t.Error("expected 'string' type label")
	}
	if !strings.Contains(md, "integer") {
		t.Error("expected 'integer' type label")
	}

	// Must contain required marker
	if !strings.Contains(md, "**required**") {
		t.Error("expected '**required**' for title field")
	}

	// Must contain enum values
	if !strings.Contains(md, "draft|published") {
		t.Error("expected enum values 'draft|published'")
	}

	// Must list all endpoints
	for _, ep := range []string{
		"GET /posts",
		"GET /posts/{id}",
		"POST /posts",
		"PUT /posts/{id}",
		"DELETE /posts/{id}",
		"POST /posts/_batch",
		"PATCH /posts/_batch",
		"DELETE /posts/_batch",
		"GET /posts/_events",
	} {
		if !strings.Contains(md, ep) {
			t.Errorf("expected endpoint %q in markdown", ep)
		}
	}

	// Must contain include/relation info
	if !strings.Contains(md, "## Includes") {
		t.Error("expected '## Includes' section")
	}
	if !strings.Contains(md, "author") {
		t.Error("expected 'author' relation")
	}
	if !strings.Contains(md, "comments") {
		t.Error("expected 'comments' relation")
	}

	// Must contain filter operators
	if !strings.Contains(md, "_gt") {
		t.Error("expected '_gt' filter operator")
	}
	if !strings.Contains(md, "_like") {
		t.Error("expected '_like' filter operator")
	}
	if !strings.Contains(md, "_in") {
		t.Error("expected '_in' filter operator")
	}

	// Must contain pagination docs (offset + cursor)
	if !strings.Contains(md, "cursor") {
		t.Error("expected cursor pagination documentation")
	}
	if !strings.Contains(md, "totalPages") {
		t.Error("expected offset pagination documentation")
	}
}

// ============================================================================
// Soft-delete entity includes note
// ============================================================================

func TestEntityLLMMD_SoftDelete(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{
		SoftDelete: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	})

	md := crud.EntityLLMMD(ent)
	if !strings.Contains(md, "soft-delete") {
		t.Error("expected soft-delete note in markdown")
	}
}

// ============================================================================
// Multi-tenant entity includes note
// ============================================================================

func TestEntityLLMMD_MultiTenant(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{
		MultiTenant: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	})

	md := crud.EntityLLMMD(ent)
	if !strings.Contains(md, "Multi-tenancy") {
		t.Error("expected Multi-tenancy section")
	}
}

// ============================================================================
// Custom endpoints section
// ============================================================================

func TestEntityLLMMD_CustomEndpoints(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
		},
		Endpoints: []entity.Endpoint{
			{Method: "POST", Path: "{id}/publish", Description: "Publish a draft post"},
			{Method: "GET", Path: "{id}/stats", Description: "View stats"},
		},
	})

	md := crud.EntityLLMMD(ent)
	if !strings.Contains(md, "## Custom Endpoints") {
		t.Error("expected '## Custom Endpoints' section")
	}
	if !strings.Contains(md, "Publish a draft post") {
		t.Error("expected custom endpoint description")
	}
	if !strings.Contains(md, "View stats") {
		t.Error("expected custom endpoint description")
	}
}

// ============================================================================
// Hidden fields are excluded
// ============================================================================

func TestEntityLLMMD_HiddenFieldsExcluded(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "internal_flag", Type: schema.Bool, Hidden: true},
		},
	})

	md := crud.EntityLLMMD(ent)
	if strings.Contains(md, "internal_flag") {
		t.Error("hidden field should not appear in markdown")
	}
	if !strings.Contains(md, "title") {
		t.Error("visible field should appear in markdown")
	}
}

// ============================================================================
// RegistryLLMMD generates a top-level index
// ============================================================================

func TestRegistryLLMMD_Index(t *testing.T) {
	reg := NewTestRegistry(t,
		"posts", entity.EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		},
		"users", entity.EntityConfig{
			Fields: []schema.Field{{Name: "email", Type: schema.String}},
		},
	)

	md := crud.RegistryLLMMD(reg, "Test App")

	if !strings.Contains(md, "# Test App") {
		t.Error("expected app title header")
	}
	if !strings.Contains(md, "posts") {
		t.Error("expected 'posts' entity in index")
	}
	if !strings.Contains(md, "users") {
		t.Error("expected 'users' entity in index")
	}
	if !strings.Contains(md, "/posts/llm.md") {
		t.Error("expected link to posts/llm.md")
	}
	if !strings.Contains(md, "/users/llm.md") {
		t.Error("expected link to users/llm.md")
	}
	if !strings.Contains(md, "## Quick Reference") {
		t.Error("expected Quick Reference section")
	}
	if !strings.Contains(md, "/llm-pages.md") {
		t.Error("expected link to /llm-pages.md in registry index")
	}
}

// ============================================================================
// LLMMDHandler serves markdown over HTTP
// ============================================================================

func TestLLMMDHandler_HTTP(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	})

	handler := crud.LLMMDHandler(ent)
	req := httptest.NewRequest("GET", "/posts/llm.md", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/markdown") {
		t.Errorf("expected text/markdown content type, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "# posts") {
		t.Error("expected markdown body with '# posts'")
	}
}

// ============================================================================
// RegistryLLMMDHandler serves index markdown over HTTP
// ============================================================================

func TestRegisterCrudRoutesFunc_NoLLMMD(t *testing.T) {
	db := setupTestDB(t, DialectSQLite)
	defer db.Close()

	ent := entity.Define("items", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	})

	mux := router.New()
	handler := crud.RegisterCrudRoutesFunc(mux, ent, db, "/items", crud.CrudRouteOptions{NoLLMMD: true})
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}

	// The llm.md route should NOT be registered
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/items/llm.md", nil)
	mux.ServeHTTP(rec, req)

	// Should get 404 or method not allowed — not 200 with markdown
	if rec.Code == 200 {
		t.Error("expected /items/llm.md to NOT be registered with NoLLMMD=true")
	}
}

func TestRegistryLLMMDHandler_HTTP(t *testing.T) {
	reg := NewTestRegistry(t,
		"posts", entity.EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		},
	)

	handler := crud.RegistryLLMMDHandler(reg, "Test")
	req := httptest.NewRequest("GET", "/llm.md", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/markdown") {
		t.Errorf("expected text/markdown content type, got %q", ct)
	}
}

// ============================================================================
// Helper: in-memory registry for testing
// ============================================================================

// NewTestRegistry creates a framework.Registry with the given entities
// registered by name. Uses the framework's own Registry type so the test
// lives in package framework.
// ============================================================================
// TDD: batch size must appear in entity llm.md
// ============================================================================

func TestEntityLLMMD_BatchSizeDocumented(t *testing.T) {
	ent := entity.Define("products", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	})
	md := crud.EntityLLMMD(ent)

	if !strings.Contains(md, fmt.Sprintf("Maximum %d items per batch", crud.MaxBatchSize)) {
		t.Error("expected batch size limit to appear in entity llm.md")
	}
}

func TestEntityLLMMD_DefaultSanitized(t *testing.T) {
	ent := entity.Define("items", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
			{Name: "config", Type: schema.JSON, Default: map[string]any{"secret": "api-key-123"}},
			{Name: "long", Type: schema.String, Default: strings.Repeat("x", 200)},
		},
	})
	md := crud.EntityLLMMD(ent)

	// Complex types should show type name, not contents
	if strings.Contains(md, "secret") {
		t.Error("complex default should not expose internal map contents")
	}
	if strings.Contains(md, "api-key") {
		t.Error("complex default should not expose secret values")
	}
	// Long values should be truncated
	if strings.Contains(md, strings.Repeat("x", 200)) {
		t.Error("long default should be truncated")
	}
	// Simple defaults should still appear
	if !strings.Contains(md, "default:") {
		t.Error("expected default annotation for fields with defaults")
	}
}

func NewTestRegistry(t *testing.T, nameConfigs ...any) *Registry {
	t.Helper()
	if len(nameConfigs)%2 != 0 {
		t.Fatal("NewTestRegistry expects alternating name, config pairs")
	}
	reg := NewRegistry()
	for i := 0; i < len(nameConfigs); i += 2 {
		name := nameConfigs[i].(string)
		cfg := nameConfigs[i+1].(entity.EntityConfig)
		ent := entity.Define(name, cfg)
		if err := reg.Register(ent); err != nil {
			t.Fatalf("failed to register entity %q: %v", name, err)
		}
	}
	return reg
}
