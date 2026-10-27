package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/backoff"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// ============================================================================
// The settle window opens when settling starts. processDelivery used to
// arm the 10s settle deadline BEFORE the handler ran, so a handler that
// succeeded after 11s (inside its 30s budget) recorded dispatched under an
// expired context: the write failed, the delivery stayed pending, and the
// lease recovery redelivered it until it dead-lettered.
// ============================================================================

// TestSlowHandlerStillSettles: a handler slower than the settle window,
// but inside its own budget, ends dispatched.
func TestSlowHandlerStillSettles(t *testing.T) {
	db, o := openOutbox(t, WithHandlerTimeout(5*time.Second))
	o.settleTimeout = 50 * time.Millisecond
	ctx := context.Background()
	o.Consume("c", "t", func(context.Context, event.Event) error {
		time.Sleep(200 * time.Millisecond) // 4x the settle window
		return nil
	})
	id := appendCommitted(t, db, o, "t")

	if n := o.pump(ctx); n != 1 {
		t.Fatalf("pump processed %d deliveries, want 1", n)
	}
	d := findDelivery(t, mustDeliveries(t, o, id), "c")
	if d.Status != "dispatched" {
		t.Fatalf("delivery status = %q (err %q), want dispatched: the settle deadline ran out "+
			"while the handler was still working", d.Status, d.LastError)
	}
}

// ============================================================================
// A stale settle must not touch a newer claim. W1's lease expires and W2
// re-claims the delivery; W1's late settle used to match on (row_id,
// consumer, status='pending') alone, so it cleared W2's lease (claimed_until
// = NULL) and a third relay claimed the delivery after the backoff while W2
// was still running it. Every settle now matches on the claimed_until value
// its own claim wrote.
// ============================================================================

// TestStaleSettleKeepsNewLease runs the claim/expire/reclaim/stale-settle
// sequence against every settle arm on SQLite and Postgres.
func TestStaleSettleKeepsNewLease(t *testing.T) {
	dialects := []struct {
		name string
		open func(t *testing.T, opts ...Option) (*sql.DB, *Outbox)
	}{
		{"sqlite", openOutbox},
		{"postgres", pgOutbox},
	}
	arms := []struct {
		name  string
		grace time.Duration
		// settle runs one settle arm with the given claim snapshot.
		settle func(o *Outbox, d claimedDelivery)
		// want is the status the CURRENT claimant's settle leaves.
		want string
		// wait is the delay to the next attempt that settle schedules;
		// nil means none (next_attempt_at NULL).
		wait func(o *Outbox, d claimedDelivery) time.Duration
		// refund is the attempts that settle hands back.
		refund int
	}{
		{"failure-requeue", time.Hour, func(o *Outbox, d claimedDelivery) {
			o.markDeliveryFailure(context.Background(), d, errors.New("late failure"))
		}, "pending", func(o *Outbox, d claimedDelivery) time.Duration {
			return backoff.Exponential(o.backoffBase, o.backoffMax, d.Attempts)
		}, 0},
		{"failure-dead", time.Hour, func(o *Outbox, d claimedDelivery) {
			d.Attempts = o.maxAttempts // take the dead-letter arm
			o.markDeliveryFailure(context.Background(), d, errors.New("late failure"))
		}, "dead", nil, 0},
		{"dispatched", time.Hour, func(o *Outbox, d claimedDelivery) {
			o.markDeliveryDispatched(context.Background(), d)
		}, "dispatched", nil, 0},
		{"nohandler-requeue", time.Hour, func(o *Outbox, d claimedDelivery) {
			o.requeueNoHandler(context.Background(), d)
		}, "pending", func(o *Outbox, _ claimedDelivery) time.Duration { return o.backoffMax }, 1},
		{"nohandler-abandon", 0, func(o *Outbox, d claimedDelivery) {
			o.requeueNoHandler(context.Background(), d)
		}, "abandoned", nil, 0},
	}
	for _, dl := range dialects {
		for _, arm := range arms {
			t.Run(dl.name+"/"+arm.name, func(t *testing.T) {
				// Nanoseconds in the clock exercise Postgres's microsecond
				// rounding of the fence value.
				clock := time.Date(2026, 10, 27, 12, 0, 0, 123456789, time.UTC)
				db, o := dl.open(t, WithHandlerGrace(arm.grace), WithMaxAttempts(5))
				o.now = func() time.Time { return clock }
				ctx := context.Background()
				id := appendCommitted(t, db, o, "t")
				if _, err := o.expandDeliveries(ctx); err != nil {
					t.Fatalf("expand: %v", err)
				}

				w1 := claimOne(t, o, "W1")
				clock = clock.Add(o.lease + time.Second) // W1's lease expires mid-handler
				w2 := claimOne(t, o, "W2")

				arm.settle(o, w1) // W1's late settle carries the stale snapshot

				d := findDelivery(t, mustDeliveries(t, o, id), "c")
				if d.Status != "pending" || d.Attempts != w2.Attempts {
					t.Fatalf("stale settle changed the row to %s/attempts=%d, want pending/%d (W2's claim)",
						d.Status, d.Attempts, w2.Attempts)
				}
				lease := storedLease(t, db, o, id)
				if lease == nil || lease.Sub(w2.ClaimedUntil).Abs() > time.Microsecond {
					t.Fatalf("stale settle rewrote W2's lease: claimed_until = %v, want %v", lease, w2.ClaimedUntil)
				}
				// Past every backoff W1 could have written, still inside W2's
				// lease: no third relay may claim the delivery.
				clock = clock.Add(o.backoffMax + time.Second)
				if got, err := o.claimDeliveries(ctx); err != nil || len(got) != 0 {
					t.Fatalf("W3 claimed %d deliveries (err %v) while W2 still holds the lease", len(got), err)
				}

				// The fence is not a blanket refusal: W2's own settle lands,
				// whole: lease cleared, next attempt scheduled, claim refunded.
				arm.settle(o, w2)
				got := findDelivery(t, mustDeliveries(t, o, id), "c")
				if got.Status != arm.want {
					t.Fatalf("current claimant's settle left status %q, want %q", got.Status, arm.want)
				}
				if lease := storedLease(t, db, o, id); lease != nil {
					t.Errorf("current claimant's settle kept the lease %v", lease)
				}
				if want := w2.Attempts - arm.refund; got.Attempts != want {
					t.Errorf("attempts = %d after the settle, want %d", got.Attempts, want)
				}
				switch {
				case arm.wait == nil && got.NextAttemptAt != nil:
					t.Errorf("next_attempt_at = %v, want none", got.NextAttemptAt)
				case arm.wait != nil:
					want := clock.Add(arm.wait(o, w2))
					if got.NextAttemptAt == nil || got.NextAttemptAt.Sub(want).Abs() > time.Microsecond {
						t.Errorf("next_attempt_at = %v, want %v", got.NextAttemptAt, want)
					}
				}
			})
		}
	}
}

// appendCommitted stages one event of type typ in its own transaction and
// declares consumer "c" for it when none is declared yet.
func appendCommitted(t *testing.T, db *sql.DB, o *Outbox, typ string) string {
	t.Helper()
	if !o.hasConsumers() {
		o.Consume("c", typ, noopHandler)
	}
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	id, err := o.Append(ctx, tx, typ, map[string]any{"k": 1})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return id
}

// claimOne claims and requires exactly one delivery.
func claimOne(t *testing.T, o *Outbox, who string) claimedDelivery {
	t.Helper()
	got, err := o.claimDeliveries(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("%s claim: %v (len %d), want one delivery", who, err, len(got))
	}
	return got[0]
}

// storedLease reads consumer "c"'s claimed_until for row id.
func storedLease(t *testing.T, db *sql.DB, o *Outbox, id string) *time.Time {
	t.Helper()
	var raw any
	if err := db.QueryRow(fmt.Sprintf(
		`SELECT claimed_until FROM %s WHERE row_id = $1 AND consumer = 'c'`, o.qd()), id).Scan(&raw); err != nil {
		t.Fatalf("read claimed_until: %v", err)
	}
	ts, err := outboxTimePtr(raw)
	if err != nil {
		t.Fatalf("decode claimed_until: %v", err)
	}
	return ts
}
