package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Customers struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Email   string `json:"email,omitempty"`
	Company string `json:"company,omitempty"`
	Status  string `json:"status,omitempty"`
	Mrr     string `json:"mrr,omitempty"`
	UserId  string `json:"userId,omitempty"`
}

// ====== Customers column references ======

var (
	CustomersID      = framework.NewUUIDColumn("id")
	CustomersName    = framework.NewStringColumn("name")
	CustomersEmail   = framework.NewStringColumn("email")
	CustomersCompany = framework.NewStringColumn("company")
	CustomersStatus  = framework.NewStringColumn("status")
	CustomersMrr     = framework.NewFloatColumn("mrr")
	CustomersUserId  = framework.NewStringColumn("user_id")
)

// CustomersRepo is the typed repository for customers rows.
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

// Handler returns the underlying CrudHandler — useful for advanced wiring or
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

// Create persists row and back-fills server-generated fields onto it.
func (r *CustomersRepo) Create(ctx context.Context, row *Customers) error {
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
func (r *CustomersRepo) Get(ctx context.Context, id string, includes ...string) (*Customers, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Customers
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *CustomersRepo) Update(ctx context.Context, id string, row *Customers) error {
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

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *CustomersRepo) FirstOrCreate(ctx context.Context, row *Customers, match framework.Condition) (*Customers, error) {
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
func (r *CustomersRepo) BatchCreate(ctx context.Context, rows []*Customers) ([]*Customers, error) {
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
func (r *CustomersRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Customers) ([]*Customers, error) {
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
func (r *CustomersRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
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

// registerCustomers registers the "customers" entity with app.
func registerCustomers(app *framework.App) {
	app.Entity("customers", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Max: floatPtr(120)},
			{Name: "email", Type: schema.String, Required: true, Unique: true},
			{Name: "company", Type: schema.String, Max: floatPtr(120)},
			{Name: "status", Type: schema.Enum, Default: "trialing", Values: []string{"trialing", "active", "past_due", "canceled"}},
			{Name: "mrr", Type: schema.Decimal, Default: "0", Min: floatPtr(0)},
			{Name: "user_id", Type: schema.String, Hidden: true},
		},
		OwnerField: "user_id",
		CRUD:       boolPtr(true),
		MCP:        true,
		Indices: []framework.Index{
			{Name: "idx_customers_email", Columns: []string{"email"}, Unique: true},
		},
		Properties: map[string]any{"label": "Customers"},
	})
	_ = Customers{}
}

func init() {
	registrars = append(registrars, registrar{order: 1, fn: registerCustomers})
}
