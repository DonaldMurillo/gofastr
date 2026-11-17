package event

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/db"
)

// The CRUD lifecycle event types.
const (
	EntityCreated = "entity.created"
	EntityUpdated = "entity.updated"
	EntityDeleted = "entity.deleted"
)

// DataKeyOwnerID is the Data key holding a CRUD event's record owner id.
const DataKeyOwnerID = "ownerId"

// Event is something that happened, delivered to the handlers of its Type.
type Event struct {
	// ID is the outbox row ID on a durable delivery, empty on a live emit.
	ID        string    `json:"id,omitempty"`
	Type      string    `json:"type"`
	Data      any       `json:"data,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// EventHandler is a callback that processes an event.
type EventHandler func(ctx context.Context, event Event) error

type subEntry struct {
	id      uint64
	handler EventHandler
}

// EventBus is an in-process publish/subscribe bus.
type EventBus struct {
	mu       sync.RWMutex
	handlers map[string][]subEntry
	nextID   atomic.Uint64

	tap *tapHandle

	fanoutAttached bool
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[string][]subEntry),
	}
}

type tapHandle struct {
	fn func(context.Context, Event)
}

type remoteCtxKey struct{}

func withRemoteReemit(ctx context.Context) context.Context {
	return context.WithValue(ctx, remoteCtxKey{}, true)
}

// IsRemote reports whether the event arrived from another replica through
// AttachFanout; a handler that derives a new event skips it then.
func IsRemote(ctx context.Context) bool {
	_, ok := ctx.Value(remoteCtxKey{}).(bool)
	return ok
}

func (eb *EventBus) setTap(fn func(context.Context, Event)) (clear func()) {
	h := &tapHandle{fn: fn}
	eb.mu.Lock()
	eb.tap = h
	eb.mu.Unlock()
	return func() {
		eb.mu.Lock()
		if eb.tap == h {
			eb.tap = nil
		}
		eb.mu.Unlock()
	}
}

func (eb *EventBus) invokeTap(ctx context.Context, event Event) {
	eb.mu.RLock()
	h := eb.tap
	eb.mu.RUnlock()
	if h != nil && h.fn != nil {
		//gofastr:allow(recovercallback) the tap is this package's own fanout-bridge closure, installed only by the unexported setTap from AttachFanout; it recovers its marshal (marshalEvent) and only does non-blocking channel sends, and no host code is stored in it
		h.fn(ctx, event)
	}
}

// On registers handler for eventType for the bus's lifetime.
func (eb *EventBus) On(eventType string, handler EventHandler) {
	_ = eb.Subscribe(eventType, handler)
}

// Subscribe registers handler for eventType and returns a cancel that removes
// it, safe to call twice. A nil handler registers nothing.
func (eb *EventBus) Subscribe(eventType string, handler EventHandler) (cancel func()) {
	if handler == nil {
		return func() {}
	}
	id := eb.nextID.Add(1)
	eb.mu.Lock()
	eb.handlers[eventType] = append(eb.handlers[eventType], subEntry{id: id, handler: handler})
	eb.mu.Unlock()
	return func() {
		eb.mu.Lock()
		entries := eb.handlers[eventType]
		for i, e := range entries {
			if e.id == id {
				if entries = slices.Delete(entries, i, i+1); len(entries) == 0 {
					delete(eb.handlers, eventType)
				} else {
					eb.handlers[eventType] = entries
				}
				break
			}
		}
		eb.mu.Unlock()
	}
}

// Snapshot returns a copy of eventType's handlers in registration order.
func (eb *EventBus) Snapshot(eventType string) []EventHandler {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	entries := eb.handlers[eventType]
	out := make([]EventHandler, len(entries))
	for i, e := range entries {
		out[i] = e.handler
	}
	return out
}

// Emit runs the type's handlers in order and returns the first error; a
// handler's panic is logged and swallowed.
func (eb *EventBus) Emit(ctx context.Context, event Event) error {
	return eb.emit(ctx, event, emitSafe)
}

// EmitStrict is Emit that returns a handler's panic as an error.
func (eb *EventBus) EmitStrict(ctx context.Context, event Event) error {
	return eb.emit(ctx, event, emitStrict)
}

func (eb *EventBus) emit(ctx context.Context, event Event, invoke func(context.Context, EventHandler, Event) error) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	eb.invokeTap(ctx, event)
	handlers := eb.Snapshot(event.Type)
	observeEmission(event.Type, len(handlers))
	for _, h := range handlers {
		if h == nil {
			continue
		}
		if err := invoke(ctx, h, event); err != nil {
			return err
		}
	}
	return nil
}

func emitStrict(ctx context.Context, h EventHandler, event Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Default().Error("event: subscriber panicked (surfaced as a delivery error)",
				"event_type", event.Type,
				"panic", textsafe.Recovered(r),
				"stack", string(debug.Stack()),
			)
			err = fmt.Errorf("event: subscriber for %q panicked: %v", event.Type, r)
		}
	}()
	return h(ctx, event)
}

// AsyncHandlerTimeout bounds each EmitAsync handler.
const AsyncHandlerTimeout = time.Minute

// EmitAsync runs each handler on its own goroutine and returns at once.
// Handlers get ctx's values without its cancel or its transaction, each
// bounded by AsyncHandlerTimeout; a panic is logged and swallowed.
func (eb *EventBus) EmitAsync(ctx context.Context, event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	hs := eb.Snapshot(event.Type)
	ctx = context.WithoutCancel(ctx)
	ctx = db.WithoutTx(ctx)
	go func() {
		tctx, cancel := context.WithTimeout(ctx, AsyncHandlerTimeout)
		eb.invokeTap(tctx, event)
		cancel()
		for _, h := range hs {
			if h == nil {
				continue
			}
			go func() {
				hctx, cancel := context.WithTimeout(ctx, AsyncHandlerTimeout)
				defer cancel()
				_ = emitSafe(hctx, h, event)
			}()
		}
	}()
}

func emitSafe(ctx context.Context, h EventHandler, event Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Default().Error("event: subscriber panicked; recovered (event delivery is best-effort, the write is unaffected)",
				"event_type", event.Type,
				"panic", textsafe.Recovered(r),
				"stack", string(debug.Stack()),
			)
			err = nil
		}
	}()
	return h(ctx, event)
}
