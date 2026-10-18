package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Orders struct {
	ID              string            `json:"id"`
	UserId          string            `json:"userId,omitempty"`
	OrderNumber     string            `json:"orderNumber,omitempty"`
	Status          OrdersStatusValue `json:"status,omitempty"`
	CustomerName    string            `json:"customerName,omitempty"`
	CustomerEmail   string            `json:"customerEmail,omitempty"`
	CustomerPhone   string            `json:"customerPhone,omitempty"`
	ShippingAddress map[string]any    `json:"shippingAddress,omitempty"`
	BillingAddress  map[string]any    `json:"billingAddress,omitempty"`
	Subtotal        string            `json:"subtotal,omitempty"`
	Tax             string            `json:"tax,omitempty"`
	ShippingCost    string            `json:"shippingCost,omitempty"`
	Total           string            `json:"total,omitempty"`
	Notes           string            `json:"notes,omitempty"`
	ShippedAt       string            `json:"shippedAt,omitempty"`
	DeliveredAt     string            `json:"deliveredAt,omitempty"`
	Items           []*OrderItems     `json:"items,omitempty"`
}

// OrdersStatusValue is one value of the orders field status.
type OrdersStatusValue string

const (
	OrdersStatusPending    OrdersStatusValue = "pending"
	OrdersStatusConfirmed  OrdersStatusValue = "confirmed"
	OrdersStatusProcessing OrdersStatusValue = "processing"
	OrdersStatusShipped    OrdersStatusValue = "shipped"
	OrdersStatusDelivered  OrdersStatusValue = "delivered"
	OrdersStatusCancelled  OrdersStatusValue = "cancelled"
	OrdersStatusRefunded   OrdersStatusValue = "refunded"
)

// OrdersInput is the create payload for OrdersRepo. A required field with no
// default is always sent; a nil field takes its default, and new(false)
// stores false.
type OrdersInput struct {
	UserId          *string            `json:"userId,omitempty"`
	Status          *OrdersStatusValue `json:"status,omitempty"`
	CustomerName    string             `json:"customerName"`
	CustomerEmail   string             `json:"customerEmail"`
	CustomerPhone   *string            `json:"customerPhone,omitempty"`
	ShippingAddress *map[string]any    `json:"shippingAddress,omitempty"`
	BillingAddress  *map[string]any    `json:"billingAddress,omitempty"`
	Subtotal        string             `json:"subtotal"`
	Tax             *string            `json:"tax,omitempty"`
	ShippingCost    *string            `json:"shippingCost,omitempty"`
	Total           string             `json:"total"`
	Notes           *string            `json:"notes,omitempty"`
	ShippedAt       *string            `json:"shippedAt,omitempty"`
	DeliveredAt     *string            `json:"deliveredAt,omitempty"`
}

// OrdersPatch is the update payload for OrdersRepo: a nil field is left as
// stored, and a pointer to false, 0 or "" writes it to a bool, int,
// float, string or text field. "" for any other type counts as absent,
// so a patch cannot clear a date, uuid or enum.
type OrdersPatch struct {
	Status          *OrdersStatusValue `json:"status,omitempty"`
	CustomerName    *string            `json:"customerName,omitempty"`
	CustomerEmail   *string            `json:"customerEmail,omitempty"`
	CustomerPhone   *string            `json:"customerPhone,omitempty"`
	ShippingAddress *map[string]any    `json:"shippingAddress,omitempty"`
	BillingAddress  *map[string]any    `json:"billingAddress,omitempty"`
	Subtotal        *string            `json:"subtotal,omitempty"`
	Tax             *string            `json:"tax,omitempty"`
	ShippingCost    *string            `json:"shippingCost,omitempty"`
	Total           *string            `json:"total,omitempty"`
	Notes           *string            `json:"notes,omitempty"`
	ShippedAt       *string            `json:"shippedAt,omitempty"`
	DeliveredAt     *string            `json:"deliveredAt,omitempty"`
}

// ====== Orders column references ======

var (
	OrdersID              = framework.NewUUIDColumn("id")
	OrdersUserId          = framework.NewStringColumn("user_id")
	OrdersOrderNumber     = framework.NewStringColumn("order_number")
	OrdersStatus          = framework.NewEnumColumn[OrdersStatusValue]("status")
	OrdersCustomerName    = framework.NewStringColumn("customer_name")
	OrdersCustomerEmail   = framework.NewStringColumn("customer_email")
	OrdersCustomerPhone   = framework.NewStringColumn("customer_phone")
	OrdersShippingAddress = framework.NewStringColumn("shipping_address")
	OrdersBillingAddress  = framework.NewStringColumn("billing_address")
	OrdersSubtotal        = framework.NewFloatColumn("subtotal")
	OrdersTax             = framework.NewFloatColumn("tax")
	OrdersShippingCost    = framework.NewFloatColumn("shipping_cost")
	OrdersTotal           = framework.NewFloatColumn("total")
	OrdersNotes           = framework.NewStringColumn("notes")
	OrdersShippedAt       = framework.NewTimestampColumn("shipped_at")
	OrdersDeliveredAt     = framework.NewTimestampColumn("delivered_at")
)

// Orders include names: pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	OrdersInclItems = "items"
)

// OrdersRepo is the typed repository for orders rows.
// Event helpers: OnOrdersCreated/OnOrdersUpdated/OnOrdersDeleted in this package.
type OrdersRepo struct {
	handler *framework.CrudHandler
}

// NewOrdersRepo wires a typed repo against the App's "orders" entity. Panics if the
// entity hasn't been registered yet.
func NewOrdersRepo(app *framework.App) *OrdersRepo {
	entity, err := app.Registry.Get("orders")
	if err != nil {
		panic("entities: orders not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("orders")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &OrdersRepo{handler: h}
}

// Handler returns the underlying CrudHandler: useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *OrdersRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *OrdersRepo) WithTx(tx *sql.Tx) *OrdersRepo {
	h := *r.handler
	h.DB = tx
	return &OrdersRepo{handler: &h}
}

// row decodes one CRUD result.
func (r *OrdersRepo) row(out map[string]any) (*Orders, error) {
	var row Orders
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// rows decodes a batch result, keeping its order.
func (r *OrdersRepo) rows(outs []map[string]any) ([]*Orders, error) {
	rows := make([]*Orders, len(outs))
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
func (r *OrdersRepo) Create(ctx context.Context, in OrdersInput) (*Orders, error) {
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
func (r *OrdersRepo) Get(ctx context.Context, id string, includes ...string) (*Orders, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// Update writes patch's non-nil fields onto the row with this id and
// returns the row as stored afterwards.
func (r *OrdersRepo) Update(ctx context.Context, id string, patch OrdersPatch) (*Orders, error) {
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
func (r *OrdersRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *OrdersRepo) Query() *framework.TypedQuery[Orders] {
	return framework.NewTypedQuery[Orders](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *OrdersRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(OrdersID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *OrdersRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate returns the first row matching match, or creates in and
// returns the stored row when none does.
func (r *OrdersRepo) FirstOrCreate(ctx context.Context, in OrdersInput, match framework.Condition) (*Orders, error) {
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
func (r *OrdersRepo) BatchCreate(ctx context.Context, ins []OrdersInput) ([]*Orders, error) {
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
func (r *OrdersRepo) BatchUpdate(ctx context.Context, ids []string, patches []OrdersPatch) ([]*Orders, error) {
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
func (r *OrdersRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnOrdersCreated subscribes to entity.created events scoped to "orders".
// Returns a cancel func; call it to remove the handler.
func OnOrdersCreated(app *framework.App, fn func(ctx context.Context, row *Orders) error) func() {
	return onEntityEvent[Orders](app, "orders", framework.EntityCreated, fn)
}

// OnOrdersUpdated subscribes to entity.updated events scoped to "orders".
func OnOrdersUpdated(app *framework.App, fn func(ctx context.Context, row *Orders) error) func() {
	return onEntityEvent[Orders](app, "orders", framework.EntityUpdated, fn)
}

// OnOrdersDeleted subscribes to entity.deleted events scoped to "orders". Callback
// receives the deleted row's id only: by the time the event fires the row
// has been removed (or soft-deleted).
func OnOrdersDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return onEntityDeleted(app, "orders", fn)
}

// extractOrdersRecord unmarshals an event payload's "record" field into a
// *Orders, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractOrdersRecord(ev framework.Event, entityName string) (*Orders, bool) {
	return extractEntityRecord[Orders](ev, entityName)
}

// registerOrders registers the "orders" entity with app.
func registerOrders(app *framework.App) {
	app.Entity("orders", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "order_number", Type: schema.String, Required: true, Unique: true, ReadOnly: true, AutoGenerate: schema.AutoUUID},
			{Name: "status", Type: schema.Enum, Default: "pending", Values: []string{"pending", "confirmed", "processing", "shipped", "delivered", "cancelled", "refunded"}},
			{Name: "customer_name", Type: schema.String, Required: true, Max: floatPtr(200)},
			{Name: "customer_email", Type: schema.String, Required: true, Pattern: "^[^@]+@[^@]+\\.[^@]+$"},
			{Name: "customer_phone", Type: schema.String, Max: floatPtr(30)},
			{Name: "shipping_address", Type: schema.JSON},
			{Name: "billing_address", Type: schema.JSON},
			{Name: "subtotal", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "tax", Type: schema.Decimal, Default: 0, Min: floatPtr(0)},
			{Name: "shipping_cost", Type: schema.Decimal, Default: 0, Min: floatPtr(0)},
			{Name: "total", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "notes", Type: schema.Text},
			{Name: "shipped_at", Type: schema.Timestamp},
			{Name: "delivered_at", Type: schema.Timestamp},
		},
		Relations: []framework.Relation{
			{Type: framework.RelHasMany, Name: "items", Entity: "order_items", ForeignKey: "order_id"},
		},
		Scope: &framework.ScopeConfig{
			OwnerField: "user_id",
		},
		Exposure: &framework.ExposureConfig{
			CRUD: boolPtr(true),
			MCP:  true,
		},
		Pagination: &framework.PaginationConfig{
			CursorField:  "id",
			CursorFields: []string{"created_at", "id"},
		},
		Indices: []framework.Index{
			{Name: "idx_orders_status", Columns: []string{"status"}},
			{Name: "idx_orders_user", Columns: []string{"user_id"}},
		},
		Properties: map[string]any{"icon": "shopping-cart", "label": "Orders"},
	})
	_ = Orders{}
}

func init() {
	registrars = append(registrars, registrar{order: 2, fn: registerOrders})
}
