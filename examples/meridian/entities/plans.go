package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Plans struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Slug     string `json:"slug,omitempty"`
	Price    string `json:"price,omitempty"`
	Interval string `json:"interval,omitempty"`
	Active   bool   `json:"active,omitempty"`
}

// ====== Plans column references ======

var (
	PlansID       = framework.NewUUIDColumn("id")
	PlansName     = framework.NewStringColumn("name")
	PlansSlug     = framework.NewStringColumn("slug")
	PlansPrice    = framework.NewFloatColumn("price")
	PlansInterval = framework.NewStringColumn("interval")
	PlansActive   = framework.NewBoolColumn("active")
)

// PlansRepo is the typed repository for plans rows.
type PlansRepo struct {
	handler *framework.CrudHandler
}

// NewPlansRepo wires a typed repo against the App's "plans" entity. Panics if the
// entity hasn't been registered yet.
func NewPlansRepo(app *framework.App) *PlansRepo {
	entity, err := app.Registry.Get("plans")
	if err != nil {
		panic("entities: plans not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("plans")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &PlansRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *PlansRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *PlansRepo) WithTx(tx *sql.Tx) *PlansRepo {
	h := *r.handler
	h.DB = tx
	return &PlansRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *PlansRepo) Create(ctx context.Context, row *Plans) error {
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
func (r *PlansRepo) Get(ctx context.Context, id string, includes ...string) (*Plans, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Plans
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *PlansRepo) Update(ctx context.Context, id string, row *Plans) error {
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
func (r *PlansRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *PlansRepo) Query() *framework.TypedQuery[Plans] {
	return framework.NewTypedQuery[Plans](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *PlansRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(PlansID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *PlansRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *PlansRepo) FirstOrCreate(ctx context.Context, row *Plans, match framework.Condition) (*Plans, error) {
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
func (r *PlansRepo) BatchCreate(ctx context.Context, rows []*Plans) ([]*Plans, error) {
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
func (r *PlansRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Plans) ([]*Plans, error) {
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
func (r *PlansRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

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

// registerPlans registers the "plans" entity with app.
func registerPlans(app *framework.App) {
	app.Entity("plans", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Max: floatPtr(80)},
			{Name: "slug", Type: schema.String, Required: true, Unique: true, Max: floatPtr(80)},
			{Name: "price", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "interval", Type: schema.Enum, Default: "month", Values: []string{"month", "year"}},
			{Name: "active", Type: schema.Bool, Default: true},
		},
		CRUD:       boolPtr(true),
		MCP:        true,
		Properties: map[string]any{"label": "Plans"},
	})
	_ = Plans{}
}

func init() {
	registrars = append(registrars, registrar{order: 0, fn: registerPlans})
}
