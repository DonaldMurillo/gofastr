package island

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// Pins: the cross-replica remote-roster table is bounded in every dimension
// (distinct topics, members per (topic, replica) entry, replicas per topic),
// and the client-supplied SSE session id is length-bounded like every other
// hostile-client key. (2026-09-06 adversarial pass, round 5.)
// Property: (a) the cross-replica remote-roster table is bounded against a
// misbehaving fanout peer in every dimension — maxRemoteReplicasPerTopic's own
// comment names the forged-peer threat, yet only replica-ids-per-topic is
// capped (512 today); distinct topic count and per-announcement member count
// are unchecked. (b) a client-supplied SSE session id is bounded like every
// other hostile-client key — presence topics are capped at 16x128 bytes with
// the identical rationale, but ?session= is accepted at any length.
//
// Surfaces: presence_fanout.go:mergeRemotePresence (topic count/length
// unchecked; dedupMembers caps nothing; sustained beats refresh entries
// forever, so a flooding peer's allocation is durable) and
// stream.go:ServeSSEWithPresence (only the EMPTY session id is rejected;
// manager.go:subscribeImpl then holds the arbitrary-length key in m.streams
// for the stream's lifetime — default 4096 global streams x a 1MiB key ≈ 4GiB
// pinnable by one unauthenticated client).
//
// Finding: probes confirmed 2048 forged one-member topics and a single
// 100_000-member announcement are all stored verbatim today, and a 1MiB
// ?session= value subscribes and streams normally.
//
// Fix direction: bound the remote-roster table like the roster caps that
// already exist — cap distinct topics (e.g. 1024, an LRU is fine too) and
// members per (topic, replica) entry at 4096 (mirrors DefaultGlobalStreams: a
// legitimate replica cannot announce more members on one topic than it holds
// streams); reject an over-long ?session= next to the empty-id check the same
// way ParsePresenceTopics drops over-long topics.

// TestRosterRedBoundsForgedPeer floods mergeRemotePresence the way a
// misbehaving fanout peer can (the lane is not authenticated) and asserts the
// remote-roster table stays bounded; a legit small roster still merges.
func TestRosterRedBoundsForgedPeer(t *testing.T) {
	m := NewManager()
	stop, err := m.SetFanout(fanout.NewInProcess()) // remoteRosters must be live for mergeRemotePresence
	if err != nil {
		t.Fatal("setup broken: SetFanout: " + err.Error())
	}
	defer stop()

	// Leg 1: one peer forges 2048 distinct topics, one member each.
	for i := range 2048 {
		m.mergeRemotePresence("peer-1", presenceFanoutMsg{
			Topic:   fmt.Sprintf("forged-%d", i),
			Members: []PresenceMember{{UserID: "u0", DisplayName: "forged"}},
		})
	}
	const wantMaxTopics = 1024 // mirrors maxPresenceTopicLen discipline: presence keys from peers are bounded keys
	m.mu.RLock()
	topics := len(m.remoteRosters)
	m.mu.RUnlock()
	if topics > wantMaxTopics {
		t.Errorf("SECURITY: [island-roster-unbounded] a misbehaving fanout peer planted %d distinct topics in remoteRosters (cap should be %d, mirroring maxPresenceTopics/maxPresenceTopicLen discipline; maxRemoteReplicasPerTopic=512 already names the forged-peer threat this table must bound) — sustained heartbeats refresh every entry past any TTL", topics, wantMaxTopics)
	}

	// Leg 2: one announcement with 100_000 members (64-byte ids, built once).
	members := make([]PresenceMember, 100_000)
	for i := range members {
		members[i] = PresenceMember{UserID: fmt.Sprintf("u%063d", i), DisplayName: "n"}
	}
	m.mergeRemotePresence("peer-1", presenceFanoutMsg{Topic: "forged-big", Members: members})
	const wantMaxMembers = 4096 // mirrors DefaultGlobalStreams: a legit replica cannot have more members on one topic than streams
	m.mu.RLock()
	stored := 0
	if entry, ok := m.remoteRosters["forged-big"]["peer-1"]; ok {
		stored = len(entry.members)
	}
	m.mu.RUnlock()
	if stored > wantMaxMembers {
		t.Errorf("SECURITY: [island-roster-unbounded] a single forged announcement planted %d members into one (topic, replica) entry (cap should be %d, mirroring DefaultGlobalStreams — a legit replica holds at most that many streams per topic); dedupMembers dedups but caps nothing", stored, wantMaxMembers)
	}

	// Positive control: a legit small roster still merges and is visible.
	m.mergeRemotePresence("legit-1", presenceFanoutMsg{
		Topic:   "doc:1",
		Members: []PresenceMember{{UserID: "u1", DisplayName: "Alice"}},
	})
	if roster := m.PresenceRoster("doc:1"); len(roster) != 1 || roster[0].UserID != "u1" {
		t.Errorf("positive control: legit small roster must still merge, got %v", roster)
	}
}

// TestSSESessionRedKeyBounded asserts an over-long client-supplied ?session=
// value is refused like the empty id (the key is pinned in m.streams for the
// stream's lifetime), while a normal 32-byte session still connects.
func TestSSESessionRedKeyBounded(t *testing.T) {
	m := NewManager()

	// serve runs one SSE request against a recorder on a peer that has already
	// disconnected: the loop's context.Canceled path reclaims the stream
	// promptly, so the handler returns without waiting out heartbeat or bound.
	serve := func(session string) (*httptest.ResponseRecorder, error) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := httptest.NewRequest(http.MethodGet, "/islands/sse?session="+session, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		done := make(chan struct{})
		go func() { m.ServeSSE(rec, req); close(done) }()
		select {
		case <-done:
			return rec, nil
		case <-time.After(5 * time.Second):
			return nil, fmt.Errorf("ServeSSE still running after 5s (session len %d)", len(session))
		}
	}

	rec, err := serve(strings.Repeat("x", 1<<20))
	if err != nil {
		t.Fatal("setup broken: " + err.Error())
	}
	if rec.Code < 400 {
		t.Errorf("SECURITY: [island-sse-session-key] a 1MiB ?session= value must be refused like the empty-id case (ServeSSEWithPresence only rejects the empty string; subscribeImpl then pins the arbitrary-length key in m.streams for the stream's lifetime — default 4096 global streams x 1MiB ≈ 4GiB from one unauthenticated client), got status %d and a live stream", rec.Code)
	}

	// Positive control: a 32-byte session id still connects.
	rec, err = serve(strings.Repeat("s", 32))
	if err != nil {
		t.Fatal("setup broken: " + err.Error())
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "connected") {
		t.Errorf("positive control: a 32-byte session id must still connect (200 + connected comment), got %d body %q", rec.Code, rec.Body.String())
	}
}
