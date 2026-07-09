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

// waitForStatus polls List until the row with id reaches want, or fails
// after a short deadline. The relay is asynchronous, so tests must poll
// rather than assert immediately.
func waitForStatus(t *testing.T, o *Outbox, id, want string) {
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
	t.Fatalf("row %s never reached status %q within deadline", id, want)
}

// ---------------------------------------------------------------------------
// Relay delivers to a real EventBus subscriber and marks the row
// dispatched. Event.ID/Type/Timestamp round-trip; Data is map[string]any
// with numbers as float64.
// ---------------------------------------------------------------------------

func TestRelay_DeliversAndDispatches(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	bus := event.NewEventBus()
	got := make(chan event.Event, 1)
	bus.On("order.placed", func(_ context.Context, e event.Event) error {
		got <- e
		return nil
	})

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "order.placed", map[string]any{"total": 99, "sku": "A1"})
	created := time.Now()
	tx.Commit()

	stop := o.StartRelay(ctx, bus)
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

	waitForStatus(t, o, id, "dispatched")
}

// ---------------------------------------------------------------------------
// Failing handler: attempts increment with backoff, eventually dead.
// ---------------------------------------------------------------------------

func TestRelay_FailingHandler_GoesDead(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(3), WithPollInterval(5*time.Millisecond))
	// Tiny backoff so retries resolve well within the test deadline.
	o.backoffBase = time.Millisecond
	o.backoffMax = 5 * time.Millisecond
	ctx := context.Background()

	bus := event.NewEventBus()
	var emits int32
	bus.On("flaky", func(_ context.Context, _ event.Event) error {
		atomic.AddInt32(&emits, 1)
		return errors.New("handler unavailable")
	})

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "flaky", nil)
	tx.Commit()

	stop := o.StartRelay(ctx, bus)
	defer stop()

	waitForStatus(t, o, id, "dead")

	r := findRow(t, mustList(t, o, "dead", 0), id)
	if r.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", r.Attempts)
	}
	if r.LastError == "" {
		t.Error("LastError empty, want the handler error")
	}
	if got := atomic.LoadInt32(&emits); got != 3 {
		t.Errorf("handler invoked %d times, want 3", got)
	}
}

// A subscriber that PANICS must be treated as a delivery failure (retry →
// dead), never silently marked dispatched. The bus's plain Emit swallows
// panics to protect hook callers' transactions; the relay must use the
// panic-surfacing path so a lost event is retried and dead-lettered.
func TestRelay_PanickingHandler_GoesDead(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(3), WithPollInterval(5*time.Millisecond))
	o.backoffBase = time.Millisecond
	o.backoffMax = 5 * time.Millisecond
	ctx := context.Background()

	bus := event.NewEventBus()
	var emits int32
	bus.On("boom", func(_ context.Context, _ event.Event) error {
		atomic.AddInt32(&emits, 1)
		panic("subscriber exploded")
	})

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "boom", nil)
	tx.Commit()

	stop := o.StartRelay(ctx, bus)
	defer stop()

	waitForStatus(t, o, id, "dead")

	r := findRow(t, mustList(t, o, "dead", 0), id)
	if r.Attempts != 3 {
		t.Errorf("attempts = %d, want 3 (panic must be retried like an error)", r.Attempts)
	}
	if r.LastError == "" {
		t.Error("LastError empty, want the recovered panic")
	}
	if got := atomic.LoadInt32(&emits); got != 3 {
		t.Errorf("handler invoked %d times, want 3", got)
	}
	// The row must NOT have been marked dispatched at any point.
	if dispatched := mustList(t, o, "dispatched", 0); len(dispatched) != 0 {
		t.Errorf("a panicking-handler row was marked dispatched: %+v", dispatched)
	}
}

// ---------------------------------------------------------------------------
// Nudge wakes the relay faster than the poll interval.
// ---------------------------------------------------------------------------

func TestRelay_NudgeFasterThanPoll(t *testing.T) {
	db, o := openOutbox(t, WithPollInterval(time.Hour)) // poll would take an hour
	ctx := context.Background()

	bus := event.NewEventBus()
	got := make(chan event.Event, 1)
	bus.On("n", func(_ context.Context, e event.Event) error {
		got <- e
		return nil
	})

	stop := o.StartRelay(ctx, bus)
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
// Claim exclusivity: two sequential claims don't double-claim a row.
// (SQLite serialises writers, so we assert sequential exclusivity rather
// than true parallelism.)
// ---------------------------------------------------------------------------

func TestClaim_Exclusivity(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	tx, _ := db.BeginTx(ctx, nil)
	o.Append(ctx, tx, "t", nil)
	tx.Commit()

	batch1, err := o.claimBatch(ctx)
	if err != nil {
		t.Fatalf("claim1: %v", err)
	}
	if len(batch1) != 1 {
		t.Fatalf("claim1 got %d rows, want 1", len(batch1))
	}

	// The row is now leased (claimed_until in the future) — a second
	// claim must not see it.
	batch2, err := o.claimBatch(ctx)
	if err != nil {
		t.Fatalf("claim2: %v", err)
	}
	if len(batch2) != 0 {
		t.Fatalf("claim2 got %d rows, want 0 (row is leased)", len(batch2))
	}
}

// ---------------------------------------------------------------------------
// Lease expiry: a claimed-but-never-dispatched row becomes claimable
// after claimed_until passes.
// ---------------------------------------------------------------------------

func TestClaim_LeaseExpiry(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	tx, _ := db.BeginTx(ctx, nil)
	o.Append(ctx, tx, "t", nil)
	tx.Commit()

	// Claim → leases for 5 minutes.
	batch1, err := o.claimBatch(ctx)
	if err != nil || len(batch1) != 1 {
		t.Fatalf("claim1: err=%v rows=%d", err, len(batch1))
	}

	// Simulate an abandoned claim: push claimed_until into the past.
	past := time.Now().UTC().Add(-10 * time.Minute)
	if _, err := db.Exec(fmt.Sprintf("UPDATE %s SET claimed_until = ? WHERE id = ?", o.qt()),
		past, batch1[0].ID); err != nil {
		t.Fatalf("set past lease: %v", err)
	}

	// The expired lease makes the row claimable again.
	batch2, err := o.claimBatch(ctx)
	if err != nil {
		t.Fatalf("claim2: %v", err)
	}
	if len(batch2) != 1 {
		t.Fatalf("claim2 after lease expiry got %d rows, want 1", len(batch2))
	}
	if batch2[0].ID != batch1[0].ID {
		t.Fatalf("re-claimed different row id %q (want %q)", batch2[0].ID, batch1[0].ID)
	}
}

// ---------------------------------------------------------------------------
// Relay with a full batch keeps pumping until drained (no idle gap).
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// stop() returns promptly even while a backlog keeps every pump full —
// the loop must honour stop between pumps, not only when idle.
// ---------------------------------------------------------------------------

func TestRelay_StopDuringBacklog(t *testing.T) {
	db, o := openOutbox(t, WithBatchSize(1), WithPollInterval(time.Hour))
	ctx := context.Background()

	bus := event.NewEventBus()
	var delivered int32
	bus.On("slow", func(_ context.Context, _ event.Event) error {
		atomic.AddInt32(&delivered, 1)
		time.Sleep(50 * time.Millisecond)
		return nil
	})

	for i := 0; i < 50; i++ {
		tx, _ := db.BeginTx(ctx, nil)
		o.Append(ctx, tx, "slow", map[string]any{"i": i})
		tx.Commit()
	}

	stop := o.StartRelay(ctx, bus)

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

	bus := event.NewEventBus()
	var delivered int32
	bus.On("bulk", func(_ context.Context, _ event.Event) error {
		atomic.AddInt32(&delivered, 1)
		return nil
	})

	// Enqueue more rows than one batch — the relay must keep pumping.
	for i := 0; i < 5; i++ {
		tx, _ := db.BeginTx(ctx, nil)
		o.Append(ctx, tx, "bulk", map[string]any{"i": i})
		tx.Commit()
	}

	stop := o.StartRelay(ctx, bus)
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
