package island

import (
	"context"
	"log"
	"runtime/debug"
	"sort"
	"strings"
	"sync/atomic"
	"time"

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
// CROSS-REPLICA: the roster is the MERGED set — THIS replica's connections
// ∪ every other live replica's announced members (see presence_fanout.go).
// Each replica broadcasts its full local roster per topic over a dedicated
// presence lane on the fanout transport; a remote-roster table keyed by
// (replica, topic) with a TTL merges them. Lossy and self-healing: a
// dropped announcement reconverges on the next periodic heartbeat, and a
// crashed replica's members vanish within the TTL.

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
// roster-change notifications, then announces the shrunk local roster to
// other replicas (no-op without a fanout). Safe to call on a nil handle.
func (h *PresenceHandle) Leave() {
	if h == nil || h.manager == nil {
		return
	}
	m := h.manager
	m.mu.Lock()
	delete(m.presenceConns, h.id)
	cb := m.OnPresenceChange
	m.mu.Unlock()
	firePresenceChange(cb, h.topics)
	// Announce the updated local roster so peers drop this member promptly.
	for _, t := range h.topics {
		m.broadcastLocalTopic(t)
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

// filterAuthorizedTopics returns the subset of topics the AuthorizeTopic hook
// permits for ctx. A nil hook (the default) authorizes everything — presence
// is public unless an app opts into gating. Rejected topics are dropped
// silently; no error distinguishes an unauthorized topic from a nonexistent
// one, so the filter cannot be used as a private-topic existence oracle.
// Called at SSE-connect time (ServeSSEWithPresence) BEFORE PresenceJoin, so an
// unauthorized topic never produces a subscription or a roster entry.
func (m *Manager) filterAuthorizedTopics(ctx context.Context, topics []string) []string {
	if len(topics) == 0 {
		return topics
	}
	// Read the hook under the lock — matching how PresenceJoin reads
	// OnPresenceChange — then call it OUTSIDE the lock so an app hook may
	// safely read the roster (PresenceRoster/PresenceSessions) without
	// deadlocking. AuthorizeTopic is expected set-once before serving
	// traffic (see the field doc); the locked read keeps a concurrent
	// hot-swap from being a torn read.
	m.mu.RLock()
	auth := m.AuthorizeTopic
	m.mu.RUnlock()
	if auth == nil {
		return topics
	}
	out := make([]string, 0, len(topics))
	for _, t := range topics {
		if auth(ctx, t) {
			out = append(out, t)
		}
	}
	return out
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

	firePresenceChange(cb, conn.topics)
	// Announce the grown local roster so peers add this member promptly.
	// No-op without a fanout; the periodic heartbeat reconverges any drop.
	for _, t := range conn.topics {
		m.broadcastLocalTopic(t)
	}
	return &PresenceHandle{manager: m, id: id, topics: conn.topics}
}

// PresenceRoster returns the connected identities for a topic, deduplicated
// by UserID and sorted deterministically (by UserID). The result is the
// MERGED roster: THIS replica's connections ∪ every other live replica's
// announced members, deduped by UserID exactly like the local dedup. With
// no fanout attached (no SetFanout) remoteRosters is nil and the result is
// byte-identical to the single-replica roster. Expired remote entries are
// filtered at read time so a roster read between heartbeats never surfaces
// stale members. See presence_fanout.go for the cross-replica model.
func (m *Manager) PresenceRoster(topic string) []PresenceMember {
	if topic == "" {
		return nil
	}
	m.mu.RLock()
	members := mergedRosterLocked(m, topic, time.Now())
	m.mu.RUnlock()
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

// firePresenceChange invokes the app-supplied OnPresenceChange hook for
// each topic, recovering a panicking hook. The roster mutation has
// already happened by the time the hook fires; letting an app callback
// panic escape here would strand half-registered presence state (join
// returns no handle) or skip the cross-replica announcement (leave).
// Same recover-and-log posture as Island.Render.
func firePresenceChange(cb func(string), topics []string) {
	if cb == nil {
		return
	}
	for _, t := range topics {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("island: OnPresenceChange panic for topic %q: %v\n%s", t, r, debug.Stack())
				}
			}()
			cb(t)
		}()
	}
}
