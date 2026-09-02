package fanout

// Adversarial tests for the PostgresFanout surfaces that sit BEFORE the
// transport: input validation, registration state machine, and the
// slow-subscriber isolation of the dispatch path. These run without a live
// Postgres — any transport touch on a zero-value fanout's nil db would
// nil-pointer, which is exactly what makes "validated before transport"
import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// ---------------------------------------------------------------------------
// Property: Publish rejects a closed fanout and non-UTF-8 topic/payload
// BEFORE any transport write. Postgres would otherwise silently substitute
// U+FFFD for invalid bytes, corrupting the broadcast for every replica. The
// zero-value fanout has a nil db: if validation ever ran after the transport
// call this test dies on a nil-pointer instead of passing.
// ---------------------------------------------------------------------------

func TestPublishValidatesBeforeTransport(t *testing.T) {
	cases := []struct {
		name    string
		topic   string
		payload []byte
	}{
		{"invalid utf8 topic", "bad\xfftopic", []byte(`{}`)},
		{"invalid utf8 payload", "orders", []byte("{'a': '\xff'}")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &PostgresFanout{} // nil db: any transport touch panics
			var err error
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("SECURITY: [fanout] Publish(%q) reached the transport with invalid input (nil-db panic): %v", tc.topic, r)
					}
				}()
				err = p.Publish(context.Background(), tc.topic, tc.payload)
			}()
			if err == nil {
				t.Errorf("SECURITY: [fanout] Publish accepted %q (would be corrupted to U+FFFD by the transport)", tc.name)
			}
		})
	}

	p := &PostgresFanout{}
	p.closed.Store(true)
	if err := p.Publish(context.Background(), "orders", []byte(`{}`)); err == nil {
		t.Error("SECURITY: [fanout] Publish on a closed fanout returned nil, want an error")
	}
}

// ---------------------------------------------------------------------------
// Property: Subscribe refuses the registrations that can never be matched
// or delivered — a nil callback (uninvokable) and a non-UTF-8 topic (Publish
// rejects its mirror, so the subscriber would be a silent black hole).
// ---------------------------------------------------------------------------

func TestSubscribeRejectsNilAndNonUTF8Topic(t *testing.T) {
	p := &PostgresFanout{subs: map[string]map[uint64]*pgSub{}}

	if _, err := p.Subscribe("orders", nil); err == nil {
		t.Error("SECURITY: [fanout] Subscribe accepted a nil callback")
	}
	bad := "bad\xfft"
	if _, err := p.Subscribe(bad, func([]byte) {}); err == nil {
		t.Errorf("SECURITY: [fanout] Subscribe accepted non-UTF-8 topic %q", bad)
	}
	// The refused registrations must not linger in the topic map.
	p.mu.RLock()
	n := len(p.subs)
	p.mu.RUnlock()
	if n != 0 {
		t.Errorf("refused registrations left %d topics registered", n)
	}
}

// ---------------------------------------------------------------------------
// Property: Close is terminal for registration — Subscribe after Close must
// refuse (and tear down the queue it just built) instead of minting a
// subscriber whose goroutine nothing will ever stop.
// ---------------------------------------------------------------------------

func TestSubscribeAfterCloseRefused(t *testing.T) {
	p := &PostgresFanout{subs: map[string]map[uint64]*pgSub{}}
	p.closed.Store(true)

	cancel, err := p.Subscribe("orders", func([]byte) {})
	if err == nil {
		t.Fatal("SECURITY: [fanout] Subscribe succeeded on a closed fanout (leaks an unstoppable subscriber goroutine)")
	}
	if cancel != nil {
		cancel() // the no-op cancel returned on refusal must be safe to call
	}
}

// ---------------------------------------------------------------------------
// Property: deliver routes to every subscriber of the topic, never to other
// topics, and never blocks on a subscriber whose callback is stuck — the
// single LISTEN/NOTIFY dispatcher must survive one slow consumer. Surfaces:
// a blocked subscriber on the SAME topic and a healthy one on another.
// ---------------------------------------------------------------------------

func TestDeliverSurvivesStuckSubscriber(t *testing.T) {
	p := &PostgresFanout{subs: map[string]map[uint64]*pgSub{}}

	stuck := make(chan struct{})
	cancelStuck, err := p.Subscribe("orders", func([]byte) { <-stuck })
	if err != nil {
		t.Fatalf("subscribe stuck: %v", err)
	}
	defer cancelStuck()

	healthy := make(chan []byte, 1)
	cancelHealthy, err := p.Subscribe("audit", func(b []byte) { healthy <- b })
	if err != nil {
		t.Fatalf("subscribe healthy: %v", err)
	}
	defer cancelHealthy()

	// The stuck subscriber's queue fills and starts dropping; deliver must
	// still return promptly on the shared dispatch path.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 50 {
			p.deliver("orders", []byte(`{}`))
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SECURITY: [fanout] deliver blocked on a stuck subscriber (would freeze the single dispatch goroutine for every topic)")
	}

	p.deliver("audit", []byte(`{"ok":1}`))
	select {
	case b := <-healthy:
		if string(b) != `{"ok":1}` {
			t.Errorf("healthy subscriber got %q", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("healthy subscriber on another topic never received its payload")
	}
	close(stuck)

	// Cancel is idempotent and removes only its own registration.
	cancelHealthy()
	cancelHealthy()
	p.mu.RLock()
	n := len(p.subs["audit"])
	p.mu.RUnlock()
	if n != 0 {
		t.Errorf("audit still has %d subscribers after cancel", n)
	}
}

// ---------------------------------------------------------------------------
// Property: construction on an unreachable DSN fails FAST (bounded by
// WithListenTimeout) instead of hanging forever in pq's reconnect loop —
// a wedged boot must surface as a prompt error.
// ---------------------------------------------------------------------------

func TestNewPostgresUnreachableFailsFast(t *testing.T) {
	// Loopback port 1: connection refused instantly, no DNS, no external
	// network. pq.Listener retries forever; the listen deadline must bound it.
	dsn := "host=127.0.0.1 port=1 dbname=fanout_test sslmode=disable connect_timeout=1"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	start := time.Now()
	_, err = NewPostgres(dsn, db, WithoutEnsureTable(), WithListenTimeout(300*time.Millisecond))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("NewPostgres succeeded against an unreachable DSN")
	}
	if !strings.Contains(err.Error(), "timed out") && !strings.Contains(err.Error(), "listen") {
		t.Errorf("err = %v, want the listen-timeout error", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("NewPostgres took %v to fail on an unreachable DSN, want ~the listen timeout", elapsed)
	}
}
