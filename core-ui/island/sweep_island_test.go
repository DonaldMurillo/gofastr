package island

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

func TestRefusedStreamSaysWhenToRetry(t *testing.T) {
	mgr := NewManager(WithStreamCaps(1, 0))
	_, held, _ := mgr.ConnectSession("sess", "")
	defer held()
	rec := httptest.NewRecorder()
	mgr.ServeSSE(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/sse?session=sess", nil))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("refused stream = %d, Retry-After %q; want 429 with a Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestRemoteRosterCapsMembersAndReplicas(t *testing.T) {
	m := NewManager()
	stop, err := m.SetFanout(fanout.NewInProcess())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	members := make([]PresenceMember, maxRemoteMembersPerReplica+100)
	for i := range members {
		members[i] = PresenceMember{UserID: fmt.Sprintf("u%d", i)}
	}
	m.mergeRemotePresence("peer-1", presenceFanoutMsg{Topic: "big", Members: members})
	for i := range maxRemoteReplicasPerTopic + 50 {
		m.mergeRemotePresence(fmt.Sprintf("forged-%d", i), presenceFanoutMsg{Topic: "wide", Members: members[:1]})
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if got := len(m.remoteRosters["big"]["peer-1"].members); got != maxRemoteMembersPerReplica {
		t.Errorf("one announcement stored %d members, want the cap %d", got, maxRemoteMembersPerReplica)
	}
	if got := len(m.remoteRosters["wide"]); got != maxRemoteReplicasPerTopic {
		t.Errorf("one topic holds %d replicas, want the cap %d", got, maxRemoteReplicasPerTopic)
	}
}

func TestSetFanoutTwiceIsRefused(t *testing.T) {
	mgr := NewManager()
	stop, err := mgr.SetFanout(fanout.NewInProcess())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, err := mgr.SetFanout(fanout.NewInProcess()); err == nil {
		t.Fatal("a second SetFanout was accepted")
	}
}

func TestSetFanoutAnnouncesEarlierJoins(t *testing.T) {
	bus := fanout.NewInProcess()
	a, b := NewManager(), NewManager()
	stopB, err := b.SetFanout(bus)
	if err != nil {
		t.Fatal(err)
	}
	defer stopB()
	h := a.PresenceJoin("sess-a", PresenceIdentity{UserID: "u1"}, []string{"doc:1"})
	defer h.Leave()
	stopA, err := a.SetFanout(bus)
	if err != nil {
		t.Fatal(err)
	}
	defer stopA()
	deadline := time.Now().Add(time.Second)
	for len(b.PresenceRoster("doc:1")) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("a join made before SetFanout waited for the first heartbeat")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCancelReleasesGlobalAndPeerSlots(t *testing.T) {
	mgr := NewManager(WithStreamCaps(0, 1), WithPeerStreamCap(1))
	_, c1, err := mgr.ConnectSession("a", "peer")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := mgr.ConnectSession("b", "peer"); err == nil {
		t.Fatal("a second stream passed a cap of one")
	}
	c1()
	_, c2, err := mgr.ConnectSession("b", "peer")
	if err != nil {
		t.Fatalf("a released slot was not reusable: %v", err)
	}
	c2()
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	if mgr.globalSubs != 0 || len(mgr.peerSubs) != 0 || len(mgr.streams) != 0 {
		t.Fatalf("cancel left state behind: global=%d peers=%v streams=%d", mgr.globalSubs, mgr.peerSubs, len(mgr.streams))
	}
}
