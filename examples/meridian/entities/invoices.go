package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Invoices struct {
	ID         string     `json:"id"`
	CustomerId string     `json:"customerId,omitempty"`
	Number     string     `json:"number,omitempty"`
	Amount     string     `json:"amount,omitempty"`
	Status     string     `json:"status,omitempty"`
	IssuedOn   string     `json:"issuedOn,omitempty"`
	DueOn      string     `json:"dueOn,omitempty"`
	PaidOn     string     `json:"paidOn,omitempty"`
	UserId     string     `json:"userId,omitempty"`
	Customer   *Customers `json:"customer,omitempty"`
}

// ====== Invoices column references ======

var (
	InvoicesID         = framework.NewUUIDColumn("id")
	InvoicesCustomerId = framework.NewUUIDColumn("customer_id")
	InvoicesNumber     = framework.NewStringColumn("number")
	InvoicesAmount     = framework.NewFloatColumn("amount")
	InvoicesStatus     = framework.NewStringColumn("status")
	InvoicesIssuedOn   = framework.NewTimestampColumn("issued_on")
	InvoicesDueOn      = framework.NewTimestampColumn("due_on")
	InvoicesPaidOn     = framework.NewTimestampColumn("paid_on")
	InvoicesUserId     = framework.NewStringColumn("user_id")
)

// Invoices include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	InvoicesInclCustomer = "customer"
)

// InvoicesRepo is the typed repository for invoices rows.
type InvoicesRepo struct {
	handler *framework.CrudHandler
}

// NewInvoicesRepo wires a typed repo against the App's "invoices" entity. Panics if the
// entity hasn't been registered yet.
func NewInvoicesRepo(app *framework.App) *InvoicesRepo {
	entity, err := app.Registry.Get("invoices")
	if err != nil {
		panic("entities: invoices not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("invoices")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &InvoicesRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *InvoicesRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *InvoicesRepo) WithTx(tx *sql.Tx) *InvoicesRepo {
	h := *r.handler
	h.DB = tx
	return &InvoicesRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *InvoicesRepo) Create(ctx context.Context, row *Invoices) error {
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
func (r *InvoicesRepo) Get(ctx context.Context, id string, includes ...string) (*Invoices, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Invoices
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *InvoicesRepo) Update(ctx context.Context, id string, row *Invoices) error {
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
func (r *InvoicesRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *InvoicesRepo) Query() *framework.TypedQuery[Invoices] {
	return framework.NewTypedQuery[Invoices](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *InvoicesRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(InvoicesID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *InvoicesRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *InvoicesRepo) FirstOrCreate(ctx context.Context, row *Invoices, match framework.Condition) (*Invoices, error) {
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
func (r *InvoicesRepo) BatchCreate(ctx context.Context, rows []*Invoices) ([]*Invoices, error) {
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
func (r *InvoicesRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Invoices) ([]*Invoices, error) {
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
func (r *InvoicesRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
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

// registerInvoices registers the "invoices" entity with app.
func registerInvoices(app *framework.App) {
	app.Entity("invoices", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "customer_id", Type: schema.Relation, Required: true, To: "customers"},
			{Name: "number", Type: schema.String, Required: true, Unique: true},
			{Name: "amount", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "status", Type: schema.Enum, Default: "draft", Values: []string{"draft", "open", "paid", "past_due", "void"}},
			{Name: "issued_on", Type: schema.Date},
			{Name: "due_on", Type: schema.Date},
			{Name: "paid_on", Type: schema.Date},
			{Name: "user_id", Type: schema.String, Hidden: true},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "customer", Entity: "customers", ForeignKey: "customer_id"},
		},
		OwnerField: "user_id",
		CRUD:       boolPtr(true),
		MCP:        true,
		Properties: map[string]any{"label": "Invoices"},
	})
	_ = Invoices{}
}

func init() {
	registrars = append(registrars, registrar{order: 3, fn: registerInvoices})
}
