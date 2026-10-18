package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Customers struct {
	ID      string               `json:"id"`
	Name    string               `json:"name,omitempty"`
	Email   string               `json:"email,omitempty"`
	Company string               `json:"company,omitempty"`
	Status  CustomersStatusValue `json:"status,omitempty"`
	Mrr     string               `json:"mrr,omitempty"`
	Logo    string               `json:"logo,omitempty"`
	UserId  string               `json:"userId,omitempty"`
}

// CustomersStatusValue is one value of the customers field status.
type CustomersStatusValue string

const (
	CustomersStatusTrialing CustomersStatusValue = "trialing"
	CustomersStatusActive   CustomersStatusValue = "active"
	CustomersStatusPastDue  CustomersStatusValue = "past_due"
	CustomersStatusCanceled CustomersStatusValue = "canceled"
)

// CustomersInput is the create payload for CustomersRepo. A required field with no
// default is always sent; a nil field takes its default, and new(false)
// stores false.
type CustomersInput struct {
	Name    string                `json:"name"`
	Email   string                `json:"email"`
	Company *string               `json:"company,omitempty"`
	Status  *CustomersStatusValue `json:"status,omitempty"`
	Mrr     *string               `json:"mrr,omitempty"`
	Logo    *string               `json:"logo,omitempty"`
	UserId  *string               `json:"userId,omitempty"`
}

// CustomersPatch is the update payload for CustomersRepo: a nil field is left as
// stored, and a pointer to false, 0 or "" writes it to a bool, int,
// float, string or text field. "" for any other type is not a value:
// absent on an optional field, a validation error on a required one.
type CustomersPatch struct {
	Name    *string               `json:"name,omitempty"`
	Email   *string               `json:"email,omitempty"`
	Company *string               `json:"company,omitempty"`
	Status  *CustomersStatusValue `json:"status,omitempty"`
	Mrr     *string               `json:"mrr,omitempty"`
	Logo    *string               `json:"logo,omitempty"`
}

// ====== Customers column references ======

var (
	CustomersID      = framework.NewUUIDColumn("id")
	CustomersName    = framework.NewStringColumn("name")
	CustomersEmail   = framework.NewStringColumn("email")
	CustomersCompany = framework.NewStringColumn("company")
	CustomersStatus  = framework.NewEnumColumn[CustomersStatusValue]("status")
	CustomersMrr     = framework.NewFloatColumn("mrr")
	CustomersLogo    = framework.NewStringColumn("logo")
	CustomersUserId  = framework.NewStringColumn("user_id")
)

// CustomersRepo is the typed repository for customers rows.
// Event helpers: OnCustomersCreated/OnCustomersUpdated/OnCustomersDeleted in this package.
type CustomersRepo struct {
	handler *framework.CrudHandler
}

// NewCustomersRepo wires a typed repo against the App's "customers" entity. Panics if the
// entity hasn't been registered yet.
func NewCustomersRepo(app *framework.App) *CustomersRepo {
	entity, err := app.Registry.Get("customers")
	if err != nil {
		panic("entities: customers not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("customers")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &CustomersRepo{handler: h}
}

// Handler returns the underlying CrudHandler: useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *CustomersRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *CustomersRepo) WithTx(tx *sql.Tx) *CustomersRepo {
	h := *r.handler
	h.DB = tx
	return &CustomersRepo{handler: &h}
}

// row decodes one CRUD result.
func (r *CustomersRepo) row(out map[string]any) (*Customers, error) {
	var row Customers
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// rows decodes a batch result, keeping its order.
func (r *CustomersRepo) rows(outs []map[string]any) ([]*Customers, error) {
	rows := make([]*Customers, len(outs))
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
func (r *CustomersRepo) Create(ctx context.Context, in CustomersInput) (*Customers, error) {
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
func (r *CustomersRepo) Get(ctx context.Context, id string, includes ...string) (*Customers, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// Update writes patch's non-nil fields onto the row with this id and
// returns the row as stored afterwards.
func (r *CustomersRepo) Update(ctx context.Context, id string, patch CustomersPatch) (*Customers, error) {
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
func (r *CustomersRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *CustomersRepo) Query() *framework.TypedQuery[Customers] {
	return framework.NewTypedQuery[Customers](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *CustomersRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(CustomersID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *CustomersRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate returns the first row matching match, or creates in and
// returns the stored row when none does.
func (r *CustomersRepo) FirstOrCreate(ctx context.Context, in CustomersInput, match framework.Condition) (*Customers, error) {
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
func (r *CustomersRepo) BatchCreate(ctx context.Context, ins []CustomersInput) ([]*Customers, error) {
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
func (r *CustomersRepo) BatchUpdate(ctx context.Context, ids []string, patches []CustomersPatch) ([]*Customers, error) {
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
func (r *CustomersRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnCustomersCreated subscribes to entity.created events scoped to "customers".
// Returns a cancel func; call it to remove the handler.
func OnCustomersCreated(app *framework.App, fn func(ctx context.Context, row *Customers) error) func() {
	return onEntityEvent[Customers](app, "customers", framework.EntityCreated, fn)
}

// OnCustomersUpdated subscribes to entity.updated events scoped to "customers".
func OnCustomersUpdated(app *framework.App, fn func(ctx context.Context, row *Customers) error) func() {
	return onEntityEvent[Customers](app, "customers", framework.EntityUpdated, fn)
}

// OnCustomersDeleted subscribes to entity.deleted events scoped to "customers". Callback
// receives the deleted row's id only: by the time the event fires the row
// has been removed (or soft-deleted).
func OnCustomersDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return onEntityDeleted(app, "customers", fn)
}

// extractCustomersRecord unmarshals an event payload's "record" field into a
// *Customers, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractCustomersRecord(ev framework.Event, entityName string) (*Customers, bool) {
	return extractEntityRecord[Customers](ev, entityName)
}

// registerCustomers registers the "customers" entity with app.
func registerCustomers(app *framework.App) {
	app.Entity("customers", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Max: floatPtr(120)},
			{Name: "email", Type: schema.String, Required: true},
			{Name: "company", Type: schema.String, Max: floatPtr(120)},
			{Name: "status", Type: schema.Enum, Default: "trialing", Values: []string{"trialing", "active", "past_due", "canceled"}},
			{Name: "mrr", Type: schema.Decimal, Default: "0", Min: floatPtr(0)},
			{Name: "logo", Type: schema.Image},
			{Name: "user_id", Type: schema.String, Hidden: true},
		},
		Scope: &framework.ScopeConfig{
			OwnerField: "user_id",
		},
		SearchFields: []string{"name"},
		Exposure: &framework.ExposureConfig{
			CRUD: boolPtr(true),
			MCP:  true,
		},
		Indices: []framework.Index{
			{Name: "idx_customers_owner_email", Columns: []string{"user_id", "email"}, Unique: true},
		},
		Display:    &framework.DisplayConfig{TitleFields: []string{"name"}, Description: "Every account that pays you.", Columns: []string{"name", "email", "company", "status", "mrr"}, Nav: &framework.EntityNav{Group: "billing", Icon: "users", Order: 1}, Views: []framework.ListView{{Key: "active", Label: "Active", Where: "status = \"active\""}, {Key: "past_due", Label: "Past due", Where: "status = \"past_due\""}}, Facets: []string{"status"}, Fields: map[string]framework.FieldDisplay{"email": {Input: "email"}, "mrr": {Label: "MRR", Input: "money"}}},
		Properties: map[string]any{"label": "Customers"},
	})
	_ = Customers{}
}

func init() {
	registrars = append(registrars, registrar{order: 1, fn: registerCustomers})
}
