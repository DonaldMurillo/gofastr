package rtc

// room.go owns the room state machine: the per-room StateChannel, the
// join/leave/status/signal events and their sequence discipline, the
// per-socket read loop, and the idle-room sweep.
//
// One lock (Signaler.mu) serializes every room mutation with the
// channel Publish that reports it, so event order on the wire equals
// mutation order. This mirrors applyCommand/relaySignal in
// examples/webmcp-remote-assist: a relayed signal is not room state,
// but it IS a channel event, and a snapshot whose sequence trailed the
// events a page had already applied would be rejected by the page's
// reducer, so every Publish here is paired with one room.seq bump
// under the same lock hold.
import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/stream"
)

// Event kinds, on the room channel and on the fanout lane.
const (
	evJoin   = "join"
	evLeave  = "leave"
	evStatus = "status"
	evSignal = "signal"
	// evICEServers pushes a fresh ICE list (a re-minted TURN credential)
	// to every local peer of a room, half a TTL apart.
	evICEServers = "iceServers"
	evRoster     = "roster"
)

// roomSnapshot is one peer's hydration payload: itself, the rest of
// the room, and its ICE list. The TURN entry is minted here, per peer,
// at snapshot time; no other peer's credential is ever marshaled into
// this structure.
type roomSnapshot struct {
	Room       string      `json:"room"`
	Self       PeerInfo    `json:"self"`
	Peers      []PeerInfo  `json:"peers"`
	ICEServers []ICEServer `json:"iceServers"`
}

// signalPayload is one addressed signaling frame. Data crosses the
// process uninterpreted; the server validates the envelope only.
type signalPayload struct {
	From string          `json:"from"`
	To   string          `json:"to"`
	Type string          `json:"type"` // offer | answer | ice
	Data json.RawMessage `json:"data"`
}

type leavePayload struct {
	ID string `json:"id"`
}

type statusPayload struct {
	ID     string          `json:"id"`
	Status json.RawMessage `json:"status"`
}

// iceServersPayload is the iceServers event: the peer's ICE list with
// its own TURN credential re-minted.
type iceServersPayload struct {
	ICEServers []ICEServer `json:"iceServers"`
}

// roomEvent is the pre-filter event published to the room channel.
// FilterEvent decides, per peer, which payload (if any) each one sees.
type roomEvent struct {
	kind   string
	peer   PeerInfo        // join
	id     string          // leave, status
	status json.RawMessage // status
	signal *signalPayload  // signal
}

// inboundMsg is what a peer may send on its socket. Everything else is
// ignored; unknown kinds and malformed JSON never close the socket.
type inboundMsg struct {
	Kind string          `json:"kind"` // signal | status
	To   string          `json:"to"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// room is one signaling room: its local peers, its channel, and the
// channel-wide version SnapshotFor reports.
type room struct {
	name  string
	peers map[string]*roomPeer
	seq   uint64
	// emptySince is the instant the room last lost its last local
	// peer; zero while the room has members. The sweep drops the room
	// RoomIdleTTL after this.
	emptySince time.Time
	channel    *stream.StateChannel[string, roomSnapshot, roomEvent]
}

// roomPeer is one locally connected socket and its identity.
type roomPeer struct {
	info    PeerInfo
	conn    *stream.WebSocketConn
	limiter frameLimiter
	// seat is this socket's entry in its principal's seat FIFO
	// (seats.go); nil only for peers constructed outside Serve.
	seat *socketSeat
}

// roomSource adapts the Signaler to stream.SnapshotSource for one
// room. SnapshotFor and the channel's FilterEvent calls read the room
// under Signaler.mu, so payload and sequence come from one locked
// read (the one-immutable-read contract stream documents).
type roomSource struct {
	s    *Signaler
	name string
}

// SnapshotFor builds the peer's view: itself, the other members
// (local plus live remote), and its ICE list with its own TURN
// credential minted at snapshot time.
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
	return snap, rm.seq
}

// iceServersFor is the configured ICE list plus, with TURN set, a
// credential minted for peerID at now. cfg is immutable after New, so
// no lock is needed.
func (s *Signaler) iceServersFor(peerID string, now time.Time) []ICEServer {
	out := append([]ICEServer(nil), s.cfg.ICEServers...)
	if s.cfg.TURN != nil {
		out = append(out, s.cfg.TURN.Credentials(peerID, now))
	}
	return out
}

// FilterEvent shapes each event per peer BEFORE serialization:
//
//   - signal: addressed delivery. The frame is marshaled for its `to`
//     only; the sender never receives its own SDP back and no other
//     peer ever sees the bytes.
//   - join/leave/status: every peer except the subject.
//   - iceServers: every peer, each with its own credential.
func (src roomSource) FilterEvent(peerID string, ev roomEvent) (any, bool) {
	switch ev.kind {
	case evICEServers:
		return iceServersPayload{ICEServers: src.s.iceServersFor(peerID, time.Now())}, true
	case evJoin:
		if peerID == ev.peer.ID {
			return nil, false
		}
		return ev.peer, true
	case evLeave:
		if peerID == ev.id {
			return nil, false
		}
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

// publishLocked enqueues one event and advances the room version, the
// pairing that keeps snapshots sortable above every event already
// delivered. Caller holds s.mu. Publish is non-blocking by contract.
func (s *Signaler) publishLocked(rm *room, kind string, ev roomEvent) {
	rm.seq++
	rm.channel.Publish(kind, ev)
}

// mintPeerID returns 16 random bytes hex-encoded. A peer id needs to
// be unguessable (it addresses signaling), so a rand failure refuses
// the join rather than falling back to anything enumerable.
func mintPeerID() (string, bool) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", false
	}
	return hex.EncodeToString(buf[:]), true
}

// Serve upgrades r to a WebSocket, joins the peer to join.Room, and
// runs the read loop until the socket closes. The caller has already
// authorized the request; join is trusted. Blocks. A refused join
// (400 bad Join, 409 room full, 503 closed) is an HTTP status for a
// plain request and, for an upgrade request, an accepted handshake
// closed with code 4000+status (see refuse).
func (s *Signaler) Serve(w http.ResponseWriter, r *http.Request, join Join) {
	if err := validJoin(join); err != nil {
		s.logger.Warn("rtc: join refused", "reason", "bad_join")
		s.refuse(w, r, http.StatusBadRequest, "invalid join")
		return
	}
	peerID := join.PeerID
	if peerID == "" {
		var ok bool
		if peerID, ok = mintPeerID(); !ok {
			s.logger.Warn("rtc: join refused", "reason", "no_peer_id")
			s.refuse(w, r, http.StatusServiceUnavailable, "try again")
			return
		}
	}

	// Cap check before the upgrade: a refused join must be an HTTP
	// response, not a dead socket. A rejoin of an existing id replaces
	// that peer and consumes no new slot.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		s.refuse(w, r, http.StatusServiceUnavailable, "signaler closed")
		return
	}
	now := time.Now()
	merged := s.mergedPeersLocked(join.Room, now)
	rejoin := slices.ContainsFunc(merged, func(p PeerInfo) bool { return p.ID == peerID })
	if !rejoin && len(merged) >= s.cfg.MaxPeers {
		room := scrubLogField(join.Room)
		s.mu.Unlock()
		s.logger.Warn("rtc: join refused", "reason", "room_full", "room", room)
		s.refuse(w, r, http.StatusConflict, "room full")
		return
	}
	s.mu.Unlock()

	ws := s.cfg.WS
	ws.ConnectionID = join.Room + "/" + peerID
	ws.OnClose = nil
	conn, err := stream.Upgrade(w, r, ws)
	if err != nil {
		// Upgrade writes nothing on a handshake it refuses (a plain
		// GET, a cross-origin upgrade); without this the request
		// fell out of the handler as an empty 200.
		http.Error(w, "websocket upgrade required", http.StatusBadRequest)
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
		limiter: newFrameLimiter(s.cfg.MaxFramesPerSecond),
	}

	var oldConn *stream.WebSocketConn
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = conn.CloseWithStatus(4000+http.StatusServiceUnavailable, "signaler closed")
		return
	}
	// The cap again, under the lock that registers: two joins can both
	// pass the pre-upgrade check before either inserts. That check
	// keeps the common refusal an HTTP status; this one keeps the
	// invariant, at the price of a closed socket for the loser.
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
	// A reconnecting tab replaces the old socket: the room sees leave,
	// then join, and the old socket's cleanup no-ops.
	if old := rm.peers[peerID]; old != nil {
		delete(rm.peers, peerID)
		s.publishLocked(rm, evLeave, roomEvent{kind: evLeave, id: peerID})
		s.mirrorLocked(join.Room, fanoutMsg{Kind: evLeave, ID: peerID})
		s.broadcastRosterLocked(join.Room)
		s.spliceSeatLocked(old.seat)
		oldConn = old.conn
	}
	rm.peers[peerID] = p
	rm.emptySince = time.Time{}
	// Seat the socket at the registration that admits it: past the
	// principal's cap the oldest one is displaced (evict-oldest, the
	// replace-on-rejoin policy across rooms), and its connection is
	// closed outside the lock below.
	displaced := s.admitSeatLocked(join.Room, p, join.User)
	s.mu.Unlock()
	if oldConn != nil {
		oldConn.Close()
	}
	for _, c := range displaced {
		c.Close()
	}
	// Hydrate BEFORE announcing. The channel registers a socket only
	// when its snapshot job runs, and delivers an event only to sockets
	// registered when the event is dequeued. Publishing the join first
	// would let an older peer answer it with an offer that the channel
	// enqueues ahead of this socket's registration, and that offer
	// would be lost: the rtc module's impolite side offers the instant
	// it sees a join, so the window sits on the critical path. Connect
	// returns once the snapshot is queued; the join that follows
	// carries a greater sequence and is filtered out for its subject.
	defer s.peerGone(join.Room, p)
	rm.channel.Connect(peerID, conn)

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

	for {
		data, rerr := conn.Read()
		if rerr != nil {
			return
		}
		if !p.limiter.allow(time.Now()) {
			s.logger.Warn("rtc: peer closed", "reason", "frame_rate")
			conn.Close()
			return
		}
		var in inboundMsg
		// Strict decode, the house rule for every inbound request-body
		// decode: a frame naming its kind twice (last wins) or spelling
		// a field with folded case (overwrites the first binding) reads
		// two ways, so the frame is dropped — malformed, like any other
		// JSON the envelope cannot carry unambiguously. Drop, not close:
		// the socket stays usable for well-formed frames.
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

// peerGone is the single leave path: remove the peer, free its seat,
// publish the leave, and start the idle clock when the room emptied. It
// no-ops when the peer was already replaced or the room already dropped,
// so a replacement's inline leave cannot double-fire.
func (s *Signaler) peerGone(roomName string, p *roomPeer) {
	s.mu.Lock()
	rm, ok := s.rooms[roomName]
	if !ok || rm.peers[p.info.ID] != p {
		s.mu.Unlock()
		return
	}
	delete(rm.peers, p.info.ID)
	s.publishLocked(rm, evLeave, roomEvent{kind: evLeave, id: p.info.ID})
	s.mirrorLocked(roomName, fanoutMsg{Kind: evLeave, ID: p.info.ID})
	// The seat frees with the socket: the FIFO never retains a departed
	// peer, so the cap counts exactly the live ones.
	s.spliceSeatLocked(p.seat)
	if len(rm.peers) == 0 {
		s.markEmptyLocked(rm)
	}
	s.broadcastRosterLocked(roomName)
	roomLog, peerLog := scrubLogField(roomName), scrubLogField(p.info.ID)
	s.mu.Unlock()
	s.logger.Debug("rtc: leave", "room", roomLog, "peer", peerLog)
}

// handleSignal validates one inbound signaling frame and delivers it
// locally or over the fanout lane. Validation failures drop the frame;
// only an over-size payload closes the socket.
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
	if !s.remoteHasPeerLocked(roomName, sig.To) {
		s.mu.Unlock()
		return // unknown target: dropped, not an error
	}
	send, nodeID := s.fanoutSend, s.nodeID
	s.mu.Unlock()
	if send != nil {
		publishFanout(send, nodeID, fanoutMsg{Kind: evSignal, Room: roomName, Signal: sig})
	}
}

// handleStatus stores and broadcasts a peer's status document. The
// document must be a JSON object within MaxStatusBytes; anything else
// is dropped and the socket stays usable.
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
	// The status mirror alone: a roster beat here as well doubled the
	// lane messages and multiplied the bytes by the room size for
	// every frame a client is allowed to send (128/s). The heartbeat
	// is the convergence backstop for a mirror a replica missed.
	s.mirrorLocked(roomName, fanoutMsg{Kind: evStatus, ID: p.info.ID, Status: p.info.Status})
	s.mu.Unlock()
}

// isObjectJSON reports whether data's first non-whitespace byte opens
// a JSON object.
func isObjectJSON(data []byte) bool {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// newRoomLocked creates the room and starts its channel goroutine.
// The first room with TURN configured also starts the credential
// refresh loop. Caller holds s.mu.
func (s *Signaler) newRoomLocked(name string) *room {
	rm := &room{name: name, peers: make(map[string]*roomPeer)}
	rm.channel = stream.NewStateChannel[string, roomSnapshot, roomEvent](roomSource{s: s, name: name})
	// Signals are not in the snapshot: a peer that cannot take a
	// frame must reconnect and re-hydrate (the far side then sees a
	// leave and a join and renegotiates), never run on silently
	// without its answer.
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

// markEmptyLocked starts a room's idle clock and arms its sweep. It
// is the ONE place a room becomes empty (peerGone, and the lane join
// that displaces the last local socket): the sweep runs on its own
// clock because nothing else is guaranteed to touch the room again.
// The timer sweeps only this room, so a burst of emptyings is not a
// burst of full-table walks under s.mu. A rejoin before it fires is
// skipped (peers > 0); a closed Signaler holds no rooms. Caller holds
// s.mu.
func (s *Signaler) markEmptyLocked(rm *room) {
	rm.emptySince = time.Now()
	name := rm.name
	time.AfterFunc(s.cfg.RoomIdleTTL, func() { s.sweepRoom(name) })
}

// sweepRoom is the timer callback: drop the named room if it is still
// empty and past its TTL.
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

// idleLocked reports whether a room has been empty for RoomIdleTTL.
func (s *Signaler) idleLocked(rm *room, now time.Time) bool {
	return len(rm.peers) == 0 && !rm.emptySince.IsZero() && now.Sub(rm.emptySince) >= s.cfg.RoomIdleTTL
}

// sweepIdleLocked drops every room that lost its last local peer more
// than RoomIdleTTL ago; Rooms calls it so the accessor never lists a
// room whose timer is about to fire. Caller holds s.mu. Only the
// LOCAL room goes: what other replicas hold for the name lives in
// s.remote under its own TTL (sweepExpiredRemoteLocked), and dropping
// it here made a live remote peer vanish from the roster until the
// next heartbeat.
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

// mergedPeersLocked returns the local plus live-remote roster of a
// room, deduped by id (local wins) and sorted by Order then ID.
// Caller holds s.mu.
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
	if replicas, ok := s.remote[roomName]; ok {
		for _, node := range slices.Sorted(maps.Keys(replicas)) {
			entry := replicas[node]
			if now.After(entry.expires) {
				continue
			}
			for _, m := range entry.members {
				if seen[m.ID] {
					continue
				}
				out = append(out, m)
				seen[m.ID] = true
			}
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

// remoteHasPeerLocked reports whether a live remote replica currently
// holds the peer. Caller holds s.mu.
func (s *Signaler) remoteHasPeerLocked(roomName, peerID string) bool {
	replicas, ok := s.remote[roomName]
	if !ok {
		return false
	}
	now := time.Now()
	for _, node := range slices.Sorted(maps.Keys(replicas)) {
		entry := replicas[node]
		if now.After(entry.expires) {
			continue
		}
		if slices.ContainsFunc(entry.members, func(m PeerInfo) bool { return m.ID == peerID }) {
			return true
		}
	}
	return false
}

// frameLimiter is a per-socket token bucket: rate tokens per second,
// burst 2x rate, no initial leniency beyond the burst.
type frameLimiter struct {
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newFrameLimiter(perSecond int) frameLimiter {
	burst := 2 * float64(perSecond)
	return frameLimiter{rate: float64(perSecond), burst: burst, tokens: burst}
}

func (f *frameLimiter) allow(now time.Time) bool {
	if !f.last.IsZero() {
		if elapsed := now.Sub(f.last).Seconds(); elapsed > 0 {
			f.tokens = min(f.burst, f.tokens+elapsed*f.rate)
			f.last = now
		}
	} else {
		f.last = now
	}
	if f.tokens >= 1 {
		f.tokens--
		return true
	}
	return false
}
