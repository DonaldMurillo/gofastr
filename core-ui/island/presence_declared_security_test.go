package island

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// serveDeclared starts an SSE server whose page declared declared and whose
// client asks for ?presence= on top.
func serveDeclared(t *testing.T, m *Manager, declared ...string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.ServeSSEWithPresence(w, r, PresenceIdentity{UserID: "u1"}, PresenceTopics{
			Declared:  declared,
			Requested: ParsePresenceTopics(r.URL.Query().Get("presence")),
		})
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func connectSSE(t *testing.T, rawURL string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect SSE: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
}

// Issue #614 O31: with no AuthorizeTopic hook, any ?presence= topic joined,
// so a viewer could read the roster (emails) of any topic it could name. A
// nil hook now admits only the topics the page declared.
func TestNilHookRefusesRequestedTopic(t *testing.T) {
	m := NewManager()
	base := serveDeclared(t, m, "doc:1")
	connectSSE(t, base+"?session=sess-1&presence=doc:2")

	waitFor(t, 3*time.Second, "declared topic joins", func() bool { return len(m.PresenceRoster("doc:1")) == 1 })
	if r := m.PresenceRoster("doc:2"); len(r) != 0 {
		t.Fatalf("SECURITY: a requested topic joined with no hook: %v", r)
	}
	if s := m.PresenceSessions("doc:2"); len(s) != 0 {
		t.Fatalf("SECURITY: a requested topic became a push target with no hook: %v", s)
	}
}

// An installed hook answers for every topic, declared ones too, so the
// heartbeat re-check covers them.
func TestHookGatesDeclaredTopics(t *testing.T) {
	m := NewManager()
	m.SetAuthorizeTopic(func(_ context.Context, topic string) bool { return topic == "doc:2" })
	base := serveDeclared(t, m, "doc:1")
	connectSSE(t, base+"?session=sess-1&presence=doc:2")

	waitFor(t, 3*time.Second, "authorized requested topic joins", func() bool { return len(m.PresenceRoster("doc:2")) == 1 })
	if r := m.PresenceRoster("doc:1"); len(r) != 0 {
		t.Fatalf("a declared topic the hook refused joined: %v", r)
	}
}

// Declared and requested topics share the per-connection bounds: duplicates
// count once and at most maxPresenceTopics join.
func TestDeclaredTopicsBounded(t *testing.T) {
	m := NewManager()
	m.SetAuthorizeTopic(func(context.Context, string) bool { return true })
	got := m.admitTopics(context.Background(), PresenceTopics{
		Declared:  []string{"a", "a", string(make([]byte, maxPresenceTopicLen+1))},
		Requested: []string{"a", "b"},
	})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("admitted = %q, want [a b]", got)
	}
	many := make([]string, 0, 2*maxPresenceTopics)
	for i := range 2 * maxPresenceTopics {
		many = append(many, "t"+string(rune('A'+i)))
	}
	if got := m.admitTopics(context.Background(), PresenceTopics{Declared: many}); len(got) != maxPresenceTopics {
		t.Fatalf("admitted %d topics, want %d", len(got), maxPresenceTopics)
	}
}
