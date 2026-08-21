package queue

import (
	"context"
	"fmt"
	"io"
)

// MetricsCollector returns a Prometheus text-exposition collector that
// samples a Browsable queue's Stats at scrape time, emitting queue_depth
// (pending jobs, a gauge) and queue_dead_letter_total (dead jobs, a counter)
// labelled with the queue lane. Register the returned func on the app's
// *middleware.Metrics via RegisterCollector so the queue surfaces on the
// single /metrics endpoint. lane is the label value (the queue's name/lane).
//
// The collector tolerates a Stats error by emitting nothing for that scrape
// (a transient DB error must never break /metrics).
func MetricsCollector(q Browsable, lane string) func(io.Writer) {
	return func(w io.Writer) {
		stats, err := q.Stats(context.Background())
		if err != nil {
			return
		}
		// Missing status keys default to 0, a fresh queue has nothing
		// pending or dead-lettered yet.
		pending := stats["pending"]
		dead := stats["dead"]

		fmt.Fprintf(w, "# HELP queue_depth Jobs waiting to be processed.\n")
		fmt.Fprintf(w, "# TYPE queue_depth gauge\n")
		fmt.Fprintf(w, "queue_depth{lane=%q} %d\n", lane, pending)

		fmt.Fprintf(w, "# HELP queue_dead_letter_total Jobs that exhausted their retry budget and were dead-lettered.\n")
		fmt.Fprintf(w, "# TYPE queue_dead_letter_total counter\n")
		fmt.Fprintf(w, "queue_dead_letter_total{lane=%q} %d\n", lane, dead)
	}
}
