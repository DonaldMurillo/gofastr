package island

import (
	"errors"
	"sync"
	"sync/atomic"
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
	entry, hasStream := m.streams[sessionID]
	m.mu.Unlock()

	if hasStream {
		update := IslandUpdate{
			IslandID: islandID,
			HTML:     string(html),
		}
		select {
		case entry.ch <- update:
		case <-entry.done:
		default:
			// Drop update if channel is full — client may be slow.
			m.dropped.Add(1)
		}
	}

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
	m.mu.RLock()
	entry, ok := m.streams[sessionID]
	m.mu.RUnlock()

	if ok {
		select {
		case entry.ch <- update:
		case <-entry.done:
		default:
			m.dropped.Add(1)
		}
	}
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
