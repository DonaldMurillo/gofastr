package tenant

import (
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
