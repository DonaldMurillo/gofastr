package rtc

import "github.com/DonaldMurillo/gofastr/core/stream"

const defaultSeatsPerUser = 16

type socketSeat struct {
	user string
	room string
	peer *roomPeer
}

func (s *Signaler) spliceSeatLocked(seat *socketSeat) {
	if seat == nil {
		return
	}
	stream.SpliceSeat(s.seatOrder, seat.user, seat)
}

func (s *Signaler) admitSeatLocked(roomName string, p *roomPeer, user string) (displaced []*stream.WebSocketConn) {
	if user == "" {
		p.seat = nil
		return nil
	}
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
				s.detachLocked(rm, oldest.peer)
				delete(rm.peers, oldest.peer.info.ID)
				rm.listed.forget(oldest.peer.info.ID)
				s.publishDepartureLocked(oldest.room, rm, oldest.peer)
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
