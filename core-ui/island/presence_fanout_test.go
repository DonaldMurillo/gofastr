package island

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// dropFirstN wraps a Fanout and silently drops the first N publishes on the
// given topic, then passes the rest through unchanged. Used to prove the
// heartbeat reconverges after a dropped announcement (the lossy model).
type dropFirstN struct {
	Fanout  fanout.Fanout
	Topic   string
	Drop    int
	mu      sync.Mutex
	dropped int
}

func (d *dropFirstN) Publish(ctx context.Context, topic string, payload []byte) error {
	if topic == d.Topic {
		d.mu.Lock()
		if d.dropped < d.Drop {
			d.dropped++
			d.mu.Unlock()
			return nil // swallow — simulate a message lost in transit
		}
		d.mu.Unlock()
	}
	return d.Fanout.Publish(ctx, topic, payload)
}

func (d *dropFirstN) Subscribe(topic string, fn func([]byte)) (func(), error) {
	return d.Fanout.Subscribe(topic, fn)
}

// twoManagersOnOneBus wires two Managers to one in-process fanout (two
// "replicas" in one binary) with short presence intervals so tests don't
// sleep for the 15s production default. Returns the individual stop funcs
// so a test can tear down one replica at a time (e.g. graceful-leave).
func twoManagersOnOneBus(t *testing.T, heartbeat, ttl time.Duration) (a, b *Manager, f *fanout.InProcess, stopA, stopB func()) {
	t.Helper()
	bus := fanout.NewInProcess()
	ra := NewManager()
	rb := NewManager()
	sa, err := ra.SetFanout(bus)
	if err != nil {
		t.Fatalf("SetFanout A: %v", err)
	}
	sb, err := rb.SetFanout(bus)
	if err != nil {
		t.Fatalf("SetFanout B: %v", err)
	}
	ra.reconfigurePresence(heartbeat, ttl)
	rb.reconfigurePresence(heartbeat, ttl)
	return ra, rb, bus, sa, sb
}

// TestPresenceFanoutJoinVisibleCrossReplica — a join on replica A makes the
// member appear in replica B's roster (immediate announce on join).
func TestPresenceFanoutJoinVisibleCrossReplica(t *testing.T) {
	a, b, _, stopA, stopB := twoManagersOnOneBus(t, 50*time.Millisecond, 200*time.Millisecond)
	defer stopA()
	defer stopB()

	h := a.PresenceJoin("sess-a1", PresenceIdentity{UserID: "u1", DisplayName: "Alice"}, []string{"doc:1"})
	defer h.Leave()

	waitFor(t, time.Second, "B sees A's member", func() bool {
		roster := b.PresenceRoster("doc:1")
		return len(roster) == 1 && roster[0].UserID == "u1"
	})
}

// TestPresenceFanoutLeaveDropsCrossReplica — a leave on A removes the member
// from B's roster (immediate announce of the shrunk roster).
func TestPresenceFanoutLeaveDropsCrossReplica(t *testing.T) {
	a, b, _, stopA, stopB := twoManagersOnOneBus(t, 50*time.Millisecond, 200*time.Millisecond)
	defer stopA()
	defer stopB()

	h := a.PresenceJoin("sess-a1", PresenceIdentity{UserID: "u1", DisplayName: "Alice"}, []string{"doc:1"})
	waitFor(t, time.Second, "B sees A's member before leave", func() bool {
		return len(b.PresenceRoster("doc:1")) == 1
	})

	h.Leave()

	waitFor(t, time.Second, "B drops A's member after leave", func() bool {
		return len(b.PresenceRoster("doc:1")) == 0
	})
}

// TestPresenceFanoutDeathExpiresByTTL — when replica A goes silent (crash
// sim: heartbeat halted, no graceful leave), B drops A's members within TTL.
func TestPresenceFanoutDeathExpiresByTTL(t *testing.T) {
	a, b, _, stopA, stopB := twoManagersOnOneBus(t, 20*time.Millisecond, 80*time.Millisecond)
	defer stopA()
	defer stopB()

	h := a.PresenceJoin("sess-a1", PresenceIdentity{UserID: "u1"}, []string{"doc:1"})
	defer h.Leave()
	waitFor(t, time.Second, "B sees A's member before death", func() bool {
		return len(b.PresenceRoster("doc:1")) == 1
	})

	// Crash A: heartbeat stops, no graceful-leave broadcast is sent.
	a.haltPresenceHeartbeat()

	// Within TTL, B must drop A's members (TTL sweep).
	waitFor(t, 2*time.Second, "B expires A's member within TTL", func() bool {
		return len(b.PresenceRoster("doc:1")) == 0
	})
}

// TestPresenceFanoutGracefulLeavePrompt — stop() on A sends a graceful leave
// so B drops A's members promptly (well within TTL).
func TestPresenceFanoutGracefulLeavePrompt(t *testing.T) {
	a, b, _, stopA, stopB := twoManagersOnOneBus(t, 50*time.Millisecond, 5*time.Second)
	defer stopA()
	defer stopB()
	// B's TTL is huge so a prompt drop can only come from the graceful
	// leave broadcast, not from TTL expiry.

	h := a.PresenceJoin("sess-a1", PresenceIdentity{UserID: "u1"}, []string{"doc:1"})
	defer h.Leave()
	waitFor(t, time.Second, "B sees A's member before stop", func() bool {
		return len(b.PresenceRoster("doc:1")) == 1
	})

	stopA() // graceful-leave broadcast

	waitFor(t, time.Second, "B drops A promptly on graceful leave", func() bool {
		return len(b.PresenceRoster("doc:1")) == 0
	})
}

// TestPresenceFanoutHeartbeatReconverges — a dropped immediate announcement
// is healed by the next periodic heartbeat (lossy self-healing).
func TestPresenceFanoutHeartbeatReconverges(t *testing.T) {
	f := fanout.NewInProcess()
	drop := &dropFirstN{Fanout: f, Topic: presenceFanoutTopic, Drop: 1} // drop A's first announce
	a := NewManager()
	b := NewManager()
	stopA, err := a.SetFanout(drop)
	if err != nil {
		t.Fatalf("SetFanout A: %v", err)
	}
	defer stopA()
	stopB, err := b.SetFanout(drop)
	if err != nil {
		t.Fatalf("SetFanout B: %v", err)
	}
	defer stopB()
	a.reconfigurePresence(40*time.Millisecond, 200*time.Millisecond)
	b.reconfigurePresence(40*time.Millisecond, 200*time.Millisecond)

	h := a.PresenceJoin("sess-a1", PresenceIdentity{UserID: "u1"}, []string{"doc:1"})
	defer h.Leave()

	// The immediate announce was dropped; the heartbeat must reconverge.
	waitFor(t, time.Second, "heartbeat heals the dropped announce", func() bool {
		return len(b.PresenceRoster("doc:1")) == 1
	})
}

// TestPresenceFanoutOnChangeFiresOnRemote — B's OnPresenceChange fires when
// a remote merge changes B's merged roster (the push-propagation contract).
func TestPresenceFanoutOnChangeFiresOnRemote(t *testing.T) {
	a, b, _, stopA, stopB := twoManagersOnOneBus(t, 50*time.Millisecond, 200*time.Millisecond)
	defer stopA()
	defer stopB()

	var (
		mu      sync.Mutex
		firedOn []string
	)
	b.OnPresenceChange = func(topic string) {
		mu.Lock()
		firedOn = append(firedOn, topic)
		mu.Unlock()
	}

	h := a.PresenceJoin("sess-a1", PresenceIdentity{UserID: "u1"}, []string{"doc:9"})
	defer h.Leave()

	waitFor(t, time.Second, "B's OnPresenceChange fires for doc:9", func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, top := range firedOn {
			if top == "doc:9" {
				return true
			}
		}
		return false
	})
}

// TestPresenceFanoutMergedRosterDedup — the same user connected on both
// replicas appears as ONE member in the merged roster (dedup is by UserID,
// matching the local-only dedup invariant).
func TestPresenceFanoutMergedRosterDedup(t *testing.T) {
	a, b, _, stopA, stopB := twoManagersOnOneBus(t, 50*time.Millisecond, 200*time.Millisecond)
	defer stopA()
	defer stopB()

	id := PresenceIdentity{UserID: "shared", DisplayName: "Same Person"}
	ha := a.PresenceJoin("sess-a", id, []string{"doc:5"})
	defer ha.Leave()
	hb := b.PresenceJoin("sess-b", id, []string{"doc:5"})
	defer hb.Leave()

	// Give B time to learn A's announcement.
	waitFor(t, time.Second, "B's roster dedups the shared user to one", func() bool {
		roster := b.PresenceRoster("doc:5")
		return len(roster) == 1 && roster[0].UserID == "shared"
	})
}

// TestPresenceFanoutNoFanoutByteIdentical — with no fanout attached,
// PresenceRoster behaves exactly as the single-replica implementation: only
// local connections, identical output shape (sorted, empty-topic → nil).
func TestPresenceFanoutNoFanoutByteIdentical(t *testing.T) {
	m := NewManager()
	if got := m.PresenceRoster(""); got != nil {
		t.Errorf("empty topic should yield nil without fanout, got %v", got)
	}
	h := m.PresenceJoin("s1", PresenceIdentity{UserID: "zeta", DisplayName: "Z"}, []string{"t"})
	defer h.Leave()
	h2 := m.PresenceJoin("s2", PresenceIdentity{UserID: "alpha", DisplayName: "A"}, []string{"t"})
	defer h2.Leave()

	roster := m.PresenceRoster("t")
	if len(roster) != 2 {
		t.Fatalf("expected 2 local members, got %d", len(roster))
	}
	if roster[0].UserID != "alpha" || roster[1].UserID != "zeta" {
		t.Errorf("roster not sorted/by-value as before fanout: %+v", roster)
	}
	// No remote state should exist.
	m.mu.RLock()
	remote := m.remoteRosters
	m.mu.RUnlock()
	if remote != nil {
		t.Errorf("remoteRosters must be nil without fanout, got %v", remote)
	}
}

// TestPresenceFanoutStopDetachesCleanly — after stop, no presence goroutine
// leaks and remote state is cleared; a subsequent SetFanout re-attaches.
func TestPresenceFanoutStopDetachesCleanly(t *testing.T) {
	a, _, _, stopA, stopB := twoManagersOnOneBus(t, 50*time.Millisecond, 200*time.Millisecond)
	stopA()
	stopB()

	a.mu.RLock()
	done := a.presenceDone
	remote := a.remoteRosters
	send := a.presenceSend
	a.mu.RUnlock()
	if done != nil {
		t.Errorf("presenceDone not cleared after stop: %v", done)
	}
	if remote != nil {
		t.Errorf("remoteRosters not cleared after stop: %v", remote)
	}
	if send != nil {
		t.Error("presenceSend not cleared after stop")
	}

	// Re-attach must succeed (no "already attached" leak).
	if _, err := a.SetFanout(fanout.NewInProcess()); err != nil {
		t.Fatalf("re-attach after stop failed: %v", err)
	}
}

// TestPresenceFanoutRosterExcludesExpired — an expired remote entry does not
// appear in PresenceRoster even before the sweep runs (read-time filtering).
func TestPresenceFanoutRosterExcludesExpired(t *testing.T) {
	a, b, _, stopA, stopB := twoManagersOnOneBus(t, 50*time.Millisecond, 200*time.Millisecond)
	defer stopA()
	defer stopB()

	h := a.PresenceJoin("sess-a1", PresenceIdentity{UserID: "u1"}, []string{"doc:7"})
	defer h.Leave()
	waitFor(t, time.Second, "B sees A's member", func() bool {
		return len(b.PresenceRoster("doc:7")) == 1
	})

	// Forge an already-expired remote entry directly and confirm it is hidden.
	b.mu.Lock()
	if b.remoteRosters == nil {
		b.remoteRosters = make(map[string]map[string]remoteRosterEntry)
	}
	if b.remoteRosters["doc:7"] == nil {
		b.remoteRosters["doc:7"] = make(map[string]remoteRosterEntry)
	}
	b.remoteRosters["doc:7"]["stale-replica"] = remoteRosterEntry{
		members:   []PresenceMember{{UserID: "ghost", DisplayName: "G"}},
		expiresAt: time.Now().Add(-time.Hour), // already expired
	}
	b.mu.Unlock()

	for _, mem := range b.PresenceRoster("doc:7") {
		if mem.UserID == "ghost" {
			t.Errorf("expired remote member appeared in roster: %+v", mem)
		}
	}
}

// TestPresenceFanoutAnnouncementCarriesNoSessionID — the on-wire announcement
// contains ONLY the same identity fields PresenceRoster exposes; it never
// leaks session ids or anything else (identity safety).
func TestPresenceFanoutAnnouncementCarriesNoSessionID(t *testing.T) {
	a, _, f, stopA, stopB := twoManagersOnOneBus(t, 50*time.Millisecond, 200*time.Millisecond)
	defer stopA()
	defer stopB()

	var captured []byte
	var capMu sync.Mutex
	// Sniff the presence topic directly.
	cancel, _ := f.Subscribe(presenceFanoutTopic, func(payload []byte) {
		capMu.Lock()
		captured = append([]byte(nil), payload...)
		capMu.Unlock()
	})
	defer cancel()

	h := a.PresenceJoin("sess-secret", PresenceIdentity{UserID: "u1", DisplayName: "Alice"}, []string{"doc:1"})
	defer h.Leave()

	waitFor(t, time.Second, "presence announcement observed", func() bool {
		capMu.Lock()
		defer capMu.Unlock()
		return captured != nil
	})

	capMu.Lock()
	defer capMu.Unlock()
	if strings.Contains(string(captured), "sess-secret") {
		t.Errorf("announcement leaked session id: %s", captured)
	}
	// Must carry the roster identity (UserID + DisplayName) — the same data
	// PresenceRoster already exposes.
	if !strings.Contains(string(captured), "u1") || !strings.Contains(string(captured), "Alice") {
		t.Errorf("announcement missing server-derived identity: %s", captured)
	}
}

// waitFor polls cond every 5ms until it returns true or the deadline elapses.
func waitFor(t *testing.T, deadline time.Duration, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}
