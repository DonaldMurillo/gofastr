package event_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

func TestEventBus_EmitNilHandlerDoesNotPanic(t *testing.T) {
	t.Parallel()
	bus := event.NewEventBus()
	bus.On("thing.happened", nil)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SECURITY: [event] Emit panicked on nil handler: %v. Attack: process crash via nil event subscriber.", r)
		}
	}()

	_ = bus.Emit(context.Background(), event.Event{Type: "thing.happened"})
}

func TestEventBus_EmitPanickingHandlerDoesNotCrash(t *testing.T) {
	t.Parallel()
	bus := event.NewEventBus()
	bus.On("thing.happened", func(context.Context, event.Event) error {
		panic("boom")
	})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SECURITY: [event] Emit propagated handler panic: %v. Attack: synchronous event-triggered process crash.", r)
		}
	}()

	_ = bus.Emit(context.Background(), event.Event{Type: "thing.happened"})
}

func TestEventBus_EmitAsyncPanickingHandlerDoesNotCrashProcess(t *testing.T) {
	t.Parallel()
	if os.Getenv("GOFASTR_EVENT_ASYNC_PANIC") == "1" {
		bus := event.NewEventBus()
		bus.On("thing.happened", func(context.Context, event.Event) error {
			panic("boom")
		})
		bus.EmitAsync(context.Background(), event.Event{Type: "thing.happened"})
		time.Sleep(100 * time.Millisecond)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestEventBus_EmitAsyncPanickingHandlerDoesNotCrashProcess$")
	cmd.Env = append(os.Environ(), "GOFASTR_EVENT_ASYNC_PANIC=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SECURITY: [event] EmitAsync panicking handler crashed subprocess: %v\n%s", err, out)
	}
}

func TestEventBus_SubscribeNilHandlerDoesNotRetainSubscription(t *testing.T) {
	t.Parallel()
	bus := event.NewEventBus()
	bus.Subscribe("thing.happened", nil)

	if got := len(bus.Snapshot("thing.happened")); got != 0 {
		t.Fatalf("SECURITY: [event] nil handler was retained as a live subscription (count=%d). Attack: latent process crash via nil subscriber registration.", got)
	}
}

func TestEventBus_EmitAsyncNilHandlerDoesNotCrashProcess(t *testing.T) {
	t.Parallel()
	if os.Getenv("GOFASTR_EVENT_ASYNC_NIL") == "1" {
		bus := event.NewEventBus()
		bus.On("thing.happened", nil)
		bus.EmitAsync(context.Background(), event.Event{Type: "thing.happened"})
		time.Sleep(100 * time.Millisecond)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestEventBus_EmitAsyncNilHandlerDoesNotCrashProcess$")
	cmd.Env = append(os.Environ(), "GOFASTR_EVENT_ASYNC_NIL=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SECURITY: [event] EmitAsync nil handler crashed subprocess: %v\n%s", err, out)
	}
}

// ---------------------------------------------------------------------------
// Property: EmitStrict SURFACES a panicking subscriber as a delivery error
// instead of swallowing it — the exact contract the transactional outbox's
// relay depends on (a panicking consumer must be retried/dead-lettered,
// never silently credited as delivered).
// ---------------------------------------------------------------------------

func TestEventBus_EmitStrictSurfacesPanicAsError(t *testing.T) {
	t.Parallel()
	bus := event.NewEventBus()
	bus.On("order.placed", func(context.Context, event.Event) error {
		panic("consumer bug")
	})
	afterRan := false
	bus.On("order.placed", func(context.Context, event.Event) error {
		afterRan = true
		return nil
	})

	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: [event] EmitStrict let a subscriber panic escape: %v", r)
			}
		}()
		err = bus.EmitStrict(context.Background(), event.Event{Type: "order.placed"})
	}()
	if err == nil {
		t.Fatal("SECURITY: [event] EmitStrict swallowed a panicking subscriber (outbox would dead-mark it dispatched)")
	}
	if afterRan {
		t.Error("EmitStrict continued to later subscribers after a panicking one (first-error-stops violated)")
	}
}

// ---------------------------------------------------------------------------
// Property: Emit stops at the FIRST handler error and returns it — later
// subscribers are skipped for that event, deterministically, regardless of
// registration order. A bus that kept delivering after an error would run
// side effects for an event whose earlier consumer already rejected it.
// ---------------------------------------------------------------------------

func TestEventBus_EmitStopsAtFirstHandlerError(t *testing.T) {
	t.Parallel()
	bus := event.NewEventBus()
	var calls []int
	reject := errors.New("rejected")
	bus.On("t", func(context.Context, event.Event) error {
		calls = append(calls, 1)
		return reject
	})
	bus.On("t", func(context.Context, event.Event) error {
		calls = append(calls, 2)
		return nil
	})

	if err := bus.Emit(context.Background(), event.Event{Type: "t"}); !errors.Is(err, reject) {
		t.Fatalf("Emit err = %v, want the first handler's error", err)
	}
	if len(calls) != 1 {
		t.Errorf("handlers called = %v, want only the first (short-circuit on error)", calls)
	}
}

// ---------------------------------------------------------------------------
// Property: a subscription's cancel removes exactly its own handler — safe
// to call repeatedly, never a sibling's registration. A cancel that
// over-removed would silently unsubscribe an unrelated consumer; one that
// under-removed would keep delivering to a torn-down subscriber.
// ---------------------------------------------------------------------------

func TestEventBus_CancelRemovesOnlyOwnSubscription(t *testing.T) {
	t.Parallel()
	bus := event.NewEventBus()
	aGots, bGots := 0, 0

	cancelA := bus.Subscribe("t", func(context.Context, event.Event) error {
		aGots++
		return nil
	})
	cancelB := bus.Subscribe("t", func(context.Context, event.Event) error {
		bGots++
		return nil
	})

	cancelA()
	cancelA() // idempotent
	if err := bus.Emit(context.Background(), event.Event{Type: "t"}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if aGots != 0 {
		t.Errorf("cancelled subscriber A still received %d events", aGots)
	}
	if bGots != 1 {
		t.Errorf("sibling B received %d events, want 1 (cancel must not touch siblings)", bGots)
	}
	cancelB()
}

// ---------------------------------------------------------------------------
// Property: panic isolation at the fanout bridge's publisher goroutine —
// AttachFanout hands a framework-owned goroutine to host-supplied backend
// code (fanout.Fanout.Publish); a panic there is recovered and logged
// like the marshal-failure path, and the goroutine keeps draining the
// documented lossy best-effort lane instead of killing the process.
// ---------------------------------------------------------------------------

const bridgePanicChildEnv = "GOFASTR_TEST_EVENT_BRIDGE_PANIC_CHILD"

// TestBridgePublishPanicContained: a panicking fanout backend must not
// kill the process from the bridge's publisher goroutine, and the
// goroutine must survive to publish later events. Proven by re-exec.
func TestBridgePublishPanicContained(t *testing.T) {
	if os.Getenv(bridgePanicChildEnv) == "1" {
		bridgePublishPanicChild()
		return // unreachable; the child exits
	}

	cmd := exec.Command(os.Args[0],
		"-test.run", "^TestBridgePublishPanicContained$",
		"-test.count=1")
	cmd.Env = append(os.Environ(), bridgePanicChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SECURITY: [bridge-publish-panic] a panicking fanout backend killed the whole process from the bridge's publisher goroutine: %v\n--- child output ---\n%s", err, out)
	}
	if strings.Contains(string(out), "panic:") {
		t.Fatalf("SECURITY: [bridge-publish-panic] child reported a panic:\n%s", out)
	}
}

// panicFanout panics on its first Publish (a backend bug) and succeeds
// afterwards, so survival is observable: the second emit must still be
// published by a living publisher goroutine.
type panicFanout struct {
	calls atomic.Int32
}

func (f *panicFanout) Publish(_ context.Context, _ string, _ []byte) error {
	if f.calls.Add(1) == 1 {
		panic("fanout backend bug on first publish")
	}
	return nil
}

func (f *panicFanout) Subscribe(_ string, _ func([]byte)) (func(), error) {
	return func() {}, nil
}

// bridgePublishPanicChild is the child-side scenario. It never returns.
func bridgePublishPanicChild() {
	eb := event.NewEventBus()
	f := &panicFanout{}
	stop, err := event.AttachFanout(eb, f)
	if err != nil {
		os.Exit(10)
	}
	defer stop()

	// First emit: the tap enqueues synchronously; the publisher goroutine
	// calls f.Publish, which panics. Without the recover guard this
	// terminates the process (exit status 2).
	if err := eb.Emit(context.Background(), event.Event{Type: "bridge.ping", Data: map[string]any{"n": 1}}); err != nil {
		os.Exit(11)
	}
	// Second emit: proves the publisher goroutine (and the process)
	// survived the first panic and kept draining the bridge queue.
	if err := eb.Emit(context.Background(), event.Event{Type: "bridge.ping", Data: map[string]any{"n": 2}}); err != nil {
		os.Exit(12)
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if f.calls.Load() < 2 {
		os.Exit(13) // publisher goroutine died or stalled: second publish never happened
	}
	os.Exit(0)
}
