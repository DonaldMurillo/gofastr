package island

import (
	"errors"
	"sync"
)

// IslandUpdate represents an update to push to a client.
type IslandUpdate struct {
	IslandID string
	HTML     string
}

// Manager tracks active islands across all client sessions.
type Manager struct {
	mu      sync.RWMutex
	islands map[string]*Island           // islandID → Island
	clients map[string]map[string]bool   // sessionID → set of islandIDs
	streams map[string]chan IslandUpdate // sessionID → update channel
}

// NewManager creates a new island manager.
func NewManager() *Manager {
	return &Manager{
		islands: make(map[string]*Island),
		clients: make(map[string]map[string]bool),
		streams: make(map[string]chan IslandUpdate),
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
	m.mu.RLock()
	isl, ok := m.islands[islandID]
	if !ok {
		m.mu.RUnlock()
		return errors.New("island not found: " + islandID)
	}
	html := isl.Update()
	sessionID := isl.SessionID

	ch, hasStream := m.streams[sessionID]
	m.mu.RUnlock()

	if hasStream {
		update := IslandUpdate{
			IslandID: islandID,
			HTML:     string(html),
		}
		select {
		case ch <- update:
		default:
			// Drop update if channel is full — client may be slow.
		}
	}

	return nil
}

// Subscribe returns a channel that receives island updates for a session.
// If a subscription already exists, it returns the existing channel.
func (m *Manager) Subscribe(sessionID string) <-chan IslandUpdate {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ch, ok := m.streams[sessionID]; ok {
		return ch
	}

	ch := make(chan IslandUpdate, 64)
	m.streams[sessionID] = ch
	return ch
}

// Unsubscribe removes the update channel for a session and closes it.
func (m *Manager) Unsubscribe(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch, ok := m.streams[sessionID]
	if !ok {
		return
	}

	delete(m.streams, sessionID)
	close(ch)
}

// PushUpdate sends a direct update to a session's SSE stream.
func (m *Manager) PushUpdate(update IslandUpdate, sessionID string) {
	m.mu.RLock()
	ch, ok := m.streams[sessionID]
	m.mu.RUnlock()

	if ok {
		select {
		case ch <- update:
		default:
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
