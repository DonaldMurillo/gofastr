package rtc

// Pins: per-principal seat caps on long-lived stream surfaces — the rtc socket included (round 5, fixed).
// Property: per-principal seat caps on long-lived stream surfaces (core/stream seats.go
// defaultSeatsPerPrincipal=16, evict-oldest; pinned for SSE/MCP/ACP/CRUD streams in rounds
// 1-4). The rtc signaling socket is the one stream surface with no seat, room-count, or
// join-rate bound.
// Surfaces: room.go::Serve :219-363 (admission = per-room MaxPeers only; fresh frameLimiter
// bucket per socket :277), newRoomLocked :462-478 (unbounded s.rooms; every occupied room
// permanently holds a StateChannel goroutine — sweepIdleLocked drops only EMPTY rooms),
// rtc.go::refuse :453-476 (unthrottled).
// Finding: 300 sequential dials to distinct rooms by ONE principal, zero refusals, rooms
// resident with goroutines (verified 2026-09-06) — unauthenticated-in-practice
// fd/goroutine/memory exhaustion on the newest long-lived surface.
// Fix direction: Config.MaxSocketsPerUser (SSE parity 16, evict-oldest per rtc's own
// replace-on-rejoin), seat freed in peerGone.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSeatFloodRedCapsSocketsPerUser(t *testing.T) {
	s := newTestSignaler(t, Config{})
	// seatFloodServe is serveJoin (wsclient_test.go) plus the one field a
	// real host derives from its own auth and Serve trusts: Join.User.
	// Every socket below belongs to the same principal.
	seatFloodServe := func(w http.ResponseWriter, r *http.Request) {
		room, _ := strings.CutPrefix(r.URL.Path, "/ws/")
		s.Serve(w, r, Join{
			Room:   room,
			PeerID: r.URL.Query().Get("peer"),
			User:   "u",
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(seatFloodServe))
	t.Cleanup(srv.Close)
	t.Cleanup(s.Close)

	const seats = 16 // core/stream defaultSeatsPerPrincipal parity
	conns := make([]*wsClient, 0, seats+1)
	defer func() {
		for _, c := range conns {
			c.close()
		}
	}()
	for i := 0; i < seats+1; i++ {
		// Distinct rooms, distinct peer ids, ONE principal: nothing
		// per-room can refuse this, only a per-principal seat can.
		c, _ := join(t, srv.URL, fmt.Sprintf("flood-%d", i), fmt.Sprintf("p%d", i))
		conns = append(conns, c)
	}

	seatFloodHeld := func() int {
		s.mu.Lock()
		defer s.mu.Unlock()
		n := 0
		for _, rm := range s.rooms {
			for _, p := range rm.peers {
				if p.info.User == "u" {
					n++
				}
			}
		}
		return n
	}
	if got := seatFloodHeld(); got > seats {
		t.Errorf("SECURITY: [rtc-seat-flood] one principal (Join.User %q) holds %d concurrent signaling sockets across %d distinct rooms (bound %d): every socket is a read loop plus a per-room StateChannel goroutine that only an EMPTY-room sweep drops, so sequential dials by one caller are all admitted (fd/goroutine/memory exhaustion)", "u", got, seats+1, seats)
	}

	// The observable spelling of the same bound, evict-oldest: the
	// (seats+1)th socket by the principal displaces its oldest one, the
	// way a rejoin of one id displaces the old socket today.
	seatFloodEvicted := func() bool {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if !conns[0].alive(50 * time.Millisecond) {
				return true
			}
		}
		return false
	}
	if !seatFloodEvicted() {
		t.Errorf("SECURITY: [rtc-seat-flood] the %dth socket by the same principal did not evict the 1st within 2s: no per-principal seat cap exists on the signaling surface", seats+1)
	}
}
