package stream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Finding 1: Subscribe goroutine leaks; ctx.Done() never observed.
func TestSSESubscribeExitsOnCtxDone(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "t", DefaultBuf: 4})

	ctx, cancel := context.WithCancel(context.Background())
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/events?subscriber_id=ctx-test", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		broker.Subscribe(w, r)
		close(done)
	}()

	// Wait for registration
	deadline := time.Now().Add(500 * time.Millisecond)
	for broker.SubscriberCount() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Subscribe did not return within 500ms of ctx cancel")
	}

	if got := broker.SubscriberCount(); got != 0 {
		t.Fatalf("SubscriberCount after cancel = %d, want 0", got)
	}
}

// Finding 5a: MaxBuf must clamp client-requested buffer size, AND must
// honor in-bounds requests verbatim.
func TestSSEBufferClampedToMax(t *testing.T) {
	t.Run("oversize clamped", func(t *testing.T) {
		broker := NewSSEBroker(SSEBrokerConfig{Topic: "t", DefaultBuf: 8, MaxBuf: 32})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		r := httptest.NewRequest("GET", "/events?buffer=99999&subscriber_id=clamp-test", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		done := make(chan struct{})
		go func() {
			broker.Subscribe(w, r)
			close(done)
		}()

		deadline := time.Now().Add(500 * time.Millisecond)
		for broker.SubscriberCount() != 1 {
			if time.Now().After(deadline) {
				t.Fatal("subscriber never registered")
			}
			time.Sleep(5 * time.Millisecond)
		}

		broker.mu.RLock()
		sub := broker.subscribers["clamp-test"]
		broker.mu.RUnlock()
		if sub == nil {
			t.Fatal("subscriber not found")
		}
		if cap(sub.ch) > 32 {
			t.Fatalf("buffer cap = %d, want <= MaxBuf 32", cap(sub.ch))
		}

		cancel()
		<-done
	})

	// In-bounds request must NOT be clamped to DefaultBuf — caller
	// should get exactly what they asked for when it sits within
	// [DefaultBuf, MaxBuf].
	t.Run("in-bounds honored", func(t *testing.T) {
		broker := NewSSEBroker(SSEBrokerConfig{Topic: "t", DefaultBuf: 8, MaxBuf: 32})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		r := httptest.NewRequest("GET", "/events?buffer=16&subscriber_id=in-bounds", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		done := make(chan struct{})
		go func() {
			broker.Subscribe(w, r)
			close(done)
		}()

		deadline := time.Now().Add(500 * time.Millisecond)
		for broker.SubscriberCount() != 1 {
			if time.Now().After(deadline) {
				t.Fatal("subscriber never registered")
			}
			time.Sleep(5 * time.Millisecond)
		}

		broker.mu.RLock()
		sub := broker.subscribers["in-bounds"]
		broker.mu.RUnlock()
		if sub == nil {
			t.Fatal("subscriber not found")
		}
		if cap(sub.ch) != 16 {
			t.Fatalf("buffer cap = %d, want exactly 16 (in-bounds request must be honored verbatim)", cap(sub.ch))
		}

		cancel()
		<-done
	})
}

// Finding 5b: oversize subscriber_id must be rejected/truncated.
func TestSSESubscriberIDLengthCap(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "t"})
	long := strings.Repeat("x", 1024)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("GET", "/events?subscriber_id="+long, nil).WithContext(ctx)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		broker.Subscribe(w, r)
		close(done)
	}()

	// Subscriber must not register with full 1024-char id; either rejected (no subscriber)
	// or truncated to <=maxSubscriberID
	time.Sleep(50 * time.Millisecond)
	broker.mu.RLock()
	count := len(broker.subscribers)
	var key string
	for k := range broker.subscribers {
		key = k
		break
	}
	broker.mu.RUnlock()

	if count > 0 && len(key) > maxSubscriberID {
		t.Fatalf("subscriber id length = %d, exceeds cap %d", len(key), maxSubscriberID)
	}
	cancel()
	<-done
}

// Finding 5c: subscribe with duplicate id should not orphan previous subscriber.
func TestSSESubscriberIDCollision(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "t"})

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	r1 := httptest.NewRequest("GET", "/events?subscriber_id=dup", nil).WithContext(ctx1)
	w1 := httptest.NewRecorder()
	done1 := make(chan struct{})
	go func() {
		broker.Subscribe(w1, r1)
		close(done1)
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for broker.SubscriberCount() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("first subscriber never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	r2 := httptest.NewRequest("GET", "/events?subscriber_id=dup", nil).WithContext(ctx2)
	w2 := httptest.NewRecorder()
	done2 := make(chan struct{})
	go func() {
		broker.Subscribe(w2, r2)
		close(done2)
	}()

	// With no Config.Principal the broker cannot tell the two callers
	// apart, so it evicts NOTHING: the newcomer is registered under a
	// freshly generated id and the incumbent keeps streaming. The
	// original concern still holds — neither subscriber may be silently
	// leaked — so assert both are registered, and that both unregister
	// when their requests end.
	deadline = time.Now().Add(500 * time.Millisecond)
	for broker.SubscriberCount() != 2 {
		if time.Now().After(deadline) {
			t.Fatalf("second subscriber never registered: count = %d", broker.SubscriberCount())
		}
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-done1:
		t.Fatal("incumbent was evicted by a caller the broker cannot prove is the same")
	case <-time.After(50 * time.Millisecond):
	}

	cancel1()
	cancel2()
	<-done1
	<-done2
	if got := broker.SubscriberCount(); got != 0 {
		t.Fatalf("subscribers leaked after both requests ended: count = %d", got)
	}
}

// With a Principal the broker CAN tell callers apart, so the eviction
// subscriber_id was designed for comes back: a reconnect replaces its own
// entry, and a different caller asking for that id does not.
func TestSSEEvictionScopedByPrincipal(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{
		Topic:     "t",
		Principal: func(r *http.Request) string { return r.Header.Get("X-User") },
	})

	start := func(user string) (chan struct{}, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		r := httptest.NewRequest("GET", "/events?subscriber_id=dup", nil).WithContext(ctx)
		r.Header.Set("X-User", user)
		done := make(chan struct{})
		go func() {
			broker.Subscribe(httptest.NewRecorder(), r)
			close(done)
		}()
		return done, cancel
	}
	waitCount := func(want int) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for broker.SubscriberCount() != want {
			if time.Now().After(deadline) {
				t.Fatalf("subscriber count = %d, want %d", broker.SubscriberCount(), want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	alice1, cancelA1 := start("alice")
	defer cancelA1()
	waitCount(1)

	// A different principal must NOT displace alice.
	bob, cancelB := start("bob")
	defer cancelB()
	waitCount(2)
	select {
	case <-alice1:
		t.Fatal("bob evicted alice's stream by guessing her subscriber_id")
	case <-time.After(50 * time.Millisecond):
	}

	// Alice reconnecting with the same id replaces her own entry.
	alice2, cancelA2 := start("alice")
	defer cancelA2()
	select {
	case <-alice1:
	case <-time.After(time.Second):
		t.Fatal("alice's reconnect did not replace her own entry")
	}
	waitCount(2)

	cancelA2()
	cancelB()
	<-alice2
	<-bob
}

// Finding 11: Publish must not deadlock or panic under concurrent fan-out
// even when subscribers are slow readers.
func TestSSEPublishConcurrentSafe(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "t", DefaultBuf: 4})

	const n = 8
	cancels := make([]context.CancelFunc, n)
	dones := make([]chan struct{}, n)
	for i := range n {
		ctx, cancel := context.WithCancel(context.Background())
		cancels[i] = cancel
		r := httptest.NewRequest("GET", "/events", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		done := make(chan struct{})
		dones[i] = done
		go func() {
			broker.Subscribe(w, r)
			close(done)
		}()
	}

	deadline := time.Now().Add(time.Second)
	for broker.SubscriberCount() < n {
		if time.Now().After(deadline) {
			t.Fatalf("only %d/%d registered", broker.SubscriberCount(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 200 {
				broker.Publish("burst", "x")
			}
		})
	}

	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("publishers blocked under concurrent fan-out")
	}

	for _, c := range cancels {
		c()
	}
	for _, d := range dones {
		<-d
	}
}

// Finding 17: Heartbeat comment frame must be emitted within HeartbeatInterval.
func TestSSEHeartbeatEmits(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{
		Topic:             "t",
		HeartbeatInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("GET", "/events", nil).WithContext(ctx)
	rec := &syncRecorder{rr: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		broker.Subscribe(rec, r)
		close(done)
	}()

	// Wait for at least one heartbeat to be emitted
	deadline := time.Now().Add(time.Second)
	for {
		body := rec.snapshot()
		if strings.Contains(body, ":\n\n") || strings.Contains(body, ": heartbeat") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no heartbeat written in 1s; body=%q", body)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	<-done
}

// syncRecorder wraps httptest.ResponseRecorder with a mutex so test
// goroutines can safely read the buffer while Subscribe writes to it.
type syncRecorder struct {
	mu  sync.Mutex
	rr  *httptest.ResponseRecorder
	buf strings.Builder
}

func (s *syncRecorder) Header() http.Header { return s.rr.Header() }
func (s *syncRecorder) WriteHeader(code int) {
	s.rr.WriteHeader(code)
}
func (s *syncRecorder) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.Write(p)
	return len(p), nil
}
func (s *syncRecorder) Flush() {}
func (s *syncRecorder) snapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// Finding 21: generated subscriber IDs must be unguessable (hex, length).
func TestSSEGeneratedIDUnguessable(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for range 100 {
		id := generateSubscriberID()
		if len(id) < 16 {
			t.Fatalf("generated id %q length %d < 16", id, len(id))
		}
		// must be hex
		for _, c := range id {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Fatalf("non-hex char %q in id %q", c, id)
			}
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = struct{}{}
	}
}

// Stress regression to keep race detector happy in concurrent fan-out.
var _ = atomic.Int64{}
