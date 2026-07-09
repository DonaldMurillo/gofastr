package event_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// attachBuses wires n buses to a single InProcess fanout, returning the buses
// and a single stop that detaches all of them. Each bus gets a handler for
// "ping" that increments its counter.
func attachBuses(t *testing.T, n int) (buses []*event.EventBus, counts []*int64, stop func()) {
	t.Helper()
	f := fanout.NewInProcess()
	buses = make([]*event.EventBus, n)
	counts = make([]*int64, n)
	stops := make([]func(), n)
	for i := range buses {
		buses[i] = event.NewEventBus()
		var c int64
		counts[i] = &c
		buses[i].On("ping", func(_ context.Context, _ event.Event) error {
			atomic.AddInt64(&c, 1)
			return nil
		})
		s, err := event.AttachFanout(buses[i], f)
		if err != nil {
			t.Fatalf("AttachFanout[%d]: %v", i, err)
		}
		stops[i] = s
	}
	return buses, counts, func() {
		for _, s := range stops {
			s()
		}
	}
}

// waitCounter polls c until it reaches want or the deadline passes.
func waitCounter(t *testing.T, c *int64, want int64, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(c) >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("%s: counter = %d, want >= %d", msg, atomic.LoadInt64(c), want)
}

// TestBridgeTwoBusCrossDelivery: an Emit on bus A fires the handler on bus B
// (connected to the other replica via the shared fanout).
func TestBridgeTwoBusCrossDelivery(t *testing.T) {
	buses, counts, stop := attachBuses(t, 2)
	defer stop()

	if err := buses[0].Emit(context.Background(), event.Event{Type: "ping"}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	// A fires synchronously (1), B fires via remote re-emit.
	waitCounter(t, counts[0], 1, "bus A")
	waitCounter(t, counts[1], 1, "bus B")
}

// TestBridgeThreeBusExactlyOnce: a 3-bus network delivers each event exactly
// once per remote bus and never loops or duplicates — the core loop guard.
func TestBridgeThreeBusExactlyOnce(t *testing.T) {
	buses, counts, stop := attachBuses(t, 3)
	defer stop()

	if err := buses[0].Emit(context.Background(), event.Event{Type: "ping", Data: map[string]any{"k": "v"}}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	waitCounter(t, counts[0], 1, "bus A (origin)")
	waitCounter(t, counts[1], 1, "bus B")
	waitCounter(t, counts[2], 1, "bus C")

	// Give any would-be loop time to manifest, then assert exactly-once.
	time.Sleep(120 * time.Millisecond)
	for i, c := range counts {
		if got := atomic.LoadInt64(c); got != 1 {
			t.Fatalf("bus %d fired %d times, want exactly 1 (loop/duplicate detected)", i, got)
		}
	}
}

// TestBridgeDataRoundTripsAsMap: remote handlers see Event.Data as a
// map[string]any (JSON round-trip), even though it was emitted as a typed map.
func TestBridgeDataRoundTripsAsMap(t *testing.T) {
	f := fanout.NewInProcess()
	busA := event.NewEventBus()
	busB := event.NewEventBus()
	stopA, _ := event.AttachFanout(busA, f)
	defer stopA()
	stopB, _ := event.AttachFanout(busB, f)
	defer stopB()

	got := make(chan event.Event, 1)
	busB.On("ping", func(_ context.Context, e event.Event) error {
		got <- e
		return nil
	})

	_ = busA.Emit(context.Background(), event.Event{
		Type: "ping",
		Data: map[string]any{"entity": "widget", "count": float64(3)},
	})
	select {
	case e := <-got:
		m, ok := e.Data.(map[string]any)
		if !ok {
			t.Fatalf("remote Data type = %T, want map[string]any", e.Data)
		}
		if m["entity"] != "widget" {
			t.Errorf("entity = %v, want widget", m["entity"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for remote delivery")
	}
}

// TestBridgeMarshalFailureSkipsPublish: an event whose Data can't be JSON-
// marshalled is skipped for remote publish but still delivered locally.
func TestBridgeMarshalFailureSkipsPublish(t *testing.T) {
	f := fanout.NewInProcess()
	busA := event.NewEventBus()
	busB := event.NewEventBus()
	stopA, _ := event.AttachFanout(busA, f)
	defer stopA()
	stopB, _ := event.AttachFanout(busB, f)
	defer stopB()

	var aLocal int64
	busA.On("bad", func(context.Context, event.Event) error {
		atomic.AddInt64(&aLocal, 1)
		return nil
	})
	var bRemote int64
	busB.On("bad", func(context.Context, event.Event) error {
		atomic.AddInt64(&bRemote, 1)
		return nil
	})

	// A channel is not JSON-marshalable.
	_ = busA.Emit(context.Background(), event.Event{
		Type: "bad",
		Data: make(chan int),
	})

	// Local delivery on A must still work.
	waitCounter(t, &aLocal, 1, "local emit on A")
	// Remote must NOT have been published (marshal failed), so B stays 0.
	time.Sleep(120 * time.Millisecond)
	if got := atomic.LoadInt64(&bRemote); got != 0 {
		t.Fatalf("marshal-failed event was delivered to remote bus B (%d times); publish should have been skipped", got)
	}
}

// TestBridgeStopDetaches: after stop(), an Emit on A is no longer mirrored to
// the fanout, so B never sees it.
func TestBridgeStopDetaches(t *testing.T) {
	f := fanout.NewInProcess()
	busA := event.NewEventBus()
	busB := event.NewEventBus()
	stopA, _ := event.AttachFanout(busA, f)
	stopB, _ := event.AttachFanout(busB, f)
	defer stopB()

	var bRemote int64
	busB.On("ping", func(context.Context, event.Event) error {
		atomic.AddInt64(&bRemote, 1)
		return nil
	})

	// Confirm it works before stop.
	_ = busA.Emit(context.Background(), event.Event{Type: "ping"})
	waitCounter(t, &bRemote, 1, "before stop")

	stopA()
	// Reset and emit again; B must not advance.
	prev := atomic.LoadInt64(&bRemote)
	_ = busA.Emit(context.Background(), event.Event{Type: "ping"})
	time.Sleep(120 * time.Millisecond)
	if got := atomic.LoadInt64(&bRemote); got != prev {
		t.Fatalf("after stop, B advanced %d -> %d; A's tap should be detached", prev, got)
	}
}

// TestBridgeEmitAsyncCrossDelivers: EmitAsync (the path crud.EmitEvent uses)
// also crosses replicas via the tap.
func TestBridgeEmitAsyncCrossDelivers(t *testing.T) {
	buses, counts, stop := attachBuses(t, 2)
	defer stop()

	buses[0].EmitAsync(context.Background(), event.Event{Type: "ping"})
	waitCounter(t, counts[0], 1, "bus A (async)")
	waitCounter(t, counts[1], 1, "bus B (async)")
}

// stallFanout is a fanout.Fanout whose Publish blocks until release is closed
// (ignoring ctx, like a pathological stalled backend). Used to prove the
// bridge tap never blocks a synchronous emitter on a stalled backend.
type stallFanout struct {
	release <-chan struct{}
}

func (s *stallFanout) Publish(_ context.Context, _ string, _ []byte) error {
	<-s.release
	return nil
}
func (s *stallFanout) Subscribe(_ string, _ func([]byte)) (func(), error) {
	return func() {}, nil
}

// TestBridgeEmitDoesNotBlockOnStalledFanout: a fanout whose Publish blocks
// forever must not stall Emit — the tap enqueues to a bounded queue serviced
// by a dedicated goroutine, so synchronous emitters return promptly and
// overflow drops oldest (lossy real-time lane).
func TestBridgeEmitDoesNotBlockOnStalledFanout(t *testing.T) {
	release := make(chan struct{})
	bus := event.NewEventBus()
	stop, err := event.AttachFanout(bus, &stallFanout{release: release})
	if err != nil {
		t.Fatalf("AttachFanout: %v", err)
	}

	// Emit far more than the queue depth while the backend is stalled; none
	// of these should block.
	start := time.Now()
	for i := 0; i < 600; i++ {
		if err := bus.Emit(context.Background(), event.Event{Type: "x"}); err != nil {
			close(release)
			stop()
			t.Fatalf("Emit: %v", err)
		}
	}
	if d := time.Since(start); d > time.Second {
		close(release)
		stop()
		t.Fatalf("Emit blocked %s on a stalled fanout backend (tap must be non-blocking)", d)
	}

	// Release the backend so the publisher goroutine can finish and stop() returns.
	close(release)
	stop()
}

// TestAttachFanoutRejectsDoubleAttach: a second AttachFanout on the same bus
// must error (otherwise the first subscription stays live with a stale nodeID
// and echoes this bus's own emissions back to its handlers — double delivery).
func TestAttachFanoutRejectsDoubleAttach(t *testing.T) {
	f := fanout.NewInProcess()
	bus := event.NewEventBus()
	stop1, err := event.AttachFanout(bus, f)
	if err != nil {
		t.Fatalf("attach 1: %v", err)
	}
	defer stop1()
	if _, err := event.AttachFanout(bus, f); err == nil {
		t.Fatal("expected error on double AttachFanout, got nil")
	}
	// A single local emit still delivers exactly once (no self-echo via a
	// stale first subscription).
	var n atomic.Int64
	bus.On("ping", func(context.Context, event.Event) error { n.Add(1); return nil })
	if err := bus.Emit(context.Background(), event.Event{Type: "ping"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if got := n.Load(); got != 1 {
		t.Errorf("handler fired %d times for one local Emit, want 1", got)
	}
}

// TestAttachFanoutDetachReattach: stop() clears the attachment guard so
// detach→attach works (a transient detach must not permanently lock the bus).
func TestAttachFanoutDetachReattach(t *testing.T) {
	f := fanout.NewInProcess()
	bus := event.NewEventBus()
	stop1, err := event.AttachFanout(bus, f)
	if err != nil {
		t.Fatalf("attach 1: %v", err)
	}
	stop1() // detach clears the guard
	stop2, err := event.AttachFanout(bus, f)
	if err != nil {
		t.Fatalf("reattach after detach failed: %v", err)
	}
	stop2()
}

// TestIsRemoteReflectsOrigin: IsRemote is true inside a handler that received
// an event from another replica, and false for a locally-emitted event.
func TestIsRemoteReflectsOrigin(t *testing.T) {
	f := fanout.NewInProcess()
	busA := event.NewEventBus()
	busB := event.NewEventBus()
	stopA, _ := event.AttachFanout(busA, f)
	defer stopA()
	stopB, _ := event.AttachFanout(busB, f)
	defer stopB()

	var remote, local atomic.Bool
	busB.On("x", func(ctx context.Context, _ event.Event) error {
		if event.IsRemote(ctx) {
			remote.Store(true)
		} else {
			local.Store(true)
		}
		return nil
	})
	// A→B is a remote delivery; B-local is not.
	if err := busA.Emit(context.Background(), event.Event{Type: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := busB.Emit(context.Background(), event.Event{Type: "x"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if !remote.Load() {
		t.Error("IsRemote=false for a remote-delivered event")
	}
	if !local.Load() {
		t.Error("IsRemote=true for a locally-emitted event")
	}
}

// TestDerivedEventGatedExactlyOnce: the documented gating pattern
// (`if IsRemote(ctx) { return nil }`) makes a derived event ("y" from "x")
// deliver exactly once on every replica of a 2-bus network. Without the
// gate, every replica re-derives y and remote replicas observe duplicates.
func TestDerivedEventGatedExactlyOnce(t *testing.T) {
	f := fanout.NewInProcess()
	busA := event.NewEventBus()
	busB := event.NewEventBus()
	stopA, _ := event.AttachFanout(busA, f)
	defer stopA()
	stopB, _ := event.AttachFanout(busB, f)
	defer stopB()

	// Reactive handler on BOTH buses: derive "y" from "x", but only on the
	// origin replica.
	derive := func(bus *event.EventBus) event.EventHandler {
		return func(ctx context.Context, _ event.Event) error {
			if event.IsRemote(ctx) {
				return nil // remote-origin x: don't re-derive y here
			}
			return bus.Emit(context.Background(), event.Event{Type: "y"})
		}
	}
	busA.On("x", derive(busA))
	busB.On("x", derive(busB))

	var yA, yB atomic.Int64
	busA.On("y", func(context.Context, event.Event) error { yA.Add(1); return nil })
	busB.On("y", func(context.Context, event.Event) error { yB.Add(1); return nil })

	if err := busA.Emit(context.Background(), event.Event{Type: "x"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if gotA, gotB := yA.Load(), yB.Load(); gotA != 1 || gotB != 1 {
		t.Errorf("derived y: origin A=%d, remote B=%d; want 1 each (exactly-once with IsRemote gating)", gotA, gotB)
	}
}
