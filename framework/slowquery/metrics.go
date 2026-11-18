package slowquery

import (
	"fmt"
	"io"
)

// MetricsCollector writes slow_queries_total for s, zero when s is nil, for middleware.Metrics.RegisterCollector.
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
