package outbox

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
	_ "github.com/mattn/go-sqlite3"
)

// noopHandler is a consumer handler that always succeeds.
func noopHandler(context.Context, event.Event) error { return nil }

// ---------------------------------------------------------------------------
// Expansion creates exactly one pending delivery per declared consumer
// whose event type matches the row — and is idempotent.
// ---------------------------------------------------------------------------

func TestExpand_OneDeliveryPerMatchingConsumer(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()

	o.Consume("a", "order.placed", noopHandler)
	o.Consume("b", "order.placed", noopHandler)
	o.Consume("c", "user.created", noopHandler) // different type

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "order.placed", map[string]any{"k": 1})
	tx.Commit()

	n, err := o.expandDeliveries(ctx)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if n != 2 {
		t.Fatalf("expand created %d deliveries, want 2 (a,b for order.placed)", n)
	}
	ds := mustDeliveries(t, o, id)
	if len(ds) != 2 {
		t.Fatalf("got %d deliveries, want 2", len(ds))
	}
	for _, name := range []string{"a", "b"} {
		d := findDelivery(t, ds, name)
		if d.Status != "pending" || d.Attempts != 0 {
			t.Errorf("delivery %s = %+v, want pending/0", name, d)
		}
	}

	// Idempotent: a second expand creates nothing.
	n2, err := o.expandDeliveries(ctx)
	if err != nil {
		t.Fatalf("expand2: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second expand created %d, want 0 (idempotent)", n2)
	}
	if len(mustDeliveries(t, o, id)) != 2 {
		t.Errorf("deliveries grew after re-expand")
	}
}

// ---------------------------------------------------------------------------
// Sibling isolation: consumer A's handler always errors, B's succeeds →
// B's delivery reaches dispatched while A keeps retrying; the parent stays
// pending until A terminates. One broken consumer never blocks its
// siblings or the parent prematurely.
// ---------------------------------------------------------------------------

func TestSiblingIsolation_ErrorDoesNotBlockSibling(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(1000), WithPollInterval(5*time.Millisecond))
	// Large backoff so A never exhausts its attempts within the test
	// window — it stays pending, keeping the parent pending.
	o.backoffBase = 200 * time.Millisecond
	o.backoffMax = 500 * time.Millisecond
	ctx := context.Background()

	o.Consume("a", "t", func(context.Context, event.Event) error {
		return errors.New("a unavailable")
	})
	o.Consume("b", "t", noopHandler)

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", map[string]any{"k": 1})
	tx.Commit()

	stop := o.StartRelay(ctx)
	defer stop()

	// B dispatches promptly despite A failing.
	waitForDelivery(t, o, id, "b", "dispatched")

	ds := mustDeliveries(t, o, id)
	da := findDelivery(t, ds, "a")
	if da.Status != "pending" {
		t.Errorf("delivery a = %q, want pending (still retrying, not blocking b)", da.Status)
	}
	if da.Attempts < 1 {
		t.Errorf("delivery a attempts = %d, want >= 1", da.Attempts)
	}
	// Parent must NOT be dispatched while A is still pending.
	if r := findRow(t, mustList(t, o, "", 50), id); r.Status != "pending" {
		t.Errorf("parent = %q, want pending (a still pending)", r.Status)
	}
}

// ---------------------------------------------------------------------------
// Poison consumer dead-letters without blocking sibling: A panics every
// time → A goes dead after maxAttempts, B dispatched, parent becomes
// dispatched (A dead + B dispatched, no pending left).
// ---------------------------------------------------------------------------

func TestPoisonConsumer_DeadLettersWithoutBlockingSibling(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(2), WithPollInterval(5*time.Millisecond))
	o.backoffBase = time.Millisecond
	o.backoffMax = 5 * time.Millisecond
	ctx := context.Background()

	o.Consume("poison", "t", func(context.Context, event.Event) error {
		panic("always")
	})
	o.Consume("ok", "t", noopHandler)

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()

	stop := o.StartRelay(ctx)
	defer stop()

	waitForDelivery(t, o, id, "poison", "dead")
	waitForDelivery(t, o, id, "ok", "dispatched")
	// Parent completes: no pending deliveries (poison dead, ok dispatched).
	waitForParent(t, o, id, "dispatched")

	dPoison := findDelivery(t, mustDeliveries(t, o, id), "poison")
	if dPoison.Attempts != 2 {
		t.Errorf("poison attempts = %d, want 2", dPoison.Attempts)
	}
}

// ---------------------------------------------------------------------------
// Per-delivery attempts are independent: a failing consumer accrues
// attempts while a succeeding sibling stays at zero.
// ---------------------------------------------------------------------------

func TestPerDelivery_AttemptsIndependent(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(1000), WithPollInterval(5*time.Millisecond))
	o.backoffBase = 200 * time.Millisecond
	o.backoffMax = 500 * time.Millisecond
	ctx := context.Background()

	o.Consume("a", "t", func(context.Context, event.Event) error {
		return errors.New("nope")
	})
	o.Consume("b", "t", noopHandler)

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()

	stop := o.StartRelay(ctx)
	defer stop()

	db1 := waitForDelivery(t, o, id, "b", "dispatched")
	if db1.Attempts != 0 {
		t.Errorf("delivery b attempts = %d, want 0 (succeeded first try)", db1.Attempts)
	}
	da := findDelivery(t, mustDeliveries(t, o, id), "a")
	if da.Attempts < 1 {
		t.Errorf("delivery a attempts = %d, want >= 1 (independent of b)", da.Attempts)
	}
}

// ---------------------------------------------------------------------------
// Removed consumer: a delivery whose consumer has no handler anywhere is
// ABANDONED once past the grace, so it no longer blocks parent completion —
// whether the surviving sibling is still pending or already dispatched.
// Uses grace=0 + a direct pump for determinism.
// ---------------------------------------------------------------------------

func TestRemovedConsumer_AbandonedSiblingPending(t *testing.T) {
	db, o := openOutbox(t, WithHandlerGrace(0))
	ctx := context.Background()

	o.Consume("keep", "t", noopHandler) // still declared; oldsvc is removed

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()
	insertDelivery(t, db, o, id, "oldsvc", "pending", 2, "stale")

	o.pump(ctx) // expand "keep", deliver it, abandon "oldsvc", complete parent

	ds := mustDeliveries(t, o, id)
	if got := findDelivery(t, ds, "oldsvc").Status; got != "abandoned" {
		t.Errorf("removed-consumer delivery = %q, want abandoned", got)
	}
	if got := findDelivery(t, ds, "keep").Status; got != "dispatched" {
		t.Errorf("declared-consumer delivery = %q, want dispatched", got)
	}
	if got := findRow(t, mustList(t, o, "", 0), id).Status; got != "dispatched" {
		t.Errorf("parent = %q, want dispatched", got)
	}
}

func TestRemovedConsumer_AbandonedSiblingDispatched(t *testing.T) {
	db, o := openOutbox(t, WithHandlerGrace(0))
	ctx := context.Background()

	o.Consume("keep", "t", noopHandler)

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()
	// Surviving sibling already delivered in a prior process; the removed
	// consumer's delivery is still pending — nothing settles inline to
	// complete the parent, so abandonment must both settle it and complete.
	insertDelivery(t, db, o, id, "keep", "dispatched", 0, "")
	insertDelivery(t, db, o, id, "oldsvc", "pending", 1, "")

	o.pump(ctx)

	if got := findDelivery(t, mustDeliveries(t, o, id), "oldsvc").Status; got != "abandoned" {
		t.Errorf("removed-consumer delivery = %q, want abandoned", got)
	}
	if got := findRow(t, mustList(t, o, "", 0), id).Status; got != "dispatched" {
		t.Errorf("parent = %q, want dispatched", got)
	}
}

// ---------------------------------------------------------------------------
// Rolling-deploy ADD safety: a FRESH delivery for a consumer this replica
// doesn't declare (a newly-added consumer, still deploying) must be REQUEUED,
// never abandoned — otherwise a lagging replica would silently drop the new
// consumer's events. Uses a large grace so the fresh delivery is within it.
// ---------------------------------------------------------------------------

func TestAddedConsumer_FreshDeliveryNotAbandoned(t *testing.T) {
	db, o := openOutbox(t, WithHandlerGrace(time.Hour))
	ctx := context.Background()

	o.Consume("keep", "t", noopHandler) // this replica lacks "newsvc"

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()
	// "newsvc" was expanded by an up-to-date replica; it's fresh (age 0).
	insertDelivery(t, db, o, id, "newsvc", "pending", 0, "")

	o.pump(ctx)

	// The lagging replica must NOT have abandoned the fresh delivery, and must
	// NOT have completed/dropped the parent — the new replica still owes it.
	if got := findDelivery(t, mustDeliveries(t, o, id), "newsvc").Status; got != "pending" {
		t.Errorf("fresh removed-elsewhere delivery = %q, want pending (requeued, not abandoned)", got)
	}
	if got := findRow(t, mustList(t, o, "", 0), id).Status; got != "pending" {
		t.Errorf("parent = %q, want pending (new consumer not yet delivered)", got)
	}
}

// ---------------------------------------------------------------------------
// Orphan parent (event type nobody subscribes to): dropped once past the
// grace, but NOT before — so a rolling deploy adding a type's first consumer
// doesn't lose fresh events.
// ---------------------------------------------------------------------------

func TestOrphanType_DroppedAfterGrace(t *testing.T) {
	db, o := openOutbox(t, WithHandlerGrace(0))
	ctx := context.Background()
	o.Consume("c", "other", noopHandler) // a consumer, but for a different type

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil) // no consumer for "t"
	tx.Commit()

	o.pump(ctx)

	if ds := mustDeliveries(t, o, id); len(ds) != 0 {
		t.Errorf("got %d deliveries, want 0 (no consumer for type)", len(ds))
	}
	if got := findRow(t, mustList(t, o, "", 0), id).Status; got != "dispatched" {
		t.Errorf("orphan parent = %q, want dispatched (dropped)", got)
	}
}

func TestOrphanType_NotDroppedWithinGrace(t *testing.T) {
	db, o := openOutbox(t, WithHandlerGrace(time.Hour))
	ctx := context.Background()
	o.Consume("c", "other", noopHandler)

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()

	o.pump(ctx)

	// Within grace, a not-yet-consumed type's fresh event is kept, so a
	// first-consumer rollout in progress can still pick it up.
	if got := findRow(t, mustList(t, o, "", 0), id).Status; got != "pending" {
		t.Errorf("fresh orphan parent = %q, want pending (kept within grace)", got)
	}
}

// Rolling-deploy ADD, premature-completion variant: a parent must NOT be
// completed while young, because a consumer added on another replica hasn't
// had its delivery row created yet. If the parent completes first, expand
// (WHERE status='pending') can never create that delivery — the new consumer
// silently loses the event. The completion path must be age-gated like the
// abandon path. Here c2 is declared AFTER the parent was already fully
// delivered to c1; with a large grace, c2 must still pick the parent up.
func TestRollingAdd_YoungParentNotCompletedBeforeNewConsumerExpands(t *testing.T) {
	db, o := openOutbox(t, WithHandlerGrace(time.Hour))
	ctx := context.Background()
	o.Consume("c1", "t", noopHandler) // only c1 known at first

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()

	o.pump(ctx) // expands + delivers c1; must NOT complete the young parent
	if got := findRow(t, mustList(t, o, "", 0), id).Status; got != "pending" {
		t.Fatalf("parent = %q, want pending (young; a new consumer may still appear)", got)
	}

	// A new consumer is rolled out and now declared; the still-pending parent
	// must be expanded and delivered to it — not lost.
	o.Consume("c2", "t", noopHandler)
	o.pump(ctx)
	if got := findDelivery(t, mustDeliveries(t, o, id), "c2").Status; got != "dispatched" {
		t.Errorf("newly-added consumer c2 delivery = %q, want dispatched (event not lost)", got)
	}
}

// Backlog catch-up: an OLD, not-yet-expanded parent of a STILL-CONSUMED type
// must NOT be orphan-dropped — expand (batch-bounded, oldest-first) will reach
// it. Regression guard: a pure-time orphan drop (age only) would lose it after
// a >grace relay outage with a backlog. Here grace=0 makes every parent "old",
// and sweepParents runs BEFORE any expand.
func TestConsumedType_OldUnexpandedNotDropped(t *testing.T) {
	db, o := openOutbox(t, WithHandlerGrace(0))
	ctx := context.Background()
	o.Consume("c", "t", noopHandler) // "t" IS consumed

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()

	// Sweep BEFORE expanding: the parent is old (grace 0) with no delivery yet.
	if err := o.sweepParents(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := findRow(t, mustList(t, o, "", 0), id).Status; got != "pending" {
		t.Errorf("consumed-type parent = %q, want pending (type has a consumer; must not be dropped)", got)
	}
}

// ---------------------------------------------------------------------------
// Replay reopens a dead-lettered row: after the poison consumer is fixed
// (swapped for a success handler) and Replay called, the relay delivers
// the resurrected delivery and re-completes the parent.
// ---------------------------------------------------------------------------

func TestReplay_RedeliversAfterFix(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(2), WithPollInterval(5*time.Millisecond))
	o.backoffBase = time.Millisecond
	o.backoffMax = 5 * time.Millisecond
	ctx := context.Background()

	var fail int32
	o.Consume("svc", "t", func(context.Context, event.Event) error {
		if atomic.AddInt32(&fail, 1) <= 2 {
			return errors.New("transient")
		}
		return nil // fixed after the dead-letter
	})

	tx, _ := db.BeginTx(ctx, nil)
	id, _ := o.Append(ctx, tx, "t", nil)
	tx.Commit()

	stop := o.StartRelay(ctx)
	defer stop()

	// Delivery dead-letters after 2 failures; parent completes (dead).
	waitForDelivery(t, o, id, "svc", "dead")
	waitForParent(t, o, id, "dispatched")

	// Fix the handler (now succeeding) and replay the dead delivery.
	atomic.StoreInt32(&fail, 100)
	if err := o.ReplayConsumer(ctx, id, "svc"); err != nil {
		t.Fatalf("replay: %v", err)
	}
	// Relay re-delivers: delivery dispatched, parent re-completes.
	waitForDelivery(t, o, id, "svc", "dispatched")
	waitForParent(t, o, id, "dispatched")
}
