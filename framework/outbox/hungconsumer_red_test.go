//go:build red

package outbox

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// CONTRACT-QUESTION red: must the relay bound or isolate a consumer
// handler invocation, or is the "MUST be side-effecting but prompt"
// sentence in Consume's doc (consumer.go) the whole contract? Both queue
// backends give host handlers a wall-clock budget (DBQueue
// WithDBHandlerTimeout, MemoryQueue's default 30s timeout); the outbox
// relay invokes handlers on its single pump goroutine with no timeout and
// no concurrency, so ONE consumer handler that blocks (a dependency that
// hangs, a handler that ignores ctx) wedges EVERY consumer's durable
// delivery on the replica — including consumers of entirely unrelated
// event types — with no log line. The maintainer must either accept and
// document "a hung consumer stalls the durable lane" as the contract, add
// a per-delivery handler timeout option (queue parity), or settle
// deliveries concurrently.
// Family: F7 delivery semantics under crash and retry (poison/slow message stalls the lane)
// Property: sibling isolation as documented ("one consumer failing never blocks another", doc.go) must survive a handler that HANGS, not just one that errors.
// Surfaces: relay.go:relayLoop+pump (sequential processDelivery on one goroutine), relay.go:processDelivery (invokeHandler with no per-delivery deadline), delivery.go:invokeHandler (runs h synchronously, recovers panics only).
// Finding: with consumer "stuck" blocked inside its handler, a delivery for an unrelated consumer on a different event type is claimed in the same batch (or any later pump) and never reaches its handler; the relay goroutine is stuck inside the hung handler. Observed: sibling handler not invoked within 5s of the stuck handler entering.
// Severity: high — the durable event lane for every consumer on the replica is wedged by one unbounded handler; nothing is logged and each cycle only recovers after the 5-minute lease expiry re-runs the same stuck handler.
// Fix direction: give the relay a per-delivery handler timeout default (queue parity) or run claimed deliveries on bounded per-delivery goroutines so a hung consumer cannot starve its siblings.

// TestHungConsumerCannotStallSibling: while consumer "stuck" blocks inside
// its handler, an unrelated consumer's delivery must still be invoked.
func TestHungConsumerCannotStallSibling(t *testing.T) {
	db, o := openOutbox(t,
		WithHandlerGrace(time.Hour),
		WithPollInterval(20*time.Millisecond),
		WithBatchSize(10),
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
	if _, err := o.Append(ctx, tx, "t.stuck", map[string]any{"n": 1}); err != nil {
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
				"the single relay goroutine is blocked inside processDelivery, so one unbounded handler wedges every " +
				"consumer's durable lane on this replica (both queue backends give handlers a wall-clock budget; the relay does not)")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
