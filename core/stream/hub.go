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
	stop        chan struct{}
	stopped     atomic.Bool
}

// NewHub creates a new Hub.
func NewHub() *Hub {
	return &Hub{
		connections: make(map[*WebSocketConn]struct{}),
		broadcast:   make(chan []byte, 64),
		stop:        make(chan struct{}),
	}
}

// Run starts the hub's event loop. Block until Stop is called.
// Must be called in a goroutine:
//
//	go hub.Run()
//
// Register/Unregister are now mutex-only ops on the connections map,
// so Run is only responsible for draining the broadcast channel and
// shutting down on Stop.
func (h *Hub) Run() {
	for {
		select {
		case <-h.stop:
			// Snapshot conns under lock, then close them OUTSIDE the
			// lock so a slow Close() can't pin Stop on the lock.
			h.mu.Lock()
			conns := make([]*WebSocketConn, 0, len(h.connections))
			for conn := range h.connections {
				conns = append(conns, conn)
			}
			h.connections = nil
			h.mu.Unlock()

			// Close in parallel; a single slow conn doesn't pin Stop.
			var wg sync.WaitGroup
			for _, c := range conns {
				wg.Add(1)
				go func(cc *WebSocketConn) {
					defer wg.Done()
					cc.Close()
				}(c)
			}
			// We don't wait — Stop returns immediately. Closers run
			// concurrently and finish at their own pace.
			return

		case msg := <-h.broadcast:
			h.mu.RLock()
			for conn := range h.connections {
				// Skip conns that have already closed — their sendBuffer
				// drains nothing and a dead conn would otherwise silently
				// absorb broadcasts until GC. The closed-channel check
				// races benignly with concurrent Close(): worst case we
				// queue one extra message on a closed conn, which the
				// writePump (already exited) ignores.
				select {
				case <-conn.Closed():
					continue
				default:
				}
				// Non-blocking send — drop if buffer full (backpressure)
				// or the conn closed between the check and the send.
				select {
				case <-conn.Closed():
					// Conn closed mid-broadcast — drop without blocking.
				case conn.sendBuffer <- msg:
				default:
					// Buffer full, drop the message for this connection
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Register adds a connection to the hub. Non-blocking. Returns immediately
// if the hub has been stopped.
func (h *Hub) Register(conn *WebSocketConn) {
	if h.stopped.Load() {
		return
	}
	h.mu.Lock()
	if h.connections == nil {
		// Stop closed the map already; bail.
		h.mu.Unlock()
		return
	}
	h.connections[conn] = struct{}{}
	h.mu.Unlock()

	// Auto-unregister when the connection closes.
	go func() {
		<-conn.Closed()
		h.Unregister(conn)
	}()
}

// Unregister removes a connection from the hub. Non-blocking.
func (h *Hub) Unregister(conn *WebSocketConn) {
	h.mu.Lock()
	if h.connections != nil {
		delete(h.connections, conn)
	}
	h.mu.Unlock()
}

// Broadcast sends a message to all registered connections.
// Non-blocking — if the hub's broadcast channel is full, the message is dropped.
func (h *Hub) Broadcast(msg []byte) {
	if h.stopped.Load() {
		return
	}
	select {
	case h.broadcast <- msg:
	case <-h.stop:
		// hub stopped while we were waiting
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
	select {
	case h.broadcast <- msg:
	case <-h.stop:
		// hub stopped while we were blocked
	}
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
