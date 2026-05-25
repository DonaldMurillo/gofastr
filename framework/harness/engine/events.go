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

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// Bus is the typed event bus per SessionID. Events have monotonically
// increasing IDs so any transport can implement resume-from-id without
// transport-specific bookkeeping.
//
// Subscribers receive a buffered channel; slow subscribers risk
// dropped events. The Bus does NOT persist — see session.Store for the
// durable log.
type Bus struct {
	session ids.SessionID

	nextID uint64 // monotonic, atomic

	mu          sync.RWMutex
	subscribers map[*subscription]struct{}
	closed      bool
}

type subscription struct {
	ch     chan control.EventEnvelope
	ctx    context.Context
	cancel context.CancelFunc
}

// NewBus returns a new Bus for the given session.
func NewBus(session ids.SessionID) *Bus {
	return &Bus{
		session:     session,
		subscribers: make(map[*subscription]struct{}),
	}
}

// Session returns the SessionID this bus is bound to.
func (b *Bus) Session() ids.SessionID { return b.session }

// Subscribe returns a receive-only channel of events. The channel is
// closed when ctx is cancelled or the Bus is closed.
//
// Buffer is sized to absorb short bursts without blocking the engine.
// Subscribers that fall behind have their stale events dropped to
// preserve liveness — see § Per-transport rules → Backpressure for
// the policy.
func (b *Bus) Subscribe(ctx context.Context) <-chan control.EventEnvelope {
	sub := &subscription{ch: make(chan control.EventEnvelope, 256)}
	sub.ctx, sub.cancel = context.WithCancel(ctx)

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(sub.ch)
		return sub.ch
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
	return sub.ch
}

// Publish encodes the event into a canonical envelope, assigns the
// next sequence ID, and broadcasts to every active subscriber. It
// returns the envelope it published so callers can persist or inspect.
func (b *Bus) Publish(e control.Event, originator ids.ClientID) (control.EventEnvelope, error) {
	id := atomic.AddUint64(&b.nextID, 1)
	env, err := control.EncodeEvent(id, e, b.session, originator, time.Now().UTC())
	if err != nil {
		return control.EventEnvelope{}, err
	}
	b.broadcast(env)
	return env, nil
}

// Replay sends pre-encoded envelopes to a single subscription. Used
// by transports implementing resume-from-id: they fetch missing events
// from session.Store and replay them before the live stream resumes.
//
// Note: Replay does NOT assign new IDs; envelopes retain their original
// sequence numbers from the persistent log.
func (b *Bus) Replay(envelopes []control.EventEnvelope, dst chan<- control.EventEnvelope) {
	for _, env := range envelopes {
		dst <- env
	}
}

// NextID returns the next sequence ID that will be assigned. Useful
// for testing.
func (b *Bus) NextID() uint64 { return atomic.LoadUint64(&b.nextID) + 1 }

// Close terminates the bus and all subscriptions.
func (b *Bus) Close() {
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
	// Hold RLock through the send loop — the per-sub close
	// goroutine takes WLock to delete + close, so it can't race a
	// send-on-closed panic. Sends are non-blocking (select default
	// drops) so holding the lock isn't a liveness problem.
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subscribers {
		select {
		case s.ch <- env:
			// delivered
		default:
			// Subscriber is slow; drop to preserve liveness. In
			// practice a transport that needs lossless delivery
			// implements its own buffering against session.Store.
		}
	}
}
