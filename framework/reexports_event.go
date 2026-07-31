package framework

import "github.com/DonaldMurillo/gofastr/framework/event"

// Re-exports of framework/event so existing callers and generated code
// using framework.X keep compiling after the event package extraction.

type (
	Event        = event.Event
	EventHandler = event.EventHandler
	EventBus     = event.EventBus
)

const (
	EntityCreated = event.EntityCreated
	EntityUpdated = event.EntityUpdated
	EntityDeleted = event.EntityDeleted
)

// NewEventBus wraps event.NewEventBus.
func NewEventBus() *event.EventBus { return event.NewEventBus() }
