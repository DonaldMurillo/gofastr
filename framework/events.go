package framework

import (
	"context"
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
func (eb *EventBus) Subscribe(eventType string, handler EventHandler) (cancel func()) {
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

// snapshot returns a copy of the handlers registered for the event type, in
// registration order, so emission doesn't hold the lock while user code runs.
func (eb *EventBus) snapshot(eventType string) []EventHandler {
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
func (eb *EventBus) Emit(ctx context.Context, event Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	for _, h := range eb.snapshot(event.Type) {
		if err := h(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

// EmitAsync publishes an event in a goroutine (fire-and-forget).
func (eb *EventBus) EmitAsync(ctx context.Context, event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	hs := eb.snapshot(event.Type)
	go func() {
		for _, h := range hs {
			_ = h(ctx, event)
		}
	}()
}
