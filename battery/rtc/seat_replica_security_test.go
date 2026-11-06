package rtc

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

func TestCapEvictionKeepsRemotePeer(t *testing.T) {
	for _, replica := range []string{"none", "older", "newer"} {
		t.Run(replica, func(t *testing.T) {
			bus := fanout.NewInProcess()
			signaler := newTestSignaler(t, Config{MaxSocketsPerUser: 1})
			signaler.heartbeatEvery, signaler.remoteTTL = time.Hour, time.Hour
			if _, err := signaler.SetFanout(bus); err != nil {
				t.Fatal(err)
			}
			base := startSignaler(t, signaler)
			peer, _ := joinAs(t, base, "room1", "subject", "user")
			defer peer.close()
			observer, _ := join(t, base, "room1", "observer")
			defer observer.close()
			peer.expectEnv(t, "join")
			if replica != "none" {
				order := int64(1)
				if replica == "newer" {
					order = time.Now().Add(time.Hour).UnixNano()
				}
				remote := PeerInfo{ID: "subject", Order: order}
				fakeReplica(t, bus, "remote", fanoutMsg{Kind: evRoster, Room: "room1", Members: []PeerInfo{remote}})
				waitFor(t, 3*time.Second, func() bool { return signaler.remoteHasPeer("room1", "subject") }, "remote subject")
			}
			next, _ := joinAs(t, base, "room2", "next", "user")
			defer next.close()
			joins, leaves := joinLeaveCounts(t, collectEnvs(observer, 200*time.Millisecond))
			wantJoin, wantLeave := 0, 0
			if replica == "newer" {
				wantJoin = 1
			}
			if replica == "none" {
				wantLeave = 1
			}
			if joins["subject"] != wantJoin || leaves["subject"] != wantLeave {
				t.Fatalf("subject joins=%d leaves=%d; want %d/%d", joins["subject"], leaves["subject"], wantJoin, wantLeave)
			}
		})
	}
}
