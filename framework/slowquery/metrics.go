package slowquery

import (
	"fmt"
	"io"
)

// MetricsCollector returns a Prometheus text-exposition collector emitting
// slow_queries_total (the running count of queries that crossed the logger's
// threshold). Register it on the app's *middleware.Metrics via RegisterCollector
// so the count surfaces on the single /metrics endpoint.
//
// A nil *SlowQueryLogger reports zero, so wiring the collector before the
// logger exists (or when slow-query logging is disabled) never panics /metrics.
func MetricsCollector(s *SlowQueryLogger) func(io.Writer) {
	return func(w io.Writer) {
		var n uint64
		if s != nil {
			n = s.Hits()
		}
		fmt.Fprintf(w, "# HELP slow_queries_total DB queries that exceeded the slow-query threshold.\n")
		fmt.Fprintf(w, "# TYPE slow_queries_total counter\n")
		fmt.Fprintf(w, "slow_queries_total %d\n", n)
	}
}
