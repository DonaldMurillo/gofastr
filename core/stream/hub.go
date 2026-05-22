package stream

import (
	"sync"
	"sync/atomic"
)

// Hub manages a set of WebSocket connections and broadcasts messages
// to all of them. Multiple hubs can coexist (one per room, channel, etc.).
//
// Usage:
//
//	hub := stream.NewHub()
//	go hub.Run()
//
//	// On connect:
//	hub.Register(conn)
//
//	// Broadcast:
//	hub.Broadcast([]byte("hello everyone"))
//
//	// On disconnect:
//	hub.Unregister(conn)
type Hub struct {
	mu          sync.RWMutex
	connections map[*WebSocketConn]struct{}
	broadcast   chan []byte
	register    chan *WebSocketConn
	unregister  chan *WebSocketConn
	stop        chan struct{}
	stopped     atomic.Bool
}

// NewHub creates a new Hub.
func NewHub() *Hub {
	return &Hub{
		connections: make(map[*WebSocketConn]struct{}),
		broadcast:   make(chan []byte, 64),
		register:    make(chan *WebSocketConn),
		unregister:  make(chan *WebSocketConn),
		stop:        make(chan struct{}),
	}
}

// Run starts the hub's event loop. Block until Stop is called.
// Must be called in a goroutine:
//
//	go hub.Run()
func (h *Hub) Run() {
	for {
		select {
		case <-h.stop:
			h.mu.Lock()
			for conn := range h.connections {
				conn.Close()
			}
			h.connections = nil
			h.mu.Unlock()
			return

		case conn := <-h.register:
			h.mu.Lock()
			h.connections[conn] = struct{}{}
			h.mu.Unlock()
			// Auto-unregister when the connection closes
			go func() {
				<-conn.Closed()
				h.Unregister(conn)
			}()

		case conn := <-h.unregister:
			h.mu.Lock()
			delete(h.connections, conn)
			h.mu.Unlock()

		case msg := <-h.broadcast:
			h.mu.RLock()
			for conn := range h.connections {
				// Non-blocking send — drop if buffer full (backpressure)
				select {
				case conn.sendBuffer <- msg:
				default:
					// Buffer full, drop the message for this connection
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Register adds a connection to the hub.
func (h *Hub) Register(conn *WebSocketConn) {
	if h.stopped.Load() {
		return
	}
	h.register <- conn
}

// Unregister removes a connection from the hub.
func (h *Hub) Unregister(conn *WebSocketConn) {
	if h.stopped.Load() {
		return
	}
	select {
	case h.unregister <- conn:
	default:
		// Hub is busy; connection will be cleaned up on stop
	}
}

// Broadcast sends a message to all registered connections.
// Non-blocking — if the hub's broadcast channel is full, the message is dropped.
func (h *Hub) Broadcast(msg []byte) {
	if h.stopped.Load() {
		return
	}
	select {
	case h.broadcast <- msg:
	default:
		// Hub broadcast channel full, drop
	}
}

// BroadcastWait sends a message to all connections, blocking if the
// broadcast channel is full.
func (h *Hub) BroadcastWait(msg []byte) {
	if h.stopped.Load() {
		return
	}
	h.broadcast <- msg
}

// Stop stops the hub and closes all registered connections.
func (h *Hub) Stop() {
	if h.stopped.CompareAndSwap(false, true) {
		close(h.stop)
	}
}

// Count returns the number of active connections.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections)
}
