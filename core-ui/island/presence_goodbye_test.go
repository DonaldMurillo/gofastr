package island

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

func TestFanoutStopContextAlreadyExpired(t *testing.T) {
	m := NewManager()
	stop, err := m.SetFanoutContext(fanout.NewInProcess())
	if err != nil {
		t.Fatal(err)
	}
	if err := stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	for range 64 {
		if err := stop(expired); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("stop with an expired context = %v, want context.DeadlineExceeded", err)
		}
	}
}

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

type cancelGoodbyeFanout struct {
	*holdRoster
	goodbye chan struct{}
}

func (f *cancelGoodbyeFanout) Publish(ctx context.Context, topic string, payload []byte) error {
	if topic == presenceFanoutTopic {
		_, body, err := fanout.Unwrap(payload)
		var msg presenceFanoutMsg
		if err == nil && json.Unmarshal(body, &msg) == nil && len(msg.Members) == 0 {
			close(f.goodbye)
			<-ctx.Done()
			return ctx.Err()
		}
	}
	return f.holdRoster.Publish(ctx, topic, payload)
}

type firstDoneGate struct {
	context.Context
	entered, release chan struct{}
	once             sync.Once
}

func (c *firstDoneGate) Done() <-chan struct{} {
	first := false
	c.once.Do(func() { first = true })
	if first {
		close(c.entered)
		<-c.release
	}
	return c.Context.Done()
}

func TestFanoutStopReportsInterruptedGoodbye(t *testing.T) {
	wait, stopWaiting := context.WithTimeout(t.Context(), 2*time.Second)
	defer stopWaiting()
	await := func(ch <-chan struct{}, what string) {
		t.Helper()
		select {
		case <-ch:
		case <-wait.Done():
			t.Fatalf("timed out waiting for %s", what)
		}
	}
	f := &cancelGoodbyeFanout{
		holdRoster: &holdRoster{Fanout: fanout.NewInProcess(), entered: make(chan struct{}), release: make(chan struct{}), landed: make(chan struct{})},
		goodbye:    make(chan struct{}),
	}
	releaseRoster := sync.OnceFunc(func() { close(f.holdRoster.release) })
	defer releaseRoster()
	m := NewManager()
	stop, err := m.SetFanoutContext(f)
	if err != nil {
		t.Fatal(err)
	}
	joined := m.PresenceJoin("session", PresenceIdentity{UserID: "user"}, []string{"doc:1"})
	defer joined.Leave()
	await(f.holdRoster.entered, "the queued roster")

	ctx, cancel := context.WithCancel(wait)
	defer cancel()
	gate := &firstDoneGate{Context: ctx, entered: make(chan struct{}), release: make(chan struct{})}
	releaseSelect := sync.OnceFunc(func() { close(gate.release) })
	defer releaseSelect()
	result := make(chan error, 1)
	go func() { result <- stop(gate) }()
	await(gate.entered, "the stop waiter to evaluate ctx.Done")
	releaseRoster()
	await(f.goodbye, "the goodbye Publish")
	cancel()

	// Let detach finish before the first waiter can select, making both cases ready.
	detachErr := stop(wait)
	releaseSelect()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("stop = %v, want context.Canceled", err)
		}
	case <-wait.Done():
		t.Fatal("stop did not return")
	}
	if !errors.Is(detachErr, context.Canceled) {
		t.Errorf("detach result = %v, want context.Canceled", detachErr)
	}
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
