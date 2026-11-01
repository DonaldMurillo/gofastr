package island

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// A heartbeat snapshotted a topic's roster under the read lock and queued it
// after releasing it, so the last member's leave could be queued in between:
// peers then received the leave and, after it, the stale roster that still
// held the member, and nothing re-sent the topic once it had no local
// connections (2026-10-31 O63). The snapshot is now queued under the same
// lock, as battery/rtc's announceRoom does, so a later mutation's roster is
// always queued after it.
func TestLeaveNotOvertakenByHeartbeat(t *testing.T) {
	m, stop := fanoutManager(t)
	defer stop()
	h := m.PresenceJoin("sess-1", PresenceIdentity{UserID: "u1"}, []string{"doc:1"})

	var (
		mu    sync.Mutex
		sent  []presenceFanoutMsg
		calls int
	)
	left := make(chan struct{})
	record := func(raw []byte) {
		_, body, err := fanout.Unwrap(raw)
		if err != nil {
			t.Errorf("unwrap: %v", err)
			return
		}
		var msg presenceFanoutMsg
		if err := json.Unmarshal(body, &msg); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		mu.Lock()
		sent = append(sent, msg)
		mu.Unlock()
	}
	m.mu.Lock()
	m.presenceSend = func(raw []byte) {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if first {
			// The heartbeat holds its snapshot; let the leave run now.
			go func() { h.Leave(); close(left) }()
			select {
			case <-left:
			case <-time.After(200 * time.Millisecond):
			}
		}
		record(raw)
	}
	m.mu.Unlock()

	m.broadcastAllLocalTopics()
	<-left

	mu.Lock()
	defer mu.Unlock()
	if len(sent) == 0 {
		t.Fatal("setup: nothing was announced")
	}
	if last := sent[len(sent)-1]; len(last.Members) != 0 {
		t.Errorf("the last announcement for doc:1 still lists %v after its last member left", last.Members)
	}
}
