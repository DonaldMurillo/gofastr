package outbox

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
	_ "github.com/mattn/go-sqlite3"
)

// waitForParent polls List until the parent row with id reaches want, or
// fails after a short deadline. The relay is asynchronous, so tests must
// poll rather than assert immediately.
func waitForParent(t *testing.T, o *Outbox, id, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, r := range mustList(t, o, want, 50) {
			if r.ID == id {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("parent %s never reached status %q within deadline", id, want)
}

// waitForDelivery polls ListDeliveries until the (rowID, consumer)
// delivery reaches want, or fails after a short deadline.
func waitForDelivery(t *testing.T, o *Outbox, rowID, consumer, want string) Delivery {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, d := range mustDeliveries(t, o, rowID) {
			if d.Consumer == consumer && d.Status == want {
				return d
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("delivery %s/%s never reached status %q within deadline", rowID, consumer, want)
	return Delivery{}
}

// ---------------------------------------------------------------------------
// Relay delivers to a declared consumer and marks parent + delivery
// dispatched. Event.ID/Type/Timestamp round-trip; Data is map[string]any
// with numbers as float64.
// ---------------------------------------------------------------------------

func TestRelay_DeliversAndDispatches(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	got := make(chan event.Event, 1)
	o.Consume("logger", "order.placed", func(_ context.Context, e event.Event) error {
		got <- e
		return nil
	})

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "order.placed", map[string]any{"total": 99, "sku": "A1"})
	created := time.Now()
	tx.Commit()

	stop := o.StartRelay(ctx)
	defer stop()

	var e event.Event
	select {
	case e = <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for relay delivery")
	}

	if e.ID != id {
		t.Errorf("Event.ID = %q, want %q", e.ID, id)
	}
	if e.Type != "order.placed" {
		t.Errorf("Event.Type = %q", e.Type)
	}
	data, ok := e.Data.(map[string]any)
	if !ok {
		t.Fatalf("Event.Data type = %T, want map[string]any", e.Data)
	}
	if data["sku"] != "A1" {
		t.Errorf("data[sku] = %v, want A1", data["sku"])
	}
	// JSON round-trips numbers as float64.
	f, ok := data["total"].(float64)
	if !ok || f != 99 {
		t.Errorf("data[total] = %v (%T), want float64 99", data["total"], data["total"])
	}
	// Timestamp comes from the row's created_at, not delivery time.
	if e.Timestamp.Before(created.Add(-time.Second)) || e.Timestamp.After(created.Add(time.Second)) {
		t.Errorf("Event.Timestamp = %v, want ~%v", e.Timestamp, created)
	}

	waitForParent(t, o, id, "dispatched")
	d := findDelivery(t, mustDeliveries(t, o, id), "logger")
	if d.Status != "dispatched" {
		t.Errorf("delivery status = %q, want dispatched", d.Status)
	}
	if d.DispatchedAt == nil {
		t.Error("delivery DispatchedAt nil, want set")
	}
}

// ---------------------------------------------------------------------------
// Failing consumer: the delivery's attempts increment with backoff,
// eventually dead. The parent still completes (dead != pending).
// ---------------------------------------------------------------------------

func TestRelay_FailingConsumer_GoesDead(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(3), WithPollInterval(5*time.Millisecond))
	o.backoffBase = time.Millisecond
	o.backoffMax = 5 * time.Millisecond
	ctx := context.Background()

	var calls int32
	o.Consume("flaky", "flaky", func(_ context.Context, _ event.Event) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("handler unavailable")
	})

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "flaky", nil)
	tx.Commit()

	stop := o.StartRelay(ctx)
	defer stop()

	d := waitForDelivery(t, o, id, "flaky", "dead")
	if d.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", d.Attempts)
	}
	if d.LastError == "" {
		t.Error("LastError empty, want the handler error")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("handler invoked %d times, want 3", got)
	}
	// A dead delivery leaves no pending → parent completes.
	waitForParent(t, o, id, "dispatched")
}

// A consumer that PANICS must be treated as a delivery failure (retry →
// dead), never silently marked dispatched. The relay recovers the panic
// into a delivery error so a lost event is retried and dead-lettered.
func TestRelay_PanickingConsumer_GoesDead(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(3), WithPollInterval(5*time.Millisecond))
	o.backoffBase = time.Millisecond
	o.backoffMax = 5 * time.Millisecond
	ctx := context.Background()

	var calls int32
	o.Consume("boom", "boom", func(_ context.Context, _ event.Event) error {
		atomic.AddInt32(&calls, 1)
		panic("consumer exploded")
	})

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "boom", nil)
	tx.Commit()

	stop := o.StartRelay(ctx)
	defer stop()

	d := waitForDelivery(t, o, id, "boom", "dead")
	if d.Attempts != 3 {
		t.Errorf("attempts = %d, want 3 (panic must be retried like an error)", d.Attempts)
	}
	if d.LastError == "" {
		t.Error("LastError empty, want the recovered panic")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("handler invoked %d times, want 3", got)
	}
	// The delivery must NOT have been marked dispatched at any point.
	if d := findDelivery(t, mustDeliveries(t, o, id), "boom"); d.Status != "dead" {
		t.Errorf("panicking-consumer delivery = %q, want dead", d.Status)
	}
}

// ---------------------------------------------------------------------------
// Nudge wakes the relay faster than the poll interval.
// ---------------------------------------------------------------------------

func TestRelay_NudgeFasterThanPoll(t *testing.T) {
	db, o := openOutbox(t, WithPollInterval(time.Hour)) // poll would take an hour
	ctx := context.Background()

	got := make(chan event.Event, 1)
	o.Consume("n", "n", func(_ context.Context, e event.Event) error {
		got <- e
		return nil
	})

	stop := o.StartRelay(ctx)
	defer stop()

	// Let the initial pump drain (it finds nothing), so only Nudge can
	// wake the relay.
	time.Sleep(50 * time.Millisecond)

	tx, _ := db.BeginTx(ctx, nil)
	o.Append(ctx, tx, "n", map[string]any{"i": 1})
	tx.Commit()
	o.Nudge()

	select {
	case <-got:
		// Delivered well before the 1h poll interval — Nudge worked.
	case <-time.After(3 * time.Second):
		t.Fatal("Nudge did not wake the relay within 3s (poll interval is 1h)")
	}
}

// ---------------------------------------------------------------------------
// Claim exclusivity at the delivery grain: two sequential claims don't
// double-claim a delivery. (SQLite serialises writers, so we assert
// sequential exclusivity rather than true parallelism.)
// ---------------------------------------------------------------------------

func TestClaim_DeliveryExclusivity(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	o.Consume("c", "t", func(context.Context, event.Event) error { return nil })

	tx, _ := db.BeginTx(ctx, nil)
	o.Append(ctx, tx, "t", nil)
	tx.Commit()

	// Expand creates the pending delivery; then claim it.
	if _, err := o.expandDeliveries(ctx); err != nil {
		t.Fatalf("expand: %v", err)
	}
	batch1, err := o.claimDeliveries(ctx)
	if err != nil {
		t.Fatalf("claim1: %v", err)
	}
	if len(batch1) != 1 {
		t.Fatalf("claim1 got %d deliveries, want 1", len(batch1))
	}

	// The delivery is now leased — a second claim must not see it.
	batch2, err := o.claimDeliveries(ctx)
	if err != nil {
		t.Fatalf("claim2: %v", err)
	}
	if len(batch2) != 0 {
		t.Fatalf("claim2 got %d deliveries, want 0 (leased)", len(batch2))
	}
}

// ---------------------------------------------------------------------------
// Lease expiry at the delivery grain: a claimed-but-never-settled delivery
// becomes claimable again after claimed_until passes.
// ---------------------------------------------------------------------------

func TestClaim_DeliveryLeaseExpiry(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	o.Consume("c", "t", func(context.Context, event.Event) error { return nil })

	tx, _ := db.BeginTx(ctx, nil)
	o.Append(ctx, tx, "t", nil)
	tx.Commit()

	if _, err := o.expandDeliveries(ctx); err != nil {
		t.Fatalf("expand: %v", err)
	}
	batch1, err := o.claimDeliveries(ctx)
	if err != nil || len(batch1) != 1 {
		t.Fatalf("claim1: err=%v deliveries=%d", err, len(batch1))
	}

	// Simulate an abandoned claim: push claimed_until into the past.
	past := time.Now().UTC().Add(-10 * time.Minute)
	if _, err := db.Exec(fmt.Sprintf("UPDATE %s SET claimed_until = ? WHERE row_id = ? AND consumer = ?", o.qd()),
		past, batch1[0].RowID, batch1[0].Consumer); err != nil {
		t.Fatalf("set past lease: %v", err)
	}

	// The expired lease makes the delivery claimable again.
	batch2, err := o.claimDeliveries(ctx)
	if err != nil {
		t.Fatalf("claim2: %v", err)
	}
	if len(batch2) != 1 {
		t.Fatalf("claim2 after lease expiry got %d deliveries, want 1", len(batch2))
	}
	if batch2[0].RowID != batch1[0].RowID || batch2[0].Consumer != batch1[0].Consumer {
		t.Fatalf("re-claimed different delivery %+v (want %+v)", batch2[0], batch1[0])
	}
}

// ---------------------------------------------------------------------------
// stop() returns promptly even while a backlog keeps every pump full —
// the loop must honour stop between pumps, not only when idle.
// ---------------------------------------------------------------------------

func TestRelay_StopDuringBacklog(t *testing.T) {
	db, o := openOutbox(t, WithBatchSize(1), WithPollInterval(time.Hour))
	ctx := context.Background()

	var delivered int32
	o.Consume("slow", "slow", func(_ context.Context, _ event.Event) error {
		atomic.AddInt32(&delivered, 1)
		time.Sleep(50 * time.Millisecond)
		return nil
	})

	for i := 0; i < 50; i++ {
		tx, _ := db.BeginTx(ctx, nil)
		o.Append(ctx, tx, "slow", map[string]any{"i": i})
		tx.Commit()
	}

	stop := o.StartRelay(ctx)

	// Let the drain get going, then stop mid-backlog.
	deadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(&delivered) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	begin := time.Now()
	stop()
	elapsed := time.Since(begin)

	if elapsed > time.Second {
		t.Fatalf("stop() took %v mid-backlog, want prompt return", elapsed)
	}
	if got := atomic.LoadInt32(&delivered); got >= 40 {
		t.Fatalf("relay drained %d/50 rows before stopping — stop was ignored during backlog", got)
	}
}

func TestRelay_DrainsFullBatch(t *testing.T) {
	db, o := openOutbox(t, WithBatchSize(2))
	ctx := context.Background()

	var delivered int32
	o.Consume("bulk", "bulk", func(_ context.Context, _ event.Event) error {
		atomic.AddInt32(&delivered, 1)
		return nil
	})

	// Enqueue more rows than one batch — the relay must keep pumping.
	for i := 0; i < 5; i++ {
		tx, _ := db.BeginTx(ctx, nil)
		o.Append(ctx, tx, "bulk", map[string]any{"i": i})
		tx.Commit()
	}

	stop := o.StartRelay(ctx)
	defer stop()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&delivered) == 5 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&delivered); got != 5 {
		t.Fatalf("delivered %d events, want 5", got)
	}
}
