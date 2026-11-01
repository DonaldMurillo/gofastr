package island

import (
	"strconv"
	"testing"
	"time"
)

// fillRemoteTopics fills the remote-roster table to maxRemoteRosterTopics
// live topics from replica r1, t:0 the least recently refreshed.
func fillRemoteTopics(m *Manager, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range maxRemoteRosterTopics {
		topic := "t:" + strconv.Itoa(i)
		m.remoteRosters[topic] = map[string]remoteRosterEntry{
			"r1": {members: []PresenceMember{{UserID: "u" + strconv.Itoa(i)}}, expiresAt: now.Add(time.Hour)},
		}
		m.remoteRosterTouch[topic] = now.Add(-time.Duration(maxRemoteRosterTopics-i) * time.Second)
	}
}

// Once the remote topic table was full, an announcement for a NEW topic
// evicted the least recently refreshed one, live or not: a flood of
// one-member topics re-announced every heartbeat cycled every live topic
// out and blanked cross-replica presence (2026-10-31 O61). A live topic now
// keeps its slot; the new topic is dropped until one expires.
func TestTopicCapKeepsLiveTopics(t *testing.T) {
	m, stop := fanoutManager(t)
	defer stop()
	now := time.Now()
	fillRemoteTopics(m, now)
	var fired []string
	m.SetOnPresenceChange(func(topic string) { fired = append(fired, topic) })

	for i := range 64 {
		m.mergeRemotePresence("flood", presenceFanoutMsg{ReplicaID: "flood", Topic: "x:" + strconv.Itoa(i), Members: []PresenceMember{{UserID: "f"}}})
	}

	for i := range maxRemoteRosterTopics {
		topic := "t:" + strconv.Itoa(i)
		if got := m.PresenceRoster(topic); len(got) != 1 {
			t.Fatalf("SECURITY: live topic %s lost its remote member to the flood (roster %v)", topic, got)
		}
	}
	if got := m.PresenceRoster("x:0"); len(got) != 0 {
		t.Errorf("a new topic past the cap was admitted: %v", got)
	}
	if len(fired) != 0 {
		t.Errorf("OnPresenceChange fired for %v; nothing viewers see changed", fired)
	}
	m.mu.RLock()
	n := len(m.remoteRosters)
	m.mu.RUnlock()
	if n > maxRemoteRosterTopics {
		t.Errorf("remote table grew to %d topics past the cap %d", n, maxRemoteRosterTopics)
	}
}

// A topic with one expired replica and one live one is still live: it is
// not reclaimed for a new topic.
func TestTopicCapReclaimsOnlyExpired(t *testing.T) {
	m, stop := fanoutManager(t)
	defer stop()
	now := time.Now()
	fillRemoteTopics(m, now)
	m.mu.Lock()
	m.remoteRosters["t:0"]["r0"] = remoteRosterEntry{members: []PresenceMember{{UserID: "old"}}, expiresAt: now.Add(-time.Second)}
	m.remoteRosters["t:1"]["r1"] = remoteRosterEntry{members: []PresenceMember{{UserID: "u1"}}, expiresAt: now.Add(-time.Second)}
	m.mu.Unlock()

	m.mergeRemotePresence("r2", presenceFanoutMsg{ReplicaID: "r2", Topic: "new", Members: []PresenceMember{{UserID: "n"}}})

	if got := m.PresenceRoster("t:0"); len(got) != 1 || got[0].UserID != "u0" {
		t.Errorf("SECURITY: a topic with a live replica was reclaimed: roster %v", got)
	}
	if got := m.PresenceRoster("new"); len(got) != 1 {
		t.Errorf("the new topic was not admitted into the expired topic's slot: %v", got)
	}
	m.mu.RLock()
	_, kept := m.remoteRosters["t:1"]
	m.mu.RUnlock()
	if kept {
		t.Error("the fully expired topic t:1 kept its slot")
	}
}
