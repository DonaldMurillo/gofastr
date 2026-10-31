package outbox

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// A shutdown mid-batch hands the unstarted rest of the batch back with
// its attempts refunded, the way the lease-short branch already did. The
// claim had counted an attempt on every row; left leased, the budget
// sweep dead-lettered a never-run delivery once that lease expired
// (MaxAttempts 1 here, so the first shutdown is enough).
func TestPumpShutdownRefundsUnstarted(t *testing.T) {
	dialects := []struct {
		name string
		open func(t *testing.T, opts ...Option) (*sql.DB, *Outbox)
	}{
		{"sqlite", openOutbox},
		{"postgres", pgOutbox},
	}
	for _, dl := range dialects {
		t.Run(dl.name, func(t *testing.T) {
			db, o := dl.open(t, WithMaxAttempts(1))
			clock := time.Now().UTC().Truncate(time.Second)
			o.now = func() time.Time { return clock }

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ran := map[string]int{}
			o.Consume("c", "t", func(_ context.Context, ev event.Event) error {
				ran[ev.ID]++
				cancel() // shutdown arrives while the first delivery runs
				return nil
			})
			ids := []string{appendCommitted(t, db, o, "t"), appendCommitted(t, db, o, "t"), appendCommitted(t, db, o, "t")}
			if n := o.pump(ctx); n != len(ids) {
				t.Fatalf("pump claimed %d deliveries, want the whole batch of %d", n, len(ids))
			}
			started := 0
			for _, id := range ids {
				if ran[id] > 0 {
					started++
					continue
				}
				if d := findDelivery(t, mustDeliveries(t, o, id), "c"); d.Attempts != 0 || d.Status != "pending" {
					t.Errorf("unstarted event %s after the shutdown: %q with %d attempts, want pending with 0", id, d.Status, d.Attempts)
				}
			}
			if started != 1 {
				t.Fatalf("%d deliveries ran before the shutdown, want 1", started)
			}

			// Another relay, after the lease would have run out.
			clock = clock.Add(o.lease + time.Second)
			if err := o.deadLetterExpiredExhaustedClaims(context.Background()); err != nil {
				t.Fatalf("sweep: %v", err)
			}
			for range 2 * len(ids) {
				if o.pump(context.Background()) == 0 {
					break
				}
			}
			for _, id := range ids {
				if d := findDelivery(t, mustDeliveries(t, o, id), "c"); ran[id] != 1 || d.Status != "dispatched" || d.Attempts != 1 {
					t.Errorf("event %s: handler ran %d times, delivery %q after %d attempts (%s), want one run, dispatched after 1",
						id, ran[id], d.Status, d.Attempts, d.LastError)
				}
			}
		})
	}
}
