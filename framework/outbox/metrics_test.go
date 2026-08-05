package outbox

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// TestOutbox_MetricsCollector pins the per-consumer pending/dead-letter
// surface exposed for the unified /metrics endpoint: after staging a row and
// expanding deliveries, the collector renders outbox_pending for the consumer
// with the right count, plus a dead-letter line for a dead delivery.
func TestOutbox_MetricsCollector(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	o, err := New(db)
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	ctx := context.Background()

	o.Consume("orders", "order.placed", func(context.Context, event.Event) error { return nil })

	// Stage one pending parent (raw *sql.DB satisfies db.Executor → autocommit).
	if _, err := o.Append(ctx, db, "order.placed", map[string]any{"id": 1}); err != nil {
		t.Fatalf("append: %v", err)
	}
	// Expand creates the pending per-consumer delivery row.
	if _, err := o.expandDeliveries(ctx); err != nil {
		t.Fatalf("expand: %v", err)
	}

	// Synthetic dead-letter for a second consumer to exercise the dead path.
	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (row_id, consumer, status, attempts, created_at) VALUES ($1, $2, 'dead', 99, $3)`,
		o.qd()), "row-bill", "billing", time.Now().UTC()); err != nil {
		t.Fatalf("seed dead: %v", err)
	}

	pending, dead, err := o.DeliveryCounts(ctx)
	if err != nil {
		t.Fatalf("DeliveryCounts: %v", err)
	}
	if pending["orders"] != 1 {
		t.Fatalf("pending[orders] = %d, want 1 (pending=%v dead=%v)", pending["orders"], pending, dead)
	}
	if dead["billing"] != 1 {
		t.Fatalf("dead[billing] = %d, want 1", dead["billing"])
	}

	var buf bytes.Buffer
	o.MetricsCollector()(&buf)
	out := buf.String()

	if !strings.Contains(out, "# TYPE outbox_pending gauge") {
		t.Errorf("missing outbox_pending TYPE:\n%s", out)
	}
	if !strings.Contains(out, `outbox_pending{consumer="orders"} 1`) {
		t.Errorf("missing outbox_pending orders line:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE outbox_dead_letter_total counter") {
		t.Errorf("missing dead_letter TYPE:\n%s", out)
	}
	if !strings.Contains(out, `outbox_dead_letter_total{consumer="billing"} 1`) {
		t.Errorf("missing dead_letter billing line:\n%s", out)
	}
}

// TestOutbox_DeliveryCountsToleratesEmpty ensures a fresh outbox with no
// deliveries returns empty maps (not nil-pointer / not an error) so the
// collector renders the HELP/TYPE headers with zero samples cleanly.
func TestOutbox_DeliveryCountsToleratesEmpty(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	o, err := New(db)
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	pending, dead, err := o.DeliveryCounts(context.Background())
	if err != nil {
		t.Fatalf("DeliveryCounts: %v", err)
	}
	if len(pending) != 0 || len(dead) != 0 {
		t.Fatalf("expected empty counts, got pending=%v dead=%v", pending, dead)
	}
}
