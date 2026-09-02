package stream

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

// SequencedEnvelope is the wire shape a StateChannel puts on every
// message it delivers: the hydration snapshot and every live event.
// The client-side companion (core-ui/runtime/src/ws.js,
// createSequencedReducer) applies an envelope only when its sequence is
// strictly greater than the last applied one, so a delayed snapshot can
// never resurrect state a newer event already replaced.
//
// Sequence is a uint64 on the wire and a Number in the browser, which
// compares exactly up to 2^53 (nine quadrillion events on one channel).
// That bound is stated rather than engineered around: a string-or-BigInt
// protocol would cost every consumer a conversion for a limit no
// deployment reaches.
type SequencedEnvelope[T any] struct {
	Sequence uint64    `json:"sequence"`
	Type     string    `json:"type"`
	Payload  T         `json:"payload"`
	SentAt   time.Time `json:"sentAt"`
}

// SnapshotEnvelopeType is the Type a StateChannel snapshot envelope
// carries. Every other Type the channel emits comes from the Publish
// call, so an application can reserve names besides this one.
const SnapshotEnvelopeType = "snapshot"

// SnapshotSource is the application-owned half of a StateChannel. The
// channel never stores business state; it asks the source for a role's
// view on connect and asks it to shape each event per role before
// anything is serialized.
//
// Sequences share ONE space per channel, across roles. SnapshotFor must
// return the snapshot payload and its sequence from a single immutable
// read (copy-on-read), where the sequence is the channel-wide version of
// the state returned. FilterEvent then sees only events; the channel
// reconciles its own event counter to every snapshot sequence it sends,
// so events published after a snapshot always sort above it.
//
// Minimization happens here, at the source: FilterEvent runs BEFORE
// serialization, so a field the source strips never crosses the
// transport. Hiding a field in the UI is not data minimization.
type SnapshotSource[Role comparable, Snapshot any, Event any] interface {
	// SnapshotFor returns the role's view of the current state and the
	// sequence of that state, from one immutable read. Called from the
	// channel's Run loop: a slow read delays event delivery for every
	// connection on the channel, so keep it an in-memory copy.
	SnapshotFor(Role) (Snapshot, uint64)

	// FilterEvent returns the payload a role may receive for an event,
	// and whether the role may see it at all. Called once per role that
	// currently has connections, before the envelope is marshaled.
	FilterEvent(Role, Event) (any, bool)
}

// StateChannel layers reconnect hydration and ordered live events over
// WebSocketConn. It is a small helper above Hub, not a state framework:
// persistence and business state stay application-owned, and the channel
// owns the envelope shape, sequencing, initial hydration, and per-role
// filtering.
//
// Usage:
//
//	channel := stream.NewStateChannel(source)
//	go channel.Run()
//
//	// On connect (handler goroutine); returns once the snapshot is
//	// queued on the connection:
//	channel.Connect(role, conn)
//
//	// After the application has applied a mutation to its own state:
//	channel.Publish("cleared", event)
//
// Ordering contract, per connection, guaranteed on the wire:
//
//   - The hydration snapshot for a connection is sent before any event
//     published after that connection's snapshot was queued, and every
//     such event carries a sequence greater than the snapshot's.
//   - Events reach each connection in strictly increasing sequence
//     order, so the client reducer's reject-stale rule never discards a
//     live event as a side effect.
//   - An event whose mutation a snapshot already contains may still be
//     delivered after it (at-least-once); sequences make the client
//     state convergent, not exactly-once.
//
// Delivery is best-effort like Hub: a connection whose send buffer is
// full has events dropped for it, and a connection that cannot accept
// its hydration snapshot at all is closed (it cannot catch up on its
// own, so it reconnects and hydrates again). Publish never blocks; when
// the channel's job queue is full the event is dropped, mirroring
// Hub.Broadcast.
type StateChannel[Role comparable, Snapshot any, Event any] struct {
	source SnapshotSource[Role, Snapshot, Event]

	mu    sync.RWMutex
	conns map[*WebSocketConn]Role

	jobs    chan stateJob[Role, Snapshot, Event]
	stop    chan struct{}
	stopped atomic.Bool

	// nextSeq is the next sequence an event will carry. Owned by the
	// Run loop alone (assignment happens between job dequeues), so it
	// needs no lock; it is reconciled to every snapshot sequence sent,
	// which is what keeps post-snapshot events above it.
	nextSeq uint64
}

// connRole pairs a connection with the role it connected as, snapped
// out of the conns map under RLock so filtering and marshaling run
// without holding the channel's lock.
type connRole[Role comparable] struct {
	conn *WebSocketConn
	role Role
}

type stateJobKind uint8

const (
	stateJobEvent stateJobKind = iota
	stateJobSnapshot
)

// stateJob is one unit of work for the Run loop. Snapshot and event
// jobs share ONE FIFO queue: the queue position of a snapshot relative
// to an event is exactly the ordering contract above, so the loop's
// single-threaded processing is what makes the contract hold.
type stateJob[Role comparable, Snapshot any, Event any] struct {
	kind  stateJobKind
	typ   string // event jobs: the envelope Type
	event Event  // event jobs: the payload before role filtering
	conn  *WebSocketConn
	role  Role
	done  chan struct{} // snapshot jobs: closed once queued or refused
}

// NewStateChannel creates a StateChannel over the given source. Call
// Run in a goroutine before connecting connections.
func NewStateChannel[Role comparable, Snapshot any, Event any](
	source SnapshotSource[Role, Snapshot, Event],
) *StateChannel[Role, Snapshot, Event] {
	return &StateChannel[Role, Snapshot, Event]{
		source: source,
		conns:  make(map[*WebSocketConn]Role),
		jobs:   make(chan stateJob[Role, Snapshot, Event], 64),
		stop:   make(chan struct{}),
	}
}

// Connect hydrates conn with the role's snapshot and keeps it live for
// published events. It blocks until the snapshot is queued on the
// connection (or the connection/channel is closed), so on return the
// handler knows hydration happened.
//
// If the channel is stopped, or its job queue is full, the connection
// is closed: a connection that never hydrates cannot catch up.
func (c *StateChannel[Role, Snapshot, Event]) Connect(role Role, conn *WebSocketConn) {
	if c.stopped.Load() {
		go conn.Close()
		return
	}
	job := stateJob[Role, Snapshot, Event]{
		kind: stateJobSnapshot,
		conn: conn,
		role: role,
		done: make(chan struct{}),
	}
	select {
	case c.jobs <- job:
	case <-c.stop:
		go conn.Close()
		return
	default:
		// Queue full: the loop is wedged behind a slow SnapshotFor.
		// Refuse the connection rather than leave it unhydrated.
		go conn.Close()
		return
	}
	select {
	case <-job.done:
	case <-c.stop:
		// The channel stopped without hydrating this connection (the
		// job may be stranded in the queue with no loop left to drain
		// it). It can never catch up; close it.
		go conn.Close()
	case <-conn.Closed():
	}
}

// Publish broadcasts an event to every connected role. typ becomes the
// envelope's Type; the payload each role receives is FilterEvent's
// return value for that role. Non-blocking: returns immediately; the
// event is dropped if the channel is stopped or its job queue is full.
func (c *StateChannel[Role, Snapshot, Event]) Publish(typ string, event Event) {
	if c.stopped.Load() {
		return
	}
	select {
	case c.jobs <- stateJob[Role, Snapshot, Event]{kind: stateJobEvent, typ: typ, event: event}:
	case <-c.stop:
	default:
		// Queue full; drop, mirroring Hub.Broadcast's lossy semantics.
	}
}

// Unregister removes a connection without closing it. Connections are
// unregistered automatically when they close, so this is only for
// explicit removal.
func (c *StateChannel[Role, Snapshot, Event]) Unregister(conn *WebSocketConn) {
	c.mu.Lock()
	if c.conns != nil {
		delete(c.conns, conn)
	}
	c.mu.Unlock()
}

// Stop stops the channel and closes every registered connection. Safe
// to call more than once. Publish and Connect after Stop are no-ops
// (Connect closes the connection it was given).
func (c *StateChannel[Role, Snapshot, Event]) Stop() {
	if c.stopped.CompareAndSwap(false, true) {
		close(c.stop)
	}
}

// Count returns the number of registered connections.
func (c *StateChannel[Role, Snapshot, Event]) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.conns)
}

// Run starts the channel's event loop. Block until Stop is called:
//
//	go channel.Run()
//
// The loop is the single owner of sequencing: it dequeues snapshot and
// event jobs in FIFO order, assigns event sequences at dequeue time
// (so the wire order per connection is the sequence order), and is the
// only writer of nextSeq.
func (c *StateChannel[Role, Snapshot, Event]) Run() {
	for {
		select {
		case <-c.stop:
			c.mu.Lock()
			conns := make([]*WebSocketConn, 0, len(c.conns))
			for conn := range c.conns {
				conns = append(conns, conn)
			}
			c.conns = nil
			c.mu.Unlock()
			// Close outside the lock and off the loop: Close performs a
			// handshake that can wait out CloseTimeout on a dead peer.
			for _, conn := range conns {
				go conn.Close()
			}
			return
		case job := <-c.jobs:
			if job.kind == stateJobSnapshot {
				c.runSnapshot(job)
			} else {
				c.runEvent(job)
			}
		}
	}
}

// runSnapshot hydrates one new connection: one immutable read of the
// role's snapshot, register, then queue the snapshot envelope on the
// connection (non-blocking, in FIFO with the loop's event sends, so
// nothing can pass it on the wire).
func (c *StateChannel[Role, Snapshot, Event]) runSnapshot(job stateJob[Role, Snapshot, Event]) {
	defer close(job.done)

	// One read: payload and sequence below come from this call only.
	snap, seq := c.source.SnapshotFor(job.role)
	if seq >= c.nextSeq {
		c.nextSeq = seq + 1
	}

	c.mu.Lock()
	if c.conns == nil {
		// Stop won the race; nothing registers past shutdown.
		c.mu.Unlock()
		go job.conn.Close()
		return
	}
	c.conns[job.conn] = job.role
	c.mu.Unlock()
	go func() {
		<-job.conn.Closed()
		c.Unregister(job.conn)
	}()

	data, err := json.Marshal(SequencedEnvelope[Snapshot]{
		Sequence: seq,
		Type:     SnapshotEnvelopeType,
		Payload:  snap,
		SentAt:   time.Now(),
	})
	if err != nil {
		// The snapshot does not serialize; this connection can never
		// hydrate. Close it so the client reconnects and the failure
		// surfaces, rather than leaving a silently blind connection.
		go job.conn.Close()
		return
	}
	select {
	case <-job.conn.Closed():
	default:
		select {
		case job.conn.sendBuffer <- data:
		default:
			// Buffer full: the connection cannot accept hydration, and
			// every later event is newer than the snapshot it missed.
			// Close it so it reconnects instead of running blind.
			go job.conn.Close()
		}
	}
}

// runEvent fans one event out to every connected role: filter once per
// distinct role, marshal once per role, then a non-blocking send per
// connection (drops for that connection only, like Hub.Run).
func (c *StateChannel[Role, Snapshot, Event]) runEvent(job stateJob[Role, Snapshot, Event]) {
	seq := c.nextSeq
	c.nextSeq++

	c.mu.RLock()
	pairs := make([]connRole[Role], 0, len(c.conns))
	for conn, role := range c.conns {
		select {
		case <-conn.Closed():
			continue
		default:
		}
		pairs = append(pairs, connRole[Role]{conn: conn, role: role})
	}
	c.mu.RUnlock()
	if len(pairs) == 0 {
		return
	}

	// Common case: every connection shares one role, filter and marshal
	// once with no per-role grouping.
	uniform := true
	for _, p := range pairs[1:] {
		if p.role != pairs[0].role {
			uniform = false
			break
		}
	}
	if uniform {
		conns := make([]*WebSocketConn, len(pairs))
		for i, p := range pairs {
			conns[i] = p.conn
		}
		c.deliver(conns, pairs[0].role, job, seq)
		return
	}

	byRole := make(map[Role][]*WebSocketConn)
	for _, p := range pairs {
		byRole[p.role] = append(byRole[p.role], p.conn)
	}
	for role, conns := range byRole {
		c.deliver(conns, role, job, seq)
	}
}

// deliver filters, serializes, and sends one event to the connections
// of one role. Filtering happens before json.Marshal by construction:
// the marshaled payload is exactly what FilterEvent returned.
func (c *StateChannel[Role, Snapshot, Event]) deliver(conns []*WebSocketConn, role Role, job stateJob[Role, Snapshot, Event], seq uint64) {
	payload, ok := c.source.FilterEvent(role, job.event)
	if !ok {
		return
	}
	data, err := json.Marshal(SequencedEnvelope[any]{
		Sequence: seq,
		Type:     job.typ,
		Payload:  payload,
		SentAt:   time.Now(),
	})
	if err != nil {
		// An event the source cannot serialize is dropped for this role;
		// the snapshot path is where an unserializable payload is fatal.
		return
	}
	for _, conn := range conns {
		select {
		case <-conn.Closed():
		case conn.sendBuffer <- data:
		default:
			// Buffer full: drop for this connection, as Hub.Run does.
		}
	}
}
