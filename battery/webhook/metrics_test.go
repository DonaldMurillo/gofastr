package webhook

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestWebhookMetrics_CountersAndCollector drives one failing delivery through
// a Manager (subscriber pointed at a 500 httptest server) and asserts that the
// delivery/failure counters advance and that MetricsCollector emits valid
// Prometheus text exposition for both counters.
func TestWebhookMetrics_CountersAndCollector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	store := NewMemoryStore()
	mgr := New(store, Options{
		MaxAttempts:          5,
		Backoff:              []time.Duration{0},
		AllowPrivateNetworks: true, // httptest runs on loopback
	})

	ctx := context.Background()
	if _, err := mgr.Subscribe(ctx, Subscriber{
		URL:    srv.URL,
		Secret: "x",
		Events: []string{"*"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Publish(ctx, "x", []byte("{}")); err != nil {
		t.Fatal(err)
	}

	// One due delivery → one attempt → 500 → one delivery, one failure.
	mgr.tick(ctx)

	if got := mgr.DeliveriesTotal(); got < 1 {
		t.Fatalf("DeliveriesTotal = %d, want >= 1 (delivery attempt not counted)", got)
	}
	if got := mgr.FailuresTotal(); got < 1 {
		t.Fatalf("FailuresTotal = %d, want >= 1 (failed attempt not counted)", got)
	}
	if mgr.FailuresTotal() > mgr.DeliveriesTotal() {
		t.Fatalf("FailuresTotal %d > DeliveriesTotal %d (failures must be a subset of attempts)",
			mgr.FailuresTotal(), mgr.DeliveriesTotal())
	}

	// The collector writes Prometheus text; a caller registers it on the
	// app's *middleware.Metrics via RegisterCollector.
	var buf bytes.Buffer
	mgr.MetricsCollector()(&buf)
	out := buf.String()
	for _, want := range []string{
		"# HELP webhook_deliveries_total Total webhook delivery attempts.",
		"# TYPE webhook_deliveries_total counter",
		"webhook_deliveries_total ",
		"# HELP webhook_failures_total Webhook delivery attempts that did not succeed (transport error, non-2xx, dead-lettered).",
		"# TYPE webhook_failures_total counter",
		"webhook_failures_total ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics output missing %q; got:\n%s", want, out)
		}
	}
}
