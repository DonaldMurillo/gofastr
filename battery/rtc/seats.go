package rtc

// seats.go owns the per-principal socket cap on the signaling surface, the
// rtc spelling of the seat policy core/stream pins for SSE and framework/
// crud pins for the event stream: one number (16) and an overflow policy.
// rtc's policy is evict-oldest — the same displacement a rejoin of one id
// applies to its old socket, extended across rooms — because a signaling
// client's previous connection is routinely half-open (mobile handoff,
// laptop sleep) and the new one must win.
//
// The seat is held for the life of the socket: admitted at the same locked
// registration that inserts the peer, freed in peerGone (the single leave
// path) or by whichever path displaces the peer (a rejoin, an eviction).
// A principal is Join.User; sockets whose host derived no user share one
// bucket, the posture core/stream's broker takes when it cannot tell
// callers apart.

import "github.com/DonaldMurillo/gofastr/core/stream"

// defaultSeatsPerUser is the per-principal socket cap a zero-value Config
// gets: core/stream's defaultSeatsPerPrincipal and framework/crud's
// defaultEventStreamSeats, so the three seat policies in the tree read as
// one number. Sixteen covers multi-tab / multi-device use of one account
// while bounding what a single low-privilege principal can hold resident
// (each seat is a read loop plus a per-room StateChannel goroutine).
const defaultSeatsPerUser = 16

// socketSeat is one admitted signaling socket in its principal's FIFO.
type socketSeat struct {
	user string
	room string
	peer *roomPeer
}

// spliceSeatLocked removes seat from its principal's FIFO; the caller
// holds s.mu. A no-op when the seat is already gone (released after a
// displacement, or displaced after a release), so a seat is removed exactly
// once and the FIFO never retains a departed socket.
func (s *Signaler) spliceSeatLocked(seat *socketSeat) {
	if seat == nil {
		return
	}
	q := s.seatOrder[seat.user]
	for i, v := range q {
		if v == seat {
			s.seatOrder[seat.user] = append(q[:i], q[i+1:]...)
			break
		}
	}
	if len(s.seatOrder[seat.user]) == 0 {
		delete(s.seatOrder, seat.user)
	}
}

// admitSeatLocked seats one socket for user and returns the connections
// the admission displaced, for the caller to close outside the lock. At
// the cap, the principal's oldest seat goes: its peer is removed from its
// room through the same inline-leave shape a rejoin applies (leave event,
// mirror, roster, empty-room clock), so the room's members see the
// departure and the room can sweep when it empties. The displaced socket's
// own peerGone later no-ops: its peer is already gone from the room and
// its seat already spliced. Caller holds s.mu.
func (s *Signaler) admitSeatLocked(roomName string, p *roomPeer, user string) (displaced []*stream.WebSocketConn) {
	seat := &socketSeat{user: user, room: roomName, peer: p}
	p.seat = seat
	if s.cfg.MaxSocketsPerUser > 0 {
		for len(s.seatOrder[user]) >= s.cfg.MaxSocketsPerUser {
			oldest := s.seatOrder[user][0]
			s.seatOrder[user] = s.seatOrder[user][1:]
			if len(s.seatOrder[user]) == 0 {
				delete(s.seatOrder, user)
			}
			if rm := s.rooms[oldest.room]; rm != nil && rm.peers[oldest.peer.info.ID] == oldest.peer {
				delete(rm.peers, oldest.peer.info.ID)
				s.publishLocked(rm, evLeave, roomEvent{kind: evLeave, id: oldest.peer.info.ID})
				s.mirrorLocked(oldest.room, fanoutMsg{Kind: evLeave, ID: oldest.peer.info.ID})
				if len(rm.peers) == 0 {
					s.markEmptyLocked(rm)
				}
				s.broadcastRosterLocked(oldest.room)
			}
			displaced = append(displaced, oldest.peer.conn)
		}
	}
	s.seatOrder[user] = append(s.seatOrder[user], seat)
	return displaced
}
