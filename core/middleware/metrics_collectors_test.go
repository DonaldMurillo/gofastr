package middleware

import (
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMetrics_CollectorAppearsInScrape pins the contract that a collector
// registered via RegisterCollector is rendered into the /metrics output
// after the HTTP metrics, in name order. This is the seam batteries use to
// add subsystem metrics (queue depth, outbox lag, …) to the single surface
// without a second handler.
func TestMetrics_CollectorAppearsInScrape(t *testing.T) {
	m := NewMetrics()
	m.RegisterCollector("queue", func(w io.Writer) {
		w.Write([]byte("# HELP queue_depth Jobs waiting to be processed.\n"))
		w.Write([]byte("# TYPE queue_depth gauge\n"))
		w.Write([]byte("queue_depth{lane=\"ingest\"} 7\n"))
	})

	rec := httptest.NewRecorder()
	MetricsHandler(m).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()

	// HTTP metrics still present.
	if !strings.Contains(body, "# TYPE http_requests_total counter") {
		t.Fatalf("http metrics missing:\n%s", body)
	}
	// Collector output appended after HTTP metrics.
	if !strings.Contains(body, "queue_depth{lane=\"ingest\"} 7") {
		t.Fatalf("collector output missing:\n%s", body)
	}
	if !strings.Contains(body, "# HELP queue_depth Jobs waiting to be processed.\n") {
		t.Fatalf("collector HELP line missing:\n%s", body)
	}
}

// TestMetrics_CollectorsOrderedByName ensures deterministic scrape output
// regardless of registration order — operators and diff tools rely on it.
func TestMetrics_CollectorsOrderedByName(t *testing.T) {
	m := NewMetrics()
	// Register out of order.
	m.RegisterCollector("zeta", func(w io.Writer) { w.Write([]byte("zeta_line\n")) })
	m.RegisterCollector("alpha", func(w io.Writer) { w.Write([]byte("alpha_line\n")) })

	rec := httptest.NewRecorder()
	MetricsHandler(m).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()

	if i, j := strings.Index(body, "alpha_line"), strings.Index(body, "zeta_line"); i < 0 || j < 0 || i > j {
		t.Fatalf("collectors not ordered alpha-before-zeta:\n%s", body)
	}
}

// TestMetrics_RegisterCollectorReplaces pins idempotency-by-name: registering
// the same name twice does not duplicate output, the latest fn wins.
func TestMetrics_RegisterCollectorReplaces(t *testing.T) {
	m := NewMetrics()
	m.RegisterCollector("x", func(w io.Writer) { w.Write([]byte("x_old\n")) })
	m.RegisterCollector("x", func(w io.Writer) { w.Write([]byte("x_new\n")) })

	rec := httptest.NewRecorder()
	MetricsHandler(m).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()

	if strings.Contains(body, "x_old") {
		t.Fatalf("old collector still present after replace:\n%s", body)
	}
	if !strings.Contains(body, "x_new") {
		t.Fatalf("new collector missing:\n%s", body)
	}
}

// A collector registered while a scrape is in flight must not race the
// handler's map access — the scrape iterates a snapshot taken under the
// lock, never the live map.
func TestMetrics_ConcurrentRegisterAndScrape(t *testing.T) {
	m := NewMetrics()
	h := MetricsHandler(m)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			m.RegisterCollector(fmt.Sprintf("churn%d", i%8), func(w io.Writer) { w.Write([]byte("churn 1\n")) })
		}
	}()
	for i := 0; i < 500; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	}
	close(stop)
	<-done
}
