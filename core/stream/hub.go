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
//	// Broadcast (text must be valid UTF-8; BroadcastBinary sends bytes):
//	if err := hub.Broadcast([]byte("hello everyone")); err != nil { ... }
//
//	// On disconnect:
//	hub.Unregister(conn)
type Hub struct {
	mu sync.RWMutex
	// connections maps each registered connection to the stop channel
	// of its close watcher; Unregister closes it so the watcher exits.
	connections map[*WebSocketConn]chan struct{}
	broadcast   chan wsMessage
	stop        chan struct{}
	stopped     atomic.Bool
}

// NewHub creates a new Hub.
func NewHub() *Hub {
	return &Hub{
		connections: make(map[*WebSocketConn]chan struct{}),
		broadcast:   make(chan wsMessage, 64),
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
			// We don't wait: Stop returns immediately. Closers run
			// concurrently and finish at their own pace.
			return

		case msg := <-h.broadcast:
			h.mu.RLock()
			for conn := range h.connections {
				// Skip conns that have already closed: their sendBuffer
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
				// Non-blocking send: drop if buffer full (backpressure)
				// or the conn closed between the check and the send.
				select {
				case <-conn.Closed():
					// Conn closed mid-broadcast; drop without blocking.
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
	if _, ok := h.connections[conn]; ok {
		// Already registered: its watcher is still running.
		h.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	h.connections[conn] = stop
	h.mu.Unlock()

	// Auto-unregister when the connection closes. The watcher ends with
	// the registration: a connection moved between hubs (Unregister
	// here, Register there) leaves no goroutine behind per move.
	go func() {
		select {
		case <-conn.Closed():
			h.Unregister(conn)
		case <-stop:
		case <-h.stop:
		}
	}()
}

// Unregister removes a connection from the hub and stops its close
// watcher. Non-blocking.
func (h *Hub) Unregister(conn *WebSocketConn) {
	h.mu.Lock()
	if stop, ok := h.connections[conn]; ok {
		close(stop)
		delete(h.connections, conn)
	}
	h.mu.Unlock()
}

// Broadcast sends a text message to all registered connections.
// Non-blocking: if the hub's broadcast channel is full, the message is
// dropped, and a connection whose send buffer is full drops it too.
//
// A text message must be valid UTF-8 (RFC 6455 §5.6). Broadcast checks
// msg first and returns ErrInvalidUTF8, sending it to no one, when it
// is not; send arbitrary bytes with BroadcastBinary. Any other return
// is nil: a dropped message is not an error.
func (h *Hub) Broadcast(msg []byte) error {
	m, err := textMessage(msg)
	if err != nil {
		return err
	}
	h.send(m)
	return nil
}

// BroadcastBinary is Broadcast for a binary message (opcode 0x2): the
// bytes reach every connection exactly as given, with no UTF-8 check.
// It shares each connection's send queue, so text and binary keep the
// order they were broadcast in.
func (h *Hub) BroadcastBinary(msg []byte) {
	h.send(wsMessage{opcode: wsopcodeBinary, data: msg})
}

// BroadcastWait sends a text message to all connections, blocking if
// the broadcast channel is full. Like Broadcast, it returns
// ErrInvalidUTF8 and sends nothing for msg that is not valid UTF-8.
func (h *Hub) BroadcastWait(msg []byte) error {
	m, err := textMessage(msg)
	if err != nil {
		return err
	}
	h.sendWait(m)
	return nil
}

// BroadcastBinaryWait is BroadcastWait for a binary message.
func (h *Hub) BroadcastBinaryWait(msg []byte) {
	h.sendWait(wsMessage{opcode: wsopcodeBinary, data: msg})
}

// send queues one message for Run without blocking.
func (h *Hub) send(m wsMessage) {
	if h.stopped.Load() {
		return
	}
	select {
	case h.broadcast <- m:
	case <-h.stop:
		// hub stopped while we were waiting
	default:
		// Hub broadcast channel full, drop
	}
}

// sendWait queues one message for Run, blocking while the channel is
// full.
func (h *Hub) sendWait(m wsMessage) {
	if h.stopped.Load() {
		return
	}
	select {
	case h.broadcast <- m:
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
