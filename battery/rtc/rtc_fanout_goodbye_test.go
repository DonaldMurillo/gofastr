package rtc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// holdFirst holds the first rtc lane message that is not an empty
// roster inside Publish until release closes, and records every lane
// message in the order it reaches the bus.
type holdFirst struct {
	fanout.Fanout
	entered chan struct{}
	release chan struct{}
	landed  chan struct{}
	once    sync.Once
	mu      sync.Mutex
	order   []string
}

func (h *holdFirst) Publish(ctx context.Context, topic string, payload []byte) error {
	if topic == rtcFanoutTopic {
		_, body, err := fanout.Unwrap(payload)
		var msg fanoutMsg
		if err == nil && json.Unmarshal(body, &msg) == nil {
			if msg.Kind != evRoster || len(msg.Members) > 0 {
				held := false
				h.once.Do(func() { held = true })
				if held {
					close(h.entered)
					<-h.release
					defer close(h.landed)
				}
			}
			h.mu.Lock()
			h.order = append(h.order, fmt.Sprintf("%s/%d", msg.Kind, len(msg.Members)))
			h.mu.Unlock()
		}
	}
	return h.Fanout.Publish(ctx, topic, payload)
}

// A join already in flight when the Signaler detaches reaches the bus
// before the empty roster: landing after it restored the departed peer
// on every other replica until the TTL.
func TestDetachEmptyRosterLandsLast(t *testing.T) {
	h := &holdFirst{Fanout: fanout.NewInProcess(), entered: make(chan struct{}), release: make(chan struct{}), landed: make(chan struct{})}
	s := newTestSignaler(t, Config{})
	s.heartbeatEvery = time.Hour
	stop, err := s.SetFanout(h)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := join(t, startSignaler(t, s), "room1", "p1")
	defer c.close()
	select {
	case <-h.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the join never reached Publish")
	}
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	// Give a detach that does not wait time to send its rosters first.
	select {
	case <-stopped:
	case <-time.After(100 * time.Millisecond):
	}
	close(h.release)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("detach did not return")
	}
	select {
	case <-h.landed:
	case <-time.After(2 * time.Second):
		t.Fatal("the held join never reached the bus")
	}
	h.mu.Lock()
	order := append([]string(nil), h.order...)
	h.mu.Unlock()
	if len(order) == 0 || order[len(order)-1] != evRoster+"/0" {
		t.Fatalf("lane messages = %v, want the empty roster last", order)
	}
}
