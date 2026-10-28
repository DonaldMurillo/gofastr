package event

import (
	"context"
	"testing"
	"time"
)

type asyncCtxKey struct{}

// CRUD emits on the request's context, and the handler returns before an
// async subscriber gets to run. The webhook bridge's ListSubscribers query
// then failed with "context canceled" and the committed write lost its
// webhook (2026-10-27 B25). An async subscriber now runs on a context the
// caller's cancel does not reach, keeps its values, and is bounded by
// AsyncHandlerTimeout so a stuck subscriber does not run forever.
func TestEmitAsyncOutlivesCallerCtx(t *testing.T) {
	bus := NewEventBus()
	release := make(chan struct{})
	type seen struct {
		err         error
		val         any
		deadline    time.Time
		hasDeadline bool
	}
	got := make(chan seen, 3)
	handler := func(ctx context.Context, _ Event) error {
		<-release
		d, ok := ctx.Deadline()
		got <- seen{ctx.Err(), ctx.Value(asyncCtxKey{}), d, ok}
		return nil
	}
	bus.Subscribe("order.created", handler)
	bus.Subscribe("order.created", handler)
	// The fanout tap publishes to other replicas on the same context.
	bus.setTap(func(ctx context.Context, e Event) { _ = handler(ctx, e) })

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), asyncCtxKey{}, "req-1"))
	start := time.Now()
	bus.EmitAsync(ctx, Event{Type: "order.created"})
	cancel()
	close(release)

	for range 3 {
		select {
		case s := <-got:
			if s.err != nil {
				t.Errorf("async subscriber saw %v after the caller returned, want a live context", s.err)
			}
			if s.val != "req-1" {
				t.Errorf("async subscriber lost the caller's values: got %v", s.val)
			}
			if !s.hasDeadline || s.deadline.After(start.Add(AsyncHandlerTimeout+time.Second)) {
				t.Errorf("async subscriber has no bound: deadline %v (set %v)", s.deadline, s.hasDeadline)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("async subscriber never ran")
		}
	}
}
