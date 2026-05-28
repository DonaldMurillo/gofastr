package log

import (
	"fmt"
	"io"
	"net/http"
)

// MetricsHandler returns an http.Handler that emits the plugin's
// counters in Prometheus text exposition format (version 0.0.4).
// Mount under whatever path your app exposes for scraping, e.g.:
//
//	app.Router().Get("/metrics", logPlugin.MetricsHandler())
//
// All counters are monotonically non-decreasing process-lifetime
// totals, which matches Prometheus's counter semantics. Operators
// pair this with the framework's other surfaces (auth metrics,
// queue stats, etc.) on the same endpoint by writing a small
// composite handler that calls each plugin's MetricsHandler in turn
// — there is no global registry to coordinate with.
func (p *Plugin) MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		// Metrics may leak operator-visible counts (drop totals, retry
		// budgets); ensure no shared cache stores the body.
		w.Header().Set("Cache-Control", "no-store")
		writeMetrics(w, p.Metrics())
	})
}

// writeMetrics is split out so tests can drive it directly without
// going through HTTP.
func writeMetrics(w io.Writer, m Metrics) {
	// Counter naming follows Prometheus convention: <subsystem>_<unit>_total
	// with HELP + TYPE comments preceding each metric.
	fmt.Fprintf(w, "# HELP gofastr_log_post_stop_drops_total Log entries dropped because sinks were closed (post-Stop writes).\n")
	fmt.Fprintf(w, "# TYPE gofastr_log_post_stop_drops_total counter\n")
	fmt.Fprintf(w, "gofastr_log_post_stop_drops_total %d\n", m.PostStopDrops)

	fmt.Fprintf(w, "# HELP gofastr_log_sink_write_failures_total Log entries dropped because a sink Write returned an error (disk full, network, etc.).\n")
	fmt.Fprintf(w, "# TYPE gofastr_log_sink_write_failures_total counter\n")
	fmt.Fprintf(w, "gofastr_log_sink_write_failures_total %d\n", m.SinkWriteFailures)

	fmt.Fprintf(w, "# HELP gofastr_log_webhook_dropped_total Log entries dropped from webhook sinks' bounded queues (drop-oldest under backpressure).\n")
	fmt.Fprintf(w, "# TYPE gofastr_log_webhook_dropped_total counter\n")
	fmt.Fprintf(w, "gofastr_log_webhook_dropped_total %d\n", m.WebhookDropped)

	fmt.Fprintf(w, "# HELP gofastr_log_webhook_gave_up_total Webhook batches given up after exhausting MaxRetries.\n")
	fmt.Fprintf(w, "# TYPE gofastr_log_webhook_gave_up_total counter\n")
	fmt.Fprintf(w, "gofastr_log_webhook_gave_up_total %d\n", m.WebhookGaveUp)
}
