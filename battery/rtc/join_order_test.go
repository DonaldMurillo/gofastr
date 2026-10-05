package rtc

import (
	"encoding/json"
	"testing"
	"time"
)

// A socket that hydrates while an older peer's join is still on its
// way to the channel must not hear that join: its snapshot already
// carries the peer, and the browser module treats a join for a known
// id as a reconnect, dropping the connection it just built and
// rebuilding it against a peer that kept the old one. The seam places
// pB's hydration inside pA's hydrate-then-announce window; pB's first
// envelope after its snapshot must not be a join for pA.
func TestLateJoinNotReplayedToHydratedPeer(t *testing.T) {
	s := newTestSignaler(t, Config{})
	s.testAtAnnounce = make(chan string)
	s.testAnnounceGate = make(chan struct{})
	var base string
	var b *wsClient
	dialed := make(chan error, 1)
	go func() {
		for id := range s.testAtAnnounce {
			if id == "pA" {
				c, err := wsConnect(base + "/ws/room1?peer=pB")
				if err == nil {
					b = c
					// Read pB's snapshot, taken inside pA's window, so
					// it lists pA before pA's join is published.
					_, _ = b.recvEnv(300 * time.Millisecond)
				}
				dialed <- err
			}
			s.testAnnounceGate <- struct{}{}
		}
	}()
	base = startSignaler(t, s)

	a, _ := join(t, base, "room1", "pA")
	defer a.close()
	if err := <-dialed; err != nil {
		t.Fatalf("dial pB: %v", err)
	}
	defer b.close()
	a.expectEnv(t, "join") // pB

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		env, err := b.recvEnv(300 * time.Millisecond)
		if err != nil {
			return // silence after hydration: pA's join never reached pB
		}
		if env.Type != "join" {
			continue
		}
		var p struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(env.Payload, &p)
		if p.ID == "pA" {
			t.Fatalf("pB heard a join for pA after a snapshot that already listed pA: %s", env.Payload)
		}
	}
}
