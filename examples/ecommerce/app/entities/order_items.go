//gofastr:generated generator=app kind=scaffold version=dev sum=f30f969a16c6aa95

package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type OrderItems struct {
	ID          string    `json:"id"`
	UserId      string    `json:"userId,omitempty"`
	OrderId     string    `json:"orderId,omitempty"`
	ProductId   string    `json:"productId,omitempty"`
	ProductName string    `json:"productName,omitempty"`
	Quantity    int       `json:"quantity,omitempty"`
	UnitPrice   string    `json:"unitPrice,omitempty"`
	TotalPrice  string    `json:"totalPrice,omitempty"`
	Order       *Orders   `json:"order,omitempty"`
	Product     *Products `json:"product,omitempty"`
}

// OrderItemsInput is the create payload for OrderItemsRepo. A required field with no
// default is always sent; a nil field takes its default, and new(false)
// stores false.
type OrderItemsInput struct {
	UserId      *string `json:"userId,omitempty"`
	OrderId     string  `json:"orderId"`
	ProductId   string  `json:"productId"`
	ProductName string  `json:"productName"`
	Quantity    int     `json:"quantity"`
	UnitPrice   string  `json:"unitPrice"`
	TotalPrice  string  `json:"totalPrice"`
}

// OrderItemsPatch is the update payload for OrderItemsRepo: a nil field is left as
// stored, and a pointer to false, 0 or "" writes it to a bool, int,
// float, string or text field. "" for any other type is not a value:
// absent on an optional field, a validation error on a required one.
type OrderItemsPatch struct {
	OrderId     *string `json:"orderId,omitempty"`
	ProductId   *string `json:"productId,omitempty"`
	ProductName *string `json:"productName,omitempty"`
	Quantity    *int    `json:"quantity,omitempty"`
	UnitPrice   *string `json:"unitPrice,omitempty"`
	TotalPrice  *string `json:"totalPrice,omitempty"`
}

// ====== OrderItems column references ======

var (
	OrderItemsID          = framework.NewUUIDColumn("id")
	OrderItemsUserId      = framework.NewStringColumn("user_id")
	OrderItemsOrderId     = framework.NewUUIDColumn("order_id")
	OrderItemsProductId   = framework.NewUUIDColumn("product_id")
	OrderItemsProductName = framework.NewStringColumn("product_name")
	OrderItemsQuantity    = framework.NewIntColumn("quantity")
	OrderItemsUnitPrice   = framework.NewFloatColumn("unit_price")
	OrderItemsTotalPrice  = framework.NewFloatColumn("total_price")
)

// OrderItems include names: pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	OrderItemsInclOrder   = "order"
	OrderItemsInclProduct = "product"
)

// OrderItemsRepo is the typed repository for order_items rows.
// Event helpers: OnOrderItemsCreated/OnOrderItemsUpdated/OnOrderItemsDeleted in this package.
type OrderItemsRepo struct {
	handler *framework.CrudHandler
}

// NewOrderItemsRepo wires a typed repo against the App's "order_items" entity. Panics if the
// entity hasn't been registered yet.
func NewOrderItemsRepo(app *framework.App) *OrderItemsRepo {
	entity, err := app.Registry.Get("order_items")
	if err != nil {
		panic("entities: order_items not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("order_items")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &OrderItemsRepo{handler: h}
}

// Handler returns the underlying CrudHandler: useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *OrderItemsRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *OrderItemsRepo) WithTx(tx *sql.Tx) *OrderItemsRepo {
	h := *r.handler
	h.DB = tx
	return &OrderItemsRepo{handler: &h}
}

// row decodes one CRUD result.
func (r *OrderItemsRepo) row(out map[string]any) (*OrderItems, error) {
	var row OrderItems
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// rows decodes a batch result, keeping its order.
func (r *OrderItemsRepo) rows(outs []map[string]any) ([]*OrderItems, error) {
	rows := make([]*OrderItems, len(outs))
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
func (r *OrderItemsRepo) Create(ctx context.Context, in OrderItemsInput) (*OrderItems, error) {
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
func (r *OrderItemsRepo) Get(ctx context.Context, id string, includes ...string) (*OrderItems, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// Update writes patch's non-nil fields onto the row with this id and
// returns the row as stored afterwards.
func (r *OrderItemsRepo) Update(ctx context.Context, id string, patch OrderItemsPatch) (*OrderItems, error) {
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
func (r *OrderItemsRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *OrderItemsRepo) Query() *framework.TypedQuery[OrderItems] {
	return framework.NewTypedQuery[OrderItems](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *OrderItemsRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(OrderItemsID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *OrderItemsRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate returns the first row matching match, or creates in and
// returns the stored row when none does.
func (r *OrderItemsRepo) FirstOrCreate(ctx context.Context, in OrderItemsInput, match framework.Condition) (*OrderItems, error) {
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
func (r *OrderItemsRepo) BatchCreate(ctx context.Context, ins []OrderItemsInput) ([]*OrderItems, error) {
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
func (r *OrderItemsRepo) BatchUpdate(ctx context.Context, ids []string, patches []OrderItemsPatch) ([]*OrderItems, error) {
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
func (r *OrderItemsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnOrderItemsCreated subscribes to entity.created events scoped to "order_items".
// Returns a cancel func; call it to remove the handler.
func OnOrderItemsCreated(app *framework.App, fn func(ctx context.Context, row *OrderItems) error) func() {
	return onEntityEvent[OrderItems](app, "order_items", framework.EntityCreated, fn)
}

// OnOrderItemsUpdated subscribes to entity.updated events scoped to "order_items".
func OnOrderItemsUpdated(app *framework.App, fn func(ctx context.Context, row *OrderItems) error) func() {
	return onEntityEvent[OrderItems](app, "order_items", framework.EntityUpdated, fn)
}

// OnOrderItemsDeleted subscribes to entity.deleted events scoped to "order_items". Callback
// receives the deleted row's id only: by the time the event fires the row
// has been removed (or soft-deleted).
func OnOrderItemsDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return onEntityDeleted(app, "order_items", fn)
}

// extractOrderItemsRecord unmarshals an event payload's "record" field into a
// *OrderItems, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractOrderItemsRecord(ev framework.Event, entityName string) (*OrderItems, bool) {
	return extractEntityRecord[OrderItems](ev, entityName)
}

// registerOrderItems registers the "order_items" entity with app.
func registerOrderItems(app *framework.App) {
	app.Entity("order_items", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "order_id", Type: schema.Relation, Required: true, To: "orders"},
			{Name: "product_id", Type: schema.Relation, Required: true, To: "products"},
			{Name: "product_name", Type: schema.String, Required: true, Max: floatPtr(200)},
			{Name: "quantity", Type: schema.Int, Required: true, Min: floatPtr(1)},
			{Name: "unit_price", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "total_price", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "order", Entity: "orders", ForeignKey: "order_id"},
			{Type: framework.RelManyToOne, Name: "product", Entity: "products", ForeignKey: "product_id"},
		},
		Scope: &framework.ScopeConfig{
			OwnerField: "user_id",
		},
		Exposure: &framework.ExposureConfig{
			CRUD: boolPtr(true),
			MCP:  true,
		},
	})
	_ = OrderItems{}
}

func init() {
	registrars = append(registrars, registrar{order: 3, fn: registerOrderItems})
}
