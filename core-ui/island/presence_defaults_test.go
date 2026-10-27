package island

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// A manager attached with the public defaults runs the presence
// heartbeat: without it a dropped announcement never heals and a remote
// member's TTL expiry never reaches OnPresenceChange.
func TestSetFanoutStartsHeartbeat(t *testing.T) {
	m := NewManager()
	stop, err := m.SetFanout(fanout.NewInProcess())
	if err != nil {
		t.Fatalf("SetFanout: %v", err)
	}
	defer stop()
	m.mu.RLock()
	hb := m.presenceHeartbeat
	m.mu.RUnlock()
	if hb != defaultPresenceHeartbeat {
		t.Fatalf("presence heartbeat = %v, want %v", hb, defaultPresenceHeartbeat)
	}
}

// Detaching the fanout drops every remote member, and local viewers are
// told, as they are for a TTL expiry.
func TestDetachFanoutNotifiesViewers(t *testing.T) {
	a, b, _, stopA, stopB := twoManagersOnOneBus(t, 50*time.Millisecond, time.Second)
	defer stopA()

	var mu sync.Mutex
	var fired []string
	b.SetOnPresenceChange(func(topic string) {
		mu.Lock()
		fired = append(fired, topic)
		mu.Unlock()
	})

	h := a.PresenceJoin("sess-a1", PresenceIdentity{UserID: "u1", DisplayName: "Alice"}, []string{"doc:1"})
	defer h.Leave()
	waitFor(t, time.Second, "B sees A's member", func() bool {
		return len(b.PresenceRoster("doc:1")) == 1
	})
	mu.Lock()
	fired = nil
	mu.Unlock()

	stopB()
	if got := b.PresenceRoster("doc:1"); len(got) != 0 {
		t.Fatalf("roster after detach = %v, want empty", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(fired, "doc:1") {
		t.Fatalf("OnPresenceChange after detach = %v, want doc:1", fired)
	}
}
