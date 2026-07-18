package island

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// IslandUpdate represents an update to push to a client.
type IslandUpdate struct {
	IslandID string
	HTML     string
}

// streamEntry holds a per-session update stream. The data channel is never
// closed; instead, the done channel is closed on Unsubscribe to signal
// termination. This prevents panics from sending to a closed channel.
//
// refs counts the number of live subscribers sharing this entry (e.g. the
// same session open in multiple browser tabs). The stream is only torn down
// — done closed and the entry deleted — when the last subscriber leaves, so
// closing one tab does not kill the others' live updates.
type streamEntry struct {
	ch   chan IslandUpdate
	done chan struct{} // closed when the last subscriber unsubscribes
	refs int           // number of live subscribers; guarded by Manager.mu
}

// Manager tracks active islands across all client sessions.
type Manager struct {
	mu      sync.RWMutex
	islands map[string]*Island         // islandID → Island
	clients map[string]map[string]bool // sessionID → set of islandIDs
	streams map[string]*streamEntry    // sessionID → update stream

	// ── Presence ──
	// presenceConns maps a unique connection id to its presence
	// registration (identity + topics). The LOCAL roster is derived from
	// this live set at read time (dedup by UserID), so there is no manual
	// ref-count to drift. nextPresenceID is atomically incremented and
	// is safe to use without holding mu. OnPresenceChange, when set, is
	// fired outside the lock after a local OR remote roster change
	// mutates a topic's merged roster. See presence.go (local) and
	// presence_fanout.go (cross-replica).
	presenceConns    map[uint64]*presenceConn
	nextPresenceID   uint64
	OnPresenceChange func(topic string)

	// dropped counts island updates discarded because a client's SSE buffer
	// was full (slow/stalled consumer). Exposed via DroppedUpdates so the
	// otherwise-silent loss is observable (wire it to a metric/health check).
	dropped atomic.Int64

	// fanout, when attached via SetFanout, mirrors Push/PushUpdate updates
	// to other replicas and re-delivers theirs locally. nodeID is the
	// originator stamp used to drop own-node echoes. fanoutSend is the
	// non-blocking enqueue into the publish queue (fanout.PublishQueue) —
	// Push/PushUpdate run on HTTP request goroutines and must never wait on
	// the backend's network/DB round-trip. All guarded by mu.
	fanout     fanout.Fanout
	nodeID     string
	fanoutSend func([]byte)

	// ── Presence fanout (cross-replica; presence_fanout.go) ──
	// The SAME transport as `fanout` above, used for a DEDICATED presence
	// lane on topic presenceFanoutTopic ("gofastr.presence") — parallel to
	// the island-invalidation lane, never sharing its payload shape.
	// presenceSend is the non-blocking enqueue into the presence publish
	// queue. remoteRosters maps topic → replicaID → entry (with TTL); nil
	// when no fanout is attached (PresenceRoster then returns local-only).
	// presenceHeartbeat/presenceTTL are the convergence intervals (defaults
	// set in SetFanout; test-tunable via reconfigurePresence). presenceDone
	// closes to stop the heartbeat goroutine; presenceWG tracks it so stop
	// waits and no goroutine leaks. All guarded by mu except presenceWG.
	presenceSend      func([]byte)
	remoteRosters     map[string]map[string]remoteRosterEntry
	presenceHeartbeat time.Duration
	presenceTTL       time.Duration
	presenceDone      chan struct{}
	presenceWG        sync.WaitGroup
}

// DroppedUpdates returns the cumulative number of island updates dropped
// because a client's SSE channel was full. A steadily climbing value means
// consumers can't keep up (undersized buffer, stalled browsers).
func (m *Manager) DroppedUpdates() int64 { return m.dropped.Load() }

// NewManager creates a new island manager.
func NewManager() *Manager {
	return &Manager{
		islands: make(map[string]*Island),
		clients: make(map[string]map[string]bool),
		streams: make(map[string]*streamEntry),
	}
}

// Register adds an island to the manager, associated with a session.
// Returns an error if an island with the same ID already exists.
func (m *Manager) Register(island *Island) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.islands[island.ID]; exists {
		return errors.New("island already registered: " + island.ID)
	}

	m.islands[island.ID] = island

	if m.clients[island.SessionID] == nil {
		m.clients[island.SessionID] = make(map[string]bool)
	}
	m.clients[island.SessionID][island.ID] = true

	return nil
}

// Unregister removes an island from the manager.
func (m *Manager) Unregister(islandID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	isl, ok := m.islands[islandID]
	if !ok {
		return
	}

	delete(m.islands, islandID)

	if set, ok := m.clients[isl.SessionID]; ok {
		delete(set, islandID)
		if len(set) == 0 {
			delete(m.clients, isl.SessionID)
		}
	}
}

// Push re-renders an island and sends the update to the client's SSE stream.
func (m *Manager) Push(islandID string) error {
	m.mu.Lock()
	isl, ok := m.islands[islandID]
	if !ok {
		m.mu.Unlock()
		return errors.New("island not found: " + islandID)
	}
	html := isl.Update()
	sessionID := isl.SessionID
	m.mu.Unlock()

	update := IslandUpdate{
		IslandID: islandID,
		HTML:     string(html),
	}
	m.deliver(update, sessionID)
	m.publishFanout(sessionID, update)
	return nil
}

// Subscribe returns a channel that receives island updates for a session.
// If a subscription already exists, it returns the existing channel.
func (m *Manager) Subscribe(sessionID string) <-chan IslandUpdate {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.streams[sessionID]; ok {
		entry.refs++
		return entry.ch
	}

	entry := &streamEntry{
		ch:   make(chan IslandUpdate, 64),
		done: make(chan struct{}),
		refs: 1,
	}
	m.streams[sessionID] = entry
	return entry.ch
}

// Unsubscribe releases one subscriber's hold on a session's update stream.
// The stream is only torn down — done closed and the entry deleted — when the
// last subscriber leaves. This keeps the stream alive for other tabs sharing
// the same session. The data channel is never closed, preventing panics from
// concurrent sends.
func (m *Manager) Unsubscribe(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.streams[sessionID]
	if !ok {
		return
	}

	entry.refs--
	if entry.refs > 0 {
		return // other subscribers still hold this stream
	}

	delete(m.streams, sessionID)
	close(entry.done) // signal done; data channel is never closed
}

// PushUpdate sends a direct update to a session's SSE stream.
func (m *Manager) PushUpdate(update IslandUpdate, sessionID string) {
	m.deliver(update, sessionID)
	m.publishFanout(sessionID, update)
}

// Get retrieves an island by ID.
func (m *Manager) Get(islandID string) (*Island, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	isl, ok := m.islands[islandID]
	return isl, ok
}

// ListBySession returns all island IDs for a session.
func (m *Manager) ListBySession(sessionID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	set, ok := m.clients[sessionID]
	if !ok {
		return nil
	}

	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	return ids
}

// islandFanoutTopic is the single fanout channel the island managers across
// replicas publish on and subscribe to.
const islandFanoutTopic = "gofastr.islands"

// islandFanoutMsg is the wire shape for a fanned-out update. sessionID must
// travel inside the payload because the receiving replica delivers by
// looking up its own streams[sessionID] — it does not know the session
// otherwise.
type islandFanoutMsg struct {
	SessionID string `json:"s"`
	IslandID  string `json:"i"`
	HTML      string `json:"h"`
}

// deliver sends update to the local stream for sessionID if present, using
// the same non-blocking send + dropped-counter semantics as before the
// fanout seam. Shared by Push, PushUpdate, and the fanout receive path so a
// remote-delivered update is indistinguishable from a local one at the
// channel level.
func (m *Manager) deliver(update IslandUpdate, sessionID string) {
	m.mu.RLock()
	entry, ok := m.streams[sessionID]
	m.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case entry.ch <- update:
	case <-entry.done:
	default:
		// Drop update if channel is full — client may be slow.
		m.dropped.Add(1)
	}
}

// publishFanout mirrors update to other replicas via the attached fanout, if
// any. Best-effort: a dropped real-time message is acceptable (the durable
// lane is the outbox's job, not the island lane). No-op when no fanout is
// attached. The enqueue never blocks — callers are HTTP request goroutines
// and a stalled backend must not stall them (see fanout.PublishQueue).
func (m *Manager) publishFanout(sessionID string, update IslandUpdate) {
	m.mu.RLock()
	send := m.fanoutSend
	nodeID := m.nodeID
	m.mu.RUnlock()
	if send == nil {
		return
	}
	msg := islandFanoutMsg{SessionID: sessionID, IslandID: update.IslandID, HTML: update.HTML}
	body, err := json.Marshal(msg)
	if err != nil {
		return
	}
	send(fanout.Wrap(nodeID, body))
}

// SetFanout attaches a fanout so Push/PushUpdate updates cross replicas and
// updates originating on other replicas are re-delivered to the local
// session stream (topic "gofastr.islands"). It ALSO wires the cross-replica
// PRESENCE lane (topic "gofastr.presence") over the same transport so a
// topic's merged roster reflects every replica's connections, not just this
// one — see presence_fanout.go. Own-node messages are dropped on both lanes
// so updates/announcements are never echoed back, and received messages are
// NEVER re-publishing (no loop). Delivery is lossy best-effort; presence
// reconverges via periodic full-roster heartbeats.
//
// Island delivery-where-connected is fixed as before; Island OBJECTS and
// signal state remain per-replica, so sticky sessions remain the
// recommendation for stateful widget apps. Presence state, by contrast, is
// fully aggregated across replicas.
//
// The returned stop detaches BOTH lanes (cancels subscriptions, stops the
// publish queues, stops the presence heartbeat goroutine, publishes a
// graceful-leave so peers drop this replica promptly, and clears the
// bridge); safe to call multiple times. Returns an error if a fanout is
// already attached or f is nil.
func (m *Manager) SetFanout(f fanout.Fanout) (stop func(), err error) {
	if f == nil {
		return nil, errors.New("island: SetFanout: nil fanout")
	}
	nodeID := fanout.NewNodeID()
	islandSend, islandStopQueue := fanout.PublishQueue(f, islandFanoutTopic, 0)
	presenceSend, presenceStopQueue := fanout.PublishQueue(f, presenceFanoutTopic, 0)

	m.mu.Lock()
	if m.fanout != nil {
		m.mu.Unlock()
		islandStopQueue()
		presenceStopQueue()
		return nil, errors.New("island: fanout already attached")
	}
	m.fanout = f
	m.nodeID = nodeID
	m.fanoutSend = islandSend
	// Presence lane state.
	m.presenceSend = presenceSend
	m.remoteRosters = make(map[string]map[string]remoteRosterEntry)
	m.presenceHeartbeat = defaultPresenceHeartbeat
	m.presenceTTL = defaultPresenceTTL
	m.presenceDone = make(chan struct{})
	m.mu.Unlock()

	// Island lane: re-deliver remote updates to local session streams.
	islandCancel, subErr := f.Subscribe(islandFanoutTopic, func(raw []byte) {
		origin, body, uerr := fanout.Unwrap(raw)
		if uerr != nil {
			return
		}
		if origin == nodeID {
			return // own-node: drop to avoid echo
		}
		var msg islandFanoutMsg
		if jerr := json.Unmarshal(body, &msg); jerr != nil {
			return
		}
		// Deliver locally only; NEVER re-publish on receive.
		m.deliver(IslandUpdate{IslandID: msg.IslandID, HTML: msg.HTML}, msg.SessionID)
	})
	if subErr != nil {
		islandStopQueue()
		presenceStopQueue()
		m.mu.Lock()
		m.fanout = nil
		m.nodeID = ""
		m.fanoutSend = nil
		m.clearPresenceFanoutLocked()
		m.mu.Unlock()
		return nil, fmt.Errorf("island: SetFanout: subscribe: %w", subErr)
	}

	// Presence lane: merge remote announcements into the local roster table.
	presenceCancel, presenceSubErr := f.Subscribe(presenceFanoutTopic, func(raw []byte) {
		origin, body, uerr := fanout.Unwrap(raw)
		if uerr != nil {
			return
		}
		if origin == nodeID {
			return // own-node: drop; this replica's rosters are already local
		}
		var msg presenceFanoutMsg
		if jerr := json.Unmarshal(body, &msg); jerr != nil {
			return
		}
		m.mergeRemotePresence(origin, msg)
	})
	if presenceSubErr != nil {
		islandCancel()
		islandStopQueue()
		presenceStopQueue()
		m.mu.Lock()
		m.fanout = nil
		m.nodeID = ""
		m.fanoutSend = nil
		m.clearPresenceFanoutLocked()
		m.mu.Unlock()
		return nil, fmt.Errorf("island: SetFanout: presence subscribe: %w", presenceSubErr)
	}

	// Start the presence heartbeat goroutine (bound to the presenceDone
	// channel we just created) and immediately announce this replica's
	// current local rosters so peers converge without waiting for the first
	// beat (covers joins that happened before SetFanout attached).
	m.mu.RLock()
	presenceDone := m.presenceDone
	m.mu.RUnlock()
	m.presenceWG.Add(1)
	go m.presenceHeartbeatLoop(presenceDone)
	m.broadcastAllLocalTopics()

	var once sync.Once
	return func() {
		once.Do(func() {
			// Graceful leave: tell peers to drop this replica now (TTL is
			// only the crash fallback). No-op if the heartbeat was halted.
			m.gracefulLeaveLocalTopics(f)
			// Stop the heartbeat goroutine.
			m.mu.Lock()
			done := m.presenceDone
			m.mu.Unlock()
			if done != nil {
				close(done)
			}
			m.presenceWG.Wait()
			// Detach both lanes' subscriptions + publish queues.
			islandCancel()
			presenceCancel()
			islandStopQueue()
			presenceStopQueue()
			// Clear all fanout + presence state.
			m.mu.Lock()
			m.fanout = nil
			m.nodeID = ""
			m.fanoutSend = nil
			m.clearPresenceFanoutLocked()
			m.mu.Unlock()
		})
	}, nil
}

// clearPresenceFanoutLocked resets the cross-replica presence fields. Caller
// holds mu. Used by SetFanout's rollback and stop paths so neither leaks
// state nor a goroutine into a "fresh" single-replica manager.
func (m *Manager) clearPresenceFanoutLocked() {
	m.presenceSend = nil
	m.remoteRosters = nil
	m.presenceHeartbeat = 0
	m.presenceTTL = 0
	m.presenceDone = nil
}
