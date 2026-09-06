package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Property: one caller may not hold unbounded concurrent SSE subscriber
// seats on the MCP notification stream, and a caller the server-wide
// gate refuses holds none at all.
//
// Surfaces: core/mcp/transport.go:sseGetHandler (connect-time server-gate
// check + seat admission before any stream byte is written — the
// 2026-09-05 round-4 fix), notifications.go:addSSESubscriber (the
// per-caller cap, default defaultSSESeatCap, with the Refuse/EvictOldest
// overflow policy from seats.go), notifications.go:notifySubscribers
// (the backpressure drop frees the seat it drops).
//
// Found red in the 2026-09-05 adversarial pass round 4: 64 concurrent
// GET streams from one anonymous caller all registered — no cap, no
// gate at connect — so a private /mcp still grew one goroutine and one
// 16-slot buffered channel per connection an unauthenticated peer
// opened.

var errGateRefusedForTest = errors.New("gate refused for seat test")

// streamAttempt is one GET stream driven through ServeSSE on a
// recorder: done closes when the handler returns (a refused or evicted
// stream), and never while it holds the connection.
type streamAttempt struct {
	rec  *httptest.ResponseRecorder
	done chan struct{}
}

func startStream(t *testing.T, h http.Handler, ctx context.Context) *streamAttempt {
	t.Helper()
	at := &streamAttempt{rec: httptest.NewRecorder(), done: make(chan struct{})}
	req := httptest.NewRequest(http.MethodGet, "/sse", nil).WithContext(ctx)
	req.Header.Set("Accept", "text/event-stream")
	go func() {
		defer close(at.done)
		h.ServeHTTP(at.rec, req)
	}()
	return at
}

// waitDone polls until n attempts have completed or the deadline passes;
// it returns the completed attempts.
func waitDone(t *testing.T, attempts []*streamAttempt, n int) []*streamAttempt {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var done []*streamAttempt
		for _, at := range attempts {
			select {
			case <-at.done:
				done = append(done, at)
			default:
			}
		}
		if len(done) >= n {
			return done
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d of %d streams to complete", n, len(attempts))
	return nil
}

// TestSSESubscriberSeatsBounded: 64 concurrent notification streams from
// one anonymous caller the server-wide gate refuses — every one must be
// refused at connect (403) and hold no seat.
func TestSSESubscriberSeatsBounded(t *testing.T) {
	s := NewServer()
	// A private /mcp: the server-wide gate refuses everyone.
	s.SetGate(func(context.Context) error { return errGateRefusedForTest })

	h := s.ServeSSE("/sse")
	const streams = 64

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := make([]*streamAttempt, streams)
	for i := range streams {
		attempts[i] = startStream(t, h, ctx)
	}

	// Every stream is answered and returns; none registers.
	done := waitDone(t, attempts, streams)
	s.sseMu.Lock()
	held := len(s.sseSubs)
	s.sseMu.Unlock()
	if held > 16 {
		t.Errorf("SECURITY: [mcp-sse-seats] a single gate-REFUSED anonymous caller holds %d concurrent SSE subscriber seats (bound %d): admission must precede registration", held, defaultSSESeatCap)
	}
	if held != 0 {
		t.Errorf("gate-refused caller holds %d seats; connect-time gate refusal must hold none", held)
	}
	for _, at := range done {
		if at.rec.Code != http.StatusForbidden {
			t.Errorf("gate-refused stream answered %d, want 403", at.rec.Code)
		}
	}

	// Connection accounting on disconnect stays sound (already pinned by
	// TestDisconnectUnregistersSubscriber); assert it here too so the seat
	// count cannot be "fixed" by leaking registrations on exit.
	cancel()
	for _, at := range attempts {
		<-at.done
	}
	s.sseMu.Lock()
	drained := len(s.sseSubs)
	s.sseMu.Unlock()
	if drained != 0 {
		t.Errorf("registry kept %d subscribers after every stream disconnected", drained)
	}
}

// TestSSESubscriberSeatsRefusePastCap: with no gate at all, one caller
// (same TCP peer — every httptest.NewRequest shares the RemoteAddr)
// reaches the default cap at 16 streams and every further stream is
// answered 429 at connect, holding nothing.
func TestSSESubscriberSeatsRefusePastCap(t *testing.T) {
	s := NewServer()
	h := s.ServeSSE("/sse")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const streams = 64
	attempts := make([]*streamAttempt, streams)
	for i := range streams {
		attempts[i] = startStream(t, h, ctx)
	}

	// Exactly defaultSSESeatCap streams hold; the rest are refused.
	waitForSubscribers(t, s, defaultSSESeatCap)
	refused := waitDone(t, attempts, streams-defaultSSESeatCap)
	if len(refused) != streams-defaultSSESeatCap {
		t.Fatalf("expected %d refused streams, %d completed", streams-defaultSSESeatCap, len(refused))
	}
	s.sseMu.Lock()
	held := len(s.sseSubs)
	s.sseMu.Unlock()
	if held != defaultSSESeatCap {
		t.Errorf("caller holds %d seats, want exactly the %d cap", held, defaultSSESeatCap)
	}
	for _, at := range refused {
		if at.rec.Code != http.StatusTooManyRequests {
			t.Errorf("seat-refused stream answered %d, want 429", at.rec.Code)
		}
	}

	cancel()
	for _, at := range attempts {
		<-at.done
	}
	if got := sseRegistryCount(s); got != 0 {
		t.Errorf("registry kept %d subscribers after every stream disconnected", got)
	}
}

// TestSSESubscriberSeatsEvictOldest: the configurable overflow policy.
// Past the cap the caller's OLDEST stream is closed (its handler
// returns) and the new stream takes the seat; the count never exceeds
// the cap.
func TestSSESubscriberSeatsEvictOldest(t *testing.T) {
	s := NewServer()
	s.SetSSESeatCap(2)
	s.SetSSESeatOverflow(SeatOverflowEvictOldest)
	h := s.ServeSSE("/sse")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startStream(t, h, ctx)
	waitForSubscribers(t, s, 1)
	b := startStream(t, h, ctx)
	waitForSubscribers(t, s, 2)

	c := startStream(t, h, ctx) // evicts a
	select {
	case <-a.done:
	case <-time.After(3 * time.Second):
		t.Fatal("evicted stream's handler never returned; the oldest seat was not closed")
	}
	waitForSubscribers(t, s, 2)
	if got := sseRegistryCount(s); got != 2 {
		t.Errorf("caller holds %d seats under a cap of 2 with EvictOldest, want 2", got)
	}
	select {
	case <-b.done:
		t.Fatal("the wrong stream was evicted: b (newer than a) returned")
	default:
	}
	select {
	case <-c.done:
		t.Fatal("the newest stream was evicted by its own admission")
	default:
	}

	cancel()
	for _, at := range []*streamAttempt{a, b, c} {
		<-at.done
	}
	if got := sseRegistryCount(s); got != 0 {
		t.Errorf("registry kept %d subscribers after every stream disconnected", got)
	}
}
