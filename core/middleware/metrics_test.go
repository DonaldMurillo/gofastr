package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMetrics_RecordsRequests(t *testing.T) {
	m := NewMetrics()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /posts", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	h := MetricsMiddleware(m)(mux)

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/posts/p1", nil))
		if w.Code != 200 {
			t.Fatalf("got %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/posts", nil))
	if w.Code != 201 {
		t.Fatalf("got %d", w.Code)
	}

	// Scrape /metrics
	rec := httptest.NewRecorder()
	MetricsHandler(m).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()

	if !strings.Contains(body, `http_requests_total{method="GET",route="GET /posts/{id}",status="200"} 3`) {
		t.Fatalf("missing GET counter line:\n%s", body)
	}
	if !strings.Contains(body, `http_requests_total{method="POST",route="POST /posts",status="201"} 1`) {
		t.Fatalf("missing POST counter line:\n%s", body)
	}
	if !strings.Contains(body, "http_request_duration_ms_bucket") {
		t.Fatalf("missing duration histogram")
	}
}

// ============================================================================
// Histogram bucket math: latency observations land in the right bucket
// ============================================================================

func TestMetrics_HistogramBucketBoundaries(t *testing.T) {
	m := NewMetrics()
	// Record three latencies into the same "route" — 2ms, 7ms, 600ms.
	// Buckets: 1, 5, 10, 50, 100, 250, 500, 1000.
	// 2ms → bucket index 1 (<=5).
	// 7ms → bucket index 2 (<=10).
	// 600ms → bucket index 7 (<=1000).
	m.record("GET", "/route", 200, 2*time.Millisecond)
	m.record("GET", "/route", 200, 7*time.Millisecond)
	m.record("GET", "/route", 200, 600*time.Millisecond)

	rec := httptest.NewRecorder()
	MetricsHandler(m).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()

	// Cumulative buckets (le="X") — Prometheus expects cumulative counts.
	// le=5  → 1 (just the 2ms)
	// le=10 → 2 (2ms + 7ms)
	// le=1000 → 3 (all three)
	expects := []string{
		`http_request_duration_ms_bucket{route="/route",le="5"} 1`,
		`http_request_duration_ms_bucket{route="/route",le="10"} 2`,
		`http_request_duration_ms_bucket{route="/route",le="1000"} 3`,
		`http_request_duration_ms_count{route="/route"} 3`,
	}
	for _, want := range expects {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

// ============================================================================
// Counters under concurrent traffic don't lose increments
// ============================================================================

func TestMetrics_ConcurrentSafe(t *testing.T) {
	m := NewMetrics()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hot", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := MetricsMiddleware(m)(mux)

	const N = 200
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/hot", nil))
		}()
	}
	wg.Wait()

	rec := httptest.NewRecorder()
	MetricsHandler(m).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	want := fmt.Sprintf(`http_requests_total{method="GET",route="GET /hot",status="200"} %d`, N)
	if !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("expected counter to total %d, got:\n%s", N, rec.Body.String())
	}
}

func TestMetrics_UnmatchedRoute(t *testing.T) {
	m := NewMetrics()
	h := MetricsMiddleware(m)(http.NotFoundHandler())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/no-such-thing", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d", w.Code)
	}
	rec := httptest.NewRecorder()
	MetricsHandler(m).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(rec.Body.String(), `route="unmatched"`) {
		t.Fatalf("expected unmatched route label, got:\n%s", rec.Body.String())
	}
}
