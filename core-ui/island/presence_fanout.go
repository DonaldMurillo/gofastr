package island

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// ─── Cross-replica presence ─────────────────────────────────────────
//
// The single-replica roster (presence.go) is extended to a MERGED roster:
// local connections ∪ live remote replicas, deduped by UserID exactly like
// the local dedup. Remote state arrives as full-roster announcements over a
// DEDICATED presence lane on the same [fanout.Fanout] transport — a parallel
// topic, NOT the island-invalidation lane ("gofastr.islands"). Both lanes
// remain lossy best-effort; presence is ephemeral self-healing state, so a
// dropped announcement simply heals on the next periodic heartbeat.
//
// CONVERGENCE MODEL — full-roster heartbeats, not deltas. Each replica
// broadcasts its full local roster per active topic on join/leave (prompt)
// and every presenceHeartbeat (reconvergence backstop). A receiver keeps a
// per-(replica,topic) entry with a TTL (~3× heartbeat); expired entries are
// swept, so a crashed replica's members disappear within TTL with no
// explicit "goodbye". Graceful stop() additionally publishes an empty roster
// so a rolling restart converges promptly.
//
// IDENTITY SAFETY — announcements carry ONLY the same server-derived
// {UserID, DisplayName} the local roster already exposes via PresenceRoster.
// No session id, no IP, no new surface ever leaves this replica. Proven by
// TestPresenceFanoutAnnouncementCarriesNoSessionID.
//
// ZERO-CONFIG DEGRADATION — no fanout attached ⇒ presenceSend is nil ⇒
// every broadcast/heartbeat is a no-op, remoteRosters stays nil, and
// PresenceRoster returns exactly the single-replica result (see
// TestPresenceFanoutNoFanoutByteIdentical). No goroutine is started.

// presenceFanoutTopic is the fanout channel dedicated to presence
// announcements. It is a SEPARATE lane from islandFanoutTopic
// ("gofastr.islands", which carries island-update invalidations) so the two
// message classes never share a payload shape. Both are lossy best-effort
// over the same [fanout.Fanout] transport.
const presenceFanoutTopic = "gofastr.presence"

// Presence convergence intervals. Tunable in tests via reconfigurePresence;
// production uses the defaults.
const (
	// defaultPresenceHeartbeat is the period between full-roster
	// rebroadcasts. Picked so a dropped announcement is healed within ~15s
	// without meaningful steady-state load (one small JSON per active topic).
	defaultPresenceHeartbeat = 15 * time.Second
	// defaultPresenceTTL is how long a remote replica's contribution stays
	// fresh without a heartbeat refresh. 3× heartbeat tolerates two missed
	// beats before expiry — a crashed replica's members vanish within TTL.
	defaultPresenceTTL = 45 * time.Second
	// maxRemoteReplicasPerTopic bounds the remote-roster table per topic.
	// A misbehaving peer could forge many replica ids to bloat the table;
	// this cap drops new ones once exceeded (the lossy model tolerates it —
	// the legitimate replica's next heartbeat reclaims its slot only when a
	// stale forged id expires).
	maxRemoteReplicasPerTopic = 512
	// presenceGraceTimeout bounds the synchronous graceful-leave Publish
	// issued on stop() so a rolling restart converges promptly. Lossy lane:
	// a timeout here just falls back to TTL-based expiry.
	presenceGraceTimeout = 2 * time.Second
)

// presenceFanoutMsg is the wire shape of a presence announcement. It carries
// a replica's FULL local roster for one topic — full beats (not deltas) are
// the convergence mechanism, so a missed message heals on the next beat.
// Members is the same server-derived identity the local roster exposes (see
// PresenceMember); no session id or anything not already in PresenceRoster
// output ever leaves this replica. SentAt is the origin's clock in unix
// milliseconds (diagnostic only; freshness is driven by the receiver's TTL,
// not by clock skew).
type presenceFanoutMsg struct {
	ReplicaID string           `json:"r"`
	Topic     string           `json:"t"`
	Members   []PresenceMember `json:"m"`
	SentAt    int64            `json:"s"`
}

// remoteRosterEntry is one remote replica's contributed roster for a topic,
// plus the deadline by which it expires unless refreshed by a heartbeat.
type remoteRosterEntry struct {
	members   []PresenceMember
	expiresAt time.Time
}

// broadcastLocalTopic publishes this replica's full local roster for one
// topic over the presence lane. Called on local join/leave so peers learn
// the new state promptly; the heartbeat re-broadcasts all topics as the
// lossy reconvergence backstop. No-op when no presence fanout is attached.
func (m *Manager) broadcastLocalTopic(topic string) {
	if topic == "" {
		return
	}
	m.mu.RLock()
	send, nodeID, done := m.presenceSend, m.nodeID, m.presenceDone
	if send == nil || done == nil {
		m.mu.RUnlock()
		return
	}
	members := localRosterLocked(m, topic)
	m.mu.RUnlock()
	m.publishPresence(send, nodeID, topic, members)
}

// publishPresence marshals and stamps one announcement through the (async,
// non-blocking) presence send. Marshal of a plain struct cannot fail in a
// way the program can recover from; a failure is silently dropped (lossy).
func (m *Manager) publishPresence(send func([]byte), nodeID, topic string, members []PresenceMember) {
	msg := presenceFanoutMsg{ReplicaID: nodeID, Topic: topic, Members: members, SentAt: time.Now().UnixMilli()}
	body, err := json.Marshal(msg)
	if err != nil {
		return
	}
	send(fanout.Wrap(nodeID, body))
}

// broadcastAllLocalTopics re-publishes every active local topic's roster —
// the periodic heartbeat that reconverges after any dropped announcement.
func (m *Manager) broadcastAllLocalTopics() {
	m.mu.RLock()
	send, nodeID, done := m.presenceSend, m.nodeID, m.presenceDone
	if send == nil || done == nil {
		m.mu.RUnlock()
		return
	}
	topics := localTopicsLocked(m)
	snapshots := make(map[string][]PresenceMember, len(topics))
	for _, t := range topics {
		snapshots[t] = localRosterLocked(m, t)
	}
	m.mu.RUnlock()
	for _, t := range topics {
		m.publishPresence(send, nodeID, t, snapshots[t])
	}
}

// gracefulLeaveLocalTopics publishes an empty-roster announcement for every
// topic this replica currently holds, so peers drop it PROMPTLY on a rolling
// restart / graceful stop — without waiting for TTL. Synchronous (direct
// Publish) rather than the async queue so the leave is not lost to the
// queue's drop-on-stop; TTL remains the crash fallback. Skipped when the
// heartbeat has been halted (crash sim: a crashed process sends nothing).
func (m *Manager) gracefulLeaveLocalTopics(f fanout.Fanout) {
	m.mu.RLock()
	nodeID, done := m.nodeID, m.presenceDone
	if done == nil {
		m.mu.RUnlock()
		return
	}
	topics := localTopicsLocked(m)
	m.mu.RUnlock()
	for _, t := range topics {
		msg := presenceFanoutMsg{ReplicaID: nodeID, Topic: t, Members: nil, SentAt: time.Now().UnixMilli()}
		body, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), presenceGraceTimeout)
		_ = f.Publish(ctx, presenceFanoutTopic, fanout.Wrap(nodeID, body))
		cancel()
	}
}

// mergeRemotePresence integrates an inbound announcement into the remote
// roster table and fires OnPresenceChange if the MERGED roster for the topic
// changed. An empty Members slice is a graceful leave → the origin's entry is
// deleted (and the change propagated if the merged roster shrank). Called on
// the fanout subscriber goroutine; never re-publishes (no loop — own-node
// envelopes are dropped before this is reached).
func (m *Manager) mergeRemotePresence(origin string, msg presenceFanoutMsg) {
	if msg.Topic == "" || origin == "" {
		return
	}
	now := time.Now()
	m.mu.Lock()
	// The presence subscriber is cancelled asynchronously in stop(); an
	// in-flight callback may run after clearPresenceFanoutLocked has nilled
	// remoteRosters. A nil table means the lane is torn down — drop silently.
	if m.remoteRosters == nil {
		m.mu.Unlock()
		return
	}
	reps := m.remoteRosters[msg.Topic]
	if reps == nil {
		reps = make(map[string]remoteRosterEntry)
		m.remoteRosters[msg.Topic] = reps
	}
	before := mergedRosterLocked(m, msg.Topic, now)
	if len(msg.Members) == 0 {
		delete(reps, origin)
	} else if _, exists := reps[origin]; !exists && len(reps) >= maxRemoteReplicasPerTopic {
		// Cap reached for a NEW replica: drop the announcement (lossy). The
		// legitimate replica reconverges once a stale forged id is swept.
	} else {
		reps[origin] = remoteRosterEntry{members: dedupMembers(msg.Members), expiresAt: now.Add(m.presenceTTL)}
	}
	if len(reps) == 0 {
		delete(m.remoteRosters, msg.Topic)
	}
	after := mergedRosterLocked(m, msg.Topic, now)
	cb := m.OnPresenceChange
	m.mu.Unlock()
	if !membersEqual(before, after) && cb != nil {
		cb(msg.Topic)
	}
}

// sweepExpiredRemote drops remote-roster entries past their TTL and returns
// the topics whose merged roster shrank as a result. The caller fires
// OnPresenceChange for those so local viewers see the departed members drop
// (a crashed replica's members vanish here, within TTL of its last beat).
func (m *Manager) sweepExpiredRemote() []string {
	now := time.Now()
	var changed []string
	m.mu.Lock()
	for topic, reps := range m.remoteRosters {
		before := mergedRosterLocked(m, topic, now)
		for origin, entry := range reps {
			if now.After(entry.expiresAt) {
				delete(reps, origin)
			}
		}
		if len(reps) == 0 {
			delete(m.remoteRosters, topic)
		}
		after := mergedRosterLocked(m, topic, now)
		if !membersEqual(before, after) {
			changed = append(changed, topic)
		}
	}
	m.mu.Unlock()
	return changed
}

// presenceBeat is one heartbeat tick: sweep expired remotes, re-broadcast
// all local topics, and fire OnPresenceChange for any topic whose merged
// roster changed due to expiry.
func (m *Manager) presenceBeat() {
	changed := m.sweepExpiredRemote()
	m.broadcastAllLocalTopics()
	if len(changed) == 0 {
		return
	}
	m.mu.RLock()
	cb := m.OnPresenceChange
	m.mu.RUnlock()
	if cb != nil {
		for _, t := range changed {
			cb(t)
		}
	}
}

// presenceHeartbeatLoop is the per-replica presence goroutine: every
// presenceHeartbeat it runs presenceBeat. It exits when its CAPTURED done
// channel is closed. done is passed in (not re-read from the field) so that
// reconfigurePresence/haltPresenceHeartbeat can close the channel THIS
// goroutine is bound to — re-reading m.presenceDone each tick would race
// with reconfigure swapping in a fresh channel and leave the old goroutine
// waiting on the wrong one (a real deadlock this guard prevents). The
// interval is read fresh each tick so a live change applies on the next
// beat without restarting.
func (m *Manager) presenceHeartbeatLoop(done chan struct{}) {
	defer m.presenceWG.Done()
	for {
		m.mu.RLock()
		hb := m.presenceHeartbeat
		m.mu.RUnlock()
		if hb <= 0 {
			return
		}
		select {
		case <-done:
			return
		case <-time.After(hb):
			m.presenceBeat()
		}
	}
}

// reconfigurePresence changes the heartbeat/TTL intervals live. It swaps in
// a fresh done channel, closes the one the running goroutine captured (so
// that goroutine exits promptly), waits for it, then starts a new goroutine
// bound to the fresh channel — so the new interval takes effect immediately
// rather than waiting out the current sleep. Test-only knob; production runs
// on the defaults set by SetFanout.
func (m *Manager) reconfigurePresence(heartbeat, ttl time.Duration) {
	m.mu.Lock()
	oldDone := m.presenceDone
	m.presenceHeartbeat = heartbeat
	m.presenceTTL = ttl
	var newDone chan struct{}
	if oldDone != nil {
		newDone = make(chan struct{})
		m.presenceDone = newDone
	}
	m.mu.Unlock()
	if oldDone == nil {
		return // no fanout attached yet; values apply on next SetFanout
	}
	close(oldDone)
	m.presenceWG.Wait()
	m.presenceWG.Add(1)
	go m.presenceHeartbeatLoop(newDone)
}

// haltPresenceHeartbeat stops the heartbeat goroutine WITHOUT sending a
// graceful leave — a faithful simulation of a crashed (silent) replica so
// tests can exercise TTL-based expiry. Test-only. The goroutine exits because
// it captured the channel we close here. After halt, presenceDone is nil so
// every broadcast/gracefulLeave is a no-op (a crashed process sends
// nothing); the receiver subscription is left running (harmless). A
// subsequent stop() finishes teardown.
func (m *Manager) haltPresenceHeartbeat() {
	m.mu.Lock()
	done := m.presenceDone
	m.presenceDone = nil // guards broadcast* / gracefulLeave (done == nil ⇒ skip)
	m.mu.Unlock()
	if done != nil {
		close(done)
	}
	m.presenceWG.Wait()
}

// ─── roster helpers (lock-held) ─────────────────────────────────────

// localRosterLocked returns this replica's LOCAL-only roster for a topic
// (deduped by UserID, sorted). Caller holds mu. This is the data broadcast
// to peers; it never includes remote contributions.
func localRosterLocked(m *Manager, topic string) []PresenceMember {
	seen := make(map[string]string)
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
	return buildSortedMembers(seen)
}

// localTopicsLocked returns the distinct topic strings this replica currently
// holds local connections on, sorted for deterministic broadcast order.
func localTopicsLocked(m *Manager) []string {
	seen := make(map[string]bool)
	for _, c := range m.presenceConns {
		for _, t := range c.topics {
			seen[t] = true
		}
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// mergedRosterLocked returns the deduped, sorted union of LOCAL and remote
// (unexpired) members for a topic. This is the heart of PresenceRoster.
// Caller holds mu. Expired remote entries are filtered at read time so a
// roster read between sweeps never surfaces stale members.
func mergedRosterLocked(m *Manager, topic string, now time.Time) []PresenceMember {
	seen := make(map[string]string)
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
	if reps, ok := m.remoteRosters[topic]; ok {
		for _, entry := range reps {
			if now.After(entry.expiresAt) {
				continue
			}
			for _, mem := range entry.members {
				if _, ok := seen[mem.UserID]; !ok {
					seen[mem.UserID] = mem.DisplayName
				}
			}
		}
	}
	return buildSortedMembers(seen)
}

// buildSortedMembers turns a userID→displayName map into the deterministic
// []PresenceMember shape PresenceRoster has always returned (sorted by
// UserID). Shared by local and merged rosters.
func buildSortedMembers(seen map[string]string) []PresenceMember {
	out := make([]PresenceMember, 0, len(seen))
	for uid, name := range seen {
		out = append(out, PresenceMember{UserID: uid, DisplayName: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
}

// dedupMembers removes duplicate UserIDs from an inbound announcement's
// member list (defensive — the local roster is already deduped, but the wire
// format is not trusted to be deduped). First occurrence wins.
func dedupMembers(in []PresenceMember) []PresenceMember {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]PresenceMember, 0, len(in))
	for _, mem := range in {
		if mem.UserID == "" || seen[mem.UserID] {
			continue
		}
		seen[mem.UserID] = true
		out = append(out, mem)
	}
	return out
}

// membersEqual reports whether two sorted rosters are identical by value.
// Used to suppress redundant OnPresenceChange fires when a heartbeat or
// duplicate announcement doesn't actually change membership.
func membersEqual(a, b []PresenceMember) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].UserID != b[i].UserID || a[i].DisplayName != b[i].DisplayName {
			return false
		}
	}
	return true
}
