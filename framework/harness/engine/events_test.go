package engine

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

func TestBusPublishMonotonic(t *testing.T) {
	bus := NewBus(ids.NewSessionID())
	defer bus.Close()

	originator := ids.NewClientID()
	first, err := bus.Publish(control.TextDelta{Text: "a"}, originator)
	if err != nil {
		t.Fatal(err)
	}
	second, err := bus.Publish(control.TextDelta{Text: "b"}, originator)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID >= second.ID {
		t.Fatalf("expected monotonic IDs: first=%d second=%d", first.ID, second.ID)
	}
}

func TestBusSubscribeDelivery(t *testing.T) {
	bus := NewBus(ids.NewSessionID())
	defer bus.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := bus.Subscribe(ctx)

	originator := ids.NewClientID()
	want := []string{"a", "b", "c"}
	for _, s := range want {
		if _, err := bus.Publish(control.TextDelta{Text: s}, originator); err != nil {
			t.Fatal(err)
		}
	}
	got := []string{}
	timeout := time.After(time.Second)
	for i := 0; i < len(want); i++ {
		select {
		case env := <-ch:
			e, err := control.DecodeEvent(env)
			if err != nil {
				t.Fatal(err)
			}
			td := e.(control.TextDelta)
			got = append(got, td.Text)
		case <-timeout:
			t.Fatal("timeout waiting for events")
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBusSubscribeCancellation(t *testing.T) {
	bus := NewBus(ids.NewSessionID())
	defer bus.Close()
	ctx, cancel := context.WithCancel(context.Background())
	ch := bus.Subscribe(ctx)
	cancel()
	// Allow the goroutine to remove the subscription.
	select {
	case _, ok := <-ch:
		if ok {
			// Some buffered event possibly; that's OK as long as next is closed.
			_, ok = <-ch
		}
		if ok {
			t.Fatal("expected channel closed after context cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription channel never closed after cancel")
	}
}

func TestBusCloseClosesSubscribers(t *testing.T) {
	bus := NewBus(ids.NewSessionID())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := bus.Subscribe(ctx)

	bus.Close()
	select {
	case _, ok := <-ch:
		if ok {
			// Drain any race-buffered event then wait for close.
			_, ok = <-ch
		}
		if ok {
			t.Fatal("channel not closed after bus.Close()")
		}
	case <-time.After(time.Second):
		t.Fatal("channel never closed after bus.Close()")
	}
}

func TestBusSlowSubscriberDoesntBlock(t *testing.T) {
	bus := NewBus(ids.NewSessionID())
	defer bus.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Subscribe but never read.
	_ = bus.Subscribe(ctx)

	originator := ids.NewClientID()
	// Publish more than the buffer; should not block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1024; i++ {
			_, _ = bus.Publish(control.TextDelta{Text: "x"}, originator)
		}
		close(done)
	}()
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on slow subscriber")
	}
}
