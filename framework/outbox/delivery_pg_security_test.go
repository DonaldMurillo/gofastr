package outbox

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// Property: concurrent Postgres delivery claims hand every delivery to
// exactly one relay with zero lock-contention errors — the sibling of the
// serialized-claim property the SQLite twins in this pass pin on the SQLite
// dialect (TestClaimDeliveriesConcurrentNoBusy in delivery_security_test.go,
// TestSQLiteDequeueConcurrentNoBusy in battery/queue), where
// SELECT-then-UPDATE leaked SQLITE_BUSY/SQLITE_LOCKED to callers. Surface:
// Outbox.claimDeliveriesPostgres — the CTE UPDATE with FOR UPDATE OF d
// SKIP LOCKED.
//
// Pass 1 traced the Postgres path as canonical (one atomic claim step,
// SKIP LOCKED) and deferred this twin to a reachable Postgres (pgtest).
// Expected GREEN-PIN; a RED here is a NEW finding against that status.
// ============================================================================

// TestPGClaimDeliveriesConcurrentNoBusy drives the Postgres delivery claim
// with 4 concurrent relays released by a barrier against 200 pending
// deliveries. The claim is one atomic CTE statement, so there is no
// legitimate lock-contention error of any class (55P03/40P01/40001);
// exactly-once claim attribution and full drain are asserted alongside.
func TestPGClaimDeliveriesConcurrentNoBusy(t *testing.T) {
	db, o := pgOutbox(t, WithBatchSize(20))
	const relays = 4
	// pgtest pins MaxOpenConns(1) for advisory-lock suites; this claim uses
	// row locks only. SKIP LOCKED is untestable through a single serialized
	// connection — widen the pool so the relays genuinely race. search_path
	// rides the DSN, so every pooled connection stays schema-scoped.
	db.SetMaxOpenConns(8)

	o.Consume("a", "order.placed", noopHandler)
	o.Consume("b", "order.placed", noopHandler)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const parents = 100 // × 2 consumers = 200 deliveries
	for i := 0; i < parents; i++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin append tx: %v", err)
		}
		if _, err := o.Append(ctx, tx, "order.placed", map[string]any{"i": i}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit append %d: %v", i, err)
		}
	}
	// expandDeliveries is batch-capped per pump; drain until quiescent.
	expanded := 0
	for {
		n, err := o.expandDeliveries(ctx)
		if err != nil {
			t.Fatalf("expand: %v", err)
		}
		expanded += n
		if n == 0 {
			break
		}
	}
	if expanded != 2*parents {
		t.Fatalf("expand created %d deliveries, want %d", expanded, 2*parents)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	claims := make([][]string, relays)
	errs := make([][]string, relays)
	for r := 0; r < relays; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			<-start
			empty := 0
			for empty < 2 {
				if ctx.Err() != nil {
					errs[r] = append(errs[r], ctx.Err().Error())
					return
				}
				batch, err := o.claimDeliveries(ctx)
				switch {
				case err == nil && len(batch) > 0:
					empty = 0
					for _, d := range batch {
						claims[r] = append(claims[r], d.RowID+"|"+d.Consumer)
					}
				case err == nil:
					empty++
				default:
					errs[r] = append(errs[r], err.Error())
					time.Sleep(2 * time.Millisecond)
				}
			}
		}(r)
	}
	close(start)
	wg.Wait()

	var busy, other, all []string
	for r := 0; r < relays; r++ {
		all = append(all, claims[r]...)
		for _, e := range errs[r] {
			s := strings.ToLower(e)
			// 55P03 lock not available, 40P01 deadlock, 40001 serialization
			// failure — none legitimate from a single atomic CTE claim.
			if strings.Contains(s, "lock") || strings.Contains(s, "could not serialize access") {
				busy = append(busy, e)
			} else {
				other = append(other, e)
			}
		}
	}
	if len(busy) > 0 {
		t.Errorf("serialized-claim contract violated: %d lock-contention errors from concurrent relays; first: %q",
			len(busy), busy[0])
	}
	if len(other) > 0 {
		t.Errorf("concurrent claim surfaced unexpected errors; first: %q", other[0])
	}
	seen := make(map[string]int, 2*parents)
	for _, key := range all {
		seen[key]++
	}
	var dupes []string
	for key, n := range seen {
		if n > 1 {
			dupes = append(dupes, key)
		}
	}
	if len(dupes) > 0 {
		t.Errorf("same delivery claimed by multiple relays: %d pairs, first: %q", len(dupes), dupes[0])
	}
	if len(all) != 2*parents {
		t.Errorf("claimed %d of %d deliveries", len(all), 2*parents)
	}

	// Full-drain corroboration: nothing left claimable. A claimed-but-
	// unsettled row keeps status='pending' until the relay settles it —
	// claim eligibility is expressed by claimed_until, so that is what
	// "never claimed" must be asserted against (now = real time; every
	// claim set claimed_until = now+lease, well past the test's lifetime).
	var unclaimed int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM event_outbox_delivery WHERE status='pending' AND (claimed_until IS NULL OR claimed_until <= $1)",
		time.Now().UTC(),
	).Scan(&unclaimed); err != nil {
		t.Fatalf("count claimable deliveries: %v", err)
	}
	if unclaimed != 0 {
		t.Errorf("%d deliveries still claimable (never claimed or lease already expired)", unclaimed)
	}
}
