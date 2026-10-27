//gofastr:generated generator=app kind=scaffold version=dev sum=cda3ebd5f986832f

package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Payments struct {
	ID         string              `json:"id"`
	InvoiceId  string              `json:"invoiceId,omitempty"`
	CustomerId string              `json:"customerId,omitempty"`
	Amount     string              `json:"amount,omitempty"`
	Method     PaymentsMethodValue `json:"method,omitempty"`
	Status     PaymentsStatusValue `json:"status,omitempty"`
	UserId     string              `json:"userId,omitempty"`
	Invoice    *Invoices           `json:"invoice,omitempty"`
	Customer   *Customers          `json:"customer,omitempty"`
}

// PaymentsMethodValue is one value of the payments field method.
type PaymentsMethodValue string

const (
	PaymentsMethodCard PaymentsMethodValue = "card"
	PaymentsMethodAch  PaymentsMethodValue = "ach"
	PaymentsMethodWire PaymentsMethodValue = "wire"
)

// PaymentsStatusValue is one value of the payments field status.
type PaymentsStatusValue string

const (
	PaymentsStatusSucceeded PaymentsStatusValue = "succeeded"
	PaymentsStatusFailed    PaymentsStatusValue = "failed"
	PaymentsStatusRefunded  PaymentsStatusValue = "refunded"
)

// PaymentsInput is the create payload for PaymentsRepo. A required field with no
// default is always sent; a nil field takes its default, and new(false)
// stores false.
type PaymentsInput struct {
	InvoiceId  string               `json:"invoiceId"`
	CustomerId string               `json:"customerId"`
	Amount     string               `json:"amount"`
	Method     *PaymentsMethodValue `json:"method,omitempty"`
	Status     *PaymentsStatusValue `json:"status,omitempty"`
	UserId     *string              `json:"userId,omitempty"`
}

// PaymentsPatch is the update payload for PaymentsRepo: a nil field is left as
// stored, and a pointer to false, 0 or "" writes it to a bool, int,
// float, string or text field. "" for any other type is a blank: it
// clears an optional field and fails validation on a required one.
type PaymentsPatch struct {
	InvoiceId  *string              `json:"invoiceId,omitempty"`
	CustomerId *string              `json:"customerId,omitempty"`
	Amount     *string              `json:"amount,omitempty"`
	Method     *PaymentsMethodValue `json:"method,omitempty"`
	Status     *PaymentsStatusValue `json:"status,omitempty"`
}

// ====== Payments column references ======

var (
	PaymentsID         = framework.NewUUIDColumn("id")
	PaymentsInvoiceId  = framework.NewUUIDColumn("invoice_id")
	PaymentsCustomerId = framework.NewUUIDColumn("customer_id")
	PaymentsAmount     = framework.NewFloatColumn("amount")
	PaymentsMethod     = framework.NewEnumColumn[PaymentsMethodValue]("method")
	PaymentsStatus     = framework.NewEnumColumn[PaymentsStatusValue]("status")
	PaymentsUserId     = framework.NewStringColumn("user_id")
)

// Payments include names: pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	PaymentsInclInvoice  = "invoice"
	PaymentsInclCustomer = "customer"
)

// PaymentsRepo is the typed repository for payments rows.
// Event helpers: OnPaymentsCreated/OnPaymentsUpdated/OnPaymentsDeleted in this package.
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

// Handler returns the underlying CrudHandler: useful for advanced wiring or
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

// row decodes one CRUD result.
func (r *PaymentsRepo) row(out map[string]any) (*Payments, error) {
	var row Payments
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// rows decodes a batch result, keeping its order.
func (r *PaymentsRepo) rows(outs []map[string]any) ([]*Payments, error) {
	rows := make([]*Payments, len(outs))
	for i, out := range outs {
		row, err := r.row(out)
		if err != nil {
			return nil, err
		}
		rows[i] = row
	}
	return rows, nil
}

// Create persists in and returns the stored row, server-generated fields
// included.
func (r *PaymentsRepo) Create(ctx context.Context, in PaymentsInput) (*Payments, error) {
	body, err := framework.MarshalEntity(in)
	if err != nil {
		return nil, err
	}
	out, err := r.handler.CreateOne(ctx, body)
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// Get fetches a row by id with optional eager-loaded includes.
func (r *PaymentsRepo) Get(ctx context.Context, id string, includes ...string) (*Payments, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// Update writes patch's non-nil fields onto the row with this id and
// returns the row as stored afterwards.
func (r *PaymentsRepo) Update(ctx context.Context, id string, patch PaymentsPatch) (*Payments, error) {
	body, err := framework.MarshalEntity(patch)
	if err != nil {
		return nil, err
	}
	out, err := r.handler.UpdateOne(ctx, id, body)
	if err != nil {
		return nil, err
	}
	return r.row(out)
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

// FirstOrCreate returns the first row matching match, or creates in and
// returns the stored row when none does.
func (r *PaymentsRepo) FirstOrCreate(ctx context.Context, in PaymentsInput, match framework.Condition) (*Payments, error) {
	existing, err := r.Query().Where(match).First(ctx)
	if err == nil {
		return existing, nil
	}
	if !framework.IsNotFound(err) {
		return nil, err
	}
	return r.Create(ctx, in)
}

// BatchCreate inserts every input in one transaction; on any per-item error
// the entire batch rolls back. Returned rows are in input order.
func (r *PaymentsRepo) BatchCreate(ctx context.Context, ins []PaymentsInput) ([]*Payments, error) {
	bodies := make([]map[string]any, len(ins))
	for i, in := range ins {
		b, err := framework.MarshalEntity(in)
		if err != nil {
			return nil, err
		}
		bodies[i] = b
	}
	results, err := r.handler.BatchCreateMany(ctx, bodies)
	if err != nil {
		return nil, err
	}
	return r.rows(results)
}

// BatchUpdate writes patches[i] onto the row ids[i], all in one transaction.
func (r *PaymentsRepo) BatchUpdate(ctx context.Context, ids []string, patches []PaymentsPatch) ([]*Payments, error) {
	bodies := make([]map[string]any, len(patches))
	for i, patch := range patches {
		b, err := framework.MarshalEntity(patch)
		if err != nil {
			return nil, err
		}
		bodies[i] = b
	}
	results, err := r.handler.BatchUpdateMany(ctx, ids, bodies)
	if err != nil {
		return nil, err
	}
	return r.rows(results)
}

// BatchDelete deletes every id atomically.
func (r *PaymentsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnPaymentsCreated subscribes to entity.created events scoped to "payments".
// Returns a cancel func; call it to remove the handler.
func OnPaymentsCreated(app *framework.App, fn func(ctx context.Context, row *Payments) error) func() {
	return onEntityEvent[Payments](app, "payments", framework.EntityCreated, fn)
}

// OnPaymentsUpdated subscribes to entity.updated events scoped to "payments".
func OnPaymentsUpdated(app *framework.App, fn func(ctx context.Context, row *Payments) error) func() {
	return onEntityEvent[Payments](app, "payments", framework.EntityUpdated, fn)
}

// OnPaymentsDeleted subscribes to entity.deleted events scoped to "payments". Callback
// receives the deleted row's id only: by the time the event fires the row
// has been removed (or soft-deleted).
func OnPaymentsDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return onEntityDeleted(app, "payments", fn)
}

// extractPaymentsRecord unmarshals an event payload's "record" field into a
// *Payments, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractPaymentsRecord(ev framework.Event, entityName string) (*Payments, bool) {
	return extractEntityRecord[Payments](ev, entityName)
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
			{Type: framework.RelManyToOne, Name: "invoice", Entity: "invoices", ForeignKey: []string{"invoice_id"}},
			{Type: framework.RelManyToOne, Name: "customer", Entity: "customers", ForeignKey: []string{"customer_id"}},
		},
		Scope: &framework.ScopeConfig{
			SoftDelete: true,
			OwnerField: "user_id",
		},
		Exposure: &framework.ExposureConfig{
			CRUD: boolPtr(true),
			MCP:  true,
		},
		Display:    &framework.DisplayConfig{TitleFields: []string{"invoice_id", "customer_id"}, Description: "Money in, and the attempts that failed.", Nav: &framework.EntityNav{Group: "billing", Icon: "card", Order: 4}, Views: []framework.ListView{{Key: "failed", Label: "Failed", Where: "status = \"failed\""}}, Facets: []string{"status", "method"}, Fields: map[string]framework.FieldDisplay{"amount": {Input: "money"}}},
		Properties: map[string]any{"label": "Payments"},
	})
	_ = Payments{}
}

func init() {
	registrars = append(registrars, registrar{order: 4, fn: registerPayments})
}
