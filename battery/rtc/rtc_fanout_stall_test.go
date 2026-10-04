package rtc

import (
	"encoding/json"
	"testing"
	"time"
)

// TestStalledBeatSweepsReplacedPeer: after a remote replacement, a
// replica whose heartbeat stalls past remoteTTL is swept by the other,
// and the watcher sees the displaced id leave again. This is the
// mechanism behind #474: TestRemoteReplacesLocalPeer ran with a 300 ms
// TTL under a 500 ms silence window, so one starved heartbeat on a
// loaded CI runner replayed the leave. The displaced socket's own close
// path cannot double-fire (peerGone no-ops when the room no longer
// holds that peer); the sweep is the only second source.
func TestStalledBeatSweepsReplacedPeer(t *testing.T) {
	base1, base2, _, s2 := twoReplicas(t, func() Config { return Config{} })

	old, _ := join(t, base1, "room1", "p1")
	w, _ := join(t, base1, "room1", "pW")
	defer w.close()
	nu, _ := join(t, base2, "room1", "p1")
	defer nu.close()

	w.expectEnv(t, "leave")
	w.expectEnv(t, "join")
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := old.recv(time.Until(deadline)); err != nil {
			break
		}
	}
	// Simulate the stall: replica 2 stops beating.
	s2.haltHeartbeat()
	le := w.expectEnv(t, "leave")
	var lp leavePayload
	if json.Unmarshal(le.Payload, &lp) != nil || lp.ID != "p1" {
		t.Fatalf("leave envelope = %s", le.Payload)
	}
}
