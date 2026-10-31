package island

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// OnPresenceChange is app code. The local join and leave paths fire it
// through firePresenceChange, which recovers a panic; the cross-replica
// paths called it bare. A panic on the heartbeat goroutine's expiry sweep
// crashed the process (2026-10-27 bug bash R9), and the remote-merge and
// fanout-stop paths had the same shape.

// panickyHook records every topic it is called for and panics on bad.
func panickyHook(bad string, fired *[]string) func(string) {
	return func(topic string) {
		*fired = append(*fired, topic)
		if topic == bad {
			panic("app hook failed for " + topic)
		}
	}
}

// fanoutManager is a Manager on an in-process fanout with the heartbeat
// sweep held off, so each test drives the path it names.
func fanoutManager(t *testing.T) (*Manager, func()) {
	t.Helper()
	m := NewManager()
	stop, err := m.SetFanout(fanout.NewInProcess())
	if err != nil {
		t.Fatal(err)
	}
	m.reconfigurePresence(time.Hour, time.Hour)
	return m, stop
}

func TestExpirySweepRecoversHookPanic(t *testing.T) {
	m, stop := fanoutManager(t)
	defer stop()
	m.mu.Lock()
	for _, topic := range []string{"doc:1", "doc:2"} {
		m.remoteRosters[topic] = map[string]remoteRosterEntry{
			"r1": {members: []PresenceMember{{UserID: "u1"}}, expiresAt: time.Now().Add(-time.Second)},
		}
		m.remoteRosterTouch[topic] = time.Now().Add(-time.Minute)
	}
	m.mu.Unlock()
	var fired []string
	m.SetOnPresenceChange(panickyHook("doc:1", &fired))

	m.presenceBeat()

	if !slices.Contains(fired, "doc:2") {
		t.Fatalf("hook fired for %v; a panic on doc:1 must not skip doc:2", fired)
	}
}

func TestRemoteMergeRecoversHookPanic(t *testing.T) {
	m, stop := fanoutManager(t)
	defer stop()
	now := time.Now()
	m.mu.Lock()
	for i := range maxRemoteRosterTopics {
		topic := "t:" + strconv.Itoa(i)
		entry := remoteRosterEntry{members: []PresenceMember{{UserID: "u"}}, expiresAt: now.Add(time.Hour)}
		touched := now
		if i == 0 {
			touched = now.Add(-time.Minute)
		}
		m.remoteRosters[topic] = map[string]remoteRosterEntry{"r1": entry}
		m.remoteRosterTouch[topic] = touched
	}
	m.mu.Unlock()
	var fired []string
	m.SetOnPresenceChange(panickyHook("t:0", &fired))

	m.mergeRemotePresence("r2", presenceFanoutMsg{ReplicaID: "r2", Topic: "new", Members: []PresenceMember{{UserID: "u2"}}})

	if !slices.Contains(fired, "t:0") || !slices.Contains(fired, "new") {
		t.Fatalf("hook fired for %v; want the evicted t:0, and its panic must not skip new", fired)
	}
}

func TestFanoutStopRecoversHookPanic(t *testing.T) {
	m, stop := fanoutManager(t)
	m.mu.Lock()
	for _, topic := range []string{"doc:1", "doc:2"} {
		m.remoteRosters[topic] = map[string]remoteRosterEntry{
			"r1": {members: []PresenceMember{{UserID: "u1"}}, expiresAt: time.Now().Add(time.Hour)},
		}
		m.remoteRosterTouch[topic] = time.Now()
	}
	m.mu.Unlock()
	var fired []string
	m.SetOnPresenceChange(panickyHook("doc:1", &fired))

	stop()

	if !slices.Contains(fired, "doc:2") {
		t.Fatalf("hook fired for %v; a panic on doc:1 must not skip doc:2", fired)
	}
}
