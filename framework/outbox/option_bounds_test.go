package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// WithPollInterval(0) reached time.NewTicker on the relay goroutine, which
// panics on a non-positive interval: the panic is unrecovered, so StartRelay
// crashed the process. A non-positive interval now keeps the 1s default.
func TestNonPositivePollIntervalKeepsDefault(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		_, o := openOutbox(t, WithPollInterval(d))
		if o.pollInterval != time.Second {
			t.Fatalf("WithPollInterval(%s): poll interval %s, want the 1s default", d, o.pollInterval)
		}
		stop := o.StartRelay(context.Background())
		stop()
	}
}

// WithBatchSize(0) bound LIMIT 0 into both the expansion and the claim, so
// no event was ever delivered; a negative size was unlimited on SQLite and
// an error on every Postgres pump (LIMIT must not be negative). A
// non-positive size now keeps the default of 100, and a non-positive
// WithMaxAttempts keeps its default of 10 instead of dead-lettering on the
// first failure.
func TestNonPositiveBatchAndAttemptsKeepDefaults(t *testing.T) {
	dialects := []struct {
		name string
		open func(t *testing.T, opts ...Option) (*sql.DB, *Outbox)
	}{
		{"sqlite", openOutbox},
		{"postgres", pgOutbox},
	}
	for _, dl := range dialects {
		for _, n := range []int{0, -1} {
			t.Run(fmt.Sprintf("%s/%d", dl.name, n), func(t *testing.T) {
				db, o := dl.open(t, WithBatchSize(n), WithMaxAttempts(n))
				if o.batchSize != 100 || o.maxAttempts != 10 {
					t.Errorf("batch %d, max attempts %d; want the defaults 100 and 10", o.batchSize, o.maxAttempts)
				}
				fail := true
				o.Consume("c", "t", func(context.Context, event.Event) error {
					if fail {
						fail = false
						return errors.New("transient")
					}
					return nil
				})
				id := appendCommitted(t, db, o, "t")
				ctx := context.Background()
				if got := o.pump(ctx); got != 1 {
					t.Fatalf("pump(%d) claimed %d deliveries, want 1", n, got)
				}
				// Clear the backoff and pump the retry.
				if _, err := db.Exec("UPDATE " + o.qd() + " SET next_attempt_at = NULL"); err != nil {
					t.Fatal(err)
				}
				o.pump(ctx)
				if d := findDelivery(t, mustDeliveries(t, o, id), "c"); d.Status != "dispatched" {
					t.Fatalf("delivery %q after %d attempts (%s), want dispatched on the retry", d.Status, d.Attempts, d.LastError)
				}
			})
		}
	}
}
