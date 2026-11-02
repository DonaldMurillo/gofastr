package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// An idle notification stream re-checks its caller on its own clock,
// not only when a notification happens to arrive. The write loop
// re-established the principal only before a delivery, and notifications
// are rare (a registration, a resources/updated), so a revoked session's
// stream stayed open, holding its seat, its goroutine and its buffered
// channel, for as long as the server stayed quiet. The same silence hid
// a half-open connection: with nothing written, nothing ever failed. The
// keep-alive tick re-checks the caller and writes an SSE comment, so both
// end within one interval.
func TestRevokedIdleStreamCloses(t *testing.T) {
	var revoked atomic.Bool
	s := NewServer()
	s.sseKeepAlive = 20 * time.Millisecond
	s.SetGate(func(ctx context.Context) error {
		if _, ok := handler.GetUser(ctx); !ok {
			return fmt.Errorf("authentication required")
		}
		return nil
	})
	h := s.ServeSSE("/mcp")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := handler.SetUser(r.Context(), revocableUser{})
		ctx = handler.WithPrincipalCheck(ctx, func(ctx context.Context) (context.Context, bool) {
			return ctx, !revoked.Load()
		})
		h.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(ts.Close)
	events := openStream(t, ts, false)
	waitForSubscribers(t, s, 1)

	revoked.Store(true)
	// No notification is raised: the stream must end on its own.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				waitForSubscribers(t, s, 0)
				return
			}
			t.Fatalf("SECURITY: [revocation] revoked idle stream received %+v", ev)
		case <-deadline:
			t.Fatal("SECURITY: [revocation] revoked session's idle stream stayed open (no re-check without a notification)")
		}
	}
}

// A held stream for a caller still in good standing stays open across
// keep-alive ticks: the comment lines are invisible to an SSE consumer
// and the re-check passes.
func TestIdleStreamSurvivesKeepAlive(t *testing.T) {
	s := NewServer()
	s.sseKeepAlive = 10 * time.Millisecond
	ts := newNotificationServer(t, s)
	events := openStream(t, ts, true)
	waitForSubscribers(t, s, 1)
	time.Sleep(100 * time.Millisecond) // several ticks
	s.NotifyToolsListChanged()
	if ev := awaitSSE(t, events, "list_changed after keep-alives"); ev.event != "message" {
		t.Fatalf("event = %q, want message", ev.event)
	}
}
