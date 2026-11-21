package queue

import (
	"context"
	"fmt"
	"io"
)

// MetricsCollector returns a Prometheus collector sampling q's Stats at scrape
// time as the queue_depth and queue_dead_letter_jobs gauges.
func MetricsCollector(q Browsable, lane string) func(io.Writer) {
	return func(w io.Writer) {
		stats, err := q.Stats(context.Background())
		if err != nil {
			return
		}
		pending, depthErr := stats["pending"], error(nil)
		if d, ok := q.(depthCounter); ok {
			pending, depthErr = d.pendingDepth(context.Background())
		}
		dead := stats["failed"]

		if depthErr == nil {
			fmt.Fprintf(w, "# HELP queue_depth Jobs waiting to be processed.\n")
			fmt.Fprintf(w, "# TYPE queue_depth gauge\n")
			fmt.Fprintf(w, "queue_depth{lane=%q} %d\n", lane, pending)
		}

		fmt.Fprintf(w, "# HELP queue_dead_letter_jobs Dead-lettered jobs the queue holds now.\n")
		fmt.Fprintf(w, "# TYPE queue_dead_letter_jobs gauge\n")
		fmt.Fprintf(w, "queue_dead_letter_jobs{lane=%q} %d\n", lane, dead)
	}
}

type depthCounter interface {
	pendingDepth(ctx context.Context) (int, error)
}
