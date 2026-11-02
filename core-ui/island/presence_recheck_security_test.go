package island

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// AuthorizeTopic ran once, at connect. A viewer whose access to a topic was
// removed afterwards stayed on its roster and kept receiving its pushes until
// the stream bound (2026-10-27 bug bash, unrechecked). The heartbeat re-runs
// the gate and closes the stream, which leaves the roster.
func TestPresenceLeavesRevokedTopic(t *testing.T) {
	m := NewManager(WithSSEHeartbeat(50 * time.Millisecond))
	var revoked atomic.Bool
	m.SetAuthorizeTopic(func(_ context.Context, topic string) bool {
		return topic == "room:a" && !revoked.Load()
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		topics := ParsePresenceTopics(r.URL.Query().Get("presence"))
		m.ServeSSEWithPresence(w, r, PresenceIdentity{UserID: "u1"}, PresenceTopics{Requested: topics})
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"?session=sess-1&presence=room:a", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect SSE: %v", err)
	}
	defer resp.Body.Close()
	waitFor(t, 3*time.Second, "viewer joins room:a", func() bool { return len(m.PresenceRoster("room:a")) == 1 })

	revoked.Store(true)
	waitFor(t, 3*time.Second, "SECURITY: revoked viewer leaves room:a", func() bool { return len(m.PresenceRoster("room:a")) == 0 })
}
