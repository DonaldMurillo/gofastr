package event_test

import (
	"context"
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
