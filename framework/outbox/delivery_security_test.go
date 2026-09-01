package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// ============================================================================
// Property: concurrent SQLite claim transactions serialize cleanly — a
// multi-relay claim must not surface spurious 'database is locked' errors
// and must hand every delivery to exactly one claimant. Surface:
// Outbox.claimDeliveriesSQLite (BEGIN+SELECT+UPDATE+COMMIT claim shape),
// the sibling of battery/queue's dequeueSQLite, which fails this same
// property with SQLITE_BUSY / SQLITE_BUSY_SNAPSHOT under concurrent
// claimants (TestSQLiteDequeueConcurrentNoBusy).
// ============================================================================

// TestClaimDeliveriesConcurrentNoBusy drives the SQLite delivery claim with
// 4 concurrent relays released by a barrier against 200 pending deliveries,
// on a file-backed WAL database with a real connection pool (openOutbox's
// :memory:+MaxOpenConns(1) helper cannot express this shape). The claim
// path's comment promises "SQLite serialises writers (file-level lock under
// BEGIN) ... race-free"; this asserts that contract plus exactly-once
// claims.
func TestClaimDeliveriesConcurrentNoBusy(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "outbox.db")) +
		"?_busy_timeout=5000&_journal_mode=WAL"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = db.Close() })

	o, err := New(db, WithBatchSize(20))
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
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

	const relays = 4
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
				began := time.Now()
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
					// The wait rides on the error so a failure says which
					// kind it was: a busy error after ~busy_timeout means
					// the 5s allowance was exhausted on a slow runner,
					// while a fast one means a writer held the lock in a
					// way serialisation should have prevented (#363).
					errs[r] = append(errs[r], fmt.Sprintf("%s [after %s]",
						err, time.Since(began).Round(time.Millisecond)))
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
			if strings.Contains(s, "locked") || strings.Contains(s, "busy") {
				busy = append(busy, e)
			} else {
				other = append(other, e)
			}
		}
	}
	if len(busy) > 0 {
		t.Errorf("serialized-claim contract violated: %d busy/locked errors from concurrent relays: %q",
			len(busy), busy)
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
}
