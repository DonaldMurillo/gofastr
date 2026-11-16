package queue

import (
	"context"
	"fmt"
	"io"
)

// MetricsCollector returns a Prometheus text-exposition collector that
// samples a Browsable queue's Stats at scrape time, emitting two gauges
// labelled with the queue lane: queue_depth (pending jobs) and
// queue_dead_letter_jobs (dead-lettered jobs the queue holds now, which
// falls on a Replay or an erasure). Register the returned func on the app's
// *middleware.Metrics via RegisterCollector so the queue surfaces on the
// single /metrics endpoint. lane is the label value (the queue's name/lane).
//
// queue_depth is Stats' "pending" count, or the backend's own count where
// Stats covers dead-letter jobs only (MemoryQueue, RedisQueue). A RedisQueue
// whose client is not a [ListLengther] gets no queue_depth sample, as does
// a scrape whose depth read failed.
//
// The collector tolerates a Stats error by emitting nothing for that scrape
// (a transient DB error must never break /metrics).
func MetricsCollector(q Browsable, lane string) func(io.Writer) {
	return func(w io.Writer) {
		stats, err := q.Stats(context.Background())
		if err != nil {
			return
		}
		// Missing status keys default to 0. Every backend files a job that
		// spent its attempts under "failed". A backend whose Stats cannot
		// list pending jobs counts them through pendingDepth instead, and
		// one that cannot count them cheaply gets no queue_depth sample.
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

// depthCounter is a backend that counts its waiting jobs outside Stats.
type depthCounter interface {
	pendingDepth(ctx context.Context) (int, error)
}
