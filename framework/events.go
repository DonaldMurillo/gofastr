package framework

import (
	"context"
	"sync"
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

// EventBus provides in-process publish/subscribe event delivery.
type EventBus struct {
	mu       sync.RWMutex
	handlers map[string][]EventHandler
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[string][]EventHandler),
	}
}

// On subscribes a handler to the given event type.
func (eb *EventBus) On(eventType string, handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.handlers[eventType] = append(eb.handlers[eventType], handler)
}

// Emit publishes an event synchronously to all subscribers.
// Handlers are called in registration order. Stops and returns the first error.
func (eb *EventBus) Emit(ctx context.Context, event Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	eb.mu.RLock()
	handlers := make([]EventHandler, len(eb.handlers[event.Type]))
	copy(handlers, eb.handlers[event.Type])
	eb.mu.RUnlock()

	for _, h := range handlers {
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

	eb.mu.RLock()
	handlers := make([]EventHandler, len(eb.handlers[event.Type]))
	copy(handlers, eb.handlers[event.Type])
	eb.mu.RUnlock()

	go func() {
		for _, h := range handlers {
			_ = h(ctx, event) // ignore errors in async mode
		}
	}()
}
