package island

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// filterAuthorizedTopics drops topics the AuthorizeTopic hook rejects.

// A nil AuthorizeTopic hook authorizes everything — presence stays public by
// default (opt-in gating), so existing apps are unaffected.
func TestAuthorizeTopic_NilHookAllowsAll(t *testing.T) {
	m := NewManager()
	got := m.filterAuthorizedTopics(context.Background(), []string{"a", "b"})
	if len(got) != 2 {
		t.Fatalf("nil hook must pass all topics, got %v", got)
	}
}

// When set, the hook filters the topic list — only authorized topics survive
// to be joined.
func TestAuthorizeTopic_FiltersTopics(t *testing.T) {
	m := NewManager()
	m.AuthorizeTopic = func(_ context.Context, topic string) bool {
		return topic == "room:public"
	}
	got := m.filterAuthorizedTopics(context.Background(), []string{"room:public", "room:secret"})
	if len(got) != 1 || got[0] != "room:public" {
		t.Fatalf("hook must drop unauthorized topics, got %v", got)
	}
}

// Composition: an unauthorized topic never reaches the roster or the push-
// target set — no subscription, no roster emission — while the authorized one
// works normally. Proves the gate happens before roster registration.
func TestAuthorizeTopic_UnauthorizedTopicHasNoRoster(t *testing.T) {
	m := NewManager()
	m.AuthorizeTopic = func(_ context.Context, topic string) bool {
		return topic == "room:public"
	}
	topics := m.filterAuthorizedTopics(context.Background(), []string{"room:public", "room:secret"})
	h := m.PresenceJoin("sess-1", PresenceIdentity{UserID: "u1", DisplayName: "alice@x.com"}, topics)
	defer h.Leave()

	if r := m.PresenceRoster("room:public"); len(r) != 1 {
		t.Fatalf("authorized topic roster = %d, want 1", len(r))
	}
	// The private topic must expose nothing — neither roster (identity leak)
	// nor push sessions (existence oracle).
	if r := m.PresenceRoster("room:secret"); len(r) != 0 {
		t.Fatalf("SECURITY: unauthorized topic leaked roster: %v", r)
	}
	if s := m.PresenceSessions("room:secret"); len(s) != 0 {
		t.Fatalf("SECURITY: unauthorized topic leaked push sessions: %v", s)
	}
}

// Rejecting every topic yields an empty join (a nil handle from PresenceJoin),
// which is a safe no-op — Leave on it must not panic.
func TestAuthorizeTopic_AllRejectedIsSafe(t *testing.T) {
	m := NewManager()
	m.AuthorizeTopic = func(_ context.Context, _ string) bool { return false }
	topics := m.filterAuthorizedTopics(context.Background(), []string{"a", "b"})
	if len(topics) != 0 {
		t.Fatalf("all-reject must yield no topics, got %v", topics)
	}
	h := m.PresenceJoin("sess-1", PresenceIdentity{UserID: "u1"}, topics)
	h.Leave() // nil handle Leave is a no-op; must not panic
}

// End-to-end through the real SSE entry point: ServeSSEWithPresence must apply
// the gate BEFORE the join, so an unauthorized topic named via ?presence=
// yields no roster and no push-target session. This covers the actual
// production wiring (stream.go), not just the helper — a refactor that moved
// or dropped the filter would fail here.
func TestServeSSEWithPresence_AuthorizeTopicGate(t *testing.T) {
	m := NewManager()
	m.AuthorizeTopic = func(_ context.Context, topic string) bool {
		return topic == "room:public"
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		topics := ParsePresenceTopics(r.URL.Query().Get("presence"))
		m.ServeSSEWithPresence(w, r, PresenceIdentity{UserID: "u1", DisplayName: "alice@x.com"}, topics)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"?session=sess-1&presence=room:public,room:secret", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect SSE: %v", err)
	}
	defer resp.Body.Close()

	// Wait for the join to register (headers flush only after PresenceJoin).
	deadline := time.Now().Add(5 * time.Second)
	for len(m.PresenceRoster("room:public")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("authorized topic never joined")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The unauthorized topic must expose nothing through the real handler.
	if r := m.PresenceRoster("room:secret"); len(r) != 0 {
		t.Fatalf("SECURITY: unauthorized topic leaked roster via ServeSSEWithPresence: %v", r)
	}
	if s := m.PresenceSessions("room:secret"); len(s) != 0 {
		t.Fatalf("SECURITY: unauthorized topic leaked push sessions: %v", s)
	}
	// Sanity: the authorized topic is present with exactly this viewer.
	if r := m.PresenceRoster("room:public"); len(r) != 1 || r[0].UserID != "u1" {
		t.Fatalf("authorized roster = %v, want [u1]", r)
	}
	cancel()
}

// A panicking AuthorizeTopic hook must not leak a subscription. The hook runs
// BEFORE ConnectSession, so a panic (net/http recovers it) leaves no stream
// entry behind — no ghost session accumulating pushes. Regression for the
// window where the hook ran after Subscribe but before the Unsubscribe defer.
func TestServeSSEWithPresence_PanickingHookLeaksNoSubscription(t *testing.T) {
	m := NewManager()
	m.AuthorizeTopic = func(_ context.Context, _ string) bool { panic("boom") }
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		topics := ParsePresenceTopics(r.URL.Query().Get("presence"))
		m.ServeSSEWithPresence(w, r, PresenceIdentity{UserID: "u1"}, topics)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	// The panic aborts the request; the client gets a 500 or a reset — either
	// way the handler goroutine unwinds.
	if resp, err := http.Get(server.URL + "?session=leak-sess&presence=room:x"); err == nil {
		resp.Body.Close()
	}

	// No ghost stream entry may remain for the session.
	deadline := time.Now().Add(2 * time.Second)
	for {
		m.mu.RLock()
		_, leaked := m.streams["leak-sess"]
		m.mu.RUnlock()
		if !leaked {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("panicking AuthorizeTopic leaked a subscription (ghost stream entry)")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
