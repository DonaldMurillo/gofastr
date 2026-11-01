package rtc

// fanout.go is the cross-replica lane: full-roster heartbeats plus
// event mirrors over a core/fanout topic, mirroring the convergence
// model of core-ui/island's presence lane. Each replica broadcasts its
// full local roster per active room on every local join/leave/status
// and on a 15 s heartbeat; receivers keep a TTL'd (45 s) per-(replica,
// room) table capped at 512 replicas per room. A dropped message heals
// on the next beat; a crashed replica's peers vanish within TTL; a
// graceful stop publishes empty rosters synchronously first. Join/
// leave/status/signal mirrors give local members prompt events between
// beats. Without a fanout attached nothing here runs and behaviour is
// the single-replica result.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// rtcFanoutTopic is the lane every replica of a deployment shares.
const rtcFanoutTopic = "gofastr.rtc"

// Convergence intervals; the Signaler carries test-overridable copies.
const (
	rtcHeartbeat = 15 * time.Second // full-roster rebroadcast period
	rtcRemoteTTL = 45 * time.Second // 3x heartbeat tolerates two lost beats

	// peerRecheckEvery paces the idle re-check of a joined socket's
	// principal; inbound frames re-check on arrival.
	peerRecheckEvery = 30 * time.Second

	// maxRemoteReplicasPerRoom bounds the remote table per room so a
	// forged-replica flood cannot grow it unboundedly.
	maxRemoteReplicasPerRoom = 512

	// rtcGraceTimeout bounds the synchronous empty-roster publishes on
	// stop; past it the TTL is the fallback.
	rtcGraceTimeout = 2 * time.Second
)

// fanoutMsg is the wire shape on the rtc lane: {"n":node,"k":kind,
// "r":room, ...}. Members carries a full local roster for k=roster
// (empty means the replica left the room).
type fanoutMsg struct {
	Node    string          `json:"n"`
	Kind    string          `json:"k"`
	Room    string          `json:"r"`
	Peer    *PeerInfo       `json:"p,omitempty"` // join
	ID      string          `json:"i,omitempty"` // leave
	Status  json.RawMessage `json:"s,omitempty"` // status
	Signal  *signalPayload  `json:"g,omitempty"` // signal
	Members []PeerInfo      `json:"m,omitempty"` // roster
	// Seq numbers a signal per sending socket and target seat, from 1;
	// ToOrder is the target seat's Order the sender addressed. The
	// target's replica closes the target on a gap: a backend subscriber
	// queue that drops a signal on overflow tells nobody, so the number
	// is the only way the loss shows. Zero means unnumbered.
	Seq     uint64 `json:"q,omitempty"`
	ToOrder int64  `json:"o,omitempty"`
	// At is the sender's clock at publish, unix nanoseconds. A join
	// mirror older than the local socket it would displace is stale (the
	// peer moved HERE after that mirror left) and is dropped; see
	// remoteJoinLocked. Replica clocks therefore need to agree to within a
	// client's reconnect time, the usual NTP posture for a cluster.
	At int64 `json:"t,omitempty"`
}

// remoteEntry is one replica's contributed roster for a room plus its
// freshness deadline.
type remoteEntry struct {
	members []PeerInfo
	expires time.Time
}

// publishFanout marshals one message through the (async, non-blocking)
// send. Marshal of a plain struct cannot fail in a way the program can
// recover from. failed, when non-nil, runs if the message is not
// published (see fanout.PublishQueueReporting), a marshal failure
// included; nil leaves the loss to the lossy lane.
func publishFanout(send func([]byte, func(error)), nodeID string, msg fanoutMsg, failed func(error)) {
	msg.Node = nodeID
	msg.At = time.Now().UnixNano()
	body, err := json.Marshal(msg)
	if err != nil {
		if failed != nil {
			failed(err)
		}
		return
	}
	send(fanout.Wrap(nodeID, body), failed)
}

// SetFanout wires cross-replica relay. Duck-typed to the framework's
// Mountable wiring (see App.Mount): with WithFanout attached, mounting
// the Signaler calls this automatically. Returns the cancel that stops
// the subscription and heartbeat. An error if a fanout is already
// attached, f is nil, or the Signaler is closed.
func (s *Signaler) SetFanout(f fanout.Fanout) (func(), error) {
	if f == nil {
		return nil, errors.New("rtc: SetFanout: nil fanout")
	}
	nodeID := fanout.NewNodeID()
	send, stopQ := fanout.PublishQueueReporting(f, rtcFanoutTopic, 0)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		stopQ()
		return nil, errors.New("rtc: SetFanout after Close")
	}
	if s.fanout != nil {
		s.mu.Unlock()
		stopQ()
		return nil, errors.New("rtc: fanout already attached")
	}
	s.fanout = f
	s.nodeID = nodeID
	s.fanoutSend = send
	s.fanoutStopQ = stopQ
	s.remote = make(map[string]map[string]*remoteEntry)
	done := make(chan struct{})
	s.beatDone = done
	s.mu.Unlock()

	cancel, err := f.Subscribe(rtcFanoutTopic, s.onFanoutMessage)
	if err != nil {
		s.mu.Lock()
		s.clearFanoutLocked()
		s.mu.Unlock()
		stopQ()
		return nil, fmt.Errorf("rtc: SetFanout: subscribe: %w", err)
	}
	s.mu.Lock()
	s.fanoutCancel = cancel
	s.mu.Unlock()

	s.beatWG.Add(1)
	go s.heartbeatLoop(done)
	// Announce current rooms so peers converge without waiting for the
	// first beat (covers rooms that formed before the fanout attached).
	s.announceAll()

	var once sync.Once
	return func() {
		once.Do(func() { s.stopFanout(nil) })
	}, nil
}

// clearFanoutLocked resets the fanout fields. Caller holds s.mu.
func (s *Signaler) clearFanoutLocked() {
	s.fanout = nil
	s.nodeID = ""
	s.fanoutSend = nil
	s.fanoutStopQ = nil
	s.fanoutCancel = nil
	s.remote = make(map[string]map[string]*remoteEntry)
	if s.beatDone != nil {
		close(s.beatDone)
		s.beatDone = nil
	}
}

// stopFanout is the graceful detach behind Close and the SetFanout
// cancel: stop the heartbeat and the publish queue, publish empty
// rosters synchronously, then unsubscribe. rooms is the room map to
// announce empty; nil means the live one. Close passes the snapshot it
// took before emptying the live map, or nothing would be announced.
func (s *Signaler) stopFanout(rooms map[string]*room) {
	s.mu.Lock()
	f := s.fanout
	if f == nil {
		s.mu.Unlock()
		s.beatWG.Wait()
		return
	}
	nodeID := s.nodeID
	cancel := s.fanoutCancel
	stopQ := s.fanoutStopQ
	if rooms == nil {
		rooms = s.rooms
	}
	names := make([]string, 0, len(rooms))
	for _, name := range slices.Sorted(maps.Keys(rooms)) {
		if len(rooms[name].peers) > 0 {
			names = append(names, name)
		}
	}
	s.clearFanoutLocked()
	s.mu.Unlock()

	// The empty rosters go last: wait out the heartbeat, then stop the
	// queue, which waits for its publish in flight. A roster that landed
	// after them restored this replica's peers on the others until the
	// TTL.
	s.beatWG.Wait()
	if stopQ != nil {
		stopQ()
	}
	// Synchronous, not through the queue: the queue is stopped. Lossy
	// lane: a timeout falls back to TTL.
	publishEmptyRosters(f, nodeID, names)

	if cancel != nil {
		cancel()
	}
}

// publishEmptyRosters tells every peer this replica's rooms are empty
// now, so a rolling restart converges without waiting out the TTL.
func publishEmptyRosters(f fanout.Fanout, nodeID string, roomNames []string) {
	if nodeID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), rtcGraceTimeout)
	defer cancel()
	for _, name := range roomNames {
		// Node and At as publishFanout stamps them: a receiver drops a
		// message naming no node.
		msg := fanoutMsg{Node: nodeID, Kind: evRoster, Room: name, Members: []PeerInfo{}, At: time.Now().UnixNano()}
		body, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		_ = f.Publish(ctx, rtcFanoutTopic, fanout.Wrap(nodeID, body))
	}
}

// heartbeatLoop is the per-replica convergence goroutine: sweep
// expired remote rosters, then rebroadcast every active room's full
// roster. Exits when the captured done channel closes.
func (s *Signaler) heartbeatLoop(done chan struct{}) {
	defer s.beatWG.Done()
	for {
		s.mu.Lock()
		every := s.heartbeatEvery
		s.mu.Unlock()
		select {
		case <-done:
			return
		case <-time.After(every):
		}
		s.mu.Lock()
		if s.fanout == nil {
			s.mu.Unlock()
			return
		}
		s.sweepExpiredRemoteLocked(time.Now())
		names := make([]string, 0, len(s.rooms))
		for _, name := range slices.Sorted(maps.Keys(s.rooms)) {
			if len(s.rooms[name].peers) > 0 {
				names = append(names, name)
			}
		}
		s.mu.Unlock()
		for _, name := range names {
			s.announceRoom(name)
		}
	}
}

// haltHeartbeat stops the heartbeat goroutine WITHOUT a graceful
// leave: a faithful crash simulation for tests. The subscription stays
// up; a later stopFanout finishes teardown.
func (s *Signaler) haltHeartbeat() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.beatDone != nil {
		close(s.beatDone)
		s.beatDone = nil
	}
}

// announceAll rebroadcasts every active room (SetFanout attach).
func (s *Signaler) announceAll() {
	s.mu.Lock()
	names := make([]string, 0, len(s.rooms))
	for _, name := range slices.Sorted(maps.Keys(s.rooms)) {
		if len(s.rooms[name].peers) > 0 {
			names = append(names, name)
		}
	}
	s.mu.Unlock()
	for _, name := range names {
		s.announceRoom(name)
	}
}

// announceRoom publishes this replica's full local roster for one
// room. No-op with no fanout attached.
func (s *Signaler) announceRoom(roomName string) {
	// Enqueued under s.mu like every roster a mutation publishes, so a
	// heartbeat snapshot cannot be queued behind a newer roster and
	// overtake it on the FIFO lane. The queue send is non-blocking.
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcastRosterLocked(roomName)
}

// broadcastRosterLocked announces one room after a local mutation.
// Caller holds s.mu; the send is a non-blocking enqueue, so the lock
// hold stays short and roster order follows event order on the queue.
func (s *Signaler) broadcastRosterLocked(roomName string) {
	if s.fanoutSend == nil {
		return
	}
	msg := fanoutMsg{Kind: evRoster, Room: roomName, Members: []PeerInfo{}}
	if rm, ok := s.rooms[roomName]; ok {
		for _, id := range slices.Sorted(maps.Keys(rm.peers)) {
			msg.Members = append(msg.Members, rm.peers[id].info)
		}
	}
	publishFanout(s.fanoutSend, s.nodeID, msg, nil)
}

// onFanoutMessage integrates one lane message. Own-node envelopes are
// dropped; remote events are applied to the table and mirrored into
// the local room channel so local members see them between beats.
// Never re-publishes on receive (no loop).
func (s *Signaler) onFanoutMessage(raw []byte) {
	origin, body, err := fanout.Unwrap(raw)
	if err != nil {
		return
	}
	var msg fanoutMsg
	if json.Unmarshal(body, &msg) != nil {
		return
	}
	if msg.Kind == "" || msg.Room == "" {
		return
	}
	var kick, full, lost []*stream.WebSocketConn
	s.mu.Lock()
	if s.fanout == nil || msg.Node == "" || msg.Node == s.nodeID || origin == s.nodeID {
		s.mu.Unlock()
		return
	}
	switch msg.Kind {
	case evJoin:
		s.remoteJoinLocked(origin, &msg, &kick)
	case evLeave:
		s.remoteLeaveLocked(origin, &msg)
	case evStatus:
		s.remoteStatusLocked(origin, &msg)
	case evSignal:
		s.remoteSignalLocked(&msg, &lost)
	case evRoster:
		s.remoteRosterLocked(origin, &msg, &full)
	}
	s.mu.Unlock()
	// Displaced local sockets close on their own goroutine: Close
	// performs a closing handshake that can wait out CloseTimeout,
	// and this subscriber goroutine must keep draining the lane (a
	// stall here would expire live remote entries on other replicas).
	for _, conn := range kick {
		go conn.Close()
	}
	for _, conn := range full {
		s.logger.Warn("rtc: peer closed", "reason", "room_full")
		go func() { _ = conn.CloseWithStatus(4000+http.StatusConflict, "room full") }()
	}
	for _, conn := range lost {
		s.logger.Warn("rtc: peer closed", "reason", "signal_lost")
		go conn.Close()
	}
}

// mirrorLocked publishes one event mirror (join/leave/status) to the
// lane so remote replicas' members see it between roster beats. The
// roster beat that follows at every call site is the backstop. Caller
// holds s.mu; the send is a non-blocking enqueue.
func (s *Signaler) mirrorLocked(roomName string, msg fanoutMsg) {
	if s.fanoutSend == nil {
		return
	}
	msg.Room = roomName
	publishFanout(s.fanoutSend, s.nodeID, msg, nil)
}

// localRoomLocked returns the room when it has local members to tell.
// Caller holds s.mu.
func (s *Signaler) localRoomLocked(roomName string) *room {
	if rm, ok := s.rooms[roomName]; ok && len(rm.peers) > 0 {
		return rm
	}
	return nil
}

// validRemotePeer bounds an inbound roster entry before it reaches
// the table or a local channel.
func (s *Signaler) validRemotePeer(p PeerInfo) bool {
	return printableToken(p.ID, 1, 64) &&
		printableToken(p.Role, 0, 32) &&
		len(p.User) <= 128 && !textsafe.HasControlBytes(p.User) &&
		len(p.DisplayName) <= 64 && !textsafe.HasControlBytes(p.DisplayName) &&
		(p.Status == nil || (len(p.Status) <= s.cfg.MaxStatusBytes && json.Valid(p.Status) && isObjectJSON(p.Status)))
}

// tableForLocked returns (creating when allowed) the replica map for a
// room. Caller holds s.mu.
func (s *Signaler) tableForLocked(roomName string) map[string]*remoteEntry {
	replicas, ok := s.remote[roomName]
	if !ok {
		replicas = make(map[string]*remoteEntry)
		s.remote[roomName] = replicas
	}
	return replicas
}

// remoteJoinLocked applies one remote join: replace a same-id local
// socket (the peer moved replicas), record the member, and tell the
// local room. When it displaces a local socket it appends the conn to
// kick: closing it performs a close handshake that can wait out
// CloseTimeout, which must not happen under s.mu.
func (s *Signaler) remoteJoinLocked(origin string, msg *fanoutMsg, kick *[]*stream.WebSocketConn) {
	if msg.Peer == nil || !s.validRemotePeer(*msg.Peer) {
		return
	}
	// MaxPeers caps the room local plus remote here as on the roster
	// beat: a new id past the cap is dropped, before the displacement
	// below can remove a local peer for it. A known id (an update, or
	// a peer of this replica moving away) is never refused.
	now := time.Now()
	known := false
	for _, p := range s.mergedPeersLocked(msg.Room, now) {
		if p.ID == msg.Peer.ID {
			known = true
			break
		}
	}
	if !known && len(s.mergedPeersLocked(msg.Room, now)) >= s.cfg.MaxPeers {
		return
	}
	prev := s.remoteViewLocked(msg.Room, time.Time{})
	if rm := s.rooms[msg.Room]; rm != nil {
		if old := rm.peers[msg.Peer.ID]; old != nil {
			// A mirror published before this socket joined is the peer's
			// OLD seat announcing itself late (a bus that delivered one
			// replica's lane behind the client's reconnect), not a move
			// away: dropping it keeps the live socket. Order is the local
			// join time in the same unit. #474.
			if msg.At != 0 && msg.At < old.info.Order {
				return
			}
			delete(rm.peers, msg.Peer.ID)
			rm.listed.forget(msg.Peer.ID)
			s.publishLocked(rm, evLeave, roomEvent{kind: evLeave, id: msg.Peer.ID})
			// The displaced socket's peerGone no-ops (the room no longer
			// holds it), so its seat frees here, or the FIFO keeps a dead
			// socket that the cap later counts against a live one.
			s.spliceSeatLocked(old.seat)
			if len(rm.peers) == 0 {
				s.markEmptyLocked(rm)
			}
			s.broadcastRosterLocked(msg.Room)
			*kick = append(*kick, old.conn)
			// The room just heard a leave, so the join below is owed
			// whatever the remote view held for the id.
			delete(prev, msg.Peer.ID)
		}
	}
	peer := *msg.Peer
	replicas := s.tableForLocked(msg.Room)
	entry, ok := replicas[origin]
	if !ok {
		if len(replicas) >= maxRemoteReplicasPerRoom {
			return // cap: the lossy model tolerates the drop
		}
		entry = &remoteEntry{members: []PeerInfo{}}
		replicas[origin] = entry
	}
	entry.expires = time.Now().Add(s.remoteTTL)
	if i := slices.IndexFunc(entry.members, func(m PeerInfo) bool { return m.ID == peer.ID }); i >= 0 {
		entry.members[i] = peer
	} else {
		entry.members = append(entry.members, peer)
	}
	// A roster beat from the joining replica can list the peer before
	// its own join mirror lands (another socket's announce, or a seat
	// eviction, beat first). The delta then publishes nothing for the
	// mirror: rtc.js reads a second join for a known peer as a
	// reconnect and rebuilds the connection it just made.
	s.announceRemoteDeltaLocked(msg.Room, prev, s.remoteViewLocked(msg.Room, time.Time{}))
}

// remoteLeaveLocked drops one remote member and tells the local room.
func (s *Signaler) remoteLeaveLocked(origin string, msg *fanoutMsg) {
	if msg.ID == "" {
		return
	}
	replicas, ok := s.remote[msg.Room]
	if !ok {
		return
	}
	entry, ok := replicas[origin]
	if !ok {
		return
	}
	entry.expires = time.Now().Add(s.remoteTTL)
	if i := slices.IndexFunc(entry.members, func(m PeerInfo) bool { return m.ID == msg.ID }); i >= 0 {
		prev := s.remoteViewLocked(msg.Room, time.Time{})
		entry.members = slices.Delete(entry.members, i, i+1)
		// The delta, not the mirror, decides: a peer another replica
		// still holds (it reconnected there with the same id), or this
		// replica holds locally, has not left the room.
		s.announceRemoteDeltaLocked(msg.Room, prev, s.remoteViewLocked(msg.Room, time.Time{}))
	}
}

// remoteStatusLocked updates one remote member's status document.
func (s *Signaler) remoteStatusLocked(origin string, msg *fanoutMsg) {
	if msg.ID == "" || len(msg.Status) > s.cfg.MaxStatusBytes || !json.Valid(msg.Status) || !isObjectJSON(msg.Status) {
		return
	}
	replicas, ok := s.remote[msg.Room]
	if !ok {
		return
	}
	entry, ok := replicas[origin]
	if !ok {
		return
	}
	entry.expires = time.Now().Add(s.remoteTTL)
	if i := slices.IndexFunc(entry.members, func(m PeerInfo) bool { return m.ID == msg.ID }); i >= 0 {
		// The delta decides, as for a leave: a status from a replica
		// whose seat the peer has since left (it reconnected elsewhere)
		// changes no seat that stands for the peer, so it says nothing.
		prev := s.remoteViewLocked(msg.Room, time.Time{})
		entry.members[i].Status = append(json.RawMessage(nil), msg.Status...)
		s.announceRemoteDeltaLocked(msg.Room, prev, s.remoteViewLocked(msg.Room, time.Time{}))
	}
}

// remoteSignalLocked delivers one fanout-routed signal to its target
// when this replica owns it. A numbered signal addressed to the target's
// current seat that skips a number means the lane lost one on the way
// (the backend's subscriber queue drops its oldest payload on overflow,
// silently): the target's socket is appended to lost and closed outside
// the lock, as the sender's socket closes on a publish-side loss, so the
// client reconnects and the pair renegotiates.
func (s *Signaler) remoteSignalLocked(msg *fanoutMsg, lost *[]*stream.WebSocketConn) {
	sig := msg.Signal
	if sig == nil || sig.To == "" || sig.From == "" || sig.From == sig.To {
		return
	}
	switch sig.Type {
	case "offer", "answer", "ice":
	default:
		return
	}
	if len(sig.Data) > s.cfg.MaxSignalBytes {
		return
	}
	rm := s.localRoomLocked(msg.Room)
	if rm == nil {
		return
	}
	target, mine := rm.peers[sig.To]
	if !mine {
		return
	}
	// A signal addressed to an earlier seat of this id predates the
	// socket's count; it is delivered as before and not counted.
	if msg.ToOrder == target.info.Order && target.signalGapLocked(sig.From, msg.Seq) {
		*lost = append(*lost, target.conn)
		return
	}
	s.publishLocked(rm, evSignal, roomEvent{kind: evSignal, signal: sig})
}

// remoteRosterLocked replaces one replica's contribution for a room
// (the heartbeat payload) and mirrors membership deltas into the local
// room. An empty roster deletes the entry (graceful leave). Local
// sockets the beat pushed past MaxPeers are appended to full, closed
// outside the lock as the remote join displacements are.
func (s *Signaler) remoteRosterLocked(origin string, msg *fanoutMsg, full *[]*stream.WebSocketConn) {
	if msg.Room == "" {
		return
	}
	// MaxPeers caps a room local plus remote, so no honest replica
	// holds more than that for one room: a beat naming more is dropped
	// whole, as a beat with one bad member is. Without the bound one
	// lane message put thousands of members past the cap and a join
	// per member on every local socket.
	if len(msg.Members) > s.cfg.MaxPeers {
		return
	}
	for _, m := range msg.Members {
		if !s.validRemotePeer(m) {
			return // drop the whole beat, not a mutated entry
		}
	}
	now := time.Now()
	prev := s.remoteViewLocked(msg.Room, time.Time{})
	replicas := s.remote[msg.Room]
	entry, ok := replicas[origin]
	if !ok {
		if len(msg.Members) == 0 {
			return // an empty beat from a replica never seen creates nothing
		}
		if len(replicas) >= maxRemoteReplicasPerRoom {
			return
		}
		replicas = s.tableForLocked(msg.Room)
		entry = &remoteEntry{}
		replicas[origin] = entry
	}
	entry.expires = now.Add(s.remoteTTL)
	if len(msg.Members) == 0 {
		delete(replicas, origin)
	} else {
		entry.members = append(entry.members[:0], msg.Members...)
	}
	// Mirror the delta against the PREVIOUS remote view (not the
	// local roster) so a periodic beat that changes nothing publishes
	// nothing.
	s.announceRemoteDeltaLocked(msg.Room, prev, s.remoteViewLocked(msg.Room, time.Time{}))
	*full = append(*full, s.overCapLocalLocked(msg.Room)...)
}

// overCapLocalLocked returns the local sockets ranked past MaxPeers in a
// room's merged roster. Two replicas can each admit a join before their
// beats meet, each checking the cap against a view without the other's
// peer, so the merged room can stand past the cap. Every replica ranks
// the same merged roster by Order then ID and closes only its own peers
// past the cap: the latest seats lose, each on its own replica, and
// their leaves bring every view back under the cap. Caller holds s.mu.
func (s *Signaler) overCapLocalLocked(roomName string) []*stream.WebSocketConn {
	rm := s.rooms[roomName]
	if rm == nil {
		return nil
	}
	merged := s.mergedPeersLocked(roomName, time.Now())
	if len(merged) <= s.cfg.MaxPeers {
		return nil
	}
	var out []*stream.WebSocketConn
	for _, p := range merged[s.cfg.MaxPeers:] {
		if lp := rm.peers[p.ID]; lp != nil {
			out = append(out, lp.conn)
		}
	}
	return out
}

// announceRemoteDeltaLocked tells the local room how its remote view
// moved from prev to after: the one place a remote membership change
// reaches local sockets. A join goes out for an id new to the view or
// whose Order rose (the peer reconnected on another replica with the
// same id, which is what rtc.js reads a repeated join as). A status
// goes out when the seat standing for an id kept its Order but carries
// a different status, so a beat repairs a status mirror the lane
// dropped. A leave goes out only when no remote entry holds the id any
// more. Local wins
// both ways: a beat cannot restate a peer this replica holds (its
// name, user, and role are server-derived here), and a peer that
// migrated onto this replica leaves the remote view but not the room.
// Caller holds s.mu.
func (s *Signaler) announceRemoteDeltaLocked(roomName string, prev, after map[string]PeerInfo) {
	rm := s.localRoomLocked(roomName)
	if rm == nil {
		return
	}
	for _, id := range slices.Sorted(maps.Keys(after)) {
		if rm.peers[id] != nil {
			continue
		}
		was, had := prev[id]
		switch now := after[id]; {
		case !had || now.Order > was.Order:
			s.publishLocked(rm, evJoin, roomEvent{kind: evJoin, peer: now})
		case len(now.Status) > 0 && !bytes.Equal(now.Status, was.Status):
			s.publishLocked(rm, evStatus, roomEvent{kind: evStatus, id: id, status: now.Status})
		}
	}
	for _, id := range slices.Sorted(maps.Keys(prev)) {
		if _, still := after[id]; !still && rm.peers[id] == nil {
			s.publishLocked(rm, evLeave, roomEvent{kind: evLeave, id: id})
		}
	}
}

// remoteViewLocked returns the merged id->PeerInfo view of a room's
// remote entries. When replicas disagree on an id, the latest seat
// (greatest Order; ties go to the first node in sort order) stands for
// it, so the view does not flip between two replicas' copies as their
// beats interleave. A zero now keeps entries past their TTL that the
// sweep has not removed yet: that is the view local sockets were last
// told, and only the sweep (sweepExpiredRemoteLocked) announces an
// expiry. Caller holds s.mu.
func (s *Signaler) remoteViewLocked(roomName string, now time.Time) map[string]PeerInfo {
	view := map[string]PeerInfo{}
	replicas, ok := s.remote[roomName]
	if !ok {
		return view
	}
	for _, node := range slices.Sorted(maps.Keys(replicas)) {
		entry := replicas[node]
		if !now.IsZero() && now.After(entry.expires) {
			continue
		}
		for _, m := range entry.members {
			if seen, ok := view[m.ID]; !ok || m.Order > seen.Order {
				view[m.ID] = m
			}
		}
	}
	return view
}

// sweepExpiredRemoteLocked drops remote entries past their TTL and
// mirrors the vanished members as leaves. The crash fallback: a
// replica that stops beating disappears within TTL. Caller holds s.mu.
func (s *Signaler) sweepExpiredRemoteLocked(now time.Time) {
	for _, roomName := range slices.Sorted(maps.Keys(s.remote)) {
		replicas := s.remote[roomName]
		prev := s.remoteViewLocked(roomName, time.Time{})
		for _, node := range slices.Sorted(maps.Keys(replicas)) {
			if now.After(replicas[node].expires) {
				delete(replicas, node)
			}
		}
		// The delta, not the expired entries' members: a peer that
		// reconnected on another live replica with the same id is still
		// in the room, and nothing that replica beats later would
		// announce it again.
		s.announceRemoteDeltaLocked(roomName, prev, s.remoteViewLocked(roomName, time.Time{}))
		if len(replicas) == 0 {
			delete(s.remote, roomName)
		}
	}
}
