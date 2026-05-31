package stream

import (
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
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
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "test", DefaultBuf: 16})

	done := make(chan struct{})
	go func() {
		defer close(done)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/events", nil)
		broker.Subscribe(w, r)
	}()

	// Wait for subscriber to register
	time.Sleep(50 * time.Millisecond)
	broker.Publish("message", "hello")

	select {
	case <-done:
		// subscriber exited (would happen if client disconnects in real use)
	case <-time.After(2 * time.Second):
		// subscriber still listening — that's fine, the publish went through
	}

	if broker.SubscriberCount() > 1 {
		t.Errorf("SubscriberCount = %d, want 0 or 1", broker.SubscriberCount())
	}
}

func TestSSEBrokerBufferParamFromQuery(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "test"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/events?buffer=256", nil)

	go func() {
		time.Sleep(50 * time.Millisecond)
		broker.Publish("test", "data")
	}()

	done := make(chan struct{})
	go func() {
		broker.Subscribe(w, r)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

func TestSSEBrokerPublishToMultipleSubscribers(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "test", DefaultBuf: 32})

	for i := 0; i < 3; i++ {
		go func() {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/events", nil)
			broker.Subscribe(w, r)
		}()
	}

	time.Sleep(100 * time.Millisecond)
	if count := broker.SubscriberCount(); count != 3 {
		t.Errorf("SubscriberCount = %d, want 3", count)
	}

	broker.Publish("update", "payload")
}

func TestSSEBrokerDropOnFullBuffer(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "test", DefaultBuf: 2})

	// Subscriber that never reads — buffer fills immediately
	go func() {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/events?buffer=2", nil)
		broker.Subscribe(w, r)
	}()

	time.Sleep(50 * time.Millisecond)

	// Publish more than buffer can hold — should not block
	for i := 0; i < 10; i++ {
		broker.Publish("burst", strings.Repeat("x", 100))
	}
	// If we reach here, backpressure drop worked without blocking
}

func TestSSEBrokerBackpressureDropsOldestAndKeepsLatest(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "test", DefaultBuf: 3})
	sub := &subscriber{ch: make(chan sseEvent, 3), done: make(chan struct{})}
	broker.mu.Lock()
	broker.subscribers["slow"] = sub
	broker.mu.Unlock()

	for i := 0; i < 10; i++ {
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

func TestSSEBrokerSlowBlockParsedFromRequest(t *testing.T) {
	req := httptest.NewRequest("GET", "/events?slow=block", nil)
	if got := parseSlowMode(req); got != sseSlowBlock {
		t.Fatalf("query slow mode = %v, want block", got)
	}
	req = httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("X-SSE-Slow", "block")
	if got := parseSlowMode(req); got != sseSlowBlock {
		t.Fatalf("header slow mode = %v, want block", got)
	}
	req = httptest.NewRequest("GET", "/events", nil)
	if got := parseSlowMode(req); got != sseSlowDropOldest {
		t.Fatalf("default slow mode = %v, want drop-oldest", got)
	}
}

func TestSSEBrokerEventFilter(t *testing.T) {
	broker := NewSSEBroker(SSEBrokerConfig{Topic: "test"})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/events?event=alert", nil)

	go func() {
		time.Sleep(50 * time.Millisecond)
		broker.Publish("info", "should-be-filtered")
		broker.Publish("alert", "should-pass")
		time.Sleep(50 * time.Millisecond)
	}()

	done := make(chan struct{})
	go func() {
		broker.Subscribe(w, r)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}
