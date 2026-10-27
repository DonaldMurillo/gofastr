package outbox

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// A pump claims a whole batch under one lease and runs the deliveries one
// after another. With slow handlers the later deliveries' lease ran out
// before their turn: another relay's budget sweep dead-lettered them
// (MaxAttempts 1) without their handler ever running, or re-claimed and ran
// them while this relay ran them too. The pump now hands back, attempt
// refunded, every claim whose remaining lease cannot cover one more
// handler budget plus its settle, and the next pump claims them afresh.
func TestPumpReleasesClaimsItCannotRun(t *testing.T) {
	dialects := []struct {
		name string
		open func(t *testing.T, opts ...Option) (*sql.DB, *Outbox)
	}{
		{"sqlite", openOutbox},
		{"postgres", pgOutbox},
	}
	for _, dl := range dialects {
		t.Run(dl.name, func(t *testing.T) {
			db, o := dl.open(t, WithMaxAttempts(1), WithHandlerTimeout(30*time.Second))
			o.lease = time.Minute // settleTimeout 10s: a delivery needs 40s of lease
			var mu sync.Mutex
			clock := time.Now().UTC().Truncate(time.Second)
			o.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }

			ctx := context.Background()
			ran := map[string]int{}
			calls := 0
			o.Consume("c", "t", func(_ context.Context, ev event.Event) error {
				mu.Lock()
				clock = clock.Add(25 * time.Second) // a slow handler, inside its 30s budget
				ran[ev.ID]++
				calls++
				third := calls == 3
				mu.Unlock()
				if third {
					// Another relay's pump: its budget sweep runs before every claim.
					if err := o.deadLetterExpiredExhaustedClaims(ctx); err != nil {
						t.Errorf("sweep: %v", err)
					}
				}
				return nil
			})
			const n = 4
			ids := make([]string, n)
			for i := range ids {
				ids[i] = appendCommitted(t, db, o, "t")
			}
			for range 2 * n {
				if o.pump(ctx) == 0 {
					break
				}
			}
			for _, id := range ids {
				d := findDelivery(t, mustDeliveries(t, o, id), "c")
				if ran[id] != 1 || d.Status != "dispatched" || d.Attempts != 1 {
					t.Errorf("event %s: handler ran %d times, delivery %q after %d attempts (%s), "+
						"want one run, dispatched after 1 attempt", id, ran[id], d.Status, d.Attempts, d.LastError)
				}
			}
		})
	}
}

// A lease shorter than one handler budget plus its settle still delivers:
// the first delivery of every batch runs.
func TestPumpShortLeaseStillDelivers(t *testing.T) {
	db, o := openOutbox(t)
	o.lease = 10 * time.Second // under the default 30s handler budget + 10s settle
	ctx := context.Background()
	ids := []string{appendCommitted(t, db, o, "t"), appendCommitted(t, db, o, "t")}
	for range 4 {
		if o.pump(ctx) == 0 {
			break
		}
	}
	for _, id := range ids {
		if d := findDelivery(t, mustDeliveries(t, o, id), "c"); d.Status != "dispatched" || d.Attempts != 1 {
			t.Errorf("event %s: delivery %q after %d attempts, want dispatched after 1", id, d.Status, d.Attempts)
		}
	}
}

// A release that comes after the lease expired and another relay
// re-claimed the delivery leaves that claim's lease and attempt alone.
func TestStaleReleaseKeepsNewClaim(t *testing.T) {
	dialects := []struct {
		name string
		open func(t *testing.T, opts ...Option) (*sql.DB, *Outbox)
	}{
		{"sqlite", openOutbox},
		{"postgres", pgOutbox},
	}
	for _, dl := range dialects {
		t.Run(dl.name, func(t *testing.T) {
			db, o := dl.open(t)
			clock := time.Now().UTC().Truncate(time.Second)
			o.now = func() time.Time { return clock }
			ctx := context.Background()
			id := appendCommitted(t, db, o, "t")
			if _, err := o.expandDeliveries(ctx); err != nil {
				t.Fatal(err)
			}
			a, err := o.claimDeliveries(ctx)
			if err != nil || len(a) != 1 {
				t.Fatalf("first claim: %v (n=%d)", err, len(a))
			}
			clock = clock.Add(o.lease + time.Second)
			b, err := o.claimDeliveries(ctx)
			if err != nil || len(b) != 1 {
				t.Fatalf("re-claim: %v (n=%d)", err, len(b))
			}
			o.releaseUnstarted(ctx, a)
			if d := findDelivery(t, mustDeliveries(t, o, id), "c"); d.Attempts != 2 {
				t.Fatalf("attempts after a stale release = %d, want 2 (both claims)", d.Attempts)
			}
			// The re-claim still holds its lease: its settle lands.
			o.markDeliveryDispatched(ctx, b[0])
			if d := findDelivery(t, mustDeliveries(t, o, id), "c"); d.Status != "dispatched" {
				t.Fatalf("re-claim's settle after a stale release: status %q, want dispatched", d.Status)
			}
		})
	}
}
