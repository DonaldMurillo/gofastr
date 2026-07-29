package stream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Property: a key into a shared server-side map must not be
// attacker-chosen when a collision has a side effect on the incumbent.
//
// subscriber_id exists so apps can pass a meaningful id (user, tab,
// device). Because Subscribe evicts on a bare id match,
// `?subscriber_id=<victim>` closed the victim's done channel and
// dropped their SSE stream — repeatably, for a permanent denial.
// Eviction is now scoped to the same principal.
func TestSubscriberIDCannotEvictOther(t *testing.T) {
	b := NewSSEBroker(SSEBrokerConfig{Topic: "t"})
	defer b.Close()

	victimCtx, cancelVictim := context.WithCancel(context.Background())
	defer cancelVictim()
	victim := httptest.NewRequest("GET", "/events?subscriber_id=shared", nil).WithContext(victimCtx)
	victim.RemoteAddr = "10.0.0.1:5000"
	go b.Subscribe(httptest.NewRecorder(), victim)

	waitForSubscribers(t, b, 1)

	// A different caller asks for the same id.
	attackerCtx, cancelAttacker := context.WithCancel(context.Background())
	defer cancelAttacker()
	attacker := httptest.NewRequest("GET", "/events?subscriber_id=shared", nil).WithContext(attackerCtx)
	attacker.RemoteAddr = "10.0.0.99:6000"
	go b.Subscribe(httptest.NewRecorder(), attacker)

	waitForSubscribers(t, b, 2)

	// The victim must still be registered under its own id.
	b.mu.RLock()
	_, stillThere := b.subscribers["shared"]
	b.mu.RUnlock()
	if !stillThere {
		t.Fatal("attacker's subscriber_id evicted the incumbent's stream")
	}
}

// The cap rejects newcomers rather than evicting incumbents, so a
// reconnect loop cannot displace live streams.
func TestMaxSubscribersRejectsRatherThanEvicts(t *testing.T) {
	b := NewSSEBroker(SSEBrokerConfig{Topic: "t", MaxSubscribers: 1})
	defer b.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := httptest.NewRequest("GET", "/events?subscriber_id=a", nil).WithContext(ctx)
	first.RemoteAddr = "10.0.0.1:5000"
	go b.Subscribe(httptest.NewRecorder(), first)
	waitForSubscribers(t, b, 1)

	second := httptest.NewRequest("GET", "/events?subscriber_id=b", nil)
	second.RemoteAddr = "10.0.0.2:5000"
	rec := httptest.NewRecorder()
	b.Subscribe(rec, second) // returns immediately: over cap

	if rec.Code != 503 {
		t.Errorf("over-cap subscribe = %d, want 503", rec.Code)
	}
	if got := b.SubscriberCount(); got != 1 {
		t.Errorf("subscriber count = %d, want 1 (incumbent kept)", got)
	}
}

// The cap is exact. A reserved slot keyed on the requested subscriber id was
// tried and removed — nothing in the client runtime sends an id, so the
// reserve could never fire for a first-party client, while costing a scan of
// every subscriber under the write lock and letting the cap be exceeded by
// one. This pins the simpler contract that replaced it.
func TestSubscriberCapIsExact(t *testing.T) {
	b := NewSSEBroker(SSEBrokerConfig{
		Topic:             "t",
		MaxSubscribers:    1,
		HeartbeatInterval: time.Hour,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		req := httptest.NewRequest("GET", "/events?subscriber_id=a", nil).WithContext(ctx)
		b.Subscribe(newFlushRecorder(), req)
	}()
	waitForSubscribers(t, b, 1)

	// Same id, and a brand-new id: both are refused, and neither displaces
	// the incumbent.
	for _, q := range []string{"/events?subscriber_id=a", "/events?subscriber_id=zzz", "/events"} {
		rec := httptest.NewRecorder()
		b.Subscribe(rec, httptest.NewRequest("GET", q, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s at cap = %d, want 503", q, rec.Code)
		}
	}
	if got := b.SubscriberCount(); got != 1 {
		t.Errorf("subscriber count = %d, want exactly 1 — the cap must not be exceeded", got)
	}
}

// A half-open connection is reclaimed by the heartbeat, not by an eviction
// rule: the write fails, the stream unregisters itself, and the seat frees.
// This is what makes the cap survivable for a client whose previous
// connection died without the server noticing, and it works for every client
// rather than only ones that send a subscriber id.
func TestHeartbeatReclaimsDeadSubscriberSeat(t *testing.T) {
	b := NewSSEBroker(SSEBrokerConfig{
		Topic:             "t",
		MaxSubscribers:    1,
		HeartbeatInterval: 10 * time.Millisecond,
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		// A transport that is already gone: the first heartbeat write fails.
		b.Subscribe(&closedSSEWriter{header: http.Header{}},
			httptest.NewRequest("GET", "/events?subscriber_id=a", nil))
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a subscriber whose transport is dead was never retired by the heartbeat")
	}
	if got := b.SubscriberCount(); got != 0 {
		t.Fatalf("subscriber count = %d after the dead stream exited, want 0 — its seat is still held", got)
	}

	// The seat really is reusable.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		req := httptest.NewRequest("GET", "/events?subscriber_id=a", nil).WithContext(ctx)
		b.Subscribe(newFlushRecorder(), req)
	}()
	waitForSubscribers(t, b, 1)
}

type closedSSEWriter struct {
	header http.Header
}

func (w *closedSSEWriter) Header() http.Header       { return w.header }
func (w *closedSSEWriter) WriteHeader(int)           {}
func (w *closedSSEWriter) Write([]byte) (int, error) { return 0, http.ErrHandlerTimeout }
func (w *closedSSEWriter) Flush()                    {}

func waitForSubscribers(t *testing.T, b *SSEBroker, want int) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if b.SubscriberCount() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d subscribers (have %d)", want, b.SubscriberCount())
}
