package framework

import "github.com/DonaldMurillo/gofastr/framework/event"

// Root spellings of framework/event. framework.X is the public API; the
// extraction moved the implementation, not the name callers write.

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
