package event

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// Pre-defined event types for entity lifecycle.
const (
	EntityCreated = "entity.created"
	EntityUpdated = "entity.updated"
	EntityDeleted = "entity.deleted"
)

// Event represents something that happened in the system.
type Event struct {
	// ID uniquely identifies this delivery. It is stamped by the
	// outbox Relay (framework/outbox) from the persisted row's ID so
	// consumers can deduplicate at-least-once deliveries. It is empty
	// for events emitted directly via Emit/EmitAsync, which carry no
	// durable identity.
	ID        string    `json:"id,omitempty"`
	Type      string    `json:"type"`
	Data      any       `json:"data,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// EventHandler is a callback that processes an event.
type EventHandler func(ctx context.Context, event Event) error

// subEntry pairs a subscription id with its handler. Slice-of-entries
// preserves registration order so Emit can short-circuit deterministically.
type subEntry struct {
	id      uint64
	handler EventHandler
}

// EventBus provides in-process publish/subscribe event delivery.
//
// Both On (no cancel) and Subscribe (returns cancel) are supported. Handlers
// are stored in registration order so Emit short-circuits deterministically
// on the first error.
type EventBus struct {
	mu       sync.RWMutex
	handlers map[string][]subEntry
	nextID   atomic.Uint64
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[string][]subEntry),
	}
}

// On subscribes a handler to the given event type. The handler stays
// registered for the lifetime of the bus; use Subscribe instead when you
// need to remove the handler later.
func (eb *EventBus) On(eventType string, handler EventHandler) {
	_ = eb.Subscribe(eventType, handler)
}

// Subscribe registers a handler and returns a cancel function. Calling cancel
// removes the handler. Cancel is safe to call multiple times.
//
// A nil handler is refused — the bus never retains a nil subscription, so a
// later Emit can't crash by invoking nothing. The returned cancel is a no-op
// in that case.
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
				eb.handlers[eventType] = append(entries[:i], entries[i+1:]...)
				break
			}
		}
		eb.mu.Unlock()
	}
}

// Snapshot returns a copy of the handlers registered for the event type, in
// registration order, so emission doesn't hold the lock while user code runs.
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

// Emit publishes an event synchronously to all subscribers and returns the
// first error from a handler.
//
// A handler that panics is recovered so a buggy subscriber can't bring down
// the request that triggered the event. The panic is swallowed (the event bus
// is fire-and-iterate, not a critical-path return channel); callers that need
// hard failures should propagate them through the returned error instead. A
// nil entry — which shouldn't happen given Subscribe's guard, but kept for
// defense-in-depth against direct map manipulation — is skipped.
func (eb *EventBus) Emit(ctx context.Context, event Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	for _, h := range eb.Snapshot(event.Type) {
		if h == nil {
			continue
		}
		if err := emitSafe(ctx, h, event); err != nil {
			return err
		}
	}
	return nil
}

// EmitStrict publishes synchronously like Emit, but treats a panicking
// subscriber as a delivery ERROR (returned to the caller) rather than
// swallowing it. Emit's swallow protects user transactions when the bus is
// wired into AfterCreate/AfterUpdate hooks; the transactional outbox relay
// has the opposite need — a consumer that panics must be retried and
// eventually dead-lettered, never silently marked dispatched — so it calls
// this instead. Returns the first handler error or recovered panic.
func (eb *EventBus) EmitStrict(ctx context.Context, event Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	for _, h := range eb.Snapshot(event.Type) {
		if h == nil {
			continue
		}
		if err := emitStrict(ctx, h, event); err != nil {
			return err
		}
	}
	return nil
}

// emitStrict invokes h with a deferred recover that converts a panic into an
// error (rather than swallowing it, as emitSafe does). Used only by
// EmitStrict.
func emitStrict(ctx context.Context, h EventHandler, event Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Default().Error("event: subscriber panicked (surfaced as a delivery error)",
				"event_type", event.Type,
				"panic", r,
				"stack", string(debug.Stack()),
			)
			err = fmt.Errorf("event: subscriber for %q panicked: %v", event.Type, r)
		}
	}()
	return h(ctx, event)
}

// EmitAsync publishes an event in a goroutine (fire-and-forget).
//
// Each handler runs inside emitSafe so a panicking subscriber takes itself
// down without crashing the process. Nil entries are skipped defensively.
func (eb *EventBus) EmitAsync(ctx context.Context, event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	hs := eb.Snapshot(event.Type)
	go func() {
		for _, h := range hs {
			if h == nil {
				continue
			}
			_ = emitSafe(ctx, h, event)
		}
	}()
}

// emitSafe invokes h with a deferred recover. A panic becomes a discarded
// error so neither the calling request nor the async goroutine crashes the
// process. Returning the handler's nominal error keeps the existing Emit
// short-circuit contract intact.
func emitSafe(ctx context.Context, h EventHandler, event Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			// Swallow the ERROR — see method docs. We deliberately don't
			// surface the panic as an error because callers wire Emit into
			// AfterCreate / AfterUpdate hooks where any non-nil return
			// rolls back the user's transaction; a flaky subscriber
			// shouldn't gain veto over real writes. But a silently-
			// no-op'd handler ("send welcome email" that panicked) is a
			// debugging black hole, so LOG it at Error with the stack.
			slog.Default().Error("event: subscriber panicked; recovered (event delivery is best-effort, the write is unaffected)",
				"event_type", event.Type,
				"panic", r,
				"stack", string(debug.Stack()),
			)
			err = nil
		}
	}()
	return h(ctx, event)
}
