package island

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// These tests pin the concurrent-SSE-stream caps. An anonymous same-origin
// caller can mint a session (POST /__gofastr/session) and then open an
// unbounded number of EventSource streams against it; without a ceiling one
// client (or a reconnect-loop bug) holds a goroutine + 64-slot buffered channel
// per connection forever. The policy is REJECT-not-evict: a stream over the cap
// is refused with a clear status, and the streams already held are left alone.

// TestStreamCapRejectsPerSessionOverflow: the N+1th stream for ONE session is
// refused, and the N existing streams survive (a new stream never silently
// kills an old one).
func TestStreamCapRejectsPerSessionOverflow(t *testing.T) {
	mgr := NewManager(WithStreamCaps(2, 0)) // perSession=2, global unlimited

	_, c1, _ := mgr.ConnectSession("sess")
	defer c1()
	_, c2, _ := mgr.ConnectSession("sess")
	defer c2()

	ch3, _, err := mgr.ConnectSession("sess")
	if err == nil {
		t.Fatalf("expected per-session stream cap to refuse the 3rd stream")
	}
	if ch3 != nil {
		t.Fatalf("a refused connect must return a nil channel, got %v", ch3)
	}
	if err != ErrSessionStreamLimit {
		t.Fatalf("expected ErrSessionStreamLimit, got %v", err)
	}

	// Reject-not-evict: the two existing streams are untouched.
	mgr.mu.RLock()
	n := len(mgr.streams["sess"].subs)
	mgr.mu.RUnlock()
	if n != 2 {
		t.Fatalf("reject-not-evict: expected 2 surviving streams, got %d", n)
	}
}

// TestStreamCapRejectsGlobalOverflow: the total across sessions is bounded.
func TestStreamCapRejectsGlobalOverflow(t *testing.T) {
	mgr := NewManager(WithStreamCaps(0, 2)) // perSession unlimited, global=2

	_, c1, _ := mgr.ConnectSession("a")
	defer c1()
	_, c2, _ := mgr.ConnectSession("b")
	defer c2()

	ch3, _, err := mgr.ConnectSession("c")
	if err == nil {
		t.Fatalf("expected global stream cap to refuse the 3rd stream")
	}
	if ch3 != nil {
		t.Fatalf("a refused connect must return a nil channel, got %v", ch3)
	}
	if err != ErrGlobalStreamLimit {
		t.Fatalf("expected ErrGlobalStreamLimit, got %v", err)
	}
}

// TestStreamCapCancelReleasesSlot: cancelling a stream frees its slot, so the
// cap is a live ceiling, not a one-shot count.
func TestStreamCapCancelReleasesSlot(t *testing.T) {
	mgr := NewManager(WithStreamCaps(1, 0))

	_, c1, _ := mgr.ConnectSession("sess")
	if _, _, err := mgr.ConnectSession("sess"); err == nil {
		t.Fatal("expected 2nd stream to be refused while the 1st is held")
	}
	c1() // release

	ch, c2, err := mgr.ConnectSession("sess")
	if err != nil {
		t.Fatalf("expected a fresh stream after release, got %v", err)
	}
	defer c2()
	if ch == nil {
		t.Fatal("expected a non-nil channel after release")
	}
}

// TestSSEStreamCapReturnsClearStatus: a refused connect at the SSE entry point
// returns 429 Too Many Requests (not a silent drop and not a 500).
func TestSSEStreamCapReturnsClearStatus(t *testing.T) {
	mgr := NewManager(WithStreamCaps(1, 0))

	// Occupy the single per-session slot directly (a live SSE connection).
	_, occ, _ := mgr.ConnectSession("sess")
	defer occ()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/__gofastr/sse?session=sess", nil)
	// The refusal path returns BEFORE the blocking SSE loop, so this call is
	// prompt and does not need the request context cancelled.
	mgr.ServeSSEWithPresence(rec, req, PresenceIdentity{}, nil)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected %d for an over-cap stream, got %d (body=%q)",
			http.StatusTooManyRequests, rec.Code, rec.Body.String())
	}
	if rec.Body.String() == "" {
		t.Fatal("refusal body must explain why the stream was refused")
	}
}
