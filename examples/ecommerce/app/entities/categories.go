package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Categories struct {
	ID          string      `json:"id"`
	Name        string      `json:"name,omitempty"`
	Slug        string      `json:"slug,omitempty"`
	Description string      `json:"description,omitempty"`
	Image       string      `json:"image,omitempty"`
	SortOrder   int         `json:"sortOrder,omitempty"`
	Active      bool        `json:"active,omitempty"`
	Products    []*Products `json:"products,omitempty"`
}

// ====== Categories column references ======

var (
	CategoriesID          = framework.NewUUIDColumn("id")
	CategoriesName        = framework.NewStringColumn("name")
	CategoriesSlug        = framework.NewStringColumn("slug")
	CategoriesDescription = framework.NewStringColumn("description")
	CategoriesImage       = framework.NewStringColumn("image")
	CategoriesSortOrder   = framework.NewIntColumn("sort_order")
	CategoriesActive      = framework.NewBoolColumn("active")
)

// Categories include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	CategoriesInclProducts = "products"
)

// CategoriesRepo is the typed repository for categories rows.
type CategoriesRepo struct {
	handler *framework.CrudHandler
}

// NewCategoriesRepo wires a typed repo against the App's "categories" entity. Panics if the
// entity hasn't been registered yet.
func NewCategoriesRepo(app *framework.App) *CategoriesRepo {
	entity, err := app.Registry.Get("categories")
	if err != nil {
		panic("entities: categories not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("categories")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &CategoriesRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *CategoriesRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *CategoriesRepo) WithTx(tx *sql.Tx) *CategoriesRepo {
	h := *r.handler
	h.DB = tx
	return &CategoriesRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *CategoriesRepo) Create(ctx context.Context, row *Categories) error {
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
func (r *CategoriesRepo) Get(ctx context.Context, id string, includes ...string) (*Categories, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Categories
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *CategoriesRepo) Update(ctx context.Context, id string, row *Categories) error {
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
func (r *CategoriesRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *CategoriesRepo) Query() *framework.TypedQuery[Categories] {
	return framework.NewTypedQuery[Categories](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *CategoriesRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(CategoriesID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *CategoriesRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *CategoriesRepo) FirstOrCreate(ctx context.Context, row *Categories, match framework.Condition) (*Categories, error) {
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
func (r *CategoriesRepo) BatchCreate(ctx context.Context, rows []*Categories) ([]*Categories, error) {
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
func (r *CategoriesRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Categories) ([]*Categories, error) {
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
func (r *CategoriesRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

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

// registerCategories registers the "categories" entity with app.
func registerCategories(app *framework.App) {
	app.Entity("categories", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Max: floatPtr(100)},
			{Name: "slug", Type: schema.String, Required: true, Unique: true, Pattern: "^[a-z0-9-]+$"},
			{Name: "description", Type: schema.Text},
			{Name: "image", Type: schema.Image},
			{Name: "sort_order", Type: schema.Int, Default: 0, Min: floatPtr(0)},
			{Name: "active", Type: schema.Bool, Default: true},
		},
		Relations: []framework.Relation{
			{Type: framework.RelHasMany, Name: "products", Entity: "products", ForeignKey: "category_id"},
		},
		CRUD:       boolPtr(true),
		MCP:        true,
		Properties: map[string]any{"icon": "folder", "label": "Categories"},
	})
	_ = Categories{}
}

func init() {
	registrars = append(registrars, registrar{order: 0, fn: registerCategories})
}
