package framework

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/sourcewalk"
)

// emittedMetrics is every Prometheus metric the repository declares. from
// is where each number comes from: for a counter, an add-only source.
var emittedMetrics = []struct {
	name, typ string
	declared  string // the file holding the # TYPE line
	from      []metricSource
}{
	{"http_requests_total", "counter", "core/middleware/metrics.go", []metricSource{
		{"core/middleware/metrics.go", "m.counters[k].Load()"}, {"core/middleware/metrics.go", "c.Add(1)"}}},
	{"http_request_duration_ms", "histogram", "core/middleware/metrics.go", []metricSource{
		{"core/middleware/metrics.go", "h.buckets[i]++"}}},
	{"db_pool_open_connections", "gauge", "framework/metrics.go", []metricSource{{"framework/metrics.go", "s.OpenConnections"}}},
	{"db_pool_in_use_connections", "gauge", "framework/metrics.go", []metricSource{{"framework/metrics.go", "s.InUse"}}},
	{"db_pool_idle_connections", "gauge", "framework/metrics.go", []metricSource{{"framework/metrics.go", "s.Idle"}}},
	// sql.DBStats documents WaitCount and WaitDuration as running totals.
	{"db_pool_wait_count_total", "counter", "framework/metrics.go", []metricSource{{"framework/metrics.go", "s.WaitCount"}}},
	{"db_pool_wait_duration_seconds_total", "counter", "framework/metrics.go", []metricSource{{"framework/metrics.go", "seconds(s.WaitDuration)"}}},
	{"outbox_pending", "gauge", "framework/outbox/metrics.go", []metricSource{{"framework/outbox/metrics.go", "COUNT(*)"}}},
	{"outbox_dead_letter_deliveries", "gauge", "framework/outbox/metrics.go", []metricSource{{"framework/outbox/metrics.go", "COUNT(*)"}}},
	{"queue_depth", "gauge", "battery/queue/metrics.go", []metricSource{{"battery/queue/metrics.go", "d.pendingDepth("}}},
	{"queue_dead_letter_jobs", "gauge", "battery/queue/metrics.go", []metricSource{{"battery/queue/metrics.go", `stats["failed"]`}}},
	{"webhook_deliveries_total", "counter", "battery/webhook/webhook.go", []metricSource{
		{"battery/webhook/webhook.go", "m.deliveriesTotal.Load()"}, {"battery/webhook/webhook.go", "deliveriesTotal atomic.Uint64"}}},
	{"webhook_failures_total", "counter", "battery/webhook/webhook.go", []metricSource{
		{"battery/webhook/webhook.go", "m.failuresTotal.Load()"}, {"battery/webhook/webhook.go", "failuresTotal   atomic.Uint64"}}},
	{"slow_queries_total", "counter", "framework/slowquery/metrics.go", []metricSource{
		{"framework/slowquery/metrics.go", "s.Hits()"}, {"framework/slowquery/slowquery.go", "return s.hits.Load()"}}},
	{"gofastr_log_post_stop_drops_total", "counter", "battery/log/metrics_handler.go", []metricSource{
		{"battery/log/log.go", "PostStopDrops:     handler.metrics.closedDrops.Load()"}}},
	{"gofastr_log_sink_write_failures_total", "counter", "battery/log/metrics_handler.go", []metricSource{
		{"battery/log/log.go", "SinkWriteFailures: handler.metrics.failedSinks.Load()"}}},
	{"gofastr_log_webhook_dropped_total", "counter", "battery/log/metrics_handler.go", []metricSource{
		{"battery/log/log.go", "m.WebhookDropped += ws.Dropped()"}, {"battery/log/webhook.go", "return s.dropped.Load()"}}},
	{"gofastr_log_webhook_gave_up_total", "counter", "battery/log/metrics_handler.go", []metricSource{
		{"battery/log/log.go", "m.WebhookGaveUp += ws.GaveUp()"}, {"battery/log/webhook.go", "return s.gaveUp.Load()"}}},
}

type metricSource struct{ file, expr string }

// stateReads are the reads of current state a counter must not be fed by.
var stateReads = []string{"COUNT(", "len(", "Stats(", "pendingDepth("}

var metricTypeLine = regexp.MustCompile(`# TYPE (\w+) (\w+)`)

// queue_dead_letter_total and outbox_dead_letter_total were declared
// counters and read a count of current rows, which falls. Every metric the
// repo declares is listed here with its source, and a counter's is add-only.
func TestEveryCounterIsFedByAnAddOnlySource(t *testing.T) {
	root := ".."
	declared := map[string]string{} // name → "type file"
	err := sourcewalk.Walk(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, m := range metricTypeLine.FindAllStringSubmatch(string(src), -1) {
			declared[m[1]] = m[2] + " " + filepath.ToSlash(rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	read := func(file string) string {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		return string(src)
	}
	for _, m := range emittedMetrics {
		if got, want := declared[m.name], m.typ+" "+m.declared; got != want {
			t.Errorf("%s: the source declares %q, the table lists %q", m.name, got, want)
		}
		delete(declared, m.name)
		if counter, total := m.typ == "counter", strings.HasSuffix(m.name, "_total"); counter != total {
			t.Errorf("%s is a %s: only a counter is named _total, and every counter is", m.name, m.typ)
		}
		for _, s := range m.from {
			if !strings.Contains(read(s.file), s.expr) {
				t.Errorf("%s: %s no longer holds %q; say where the number comes from now", m.name, s.file, s.expr)
			}
			for _, state := range stateReads {
				if m.typ == "counter" && strings.Contains(s.expr, state) {
					t.Errorf("%s is a counter fed by %q, a read of current state, which can fall", m.name, s.expr)
				}
			}
		}
		if len(m.from) == 0 {
			t.Errorf("%s names no source", m.name)
		}
	}
	for name, where := range declared {
		t.Errorf("%s (%s) is not in emittedMetrics: list it with the source of its number", name, where)
	}
}
