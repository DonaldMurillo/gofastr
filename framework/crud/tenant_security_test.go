package crud

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// TestTenant_SpoofedHeaderIgnored verifies that a client-supplied
// X-Tenant-ID header does not override the server-set tenant context.
// Attack: spoofing X-Tenant-ID to access another tenant's data.
func TestTenant_SpoofedHeaderIgnored(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String},
			{Name: "name", Type: schema.String},
		},
		MultiTenant: true,
		OwnerField:  "user_id",
	}.WithTimestamps(false), `CREATE TABLE items (id TEXT PRIMARY KEY, tenant_id TEXT, user_id TEXT, name TEXT)`)

	// Add user_id field
	ch, db = setupSecurityTestHandler(t, entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String},
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "name", Type: schema.String},
		},
		MultiTenant: true,
		OwnerField:  "user_id",
	}.WithTimestamps(false), `CREATE TABLE items (id TEXT PRIMARY KEY, tenant_id TEXT, user_id TEXT, name TEXT)`)

	seedRows(t, db, "items", []map[string]any{
		{"id": "item-1", "tenant_id": "tenant-A", "user_id": "alice", "name": "Tenant A data"},
		{"id": "item-2", "tenant_id": "tenant-B", "user_id": "alice", "name": "Tenant B data"},
	})

	// Set the server-side tenant to tenant-A via context
	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/items",
		UserID: "alice",
	})
	// Server-side tenant context (set by middleware)
	ctx := tenant.SetTenantID(req.Context(), "tenant-A")
	req = req.WithContext(ctx)
	// Client tries to spoof a different tenant
	req.Header.Set("X-Tenant-ID", "tenant-B")

	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	// The spoofed header should NOT override the server-set tenant
	assertBodyNotContains(t, rr, "Tenant B data", "tenant",
		"spoofed X-Tenant-ID header overrides server-set tenant context")
}

// TestTenant_MissingTenantOnCreateHandled verifies that creating a
// record on a MultiTenant entity without a tenant in the request
// context is REJECTED. Attack: bypassing tenant scoping by omitting
// the tenant header on create — an orphan row owned by no tenant
// can later be read by an attacker who passes the matching empty
// tenant ID through the filter middleware.
func TestTenant_MissingTenantOnCreateHandled(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, makeEntityConfig("items", "items", "user_id", []schema.Field{
		{Name: "tenant_id", Type: schema.String},
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String},
	}, func(c *entity.EntityConfig) { c.MultiTenant = true }),
		`CREATE TABLE items (id TEXT PRIMARY KEY, tenant_id TEXT, user_id TEXT, name TEXT)`)

	// Create without tenant context
	req := makeRequest(t, RequestOpts{
		Method: http.MethodPost,
		Path:   "/items",
		Body:   `{"name":"orphan record"}`,
		UserID: "alice",
	})
	// No tenant.SetTenantID — tenant is empty
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)

	if rr.Code == http.StatusCreated || rr.Code == http.StatusOK {
		t.Errorf("SECURITY: [tenant] create without tenant succeeded (status %d). Attack: tenant-orphan record can be retrieved by anyone passing an empty X-Tenant-ID.", rr.Code)
	}
}

// TestTenant_CrossTenantBatchDeleteRejected verifies that a batch delete
// scoped to one tenant cannot delete records from another tenant.
// Attack: batch delete with tenant filter bypass.
func TestTenant_CrossTenantBatchDeleteRejected(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String},
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "name", Type: schema.String},
		},
		MultiTenant: true,
		OwnerField:  "user_id",
	}.WithTimestamps(false), `CREATE TABLE items (id TEXT PRIMARY KEY, tenant_id TEXT, user_id TEXT, name TEXT)`)

	seedRows(t, db, "items", []map[string]any{
		{"id": "item-A1", "tenant_id": "tenant-A", "user_id": "alice", "name": "A data"},
		{"id": "item-B1", "tenant_id": "tenant-B", "user_id": "alice", "name": "B data"},
	})

	// Delete as tenant-A — should not affect tenant-B's records
	req := makeRequest(t, RequestOpts{
		Method: http.MethodDelete,
		Path:   "/items/item-B1",
		UserID: "alice",
	})
	req.SetPathValue("id", "item-B1")
	ctx := tenant.SetTenantID(req.Context(), "tenant-A")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	ch.Delete()(rr, req)

	// Tenant-B's record should still exist
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM items WHERE id = ?", "item-B1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("SECURITY: [tenant] cross-tenant delete removed item-B1 from tenant-B while scoped to tenant-A")
	}
}

// TestTenant_TenantOwnerComboEnforced verifies that when both tenant
// and owner scoping are active, both filters are applied.
// Attack: authenticated user in tenant-A reads tenant-B data owned by
// same user ID.
func TestTenant_TenantOwnerComboEnforced(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String},
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "name", Type: schema.String},
		},
		MultiTenant: true,
		OwnerField:  "user_id",
	}.WithTimestamps(false), `CREATE TABLE items (id TEXT PRIMARY KEY, tenant_id TEXT, user_id TEXT, name TEXT)`)

	seedRows(t, db, "items", []map[string]any{
		{"id": "item-A", "tenant_id": "tenant-A", "user_id": "alice", "name": "A secret"},
		{"id": "item-B", "tenant_id": "tenant-B", "user_id": "alice", "name": "B secret"},
	})

	// Alice in tenant-A should only see tenant-A's data
	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/items",
		UserID: "alice",
	})
	ctx := tenant.SetTenantID(req.Context(), "tenant-A")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	assertBodyNotContains(t, rr, "B secret", "tenant",
		"same-owner cross-tenant data leaked when both tenant+owner scoping active")
}

func TestTenant_ListWithoutTenantContextDoesNotReturnCrossTenantRows(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String},
			{Name: "name", Type: schema.String},
		},
		MultiTenant: true,
	}.WithTimestamps(false), `CREATE TABLE items (id TEXT PRIMARY KEY, tenant_id TEXT, name TEXT)`)

	seedRows(t, db, "items", []map[string]any{
		{"id": "item-a", "tenant_id": "tenant-A", "name": "A data"},
		{"id": "item-b", "tenant_id": "tenant-B", "name": "B data"},
	})

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items"})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	if rr.Code == http.StatusOK {
		resp := decodeListResponse(t, rr.Body.String())
		if resp.Total != 0 || len(resp.Data) != 0 {
			t.Fatalf("SECURITY: [tenant] list with no tenant context returned %d rows. Attack: empty-tenant full-table scan.", len(resp.Data))
		}
	}
}

func TestTenant_GetWithoutTenantContextCannotReadTenantRow(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String},
			{Name: "name", Type: schema.String},
		},
		MultiTenant: true,
	}.WithTimestamps(false), `CREATE TABLE items (id TEXT PRIMARY KEY, tenant_id TEXT, name TEXT)`)

	seedRows(t, db, "items", []map[string]any{
		{"id": "item-a", "tenant_id": "tenant-A", "name": "A data"},
	})

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items/item-a"})
	req.SetPathValue("id", "item-a")
	rr := httptest.NewRecorder()
	ch.Get()(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("SECURITY: [tenant] get without tenant context returned 200. Attack: cross-tenant row read by empty tenant context. body=%s", rr.Body.String())
	}
}

func TestTenant_UpdateWithoutTenantContextCannotModifyRow(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String},
			{Name: "name", Type: schema.String},
		},
		MultiTenant: true,
	}.WithTimestamps(false), `CREATE TABLE items (id TEXT PRIMARY KEY, tenant_id TEXT, name TEXT)`)

	seedRows(t, db, "items", []map[string]any{
		{"id": "item-a", "tenant_id": "tenant-A", "name": "A data"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodPut,
		Path:   "/items/item-a",
		Body:   `{"name":"changed by empty tenant"}`,
	})
	req.SetPathValue("id", "item-a")
	rr := httptest.NewRecorder()
	ch.Update()(rr, req)

	var name string
	if err := db.QueryRow(`SELECT name FROM items WHERE id = ?`, "item-a").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if rr.Code == http.StatusOK || name != "A data" {
		t.Fatalf("SECURITY: [tenant] update without tenant context modified row to %q with status %d. Attack: empty-tenant cross-tenant update.", name, rr.Code)
	}
}

func TestTenant_DeleteWithoutTenantContextCannotDeleteRow(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String},
			{Name: "name", Type: schema.String},
		},
		MultiTenant: true,
	}.WithTimestamps(false), `CREATE TABLE items (id TEXT PRIMARY KEY, tenant_id TEXT, name TEXT)`)

	seedRows(t, db, "items", []map[string]any{
		{"id": "item-a", "tenant_id": "tenant-A", "name": "A data"},
	})

	req := makeRequest(t, RequestOpts{Method: http.MethodDelete, Path: "/items/item-a"})
	req.SetPathValue("id", "item-a")
	rr := httptest.NewRecorder()
	ch.Delete()(rr, req)

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM items WHERE id = ?`, "item-a").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if rr.Code == http.StatusNoContent || count == 0 {
		t.Fatalf("SECURITY: [tenant] delete without tenant context removed row. Attack: empty-tenant cross-tenant delete.")
	}
}

// suppress unused imports
var _ = schema.String
var _ = tenant.SetTenantID
