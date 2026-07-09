package island_test

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// fanoutComp is a minimal component for fanout tests.
type fanoutComp struct{ html string }

func (c *fanoutComp) Render() render.HTML { return render.Text(c.html) }

// TestIslandFanoutCrossDelivery: a session subscribed on manager B receives
// an update pushed on manager A (the other replica) via the shared fanout.
func TestIslandFanoutCrossDelivery(t *testing.T) {
	f := fanout.NewInProcess()
	mgrA := island.NewManager()
	mgrB := island.NewManager()
	stopA, err := mgrA.SetFanout(f)
	if err != nil {
		t.Fatalf("SetFanout A: %v", err)
	}
	defer stopA()
	stopB, err := mgrB.SetFanout(f)
	if err != nil {
		t.Fatalf("SetFanout B: %v", err)
	}
	defer stopB()

	// Session is connected only on B.
	chB := mgrB.Subscribe("sess-cross")

	// Update originates on A.
	mgrA.PushUpdate(island.IslandUpdate{IslandID: "isl-cross", HTML: "<p>remote</p>"}, "sess-cross")

	select {
	case upd := <-chB:
		if upd.IslandID != "isl-cross" {
			t.Errorf("IslandID = %q, want isl-cross", upd.IslandID)
		}
		if upd.HTML != "<p>remote</p>" {
			t.Errorf("HTML = %q, want <p>remote</p>", upd.HTML)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for cross-replica delivery to B")
	}
}

// TestIslandFanoutOwnNodeNoDup: when a session is subscribed on BOTH
// managers, an update pushed on A delivers once on A's stream and once on B's,
// with no echo duplicate on A (own-node drop).
func TestIslandFanoutOwnNodeNoDup(t *testing.T) {
	f := fanout.NewInProcess()
	mgrA := island.NewManager()
	mgrB := island.NewManager()
	stopA, _ := mgrA.SetFanout(f)
	defer stopA()
	stopB, _ := mgrB.SetFanout(f)
	defer stopB()

	// Session subscribed on both managers.
	chA := mgrA.Subscribe("sess-dup")
	chB := mgrB.Subscribe("sess-dup")

	mgrA.PushUpdate(island.IslandUpdate{IslandID: "isl-dup", HTML: "x"}, "sess-dup")

	// Each stream should get exactly one update.
	for name, ch := range map[string]<-chan island.IslandUpdate{"A": chA, "B": chB} {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s never received the update", name)
		}
	}
	// Give any echo loop time to manifest, then assert no duplicate on A.
	time.Sleep(120 * time.Millisecond)
	select {
	case extra := <-chA:
		t.Fatalf("A received a duplicate update (own-node echo not dropped): %+v", extra)
	default:
	}
}

// TestIslandFanoutPushReRenders: Push (not just PushUpdate) also crosses
// replicas. The island is registered on A; the session stream lives on B.
func TestIslandFanoutPushReRenders(t *testing.T) {
	f := fanout.NewInProcess()
	mgrA := island.NewManager()
	mgrB := island.NewManager()
	stopA, _ := mgrA.SetFanout(f)
	defer stopA()
	stopB, _ := mgrB.SetFanout(f)
	defer stopB()

	isl := island.NewIsland("isl-push", &fanoutComp{html: "rendered"})
	isl.SessionID = "sess-push"
	if err := mgrA.Register(isl); err != nil {
		t.Fatalf("Register: %v", err)
	}

	chB := mgrB.Subscribe("sess-push")
	if err := mgrA.Push("isl-push"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	select {
	case upd := <-chB:
		if upd.IslandID != "isl-push" {
			t.Errorf("IslandID = %q, want isl-push", upd.IslandID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Push to cross-deliver to B")
	}
}

// TestIslandFanoutNoFanoutStillWorks: without SetFanout, PushUpdate behaves
// exactly as before (local-only delivery).
func TestIslandFanoutNoFanoutStillWorks(t *testing.T) {
	mgr := island.NewManager()
	ch := mgr.Subscribe("solo")
	mgr.PushUpdate(island.IslandUpdate{IslandID: "i", HTML: "local"}, "solo")
	select {
	case upd := <-ch:
		if upd.HTML != "local" {
			t.Errorf("HTML = %q, want local", upd.HTML)
		}
	case <-time.After(time.Second):
		t.Fatal("local delivery failed without fanout")
	}
}

// TestIslandFanoutStopDetaches: after stop(), updates no longer cross.
func TestIslandFanoutStopDetaches(t *testing.T) {
	f := fanout.NewInProcess()
	mgrA := island.NewManager()
	mgrB := island.NewManager()
	stopB, _ := mgrB.SetFanout(f)
	defer stopB()
	stopA, _ := mgrA.SetFanout(f)

	chB := mgrB.Subscribe("sess-stop")
	mgrA.PushUpdate(island.IslandUpdate{IslandID: "i", HTML: "first"}, "sess-stop")
	select {
	case <-chB:
	case <-time.After(2 * time.Second):
		t.Fatal("pre-stop delivery missed")
	}

	stopA()
	mgrA.PushUpdate(island.IslandUpdate{IslandID: "i", HTML: "second"}, "sess-stop")
	select {
	case extra := <-chB:
		t.Fatalf("B received update after A's fanout was stopped: %+v", extra)
	case <-time.After(120 * time.Millisecond):
	}
}

// stalledFanout blocks Publish forever; Subscribe is a no-op. Reproduces a
// stalled backend (e.g. a hung Postgres) behind the publish path.
type stalledFanout struct{}

func (stalledFanout) Publish(ctx context.Context, _ string, _ []byte) error {
	<-ctx.Done()
	return ctx.Err()
}
func (stalledFanout) Subscribe(string, func([]byte)) (func(), error) { return func() {}, nil }

func TestPushUpdateNonBlockingOnStalledFanout(t *testing.T) {
	m := island.NewManager()
	if _, err := m.SetFanout(stalledFanout{}); err != nil {
		t.Fatalf("SetFanout: %v", err)
	}
	done := make(chan struct{})
	go func() {
		// PushUpdate runs on HTTP request goroutines (uihost signal updates);
		// it must never wait on the fanout backend.
		for i := 0; i < 50; i++ {
			m.PushUpdate(island.IslandUpdate{IslandID: "i1", HTML: "<b>x</b>"}, "s1")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("PushUpdate blocked on a stalled fanout backend")
	}
}
