package island

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// fakeAuthUser is a test double for battery/auth's User. It implements the
// presenceUser interface (GetID + GetEmail) so PresenceIdentityFromContext
// can type-assert it — exactly as a real battery/auth user would. This lets
// the tests prove identity derivation WITHOUT importing battery/auth.
type fakeAuthUser struct {
	id    string
	email string
}

func (u *fakeAuthUser) GetID() string    { return u.id }
func (u *fakeAuthUser) GetEmail() string { return u.email }

// TestPresenceJoinAddsToRoster — a join on a topic makes the identity
// appear in that topic's roster.
func TestPresenceJoinAddsToRoster(t *testing.T) {
	m := NewManager()
	h := m.PresenceJoin("sess-1", PresenceIdentity{UserID: "u1", DisplayName: "Alice"}, []string{"doc:42"})
	defer h.Leave()

	roster := m.PresenceRoster("doc:42")
	if len(roster) != 1 {
		t.Fatalf("expected 1 member, got %d (%+v)", len(roster), roster)
	}
	if roster[0].UserID != "u1" || roster[0].DisplayName != "Alice" {
		t.Errorf("unexpected member: %+v", roster[0])
	}
}

// TestPresenceLeaveRemovesMember — disconnect removes the member.
func TestPresenceLeaveRemovesMember(t *testing.T) {
	m := NewManager()
	h := m.PresenceJoin("sess-1", PresenceIdentity{UserID: "u1", DisplayName: "Alice"}, []string{"doc:42"})
	h.Leave()

	if got := m.PresenceRoster("doc:42"); len(got) != 0 {
		t.Errorf("expected empty roster after leave, got %+v", got)
	}
}

// TestPresenceMultiTabRefCount — two connections for the same user on the
// same topic produce ONE roster member; closing one tab keeps the member;
// closing the last drops it (the "no ghost presence" invariant).
func TestPresenceMultiTabRefCount(t *testing.T) {
	m := NewManager()
	id := PresenceIdentity{UserID: "u1", DisplayName: "Alice"}
	h1 := m.PresenceJoin("sess-1", id, []string{"doc:42"})
	h2 := m.PresenceJoin("sess-2", id, []string{"doc:42"})

	if got := m.PresenceRoster("doc:42"); len(got) != 1 {
		t.Fatalf("two same-user joins should dedup to 1 member, got %d", len(got))
	}

	// Close one tab — member must persist.
	h1.Leave()
	if got := m.PresenceRoster("doc:42"); len(got) != 1 {
		t.Fatalf("roster must still have 1 member after one tab closes, got %d", len(got))
	}

	// Close the last tab — member drops (no ghost).
	h2.Leave()
	if got := m.PresenceRoster("doc:42"); len(got) != 0 {
		t.Errorf("roster must be empty after last tab closes, got %+v", got)
	}
}

// TestPresenceAnonymousHandled — an anonymous (zero-identity) connection is
// tracked with a server-derived pseudo-identity, not crashed on and not
// excluded. The pseudo-identity comes from the session id, never a client
// param.
func TestPresenceAnonymousHandled(t *testing.T) {
	m := NewManager()
	h := m.PresenceJoin("sess-anon", PresenceIdentity{}, []string{"room"})
	defer h.Leave()

	roster := m.PresenceRoster("room")
	if len(roster) != 1 {
		t.Fatalf("anonymous connection should produce 1 member, got %d", len(roster))
	}
	if !strings.HasPrefix(roster[0].UserID, "anon:") {
		t.Errorf("anonymous UserID should be 'anon:'-prefixed, got %q", roster[0].UserID)
	}
	if !strings.HasPrefix(roster[0].DisplayName, "Guest ") {
		t.Errorf("anonymous DisplayName should be 'Guest …', got %q", roster[0].DisplayName)
	}
}

// TestPresenceMultipleTopics — one connection joining two topics appears in
// each topic's roster independently.
func TestPresenceMultipleTopics(t *testing.T) {
	m := NewManager()
	h := m.PresenceJoin("sess-1", PresenceIdentity{UserID: "u1", DisplayName: "A"}, []string{"a", "b"})
	defer h.Leave()

	if got := m.PresenceRoster("a"); len(got) != 1 || got[0].UserID != "u1" {
		t.Errorf("topic a roster = %+v, want u1", got)
	}
	if got := m.PresenceRoster("b"); len(got) != 1 || got[0].UserID != "u1" {
		t.Errorf("topic b roster = %+v, want u1", got)
	}
	if got := m.PresenceRoster("c"); len(got) != 0 {
		t.Errorf("topic c roster should be empty, got %+v", got)
	}
}

// TestParsePresenceTopicsBounds — oversize topics are dropped, the count is
// capped, duplicates collapse, and empty input yields nil.
func TestParsePresenceTopicsBounds(t *testing.T) {
	// empty
	if got := ParsePresenceTopics(""); got != nil {
		t.Errorf("empty input should yield nil, got %v", got)
	}
	// dedup
	got := ParsePresenceTopics("a,b,a")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("dedup: got %v", got)
	}
	// oversize dropped
	long := strings.Repeat("x", maxPresenceTopicLen+1)
	got = ParsePresenceTopics("ok," + long)
	if len(got) != 1 || got[0] != "ok" {
		t.Errorf("oversize topic should be dropped, got %v", got)
	}
	// count capped
	many := make([]string, maxPresenceTopics+5)
	for i := range many {
		many[i] = "t" + string(rune('a'+i%26)) + string(rune('0'+i))
	}
	got = ParsePresenceTopics(strings.Join(many, ","))
	if len(got) > maxPresenceTopics {
		t.Errorf("topic count should be capped at %d, got %d", maxPresenceTopics, len(got))
	}
}

// TestPresenceIdentityServerDerived — THE SPOOF TEST. Identity comes from
// the ctx auth user (handler.GetUser), never from a client param. A
// connection has NO way to claim a different identity: PresenceJoin only
// accepts a PresenceIdentity that the caller (handleSSE) derived from ctx.
// Here we prove the ctx user is what the roster reflects, and that there is
// no topic/connection value that can override it.
func TestPresenceIdentityServerDerived(t *testing.T) {
	ctx := handler.SetUser(context.Background(), &fakeAuthUser{id: "real-alice", email: "alice@test.com"})

	// The identity is resolved from ctx — the ONLY source.
	id := PresenceIdentityFromContext(ctx)
	if id.UserID != "real-alice" || id.DisplayName != "alice@test.com" {
		t.Fatalf("identity must come from ctx user, got %+v", id)
	}

	m := NewManager()
	// A hostile client might try ?presence=doc:42&user=evil — but "user" is
	// never read. The topic is the only client-supplied value; the identity
	// is the ctx-derived one.
	h := m.PresenceJoin("sess-1", id, []string{"doc:42"})
	defer h.Leave()

	roster := m.PresenceRoster("doc:42")
	if len(roster) != 1 {
		t.Fatalf("expected 1 member, got %d", len(roster))
	}
	if roster[0].UserID != "real-alice" {
		t.Errorf("SPOOF: roster identity = %q, want the ctx user 'real-alice'", roster[0].UserID)
	}
}

// TestPresenceIdentityNoUserAnonymous — a context with no authenticated
// user yields the zero identity (anonymous), which PresenceJoin then
// synthesizes a pseudo-identity for. No crash, no leak.
func TestPresenceIdentityNoUserAnonymous(t *testing.T) {
	id := PresenceIdentityFromContext(context.Background())
	if id.UserID != "" {
		t.Errorf("empty ctx should yield anonymous identity, got %+v", id)
	}
}

// TestPresenceRosterSorted — roster is deterministically sorted by UserID.
func TestPresenceRosterSorted(t *testing.T) {
	m := NewManager()
	_ = m.PresenceJoin("s1", PresenceIdentity{UserID: "charlie", DisplayName: "C"}, []string{"t"})
	_ = m.PresenceJoin("s2", PresenceIdentity{UserID: "alpha", DisplayName: "A"}, []string{"t"})
	_ = m.PresenceJoin("s3", PresenceIdentity{UserID: "bravo", DisplayName: "B"}, []string{"t"})

	roster := m.PresenceRoster("t")
	if len(roster) != 3 {
		t.Fatalf("expected 3 members, got %d", len(roster))
	}
	want := []string{"alpha", "bravo", "charlie"}
	for i, w := range want {
		if roster[i].UserID != w {
			t.Errorf("roster[%d] = %q, want %q (sorted)", i, roster[i].UserID, w)
		}
	}
}

// TestPresenceSessionsForTopic — PresenceSessions returns the distinct
// session ids on a topic (push targets), sorted.
func TestPresenceSessionsForTopic(t *testing.T) {
	m := NewManager()
	_ = m.PresenceJoin("sess-b", PresenceIdentity{UserID: "u2"}, []string{"t"})
	_ = m.PresenceJoin("sess-a", PresenceIdentity{UserID: "u1"}, []string{"t"})
	_ = m.PresenceJoin("sess-c", PresenceIdentity{UserID: "u3"}, []string{"other"})

	got := m.PresenceSessions("t")
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions on topic t, got %d (%v)", len(got), got)
	}
	if got[0] != "sess-a" || got[1] != "sess-b" {
		t.Errorf("sessions = %v, want [sess-a sess-b] sorted", got)
	}
}

// TestPresenceOnChangeFires — OnPresenceChange fires on join and leave.
func TestPresenceOnChangeFires(t *testing.T) {
	m := NewManager()
	var events []string
	m.OnPresenceChange = func(topic string) { events = append(events, topic) }

	h := m.PresenceJoin("s1", PresenceIdentity{UserID: "u1"}, []string{"doc:1", "doc:2"})
	h.Leave()

	// 2 joins + 2 leaves = 4 events
	if len(events) != 4 {
		t.Fatalf("expected 4 change events (2 join + 2 leave), got %d (%v)", len(events), events)
	}
}

// TestPresenceDedupByUserID — two different sessions, same userID,
// produce one roster member (dedup is by UserID, not session).
func TestPresenceDedupByUserID(t *testing.T) {
	m := NewManager()
	id := PresenceIdentity{UserID: "shared-user", DisplayName: "Same Person"}
	_ = m.PresenceJoin("sess-1", id, []string{"t"})
	_ = m.PresenceJoin("sess-2", id, []string{"t"})

	if got := m.PresenceRoster("t"); len(got) != 1 {
		t.Errorf("two sessions same user should dedup to 1 member, got %d", len(got))
	}
}

// TestPresenceJoinNoTopicsReturnsNil — joining with no topics is a no-op
// (nil handle); Leave on nil is safe.
func TestPresenceJoinNoTopicsReturnsNil(t *testing.T) {
	m := NewManager()
	h := m.PresenceJoin("s1", PresenceIdentity{UserID: "u1"}, nil)
	if h != nil {
		t.Errorf("joining with no topics should return nil handle, got %v", h)
	}
	var nilHandle *PresenceHandle
	nilHandle.Leave() // must not panic
}

// TestPresenceRosterEmptyTopic — querying an empty topic returns nil.
func TestPresenceRosterEmptyTopic(t *testing.T) {
	m := NewManager()
	if got := m.PresenceRoster(""); got != nil {
		t.Errorf("empty topic should yield nil, got %v", got)
	}
}
