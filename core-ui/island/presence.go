package island

import (
	"context"
	"log"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// PresenceIdentity is the server-derived identity of a connected viewer.
type PresenceIdentity struct {
	UserID      string
	DisplayName string
}

// PresenceMember is one entry in a topic's roster.
type PresenceMember struct {
	UserID      string
	DisplayName string
}

type presenceUser interface {
	GetID() string
	GetEmail() string
}

// PresenceIdentityFromContext resolves the authenticated user from the request context into a
// PresenceIdentity.
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

// Bounds on the client-supplied presence topics.
const (
	maxPresenceTopics   = 16  // max distinct topics a single connection may join
	maxPresenceTopicLen = 128 // max bytes per topic string
)

// ParsePresenceTopics parses the client-supplied ?presence= query value into a bounded, de-
// duplicated list of topic strings.
func ParsePresenceTopics(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return boundTopics(strings.Split(raw, ","))
}

func boundTopics(parts []string) []string {
	out := make([]string, 0, min(len(parts), maxPresenceTopics))
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

type presenceConn struct {
	id        uint64 // unique within the manager
	sessionID string
	identity  PresenceIdentity
	topics    []string
}

func (c *presenceConn) hasTopic(topic string) bool {
	return slices.Contains(c.topics, topic)
}

// PresenceHandle is returned by PresenceJoin and removed by Leave.
type PresenceHandle struct {
	manager *Manager
	id      uint64
	topics  []string
}

// Leave removes this connection from every topic it joined and fires roster-change notifications,
// then announces the shrunk local roster to other replicas (no-op without a fanout).
func (h *PresenceHandle) Leave() {
	if h == nil || h.manager == nil {
		return
	}
	m := h.manager
	m.mu.Lock()
	delete(m.presenceConns, h.id)
	cb := m.presenceCallback()
	m.mu.Unlock()
	firePresenceChange(cb, h.topics)
	for _, t := range h.topics {
		m.broadcastLocalTopic(t)
	}
}

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

// PresenceTopics is what one SSE connection asks to join.
type PresenceTopics struct {
	Declared  []string
	Requested []string
}

func (m *Manager) admitTopics(ctx context.Context, pt PresenceTopics) []string {
	if m.topicAuthorizer() == nil {
		return boundTopics(pt.Declared)
	}
	all := make([]string, 0, len(pt.Declared)+len(pt.Requested))
	all = append(append(all, pt.Declared...), pt.Requested...)
	return m.filterAuthorizedTopics(ctx, boundTopics(all))
}

func (m *Manager) filterAuthorizedTopics(ctx context.Context, topics []string) []string {
	if len(topics) == 0 {
		return topics
	}
	auth := m.topicAuthorizer()
	if auth == nil {
		return topics
	}
	out := make([]string, 0, len(topics))
	for _, t := range topics {
		//gofastr:allow(recovercallback) both callers run on ServeSSEWithPresence's request goroutine, its connect gate and its stream loop's re-check, so net/http's per-request recover ends the stream, not the process
		if auth(ctx, t) {
			out = append(out, t)
		}
	}
	return out
}

// PresenceJoin registers a connection on one or more topics and returns a handle whose Leave must
// be called on disconnect (ServeSSEWithPresence defers it).
func (m *Manager) PresenceJoin(sessionID string, identity PresenceIdentity, topics []string) *PresenceHandle {
	if len(topics) == 0 {
		return nil // nothing to join, nothing to clean up
	}
	if identity.UserID == "" {
		identity = anonIdentity(sessionID)
	}
	id := m.nextPresenceID.Add(1)
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
	cb := m.presenceCallback()
	m.mu.Unlock()

	firePresenceChange(cb, conn.topics)
	for _, t := range conn.topics {
		m.broadcastLocalTopic(t)
	}
	return &PresenceHandle{manager: m, id: id, topics: conn.topics}
}

// PresenceRoster returns the connected identities for a topic, deduplicated by UserID and sorted
// deterministically (by UserID).
func (m *Manager) PresenceRoster(topic string) []PresenceMember {
	if topic == "" {
		return nil
	}
	m.mu.RLock()
	members := mergedRosterLocked(m, topic, time.Now())
	m.mu.RUnlock()
	return members
}

// PresenceSessions returns the distinct session ids currently connected to a topic, the push
// targets for a live roster update.
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

func firePresenceChange(cb func(string), topics []string) {
	if cb == nil {
		return
	}
	for _, t := range topics {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("island: OnPresenceChange panic for topic %q: %s\n%s", t, textsafe.Recovered(r), debug.Stack())
				}
			}()
			cb(t)
		}()
	}
}
