package stream

import (
	"context"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestSSEBrokerDefaultBuffer(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "test"})
	if broker.defaultBuf != 64 {
		t.Errorf("defaultBuf = %d, want 64", broker.defaultBuf)
	}
}

func TestSSEBrokerCustomBuffer(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "test", DefaultBuf: 128, MaxBuf: 512})
	if broker.defaultBuf != 128 {
		t.Errorf("defaultBuf = %d, want 128", broker.defaultBuf)
	}
}

func TestSSEBrokerPublishDeliversToSubscriber(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := NewSSEBroker(SSEBrokerConfig{Topic: "test", DefaultBuf: 16})

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/events", nil).WithContext(ctx)
			broker.Subscribe(w, r)
		}()

		// Wait for subscriber to register
		synctest.Wait()
		broker.Publish("message", "hello")
		synctest.Wait()

		if broker.SubscriberCount() != 1 {
			t.Errorf("SubscriberCount = %d, want 1", broker.SubscriberCount())
		}
		cancel()
		<-done
	})
}

func TestSSEBrokerBufferParamFromQuery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := NewSSEBroker(SSEBrokerConfig{Topic: "test"})
		ctx, cancel := context.WithCancel(context.Background())
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/events?buffer=256", nil).WithContext(ctx)

		go func() {
			time.Sleep(50 * time.Millisecond)
			broker.Publish("test", "data")
		}()

		done := make(chan struct{})
		go func() {
			broker.Subscribe(w, r)
			close(done)
		}()

		// Let the delayed publish fire and the subscriber consume it.
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		cancel()
		<-done
	})
}

func TestSSEBrokerPublishToMultipleSubscribers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := NewSSEBroker(SSEBrokerConfig{Topic: "test", DefaultBuf: 32})
		ctx, cancel := context.WithCancel(context.Background())

		for range 3 {
			go func() {
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "/events", nil).WithContext(ctx)
				broker.Subscribe(w, r)
			}()
		}

		synctest.Wait()
		if count := broker.SubscriberCount(); count != 3 {
			t.Errorf("SubscriberCount = %d, want 3", count)
		}

		broker.Publish("update", "payload")
		synctest.Wait()
		cancel()
	})
}

func TestSSEBrokerDropOnFullBuffer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := NewSSEBroker(SSEBrokerConfig{Topic: "test", DefaultBuf: 2})
		ctx, cancel := context.WithCancel(context.Background())

		// Subscriber that never reads, buffer fills immediately
		go func() {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/events?buffer=2", nil).WithContext(ctx)
			broker.Subscribe(w, r)
		}()

		synctest.Wait()

		// Publish more than buffer can hold, should not block
		for range 10 {
			broker.Publish("burst", strings.Repeat("x", 100))
		}
		// If we reach here, backpressure drop worked without blocking
		cancel()
	})
}

func TestSSEBrokerBackpressureDropsOldestAndKeepsLatest(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "test", DefaultBuf: 3})
	sub := &subscriber{ch: make(chan sseEvent, 3), done: make(chan struct{})}
	broker.mu.Lock()
	broker.subscribers["slow"] = sub
	broker.mu.Unlock()

	for i := range 10 {
		broker.Publish("burst", "payload", fmt.Sprintf("%d", i))
	}

	var got []string
	for len(sub.ch) > 0 {
		got = append(got, (<-sub.ch).ID)
	}
	want := []string{"7", "8", "9"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buffer retained IDs %v, want latest events %v", got, want)
	}
}

func TestSSEBrokerSlowBlockWaitsForBufferSpace(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "test", DefaultBuf: 2})
	sub := &subscriber{
		ch:       make(chan sseEvent, 2),
		done:     make(chan struct{}),
		slowMode: sseSlowBlock,
	}
	broker.mu.Lock()
	broker.subscribers["block"] = sub
	broker.mu.Unlock()

	broker.Publish("burst", "payload", "0")
	broker.Publish("burst", "payload", "1")

	published := make(chan struct{})
	go func() {
		broker.Publish("burst", "payload", "2")
		close(published)
	}()

	select {
	case <-published:
		t.Fatal("slow=block publish returned before buffer space was available")
	case <-time.After(25 * time.Millisecond):
	}

	if got := (<-sub.ch).ID; got != "0" {
		t.Fatalf("first buffered event = %q, want 0", got)
	}
	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("slow=block publish did not resume after buffer space opened")
	}

	var got []string
	for len(sub.ch) > 0 {
		got = append(got, (<-sub.ch).ID)
	}
	want := []string{"1", "2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buffer retained IDs %v, want %v", got, want)
	}
}

// Block mode is opt-in per broker. This test previously asserted that
// ANY request could select it; that contract was withdrawn because a
// block-mode subscriber stalls deliver(), and so every other
// subscriber plus the calling handler, which made it an
// unauthenticated DoS on a public endpoint. The request still chooses,
// but only within a broker whose host enabled AllowClientSlowMode.
func TestSSEBrokerSlowBlockParsedFromRequest(t *testing.T) {
	opted := NewSSEBroker(SSEBrokerConfig{Topic: "t", AllowClientSlowMode: true})

	req := httptest.NewRequest("GET", "/events?slow=block", nil)
	if got := opted.parseSlowMode(req); got != sseSlowBlock {
		t.Fatalf("query slow mode = %v, want block", got)
	}
	req = httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("X-SSE-Slow", "block")
	if got := opted.parseSlowMode(req); got != sseSlowBlock {
		t.Fatalf("header slow mode = %v, want block", got)
	}
	req = httptest.NewRequest("GET", "/events", nil)
	if got := opted.parseSlowMode(req); got != sseSlowDropOldest {
		t.Fatalf("default slow mode = %v, want drop-oldest", got)
	}
}

// A broker that did not opt in ignores the client's request entirely.
func TestSSESlowModeIgnoredWhenNotOptedIn(t *testing.T) {
	b := NewSSEBroker(SSEBrokerConfig{Topic: "t"})
	for _, target := range []string{"/events?slow=block", "/events"} {
		req := httptest.NewRequest("GET", target, nil)
		req.Header.Set("X-SSE-Slow", "block")
		if got := b.parseSlowMode(req); got != sseSlowDropOldest {
			t.Errorf("%s: got %v, want drop-oldest (client must not pick block mode)", target, got)
		}
	}
}

func TestSSEBrokerEventFilter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := NewSSEBroker(SSEBrokerConfig{Topic: "test"})
		ctx, cancel := context.WithCancel(context.Background())

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/events?event=alert", nil).WithContext(ctx)

		go func() {
			time.Sleep(50 * time.Millisecond)
			broker.Publish("info", "should-be-filtered")
			broker.Publish("alert", "should-pass")
		}()

		done := make(chan struct{})
		go func() {
			broker.Subscribe(w, r)
			close(done)
		}()

		// Let the delayed publishes land, then check the filter's work.
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		body := w.Body.String()
		if strings.Contains(body, "should-be-filtered") {
			t.Fatalf("filtered event leaked into stream: %q", body)
		}
		if !strings.Contains(body, "should-pass") {
			t.Fatalf("matching event missing from stream: %q", body)
		}
		cancel()
		<-done
	})
}
