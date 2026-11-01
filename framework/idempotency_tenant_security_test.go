package framework

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// One credential whose server-side active tenant switches from A to B
// resends the same Idempotency-Key, path and body. Tenant B's write must
// run and answer for B; a retry inside B replays B's response, and a
// request with no tenant still caches on its own.
func TestIdempotencyKeyCarriesTenant(t *testing.T) {
	a := NewApp(WithIdempotency(middleware.IdempotencyConfig{}))
	// The app's tenant resolution, added with Use after NewApp installed
	// the default chain: the shape a real app has (auth, then tenant).
	// The test reads the tenant from a header only to stand in for a
	// server-side "active tenant" lookup.
	a.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id := r.Header.Get("X-Test-Active-Tenant"); id != "" {
				r = r.WithContext(tenant.SetTenantID(r.Context(), id))
			}
			next.ServeHTTP(w, r)
		})
	})
	var calls int32
	a.Router().Post("/orders", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("tenant=" + tenant.GetTenantID(r.Context())))
	}))
	send := func(tenantID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"sku":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer same-credential")
		req.Header.Set("Idempotency-Key", "order-1")
		if tenantID != "" {
			req.Header.Set("X-Test-Active-Tenant", tenantID)
		}
		rr := httptest.NewRecorder()
		a.Router().ServeHTTP(rr, req)
		return rr
	}

	if rr := send("A"); rr.Code != http.StatusCreated || rr.Body.String() != "tenant=A" {
		t.Fatalf("tenant A: %d %q", rr.Code, rr.Body.String())
	}
	rr := send("B")
	if rr.Header().Get("Idempotent-Replay") == "true" || rr.Body.String() != "tenant=B" {
		t.Fatalf("tenant B was served %q (replay=%q); its write must run",
			rr.Body.String(), rr.Header().Get("Idempotent-Replay"))
	}
	if rr := send("B"); rr.Header().Get("Idempotent-Replay") != "true" || rr.Body.String() != "tenant=B" {
		t.Fatalf("tenant B retry: replay=%q body %q, want B's replay",
			rr.Header().Get("Idempotent-Replay"), rr.Body.String())
	}
	if calls != 2 {
		t.Fatalf("handler ran %d times, want 2 (A once, B once)", calls)
	}
	send("")
	if rr := send(""); rr.Header().Get("Idempotent-Replay") != "true" || rr.Body.String() != "tenant=" {
		t.Fatalf("no-tenant retry: replay=%q body %q", rr.Header().Get("Idempotent-Replay"), rr.Body.String())
	}
	if calls != 3 {
		t.Fatalf("handler ran %d times, want 3", calls)
	}
}
