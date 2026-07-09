package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Payments struct {
	ID         string     `json:"id"`
	InvoiceId  string     `json:"invoiceId,omitempty"`
	CustomerId string     `json:"customerId,omitempty"`
	Amount     string     `json:"amount,omitempty"`
	Method     string     `json:"method,omitempty"`
	Status     string     `json:"status,omitempty"`
	UserId     string     `json:"userId,omitempty"`
	Invoice    *Invoices  `json:"invoice,omitempty"`
	Customer   *Customers `json:"customer,omitempty"`
}

// ====== Payments column references ======

var (
	PaymentsID         = framework.NewUUIDColumn("id")
	PaymentsInvoiceId  = framework.NewUUIDColumn("invoice_id")
	PaymentsCustomerId = framework.NewUUIDColumn("customer_id")
	PaymentsAmount     = framework.NewFloatColumn("amount")
	PaymentsMethod     = framework.NewStringColumn("method")
	PaymentsStatus     = framework.NewStringColumn("status")
	PaymentsUserId     = framework.NewStringColumn("user_id")
)

// Payments include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	PaymentsInclInvoice  = "invoice"
	PaymentsInclCustomer = "customer"
)

// PaymentsRepo is the typed repository for payments rows.
type PaymentsRepo struct {
	handler *framework.CrudHandler
}

// NewPaymentsRepo wires a typed repo against the App's "payments" entity. Panics if the
// entity hasn't been registered yet.
func NewPaymentsRepo(app *framework.App) *PaymentsRepo {
	entity, err := app.Registry.Get("payments")
	if err != nil {
		panic("entities: payments not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("payments")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &PaymentsRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *PaymentsRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *PaymentsRepo) WithTx(tx *sql.Tx) *PaymentsRepo {
	h := *r.handler
	h.DB = tx
	return &PaymentsRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *PaymentsRepo) Create(ctx context.Context, row *Payments) error {
	body, err := framework.MarshalEntity(row)
	if err != nil {
		return err
	}
	out, err := r.handler.CreateOne(ctx, body)
	if err != nil {
		return err
	}
	return framework.UnmarshalEntity(out, row)
}

// Get fetches a row by id with optional eager-loaded includes.
func (r *PaymentsRepo) Get(ctx context.Context, id string, includes ...string) (*Payments, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Payments
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *PaymentsRepo) Update(ctx context.Context, id string, row *Payments) error {
	body, err := framework.MarshalEntity(row)
	if err != nil {
		return err
	}
	delete(body, "id") // id is taken from the path arg, not the body
	out, err := r.handler.UpdateOne(ctx, id, body)
	if err != nil {
		return err
	}
	return framework.UnmarshalEntity(out, row)
}

// Delete removes the row by id (or soft-deletes if SoftDelete is enabled on
// the entity).
func (r *PaymentsRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *PaymentsRepo) Query() *framework.TypedQuery[Payments] {
	return framework.NewTypedQuery[Payments](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *PaymentsRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(PaymentsID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *PaymentsRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *PaymentsRepo) FirstOrCreate(ctx context.Context, row *Payments, match framework.Condition) (*Payments, error) {
	existing, err := r.Query().Where(match).First(ctx)
	if err == nil {
		return existing, nil
	}
	if !framework.IsNotFound(err) {
		return nil, err
	}
	if err := r.Create(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// BatchCreate inserts every row in one transaction; on any per-item error
// the entire batch rolls back. Returned slice is in input order.
func (r *PaymentsRepo) BatchCreate(ctx context.Context, rows []*Payments) ([]*Payments, error) {
	bodies := make([]map[string]any, len(rows))
	for i, row := range rows {
		b, err := framework.MarshalEntity(row)
		if err != nil {
			return nil, err
		}
		bodies[i] = b
	}
	results, err := r.handler.BatchCreateMany(ctx, bodies)
	if err != nil {
		return nil, err
	}
	for i, res := range results {
		if err := framework.UnmarshalEntity(res, rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// BatchUpdate updates every row by its id in one transaction.
func (r *PaymentsRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Payments) ([]*Payments, error) {
	bodies := make([]map[string]any, len(rows))
	for i, row := range rows {
		b, err := framework.MarshalEntity(row)
		if err != nil {
			return nil, err
		}
		delete(b, "id")
		bodies[i] = b
	}
	results, err := r.handler.BatchUpdateMany(ctx, ids, bodies)
	if err != nil {
		return nil, err
	}
	for i, res := range results {
		if err := framework.UnmarshalEntity(res, rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// BatchDelete deletes every id atomically.
func (r *PaymentsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
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

// registerPayments registers the "payments" entity with app.
func registerPayments(app *framework.App) {
	app.Entity("payments", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "invoice_id", Type: schema.Relation, Required: true, To: "invoices"},
			{Name: "customer_id", Type: schema.Relation, Required: true, To: "customers"},
			{Name: "amount", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "method", Type: schema.Enum, Default: "card", Values: []string{"card", "ach", "wire"}},
			{Name: "status", Type: schema.Enum, Default: "succeeded", Values: []string{"succeeded", "failed", "refunded"}},
			{Name: "user_id", Type: schema.String, Hidden: true},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "invoice", Entity: "invoices", ForeignKey: "invoice_id"},
			{Type: framework.RelManyToOne, Name: "customer", Entity: "customers", ForeignKey: "customer_id"},
		},
		OwnerField: "user_id",
		CRUD:       boolPtr(true),
		MCP:        true,
		Properties: map[string]any{"label": "Payments"},
	})
	_ = Payments{}
}

func init() {
	registrars = append(registrars, registrar{order: 4, fn: registerPayments})
}
