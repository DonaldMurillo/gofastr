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

	// Either:
	//  - the second subscribe is rejected (done2 returns quickly, count stays 1)
	//  - the first subscribe is cleanly evicted (done1 returns, count == 1 for second)
	// In both cases, the previous subscriber must NOT be silently leaked.
	select {
	case <-done1:
		// previous was cleanly evicted — good
	case <-done2:
		// new one was rejected — also good
	case <-time.After(500 * time.Millisecond):
		t.Fatal("collision caused orphan: neither subscriber returned")
	}
}

// Finding 11: Publish must not deadlock or panic under concurrent fan-out
// even when subscribers are slow readers.
func TestSSEPublishConcurrentSafe(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "t", DefaultBuf: 4})

	const n = 8
	cancels := make([]context.CancelFunc, n)
	dones := make([]chan struct{}, n)
	for i := 0; i < n; i++ {
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
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				broker.Publish("burst", "x")
			}
		}()
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
	for i := 0; i < 100; i++ {
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
