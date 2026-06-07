package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestWithMetrics_MountsEndpoint confirms WithMetrics wires the middleware and
// mounts a Prometheus /metrics endpoint that records traffic.
func TestWithMetrics_MountsEndpoint(t *testing.T) {
	app := NewApp(WithMetrics())
	app.Entity("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	app, cleanup := startApp(t, app)
	defer cleanup()

	// Generate a request so the histogram has something to report.
	app.router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/posts", nil))

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "http_request") {
		t.Errorf("/metrics body missing request metrics: %s", rec.Body.String())
	}
}

// TestNoMetricsByDefault confirms /metrics is absent without WithMetrics.
func TestNoMetricsByDefault(t *testing.T) {
	app := NewApp()
	app.Entity("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	app, cleanup := startApp(t, app)
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code == http.StatusOK {
		t.Errorf("/metrics should be absent without WithMetrics, got 200")
	}
}

// TestWithMetrics_PanicsWithoutDefaults pins the incompatibility guard.
func TestWithMetrics_PanicsWithoutDefaults(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("WithMetrics + WithoutDefaultMiddleware should panic")
		}
	}()
	NewApp(WithoutDefaultMiddleware(), WithMetrics())
}
