package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

func stampFor(t *testing.T, r *Router, method, path string) (middleware.RouteTimeout, bool) {
	t.Helper()
	var got middleware.RouteTimeout
	var ok bool
	r.Handle(method, path, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got, ok = middleware.RouteTimeoutFromContext(req.Context())
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, r.Prefix()+path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("route did not serve: %d", rec.Code)
	}
	return got, ok
}

func TestNoOverridesLeavesRequestUnstamped(t *testing.T) {
	r := New()
	if _, ok := stampFor(t, r, "GET", "/plain"); ok {
		t.Error("no overrides configured, request must carry no stamp")
	}
}

func TestSetRouteTimeoutStampsBudgetAndPattern(t *testing.T) {
	r := New()
	r.SetRouteTimeout("GET", "/reports/{id}", 90*time.Second)
	got, ok := stampFor(t, r, "GET", "/reports/{id}")
	if !ok {
		t.Fatal("route override configured, stamp missing")
	}
	if got.Budget != 90*time.Second || got.Pattern != "/reports/{id}" || got.Method != "GET" {
		t.Errorf("wrong stamp: %+v", got)
	}
}

func TestGroupTimeoutAppliesToGroupRoutes(t *testing.T) {
	r := New()
	g := r.Group("/admin")
	g.SetTimeout(5 * time.Minute)
	got, ok := stampFor(t, g, "GET", "/exports")
	if !ok || got.Budget != 5*time.Minute {
		t.Errorf("group timeout not resolved: %+v ok=%v", got, ok)
	}
	// A sibling route outside the group stays unstamped.
	if _, ok := stampFor(t, r, "GET", "/outside"); ok {
		t.Error("route outside the group must carry no stamp")
	}
}

func TestRouteOverrideBeatsGroupNearestWins(t *testing.T) {
	r := New()
	outer := r.Group("/api")
	outer.SetTimeout(time.Minute)
	inner := outer.Group("/slow")
	inner.SetTimeout(10 * time.Minute)
	if got, _ := stampFor(t, inner, "GET", "/nested"); got.Budget != 10*time.Minute {
		t.Errorf("nearest group must win, got %v", got.Budget)
	}
	if got, _ := stampFor(t, outer, "GET", "/direct"); got.Budget != time.Minute {
		t.Errorf("outer group budget expected, got %v", got.Budget)
	}
	inner.SetRouteTimeout("GET", "/pinned", time.Second)
	if got, _ := stampFor(t, inner, "GET", "/pinned"); got.Budget != time.Second {
		t.Errorf("route override must beat groups, got %v", got.Budget)
	}
}

func TestRouteTimeoutThroughRealChain(t *testing.T) {
	// End to end: the app-wide Timeout middleware honors the router's
	// stamp. Default 30ms would kill the 120ms handler; the route
	// budget keeps it alive.
	r := New()
	r.Use(middleware.Timeout(30 * time.Millisecond))
	r.SetRouteTimeout("GET", "/slow", time.Second)
	r.Handle("GET", "/slow", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		time.Sleep(120 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	r.Handle("GET", "/fast-default", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case <-req.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/slow", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("route budget must outlast default: got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/fast-default", nil))
	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("un-overridden route keeps the default budget: got %d", rec.Code)
	}
}
