package event_test

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

type gateFanout struct {
	gate    chan struct{}
	entered chan struct{}
	once    sync.Once
	mu      sync.Mutex
	got     []string
}

func (g *gateFanout) Publish(_ context.Context, _ string, p []byte) error {
	g.once.Do(func() { close(g.entered) })
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
	f := &gateFanout{gate: make(chan struct{}), entered: make(chan struct{})}
	bus := event.NewEventBus()
	stop, err := event.AttachFanout(bus, f)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	ctx := context.Background()
	_ = bus.Emit(ctx, event.Event{Type: "t", Data: 0})
	select {
	case <-f.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the publisher never took the first envelope")
	}
	for i := 1; i < 400; i++ {
		_ = bus.Emit(ctx, event.Event{Type: "t", Data: i})
	}
	close(f.gate)
	want := []int{0}
	for i := 400 - 256; i < 400; i++ {
		want = append(want, i)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		n := len(f.got)
		f.mu.Unlock()
		if n >= len(want) || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	var got []int
	for _, raw := range f.got {
		_, body, err := fanout.Unwrap([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		var ev struct{ Data int }
		if err := json.Unmarshal(body, &ev); err != nil {
			t.Fatal(err)
		}
		got = append(got, ev.Data)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("published %d events %v, want the in-flight 0 and the newest 256 (144..399)", len(got), got)
	}
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
