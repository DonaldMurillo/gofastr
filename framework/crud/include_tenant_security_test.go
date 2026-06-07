package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// TestInclude_RelatedTableTenantScope pins that ?include=relation applies
// tenant scoping to the JOINED entity, not just the parent. Attack: a
// request scoped to tenant-A asks for `/posts?include=comments` and receives
// tenant-B's comments because the related table only filters by post_id,
// ignoring its own tenant_id column.
//
// Both entities are MultiTenant. A post in tenant-A carries comments seeded
// for BOTH tenants on the same post_id. The include must omit tenant-B's
// comment.
func TestInclude_RelatedTableTenantScope(t *testing.T) {
	ddl := `
CREATE TABLE posts (
	id        TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	title     TEXT
);
CREATE TABLE comments (
	id        TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	post_id   TEXT NOT NULL,
	body      TEXT
);
`
	postCfg := makeEntityConfig("posts", "posts", "",
		[]schema.Field{
			{Name: "title", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.MultiTenant = true
			c.Relations = []entity.Relation{
				entity.HasMany("comments", "comments", "post_id"),
			}
		},
	)
	commentCfg := makeEntityConfig("comments", "comments", "",
		[]schema.Field{
			{Name: "post_id", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.MultiTenant = true
		},
	)

	ch, db := setupSecurityTestHandler(t, postCfg, ddl)
	commentEnt := entity.Define(commentCfg.Table, commentCfg)
	commentEnt.SetDB(db)
	reg := newTestRegistry(t)
	reg.add(t, ch.Entity)
	reg.add(t, commentEnt)
	ch.Registry = reg

	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "tenant_id": "tenant-A", "title": "a post"},
	})
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c-a", "tenant_id": "tenant-A", "post_id": "p1", "body": "tenant A comment"},
		{"id": "c-b", "tenant_id": "tenant-B", "post_id": "p1", "body": "tenant B secret"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/posts?include=comments",
	})
	req = req.WithContext(tenant.SetTenantID(req.Context(), "tenant-A"))
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list+include returned %d (body=%s)", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "tenant B secret") {
		t.Errorf("SECURITY: [idor] include=comments leaked tenant-B's comment to tenant-A. Attack: related-table tenant scope missing. Body: %s", body)
	}
	if !strings.Contains(body, "tenant A comment") {
		t.Errorf("tenant-A's own comment missing from include — tenant scope too aggressive? Body: %s", body)
	}
}

// TestInclude_EmptyTenantFailsClosed pins that when no tenant is in context,
// a MultiTenant included child fails closed (matches nothing) rather than
// returning every tenant's rows.
func TestInclude_EmptyTenantFailsClosed(t *testing.T) {
	ddl := `
CREATE TABLE posts (
	id        TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	title     TEXT
);
CREATE TABLE comments (
	id        TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	post_id   TEXT NOT NULL,
	body      TEXT
);
`
	postCfg := makeEntityConfig("posts", "posts", "",
		[]schema.Field{{Name: "title", Type: schema.String}},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{
				entity.HasMany("comments", "comments", "post_id"),
			}
		},
	)
	commentCfg := makeEntityConfig("comments", "comments", "",
		[]schema.Field{
			{Name: "post_id", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.MultiTenant = true
		},
	)

	ch, db := setupSecurityTestHandler(t, postCfg, ddl)
	commentEnt := entity.Define(commentCfg.Table, commentCfg)
	commentEnt.SetDB(db)
	reg := newTestRegistry(t)
	reg.add(t, ch.Entity)
	reg.add(t, commentEnt)
	ch.Registry = reg

	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "tenant_id": "tenant-A", "title": "a post"},
	})
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c-a", "tenant_id": "tenant-A", "post_id": "p1", "body": "tenant A comment"},
		{"id": "c-b", "tenant_id": "tenant-B", "post_id": "p1", "body": "tenant B secret"},
	})

	// No tenant in context.
	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/posts?include=comments",
	})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list+include returned %d (body=%s)", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "tenant A comment") || strings.Contains(body, "tenant B secret") {
		t.Errorf("SECURITY: [idor] empty tenant context did not fail closed — leaked a tenant's comments. Body: %s", body)
	}
}
