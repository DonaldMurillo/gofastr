package widget

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// sessionCheck is a package-level set-once that gateSession reads on every
// gated request. A plain var read+written from different goroutines is a data
// race; this test drives concurrent SetSessionCheck + SessionCheck/gateSession
// reads to surface it under -race, then the atomic backing makes it safe.

func TestSessionCheckConcurrentReadWrite(t *testing.T) {
	t.Cleanup(func() { SetSessionCheck(nil) }) // restore global for other tests

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	authed := func(*http.Request) bool { return true }

	var wg sync.WaitGroup
	for range 300 {
		wg.Add(3)
		go func() { defer wg.Done(); SetSessionCheck(authed) }()
		go func() { defer wg.Done(); _ = SessionCheck() }()
		go func() {
			defer wg.Done()
			h := gateSession(true, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			h.ServeHTTP(httptest.NewRecorder(), req)
		}()
	}
	wg.Wait()
}
