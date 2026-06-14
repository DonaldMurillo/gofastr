package entities

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework"
)

// OnPlansCreated subscribes to entity.created events scoped to "plans".
// Returns a cancel func; call it to remove the handler.
func OnPlansCreated(app *framework.App, fn func(ctx context.Context, row *Plans) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractPlansRecord(ev, "plans")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnPlansUpdated subscribes to entity.updated events scoped to "plans".
func OnPlansUpdated(app *framework.App, fn func(ctx context.Context, row *Plans) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractPlansRecord(ev, "plans")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnPlansDeleted subscribes to entity.deleted events scoped to "plans". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnPlansDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "plans" {
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

// OnCustomersCreated subscribes to entity.created events scoped to "customers".
// Returns a cancel func; call it to remove the handler.
func OnCustomersCreated(app *framework.App, fn func(ctx context.Context, row *Customers) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractCustomersRecord(ev, "customers")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnCustomersUpdated subscribes to entity.updated events scoped to "customers".
func OnCustomersUpdated(app *framework.App, fn func(ctx context.Context, row *Customers) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractCustomersRecord(ev, "customers")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnCustomersDeleted subscribes to entity.deleted events scoped to "customers". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnCustomersDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "customers" {
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

// OnSubscriptionsCreated subscribes to entity.created events scoped to "subscriptions".
// Returns a cancel func; call it to remove the handler.
func OnSubscriptionsCreated(app *framework.App, fn func(ctx context.Context, row *Subscriptions) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractSubscriptionsRecord(ev, "subscriptions")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnSubscriptionsUpdated subscribes to entity.updated events scoped to "subscriptions".
func OnSubscriptionsUpdated(app *framework.App, fn func(ctx context.Context, row *Subscriptions) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractSubscriptionsRecord(ev, "subscriptions")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnSubscriptionsDeleted subscribes to entity.deleted events scoped to "subscriptions". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnSubscriptionsDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "subscriptions" {
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

// OnInvoicesCreated subscribes to entity.created events scoped to "invoices".
// Returns a cancel func; call it to remove the handler.
func OnInvoicesCreated(app *framework.App, fn func(ctx context.Context, row *Invoices) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractInvoicesRecord(ev, "invoices")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnInvoicesUpdated subscribes to entity.updated events scoped to "invoices".
func OnInvoicesUpdated(app *framework.App, fn func(ctx context.Context, row *Invoices) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractInvoicesRecord(ev, "invoices")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnInvoicesDeleted subscribes to entity.deleted events scoped to "invoices". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnInvoicesDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "invoices" {
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

// OnPaymentsCreated subscribes to entity.created events scoped to "payments".
// Returns a cancel func; call it to remove the handler.
func OnPaymentsCreated(app *framework.App, fn func(ctx context.Context, row *Payments) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractPaymentsRecord(ev, "payments")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnPaymentsUpdated subscribes to entity.updated events scoped to "payments".
func OnPaymentsUpdated(app *framework.App, fn func(ctx context.Context, row *Payments) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractPaymentsRecord(ev, "payments")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnPaymentsDeleted subscribes to entity.deleted events scoped to "payments". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnPaymentsDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "payments" {
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

// extractPlansRecord unmarshals an event payload's "record" field into a
// *Plans, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractPlansRecord(ev framework.Event, entityName string) (*Plans, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Plans
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}

// extractCustomersRecord unmarshals an event payload's "record" field into a
// *Customers, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractCustomersRecord(ev framework.Event, entityName string) (*Customers, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Customers
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}

// extractSubscriptionsRecord unmarshals an event payload's "record" field into a
// *Subscriptions, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractSubscriptionsRecord(ev framework.Event, entityName string) (*Subscriptions, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Subscriptions
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}

// extractInvoicesRecord unmarshals an event payload's "record" field into a
// *Invoices, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractInvoicesRecord(ev framework.Event, entityName string) (*Invoices, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Invoices
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}

// extractPaymentsRecord unmarshals an event payload's "record" field into a
// *Payments, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractPaymentsRecord(ev framework.Event, entityName string) (*Payments, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Payments
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}
