package outbox

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// Pins a hung consumer handler wedging every consumer's durable lane on the
// replica, found by the 2026-09-04 red-probe round; fixed by bounding each
// handler invocation with a per-delivery wall-clock budget (WithHandlerTimeout,
// default 30s, queue parity) that cancels the handler's context at the
// deadline and settles the delivery as failed so retries and sibling
// consumers proceed.
// Family: F7 delivery semantics under crash and retry (poison/slow message stalls the lane)
// Property: sibling isolation as documented ("one consumer failing never blocks another", doc.go) must survive a handler that HANGS, not just one that errors.
// Surfaces: relay.go::relayLoop+pump (sequential processDelivery on one goroutine),
//           relay.go::processDelivery+runHandler (per-delivery deadline, settle-as-failed on expiry),
//           delivery.go::invokeHandler (runs h on the bounded goroutine, recovers panics only).

// TestHungConsumerCannotStallSibling: while consumer "stuck" blocks inside
// its handler past its cancelled context, an unrelated consumer's delivery
// must still be invoked, and the stuck delivery must settle as failed so its
// retry cycle proceeds.
func TestHungConsumerCannotStallSibling(t *testing.T) {
	db, o := openOutbox(t,
		WithHandlerGrace(time.Hour),
		WithPollInterval(20*time.Millisecond),
		WithBatchSize(10),
		// The per-delivery budget the fix introduces (default 30s); short
		// here so the test observes the timeout, not the default.
		WithHandlerTimeout(100*time.Millisecond),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stuckEntered := make(chan struct{}, 1)
	releaseStuck := make(chan struct{})
	closeStuck := sync.OnceFunc(func() { close(releaseStuck) })
	o.Consume("stuck", "t.stuck", func(context.Context, event.Event) error {
		select {
		case stuckEntered <- struct{}{}:
		default:
		}
		<-releaseStuck // a handler blocked on a hung dependency, ignoring ctx
		return nil
	})
	var siblingRan atomic.Bool
	o.Consume("sibling", "t.ok", func(context.Context, event.Event) error {
		siblingRan.Store(true)
		return nil
	})

	stop := o.StartRelay(ctx)
	t.Cleanup(func() {
		closeStuck()
		stop()
	})

	// The stuck consumer's parent is older so its delivery is claimed first.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	stuckRow, err := o.Append(ctx, tx, "t.stuck", map[string]any{"n": 1})
	if err != nil {
		t.Fatalf("append stuck: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx1: %v", err)
	}
	time.Sleep(5 * time.Millisecond) // strictly older created_at for the stuck parent
	tx2, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx2: %v", err)
	}
	if _, err := o.Append(ctx, tx2, "t.ok", map[string]any{"n": 2}); err != nil {
		t.Fatalf("append ok: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("commit tx2: %v", err)
	}
	o.Nudge()

	select {
	case <-stuckEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("setup: stuck handler never entered")
	}

	// The sibling's delivery must not be starved by the stuck handler.
	deadline := time.After(5 * time.Second)
	for !siblingRan.Load() {
		select {
		case <-deadline:
			t.Fatal("SECURITY: [outbox] sibling consumer's delivery starved while an unrelated consumer's handler hung: " +
				"a handler that blocks past its cancelled context must settle as failed, not wedge the relay goroutine")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// The stuck delivery must settle as FAILED (retry scheduled), never hang
	// the lane or wait for the handler to return.
	deadline = time.After(5 * time.Second)
	for {
		stuck := findDelivery(t, mustDeliveries(t, o, stuckRow), "stuck")
		if stuck.Attempts >= 1 && strings.Contains(stuck.LastError, "budget") && stuck.Status == "pending" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("SECURITY: [outbox] hung consumer's delivery never settled as failed (attempts=%d status=%q last_error=%q): "+
				"the retry cycle cannot proceed while the handler ignores its context", stuck.Attempts, stuck.Status, stuck.LastError)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
