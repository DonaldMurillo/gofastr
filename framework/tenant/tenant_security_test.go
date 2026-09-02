package tenant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
)

func TestTenantMiddleware_DoesNotTrustClientHeader(t *testing.T) {
	var seen string
	h := TenantMiddleware("X-Tenant-ID")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = GetTenantID(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Tenant-ID", "victim-tenant")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if seen != "" {
		t.Fatalf("SECURITY: [tenant] middleware trusted raw client header and set tenant=%q. Attack: forged tenant identity via X-Tenant-ID.", seen)
	}
}

func TestApplyTenantFilter_EmptyTenantDoesNotLeaveQueryUnscoped(t *testing.T) {
	qb := query.Select("id").From("items")
	ApplyTenantFilter(qb, "")
	sqlStr, _ := qb.Build()
	sqlLower := strings.ToLower(sqlStr)
	if !strings.Contains(sqlLower, "tenant_id") && !strings.Contains(sqlLower, "where 1=0") && !strings.Contains(sqlLower, "where false") {
		t.Fatalf("SECURITY: [tenant] empty tenant left query unscoped: %s", sqlStr)
	}
}

// Property: the tenant helpers fail CLOSED on a missing tenant, and the
// injection leg must be symmetric with the filter leg pinned above
// (empty tenant → WHERE 1=0, never unscoped). InjectTenantID documents
// that it "ensures records are associated with the current tenant from
// the context"; with NO tenant in the context it currently no-ops,
// leaving any caller-supplied tenant_id in the write payload untouched —
// a request body naming another tenant's id then writes a cross-tenant
// row on exactly the requests where tenant resolution failed.
func TestInjectTenantIDFailsClosedNoTenant(t *testing.T) {
	// Attack shape: request-body tenant_id survives the inject step.
	data := map[string]any{"tenant_id": "victim-tenant", "name": "x"}
	InjectTenantID(data, context.Background())
	if v, ok := data["tenant_id"]; ok {
		t.Fatalf("SECURITY: [tenant] InjectTenantID left caller-supplied tenant_id=%v in the write payload with no tenant in ctx. Attack: body-named tenant id becomes the row's tenant when tenant resolution fails.", v)
	}
	// Sibling keys are untouched either way.
	if data["name"] != "x" {
		t.Fatalf("unrelated key mutated: %v", data)
	}
	// Positive control: a resolved tenant still overwrites the body value.
	data2 := map[string]any{"tenant_id": "victim-tenant"}
	InjectTenantID(data2, SetTenantID(context.Background(), "real-tenant"))
	if data2["tenant_id"] != "real-tenant" {
		t.Fatalf("resolved tenant did not overwrite body value: %v", data2["tenant_id"])
	}
}
