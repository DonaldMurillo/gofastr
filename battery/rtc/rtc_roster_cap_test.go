package rtc

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// TestRosterBeatTrimsOverCap: two replicas can each admit a join before
// their roster beats meet, so a beat brings the merged room past
// MaxPeers (ledger R20 of the 2026-10-27 bug bash: MaxPeers 1 left both
// rooms at two). Every replica ranks the merged room by Order then ID
// and closes its own peers past the cap as room full; a remote peer past
// the cap is its own replica's to close.
func TestRosterBeatTrimsOverCap(t *testing.T) {
	f := fanout.NewInProcess()
	s := newTestSignaler(t, Config{MaxPeers: 2})
	s.heartbeatEvery = time.Hour
	s.remoteTTL = time.Hour
	if _, err := s.SetFanout(f); err != nil {
		t.Fatalf("SetFanout: %v", err)
	}
	base := startSignaler(t, s)
	a, _ := join(t, base, "room1", "pA")
	defer a.close()
	b, _ := join(t, base, "room1", "pB")
	defer b.close()
	a.expectEnv(t, "join")

	// A remote seat newer than both ranks past the cap: nothing here closes.
	forgedBeat(t, f, "room1", []PeerInfo{{ID: "rLate", Order: math.MaxInt64}})
	for _, c := range []*wsClient{a, b} {
		if code, closed := closedWithin(c, 300*time.Millisecond); closed {
			t.Fatalf("a later remote seat closed a local peer with %d", code)
		}
	}

	// A remote seat older than both takes a seat: pB, the latest local
	// seat, is past the cap and closes; pA keeps its seat.
	forgedBeat(t, f, "room1", []PeerInfo{{ID: "rEarly", Order: 1}})
	if code, closed := closedWithin(b, 2*time.Second); !closed || code != 4409 {
		t.Fatalf("pB past the cap: closed=%v code=%d, want close 4409", closed, code)
	}
	if code, closed := closedWithin(a, 300*time.Millisecond); closed {
		t.Fatalf("pA within the cap was closed with %d", code)
	}
}

// closedWithin drains c for d and reports the close code if the server
// closed it.
func closedWithin(c *wsClient, d time.Duration) (uint16, bool) {
	deadline := time.Now().Add(d)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return 0, false
		}
		_, err := c.recv(left)
		if errors.Is(err, errClosed) {
			return c.closeCode, true
		}
		if err != nil {
			return 0, false
		}
	}
}
