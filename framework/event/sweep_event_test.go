package event_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

type gateFanout struct {
	gate chan struct{}
	mu   sync.Mutex
	got  []string
}

func (g *gateFanout) Publish(_ context.Context, _ string, p []byte) error {
	<-g.gate
	g.mu.Lock()
	g.got = append(g.got, string(p))
	g.mu.Unlock()
	return nil
}

func (g *gateFanout) Subscribe(string, func([]byte)) (func(), error) { return func() {}, nil }

func TestAttachFanoutRefusesNil(t *testing.T) {
	if stop, err := event.AttachFanout(event.NewEventBus(), nil); err == nil || stop != nil {
		t.Fatalf("AttachFanout(nil) = %v, %v; want an error and no stop", stop != nil, err)
	}
}

func TestBridgeOverflowDropsOldest(t *testing.T) {
	f := &gateFanout{gate: make(chan struct{})}
	bus := event.NewEventBus()
	stop, err := event.AttachFanout(bus, f)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 400 {
		_ = bus.Emit(context.Background(), event.Event{Type: "t", Data: i})
	}
	close(f.gate)
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		last := ""
		if len(f.got) > 0 {
			last = f.got[len(f.got)-1]
		}
		f.mu.Unlock()
		if strings.Contains(last, `:399,`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("newest event never published after the queue overflowed; last published %q", last)
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
}

func TestEmitStampsAZeroTimestamp(t *testing.T) {
	bus := event.NewEventBus()
	seen := make(chan time.Time, 3)
	bus.On("t", func(_ context.Context, e event.Event) error { seen <- e.Timestamp; return nil })
	_ = bus.Emit(context.Background(), event.Event{Type: "t"})
	_ = bus.EmitStrict(context.Background(), event.Event{Type: "t"})
	bus.EmitAsync(context.Background(), event.Event{Type: "t"})
	for range 3 {
		select {
		case ts := <-seen:
			if ts.IsZero() {
				t.Fatal("a handler saw a zero Timestamp")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("handler not called")
		}
	}
}
