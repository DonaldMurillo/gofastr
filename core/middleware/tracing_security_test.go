package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTracingPreservesHijacker asserts the tracing writer keeps the
// underlying Hijacker (and Flusher) so a WS-upgrade handler behind the
// Tracing() middleware keeps its upgrade path.
func TestTracingPreservesHijacker(t *testing.T) {
	var sawHijacker, sawFlusher bool
	h := Tracing()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := w.(http.Hijacker); ok {
			sawHijacker = true
		}
		if _, ok := w.(http.Flusher); ok {
			sawFlusher = true
		}
	}))

	rec := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if !sawHijacker {
		t.Fatal("Hijacker not preserved through tracing wrapper")
	}
	if !sawFlusher {
		t.Fatal("Flusher not preserved through tracing wrapper")
	}
}
