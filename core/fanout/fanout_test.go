package fanout

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recv collects payloads from a subscription into a slice under a mutex.
type recv struct {
	mu  sync.Mutex
	got [][]byte
}

func (r *recv) add(p []byte) {
	r.mu.Lock()
	r.got = append(r.got, append([]byte(nil), p...))
	r.mu.Unlock()
}

func (r *recv) snapshot() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]byte, len(r.got))
	copy(out, r.got)
	return out
}

func (r *recv) waitN(t *testing.T, n int, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(r.snapshot()) >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("%s: wanted %d payloads, got %d", msg, n, len(r.snapshot()))
}

// --- envelope helpers ---

func TestNewNodeID(t *testing.T) {
	t.Parallel()
	a := NewNodeID()
	b := NewNodeID()
	if len(a) != 32 {
		t.Fatalf("NewNodeID len = %d, want 32 hex chars", len(a))
	}
	if a == b {
		t.Fatal("NewNodeID produced two identical ids (expected random)")
	}
}

func TestWrapUnwrapRoundTrip(t *testing.T) {
	t.Parallel()
	body := []byte(`{"hello":"world"}`)
	wrapped := Wrap("node-A", body)
	if bytes.Equal(wrapped, body) {
		t.Fatal("Wrap returned body unchanged (not enveloped)")
	}
	nodeID, got, err := Unwrap(wrapped)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if nodeID != "node-A" {
		t.Errorf("nodeID = %q, want %q", nodeID, "node-A")
	}
	if !bytes.Equal(got, body) {
		t.Errorf("body = %q, want %q", got, body)
	}
}

func TestUnwrapInvalid(t *testing.T) {
	t.Parallel()
	if _, _, err := Unwrap([]byte("not json")); err == nil {
		t.Fatal("Unwrap accepted non-json input")
	}
	if _, _, err := Unwrap([]byte(`{}`)); err == nil {
		t.Fatal("Unwrap accepted envelope without node id")
	}
}

func TestWrapIsJSON(t *testing.T) {
	t.Parallel()
	// Envelope must be JSON so receivers can decode it without a custom
	// binary framing convention; "n" and "b" are the documented keys.
	wrapped := Wrap("n1", []byte("payload"))
	if !strings.Contains(string(wrapped), `"n"`) {
		t.Fatalf("envelope missing n key: %s", wrapped)
	}
	if !strings.Contains(string(wrapped), `"b"`) {
		t.Fatalf("envelope missing b key: %s", wrapped)
	}
}

// --- InProcess ---

func TestInProcessSubscribePublish(t *testing.T) {
	t.Parallel()
	f := NewInProcess()
	r := &recv{}
	cancel, err := f.Subscribe("t", r.add)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if err := f.Publish(context.Background(), "t", []byte("hello")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	r.waitN(t, 1, "single deliver")
}

func TestInProcessTwoSubscribers(t *testing.T) {
	t.Parallel()
	f := NewInProcess()
	a, b := &recv{}, &recv{}
	ca, _ := f.Subscribe("t", a.add)
	defer ca()
	cb, _ := f.Subscribe("t", b.add)
	defer cb()

	if err := f.Publish(context.Background(), "t", []byte("m1")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	a.waitN(t, 1, "a deliver")
	b.waitN(t, 1, "b deliver")
}

func TestInProcessTopicIsolation(t *testing.T) {
	t.Parallel()
	f := NewInProcess()
	other := &recv{}
	f.Subscribe("other", other.add)

	got := make(chan []byte, 4)
	cancel, _ := f.Subscribe("mine", func(p []byte) { got <- p })
	defer cancel()

	f.Publish(context.Background(), "other", []byte("nope"))
	f.Publish(context.Background(), "mine", []byte("yes"))

	select {
	case p := <-got:
		if string(p) != "yes" {
			t.Fatalf("got %q, want yes", p)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mine deliver")
	}
}

func TestInProcessOrderedDelivery(t *testing.T) {
	t.Parallel()
	f := NewInProcess()
	var (
		mu   sync.Mutex
		got  []int
		stop = make(chan struct{})
	)
	// Use a slow consumer that blocks briefly to prove ordering survives
	// queueing: messages must arrive in publish order per subscriber.
	cancel, _ := f.Subscribe("ord", func(p []byte) {
		// append under lock; the order here is the assertion.
		mu.Lock()
		got = append(got, int(p[0]))
		mu.Unlock()
	})
	defer cancel()
	_ = stop

	for i := 0; i < 20; i++ {
		if err := f.Publish(context.Background(), "ord", []byte{byte(i)}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}
	// wait for all 20
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 20 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 20 {
		t.Fatalf("got %d messages, want 20", len(got))
	}
	for i, v := range got {
		if v != i {
			t.Fatalf("out of order at %d: got %d, want %d (full=%v)", i, v, i, got)
		}
	}
}

func TestInProcessCancelStopsDelivery(t *testing.T) {
	t.Parallel()
	f := NewInProcess()
	var n atomic.Int64
	cancel, _ := f.Subscribe("t", func([]byte) { n.Add(1) })
	cancel()

	if err := f.Publish(context.Background(), "t", []byte("x")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if n.Load() != 0 {
		t.Fatalf("received after cancel: %d", n.Load())
	}
}

// TestInProcessOverflowDropOldest: a subscriber whose bounded queue fills
// (because it never drains) must drop the oldest queued message, not block
// the publisher — matching SSEBroker's lossy contract.
func TestInProcessOverflowDropOldest(t *testing.T) {
	t.Parallel()
	// Tiny queue so the overflow path is exercised quickly.
	f := NewInProcess(WithInProcessQueue(2))
	// Subscriber that never drains.
	cancel, _ := f.Subscribe("t", func([]byte) {
		time.Sleep(time.Hour)
	})
	defer cancel()

	// Publish far more than the queue can hold; none of these should block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			_ = f.Publish(context.Background(), "t", []byte{byte(i)})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full subscriber queue (should drop-oldest)")
	}
}

// --- Redis adapter ---

// fakeRedis is an in-memory implementation of RedisPubSub used to exercise
// the NewRedis adapter without a real Redis dependency.
type fakeRedis struct {
	mu         sync.Mutex
	channels   map[string][]func([]byte)
	publishErr error
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{channels: map[string][]func([]byte){}}
}

func (r *fakeRedis) Publish(_ context.Context, channel string, payload []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.publishErr != nil {
		return r.publishErr
	}
	for _, fn := range r.channels[channel] {
		fn(append([]byte(nil), payload...))
	}
	return nil
}

func (r *fakeRedis) Subscribe(_ context.Context, channel string, fn func([]byte)) (func(), error) {
	r.mu.Lock()
	r.channels[channel] = append(r.channels[channel], fn)
	idx := len(r.channels[channel]) - 1
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.channels[channel][idx] = nil
	}, nil
}

func TestRedisAdapterSubscribePublish(t *testing.T) {
	t.Parallel()
	redis := newFakeRedis()
	f := NewRedis(redis)
	r := &recv{}
	cancel, err := f.Subscribe("t", r.add)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if err := f.Publish(context.Background(), "t", []byte("hi")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	r.waitN(t, 1, "redis deliver")
}

func TestRedisAdapterPublishError(t *testing.T) {
	t.Parallel()
	redis := newFakeRedis()
	boom := errors.New("redis down")
	redis.publishErr = boom
	f := NewRedis(redis)
	if err := f.Publish(context.Background(), "t", []byte("x")); !errors.Is(err, boom) {
		t.Fatalf("Publish err = %v, want %v", err, boom)
	}
}

// TestRedisAdapterSlowSubDoesNotStallOthers: a blocking subscriber must not
// stall delivery to another subscriber on the same topic. The adapter wraps
// each subscriber in a bounded queue, so the Redis reader's synchronous
// fan-out is never blocked by one slow callback (the per-subscriber
// bounded-queue + drop-oldest contract the Fanout interface promises).
func TestRedisAdapterSlowSubDoesNotStallOthers(t *testing.T) {
	t.Parallel()
	redis := newFakeRedis()
	f := NewRedis(redis)

	block := make(chan struct{})
	t.Cleanup(func() { close(block) }) // unblock the slow subscriber on exit
	cancelA, _ := f.Subscribe("t", func([]byte) { <-block })
	defer cancelA()

	got := make(chan []byte, 1)
	cancelB, _ := f.Subscribe("t", func(p []byte) {
		select {
		case got <- p:
		default:
		}
	})
	defer cancelB()

	// Publish in a goroutine: under the bug the reader calls A's callback
	// inline and blocks, so B never receives and this times out.
	go func() { _ = f.Publish(context.Background(), "t", []byte("ping")) }()
	select {
	case p := <-got:
		if string(p) != "ping" {
			t.Fatalf("got %q, want ping", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber B stalled by subscriber A's blocking callback (reader not queue-isolated)")
	}
}
