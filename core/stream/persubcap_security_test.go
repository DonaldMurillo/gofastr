package stream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// Property: one principal may not hold unbounded concurrent SSE seats.
//
// Surfaces: sse_broker.go::Subscribe (the per-principal admission under
// b.mu — the 2026-09-05 round-4 fix), SSEBrokerConfig::MaxSeatsPerPrincipal
// (0 = the 16 default; negative lifts it) and SSEBrokerConfig::SeatOverflow
// ({SeatOverflowRefuse, 429 at connect — the default — |
// SeatOverflowEvictOldest}).
//
// Found red in the 2026-09-05 adversarial pass round 4: with the
// zero-value config (MaxSubscribers 0 = unlimited, no per-principal
// bound), one principal held 64 concurrent subscriptions, each a
// goroutine plus a buffered channel — a single low-privilege user could
// exhaust the process's goroutines/FDs/memory for every other user.

// TestSSEPerPrincipalSubscriberCap opens 64 concurrent streams for ONE
// principal under the zero-value config and asserts the per-principal
// bound: exactly defaultSeatsPerPrincipal seats held, the rest answered
// 429 at connect.
func TestSSEPerPrincipalSubscriberCap(t *testing.T) {
	b := NewSSEBroker(SSEBrokerConfig{
		Topic:     "t",
		Principal: func(r *http.Request) string { return r.Header.Get("X-User") },
		// HeartbeatInterval and MaxSubscribers left at zero-value
		// defaults: the config a host gets without opting in. The seat
		// cap still applies (0 = defaultSeatsPerPrincipal).
	})
	defer b.Close()

	const streams = 64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// codes receives each stream's HTTP status once its Subscribe call
	// RETURNS — only a refused stream returns before cancel, so exactly
	// the refusals arrive here, race-free (the recorder stays owned by
	// its own goroutine; the status crosses via the channel).
	codes := make(chan int, streams)
	for i := range streams {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/events", nil).
			WithContext(ctx)
		req.Header.Set("X-User", "user-1")
		req.URL.RawQuery = "subscriber_id=s" + strconv.Itoa(i)
		go func() {
			b.Subscribe(rec, req)
			codes <- rec.Code
		}()
	}
	waitForSubscribers(t, b, defaultSeatsPerPrincipal)

	if got := b.SubscriberCount(); got > defaultSeatsPerPrincipal {
		t.Errorf("SECURITY: [sse-seats] a single principal holds %d concurrent subscriber seats under the zero-value config (bound %d): %d goroutines and buffered channels are resident for ONE caller", got, defaultSeatsPerPrincipal, got)
	}
	if got := b.SubscriberCount(); got != defaultSeatsPerPrincipal {
		t.Errorf("principal holds %d seats, want exactly the %d cap", got, defaultSeatsPerPrincipal)
	}

	// The refused remainder is answered at connect (429), not left
	// holding an empty stream.
	var refused int
	deadline := time.After(3 * time.Second)
	for refused < streams-defaultSeatsPerPrincipal {
		select {
		case code := <-codes:
			refused++
			if code != http.StatusTooManyRequests {
				t.Errorf("seat-refused stream answered %d, want 429", code)
			}
		case <-deadline:
			t.Fatalf("only %d of %d streams were refused at connect", refused, streams-defaultSeatsPerPrincipal)
		}
	}

	// Disconnect accounting: every seat is released on cancel.
	cancel()
	waitUntil(t, func() bool { return b.SubscriberCount() == 0 })
	if got := b.SubscriberCount(); got != 0 {
		t.Errorf("registry kept %d subscribers after every stream disconnected", got)
	}
}

// TestSSEPerPrincipalCapEvictOldest pins the configurable overflow
// policy: past the cap the principal's OLDEST stream is closed and the
// new one seated; the count never exceeds the cap.
func TestSSEPerPrincipalCapEvictOldest(t *testing.T) {
	b := NewSSEBroker(SSEBrokerConfig{
		Topic:                "t",
		MaxSeatsPerPrincipal: 2,
		SeatOverflow:         SeatOverflowEvictOldest,
		Principal:            func(r *http.Request) string { return r.Header.Get("X-User") },
	})
	defer b.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	open := func(id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
		req.Header.Set("X-User", "user-1")
		req.URL.RawQuery = "subscriber_id=" + id
		go b.Subscribe(rec, req)
		return rec
	}

	open("a")
	waitForSubscribers(t, b, 1)
	open("b")
	waitForSubscribers(t, b, 2)
	open("c") // evicts a
	waitForSubscribers(t, b, 2)

	if got := b.SubscriberCount(); got != 2 {
		t.Errorf("principal holds %d seats under a cap of 2 with EvictOldest, want 2", got)
	}
	if b.SubscriberCount() > 2 {
		t.Errorf("SECURITY: [sse-seats] EvictOldest let the seat count reach %d past the cap of 2", b.SubscriberCount())
	}

	cancel()
	waitUntil(t, func() bool { return b.SubscriberCount() == 0 })
}

// waitUntil polls cond until it holds or the deadline passes.
func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition never held within timeout")
}
