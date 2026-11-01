// Package engine implements the agent loop and its supporting
// machinery: typed event bus, request/tool middleware chains,
// cancellation tree, and the loop itself.
//
// See docs/harness-architecture.md § The agent loop and § Extensibility.
package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// Bus is the typed event bus per SessionID. Events have monotonically
// increasing IDs so any transport can implement resume-from-id without
// transport-specific bookkeeping.
//
// Subscribers receive a buffered channel; slow subscribers risk
// dropped events. The Bus does NOT persist. See session.Store for the
// durable log; SetLog hands the bus a read view of it so SubscribeSince
// can replay events older than its in-memory window.
type Bus struct {
	session ids.SessionID

	nextID atomic.Uint64 // monotonic, atomic; unused on a sub-agent bus

	// parent is set on a sub-agent bus (NewSubBus): ids come from the
	// root bus, and every event is forwarded to the parent's audit
	// subscribers tagged with subAgent.
	parent   *Bus
	subAgent string

	mu          sync.RWMutex
	subscribers map[*subscription]struct{}
	closed      bool

	// closing is closed when Close starts, before it takes mu, so a
	// publish blocked on a lossless subscriber (SubscribeAll) gives up
	// and Close is never stuck behind a store that stopped reading.
	closing   chan struct{}
	closeOnce sync.Once

	// hist is the replay window: the last histMax own events, at most
	// histMaxBytes of payload. evicted is the highest id pushed out of
	// it, so a resume from below it must read the log. Guarded by
	// histMu, taken inside mu (read or write).
	histMu       sync.Mutex
	hist         []control.EventEnvelope
	histBytes    int
	evicted      uint64
	histMax      int
	histMaxBytes int

	logMu sync.RWMutex
	log   EventLog
}

// EventLog is the read side of the durable session log; session.Store
// satisfies it.
type EventLog interface {
	EventsSince(ctx context.Context, session ids.SessionID, since uint64, limit int) ([]control.EventEnvelope, error)
}

// Replay window bounds. A reconnecting client further behind than this
// is served from the session log.
const (
	defaultHistMax      = 1024
	defaultHistMaxBytes = 8 << 20
	logPage             = 1000
)

type subscription struct {
	ch     chan control.EventEnvelope
	ctx    context.Context
	cancel context.CancelFunc
	// all also receives events from sub-agent buses under this one,
	// and is lossless: a send waits for room instead of dropping.
	all bool
}

// NewBus returns a new Bus for the given session.
func NewBus(session ids.SessionID) *Bus {
	return &Bus{
		session:      session,
		subscribers:  make(map[*subscription]struct{}),
		closing:      make(chan struct{}),
		histMax:      defaultHistMax,
		histMaxBytes: defaultHistMaxBytes,
	}
}

// NewSubBus returns the private bus for one sub-agent run under parent.
// Its subscribers see only the sub-agent's events, so the parent chat
// does not fill with them, but every event takes its id from the root
// bus and is forwarded, tagged with subAgent, to the parent's
// SubscribeAll subscribers: the session log records what a sub-agent
// ran the same as anything else.
func NewSubBus(parent *Bus, subAgent string) *Bus {
	b := NewBus(parent.session)
	b.parent = parent
	b.subAgent = subAgent
	b.histMax = 0
	return b
}

// Session returns the SessionID this bus is bound to.
func (b *Bus) Session() ids.SessionID { return b.session }

// SetLog wires the durable log SubscribeSince and Backlog read events
// from when they are older than the in-memory window.
func (b *Bus) SetLog(l EventLog) {
	b.logMu.Lock()
	b.log = l
	b.logMu.Unlock()
}

// Subscribe returns a receive-only channel of events. The channel is
// closed when ctx is cancelled or the Bus is closed.
//
// Buffer is sized to absorb short bursts without blocking the engine.
// Subscribers that fall behind have their stale events dropped to
// preserve liveness. See § Per-transport rules → Backpressure for
// the policy.
func (b *Bus) Subscribe(ctx context.Context) <-chan control.EventEnvelope {
	ch, _ := b.subscribe(ctx, false, nil)
	return ch
}

// SubscribeAll is Subscribe plus the events of every sub-agent bus
// under this one (SubAgent set). The session log subscribes this way;
// client streams use Subscribe.
//
// Delivery is lossless: when the channel is full, Publish waits for
// the reader instead of dropping, because the session log is the
// record resume and audit read, and a burst past the buffer used to
// leave holes in it. A reader that stops reading therefore stalls the
// session's publishers, until ctx ends or the bus starts closing.
func (b *Bus) SubscribeAll(ctx context.Context) <-chan control.EventEnvelope {
	ch, _ := b.subscribe(ctx, true, nil)
	return ch
}

// subscribe registers a subscription. When snapshot is non-nil it runs
// under the write lock, which no broadcast holds, so every event is
// either already in the replay window it reads or delivered to the new
// channel, never both and never neither.
func (b *Bus) subscribe(ctx context.Context, all bool, snapshot func()) (<-chan control.EventEnvelope, bool) {
	sub := &subscription{ch: make(chan control.EventEnvelope, 256), all: all}
	sub.ctx, sub.cancel = context.WithCancel(ctx)

	b.mu.Lock()
	if snapshot != nil {
		snapshot()
	}
	if b.closed {
		b.mu.Unlock()
		close(sub.ch)
		return sub.ch, false
	}
	b.subscribers[sub] = struct{}{}
	b.mu.Unlock()

	go func() {
		<-sub.ctx.Done()
		// Close INSIDE the lock so broadcast (which holds RLock
		// through its send loop) can't race a send-on-closed panic.
		b.mu.Lock()
		delete(b.subscribers, sub)
		close(sub.ch)
		b.mu.Unlock()
	}()
	return sub.ch, true
}

// SubscribeSince is Subscribe for a client resuming after the event
// with id since: the channel first carries every event with a larger id
// (from the replay window, and from the session log for anything older
// than the window), then the live stream. An id range neither holds
// arrives as one StreamGap event (id 0) in its place, never as a silent
// hole. Sub-agent events are left out, as on the live stream.
//
// Delivery of the replay is lossless: the channel blocks rather than
// drops while the client reads it. The live part keeps Subscribe's
// drop-when-slow policy.
func (b *Bus) SubscribeSince(ctx context.Context, since uint64) <-chan control.EventEnvelope {
	var window []control.EventEnvelope
	var evicted uint64
	live, _ := b.subscribe(ctx, false, func() {
		window, evicted = b.windowSince(since)
	})
	out := make(chan control.EventEnvelope, 256)
	go func() {
		defer close(out)
		send := func(env control.EventEnvelope) bool {
			select {
			case out <- env:
				return true
			case <-ctx.Done():
				return false
			}
		}
		if !b.fromLog(ctx, since, evicted, send) {
			return
		}
		for _, env := range window {
			if !send(env) {
				return
			}
		}
		for env := range live {
			// The window and live are disjoint (see subscribe), so
			// the only filter left is the client's own position.
			if env.ID <= since {
				continue
			}
			if !send(env) {
				return
			}
		}
	}()
	return out
}

// Backlog returns up to limit events with id > since, from the session
// log and the replay window, with a StreamGap for a range neither holds.
// It is the polling form of SubscribeSince's replay.
func (b *Bus) Backlog(ctx context.Context, since uint64, limit int) []control.EventEnvelope {
	b.mu.Lock()
	window, evicted := b.windowSince(since)
	b.mu.Unlock()
	var out []control.EventEnvelope
	full := func() bool { return limit > 0 && len(out) >= limit }
	b.fromLog(ctx, since, evicted, func(env control.EventEnvelope) bool {
		out = append(out, env)
		return !full()
	})
	for _, env := range window {
		if full() {
			break
		}
		out = append(out, env)
	}
	return out
}

// windowSince copies the replay window's events with id > since and
// reports the highest evicted id. Caller holds b.mu.
func (b *Bus) windowSince(since uint64) ([]control.EventEnvelope, uint64) {
	b.histMu.Lock()
	defer b.histMu.Unlock()
	var out []control.EventEnvelope
	for _, env := range b.hist {
		if env.ID > since {
			out = append(out, env)
		}
	}
	return out, b.evicted
}

// fromLog reads ids in (since, upto] from the session log a page at a
// time, skipping sub-agent events, hands each to emit in order, and
// closes any range the log cannot produce with a StreamGap. It stops
// when emit returns false or ctx ends, and reports whether it ran to
// the end: a reader with a limit never pages the whole log into memory.
func (b *Bus) fromLog(ctx context.Context, since, upto uint64, emit func(control.EventEnvelope) bool) bool {
	if since >= upto {
		return true
	}
	b.logMu.RLock()
	log := b.log
	b.logMu.RUnlock()
	last := since
	for log != nil && last < upto {
		if ctx.Err() != nil {
			return false
		}
		page, err := log.EventsSince(ctx, b.session, last, logPage)
		if err != nil || len(page) == 0 {
			break
		}
		progressed := false
		for _, env := range page {
			if env.ID <= last {
				continue
			}
			if env.ID > upto {
				last = upto
				break
			}
			if env.ID > last+1 && !emit(b.gap(last+1, env.ID-1)) {
				return false
			}
			last = env.ID
			progressed = true
			if env.SubAgent == "" && !emit(env) {
				return false
			}
		}
		if !progressed {
			break
		}
	}
	if ctx.Err() != nil {
		return false
	}
	if last < upto {
		return emit(b.gap(last+1, upto))
	}
	return true
}

// gap builds the StreamGap envelope for ids from..to. It carries id 0:
// it is a notice about the sequence, not a member of it.
func (b *Bus) gap(from, to uint64) control.EventEnvelope {
	env, _ := control.EncodeEvent(0, control.StreamGap{From: from, To: to, Reason: "unavailable"}, b.session, "", time.Now().UTC())
	return env
}

// Publish encodes the event into a canonical envelope, assigns the
// next sequence ID, and broadcasts to every active subscriber. It
// returns the envelope it published so callers can persist or inspect.
func (b *Bus) Publish(e control.Event, originator ids.ClientID) (control.EventEnvelope, error) {
	id := b.root().nextID.Add(1)
	env, err := control.EncodeEvent(id, e, b.session, originator, time.Now().UTC())
	if err != nil {
		return control.EventEnvelope{}, err
	}
	env.SubAgent = b.subAgent
	b.broadcast(env)
	for p := b.parent; p != nil; p = p.parent {
		p.forward(env)
	}
	return env, nil
}

// root returns the bus that owns the id sequence.
func (b *Bus) root() *Bus {
	for b.parent != nil {
		b = b.parent
	}
	return b
}

// NextID returns the next sequence ID that will be assigned. Useful
// for testing.
func (b *Bus) NextID() uint64 { return b.root().nextID.Load() + 1 }

// Close terminates the bus and all subscriptions.
func (b *Bus) Close() {
	b.closeOnce.Do(func() { close(b.closing) })
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	subs := b.subscribers
	b.subscribers = nil
	b.mu.Unlock()

	for sub := range subs {
		sub.cancel() // triggers the goroutine that closes sub.ch
	}
}

func (b *Bus) broadcast(env control.EventEnvelope) {
	// Hold RLock through the send loop, the per-sub close
	// goroutine takes WLock to delete + close, so it can't race a
	// send-on-closed panic. Sends are non-blocking (select default
	// drops) so holding the lock isn't a liveness problem.
	b.mu.RLock()
	defer b.mu.RUnlock()
	b.remember(env)
	for s := range b.subscribers {
		b.deliver(s, env)
	}
}

// forward delivers a sub-agent event to the subscribers that asked for
// them (SubscribeAll). It stays out of the replay window: resuming
// clients get the same stream live clients do.
func (b *Bus) forward(env control.EventEnvelope) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subscribers {
		if s.all {
			b.deliver(s, env)
		}
	}
}

func (b *Bus) deliver(s *subscription, env control.EventEnvelope) {
	if s.all {
		// Lossless (SubscribeAll). The wait holds mu's read lock, which
		// is safe: the reader (the persist loop) never takes mu, and
		// the subscription's ctx or a starting Close ends the wait.
		// The first send is on its own so a Close already under way
		// never wins a race against a channel with room.
		select {
		case s.ch <- env:
			return
		default:
		}
		select {
		case s.ch <- env:
		case <-s.ctx.Done():
		case <-b.closing:
		}
		return
	}
	select {
	case s.ch <- env:
		// delivered
	default:
		// Subscriber is slow; drop to preserve liveness. A client that
		// needs what it missed reconnects with SubscribeSince.
	}
}

// remember appends env to the replay window and evicts past its bounds.
// Caller holds b.mu (read or write).
func (b *Bus) remember(env control.EventEnvelope) {
	if b.histMax <= 0 {
		return
	}
	b.histMu.Lock()
	defer b.histMu.Unlock()
	b.hist = append(b.hist, env)
	b.histBytes += len(env.Payload)
	for len(b.hist) > b.histMax || (b.histBytes > b.histMaxBytes && len(b.hist) > 1) {
		old := b.hist[0]
		b.hist[0] = control.EventEnvelope{}
		b.hist = b.hist[1:]
		b.histBytes -= len(old.Payload)
		b.evicted = max(b.evicted, old.ID)
	}
}
