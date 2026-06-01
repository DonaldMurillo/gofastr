package tenant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestDefaultTenantConfig(t *testing.T) {
	c := DefaultTenantConfig()
	if c.Field != "tenant_id" || c.Header != "X-Tenant-ID" || !c.AutoScope {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestWithMultiTenant_FieldBranches(t *testing.T) {
	// Custom column flows into TenantField.
	custom := WithMultiTenant(&entity.Entity{}, TenantConfig{Field: "org_id"})
	if !custom.Config.MultiTenant || custom.Config.TenantField != "org_id" {
		t.Fatalf("custom field not honored: %+v", custom.Config)
	}
	// Blank field leaves TenantField unset (default applies).
	blank := WithMultiTenant(&entity.Entity{}, TenantConfig{Field: ""})
	if !blank.Config.MultiTenant || blank.Config.TenantField != "" {
		t.Fatalf("blank field should leave TenantField empty: %+v", blank.Config)
	}
	// Explicit default "tenant_id" also leaves TenantField unset.
	def := WithMultiTenant(&entity.Entity{}, TenantConfig{Field: "tenant_id"})
	if !def.Config.MultiTenant || def.Config.TenantField != "" {
		t.Fatalf("default field should leave TenantField empty: %+v", def.Config)
	}
}

func TestApplyTenantFilter_NonEmptyParameterized(t *testing.T) {
	qb := query.Select("id").From("items")
	ApplyTenantFilter(qb, "acme")
	sqlStr, args := qb.Build()
	if !strings.Contains(strings.ToLower(sqlStr), "tenant_id = $1") {
		t.Fatalf("expected parameterized tenant filter, got: %s", sqlStr)
	}
	if len(args) != 1 || args[0] != "acme" {
		t.Fatalf("expected tenant arg [acme], got: %v", args)
	}
}

func TestTenantMiddleware_ServerResolvedTenant(t *testing.T) {
	// Positive path: a server-resolved string tenant is propagated.
	var seen string
	h := TenantMiddleware("X-Tenant-ID")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = GetTenantID(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil).
		WithContext(handler.SetTenant(context.Background(), "acme"))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "acme" {
		t.Fatalf("expected resolved tenant acme, got %q", seen)
	}
}

func TestTenantMiddleware_NoTenant(t *testing.T) {
	var seen = "sentinel"
	h := TenantMiddleware("X-Tenant-ID")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = GetTenantID(r.Context())
	}))
	// No tenant resolved on the context.
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if seen != "" {
		t.Fatalf("expected empty tenant, got %q", seen)
	}
}

func TestTenantMiddleware_NonStringTenantIgnored(t *testing.T) {
	var seen = "sentinel"
	h := TenantMiddleware("X-Tenant-ID")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = GetTenantID(r.Context())
	}))
	// A non-string resolved tenant must not be coerced.
	req := httptest.NewRequest(http.MethodGet, "/", nil).
		WithContext(handler.SetTenant(context.Background(), 42))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "" {
		t.Fatalf("non-string tenant should be ignored, got %q", seen)
	}
}

func TestTenantMiddleware_EmptyStringTenantIgnored(t *testing.T) {
	var seen = "sentinel"
	h := TenantMiddleware("X-Tenant-ID")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = GetTenantID(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil).
		WithContext(handler.SetTenant(context.Background(), ""))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "" {
		t.Fatalf("empty resolved tenant should not set, got %q", seen)
	}
}

func TestSetGetTenantID_RoundTrip(t *testing.T) {
	ctx := SetTenantID(context.Background(), "acme")
	if got := GetTenantID(ctx); got != "acme" {
		t.Fatalf("round trip failed: %q", got)
	}
	if got := GetTenantID(context.Background()); got != "" {
		t.Fatalf("missing tenant should be empty, got %q", got)
	}
}

func TestInjectTenantID(t *testing.T) {
	// With a tenant set, the column is injected.
	data := map[string]any{}
	InjectTenantID(data, SetTenantID(context.Background(), "acme"))
	if data["tenant_id"] != "acme" {
		t.Fatalf("expected injected tenant_id, got %v", data["tenant_id"])
	}
	// Without a tenant, nothing is injected.
	bare := map[string]any{}
	InjectTenantID(bare, context.Background())
	if _, ok := bare["tenant_id"]; ok {
		t.Fatalf("no tenant should inject nothing, got %v", bare)
	}
}
