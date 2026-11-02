package outbox

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// The lease is fixed at five minutes and has no option, while
// WithHandlerTimeout accepts any budget. A handler allowed ten minutes ran
// past its own claim's lease while still inside its budget, so another
// relay re-claimed the delivery and ran it concurrently (or, with the
// budget spent, its sweep dead-lettered a delivery that was still running
// and about to succeed). New now stretches the lease to cover the handler
// budget plus its settle.
func TestLeaseCoversLongHandlerBudget(t *testing.T) {
	dialects := []struct {
		name string
		open func(t *testing.T, opts ...Option) (*sql.DB, *Outbox)
	}{
		{"sqlite", openOutbox},
		{"postgres", pgOutbox},
	}
	for _, dl := range dialects {
		t.Run(dl.name, func(t *testing.T) {
			db, o := dl.open(t, WithHandlerTimeout(10*time.Minute))
			if min := o.handlerTimeout + o.settleTimeout; o.lease < min {
				t.Errorf("lease %s is shorter than one delivery's budget %s", o.lease, min)
			}
			var mu sync.Mutex
			clock := time.Now().UTC().Truncate(time.Second)
			o.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }

			ctx := context.Background()
			var reclaimed []claimedDelivery
			runs := 0
			o.Consume("c", "t", func(_ context.Context, ev event.Event) error {
				runs++
				mu.Lock()
				clock = clock.Add(6 * time.Minute) // slow, still inside its 10m budget
				mu.Unlock()
				// Another relay's pump while this one is still running.
				var err error
				if reclaimed, err = o.claimDeliveries(ctx); err != nil {
					t.Errorf("concurrent claim: %v", err)
				}
				return nil
			})
			id := appendCommitted(t, db, o, "t")
			if n := o.pump(ctx); n != 1 {
				t.Fatalf("pump claimed %d deliveries, want 1", n)
			}
			if len(reclaimed) != 0 {
				t.Errorf("another relay re-claimed %d deliveries while the handler was inside its budget", len(reclaimed))
			}
			if d := findDelivery(t, mustDeliveries(t, o, id), "c"); d.Status != "dispatched" || d.Attempts != 1 || runs != 1 {
				t.Errorf("delivery %q after %d attempts, %d runs; want dispatched after one", d.Status, d.Attempts, runs)
			}
		})
	}
}

// A budget near the Duration limit saturates the floor instead of
// wrapping negative and leaving the five-minute lease in place.
func TestLeaseFloorSaturates(t *testing.T) {
	_, o := openOutbox(t, WithHandlerTimeout(time.Duration(1<<63-1)))
	if o.lease < o.handlerTimeout {
		t.Fatalf("lease %s is shorter than the handler budget %s", o.lease, o.handlerTimeout)
	}
	if _, def := openOutbox(t); def.lease != 5*time.Minute {
		t.Fatalf("default lease = %s, want 5m (the default budget fits)", def.lease)
	}
}
