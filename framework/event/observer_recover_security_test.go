package event

import (
	"context"
	"testing"
)

// #669 (was #657): observeEmission ran the installed observer inline
// with no recover, so a panicking observer tore down the goroutine that
// published the event — including the request path when the bus is
// wired into an AfterCreate hook. The observer is host-installed code;
// a fault in it must not take the emission with it. Subscribers still
// run.
func TestPanickingObserverDoesNotKillEmission(t *testing.T) {
	SetObserver(func(Emission) { panic("observer boom") })
	defer SetObserver(nil)

	eb := NewEventBus()
	got := make(chan struct{}, 1)
	eb.Subscribe("posts.created", func(ctx context.Context, e Event) error {
		got <- struct{}{}
		return nil
	})
	// A panic in the observer would cross this call and fail the test.
	if err := eb.Emit(context.Background(), Event{Type: "posts.created"}); err != nil {
		t.Fatalf("Emit returned the observer's fault: %v", err)
	}
	select {
	case <-got:
	default:
		t.Fatal("the subscriber did not run after the observer panicked")
	}
}
