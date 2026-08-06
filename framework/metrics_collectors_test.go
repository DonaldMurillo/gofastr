package framework

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// TestAppMetrics_NilWithoutWithMetrics pins that the accessor returns nil
// when metrics were not enabled, so batteries can nil-check before attaching
// collectors instead of creating a metrics store out of thin air.
func TestAppMetrics_NilWithoutWithMetrics(t *testing.T) {
	app := NewApp()
	if app.Metrics() != nil {
		t.Fatal("Metrics() should be nil without WithMetrics")
	}
}

// TestAppMetrics_DBPoolCollector pins the zero-config DB-pool surface: an
// app built with WithDB + WithMetrics exposes sql.DBStats gauges/counters
// on the single /metrics endpoint once any caller takes the metrics handle
// (a battery's Init in real apps; app.Metrics() here stands in for that).
func TestAppMetrics_DBPoolCollector(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithDB(db), WithMetrics())
	m := app.Metrics()
	if m == nil {
		t.Fatal("Metrics() returned nil with WithMetrics enabled")
	}

	// Touch the pool so the stats are non-trivial.
	if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rec := httptest.NewRecorder()
	middleware.MetricsHandler(m).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()

	for _, want := range []string{
		"# TYPE db_pool_open_connections gauge",
		"db_pool_open_connections ",
		"# TYPE db_pool_in_use_connections gauge",
		"db_pool_in_use_connections ",
		"# TYPE db_pool_idle_connections gauge",
		"db_pool_idle_connections ",
		"# TYPE db_pool_wait_count_total counter",
		"db_pool_wait_count_total ",
		"# TYPE db_pool_wait_duration_seconds_total counter",
		"db_pool_wait_duration_seconds_total ",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in /metrics:\n%s", want, body)
		}
	}
}

// TestAppMetrics_OutboxCollectorAutoWired pins the framework-owned outbox
// surface: an app built with WithOutbox + WithMetrics exposes the
// per-consumer outbox gauges on the single /metrics endpoint the moment any
// caller takes the metrics handle (a battery's Init in a real app;
// app.Metrics() here stands in for that). No second handler to mount, no
// cross-package wiring — RegisterCollector("outbox", …) fires from Metrics()
// itself. The DB-pool collector must still land alongside it, so the two
// framework-owned collectors coexist on one scrape.
func TestAppMetrics_OutboxCollectorAutoWired(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithDB(db), WithMetrics(),
		WithOutbox(),
		WithOutboxConsumer("witness", event.EntityCreated,
			func(context.Context, event.Event) error { return nil }),
	)
	m := app.Metrics()
	if m == nil {
		t.Fatal("Metrics() returned nil with WithMetrics + WithOutbox enabled")
	}

	rec := httptest.NewRecorder()
	middleware.MetricsHandler(m).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()

	for _, want := range []string{
		"# TYPE outbox_pending gauge",           // framework-owned outbox collector
		"# TYPE db_pool_open_connections gauge", // coexisting DB-pool collector
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in /metrics:\n%s", want, body)
		}
	}
}
