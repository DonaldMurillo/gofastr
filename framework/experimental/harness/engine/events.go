package engine

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// Bus is one session's in-memory event bus: it numbers events, fans them out to subscribers and keeps a bounded replay window.
type Bus struct {
	session ids.SessionID

	nextID atomic.Uint64 // monotonic, atomic; unused on a sub-agent bus

	parent   *Bus
	subAgent string

	mu          sync.RWMutex
	subscribers map[*subscription]struct{}
	closed      bool

	closing   chan struct{}
	closeOnce sync.Once

	histMu       sync.Mutex
	hist         []control.EventEnvelope
	histBytes    int
	evicted      uint64
	histMax      int
	histMaxBytes int

	logMu sync.RWMutex
	log   EventLog
}

// EventLog is the read side of the durable session log; session.Store satisfies it.
type EventLog interface {
	EventsSince(ctx context.Context, session ids.SessionID, since uint64, limit int) ([]control.EventEnvelope, error)
}

const (
	defaultHistMax      = 1024
	defaultHistMaxBytes = 8 << 20
	logPage             = 1000
)

type subscription struct {
	ch     chan control.EventEnvelope
	ctx    context.Context
	cancel context.CancelFunc
	all    bool
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

// NewSubBus returns a sub-agent's private bus whose events take ids from the root bus and reach the parent's SubscribeAll subscribers.
func NewSubBus(parent *Bus, subAgent string) *Bus {
	b := NewBus(parent.session)
	b.parent = parent
	b.subAgent = subAgent
	b.histMax = 0
	return b
}

// Session returns the SessionID this bus is bound to.
func (b *Bus) Session() ids.SessionID { return b.session }

// SetLog sets the durable log SubscribeSince and Backlog read events older than the replay window from.
func (b *Bus) SetLog(l EventLog) {
	b.logMu.Lock()
	b.log = l
	b.logMu.Unlock()
}

// Subscribe returns this bus's own events, dropping any a slow reader has no room for; the channel closes with ctx or the bus.
func (b *Bus) Subscribe(ctx context.Context) <-chan control.EventEnvelope {
	ch, _ := b.subscribe(ctx, false, nil)
	return ch
}

// SubscribeAll is a lossless Subscribe that also carries every sub-agent bus's events.
func (b *Bus) SubscribeAll(ctx context.Context) <-chan control.EventEnvelope {
	ch, _ := b.subscribe(ctx, true, nil)
	return ch
}

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
		b.mu.Lock()
		delete(b.subscribers, sub)
		close(sub.ch)
		b.mu.Unlock()
	}()
	return sub.ch, true
}

// SubscribeSince replays every event after id since, with a StreamGap for a range nothing holds, then follows the live stream.
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

// Backlog returns up to limit events after id since, from the log and the replay window, with a StreamGap for a range neither holds; limit 0 means no limit.
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
		page, err := b.readLog(ctx, log, last)
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

func (b *Bus) readLog(ctx context.Context, log EventLog, since uint64) (page []control.EventEnvelope, err error) {
	defer func() {
		if r := recover(); r != nil {
			page, err = nil, fmt.Errorf("engine: session log read panicked: %s", textsafe.Recovered(r))
		}
	}()
	return log.EventsSince(ctx, b.session, since, logPage)
}

func (b *Bus) gap(from, to uint64) control.EventEnvelope {
	env, _ := control.EncodeEvent(0, control.StreamGap{From: from, To: to, Reason: "unavailable"}, b.session, "", time.Now().UTC())
	return env
}

// Publish assigns e the next id, broadcasts it and returns the envelope.
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

func (b *Bus) root() *Bus {
	for b.parent != nil {
		b = b.parent
	}
	return b
}

// NextID returns the id the next Publish will assign.
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
	b.mu.RLock()
	defer b.mu.RUnlock()
	b.remember(env)
	for s := range b.subscribers {
		b.deliver(s, env)
	}
}

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
	default:
	}
}

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
