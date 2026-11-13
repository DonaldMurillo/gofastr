package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// R11 U4-02: the serve hook is coverage tooling installed by tests and
// GOFASTR_SEMANTIC_COVERAGE=1 servers, and it runs before the middleware
// chain, so the app's recovery middleware never sees its panic. A fault
// in it must not take the request with it: the route still answers.
func TestPanickingServeHookStillServes(t *testing.T) {
	r := New()
	var ran string
	r.SetServeHook(func(method, pattern string) {
		ran = method + " " + pattern
		panic("hook boom")
	})
	r.Get("/ok", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want the handler's 418", rec.Code)
	}
	// The hook must have run, or the test never reached the recovery.
	if ran != "GET /ok" {
		t.Fatalf("serve hook saw %q, want GET /ok", ran)
	}
}
