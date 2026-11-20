package stream

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// SequencedEnvelope is the wire shape of every message a StateChannel
// sends, the hydration snapshot and each live event.
type SequencedEnvelope[T any] struct {
	Sequence uint64    `json:"sequence"`
	Type     string    `json:"type"`
	Payload  T         `json:"payload"`
	SentAt   time.Time `json:"sentAt"`
}

// SnapshotEnvelopeType is the Type of a StateChannel snapshot envelope.
const SnapshotEnvelopeType = "snapshot"

// SnapshotSource is the application-owned half of a StateChannel: it gives
// a role's view on connect and shapes each event per role before the
// channel serializes it.
type SnapshotSource[Role comparable, Snapshot any, Event any] interface {
	// SnapshotFor returns the role's view and the channel-wide sequence of
	// that state, from one read. It runs on the channel's Run loop.
	SnapshotFor(Role) (Snapshot, uint64)

	// FilterEvent returns the payload a role may receive for an event and
	// whether the role sees it at all.
	FilterEvent(Role, Event) (any, bool)
}

// StateChannel layers reconnect hydration and sequenced live events over
// WebSocketConn. The application owns its state; the channel owns the
// envelope, sequencing, hydration and per-role filtering.
type StateChannel[Role comparable, Snapshot any, Event any] struct {
	source SnapshotSource[Role, Snapshot, Event]

	mu       sync.RWMutex
	conns    map[*WebSocketConn]Role
	watchers map[*WebSocketConn]chan struct{}

	qmu     sync.Mutex
	queue   []stateJob[Role, Snapshot, Event]
	wake    chan struct{}
	stop    chan struct{}
	stopped atomic.Bool

	nextSeq uint64

	closeOnOverflow atomic.Bool
}

// CloseOnOverflow, on, closes a connection whose send buffer is full and
// deepens the job queue; an event a full queue refuses closes every
// connection it would have reached; off (the default) drops. Call before Run.
func (c *StateChannel[Role, Snapshot, Event]) CloseOnOverflow(on bool) {
	c.closeOnOverflow.Store(on)
}

type connRole[Role comparable] struct {
	conn *WebSocketConn
	role Role
}

type stateJobKind uint8

const (
	stateJobEvent stateJobKind = iota
	stateJobSnapshot
	stateJobLost
)

const (
	lossyQueueDepth    = 64
	losslessQueueDepth = 4096
)

type stateJob[Role comparable, Snapshot any, Event any] struct {
	kind  stateJobKind
	typ   string
	event Event
	conn  *WebSocketConn
	role  Role
	done  chan struct{}
}

// NewStateChannel creates a StateChannel over source. Call Run in a
// goroutine before Connect.
func NewStateChannel[Role comparable, Snapshot any, Event any](
	source SnapshotSource[Role, Snapshot, Event],
) *StateChannel[Role, Snapshot, Event] {
	return &StateChannel[Role, Snapshot, Event]{
		source:   source,
		conns:    make(map[*WebSocketConn]Role),
		watchers: make(map[*WebSocketConn]chan struct{}),
		wake:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
	}
}

// Connect queues conn's hydration snapshot for role and returns once it is
// queued; conn then receives published events. A stopped channel or a full
// job queue closes conn instead.
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
	if !c.push(job) {
		go conn.Close()
		return
	}
	select {
	case <-job.done:
	case <-c.stop:
		go conn.Close()
	case <-conn.Closed():
	}
}

// Publish queues an event for every connected role without blocking; typ
// becomes the envelope's Type and each role gets FilterEvent's payload.
func (c *StateChannel[Role, Snapshot, Event]) Publish(typ string, event Event) {
	if c.stopped.Load() {
		return
	}
	c.push(stateJob[Role, Snapshot, Event]{kind: stateJobEvent, typ: typ, event: event})
}

func (c *StateChannel[Role, Snapshot, Event]) push(job stateJob[Role, Snapshot, Event]) bool {
	lossless := c.closeOnOverflow.Load()
	depth := lossyQueueDepth
	if lossless {
		depth = losslessQueueDepth
	}
	c.qmu.Lock()
	queued := len(c.queue) < depth
	switch {
	case queued:
		c.queue = append(c.queue, job)
	case lossless && job.kind == stateJobEvent &&
		(len(c.queue) == 0 || c.queue[len(c.queue)-1].kind != stateJobLost):
		c.queue = append(c.queue, stateJob[Role, Snapshot, Event]{kind: stateJobLost})
	}
	c.qmu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return queued
}

func (c *StateChannel[Role, Snapshot, Event]) pop() (stateJob[Role, Snapshot, Event], bool) {
	c.qmu.Lock()
	defer c.qmu.Unlock()
	var zero stateJob[Role, Snapshot, Event]
	if len(c.queue) == 0 {
		return zero, false
	}
	job := c.queue[0]
	c.queue[0] = zero
	c.queue = c.queue[1:]
	if len(c.queue) == 0 {
		c.queue = nil
	}
	return job, true
}

// Unregister removes a connection without closing it.
func (c *StateChannel[Role, Snapshot, Event]) Unregister(conn *WebSocketConn) {
	c.mu.Lock()
	if c.conns != nil {
		delete(c.conns, conn)
	}
	if stop, ok := c.watchers[conn]; ok {
		close(stop)
		delete(c.watchers, conn)
	}
	c.mu.Unlock()
}

// Stop stops the channel and closes every registered connection. Safe to
// call twice.
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

// Run is the channel's loop; call it in a goroutine. It returns after
// Stop.
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
			for _, conn := range conns {
				go conn.Close()
			}
			return
		case <-c.wake:
			for !c.stopped.Load() {
				job, ok := c.pop()
				if !ok {
					break
				}
				switch job.kind {
				case stateJobSnapshot:
					c.runSnapshot(job)
				case stateJobLost:
					c.runLost()
				default:
					c.runEvent(job)
				}
			}
		}
	}
}

func (c *StateChannel[Role, Snapshot, Event]) runLost() {
	c.mu.RLock()
	conns := make([]*WebSocketConn, 0, len(c.conns))
	for conn := range c.conns {
		conns = append(conns, conn)
	}
	c.mu.RUnlock()
	closed := 0
	for _, conn := range conns {
		if conn.closing.CompareAndSwap(false, true) {
			closed++
			go conn.Close()
		}
	}
	slog.Default().Warn("stream: state channel queue full; closed the connections that missed an event",
		"closed", closed)
}

func (c *StateChannel[Role, Snapshot, Event]) runSnapshot(job stateJob[Role, Snapshot, Event]) {
	defer close(job.done)
	defer func() {
		if rec := recover(); rec != nil {
			logHostPanic("snapshot", rec)
			go job.conn.Close()
		}
	}()

	snap, seq := c.source.SnapshotFor(job.role)
	seq = max(seq, c.nextSeq)
	c.nextSeq = seq + 1

	c.mu.Lock()
	if c.conns == nil {
		c.mu.Unlock()
		go job.conn.Close()
		return
	}
	c.conns[job.conn] = job.role
	var stop chan struct{}
	if _, watched := c.watchers[job.conn]; !watched {
		if c.watchers == nil {
			c.watchers = make(map[*WebSocketConn]chan struct{})
		}
		stop = make(chan struct{})
		c.watchers[job.conn] = stop
	}
	c.mu.Unlock()
	if stop != nil {
		go func(conn *WebSocketConn) {
			select {
			case <-conn.Closed():
				c.Unregister(conn)
			case <-stop:
			case <-c.stop:
			}
		}(job.conn)
	}

	data, err := json.Marshal(SequencedEnvelope[Snapshot]{
		Sequence: seq,
		Type:     SnapshotEnvelopeType,
		Payload:  snap,
		SentAt:   time.Now(),
	})
	var msg wsMessage
	if err == nil {
		msg, err = textMessage(data)
	}
	if err != nil {
		go job.conn.Close()
		return
	}
	select {
	case <-job.conn.Closed():
	default:
		select {
		case job.conn.sendBuffer <- msg:
		default:
			go job.conn.Close()
		}
	}
}

func (c *StateChannel[Role, Snapshot, Event]) runEvent(job stateJob[Role, Snapshot, Event]) {
	seq := c.nextSeq
	c.nextSeq++
	defer func() {
		if rec := recover(); rec != nil {
			logHostPanic("event", rec)
		}
	}()

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

func (c *StateChannel[Role, Snapshot, Event]) deliver(conns []*WebSocketConn, role Role, job stateJob[Role, Snapshot, Event], seq uint64) {
	defer func() {
		if rec := recover(); rec != nil {
			logHostPanic("event", rec)
		}
	}()
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
	var msg wsMessage
	if err == nil {
		msg, err = textMessage(data)
	}
	if err != nil {
		return
	}
	for _, conn := range conns {
		select {
		case <-conn.Closed():
		case conn.sendBuffer <- msg:
		default:
			if c.closeOnOverflow.Load() && conn.closing.CompareAndSwap(false, true) {
				go conn.Close()
			}
		}
	}
}

func logHostPanic(unit string, rec any) {
	slog.Default().Error("stream: state channel source callback panicked; dropped one "+unit,
		"panic", textsafe.Recovered(rec))
}
