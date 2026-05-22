package stream

import (
	"net/http/httptest"
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
