package webhook

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// Two replicas share one delivery store and one fanout, each running the
// bridge. An event emitted on one replica is re-emitted on the other; only
// the origin may queue the delivery, or the endpoint gets one per replica
// under a fresh delivery id each time.
func TestBridgeQueuesOncePerEvent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	f := fanout.NewInProcess()
	opts := Options{AllowPrivateNetworks: true}

	var mgrs [2]*Manager
	var buses [2]*event.EventBus
	var seen [2]atomic.Int64
	for i := range buses {
		mgrs[i] = New(store, opts)
		buses[i] = event.NewEventBus()
		cancel := Bridge(buses[i], mgrs[i])
		t.Cleanup(cancel)
		// Registered after the bridge, so it runs after the bridge's
		// handler for the same emit.
		n := &seen[i]
		buses[i].On(event.EntityCreated, func(context.Context, event.Event) error {
			n.Add(1)
			return nil
		})
		stop, err := event.AttachFanout(buses[i], f)
		if err != nil {
			t.Fatalf("AttachFanout[%d]: %v", i, err)
		}
		t.Cleanup(stop)
	}

	sub, err := mgrs[0].Subscribe(ctx, Subscriber{
		URL:    "http://127.0.0.1:9/hook",
		Secret: "s", // not-a-secret: test fixture
		Events: []string{event.EntityCreated},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := buses[0].Emit(ctx, event.Event{Type: event.EntityCreated, Data: map[string]any{"id": 1}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for seen[1].Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if seen[1].Load() != 1 {
		t.Fatalf("the second replica saw the event %d times, want 1", seen[1].Load())
	}

	ds, err := store.ListDeliveries(ctx, sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 {
		t.Fatalf("got %d deliveries for one event across two replicas, want 1", len(ds))
	}
}
