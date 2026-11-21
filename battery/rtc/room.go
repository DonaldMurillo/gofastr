package rtc

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/internal/tokenbucket"
)

const (
	evJoin       = "join"
	evLeave      = "leave"
	evStatus     = "status"
	evSignal     = "signal"
	evICEServers = "iceServers"
	evRoster     = "roster"
)

type roomSnapshot struct {
	Room       string      `json:"room"`
	Self       PeerInfo    `json:"self"`
	Peers      []PeerInfo  `json:"peers"`
	ICEServers []ICEServer `json:"iceServers"`
}

type signalPayload struct {
	From string          `json:"from"`
	To   string          `json:"to"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type leavePayload struct {
	ID string `json:"id"`
}

type statusPayload struct {
	ID     string          `json:"id"`
	Status json.RawMessage `json:"status"`
}

type iceServersPayload struct {
	ICEServers []ICEServer `json:"iceServers"`
}

type roomEvent struct {
	kind   string
	peer   PeerInfo
	id     string
	status json.RawMessage
	signal *signalPayload
}

type inboundMsg struct {
	Kind string          `json:"kind"`
	To   string          `json:"to"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type room struct {
	name       string
	peers      map[string]*roomPeer
	seq        uint64
	emptySince time.Time
	channel    *stream.StateChannel[string, roomSnapshot, roomEvent]
	listed     *listedPeers
}

type roomPeer struct {
	info    PeerInfo
	conn    *stream.WebSocketConn
	limiter tokenbucket.Bucket
	seat    *socketSeat
	req     *http.Request
	recheck func(*http.Request) bool
	sigOut  map[string]signalSeq
	sigIn   map[string]uint64
}

type signalSeq struct {
	order int64
	n     uint64
}

const maxSignalPairs = 1024

type roomSource struct {
	s      *Signaler
	name   string
	listed *listedPeers
}

type listedPeers struct {
	mu sync.Mutex
	by map[string]map[string]int64
}

// SnapshotFor builds the peer's view: itself, the other members (local
// plus live remote), and its ICE list with its own TURN credential minted
// at snapshot time.
func (src roomSource) SnapshotFor(peerID string) (roomSnapshot, uint64) {
	src.s.mu.Lock()
	defer src.s.mu.Unlock()
	snap := roomSnapshot{Room: src.name, Peers: []PeerInfo{}}
	rm, ok := src.s.rooms[src.name]
	if !ok {
		return snap, 0
	}
	if p, self := rm.peers[peerID]; self {
		snap.Self = p.info
	}
	for _, info := range src.s.mergedPeersLocked(src.name, time.Now()) {
		if info.ID != peerID {
			snap.Peers = append(snap.Peers, info)
		}
	}
	snap.ICEServers = src.s.iceServersFor(peerID, time.Now())
	src.listed.record(peerID, snap.Peers)
	return snap, rm.seq
}

func (l *listedPeers) record(peerID string, peers []PeerInfo) {
	if l == nil {
		return
	}
	set := make(map[string]int64, len(peers))
	for _, p := range peers {
		set[p.ID] = p.Order
	}
	l.mu.Lock()
	l.by[peerID] = set
	l.mu.Unlock()
}

func (l *listedPeers) consume(peerID, subject string, order int64) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	set := l.by[peerID]
	listed, ok := set[subject]
	if !ok {
		return false
	}
	delete(set, subject)
	return listed == order
}

func (l *listedPeers) forget(peerID string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.by, peerID)
	l.mu.Unlock()
}

func (s *Signaler) iceServersFor(peerID string, now time.Time) []ICEServer {
	out := append([]ICEServer(nil), s.cfg.ICEServers...)
	if s.cfg.TURN != nil {
		out = append(out, s.cfg.TURN.Credentials(peerID, now))
	}
	return out
}

// FilterEvent shapes each event per peer BEFORE serialization:
func (src roomSource) FilterEvent(peerID string, ev roomEvent) (any, bool) {
	switch ev.kind {
	case evICEServers:
		return iceServersPayload{ICEServers: src.s.iceServersFor(peerID, time.Now())}, true
	case evJoin:
		if peerID == ev.peer.ID || src.listed.consume(peerID, ev.peer.ID, ev.peer.Order) {
			return nil, false
		}
		return ev.peer, true
	case evLeave:
		if peerID == ev.id {
			return nil, false
		}
		src.listed.consume(peerID, ev.id, 0)
		return leavePayload{ID: ev.id}, true
	case evStatus:
		if peerID == ev.id {
			return nil, false
		}
		return statusPayload{ID: ev.id, Status: ev.status}, true
	case evSignal:
		if ev.signal == nil || ev.signal.To != peerID {
			return nil, false
		}
		return *ev.signal, true
	default:
		return nil, false
	}
}

func (s *Signaler) publishLocked(rm *room, kind string, ev roomEvent) {
	rm.seq++
	rm.channel.Publish(kind, ev)
}

func mintPeerID() (string, bool) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", false
	}
	return hex.EncodeToString(buf[:]), true
}

// Serve upgrades r to a WebSocket, joins the peer to join.Room, and runs
// the read loop until the socket closes.
func (s *Signaler) Serve(w http.ResponseWriter, r *http.Request, join Join) {
	s.serve(w, r, join, nil)
}

func (s *Signaler) serve(w http.ResponseWriter, r *http.Request, join Join, recheck func(*http.Request) bool) {
	if err := validJoin(join); err != nil {
		s.logger.Warn("rtc: join refused", "reason", "bad_join")
		s.refuse(w, r, http.StatusBadRequest, reasonInvalidJoin, nil)
		return
	}
	if join.User == "" && s.cfg.AnonymousSeats != AnonymousPerSocket {
		s.logger.Warn("rtc: join refused", "reason", "no_user")
		s.refuse(w, r, http.StatusForbidden, reasonUserRequired, nil)
		return
	}
	peerID := join.PeerID
	if peerID == "" {
		var ok bool
		if peerID, ok = mintPeerID(); !ok {
			s.logger.Warn("rtc: join refused", "reason", "no_peer_id")
			s.refuse(w, r, http.StatusServiceUnavailable, reasonTryAgain, nil)
			return
		}
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		s.refuse(w, r, http.StatusServiceUnavailable, reasonSignalerClosed, nil)
		return
	}
	now := time.Now()
	merged := s.mergedPeersLocked(join.Room, now)
	rejoin := slices.ContainsFunc(merged, func(p PeerInfo) bool { return p.ID == peerID })
	if !rejoin && len(merged) >= s.cfg.MaxPeers {
		room := scrubLogField(join.Room)
		s.mu.Unlock()
		s.logger.Warn("rtc: join refused", "reason", "room_full", "room", room)
		s.refuse(w, r, http.StatusConflict, reasonRoomFull, nil)
		return
	}
	s.mu.Unlock()

	ws := s.cfg.WS
	ws.ConnectionID = join.Room + "/" + peerID
	ws.OnClose = nil
	conn, err := stream.Upgrade(w, r, ws)
	if err != nil {
		i18n.HTTPError(w, r, http.StatusBadRequest, i18n.ReasonWebSocketRequired)
		return
	}

	p := &roomPeer{
		info: PeerInfo{
			ID:          peerID,
			Role:        join.Role,
			User:        join.User,
			DisplayName: join.DisplayName,
			Order:       time.Now().UnixNano(),
		},
		conn:    conn,
		limiter: tokenbucket.New(s.cfg.MaxFramesPerSecond),
		req:     r,
		recheck: recheck,
	}

	var displaced []*stream.WebSocketConn
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = conn.CloseWithStatus(4000+http.StatusServiceUnavailable, "signaler closed")
		return
	}
	merged = s.mergedPeersLocked(join.Room, time.Now())
	rejoin = slices.ContainsFunc(merged, func(pi PeerInfo) bool { return pi.ID == peerID })
	if !rejoin && len(merged) >= s.cfg.MaxPeers {
		s.mu.Unlock()
		s.logger.Warn("rtc: peer closed", "reason", "room_full")
		_ = conn.CloseWithStatus(4000+http.StatusConflict, "room full")
		return
	}
	rm := s.rooms[join.Room]
	if rm == nil {
		rm = s.newRoomLocked(join.Room)
	}
	if old := rm.peers[peerID]; old != nil {
		delete(rm.peers, peerID)
		s.publishLocked(rm, evLeave, roomEvent{kind: evLeave, id: peerID})
		s.mirrorLocked(join.Room, fanoutMsg{Kind: evLeave, ID: peerID})
		s.broadcastRosterLocked(join.Room)
		s.spliceSeatLocked(old.seat)
		displaced = append(displaced, s.detachLocked(rm, old))
	}
	rm.peers[peerID] = p
	rm.emptySince = time.Time{}
	displaced = append(displaced, s.admitSeatLocked(join.Room, p, join.User)...)
	s.mu.Unlock()
	closeDetached(displaced...)
	defer s.peerGone(join.Room, p)
	rm.channel.Connect(peerID, conn)
	if s.testAtAnnounce != nil {
		s.testAtAnnounce <- peerID
		<-s.testAnnounceGate
	}

	s.mu.Lock()
	if rm.peers[peerID] == p {
		s.publishLocked(rm, evJoin, roomEvent{kind: evJoin, peer: p.info})
		joined := p.info
		s.mirrorLocked(join.Room, fanoutMsg{Kind: evJoin, Peer: &joined})
		s.broadcastRosterLocked(join.Room)
	}
	roomLog, peerLog, roleLog := scrubLogField(join.Room), scrubLogField(peerID), scrubLogField(join.Role)
	s.mu.Unlock()
	s.logger.Debug("rtc: join", "room", roomLog, "peer", peerLog, "role", roleLog)

	held := func() bool { return s.peerHeld(r, recheck) }
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go s.watchPeer(conn, held, stopWatch)

	for {
		data, rerr := conn.Read()
		if rerr != nil {
			return
		}
		if !p.limiter.Allow(time.Now()) {
			s.logger.Warn("rtc: peer closed", "reason", "frame_rate")
			conn.Close()
			return
		}
		if !held() {
			s.logger.Warn("rtc: peer closed", "reason", "unauthorized")
			_ = conn.CloseWithStatus(4000+http.StatusUnauthorized, "session ended")
			return
		}
		var in inboundMsg
		if handler.UnmarshalStrict(data, &in) != nil {
			continue
		}
		switch in.Kind {
		case evSignal:
			s.handleSignal(join.Room, p, in)
		case evStatus:
			s.handleStatus(join.Room, p, in)
		}
	}
}

func (s *Signaler) detachLocked(rm *room, p *roomPeer) *stream.WebSocketConn {
	rm.channel.Unregister(p.conn)
	return p.conn
}

func closeDetached(conns ...*stream.WebSocketConn) {
	for _, conn := range conns {
		go conn.Close()
	}
}

func (s *Signaler) peerHeld(r *http.Request, recheck func(*http.Request) bool) (held bool) {
	defer func() {
		if recover() != nil {
			held = false
		}
	}()
	fresh, ok := handler.RecheckPrincipal(r.Context())
	if !ok {
		return false
	}
	return recheck == nil || recheck(r.WithContext(fresh))
}

func (s *Signaler) watchPeer(conn *stream.WebSocketConn, held func() bool, stop <-chan struct{}) {
	t := time.NewTicker(s.recheckEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if !held() {
				s.logger.Warn("rtc: peer closed", "reason", "unauthorized")
				_ = conn.CloseWithStatus(4000+http.StatusUnauthorized, "session ended")
				return
			}
		}
	}
}

func (s *Signaler) peerGone(roomName string, p *roomPeer) {
	s.mu.Lock()
	rm, ok := s.rooms[roomName]
	if !ok || rm.peers[p.info.ID] != p {
		s.mu.Unlock()
		return
	}
	delete(rm.peers, p.info.ID)
	rm.listed.forget(p.info.ID)
	s.publishDepartureLocked(roomName, rm, p)
	s.mirrorLocked(roomName, fanoutMsg{Kind: evLeave, ID: p.info.ID})
	s.spliceSeatLocked(p.seat)
	if len(rm.peers) == 0 {
		s.markEmptyLocked(rm)
	}
	s.broadcastRosterLocked(roomName)
	roomLog, peerLog := scrubLogField(roomName), scrubLogField(p.info.ID)
	s.mu.Unlock()
	s.logger.Debug("rtc: leave", "room", roomLog, "peer", peerLog)
}

func (s *Signaler) publishDepartureLocked(roomName string, rm *room, peer *roomPeer) {
	if remote, held := s.remoteViewLocked(roomName, time.Now())[peer.info.ID]; !held {
		s.publishLocked(rm, evLeave, roomEvent{kind: evLeave, id: peer.info.ID})
	} else if remote.Order > peer.info.Order {
		s.publishLocked(rm, evJoin, roomEvent{kind: evJoin, peer: remote})
	}
}

func (s *Signaler) handleSignal(roomName string, p *roomPeer, in inboundMsg) {
	if in.To == p.info.ID {
		return
	}
	switch in.Type {
	case "offer", "answer", "ice":
	default:
		return
	}
	if len(in.Data) > s.cfg.MaxSignalBytes {
		s.logger.Warn("rtc: peer closed", "reason", "oversize_signal")
		p.conn.Close()
		return
	}
	sig := &signalPayload{From: p.info.ID, To: in.To, Type: in.Type, Data: in.Data}

	s.mu.Lock()
	rm, ok := s.rooms[roomName]
	if !ok || rm.peers[p.info.ID] != p {
		s.mu.Unlock()
		return
	}
	if _, local := rm.peers[sig.To]; local {
		s.publishLocked(rm, evSignal, roomEvent{kind: evSignal, signal: sig})
		s.mu.Unlock()
		return
	}
	toOrder, ok := s.remotePeerOrderLocked(roomName, sig.To)
	if !ok {
		s.mu.Unlock()
		return
	}
	msg := fanoutMsg{Kind: evSignal, Room: roomName, Signal: sig, ToOrder: toOrder}
	msg.Seq = p.nextSignalSeqLocked(sig.To, toOrder)
	send, nodeID := s.fanoutSend, s.nodeID
	s.mu.Unlock()
	if send != nil {
		publishFanout(send, nodeID, msg, func(error) {
			s.logger.Warn("rtc: peer closed", "reason", "signal_lost")
			closeDetached(p.conn)
		})
	}
}

func (s *Signaler) handleStatus(roomName string, p *roomPeer, in inboundMsg) {
	if len(in.Data) > s.cfg.MaxStatusBytes || !json.Valid(in.Data) || !isObjectJSON(in.Data) {
		return
	}
	s.mu.Lock()
	rm, ok := s.rooms[roomName]
	if !ok || rm.peers[p.info.ID] != p {
		s.mu.Unlock()
		return
	}
	p.info.Status = append(json.RawMessage(nil), in.Data...)
	s.publishLocked(rm, evStatus, roomEvent{kind: evStatus, id: p.info.ID, status: p.info.Status})
	s.mirrorLocked(roomName, fanoutMsg{Kind: evStatus, ID: p.info.ID, Status: p.info.Status})
	s.mu.Unlock()
}

func isObjectJSON(data []byte) bool {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func (s *Signaler) newRoomLocked(name string) *room {
	rm := &room{name: name, peers: make(map[string]*roomPeer), listed: &listedPeers{by: map[string]map[string]int64{}}}
	rm.channel = stream.NewStateChannel[string, roomSnapshot, roomEvent](roomSource{s: s, name: name, listed: rm.listed})
	rm.channel.CloseOnOverflow(true)
	go rm.channel.Run()
	s.rooms[name] = rm
	if s.cfg.TURN != nil && s.refreshDone == nil {
		s.refreshDone = make(chan struct{})
		s.refreshWG.Add(1)
		go s.turnRefreshLoop(s.refreshDone)
	}
	return rm
}

func (s *Signaler) markEmptyLocked(rm *room) {
	rm.emptySince = time.Now()
	name := rm.name
	time.AfterFunc(s.cfg.RoomIdleTTL, func() { s.sweepRoom(name) })
}

func (s *Signaler) sweepRoom(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rm := s.rooms[name]
	if rm == nil || !s.idleLocked(rm, time.Now()) {
		return
	}
	rm.channel.Stop()
	delete(s.rooms, name)
}

func (s *Signaler) idleLocked(rm *room, now time.Time) bool {
	return len(rm.peers) == 0 && !rm.emptySince.IsZero() && now.Sub(rm.emptySince) >= s.cfg.RoomIdleTTL
}

func (s *Signaler) sweepIdleLocked(now time.Time) {
	for _, name := range slices.Sorted(maps.Keys(s.rooms)) {
		rm := s.rooms[name]
		if !s.idleLocked(rm, now) {
			continue
		}
		rm.channel.Stop()
		delete(s.rooms, name)
	}
}

func (s *Signaler) mergedPeersLocked(roomName string, now time.Time) []PeerInfo {
	seen := make(map[string]bool)
	out := []PeerInfo{}
	if rm, ok := s.rooms[roomName]; ok {
		for _, id := range slices.Sorted(maps.Keys(rm.peers)) {
			p := rm.peers[id]
			out = append(out, p.info)
			seen[id] = true
		}
	}
	remote := s.remoteViewLocked(roomName, now)
	for _, id := range slices.Sorted(maps.Keys(remote)) {
		if !seen[id] {
			out = append(out, remote[id])
		}
	}
	slices.SortStableFunc(out, func(a, b PeerInfo) int {
		if a.Order != b.Order {
			if a.Order < b.Order {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func (s *Signaler) remoteHasPeerLocked(roomName, peerID string) bool {
	_, ok := s.remotePeerOrderLocked(roomName, peerID)
	return ok
}

func (s *Signaler) remotePeerOrderLocked(roomName, peerID string) (int64, bool) {
	replicas, ok := s.remote[roomName]
	if !ok {
		return 0, false
	}
	now := time.Now()
	var order int64
	found := false
	for _, node := range slices.Sorted(maps.Keys(replicas)) {
		entry := replicas[node]
		if now.After(entry.expires) {
			continue
		}
		for _, m := range entry.members {
			if m.ID == peerID && (!found || m.Order > order) {
				order, found = m.Order, true
			}
		}
	}
	return order, found
}

func (p *roomPeer) nextSignalSeqLocked(to string, order int64) uint64 {
	if p.sigOut == nil {
		p.sigOut = make(map[string]signalSeq)
	}
	c, ok := p.sigOut[to]
	if !ok && len(p.sigOut) >= maxSignalPairs {
		return 0
	}
	if c.order != order {
		c = signalSeq{order: order}
	}
	c.n++
	p.sigOut[to] = c
	return c.n
}

func (p *roomPeer) signalGapLocked(from string, seq uint64) bool {
	if seq == 0 {
		return false
	}
	if p.sigIn == nil {
		p.sigIn = make(map[string]uint64)
	}
	last, ok := p.sigIn[from]
	if !ok && len(p.sigIn) >= maxSignalPairs {
		return false
	}
	p.sigIn[from] = seq
	return seq > last+1
}
