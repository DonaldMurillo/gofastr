package stream

import (
	"sync"
	"sync/atomic"
)

// Hub holds a set of WebSocket connections and broadcasts to them.
type Hub struct {
	mu          sync.RWMutex
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

// Run is the hub's broadcast loop; run it in a goroutine until Stop.
func (h *Hub) Run() {
	for {
		select {
		case <-h.stop:
			h.mu.Lock()
			conns := make([]*WebSocketConn, 0, len(h.connections))
			for conn := range h.connections {
				conns = append(conns, conn)
			}
			h.connections = nil
			h.mu.Unlock()

			var wg sync.WaitGroup
			for _, c := range conns {
				wg.Add(1)
				go func(cc *WebSocketConn) {
					defer wg.Done()
					cc.Close()
				}(c)
			}
			return

		case msg := <-h.broadcast:
			h.mu.RLock()
			for conn := range h.connections {
				select {
				case <-conn.Closed():
					continue
				default:
				}
				select {
				case <-conn.Closed():
				case conn.sendBuffer <- msg:
				default:
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Register adds a connection until it closes; a no-op after Stop.
func (h *Hub) Register(conn *WebSocketConn) {
	if h.stopped.Load() {
		return
	}
	h.mu.Lock()
	if h.connections == nil {
		h.mu.Unlock()
		return
	}
	if _, ok := h.connections[conn]; ok {
		h.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	h.connections[conn] = stop
	h.mu.Unlock()

	go func() {
		select {
		case <-conn.Closed():
			h.Unregister(conn)
		case <-stop:
		case <-h.stop:
		}
	}()
}

// Unregister removes a connection without closing it.
func (h *Hub) Unregister(conn *WebSocketConn) {
	h.mu.Lock()
	if stop, ok := h.connections[conn]; ok {
		close(stop)
		delete(h.connections, conn)
	}
	h.mu.Unlock()
}

// Broadcast queues a text message for every connection without blocking;
// a full queue drops it. It returns ErrInvalidUTF8, sending nothing, for
// msg that is not valid UTF-8.
func (h *Hub) Broadcast(msg []byte) error {
	m, err := textMessage(msg)
	if err != nil {
		return err
	}
	h.send(m)
	return nil
}

// BroadcastBinary is Broadcast for a binary message, with no UTF-8 check.
func (h *Hub) BroadcastBinary(msg []byte) {
	h.send(wsMessage{opcode: wsopcodeBinary, data: msg})
}

// BroadcastWait is Broadcast that blocks while the hub's queue is full.
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

func (h *Hub) send(m wsMessage) {
	if h.stopped.Load() {
		return
	}
	select {
	case h.broadcast <- m:
	case <-h.stop:
	default:
	}
}

func (h *Hub) sendWait(m wsMessage) {
	if h.stopped.Load() {
		return
	}
	select {
	case h.broadcast <- m:
	case <-h.stop:
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
