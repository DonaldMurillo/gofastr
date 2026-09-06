package widget

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// sessionCheck and authenticatedCheck are package-level set-once values that
// gateSession reads on every gated request. A plain var read+written from
// different goroutines is a data race; this test drives concurrent
// Set*Check + getter/gateSession reads to surface it under -race, then the
// atomic backing makes it safe. The authenticated predicate got the same
// atomic treatment when it was added (RequireAuthenticated reads it on the
// same per-request path).

func TestSessionCheckConcurrentReadWrite(t *testing.T) {
	t.Cleanup(func() { SetSessionCheck(nil) }) // restore global for other tests
	t.Cleanup(func() { SetAuthenticatedCheck(nil) })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	authed := func(*http.Request) bool { return true }

	var wg sync.WaitGroup
	for range 300 {
		wg.Add(6)
		go func() { defer wg.Done(); SetSessionCheck(authed) }()
		go func() { defer wg.Done(); _ = SessionCheck() }()
		go func() {
			defer wg.Done()
			h := gateSession(gateAnySession, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			h.ServeHTTP(httptest.NewRecorder(), req)
		}()
		go func() { defer wg.Done(); SetAuthenticatedCheck(authed) }()
		go func() { defer wg.Done(); _ = AuthenticatedCheck() }()
		go func() {
			defer wg.Done()
			h := gateSession(gateAuthenticated, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			h.ServeHTTP(httptest.NewRecorder(), req)
		}()
	}
	wg.Wait()
}
