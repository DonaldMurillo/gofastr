package rtc

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

const rtcFanoutTopic = "gofastr.rtc"

const (
	rtcHeartbeat = 15 * time.Second
	rtcRemoteTTL = 45 * time.Second

	peerRecheckEvery = 30 * time.Second

	maxRemoteReplicasPerRoom = 512

	rtcGraceTimeout = 2 * time.Second
)

type fanoutMsg struct {
	Node    string          `json:"n"`
	Kind    string          `json:"k"`
	Room    string          `json:"r"`
	Peer    *PeerInfo       `json:"p,omitempty"`
	ID      string          `json:"i,omitempty"`
	Status  json.RawMessage `json:"s,omitempty"`
	Signal  *signalPayload  `json:"g,omitempty"`
	Members []PeerInfo      `json:"m,omitempty"`
	Seq     uint64          `json:"q,omitempty"`
	ToOrder int64           `json:"o,omitempty"`
	At      int64           `json:"t,omitempty"`
}

type remoteEntry struct {
	members []PeerInfo
	expires time.Time
}

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

// SetFanout wires cross-replica relay.
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
	s.announceAll()

	var once sync.Once
	return func() {
		once.Do(func() { s.stopFanout(nil) })
	}, nil
}

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

	s.beatWG.Wait()
	if stopQ != nil {
		stopQ()
	}
	publishEmptyRosters(f, nodeID, names)

	if cancel != nil {
		cancel()
	}
}

func publishEmptyRosters(f fanout.Fanout, nodeID string, roomNames []string) {
	if nodeID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), rtcGraceTimeout)
	defer cancel()
	for _, name := range roomNames {
		msg := fanoutMsg{Node: nodeID, Kind: evRoster, Room: name, Members: []PeerInfo{}}
		body, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		_ = f.Publish(ctx, rtcFanoutTopic, fanout.Wrap(nodeID, body))
	}
}

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

func (s *Signaler) haltHeartbeat() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.beatDone != nil {
		close(s.beatDone)
		s.beatDone = nil
	}
}

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

func (s *Signaler) announceRoom(roomName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcastRosterLocked(roomName)
}

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
	closeDetached(kick...)
	for _, conn := range full {
		s.logger.Warn("rtc: peer closed", "reason", "room_full")
		go func() { _ = conn.CloseWithStatus(4000+http.StatusConflict, "room full") }()
	}
	for range lost {
		s.logger.Warn("rtc: peer closed", "reason", "signal_lost")
	}
	closeDetached(lost...)
}

func (s *Signaler) mirrorLocked(roomName string, msg fanoutMsg) {
	if s.fanoutSend == nil {
		return
	}
	msg.Room = roomName
	publishFanout(s.fanoutSend, s.nodeID, msg, nil)
}

func (s *Signaler) localRoomLocked(roomName string) *room {
	if rm, ok := s.rooms[roomName]; ok && len(rm.peers) > 0 {
		return rm
	}
	return nil
}

func (s *Signaler) validRemotePeer(p PeerInfo) bool {
	return printableToken(p.ID, 1, 64) &&
		printableToken(p.Role, 0, 32) &&
		len(p.User) <= 128 && !textsafe.HasControlBytes(p.User) &&
		len(p.DisplayName) <= 64 && !textsafe.HasControlBytes(p.DisplayName) &&
		(p.Status == nil || (len(p.Status) <= s.cfg.MaxStatusBytes && json.Valid(p.Status) && isObjectJSON(p.Status)))
}

func (s *Signaler) tableForLocked(roomName string) map[string]*remoteEntry {
	replicas, ok := s.remote[roomName]
	if !ok {
		replicas = make(map[string]*remoteEntry)
		s.remote[roomName] = replicas
	}
	return replicas
}

func (s *Signaler) remoteJoinLocked(origin string, msg *fanoutMsg, kick *[]*stream.WebSocketConn) {
	if msg.Peer == nil || !s.validRemotePeer(*msg.Peer) {
		return
	}
	now := time.Now()
	merged := s.mergedPeersLocked(msg.Room, now)
	known := false
	for _, p := range merged {
		if p.ID == msg.Peer.ID {
			known = true
			break
		}
	}
	if !known && len(merged) >= s.cfg.MaxPeers {
		return
	}
	prev := s.remoteViewLocked(msg.Room, time.Time{})
	if rm := s.rooms[msg.Room]; rm != nil {
		if old := rm.peers[msg.Peer.ID]; old != nil {
			if msg.Peer.Order <= old.info.Order {
				return
			}
			delete(rm.peers, msg.Peer.ID)
			rm.listed.forget(msg.Peer.ID)
			s.publishLocked(rm, evLeave, roomEvent{kind: evLeave, id: msg.Peer.ID})
			s.spliceSeatLocked(old.seat)
			if len(rm.peers) == 0 {
				s.markEmptyLocked(rm)
			}
			s.broadcastRosterLocked(msg.Room)
			*kick = append(*kick, s.detachLocked(rm, old))
			delete(prev, msg.Peer.ID)
		}
	}
	peer := *msg.Peer
	replicas := s.tableForLocked(msg.Room)
	entry, ok := replicas[origin]
	if !ok {
		if len(replicas) >= maxRemoteReplicasPerRoom {
			return
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
	s.announceRemoteDeltaLocked(msg.Room, prev, s.remoteViewLocked(msg.Room, time.Time{}))
}

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
		s.announceRemoteDeltaLocked(msg.Room, prev, s.remoteViewLocked(msg.Room, time.Time{}))
	}
}

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
		prev := s.remoteViewLocked(msg.Room, time.Time{})
		entry.members[i].Status = append(json.RawMessage(nil), msg.Status...)
		s.announceRemoteDeltaLocked(msg.Room, prev, s.remoteViewLocked(msg.Room, time.Time{}))
	}
}

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
	if msg.ToOrder == target.info.Order && target.signalGapLocked(sig.From, msg.Seq) {
		*lost = append(*lost, target.conn)
		return
	}
	s.publishLocked(rm, evSignal, roomEvent{kind: evSignal, signal: sig})
}

func (s *Signaler) remoteRosterLocked(origin string, msg *fanoutMsg, full *[]*stream.WebSocketConn) {
	if msg.Room == "" {
		return
	}
	if len(msg.Members) > s.cfg.MaxPeers {
		return
	}
	for _, m := range msg.Members {
		if !s.validRemotePeer(m) {
			return
		}
	}
	now := time.Now()
	prev := s.remoteViewLocked(msg.Room, time.Time{})
	replicas := s.remote[msg.Room]
	entry, ok := replicas[origin]
	if !ok {
		if len(msg.Members) == 0 {
			return
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
	s.announceRemoteDeltaLocked(msg.Room, prev, s.remoteViewLocked(msg.Room, time.Time{}))
	*full = append(*full, s.overCapLocalLocked(msg.Room)...)
}

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

func (s *Signaler) sweepExpiredRemoteLocked(now time.Time) {
	for _, roomName := range slices.Sorted(maps.Keys(s.remote)) {
		replicas := s.remote[roomName]
		prev := s.remoteViewLocked(roomName, time.Time{})
		for _, node := range slices.Sorted(maps.Keys(replicas)) {
			if now.After(replicas[node].expires) {
				delete(replicas, node)
			}
		}
		s.announceRemoteDeltaLocked(roomName, prev, s.remoteViewLocked(roomName, time.Time{}))
		if len(replicas) == 0 {
			delete(s.remote, roomName)
		}
	}
}
