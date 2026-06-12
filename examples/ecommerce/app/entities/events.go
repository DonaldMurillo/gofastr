package entities

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework"
)

// OnCategoriesCreated subscribes to entity.created events scoped to "categories".
// Returns a cancel func; call it to remove the handler.
func OnCategoriesCreated(app *framework.App, fn func(ctx context.Context, row *Categories) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractCategoriesRecord(ev, "categories")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnCategoriesUpdated subscribes to entity.updated events scoped to "categories".
func OnCategoriesUpdated(app *framework.App, fn func(ctx context.Context, row *Categories) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractCategoriesRecord(ev, "categories")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnCategoriesDeleted subscribes to entity.deleted events scoped to "categories". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnCategoriesDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "categories" {
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

// OnOrderItemsCreated subscribes to entity.created events scoped to "order_items".
// Returns a cancel func; call it to remove the handler.
func OnOrderItemsCreated(app *framework.App, fn func(ctx context.Context, row *OrderItems) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractOrderItemsRecord(ev, "order_items")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnOrderItemsUpdated subscribes to entity.updated events scoped to "order_items".
func OnOrderItemsUpdated(app *framework.App, fn func(ctx context.Context, row *OrderItems) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractOrderItemsRecord(ev, "order_items")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnOrderItemsDeleted subscribes to entity.deleted events scoped to "order_items". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnOrderItemsDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "order_items" {
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

// OnOrdersCreated subscribes to entity.created events scoped to "orders".
// Returns a cancel func; call it to remove the handler.
func OnOrdersCreated(app *framework.App, fn func(ctx context.Context, row *Orders) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractOrdersRecord(ev, "orders")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnOrdersUpdated subscribes to entity.updated events scoped to "orders".
func OnOrdersUpdated(app *framework.App, fn func(ctx context.Context, row *Orders) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractOrdersRecord(ev, "orders")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnOrdersDeleted subscribes to entity.deleted events scoped to "orders". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnOrdersDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "orders" {
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

// OnProductsCreated subscribes to entity.created events scoped to "products".
// Returns a cancel func; call it to remove the handler.
func OnProductsCreated(app *framework.App, fn func(ctx context.Context, row *Products) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractProductsRecord(ev, "products")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnProductsUpdated subscribes to entity.updated events scoped to "products".
func OnProductsUpdated(app *framework.App, fn func(ctx context.Context, row *Products) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractProductsRecord(ev, "products")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnProductsDeleted subscribes to entity.deleted events scoped to "products". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnProductsDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "products" {
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

// OnReviewsCreated subscribes to entity.created events scoped to "reviews".
// Returns a cancel func; call it to remove the handler.
func OnReviewsCreated(app *framework.App, fn func(ctx context.Context, row *Reviews) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractReviewsRecord(ev, "reviews")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnReviewsUpdated subscribes to entity.updated events scoped to "reviews".
func OnReviewsUpdated(app *framework.App, fn func(ctx context.Context, row *Reviews) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractReviewsRecord(ev, "reviews")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnReviewsDeleted subscribes to entity.deleted events scoped to "reviews". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnReviewsDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "reviews" {
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

// extractCategoriesRecord unmarshals an event payload's "record" field into a
// *Categories, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractCategoriesRecord(ev framework.Event, entityName string) (*Categories, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Categories
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}

// extractOrderItemsRecord unmarshals an event payload's "record" field into a
// *OrderItems, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractOrderItemsRecord(ev framework.Event, entityName string) (*OrderItems, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v OrderItems
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}

// extractOrdersRecord unmarshals an event payload's "record" field into a
// *Orders, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractOrdersRecord(ev framework.Event, entityName string) (*Orders, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Orders
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}

// extractProductsRecord unmarshals an event payload's "record" field into a
// *Products, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractProductsRecord(ev framework.Event, entityName string) (*Products, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Products
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}

// extractReviewsRecord unmarshals an event payload's "record" field into a
// *Reviews, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractReviewsRecord(ev framework.Event, entityName string) (*Reviews, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Reviews
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}
