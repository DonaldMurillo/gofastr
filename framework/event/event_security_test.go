package event_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
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
