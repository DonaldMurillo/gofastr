package framework

import "github.com/DonaldMurillo/gofastr/framework/event"

// Root spellings of framework/event.

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

func NewEventBus() *event.EventBus { return event.NewEventBus() }
