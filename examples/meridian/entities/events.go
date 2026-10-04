package entities

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework"
)

// onEntityEvent subscribes to one of the record lifecycle events for entity
// (entity.created / entity.updated), unmarshalling the payload's record into
// a *T before invoking fn. Shared body behind every generated On<Camel>Created
// and On<Camel>Updated wrapper.
func onEntityEvent[T any](app *framework.App, entity, eventType string, fn func(ctx context.Context, row *T) error) func() {
	return app.Events().Subscribe(eventType, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractEntityRecord[T](ev, entity)
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// onEntityDeleted subscribes to entity.deleted events for entity. By the time
// the event fires the row has been removed (or soft-deleted), so the callback
// receives the deleted row's id only. Shared body behind every generated
// On<Camel>Deleted wrapper.
func onEntityDeleted(app *framework.App, entity string, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != entity {
			return nil
		}
		record, _ := data["record"].(map[string]any)
		id, _ := record["id"].(string)
		if id == "" {
			return nil
		}
		return fn(ctx, id)
	})
}

// extractEntityRecord unmarshals an event payload's "record" field for entity
// into a *T, returning ok=false if the event is for a different entity or the
// payload shape doesn't match. Shared body behind every generated
// extract<Camel>Record wrapper.
func extractEntityRecord[T any](ev framework.Event, entity string) (*T, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entity {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v T
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}
