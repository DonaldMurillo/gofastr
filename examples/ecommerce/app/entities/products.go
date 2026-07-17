package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Products struct {
	ID             string         `json:"id"`
	Name           string         `json:"name,omitempty"`
	Slug           string         `json:"slug,omitempty"`
	Sku            string         `json:"sku,omitempty"`
	Description    string         `json:"description,omitempty"`
	Price          string         `json:"price,omitempty"`
	CompareAtPrice string         `json:"compareAtPrice,omitempty"`
	Cost           string         `json:"cost,omitempty"`
	Stock          int            `json:"stock,omitempty"`
	CategoryId     string         `json:"categoryId,omitempty"`
	Status         string         `json:"status,omitempty"`
	Featured       bool           `json:"featured,omitempty"`
	Weight         float64        `json:"weight,omitempty"`
	Image          string         `json:"image,omitempty"`
	Tags           map[string]any `json:"tags,omitempty"`
	Category       *Categories    `json:"category,omitempty"`
	Reviews        []*Reviews     `json:"reviews,omitempty"`
	OrderItems     []*OrderItems  `json:"orderItems,omitempty"`
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
	ProductsStatus         = framework.NewStringColumn("status")
	ProductsFeatured       = framework.NewBoolColumn("featured")
	ProductsWeight         = framework.NewFloatColumn("weight")
	ProductsImage          = framework.NewStringColumn("image")
	ProductsTags           = framework.NewStringColumn("tags")
)

// Products include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
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

// Handler returns the underlying CrudHandler — useful for advanced wiring or
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

// Create persists row and back-fills server-generated fields onto it.
func (r *ProductsRepo) Create(ctx context.Context, row *Products) error {
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
func (r *ProductsRepo) Get(ctx context.Context, id string, includes ...string) (*Products, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Products
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *ProductsRepo) Update(ctx context.Context, id string, row *Products) error {
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

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *ProductsRepo) FirstOrCreate(ctx context.Context, row *Products, match framework.Condition) (*Products, error) {
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
func (r *ProductsRepo) BatchCreate(ctx context.Context, rows []*Products) ([]*Products, error) {
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
func (r *ProductsRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Products) ([]*Products, error) {
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
func (r *ProductsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnProductsCreated subscribes to entity.created events scoped to "products".
// Returns a cancel func; call it to remove the handler.
func OnProductsCreated(app *framework.App, fn func(ctx context.Context, row *Products) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractProductsRecord(ev, "products")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnProductsUpdated subscribes to entity.updated events scoped to "products".
func OnProductsUpdated(app *framework.App, fn func(ctx context.Context, row *Products) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractProductsRecord(ev, "products")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnProductsDeleted subscribes to entity.deleted events scoped to "products". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnProductsDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "products" {
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

// extractProductsRecord unmarshals an event payload's "record" field into a
// *Products, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractProductsRecord(ev framework.Event, entityName string) (*Products, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Products
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
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
			{Type: framework.RelManyToOne, Name: "category", Entity: "categories", ForeignKey: "category_id"},
			{Type: framework.RelHasMany, Name: "reviews", Entity: "reviews", ForeignKey: "product_id"},
			{Type: framework.RelHasMany, Name: "order_items", Entity: "order_items", ForeignKey: "product_id"},
		},
		SoftDelete:   true,
		CRUD:         boolPtr(true),
		MCP:          true,
		CursorField:  "id",
		CursorFields: []string{"created_at", "id"},
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
