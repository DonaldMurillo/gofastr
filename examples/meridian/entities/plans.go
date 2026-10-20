//gofastr:generated generator=app kind=scaffold version=dev sum=f92db0cebb6cbfd1

package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Plans struct {
	ID       string             `json:"id"`
	Name     string             `json:"name,omitempty"`
	Slug     string             `json:"slug,omitempty"`
	Price    string             `json:"price,omitempty"`
	Interval PlansIntervalValue `json:"interval,omitempty"`
	Active   bool               `json:"active,omitempty"`
}

// PlansIntervalValue is one value of the plans field interval.
type PlansIntervalValue string

const (
	PlansIntervalMonth PlansIntervalValue = "month"
	PlansIntervalYear  PlansIntervalValue = "year"
)

// PlansInput is the create payload for PlansRepo. A required field with no
// default is always sent; a nil field takes its default, and new(false)
// stores false.
type PlansInput struct {
	Name     string              `json:"name"`
	Slug     string              `json:"slug"`
	Price    string              `json:"price"`
	Interval *PlansIntervalValue `json:"interval,omitempty"`
	Active   *bool               `json:"active,omitempty"`
}

// PlansPatch is the update payload for PlansRepo: a nil field is left as
// stored, and a pointer to false, 0 or "" writes it to a bool, int,
// float, string or text field. "" for any other type is not a value:
// absent on an optional field, a validation error on a required one.
type PlansPatch struct {
	Name     *string             `json:"name,omitempty"`
	Slug     *string             `json:"slug,omitempty"`
	Price    *string             `json:"price,omitempty"`
	Interval *PlansIntervalValue `json:"interval,omitempty"`
	Active   *bool               `json:"active,omitempty"`
}

// ====== Plans column references ======

var (
	PlansID       = framework.NewUUIDColumn("id")
	PlansName     = framework.NewStringColumn("name")
	PlansSlug     = framework.NewStringColumn("slug")
	PlansPrice    = framework.NewFloatColumn("price")
	PlansInterval = framework.NewEnumColumn[PlansIntervalValue]("interval")
	PlansActive   = framework.NewBoolColumn("active")
)

// PlansRepo is the typed repository for plans rows.
// Event helpers: OnPlansCreated/OnPlansUpdated/OnPlansDeleted in this package.
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

// Handler returns the underlying CrudHandler: useful for advanced wiring or
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

// row decodes one CRUD result.
func (r *PlansRepo) row(out map[string]any) (*Plans, error) {
	var row Plans
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// rows decodes a batch result, keeping its order.
func (r *PlansRepo) rows(outs []map[string]any) ([]*Plans, error) {
	rows := make([]*Plans, len(outs))
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
func (r *PlansRepo) Create(ctx context.Context, in PlansInput) (*Plans, error) {
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
func (r *PlansRepo) Get(ctx context.Context, id string, includes ...string) (*Plans, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// Update writes patch's non-nil fields onto the row with this id and
// returns the row as stored afterwards.
func (r *PlansRepo) Update(ctx context.Context, id string, patch PlansPatch) (*Plans, error) {
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

// FirstOrCreate returns the first row matching match, or creates in and
// returns the stored row when none does.
func (r *PlansRepo) FirstOrCreate(ctx context.Context, in PlansInput, match framework.Condition) (*Plans, error) {
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
func (r *PlansRepo) BatchCreate(ctx context.Context, ins []PlansInput) ([]*Plans, error) {
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
func (r *PlansRepo) BatchUpdate(ctx context.Context, ids []string, patches []PlansPatch) ([]*Plans, error) {
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
func (r *PlansRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnPlansCreated subscribes to entity.created events scoped to "plans".
// Returns a cancel func; call it to remove the handler.
func OnPlansCreated(app *framework.App, fn func(ctx context.Context, row *Plans) error) func() {
	return onEntityEvent[Plans](app, "plans", framework.EntityCreated, fn)
}

// OnPlansUpdated subscribes to entity.updated events scoped to "plans".
func OnPlansUpdated(app *framework.App, fn func(ctx context.Context, row *Plans) error) func() {
	return onEntityEvent[Plans](app, "plans", framework.EntityUpdated, fn)
}

// OnPlansDeleted subscribes to entity.deleted events scoped to "plans". Callback
// receives the deleted row's id only: by the time the event fires the row
// has been removed (or soft-deleted).
func OnPlansDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return onEntityDeleted(app, "plans", fn)
}

// extractPlansRecord unmarshals an event payload's "record" field into a
// *Plans, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractPlansRecord(ev framework.Event, entityName string) (*Plans, bool) {
	return extractEntityRecord[Plans](ev, entityName)
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
		Exposure: &framework.ExposureConfig{
			CRUD:   boolPtr(true),
			MCP:    true,
			Access: framework.AccessControl{Create: "plans:write", Update: "plans:write", Delete: "plans:admin"},
		},
		Display:    &framework.DisplayConfig{TitleFields: []string{"name"}, Description: "What a customer can subscribe to.", Nav: &framework.EntityNav{Group: "catalog", Icon: "box", Order: 1}, Fields: map[string]framework.FieldDisplay{"price": {Input: "money"}}},
		Properties: map[string]any{"label": "Plans"},
	})
	_ = Plans{}
}

func init() {
	registrars = append(registrars, registrar{order: 0, fn: registerPlans})
}
