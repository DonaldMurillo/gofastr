package outbox

// Real-Postgres coverage for the per-consumer delivery core. The SQLite
// suite covers the full behaviour matrix; these tests prove the generated
// delivery SQL (CTE claim with FOR UPDATE OF d SKIP LOCKED, the ON CONFLICT
// expand upsert, the orphan-drop/completion sweep UPDATEs) executes against a
// live Postgres and the values come back out. One focused suite —
// representative paths only.
//
// Skips automatically when Postgres is unreachable (see internal/pgtest).

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/mattn/go-sqlite3"
)

// pgOutbox provisions a schema-scoped live-Postgres database (skipping
// when none is reachable) and returns a fresh Outbox over it. The outbox
// detects the Postgres dialect and creates both tables.
func pgOutbox(t *testing.T, opts ...Option) (*sql.DB, *Outbox) {
	t.Helper()
	db := pgtest.DB(t)
	// Zero handler grace so completion/abandonment happen promptly in tests
	// (matches the SQLite openOutbox helper); grace tests override after.
	o, err := New(db, append([]Option{WithHandlerGrace(0)}, opts...)...)
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	if o.dialect != dialectPostgres {
		t.Fatalf("dialect = %v, want postgres (pgtest returned a non-postgres DB)", o.dialect)
	}
	return db, o
}

// TestPostgres_DeliveryTableCreated asserts the child table + indexes are
// created on Postgres alongside the parent.
func TestPostgres_DeliveryTableCreated(t *testing.T) {
	db, _ := pgOutbox(t)
	for _, want := range []string{"event_outbox", "event_outbox_delivery"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name=$1`, want).Scan(&n); err != nil {
			t.Fatalf("query information_schema: %v", err)
		}
		if n != 1 {
			t.Errorf("table %q not created on postgres (count=%d)", want, n)
		}
	}
}

// TestPostgres_DeliversAndDispatches: a declared consumer receives the
// event and both the delivery and parent reach dispatched.
func TestPostgres_DeliversAndDispatches(t *testing.T) {
	db, o := pgOutbox(t, WithPollInterval(5*time.Millisecond))
	ctx := context.Background()

	got := make(chan event.Event, 1)
	o.Consume("logger", "order.placed", func(_ context.Context, e event.Event) error {
		got <- e
		return nil
	})

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "order.placed", map[string]any{"total": 7})
	tx.Commit()

	stop := o.StartRelay(ctx)
	defer stop()

	select {
	case e := <-got:
		if e.ID != id || e.Type != "order.placed" {
			t.Fatalf("event = %+v, want id=%s type=order.placed", e, id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for postgres delivery")
	}
	waitForParent(t, o, id, "dispatched")
	d := findDelivery(t, mustDeliveries(t, o, id), "logger")
	if d.Status != "dispatched" {
		t.Errorf("delivery = %q, want dispatched", d.Status)
	}
}

// TestPostgres_SiblingIsolation: a failing consumer does not block its
// succeeding sibling — B dispatches while A retries, parent stays pending.
func TestPostgres_SiblingIsolation(t *testing.T) {
	db, o := pgOutbox(t, WithMaxAttempts(1000), WithPollInterval(5*time.Millisecond))
	o.backoffBase = 200 * time.Millisecond
	o.backoffMax = 500 * time.Millisecond
	ctx := context.Background()

	o.Consume("a", "t", func(context.Context, event.Event) error { return errors.New("nope") })
	o.Consume("b", "t", noopHandler)

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()

	stop := o.StartRelay(ctx)
	defer stop()

	waitForDelivery(t, o, id, "b", "dispatched")
	da := findDelivery(t, mustDeliveries(t, o, id), "a")
	if da.Status != "pending" {
		t.Errorf("delivery a = %q, want pending (sibling isolation)", da.Status)
	}
	if r := findRow(t, mustList(t, o, "", 50), id); r.Status != "pending" {
		t.Errorf("parent = %q, want pending while a retries", r.Status)
	}
}

// TestPostgres_PoisonDeadLetters: a panicking consumer dead-letters while
// its sibling dispatches; the parent completes (dead + dispatched).
func TestPostgres_PoisonDeadLetters(t *testing.T) {
	db, o := pgOutbox(t, WithMaxAttempts(2), WithPollInterval(5*time.Millisecond))
	o.backoffBase = time.Millisecond
	o.backoffMax = 5 * time.Millisecond
	ctx := context.Background()

	var calls int32
	o.Consume("poison", "t", func(context.Context, event.Event) error {
		atomic.AddInt32(&calls, 1)
		panic("boom")
	})
	o.Consume("ok", "t", noopHandler)

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()

	stop := o.StartRelay(ctx)
	defer stop()

	waitForDelivery(t, o, id, "poison", "dead")
	waitForDelivery(t, o, id, "ok", "dispatched")
	waitForParent(t, o, id, "dispatched")
}

// TestPostgres_ClaimDeliveryExclusivity: the CTE claim with FOR UPDATE
// SKIP LOCKED leases a delivery so a second claim sees nothing.
func TestPostgres_ClaimDeliveryExclusivity(t *testing.T) {
	db, o := pgOutbox(t)
	ctx := context.Background()
	o.Consume("c", "t", noopHandler)

	tx, _ := db.BeginTx(ctx, nil)
	o.Append(ctx, tx, "t", nil)
	tx.Commit()

	if _, err := o.expandDeliveries(ctx); err != nil {
		t.Fatalf("expand: %v", err)
	}
	b1, err := o.claimDeliveries(ctx)
	if err != nil || len(b1) != 1 {
		t.Fatalf("claim1: err=%v n=%d", err, len(b1))
	}
	b2, err := o.claimDeliveries(ctx)
	if err != nil {
		t.Fatalf("claim2: %v", err)
	}
	if len(b2) != 0 {
		t.Errorf("claim2 got %d deliveries, want 0 (leased via SKIP LOCKED)", len(b2))
	}
}

// TestPostgres_ClaimDeliveryLeaseExpiry: an expired claimed_until releases
// the delivery for re-claim on Postgres.
func TestPostgres_ClaimDeliveryLeaseExpiry(t *testing.T) {
	db, o := pgOutbox(t)
	ctx := context.Background()
	o.Consume("c", "t", noopHandler)

	tx, _ := db.BeginTx(ctx, nil)
	o.Append(ctx, tx, "t", nil)
	tx.Commit()

	if _, err := o.expandDeliveries(ctx); err != nil {
		t.Fatalf("expand: %v", err)
	}
	b1, err := o.claimDeliveries(ctx)
	if err != nil || len(b1) != 1 {
		t.Fatalf("claim1: err=%v n=%d", err, len(b1))
	}
	past := time.Now().UTC().Add(-10 * time.Minute)
	if _, err := db.Exec(`UPDATE event_outbox_delivery SET claimed_until=$1 WHERE row_id=$2 AND consumer=$3`,
		past, b1[0].RowID, b1[0].Consumer); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	b2, err := o.claimDeliveries(ctx)
	if err != nil || len(b2) != 1 {
		t.Fatalf("claim2 after expiry: err=%v n=%d", err, len(b2))
	}
}
