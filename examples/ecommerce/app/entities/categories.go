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

// CategoriesInput is the create payload for CategoriesRepo. A required field with no
// default is always sent; a nil field takes its default, and new(false)
// stores false.
type CategoriesInput struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description,omitempty"`
	Image       *string `json:"image,omitempty"`
	SortOrder   *int    `json:"sortOrder,omitempty"`
	Active      *bool   `json:"active,omitempty"`
}

// CategoriesPatch is the update payload for CategoriesRepo: a nil field is left as
// stored, and a pointer to false, 0 or "" writes it to a bool, int,
// float, string or text field. "" for any other type is not a value:
// absent on an optional field, a validation error on a required one.
type CategoriesPatch struct {
	Name        *string `json:"name,omitempty"`
	Slug        *string `json:"slug,omitempty"`
	Description *string `json:"description,omitempty"`
	Image       *string `json:"image,omitempty"`
	SortOrder   *int    `json:"sortOrder,omitempty"`
	Active      *bool   `json:"active,omitempty"`
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

// Categories include names: pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	CategoriesInclProducts = "products"
)

// CategoriesRepo is the typed repository for categories rows.
// Event helpers: OnCategoriesCreated/OnCategoriesUpdated/OnCategoriesDeleted in this package.
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

// Handler returns the underlying CrudHandler: useful for advanced wiring or
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

// row decodes one CRUD result.
func (r *CategoriesRepo) row(out map[string]any) (*Categories, error) {
	var row Categories
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// rows decodes a batch result, keeping its order.
func (r *CategoriesRepo) rows(outs []map[string]any) ([]*Categories, error) {
	rows := make([]*Categories, len(outs))
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
func (r *CategoriesRepo) Create(ctx context.Context, in CategoriesInput) (*Categories, error) {
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
func (r *CategoriesRepo) Get(ctx context.Context, id string, includes ...string) (*Categories, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// Update writes patch's non-nil fields onto the row with this id and
// returns the row as stored afterwards.
func (r *CategoriesRepo) Update(ctx context.Context, id string, patch CategoriesPatch) (*Categories, error) {
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

// FirstOrCreate returns the first row matching match, or creates in and
// returns the stored row when none does.
func (r *CategoriesRepo) FirstOrCreate(ctx context.Context, in CategoriesInput, match framework.Condition) (*Categories, error) {
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
func (r *CategoriesRepo) BatchCreate(ctx context.Context, ins []CategoriesInput) ([]*Categories, error) {
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
func (r *CategoriesRepo) BatchUpdate(ctx context.Context, ids []string, patches []CategoriesPatch) ([]*Categories, error) {
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
func (r *CategoriesRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnCategoriesCreated subscribes to entity.created events scoped to "categories".
// Returns a cancel func; call it to remove the handler.
func OnCategoriesCreated(app *framework.App, fn func(ctx context.Context, row *Categories) error) func() {
	return onEntityEvent[Categories](app, "categories", framework.EntityCreated, fn)
}

// OnCategoriesUpdated subscribes to entity.updated events scoped to "categories".
func OnCategoriesUpdated(app *framework.App, fn func(ctx context.Context, row *Categories) error) func() {
	return onEntityEvent[Categories](app, "categories", framework.EntityUpdated, fn)
}

// OnCategoriesDeleted subscribes to entity.deleted events scoped to "categories". Callback
// receives the deleted row's id only: by the time the event fires the row
// has been removed (or soft-deleted).
func OnCategoriesDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return onEntityDeleted(app, "categories", fn)
}

// extractCategoriesRecord unmarshals an event payload's "record" field into a
// *Categories, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractCategoriesRecord(ev framework.Event, entityName string) (*Categories, bool) {
	return extractEntityRecord[Categories](ev, entityName)
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
		Exposure: &framework.ExposureConfig{
			CRUD:   boolPtr(true),
			MCP:    true,
			Access: framework.AccessControl{Create: "catalog:write", Update: "catalog:write", Delete: "catalog:admin"},
		},
		Properties: map[string]any{"icon": "folder", "label": "Categories"},
	})
	_ = Categories{}
}

func init() {
	registrars = append(registrars, registrar{order: 0, fn: registerCategories})
}
