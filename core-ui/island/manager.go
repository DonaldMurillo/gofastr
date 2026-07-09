package island

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

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
// session stream. Topic is "gofastr.islands". Own-node messages are dropped
// on receive so updates are never echoed back. Received updates are NEVER
// re-published (no loop). Delivery is lossy best-effort.
//
// This fixes delivery-where-connected: a session whose SSE connection lives
// on another replica still receives updates. Island OBJECTS and signal state
// remain per-replica — an RPC landing on a replica without the island object
// can't re-render. Sticky sessions remain the recommendation for stateful
// widget apps.
//
// The returned stop detaches the fanout (cancels the subscription and clears
// the bridge); safe to call multiple times. Returns an error if a fanout is
// already attached or f is nil.
func (m *Manager) SetFanout(f fanout.Fanout) (stop func(), err error) {
	if f == nil {
		return nil, errors.New("island: SetFanout: nil fanout")
	}
	nodeID := fanout.NewNodeID()
	send, stopQueue := fanout.PublishQueue(f, islandFanoutTopic, 0)
	m.mu.Lock()
	if m.fanout != nil {
		m.mu.Unlock()
		stopQueue()
		return nil, errors.New("island: fanout already attached")
	}
	m.fanout = f
	m.nodeID = nodeID
	m.fanoutSend = send
	m.mu.Unlock()

	cancel, subErr := f.Subscribe(islandFanoutTopic, func(raw []byte) {
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
		stopQueue()
		m.mu.Lock()
		m.fanout = nil
		m.nodeID = ""
		m.fanoutSend = nil
		m.mu.Unlock()
		return nil, fmt.Errorf("island: SetFanout: subscribe: %w", subErr)
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			stopQueue()
			m.mu.Lock()
			m.fanout = nil
			m.nodeID = ""
			m.fanoutSend = nil
			m.mu.Unlock()
		})
	}, nil
}
