package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMetricsPreservesHijacker asserts the metrics writer keeps the
// underlying Hijacker (and Flusher) so a WS-upgrade handler behind the
// global metrics middleware keeps its upgrade path.
func TestMetricsPreservesHijacker(t *testing.T) {
	var sawHijacker, sawFlusher bool
	h := MetricsMiddleware(NewMetrics())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
		t.Fatal("Hijacker not preserved through metrics wrapper")
	}
	if !sawFlusher {
		t.Fatal("Flusher not preserved through metrics wrapper")
	}
}

// TestMetricsBoundsMethodCardinality asserts arbitrary RFC-7230 method
// tokens collapse to a single "other" label so an unauthenticated client
// can't grow the in-memory metrics store without bound.
func TestMetricsBoundsMethodCardinality(t *testing.T) {
	m := NewMetrics()
	h := MetricsMiddleware(m)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))

	// A known method survives as itself.
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	// 1000 distinct attacker-chosen method tokens must NOT each spawn a key.
	for _, tok := range []string{"FOOBAR", "X1", "X2", "ZZZZZ", "AAAA", "BBBB", "CCCC", "DDDD"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Method = tok
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	for i := 0; i < 1000; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.Method = "M" + strings.Repeat("z", i%7) + string(rune('A'+i%26)) + string(rune('0'+i%10))
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	m.mu.Lock()
	keys := len(m.counters)
	methods := map[string]struct{}{}
	for k := range m.counters {
		methods[k.Method] = struct{}{}
	}
	m.mu.Unlock()

	// Should be GET + "other" only — two distinct method labels.
	if _, ok := methods["other"]; !ok {
		t.Fatalf("expected unknown methods collapsed to \"other\"; got methods %v", methods)
	}
	if len(methods) > 2 {
		t.Fatalf("method cardinality unbounded: %d distinct method labels %v", len(methods), methods)
	}
	if keys > 4 {
		t.Fatalf("counter cardinality unbounded: %d keys after 1008 distinct methods", keys)
	}
}
