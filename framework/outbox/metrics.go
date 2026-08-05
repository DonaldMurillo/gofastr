package outbox

import (
	"context"
	"fmt"
	"io"
	"sort"
)

// DeliveryCounts returns per-consumer pending and dead-letter delivery
// counts for the metrics surface.
//
//   - pending: delivery rows still awaiting dispatch (status = "pending").
//   - dead:    delivery rows that exhausted their retry budget and were
//     dead-lettered (status = "dead").
//
// One GROUP BY query (dialect-agnostic — works on both Postgres and SQLite).
// The maps are non-nil and empty when there is nothing to report, so callers
// can iterate them unconditionally.
func (o *Outbox) DeliveryCounts(ctx context.Context) (pending, dead map[string]int, err error) {
	pending = map[string]int{}
	dead = map[string]int{}
	q := fmt.Sprintf(
		`SELECT consumer, status, COUNT(*) FROM %s WHERE status IN ('pending','dead') GROUP BY consumer, status`,
		o.qd())
	rows, err := o.db.QueryContext(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var consumer, status string
		var n int
		if err := rows.Scan(&consumer, &status, &n); err != nil {
			return nil, nil, err
		}
		switch status {
		case "pending":
			pending[consumer] = n
		case "dead":
			dead[consumer] = n
		}
	}
	return pending, dead, rows.Err()
}

// MetricsCollector returns a Prometheus text-exposition collector that
// samples the outbox's per-consumer delivery counts at scrape time. Register
// it on the app's *middleware.Metrics via RegisterCollector so outbox lag and
// dead-letters land on the single /metrics surface.
//
// A transient query error emits nothing for this scrape — the collector must
// never break /metrics. Consumers are rendered in sorted order for
// deterministic scrape output.
func (o *Outbox) MetricsCollector() func(io.Writer) {
	return func(w io.Writer) {
		pending, dead, err := o.DeliveryCounts(context.Background())
		if err != nil {
			return
		}
		fmt.Fprint(w, "# HELP outbox_pending Outbox deliveries still awaiting dispatch.\n")
		fmt.Fprint(w, "# TYPE outbox_pending gauge\n")
		for _, c := range sortedConsumerKeys(pending) {
			fmt.Fprintf(w, "outbox_pending{consumer=%q} %d\n", c, pending[c])
		}
		fmt.Fprint(w, "# HELP outbox_dead_letter_total Outbox deliveries dead-lettered after exhausting the retry budget.\n")
		fmt.Fprint(w, "# TYPE outbox_dead_letter_total counter\n")
		for _, c := range sortedConsumerKeys(dead) {
			fmt.Fprintf(w, "outbox_dead_letter_total{consumer=%q} %d\n", c, dead[c])
		}
	}
}

// sortedConsumerKeys returns the keys of m sorted, for deterministic scrape
// output regardless of map iteration order.
func sortedConsumerKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
