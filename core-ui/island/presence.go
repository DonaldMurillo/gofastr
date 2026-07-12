package island

import (
	"context"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// ─── Presence ───────────────────────────────────────────────────────
//
// Presence tracks WHO is currently connected to a given SSE topic on
// THIS replica. A "topic" is an opaque string an app associates with an
// entity/room (e.g. "doc:42"). When a client opens the SSE stream with
// ?presence=<topic>, its connection is registered on that topic; the
// roster (the set of connected identities) is then queryable and can be
// pushed live to every viewer when someone joins or leaves.
//
// SECURITY MODEL — identity is SERVER-DERIVED, never client-supplied.
// The {userID, displayName} in a roster entry come exclusively from the
// request context's authenticated user (handler.GetUser), resolved at
// SSE-connect time. A client may NAME a topic (it is untrusted and
// bounded) but may NEVER claim an identity — so a user cannot spoof
// being someone else in the roster. See PresenceIdentityFromContext.
//
// Anonymous (unauthenticated) connections are tracked with a
// SERVER-DERIVED pseudo-identity synthesized from the session id (e.g.
// "anon:<session>" / "Guest a3f2"), NOT from any client param. This
// makes presence useful on apps without auth (every browser tab pair =
// one stable viewer) while preserving the invariant that a client can't
// choose its roster identity.
//
// SINGLE-REPLICA: the roster reflects only THIS replica's connections.
// Cross-replica roster aggregation (fanout of connection state) is
// future work — see framework/docs/content/presence.md.

// PresenceIdentity is the server-derived identity of a connected viewer.
// A zero value (empty UserID) means anonymous; PresenceJoin synthesizes
// a stable pseudo-identity from the session id so anonymous viewers still
// appear in the roster.
type PresenceIdentity struct {
	UserID      string
	DisplayName string
}

// PresenceMember is one entry in a topic's roster. DisplayName is the
// user's email (auth) or a synthesized guest label (anonymous).
type PresenceMember struct {
	UserID      string
	DisplayName string
}

// presenceUser is the minimal subset of battery/auth's User interface
// we type-assert against. Defining it locally avoids importing
// battery/auth (a battery the framework must not depend on) while still
// reading the trusted, middleware-seeded identity. Both battery/auth's
// concrete User types satisfy it.
type presenceUser interface {
	GetID() string
	GetEmail() string
}

// PresenceIdentityFromContext resolves the authenticated user from the
// request context into a PresenceIdentity. It is a READ of the ctx
// interface — it does not import or edit battery/auth. Returns the zero
// value (anonymous) when no user is present; callers that join a topic
// then get a synthesized pseudo-identity (see PresenceJoin).
//
// This is the security seam: identity comes from the middleware-seeded
// ctx value, NEVER from a query param or request body.
func PresenceIdentityFromContext(ctx context.Context) PresenceIdentity {
	raw, ok := handler.GetUser(ctx)
	if !ok || raw == nil {
		return PresenceIdentity{}
	}
	u, ok := raw.(presenceUser)
	if !ok {
		return PresenceIdentity{}
	}
	id := u.GetID()
	if id == "" {
		return PresenceIdentity{}
	}
	return PresenceIdentity{UserID: id, DisplayName: u.GetEmail()}
}

// Presence topic bounds — a hostile client supplies the topic string, so
// both its length and the count per connection are capped to prevent
// unbounded memory/bookkeeping.
const (
	maxPresenceTopics   = 16  // max distinct topics a single connection may join
	maxPresenceTopicLen = 128 // max bytes per topic string
)

// ParsePresenceTopics parses the client-supplied ?presence= query value
// into a bounded, de-duplicated list of topic strings. Empty / oversize
// / duplicate topics are dropped. At most maxPresenceTopics survive.
func ParsePresenceTopics(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || len(p) > maxPresenceTopicLen {
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) >= maxPresenceTopics {
			break
		}
	}
	return out
}

// presenceConn is one SSE connection's presence registration. Each
// ServeSSE call creates one; disconnect removes it. The roster is
// derived from the live set of connections (dedup by UserID at read
// time), so there is no manual ref-count to drift — closing the last
// tab of a user simply removes the last connection and the member
// drops.
type presenceConn struct {
	id        uint64 // unique within the manager
	sessionID string
	identity  PresenceIdentity
	topics    []string
}

func (c *presenceConn) hasTopic(topic string) bool {
	for _, t := range c.topics {
		if t == topic {
			return true
		}
	}
	return false
}

// PresenceHandle is returned by PresenceJoin and removed by Leave. It is
// safe to call Leave exactly once; a nil handle (returned when no topics
// were requested) is a no-op.
type PresenceHandle struct {
	manager *Manager
	id      uint64
	topics  []string
}

// Leave removes this connection from every topic it joined and fires
// roster-change notifications. Safe to call on a nil handle.
func (h *PresenceHandle) Leave() {
	if h == nil || h.manager == nil {
		return
	}
	m := h.manager
	m.mu.Lock()
	delete(m.presenceConns, h.id)
	cb := m.OnPresenceChange
	m.mu.Unlock()
	if cb != nil {
		for _, t := range h.topics {
			cb(t)
		}
	}
}

// anonIdentity synthesizes a stable pseudo-identity for an anonymous
// (unauthenticated) connection from its session id. The identity is
// server-derived — a client cannot choose it. The display name uses the
// trailing 4 chars (the session token's random tail), avoiding any fixed
// prefix the host may prepend (e.g. "sess-") so two viewers are visually
// distinct ("Guest 9f3d" vs "Guest a77e").
func anonIdentity(sessionID string) PresenceIdentity {
	tail := sessionID
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	return PresenceIdentity{
		UserID:      "anon:" + sessionID,
		DisplayName: "Guest " + tail,
	}
}

// PresenceJoin registers a connection on one or more topics and returns
// a handle whose Leave must be called on disconnect (ServeSSEWithPresence
// defers it). An empty identity (anonymous) is filled with a
// session-derived pseudo-identity so anonymous viewers still appear.
//
// OnPresenceChange (if set) is fired for each joined topic AFTER the
// connection is recorded and AFTER the lock is released, so the callback
// may safely call back into PresenceRoster / PresenceSessions / PushUpdate
// without deadlocking.
func (m *Manager) PresenceJoin(sessionID string, identity PresenceIdentity, topics []string) *PresenceHandle {
	if len(topics) == 0 {
		return nil // nothing to join — nothing to clean up
	}
	if identity.UserID == "" {
		identity = anonIdentity(sessionID)
	}
	id := atomic.AddUint64(&m.nextPresenceID, 1)
	conn := &presenceConn{
		id:        id,
		sessionID: sessionID,
		identity:  identity,
		topics:    append([]string(nil), topics...),
	}
	m.mu.Lock()
	if m.presenceConns == nil {
		m.presenceConns = make(map[uint64]*presenceConn)
	}
	m.presenceConns[id] = conn
	cb := m.OnPresenceChange
	m.mu.Unlock()

	if cb != nil {
		for _, t := range conn.topics {
			cb(t)
		}
	}
	return &PresenceHandle{manager: m, id: id, topics: conn.topics}
}

// PresenceRoster returns the connected identities for a topic, deduplicated
// by UserID and sorted deterministically (by UserID). It reflects THIS
// replica's connections only.
func (m *Manager) PresenceRoster(topic string) []PresenceMember {
	if topic == "" {
		return nil
	}
	m.mu.RLock()
	seen := make(map[string]string) // userID → displayName
	for _, c := range m.presenceConns {
		if c.identity.UserID == "" {
			continue
		}
		if c.hasTopic(topic) {
			if _, ok := seen[c.identity.UserID]; !ok {
				seen[c.identity.UserID] = c.identity.DisplayName
			}
		}
	}
	m.mu.RUnlock()

	members := make([]PresenceMember, 0, len(seen))
	for uid, name := range seen {
		members = append(members, PresenceMember{UserID: uid, DisplayName: name})
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].UserID < members[j].UserID
	})
	return members
}

// PresenceSessions returns the distinct session ids currently connected
// to a topic — the push targets for a live roster update. Sorted for
// deterministic test output.
func (m *Manager) PresenceSessions(topic string) []string {
	if topic == "" {
		return nil
	}
	m.mu.RLock()
	seen := make(map[string]bool)
	for _, c := range m.presenceConns {
		if c.hasTopic(topic) {
			seen[c.sessionID] = true
		}
	}
	m.mu.RUnlock()
	out := make([]string, 0, len(seen))
	for sid := range seen {
		out = append(out, sid)
	}
	sort.Strings(out)
	return out
}
