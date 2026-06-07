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

// tenantItemsConfig is the shared MultiTenant entity used by the
// secure-by-default tenant-gate tests below.
func tenantItemsConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Name:  "items",
		Table: "items",
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String},
			{Name: "name", Type: schema.String},
		},
		MultiTenant: true,
	}.WithTimestamps(false)
}

const tenantItemsDDL = `CREATE TABLE items (id TEXT PRIMARY KEY, tenant_id TEXT, name TEXT)`

// TestTenant_ListWithoutContextIsRejected pins the secure-by-default
// contract: a MultiTenant entity listed with NO tenant id in context is
// refused with 401 rather than leaking every tenant's rows. This replaces an
// earlier test that pinned the fail-OPEN behaviour (empty tenant disables
// filtering → returns all rows). That was a silent cross-tenant data leak: an
// unauthenticated request, or any code path that simply forgot to set the
// tenant context, read across every tenant. The in-process CRUD API
// (crud_api.go) already failed closed here; the HTTP path now matches via the
// RequireTenant gate (BREAKING — see CHANGELOG / multi-tenant.md).
//
// Deliberate cross-tenant access (admin tooling) is still possible, but must
// opt in explicitly server-side via tenant.AllowCrossTenant — see
// TestTenant_CrossTenantOptInAllowsAccess.
func TestTenant_ListWithoutContextIsRejected(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, tenantItemsConfig(), tenantItemsDDL)
	seedRows(t, db, "items", []map[string]any{
		{"id": "item-a", "tenant_id": "tenant-A", "name": "A data"},
		{"id": "item-b", "tenant_id": "tenant-B", "name": "B data"},
	})

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items"})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("list without tenant context = %d, want 401. body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "data") && strings.Contains(rr.Body.String(), "tenant-") {
		t.Fatalf("rejected list still leaked tenant rows: %s", rr.Body.String())
	}
}

func TestTenant_GetWithoutContextIsRejected(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, tenantItemsConfig(), tenantItemsDDL)
	seedRows(t, db, "items", []map[string]any{
		{"id": "item-a", "tenant_id": "tenant-A", "name": "A data"},
	})

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items/item-a"})
	req.SetPathValue("id", "item-a")
	rr := httptest.NewRecorder()
	ch.Get()(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("get without tenant context = %d, want 401. body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "A data") {
		t.Fatalf("rejected get still leaked the row: %s", rr.Body.String())
	}
}

func TestTenant_UpdateWithoutContextIsRejected(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, tenantItemsConfig(), tenantItemsDDL)
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

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("update without tenant context = %d, want 401. body=%s", rr.Code, rr.Body.String())
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM items WHERE id = ?`, "item-a").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "A data" {
		t.Fatalf("rejected cross-tenant update still mutated row to %q", name)
	}
}

func TestTenant_DeleteWithoutContextIsRejected(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, tenantItemsConfig(), tenantItemsDDL)
	seedRows(t, db, "items", []map[string]any{
		{"id": "item-a", "tenant_id": "tenant-A", "name": "A data"},
	})

	req := makeRequest(t, RequestOpts{Method: http.MethodDelete, Path: "/items/item-a"})
	req.SetPathValue("id", "item-a")
	rr := httptest.NewRecorder()
	ch.Delete()(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("delete without tenant context = %d, want 401. body=%s", rr.Code, rr.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM items WHERE id = ?`, "item-a").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rejected cross-tenant delete still removed row (count=%d)", count)
	}
}

// TestTenant_CrossTenantOptInAllowsAccess confirms the documented admin escape
// hatch: with tenant.AllowCrossTenant on the context, a MultiTenant List with
// no tenant id is permitted and spans every tenant (the scope helpers no-op on
// an empty tenant id). This is the deliberate, server-side-only opt-in that
// replaces the old "empty context silently disables filtering" behaviour.
func TestTenant_CrossTenantOptInAllowsAccess(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, tenantItemsConfig(), tenantItemsDDL)
	seedRows(t, db, "items", []map[string]any{
		{"id": "item-a", "tenant_id": "tenant-A", "name": "A data"},
		{"id": "item-b", "tenant_id": "tenant-B", "name": "B data"},
	})

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items"})
	req = req.WithContext(tenant.AllowCrossTenant(req.Context()))
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("cross-tenant List = %d, want 200. body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeListResponse(t, rr.Body.String())
	if resp.Total != 2 || len(resp.Data) != 2 {
		t.Fatalf("cross-tenant List: want all 2 rows, got total=%d len=%d", resp.Total, len(resp.Data))
	}
}

// suppress unused imports
var _ = schema.String
var _ = tenant.SetTenantID
