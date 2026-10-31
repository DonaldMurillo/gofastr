package island

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// holdRoster holds the first non-empty presence roster inside Publish
// until release closes, and records the member count of every presence
// message in the order it reaches the bus.
type holdRoster struct {
	fanout.Fanout
	entered chan struct{}
	release chan struct{}
	landed  chan struct{}
	once    sync.Once
	mu      sync.Mutex
	order   []int
}

func (h *holdRoster) Publish(ctx context.Context, topic string, payload []byte) error {
	if topic == presenceFanoutTopic {
		_, body, err := fanout.Unwrap(payload)
		var msg presenceFanoutMsg
		if err == nil && json.Unmarshal(body, &msg) == nil {
			if len(msg.Members) > 0 {
				held := false
				h.once.Do(func() { held = true })
				if held {
					close(h.entered)
					<-h.release
					defer close(h.landed)
				}
			}
			h.mu.Lock()
			h.order = append(h.order, len(msg.Members))
			h.mu.Unlock()
		}
	}
	return h.Fanout.Publish(ctx, topic, payload)
}

// A roster already in flight when the replica stops reaches the bus
// before the goodbye: landing after it restored this replica's members
// on every peer until their TTL.
func TestStopGoodbyeLandsLast(t *testing.T) {
	h := &holdRoster{Fanout: fanout.NewInProcess(), entered: make(chan struct{}), release: make(chan struct{}), landed: make(chan struct{})}
	m := NewManager()
	stop, err := m.SetFanout(h)
	if err != nil {
		t.Fatal(err)
	}
	m.reconfigurePresence(time.Hour, time.Hour)
	join := m.PresenceJoin("sess-1", PresenceIdentity{UserID: "u1"}, []string{"doc:1"})
	defer join.Leave()
	select {
	case <-h.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the join's roster never reached Publish")
	}
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	// Give a stop that does not wait time to send its goodbye first.
	select {
	case <-stopped:
	case <-time.After(100 * time.Millisecond):
	}
	close(h.release)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return")
	}
	select {
	case <-h.landed:
	case <-time.After(2 * time.Second):
		t.Fatal("the held roster never reached the bus")
	}
	h.mu.Lock()
	order := append([]int(nil), h.order...)
	h.mu.Unlock()
	if len(order) == 0 || order[len(order)-1] != 0 {
		t.Fatalf("presence messages by member count = %v, want the goodbye (0) last", order)
	}
}
