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

	// tap, when set via setTap, is invoked once per Emit/EmitAsync/
	// EmitStrict (after the timestamp is stamped) so a fanout bridge
	// (AttachFanout) can mirror local emissions to other replicas.
	// Guarded by mu.
	tap *tapHandle

	// fanoutAttached is set by AttachFanout and cleared by its stop, so a
	// second AttachFanout on the same bus errors instead of leaving a stale
	// subscription live (which would echo this bus's own emissions back).
	// Guarded by mu.
	fanoutAttached bool
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[string][]subEntry),
	}
}

// tapHandle wraps a fanout-bridge tap so setTap/clear can compare the
// installed handle by pointer identity (func values are not comparable in
// Go, but *tapHandle is). One bus carries at most one tap.
type tapHandle struct {
	fn func(context.Context, Event)
}

// remoteCtxKey marks a context as carrying a remote-reemit so the fanout
// tap skips republishing it — the loop guard for AttachFanout.
type remoteCtxKey struct{}

// withRemoteReemit returns a context whose tap signal is suppressed: an
// Emit/EmitAsync/EmitStrict on this context will NOT be mirrored to the
// fanout by the bridge. Used only by AttachFanout to re-emit events
// received from a remote replica without looping them back out.
func withRemoteReemit(ctx context.Context) context.Context {
	return context.WithValue(ctx, remoteCtxKey{}, true)
}

// IsRemote reports whether the event being handled arrived from another
// replica via an attached fanout. Handlers that DERIVE new events from the
// events they receive must gate on it — `if event.IsRemote(ctx) { return nil }`
// — so the derivation runs only on the origin replica; otherwise every replica
// derives its own copy and remote replicas observe duplicates.
//
// Locally-emitted events (Emit/EmitAsync/EmitStrict on a normal context) report
// false; events re-emitted by AttachFanout from a remote replica report true.
func IsRemote(ctx context.Context) bool {
	_, ok := ctx.Value(remoteCtxKey{}).(bool)
	return ok
}

// setTap installs fn as the bus's tap (invoked once per emit after the
// timestamp is stamped). The returned clear removes the tap; it only
// clears if the current tap is still the one it installed, so a later
// setTap that overwrote it is not clobbered. Safe to call multiple times.
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

// invokeTap fires the installed tap (if any) for event. The tap is the
// fanout bridge's hook; it must not block or panic on the emit path, and
// it must honor the remote-reemit marker (see withRemoteReemit) to avoid
// re-broadcasting events received from a remote replica.
func (eb *EventBus) invokeTap(ctx context.Context, event Event) {
	eb.mu.RLock()
	h := eb.tap
	eb.mu.RUnlock()
	if h != nil && h.fn != nil {
		h.fn(ctx, event)
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
	eb.invokeTap(ctx, event)
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
func (eb *EventBus) EmitStrict(ctx context.Context, event Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	eb.invokeTap(ctx, event)
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
		// The tap runs inside the goroutine so a (best-effort, normally
		// fast) Publish on the fanout can't add latency to the
		// fire-and-forget caller. The remote-reemit marker is still
		// honored, preventing broadcast loops.
		eb.invokeTap(ctx, event)
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
