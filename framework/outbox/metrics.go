package outbox

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
)

// DeliveryCounts returns the number of pending and of dead-lettered deliveries
// per consumer. On success both maps are non-nil. A query or scan error
// returns nil maps; an iteration error returns the counts read so far.
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

// MetricsCollector returns a collector for
// middleware.Metrics.RegisterCollector that writes the outbox_pending and
// outbox_dead_letter_deliveries gauges per consumer, in consumer order. A
// failed count writes nothing.
func (o *Outbox) MetricsCollector() func(io.Writer) {
	return func(w io.Writer) {
		pending, dead, err := o.DeliveryCounts(context.Background())
		if err != nil {
			return
		}
		fmt.Fprint(w, "# HELP outbox_pending Outbox deliveries still awaiting dispatch.\n")
		fmt.Fprint(w, "# TYPE outbox_pending gauge\n")
		for _, c := range slices.Sorted(maps.Keys(pending)) {
			fmt.Fprintf(w, "outbox_pending{consumer=%q} %d\n", c, pending[c])
		}
		fmt.Fprint(w, "# HELP outbox_dead_letter_deliveries Dead-lettered outbox deliveries held now.\n")
		fmt.Fprint(w, "# TYPE outbox_dead_letter_deliveries gauge\n")
		for _, c := range slices.Sorted(maps.Keys(dead)) {
			fmt.Fprintf(w, "outbox_dead_letter_deliveries{consumer=%q} %d\n", c, dead[c])
		}
	}
}
