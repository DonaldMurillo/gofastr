package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
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

// ============================================================================
// Property: delivery EXPANSION is duplicate-free under concurrent relays —
// the NOT EXISTS guard alone is documented as "a read-time filter, not a
// concurrency guard", the INSERT OR IGNORE upsert is what makes two
// replicas racing expand leave exactly one delivery per (row, consumer).
// A duplicate row here means one event delivered twice to a consumer.
// ============================================================================

func TestExpandConcurrentNeverDuplicates(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "expand.db")) +
		"?_busy_timeout=5000&_journal_mode=WAL"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { db.Close() })

	o, err := New(db, WithBatchSize(200))
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	o.Consume("a", "order.placed", noopHandler)
	o.Consume("b", "order.placed", noopHandler)

	ctx := context.Background()
	const parents = 20
	for i := range parents {
		tx, _ := db.BeginTx(ctx, nil)
		if _, err := o.Append(ctx, tx, "order.placed", map[string]any{"i": i}); err != nil {
			t.Fatalf("append: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	// Four "replicas" expand concurrently against the shared table.
	const relays = 4
	var wg sync.WaitGroup
	errs := make(chan error, relays*10)
	for r := 0; r < relays; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				if _, err := o.expandDeliveries(ctx); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent expand errored: %v", err)
	}

	rows, err := db.QueryContext(ctx, fmt.Sprintf(
		`SELECT row_id, consumer, COUNT(*) FROM %s GROUP BY row_id, consumer HAVING COUNT(*) > 1`, o.qd()))
	if err != nil {
		t.Fatalf("dup query: %v", err)
	}
	defer rows.Close()
	var dupes []string
	for rows.Next() {
		var rowID, consumer string
		var n int
		_ = rows.Scan(&rowID, &consumer, &n)
		dupes = append(dupes, fmt.Sprintf("%s/%s×%d", rowID, consumer, n))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("dup query rows: %v", err)
	}
	if len(dupes) > 0 {
		t.Errorf("SECURITY: [outbox] concurrent expand created duplicate deliveries (duplicate delivery = duplicate consumer invocation): %d, first: %s", len(dupes), dupes[0])
	}
	var total int
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, o.qd())).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 2*parents {
		t.Errorf("delivery rows = %d, want exactly %d (one per parent per consumer)", total, 2*parents)
	}
}

// ============================================================================
// Property: Replay's parent-reopen is guarded by status='dispatched', so a
// replay against a still-PENDING parent resurrects its dead deliveries
// without clobbering parent state, sibling dispatched deliveries stay
// terminal, and resurrected rows are scheduled immediately (attempts
// cleared, no backoff). Asserted at both Replay surfaces (whole-row and
// single-consumer) since both carry the same guard.
// ============================================================================

func TestReplayPendingParentNotClobbered(t *testing.T) {
	surfaces := []struct {
		name string
		fire func(o *Outbox, rowID string) error
	}{
		{"Replay", func(o *Outbox, rowID string) error { return o.Replay(context.Background(), rowID) }},
		{"ReplayConsumer", func(o *Outbox, rowID string) error {
			return o.ReplayConsumer(context.Background(), rowID, "deadone")
		}},
	}
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			db, o := openOutbox(t, WithHandlerGrace(time.Hour))
			ctx := context.Background()

			tx, _ := db.BeginTx(ctx, nil)
			id, _ := o.Append(ctx, tx, "t", nil)
			tx.Commit()
			insertDelivery(t, db, o, id, "deadone", "dead", 3, "boom")
			insertDelivery(t, db, o, id, "sibling", "dispatched", 1, "")

			if err := s.fire(o, id); err != nil {
				t.Fatalf("%s: %v", s.name, err)
			}

			ds := mustDeliveries(t, o, id)
			d := findDelivery(t, ds, "deadone")
			if d.Status != "pending" || d.Attempts != 0 || d.LastError != "" {
				t.Errorf("resurrected delivery = status %q attempts %d lastErr %q, want pending/0/\"\" (immediate redelivery)", d.Status, d.Attempts, d.LastError)
			}
			if d.NextAttemptAt != nil {
				t.Errorf("resurrected delivery next_attempt_at = %v, want nil (scheduled immediately)", d.NextAttemptAt)
			}
			if got := findDelivery(t, ds, "sibling").Status; got != "dispatched" {
				t.Errorf("sibling = %q, want dispatched (Replay must not touch settled siblings)", got)
			}
			if got := findRow(t, mustList(t, o, "", 0), id).Status; got != "pending" {
				t.Errorf("parent = %q, want pending (reopen guard must not flip a pending parent)", got)
			}
		})
	}
}

// ============================================================================
// Property: with maxAttempts=1 the FIRST handler failure dead-letters
// (attempts+1 >= maxAttempts must include equality), and the recorded
// attempt count is exactly 1. An off-by-one here (> instead of >=) would
// grant a poison consumer one extra invocation per exhaustion cycle.
// ============================================================================

func TestFirstFailureDeadWhenMaxAttemptsOne(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(1), WithHandlerGrace(0))
	ctx := context.Background()

	o.Consume("poison", "t", func(context.Context, event.Event) error {
		return errors.New("always")
	})

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()

	o.pump(ctx)

	d := findDelivery(t, mustDeliveries(t, o, id), "poison")
	if d.Status != "dead" {
		t.Fatalf("delivery = %q, want dead on first failure when maxAttempts=1", d.Status)
	}
	if d.Attempts != 1 {
		t.Errorf("dead delivery attempts = %d, want exactly 1", d.Attempts)
	}
	if d.LastError != "always" {
		t.Errorf("last_error = %q, want the handler error", d.LastError)
	}
	// Terminal delivery settles the parent even though nothing dispatched.
	if got := findRow(t, mustList(t, o, "", 0), id).Status; got != "dispatched" {
		t.Errorf("parent = %q, want dispatched (dead is terminal)", got)
	}
}

// ============================================================================
// Property: a delivery whose consumer has no handler on this replica is
// REQUEUED (attempts untouched, backoff at backoffMax) and never
// dead-lettered on attempts — even when attempts already sits at/above
// maxAttempts — because a replica mid-rollout may still hold the handler.
// Dead-lettering here would drop events for a consumer whose deployment
// is simply lagging.
// ============================================================================

func TestNoHandlerRequeueNeverDeadLetters(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(2), WithHandlerGrace(time.Hour))
	ctx := context.Background()

	o.Consume("keep", "t", noopHandler) // "ghost" is declared nowhere here

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()
	// Attempts already EXHAUSTED: the requeue path must still not dead-letter.
	insertDelivery(t, db, o, id, "ghost", "pending", 2, "")

	o.pump(ctx)

	d := findDelivery(t, mustDeliveries(t, o, id), "ghost")
	if d.Status != "pending" {
		t.Fatalf("no-handler delivery = %q, want pending (requeued, never dead on attempts)", d.Status)
	}
	if d.Attempts != 2 {
		t.Errorf("requeued delivery attempts = %d, want 2 (requeue must not count attempts)", d.Attempts)
	}
	if d.NextAttemptAt == nil {
		t.Error("requeued delivery next_attempt_at = nil, want a backoff (max backoff)")
	}
	if got := findRow(t, mustList(t, o, "", 0), id).Status; got != "pending" {
		t.Errorf("parent = %q, want pending (unresolved delivery keeps it open)", got)
	}
}

// ============================================================================
// Property: the completion backstop settles an all-terminal parent once it
// passes the handler grace, even though per-delivery completion no-opped
// while the parent was young — a parent must never complete before the
// grace (a consumer on another replica may still expand) nor stay pending
// forever after every delivery settled.
// ============================================================================

func TestSweepBackstopCompletesAgedParent(t *testing.T) {
	db, o := openOutbox(t, WithHandlerGrace(time.Hour))
	ctx := context.Background()

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()
	insertDelivery(t, db, o, id, "only", "dead", 1, "boom")

	o.pump(ctx) // young parent: completeParent no-ops, sweep's age gate holds it
	if got := findRow(t, mustList(t, o, "", 0), id).Status; got != "pending" {
		t.Fatalf("young all-terminal parent = %q, want pending (grace not elapsed)", got)
	}

	// Age the parent past the grace (simulates wall-clock passage).
	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		`UPDATE %s SET created_at = ? WHERE id = ?`, o.qt()),
		o.now().UTC().Add(-2*time.Hour), id); err != nil {
		t.Fatalf("age parent: %v", err)
	}

	o.pump(ctx)
	if got := findRow(t, mustList(t, o, "", 0), id).Status; got != "dispatched" {
		t.Errorf("aged all-terminal parent = %q, want dispatched (backstop completes it)", got)
	}
}

// ============================================================================
// Property: a parent payload that is not valid JSON never reaches a
// consumer handler and never settles as dispatched — the delivery fails
// (dead-lettering on exhaustion) instead of handing a consumer a silently
// empty/zero payload. A consumer that tolerated a nil Data would silently
// mis-process every corrupted event.
// ============================================================================

func TestCorruptPayloadNeverReachesHandler(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(1), WithHandlerGrace(0))
	ctx := context.Background()

	invoked := false
	o.Consume("c", "t", func(context.Context, event.Event) error {
		invoked = true
		return nil
	})

	const id = "row-corrupt"
	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (id, type, payload, status, attempts, created_at)
		 VALUES (?, 't', ?, 'pending', 0, ?)`, o.qt()),
		id, `{"broken": tru`, o.now().UTC()); err != nil {
		t.Fatalf("seed corrupt parent: %v", err)
	}
	insertDelivery(t, db, o, id, "c", "pending", 0, "")

	o.pump(ctx)

	if invoked {
		t.Error("SECURITY: [outbox] consumer handler was invoked for a corrupt (non-JSON) payload")
	}
	d := findDelivery(t, mustDeliveries(t, o, id), "c")
	if d.Status != "dead" {
		t.Fatalf("delivery = %q, want dead (corrupt payload is a terminal failure)", d.Status)
	}
	if !strings.Contains(d.LastError, "unmarshal") {
		t.Errorf("last_error = %q, want it to name the unmarshal failure", d.LastError)
	}
}

// ============================================================================
// Pins a failure settle writing the attempts counter absolutely from the
// claim-time snapshot, found by the 2026-09-04 red-probe round; fixed in
// markDeliveryFailure by incrementing relatively (attempts = attempts + 1)
// in both arms instead of persisting d.Attempts+1.
// Property: the attempts column must count every handler invocation the
// relay settled — a failure settle may never lower it below the number of
// attempts that actually ran.
// Surfaces: delivery.go markDeliveryFailure (both arms: the dead-letter
// arm and the requeue-with-backoff arm), reached from relay.go
// processDelivery for every claimed delivery whose handler fails.
// ============================================================================

// TestFailureSettleAttemptsMonotonic: two runners claim the same delivery
// (lease expiry mid-handler) and both handler invocations fail; the
// recorded attempts must equal the number of settled failures.
func TestFailureSettleAttemptsMonotonic(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(10), WithHandlerGrace(time.Hour))
	ctx := context.Background()

	o.Consume("c", "t", func(context.Context, event.Event) error {
		return errors.New("handler failing under both runners")
	})

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	id, err := o.Append(ctx, tx, "t", map[string]any{"k": 1})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := o.expandDeliveries(ctx); err != nil {
		t.Fatalf("expand: %v", err)
	}

	// Runner 1 claims (attempts snapshot 0) and its handler is still
	// running when the lease expires.
	w1, err := o.claimDeliveries(ctx)
	if err != nil || len(w1) != 1 {
		t.Fatalf("W1 claim: %v (len %d)", err, len(w1))
	}
	// Lease expiry: a second relay re-claims the still-pending delivery
	// and reads the same attempts snapshot (no settle has landed yet).
	if _, err := db.Exec(fmt.Sprintf(
		"UPDATE %s SET claimed_until = ? WHERE row_id = ? AND consumer = 'c'", o.qd()),
		o.now().UTC().Add(-time.Hour), id); err != nil {
		t.Fatalf("age W1 lease: %v", err)
	}
	w2, err := o.claimDeliveries(ctx)
	if err != nil || len(w2) != 1 {
		t.Fatalf("W2 re-claim: %v (len %d)", err, len(w2))
	}
	if w1[0].Attempts != 0 || w2[0].Attempts != 0 {
		t.Fatalf("setup: claim snapshots were %d and %d, both want 0 (pre-settle)",
			w1[0].Attempts, w2[0].Attempts)
	}

	// Both handlers fail; both settles run (the two-runner shape).
	const settledFailures = 2
	o.markDeliveryFailure(ctx, w1[0], errors.New("w1 failure"))
	o.markDeliveryFailure(ctx, w2[0], errors.New("w2 failure"))

	d := findDelivery(t, mustDeliveries(t, o, id), "c")
	if d.Attempts != settledFailures {
		t.Fatalf("SECURITY: [outbox] %d handler failures settled but the delivery records attempts=%d: "+
			"the second settle overwrote the first with the same stale claim-time snapshot, so MaxAttempts "+
			"bounds claim/settle cycles, not the handler invocations it exists to bound", settledFailures, d.Attempts)
	}
}
