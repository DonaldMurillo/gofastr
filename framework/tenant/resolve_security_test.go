package tenant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// A host middleware that called SetTenantID once left a held stream on
// the tenant it read at connect: RecheckPrincipal refreshed the user and
// replayed the old tenant, so a member removed from a tenant kept its feed
// (2026-10-27 D24). ResolveMiddleware re-runs the lookup on each re-check.
func TestResolveMiddlewareRechecksTenant(t *testing.T) {
	var mu sync.Mutex
	member := map[string]string{"u1": "tenant-a"}
	resolve := func(ctx context.Context) string {
		u, ok := handler.GetUser(ctx)
		if !ok {
			return ""
		}
		mu.Lock()
		defer mu.Unlock()
		return member[u.(string)]
	}
	var held context.Context
	h := ResolveMiddleware(resolve)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		held = r.Context()
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(handler.SetTenantID(handler.SetUser(req.Context(), "u1"), "stale"))
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got := GetTenantID(held); got != "tenant-a" {
		t.Fatalf("request tenant = %q, want tenant-a", got)
	}
	fresh, ok := handler.RecheckPrincipal(held)
	if !ok || GetTenantID(fresh) != "tenant-a" {
		t.Fatalf("unchanged membership re-check = %q, %v; want tenant-a, true", GetTenantID(fresh), ok)
	}
	mu.Lock()
	member["u1"] = "tenant-b"
	mu.Unlock()
	if fresh, _ := handler.RecheckPrincipal(held); GetTenantID(fresh) != "tenant-b" {
		t.Errorf("SECURITY: a re-check kept tenant %q after the member moved to tenant-b", GetTenantID(fresh))
	}
	mu.Lock()
	delete(member, "u1")
	mu.Unlock()
	if fresh, _ := handler.RecheckPrincipal(held); GetTenantID(fresh) != "" {
		t.Errorf("SECURITY: a re-check kept tenant %q after the member left", GetTenantID(fresh))
	}
}
