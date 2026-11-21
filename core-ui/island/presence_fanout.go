package island

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

const presenceFanoutTopic = "gofastr.presence"

// Presence convergence intervals and the bounds on what a peer may announce.
const (
	defaultPresenceHeartbeat   = 15 * time.Second
	defaultPresenceTTL         = 45 * time.Second
	maxRemoteReplicasPerTopic  = 512
	maxRemoteRosterTopics      = 1024
	maxRemoteMembersPerReplica = 4096
	presenceGraceTimeout       = 2 * time.Second
)

type presenceFanoutMsg struct {
	ReplicaID string           `json:"r"`
	Topic     string           `json:"t"`
	Members   []PresenceMember `json:"m"`
	SentAt    int64            `json:"s"`
}

type remoteRosterEntry struct {
	members   []PresenceMember
	expiresAt time.Time
}

func (m *Manager) broadcastLocalTopic(topic string) {
	if topic == "" {
		return
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	send, nodeID, done := m.presenceSend, m.nodeID, m.presenceDone
	if send == nil || done == nil {
		return
	}
	m.publishPresence(send, nodeID, topic, localRosterLocked(m, topic))
}

func (m *Manager) publishPresence(send func([]byte), nodeID, topic string, members []PresenceMember) {
	msg := presenceFanoutMsg{ReplicaID: nodeID, Topic: topic, Members: members, SentAt: time.Now().UnixMilli()}
	body, err := json.Marshal(msg)
	if err != nil {
		return
	}
	send(fanout.Wrap(nodeID, body))
}

func (m *Manager) broadcastAllLocalTopics() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	send, nodeID, done := m.presenceSend, m.nodeID, m.presenceDone
	if send == nil || done == nil {
		return
	}
	for _, t := range localTopicsLocked(m) {
		m.publishPresence(send, nodeID, t, localRosterLocked(m, t))
	}
}

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

func (m *Manager) mergeRemotePresence(origin string, msg presenceFanoutMsg) {
	if msg.Topic == "" || origin == "" {
		return
	}
	now := time.Now()
	m.mu.Lock()
	if m.remoteRosters == nil {
		m.mu.Unlock()
		return
	}
	reps := m.remoteRosters[msg.Topic]
	evicted := ""
	if reps == nil && len(m.remoteRosters) >= maxRemoteRosterTopics {
		var ok bool
		evicted, ok = m.reclaimExpiredRemoteTopicLocked(now)
		if !ok {
			m.mu.Unlock()
			return
		}
	}
	m.remoteRosterTouch[msg.Topic] = now
	if reps == nil {
		reps = make(map[string]remoteRosterEntry)
		m.remoteRosters[msg.Topic] = reps
	}
	before := shownRosterLocked(m, msg.Topic)
	if len(msg.Members) == 0 {
		delete(reps, origin)
	} else if _, exists := reps[origin]; exists || len(reps) < maxRemoteReplicasPerTopic {
		members := dedupMembers(msg.Members)
		if len(members) > maxRemoteMembersPerReplica {
			members = members[:maxRemoteMembersPerReplica]
		}
		reps[origin] = remoteRosterEntry{members: members, expiresAt: now.Add(m.presenceTTL)}
	}
	if len(reps) == 0 {
		delete(m.remoteRosters, msg.Topic)
		delete(m.remoteRosterTouch, msg.Topic)
	}
	after := mergedRosterLocked(m, msg.Topic, now)
	cb := m.presenceCallback()
	m.mu.Unlock()
	var changed []string
	if evicted != "" {
		changed = append(changed, evicted)
	}
	if !membersEqual(before, after) {
		changed = append(changed, msg.Topic)
	}
	firePresenceChange(cb, changed)
}

func (m *Manager) reclaimExpiredRemoteTopicLocked(now time.Time) (evicted string, ok bool) {
	stalest := ""
	var stalestAt time.Time
	for topic, touched := range m.remoteRosterTouch {
		if !remoteTopicExpiredLocked(m, topic, now) {
			continue
		}
		if stalest == "" || touched.Before(stalestAt) {
			stalest, stalestAt = topic, touched
		}
	}
	if stalest == "" {
		return "", false
	}
	before := shownRosterLocked(m, stalest)
	delete(m.remoteRosters, stalest)
	delete(m.remoteRosterTouch, stalest)
	if len(before) == 0 {
		return "", true
	}
	return stalest, true
}

func remoteTopicExpiredLocked(m *Manager, topic string, now time.Time) bool {
	for _, entry := range m.remoteRosters[topic] {
		if !now.After(entry.expiresAt) {
			return false
		}
	}
	return true
}

func (m *Manager) sweepExpiredRemote() []string {
	now := time.Now()
	var changed []string
	m.mu.Lock()
	for topic, reps := range m.remoteRosters {
		before := shownRosterLocked(m, topic)
		for origin, entry := range reps {
			if now.After(entry.expiresAt) {
				delete(reps, origin)
			}
		}
		if len(reps) == 0 {
			delete(m.remoteRosters, topic)
			delete(m.remoteRosterTouch, topic)
		}
		after := mergedRosterLocked(m, topic, now)
		if !membersEqual(before, after) {
			changed = append(changed, topic)
		}
	}
	m.mu.Unlock()
	return changed
}

func (m *Manager) presenceBeat() {
	changed := m.sweepExpiredRemote()
	m.broadcastAllLocalTopics()
	if len(changed) == 0 {
		return
	}
	m.mu.RLock()
	cb := m.presenceCallback()
	m.mu.RUnlock()
	firePresenceChange(cb, changed)
}

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
		return // no fanout attached yet; SetFanout resets both to the defaults
	}
	close(oldDone)
	m.presenceWG.Wait()
	m.presenceWG.Add(1)
	go m.presenceHeartbeatLoop(newDone)
}

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

func localSeenLocked(m *Manager, topic string) map[string]string {
	seen := make(map[string]string)
	for _, c := range m.presenceConns {
		if c.identity.UserID == "" || !c.hasTopic(topic) {
			continue
		}
		if _, ok := seen[c.identity.UserID]; !ok {
			seen[c.identity.UserID] = c.identity.DisplayName
		}
	}
	return seen
}

func localRosterLocked(m *Manager, topic string) []PresenceMember {
	return buildSortedMembers(localSeenLocked(m, topic))
}

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

func shownRosterLocked(m *Manager, topic string) []PresenceMember {
	return mergedRosterLocked(m, topic, time.Time{})
}

func mergedRosterLocked(m *Manager, topic string, now time.Time) []PresenceMember {
	seen := localSeenLocked(m, topic)
	if reps, ok := m.remoteRosters[topic]; ok {
		for _, entry := range reps {
			if !now.IsZero() && now.After(entry.expiresAt) {
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

func buildSortedMembers(seen map[string]string) []PresenceMember {
	out := make([]PresenceMember, 0, len(seen))
	for uid, name := range seen {
		out = append(out, PresenceMember{UserID: uid, DisplayName: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
}

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
