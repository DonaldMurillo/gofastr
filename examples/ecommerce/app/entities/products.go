//gofastr:generated generator=app kind=scaffold version=dev sum=2b2be50b0c0329f7

package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Products struct {
	ID             string              `json:"id"`
	Name           string              `json:"name,omitempty"`
	Slug           string              `json:"slug,omitempty"`
	Sku            string              `json:"sku,omitempty"`
	Description    string              `json:"description,omitempty"`
	Price          string              `json:"price,omitempty"`
	CompareAtPrice string              `json:"compareAtPrice,omitempty"`
	Cost           string              `json:"cost,omitempty"`
	Stock          int                 `json:"stock,omitempty"`
	CategoryId     string              `json:"categoryId,omitempty"`
	Status         ProductsStatusValue `json:"status,omitempty"`
	Featured       bool                `json:"featured,omitempty"`
	Weight         float64             `json:"weight,omitempty"`
	Image          string              `json:"image,omitempty"`
	Tags           any                 `json:"tags,omitempty"`
	Category       *Categories         `json:"category,omitempty"`
	Reviews        []*Reviews          `json:"reviews,omitempty"`
	OrderItems     []*OrderItems       `json:"orderItems,omitempty"`
}

// ProductsStatusValue is one value of the products field status.
type ProductsStatusValue string

const (
	ProductsStatusDraft    ProductsStatusValue = "draft"
	ProductsStatusActive   ProductsStatusValue = "active"
	ProductsStatusArchived ProductsStatusValue = "archived"
)

// ProductsInput is the create payload for ProductsRepo. A required field with no
// default is always sent; a nil field takes its default, and new(false)
// stores false.
type ProductsInput struct {
	Name           string               `json:"name"`
	Slug           string               `json:"slug"`
	Sku            *string              `json:"sku,omitempty"`
	Description    *string              `json:"description,omitempty"`
	Price          string               `json:"price"`
	CompareAtPrice *string              `json:"compareAtPrice,omitempty"`
	Cost           *string              `json:"cost,omitempty"`
	Stock          *int                 `json:"stock,omitempty"`
	CategoryId     *string              `json:"categoryId,omitempty"`
	Status         *ProductsStatusValue `json:"status,omitempty"`
	Featured       *bool                `json:"featured,omitempty"`
	Weight         *float64             `json:"weight,omitempty"`
	Image          *string              `json:"image,omitempty"`
	Tags           *any                 `json:"tags,omitempty"`
}

// ProductsPatch is the update payload for ProductsRepo: a nil field is left as
// stored, and a pointer to false, 0 or "" writes it to a bool, int,
// float, string or text field. "" for any other type is a blank: it
// clears an optional field and fails validation on a required one.
type ProductsPatch struct {
	Name           *string              `json:"name,omitempty"`
	Slug           *string              `json:"slug,omitempty"`
	Sku            *string              `json:"sku,omitempty"`
	Description    *string              `json:"description,omitempty"`
	Price          *string              `json:"price,omitempty"`
	CompareAtPrice *string              `json:"compareAtPrice,omitempty"`
	Cost           *string              `json:"cost,omitempty"`
	Stock          *int                 `json:"stock,omitempty"`
	CategoryId     *string              `json:"categoryId,omitempty"`
	Status         *ProductsStatusValue `json:"status,omitempty"`
	Featured       *bool                `json:"featured,omitempty"`
	Weight         *float64             `json:"weight,omitempty"`
	Image          *string              `json:"image,omitempty"`
	Tags           *any                 `json:"tags,omitempty"`
}

// ====== Products column references ======

var (
	ProductsID             = framework.NewUUIDColumn("id")
	ProductsName           = framework.NewStringColumn("name")
	ProductsSlug           = framework.NewStringColumn("slug")
	ProductsSku            = framework.NewStringColumn("sku")
	ProductsDescription    = framework.NewStringColumn("description")
	ProductsPrice          = framework.NewFloatColumn("price")
	ProductsCompareAtPrice = framework.NewFloatColumn("compare_at_price")
	ProductsCost           = framework.NewFloatColumn("cost")
	ProductsStock          = framework.NewIntColumn("stock")
	ProductsCategoryId     = framework.NewUUIDColumn("category_id")
	ProductsStatus         = framework.NewEnumColumn[ProductsStatusValue]("status")
	ProductsFeatured       = framework.NewBoolColumn("featured")
	ProductsWeight         = framework.NewFloatColumn("weight")
	ProductsImage          = framework.NewStringColumn("image")
	ProductsTags           = framework.NewStringColumn("tags")
)

// Products include names: pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	ProductsInclCategory   = "category"
	ProductsInclReviews    = "reviews"
	ProductsInclOrderItems = "order_items"
)

// ProductsRepo is the typed repository for products rows.
// Event helpers: OnProductsCreated/OnProductsUpdated/OnProductsDeleted in this package.
type ProductsRepo struct {
	handler *framework.CrudHandler
}

// NewProductsRepo wires a typed repo against the App's "products" entity. Panics if the
// entity hasn't been registered yet.
func NewProductsRepo(app *framework.App) *ProductsRepo {
	entity, err := app.Registry.Get("products")
	if err != nil {
		panic("entities: products not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("products")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &ProductsRepo{handler: h}
}

// Handler returns the underlying CrudHandler: useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *ProductsRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *ProductsRepo) WithTx(tx *sql.Tx) *ProductsRepo {
	h := *r.handler
	h.DB = tx
	return &ProductsRepo{handler: &h}
}

// row decodes one CRUD result.
func (r *ProductsRepo) row(out map[string]any) (*Products, error) {
	var row Products
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// rows decodes a batch result, keeping its order.
func (r *ProductsRepo) rows(outs []map[string]any) ([]*Products, error) {
	rows := make([]*Products, len(outs))
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
func (r *ProductsRepo) Create(ctx context.Context, in ProductsInput) (*Products, error) {
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
func (r *ProductsRepo) Get(ctx context.Context, id string, includes ...string) (*Products, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// Update writes patch's non-nil fields onto the row with this id and
// returns the row as stored afterwards.
func (r *ProductsRepo) Update(ctx context.Context, id string, patch ProductsPatch) (*Products, error) {
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
func (r *ProductsRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *ProductsRepo) Query() *framework.TypedQuery[Products] {
	return framework.NewTypedQuery[Products](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *ProductsRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(ProductsID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *ProductsRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate returns the first row matching match, or creates in and
// returns the stored row when none does.
func (r *ProductsRepo) FirstOrCreate(ctx context.Context, in ProductsInput, match framework.Condition) (*Products, error) {
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
func (r *ProductsRepo) BatchCreate(ctx context.Context, ins []ProductsInput) ([]*Products, error) {
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
func (r *ProductsRepo) BatchUpdate(ctx context.Context, ids []string, patches []ProductsPatch) ([]*Products, error) {
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
func (r *ProductsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnProductsCreated subscribes to entity.created events scoped to "products".
// Returns a cancel func; call it to remove the handler.
func OnProductsCreated(app *framework.App, fn func(ctx context.Context, row *Products) error) func() {
	return onEntityEvent[Products](app, "products", framework.EntityCreated, fn)
}

// OnProductsUpdated subscribes to entity.updated events scoped to "products".
func OnProductsUpdated(app *framework.App, fn func(ctx context.Context, row *Products) error) func() {
	return onEntityEvent[Products](app, "products", framework.EntityUpdated, fn)
}

// OnProductsDeleted subscribes to entity.deleted events scoped to "products". Callback
// receives the deleted row's id only: by the time the event fires the row
// has been removed (or soft-deleted).
func OnProductsDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return onEntityDeleted(app, "products", fn)
}

// extractProductsRecord unmarshals an event payload's "record" field into a
// *Products, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractProductsRecord(ev framework.Event, entityName string) (*Products, bool) {
	return extractEntityRecord[Products](ev, entityName)
}

// registerProducts registers the "products" entity with app.
func registerProducts(app *framework.App) {
	app.Entity("products", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Max: floatPtr(200)},
			{Name: "slug", Type: schema.String, Required: true, Unique: true, Pattern: "^[a-z0-9-]+$"},
			{Name: "sku", Type: schema.String, Unique: true, Max: floatPtr(50)},
			{Name: "description", Type: schema.Text},
			{Name: "price", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "compare_at_price", Type: schema.Decimal, Min: floatPtr(0)},
			{Name: "cost", Type: schema.Decimal, Hidden: true, Min: floatPtr(0)},
			{Name: "stock", Type: schema.Int, Required: true, Default: 0, Min: floatPtr(0)},
			{Name: "category_id", Type: schema.Relation, To: "categories"},
			{Name: "status", Type: schema.Enum, Default: "draft", Values: []string{"draft", "active", "archived"}},
			{Name: "featured", Type: schema.Bool, Default: false},
			{Name: "weight", Type: schema.Float, Min: floatPtr(0)},
			{Name: "image", Type: schema.Image},
			{Name: "tags", Type: schema.JSON},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "category", Entity: "categories", ForeignKey: []string{"category_id"}},
			{Type: framework.RelHasMany, Name: "reviews", Entity: "reviews", ForeignKey: []string{"product_id"}},
			{Type: framework.RelHasMany, Name: "order_items", Entity: "order_items", ForeignKey: []string{"product_id"}},
		},
		Scope: &framework.ScopeConfig{
			SoftDelete: true,
		},
		Exposure: &framework.ExposureConfig{
			CRUD:   boolPtr(true),
			MCP:    true,
			Access: framework.AccessControl{Create: "catalog:write", Update: "catalog:write", Delete: "catalog:admin"},
		},
		Pagination: &framework.PaginationConfig{
			CursorField:  "id",
			CursorFields: []string{"created_at", "id"},
		},
		Indices: []framework.Index{
			{Name: "idx_products_category", Columns: []string{"category_id"}},
			{Name: "idx_products_status", Columns: []string{"status"}},
		},
		Properties: map[string]any{"icon": "package", "label": "Products"},
	})
	_ = Products{}
}

func init() {
	registrars = append(registrars, registrar{order: 1, fn: registerProducts})
}
