package event

import (
	"context"
	"testing"
)

// Cancelling a subscription leaves no handler reachable from the bus:
// the vacated slot is cleared, and an event type with no handlers left
// drops its map entry (2026-10-27 S16).
func TestUnsubscribeReleasesHandler(t *testing.T) {
	eb := NewEventBus()
	noop := func(context.Context, Event) error { return nil }
	cancelA := eb.Subscribe("e", noop)
	cancelB := eb.Subscribe("e", noop)

	cancelA()
	entries := eb.handlers["e"]
	if len(entries) != 1 {
		t.Fatalf("%d handlers left, want 1", len(entries))
	}
	for i, e := range entries[len(entries):cap(entries)] {
		if e.handler != nil {
			t.Fatalf("vacated slot %d still holds a handler", i)
		}
	}

	cancelB()
	if _, ok := eb.handlers["e"]; ok {
		t.Fatal("an event type with no handlers keeps its map entry")
	}
	cancelB() // a second cancel stays a no-op
}
