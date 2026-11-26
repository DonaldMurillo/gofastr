package island

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

func TestFanoutStopContextAlreadyExpired(t *testing.T) {
	h := &holdRoster{Fanout: fanout.NewInProcess(), entered: make(chan struct{}), release: make(chan struct{}), landed: make(chan struct{})}
	close(h.release)
	m := NewManager()
	stop, err := m.SetFanoutContext(h)
	if err != nil {
		t.Fatal(err)
	}
	joined := m.PresenceJoin("session", PresenceIdentity{UserID: "user"}, []string{"doc:1"})
	defer joined.Leave()
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	for range 64 {
		if err := stop(expired); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("stop with an expired context = %v, want context.DeadlineExceeded", err)
		}
	}
	if err := stop(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a later stop = %v, want the first stop's context.DeadlineExceeded", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, n := range h.order {
		if n == 0 {
			t.Fatalf("presence messages by member count = %v, want no goodbye after the context ended", h.order)
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
	result := make(chan error, 1)
	go func() { result <- stop(ctx) }()
	releaseRoster()
	await(f.goodbye, "the goodbye Publish")
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "island fanout stop interrupted") {
			t.Errorf("stop = %v, want island fanout stop interrupted wrapping context.Canceled", err)
		}
	case <-wait.Done():
		t.Fatal("stop did not return")
	}
}

// unsubscribeSignal reports when the island and presence subscriptions
// are cancelled.
type unsubscribeSignal struct {
	fanout.Fanout
	island, presence chan struct{}
}

func (f *unsubscribeSignal) Subscribe(topic string, fn func([]byte)) (func(), error) {
	cancel, err := f.Fanout.Subscribe(topic, fn)
	if err != nil {
		return cancel, err
	}
	ch := f.island
	if topic == presenceFanoutTopic {
		ch = f.presence
	}
	return func() {
		cancel()
		close(ch)
	}, nil
}

// A stop held up by a stuck roster Publish still unsubscribes first, so
// a peer's island update no longer reaches local sessions while the
// abandoned stop waits on the transport.
func TestFanoutStopUnsubscribesBeforeQueuedRosters(t *testing.T) {
	bus := fanout.NewInProcess()
	h := &holdRoster{Fanout: bus, entered: make(chan struct{}), release: make(chan struct{}), landed: make(chan struct{})}
	f := &unsubscribeSignal{Fanout: h, island: make(chan struct{}), presence: make(chan struct{})}
	m := NewManager()
	stop, err := m.SetFanoutContext(f)
	if err != nil {
		t.Fatal(err)
	}
	ch, unsubscribe := m.Subscribe("session")
	defer unsubscribe()
	joined := m.PresenceJoin("session", PresenceIdentity{UserID: "user"}, []string{"doc:1"})
	defer joined.Leave()
	select {
	case <-h.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the join's roster never reached Publish")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- stop(context.Background()) }()
	defer func() {
		close(h.release)
		select {
		case err := <-stopped:
			if err != nil {
				t.Errorf("stop = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("stop did not return after the roster was released")
		}
	}()
	for _, c := range []struct {
		name string
		ch   chan struct{}
	}{{"island", f.island}, {"presence", f.presence}} {
		select {
		case <-c.ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("the %s subscription is still attached while a roster Publish is stuck", c.name)
		}
	}
	body, err := json.Marshal(islandFanoutMsg{SessionID: "session", IslandID: "isl", HTML: "<p>late</p>"})
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(context.Background(), islandFanoutTopic, fanout.Wrap("peer", body)); err != nil {
		t.Fatal(err)
	}
	select {
	case upd := <-ch:
		t.Fatalf("a peer update reached the local session after stop began: %+v", upd)
	case <-time.After(50 * time.Millisecond):
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
