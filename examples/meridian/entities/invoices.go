//gofastr:generated generator=app kind=scaffold version=dev sum=95f14324c949a4b4

package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Invoices struct {
	ID         string              `json:"id"`
	CustomerId string              `json:"customerId,omitempty"`
	Number     string              `json:"number,omitempty"`
	Amount     string              `json:"amount,omitempty"`
	Status     InvoicesStatusValue `json:"status,omitempty"`
	IssuedOn   string              `json:"issuedOn,omitempty"`
	DueOn      string              `json:"dueOn,omitempty"`
	PaidOn     string              `json:"paidOn,omitempty"`
	UserId     string              `json:"userId,omitempty"`
	Customer   *Customers          `json:"customer,omitempty"`
}

// InvoicesStatusValue is one value of the invoices field status.
type InvoicesStatusValue string

const (
	InvoicesStatusDraft   InvoicesStatusValue = "draft"
	InvoicesStatusOpen    InvoicesStatusValue = "open"
	InvoicesStatusPaid    InvoicesStatusValue = "paid"
	InvoicesStatusPastDue InvoicesStatusValue = "past_due"
	InvoicesStatusVoid    InvoicesStatusValue = "void"
)

// InvoicesMove names one of the invoices moves; pass it to InvoicesRepo.Transition.
type InvoicesMove string

const (
	InvoicesMoveMarkPaid InvoicesMove = "mark_paid"
	InvoicesMoveVoid     InvoicesMove = "void"
)

// InvoicesInput is the create payload for InvoicesRepo. A required field with no
// default is always sent; a nil field takes its default, and new(false)
// stores false.
type InvoicesInput struct {
	CustomerId string               `json:"customerId"`
	Number     string               `json:"number"`
	Amount     string               `json:"amount"`
	Status     *InvoicesStatusValue `json:"status,omitempty"`
	IssuedOn   *string              `json:"issuedOn,omitempty"`
	DueOn      *string              `json:"dueOn,omitempty"`
	PaidOn     *string              `json:"paidOn,omitempty"`
	UserId     *string              `json:"userId,omitempty"`
}

// InvoicesPatch is the update payload for InvoicesRepo: a nil field is left as
// stored, and a pointer to false, 0 or "" writes it to a bool, int,
// float, string or text field. "" for any other type is not a value:
// absent on an optional field, a validation error on a required one.
type InvoicesPatch struct {
	CustomerId *string              `json:"customerId,omitempty"`
	Number     *string              `json:"number,omitempty"`
	Amount     *string              `json:"amount,omitempty"`
	Status     *InvoicesStatusValue `json:"status,omitempty"`
	IssuedOn   *string              `json:"issuedOn,omitempty"`
	DueOn      *string              `json:"dueOn,omitempty"`
	PaidOn     *string              `json:"paidOn,omitempty"`
}

// ====== Invoices column references ======

var (
	InvoicesID         = framework.NewUUIDColumn("id")
	InvoicesCustomerId = framework.NewUUIDColumn("customer_id")
	InvoicesNumber     = framework.NewStringColumn("number")
	InvoicesAmount     = framework.NewFloatColumn("amount")
	InvoicesStatus     = framework.NewEnumColumn[InvoicesStatusValue]("status")
	InvoicesIssuedOn   = framework.NewTimestampColumn("issued_on")
	InvoicesDueOn      = framework.NewTimestampColumn("due_on")
	InvoicesPaidOn     = framework.NewTimestampColumn("paid_on")
	InvoicesUserId     = framework.NewStringColumn("user_id")
)

// Invoices include names: pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	InvoicesInclCustomer = "customer"
)

// InvoicesRepo is the typed repository for invoices rows.
// Event helpers: OnInvoicesCreated/OnInvoicesUpdated/OnInvoicesDeleted in this package.
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

// Handler returns the underlying CrudHandler: useful for advanced wiring or
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

// row decodes one CRUD result.
func (r *InvoicesRepo) row(out map[string]any) (*Invoices, error) {
	var row Invoices
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// rows decodes a batch result, keeping its order.
func (r *InvoicesRepo) rows(outs []map[string]any) ([]*Invoices, error) {
	rows := make([]*Invoices, len(outs))
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
func (r *InvoicesRepo) Create(ctx context.Context, in InvoicesInput) (*Invoices, error) {
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
func (r *InvoicesRepo) Get(ctx context.Context, id string, includes ...string) (*Invoices, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// Update writes patch's non-nil fields onto the row with this id and
// returns the row as stored afterwards.
func (r *InvoicesRepo) Update(ctx context.Context, id string, patch InvoicesPatch) (*Invoices, error) {
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

// FirstOrCreate returns the first row matching match, or creates in and
// returns the stored row when none does.
func (r *InvoicesRepo) FirstOrCreate(ctx context.Context, in InvoicesInput, match framework.Condition) (*Invoices, error) {
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
func (r *InvoicesRepo) BatchCreate(ctx context.Context, ins []InvoicesInput) ([]*Invoices, error) {
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
func (r *InvoicesRepo) BatchUpdate(ctx context.Context, ids []string, patches []InvoicesPatch) ([]*Invoices, error) {
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
func (r *InvoicesRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// Transition runs move on the row with this id and returns the row as
// stored afterwards: the one way an enforced state field changes. It runs
// as CrudHandler.RunTransition does, so a System move skips the permission
// check and a row outside the move's From answers a conflict.
func (r *InvoicesRepo) Transition(ctx context.Context, id string, move InvoicesMove) (*Invoices, error) {
	out, err := r.handler.RunTransition(ctx, id, string(move))
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// OnInvoicesCreated subscribes to entity.created events scoped to "invoices".
// Returns a cancel func; call it to remove the handler.
func OnInvoicesCreated(app *framework.App, fn func(ctx context.Context, row *Invoices) error) func() {
	return onEntityEvent[Invoices](app, "invoices", framework.EntityCreated, fn)
}

// OnInvoicesUpdated subscribes to entity.updated events scoped to "invoices".
func OnInvoicesUpdated(app *framework.App, fn func(ctx context.Context, row *Invoices) error) func() {
	return onEntityEvent[Invoices](app, "invoices", framework.EntityUpdated, fn)
}

// OnInvoicesDeleted subscribes to entity.deleted events scoped to "invoices". Callback
// receives the deleted row's id only: by the time the event fires the row
// has been removed (or soft-deleted).
func OnInvoicesDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return onEntityDeleted(app, "invoices", fn)
}

// extractInvoicesRecord unmarshals an event payload's "record" field into a
// *Invoices, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractInvoicesRecord(ev framework.Event, entityName string) (*Invoices, bool) {
	return extractEntityRecord[Invoices](ev, entityName)
}

// registerInvoices registers the "invoices" entity with app.
func registerInvoices(app *framework.App) {
	app.Entity("invoices", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "customer_id", Type: schema.Relation, Required: true, To: "customers"},
			{Name: "number", Type: schema.String, Required: true},
			{Name: "amount", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "status", Type: schema.Enum, Default: "draft", Values: []string{"draft", "open", "paid", "past_due", "void"}},
			{Name: "issued_on", Type: schema.Date},
			{Name: "due_on", Type: schema.Date},
			{Name: "paid_on", Type: schema.Date},
			{Name: "user_id", Type: schema.String, Hidden: true},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "customer", Entity: "customers", ForeignKey: []string{"customer_id"}},
		},
		Scope: &framework.ScopeConfig{
			OwnerField: "user_id",
		},
		SearchFields: []string{"number"},
		Exposure: &framework.ExposureConfig{
			CRUD: boolPtr(true),
			MCP:  true,
		},
		Indices: []framework.Index{
			{Name: "idx_invoices_owner_number", Columns: []string{"user_id", "number"}, Unique: true},
		},
		States:     &framework.StatesConfig{Field: "status", Transitions: []framework.Transition{{Key: "mark_paid", Label: "Mark paid", From: []string{"draft", "open", "past_due"}, To: "paid", Stamp: "paid_on", Variant: "primary"}, {Key: "void", Label: "Void", From: []string{"draft", "open", "past_due"}, To: "void", Variant: "danger"}}, Advisory: true},
		Display:    &framework.DisplayConfig{TitleFields: []string{"number"}, Description: "Every invoice issued, paid or not.", Columns: []string{"number", "customer_id", "amount", "status", "issued_on", "due_on"}, Nav: &framework.EntityNav{Group: "billing", Icon: "receipt", Order: 3}, Views: []framework.ListView{{Key: "past_due", Label: "Past due", Where: "status = \"past_due\"", Sort: "due_on ASC"}, {Key: "open", Label: "Open", Where: "status = \"open\"", Sort: "due_on ASC"}}, Facets: []string{"status", "customer_id"}, Fields: map[string]framework.FieldDisplay{"amount": {Input: "money"}}},
		Properties: map[string]any{"label": "Invoices"},
	})
	_ = Invoices{}
}

func init() {
	registrars = append(registrars, registrar{order: 3, fn: registerInvoices})
}
