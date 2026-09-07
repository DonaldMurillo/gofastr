package protocol_test

import (
	"context"
	"sync"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Pins: Len/TruncateAfter for undo/reset serialize with in-flight
// Live.Apply. The documented post-conditions are absolute: ResetSession
// "truncates the journal to zero entries and reloads" — it must return with
// the journal EMPTY — and Undo removes exactly the entry that was latest
// when invoked. Neither holds if the journal surgery interleaves with a
// concurrent Append.
// Surfaces: kiln/protocol/protocol.go::Tools.ResetSession (TruncateAfter(0)
// + Reload) and ::Tools.Undo (Len → TruncateAfter(n-1) → Reload) touch the
// journal OUTSIDE Live's mutex; only the trailing Reload re-locks. The
// verified interleaving: Apply blocks inside journal.Append (slow fsync on
// the JSONL journal is the production shape; the gate below is the
// deterministic test shape) while holding l.mu → ResetSession's
// TruncateAfter(0) empties the log → Apply's entry lands after → journal
// [E] → Reload replays E. ResetSession returned success with a surviving
// edit.
// Finding: ResetSession reports OK over a journal that is not empty, and
// Undo's cut is computed against a log a concurrent Append then rewrites —
// an entry that was mid-Apply when the operator hit Reset/Undo outlives the
// operation that was supposed to bound it.
// Fix direction: route the Len/TruncateAfter pair through Live (a Live
// method that holds l.mu across truncate+reload), so journal surgery and
// in-flight Applies are mutually excluded end to end.

// gatedJournal wraps a Journal so a test can freeze one Append
// mid-flight — inside Live.Apply, under l.mu — and release it on demand,
// and can observe when a TruncateAfter has completed. Seeding appends made
// before hold() pass straight through.
type gatedJournal struct {
	journal.Journal

	mu        sync.Mutex
	held      bool
	entered   chan struct{} // unbuffered: a send proves the Append is inside the wrapper
	release   chan struct{} // closed by the test to let held Appends through
	truncDone chan struct{} // buffered 1: signaled after each TruncateAfter completes
}

func newGatedJournal(inner journal.Journal) *gatedJournal {
	return &gatedJournal{
		Journal:   inner,
		entered:   make(chan struct{}),
		release:   make(chan struct{}),
		truncDone: make(chan struct{}, 1),
	}
}

// hold arms the gate: every Append after this point blocks until released.
func (g *gatedJournal) hold() {
	g.mu.Lock()
	g.held = true
	g.mu.Unlock()
}

func (g *gatedJournal) Append(e journal.Entry) (int, error) {
	g.mu.Lock()
	held := g.held
	g.mu.Unlock()
	if held {
		g.entered <- struct{}{}
		<-g.release
	}
	return g.Journal.Append(e)
}

func (g *gatedJournal) TruncateAfter(n int) error {
	err := g.Journal.TruncateAfter(n)
	select {
	case g.truncDone <- struct{}{}:
	default:
	}
	return err
}

// waitRecv waits for one signal channel, fataling on stall rather than
// sleeping.
func waitRecv(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("setup broken: %s never fired", what)
	}
}

// TestResetSessionSerializesApply drives the exact interleaving: an
// AddEntity Apply parked inside journal.Append under l.mu, ResetSession
// invoked from the operator side, then the parked Append released.
// ResetSession must return with the journal EMPTY.
func TestResetSessionSerializesApply(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-reset-serial")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	gated := newGatedJournal(journal.NewMemory())
	l, err := live.New(gated, factory)
	if err != nil {
		t.Fatal(err)
	}
	tools := protocol.New(l)
	ctx := context.Background()

	// Seed through the funnel while the gate is open.
	if res := tools.AddEntity(ctx, protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
	}); !res.OK {
		t.Fatalf("setup broken: seed add_entity: %+v", res)
	}

	gated.hold()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // the in-flight edit: parks inside Append under l.mu
		defer wg.Done()
		if res := tools.AddEntity(ctx, protocol.AddEntityArgs{
			Entity: &world.Entity{Name: "racer", Fields: []world.Field{{Name: "title", Type: "string"}}},
		}); !res.OK {
			t.Errorf("setup broken: racer add_entity: %+v", res)
		}
	}()
	waitRecv(t, gated.entered, "racer Append entering the gated journal")

	resetDone := make(chan struct{})
	go func() { // the operator hits Reset while the racer is mid-Apply
		defer wg.Done()
		defer close(resetDone)
		if res := tools.ResetSession(ctx, protocol.ResetSessionArgs{}); !res.OK {
			t.Errorf("setup broken: reset_session: %+v", res)
		}
	}()
	// With the fix, ResetSession holds Live's mutex across
	// truncate+reload, so it CANNOT return while the racer's Apply is
	// parked inside journal.Append under that same mutex. A return
	// here means the journal surgery escaped the funnel again.
	select {
	case <-resetDone:
		t.Errorf("SECURITY: [kiln-undo-reset-outside-funnel] ResetSession returned while an AddEntity Apply was still parked inside journal.Append under Live's mutex — its TruncateAfter ran outside the funnel, so the parked entry can land after the truncate and survive the reload")
		close(gated.release)
		wg.Wait()
		return
	case <-time.After(300 * time.Millisecond):
		// serialization held: the reset is queued behind the Apply
	}

	close(gated.release) // the parked Append lands; the queued Reset truncates AFTER it
	wg.Wait()
	waitRecv(t, resetDone, "ResetSession returning after the parked Append landed")

	n, err := gated.Len()
	if err != nil {
		t.Fatalf("setup broken: journal len: %v", err)
	}
	if n != 0 {
		t.Errorf("SECURITY: [kiln-undo-reset-outside-funnel] ResetSession returned OK but the journal holds %d entries — an entry that was mid-Apply when the operator hit Reset outlived the operation; documented post-condition (protocol.go): \"truncates the journal to zero entries and reloads\"", n)
	}
	var racerSurvived bool
	tools.Live().ReadSession(func(sess *journal.Session) {
		_, racerSurvived = sess.World.Entities["racer"]
	})
	if racerSurvived {
		t.Errorf("SECURITY: [kiln-undo-reset-outside-funnel] the racer entity survived reset_session — an edit that was mid-Apply when the operator hit Reset outlived the operation whose contract is an empty journal and an empty world")
	}
}

// TestUndoSerializesApply: same interleaving with two seeded entries and
// Undo instead of Reset. Undo's documented post-condition is that it
// removes exactly the entry that was latest when invoked (alpha's second
// edit, "beta"); an entry that was mid-Apply at invoke time must not
// silently become the change that survives.
func TestUndoSerializesApply(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-undo-serial")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	gated := newGatedJournal(journal.NewMemory())
	l, err := live.New(gated, factory)
	if err != nil {
		t.Fatal(err)
	}
	tools := protocol.New(l)
	ctx := context.Background()

	for _, name := range []string{"alpha", "beta"} {
		if res := tools.AddEntity(ctx, protocol.AddEntityArgs{
			Entity: &world.Entity{Name: name, Fields: []world.Field{{Name: "title", Type: "string"}}},
		}); !res.OK {
			t.Fatalf("setup broken: seed add_entity %s: %+v", name, res)
		}
	}

	gated.hold()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // the in-flight edit: parks inside Append under l.mu
		defer wg.Done()
		if res := tools.AddEntity(ctx, protocol.AddEntityArgs{
			Entity: &world.Entity{Name: "racer", Fields: []world.Field{{Name: "title", Type: "string"}}},
		}); !res.OK {
			t.Errorf("setup broken: racer add_entity: %+v", res)
		}
	}()
	waitRecv(t, gated.entered, "racer Append entering the gated journal")

	undoDone := make(chan struct{})
	go func() { // the operator hits Undo while the racer is mid-Apply
		defer wg.Done()
		defer close(undoDone)
		if res := tools.Undo(ctx, protocol.UndoArgs{}); !res.OK {
			t.Errorf("setup broken: undo: %+v", res)
		}
	}()
	// Undo's Len/TruncateAfter pair runs under Live's mutex, so like
	// Reset it cannot return while the racer's Apply holds that mutex
	// parked inside journal.Append.
	select {
	case <-undoDone:
		t.Errorf("SECURITY: [kiln-undo-reset-outside-funnel] Undo returned while an AddEntity Apply was still parked inside journal.Append under Live's mutex — its Len/TruncateAfter ran outside the funnel, so its cut was computed against a log a concurrent Append then rewrote")
		close(gated.release)
		wg.Wait()
		return
	case <-time.After(300 * time.Millisecond):
		// serialization held: the undo is queued behind the Apply
	}

	close(gated.release) // the parked Append lands; the queued Undo cuts AFTER it
	wg.Wait()
	waitRecv(t, undoDone, "Undo returning after the parked Append landed")

	// Read the journal back and collect what actually remains.
	entries, err := gated.Read()
	if err != nil {
		t.Fatalf("setup broken: journal read: %v", err)
	}
	var names []string
	for _, e := range entries {
		if e.Kind != journal.KindWorldEdit || e.Op != journal.OpAddEntity {
			continue
		}
		var p journal.AddEntityPayload
		if err := e.Decode(&p); err != nil {
			t.Fatalf("setup broken: decode add_entity entry: %v", err)
		}
		if p.Entity != nil {
			names = append(names, p.Entity.Name)
		}
	}
	for _, n := range names {
		if n == "racer" {
			t.Errorf("SECURITY: [kiln-undo-reset-outside-funnel] the racer entry survived Undo (journal now: %v) — Undo's Len/TruncateAfter ran outside Live's mutex, so its cut was computed against a log a concurrent Append then rewrote: beta (the entry latest at undo time) is gone while the mid-Apply racer landed after the truncate and the trailing Reload replayed it; documented post-condition (protocol.go): undo reverts \"the most recent change\"", names)
		}
	}
	if _, ok := tools.Live().Session().World.Entities["racer"]; ok {
		t.Errorf("SECURITY: [kiln-undo-reset-outside-funnel] the racer entity survived Undo — an edit that was mid-Apply when the operator hit Undo outlived the operation whose contract is reverting the latest change")
	}
}
