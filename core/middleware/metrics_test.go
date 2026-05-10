package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
