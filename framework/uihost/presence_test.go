package uihost

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

// testPresenceUser is a uihost-test-local double for battery/auth's User.
// It satisfies island's presenceUser interface (GetID + GetEmail) via
// structural typing — PresenceIdentityFromContext type-asserts the ctx
// value, exactly as it would a real auth user. No battery/auth import.
type testPresenceUser struct{ id, email string }

func (u *testPresenceUser) GetID() string    { return u.id }
func (u *testPresenceUser) GetEmail() string { return u.email }

// pollUntil retries cond every 5ms until it returns true or the timeout
// elapses. SSE presence registration is asynchronous (the handler runs in a
// goroutine), so roster assertions need to wait for it to settle.
func pollUntil(t *testing.T, cond func() bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting: %s", msg)
}

// TestHandleSSEPresenceAuthedUserRecords — handleSSE resolves the identity
// from the request CONTEXT (handler.SetUser) and records it on the topic.
// A fake ?user=attacker query param is IGNORED: the roster shows the ctx
// user, proving identity is server-derived (security invariant #1).
func TestHandleSSEPresenceAuthedUserRecords(t *testing.T) {
	ds := newTestUIHost()
	sess := ds.CreateSession()

	ctx, cancel := context.WithCancel(context.Background())
	// The "attacker" param is present to prove it is never read.
	req := httptest.NewRequest("GET", "/__gofastr/sse?session="+sess.ID+"&presence=doc:42&user=attacker", nil)
	req = req.WithContext(handler.SetUser(ctx, &testPresenceUser{id: "alice", email: "alice@test.com"}))

	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		ds.handleSSE(w, req) // must not panic
		close(done)
	}()

	var roster []island.PresenceMember
	pollUntil(t, func() bool {
		roster = ds.Islands.PresenceRoster("doc:42")
		return len(roster) >= 1
	}, time.Second, "authed user should appear in roster")

	if len(roster) != 1 {
		t.Fatalf("expected exactly 1 member, got %d (%+v)", len(roster), roster)
	}
	if roster[0].UserID != "alice" {
		t.Errorf("SPOOF: roster identity = %q, want ctx user 'alice' — the ?user= param must be ignored", roster[0].UserID)
	}
	if roster[0].DisplayName != "alice@test.com" {
		t.Errorf("display name = %q, want alice@test.com", roster[0].DisplayName)
	}

	cancel() // disconnect
	<-done

	// No ghost presence after disconnect (invariant #4).
	pollUntil(t, func() bool {
		return len(ds.Islands.PresenceRoster("doc:42")) == 0
	}, time.Second, "roster must empty after disconnect")
}

// TestHandleSSEPresenceAnonymousNoCrash — an unauthenticated SSE connection
// with a presence topic does not crash; it registers a pseudo-identity.
func TestHandleSSEPresenceAnonymousNoCrash(t *testing.T) {
	ds := newTestUIHost()
	sess := ds.CreateSession()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", "/__gofastr/sse?session="+sess.ID+"&presence=room", nil)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		ds.handleSSE(w, req) // must not panic on nil user
		close(done)
	}()

	pollUntil(t, func() bool {
		return len(ds.Islands.PresenceRoster("room")) >= 1
	}, time.Second, "anonymous connection should register a pseudo-identity")

	cancel()
	<-done
}

// TestHandleSSEPresenceNoTopicNoPresence — a connection without ?presence=
// joins no topic; the SSE stream still works (backward compatible).
func TestHandleSSEPresenceNoTopicNoPresence(t *testing.T) {
	ds := newTestUIHost()
	sess := ds.CreateSession()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", "/__gofastr/sse?session="+sess.ID, nil)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		ds.handleSSE(w, req)
		close(done)
	}()

	// Give it a moment, then verify no topic has members.
	time.Sleep(50 * time.Millisecond)
	if got := ds.Islands.PresenceRoster("anything"); len(got) != 0 {
		t.Errorf("connection without presence param should join no topic, got %+v", got)
	}

	cancel()
	<-done
}

// TestInjectChromePresenceMetaTag — the SSE meta tag includes &presence=
// (url-escaped) when a presenceTopic is threaded, so the client's
// EventSource connection joins the named topic. This is the meta-tag
// threading verified end-to-end through injectChrome.
func TestInjectChromePresenceMetaTag(t *testing.T) {
	ds := newTestUIHost()
	page := ds.injectChrome(`<html><head></head><body>x</body></html>`, "/p", "sess-123", "doc:42")
	if !strings.Contains(page, `&presence=doc%3A42`) {
		t.Errorf("SSE meta tag should include &presence=doc%%3A42 (url-escaped), got:\n%s", page)
	}
	if !strings.Contains(page, `/__gofastr/sse?session=sess-123`) {
		t.Errorf("SSE meta tag missing base session URL, got:\n%s", page)
	}
}

// TestInjectChromeNoPresenceMetaTag — without a presence topic, the meta
// tag is the plain session URL (backward compatible).
func TestInjectChromeNoPresenceMetaTag(t *testing.T) {
	ds := newTestUIHost()
	page := ds.injectChrome(`<html><head></head><body>x</body></html>`, "/p", "sess-123", "")
	if strings.Contains(page, "&presence=") {
		t.Errorf("SSE meta tag should NOT include &presence= when no topic, got:\n%s", page)
	}
}
