package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Orders struct {
	ID              string         `json:"id"`
	UserId          string         `json:"userId,omitempty"`
	OrderNumber     string         `json:"orderNumber,omitempty"`
	Status          string         `json:"status,omitempty"`
	CustomerName    string         `json:"customerName,omitempty"`
	CustomerEmail   string         `json:"customerEmail,omitempty"`
	CustomerPhone   string         `json:"customerPhone,omitempty"`
	ShippingAddress map[string]any `json:"shippingAddress,omitempty"`
	BillingAddress  map[string]any `json:"billingAddress,omitempty"`
	Subtotal        string         `json:"subtotal,omitempty"`
	Tax             string         `json:"tax,omitempty"`
	ShippingCost    string         `json:"shippingCost,omitempty"`
	Total           string         `json:"total,omitempty"`
	Notes           string         `json:"notes,omitempty"`
	ShippedAt       string         `json:"shippedAt,omitempty"`
	DeliveredAt     string         `json:"deliveredAt,omitempty"`
	Items           []*OrderItems  `json:"items,omitempty"`
}

// ====== Orders column references ======

var (
	OrdersID              = framework.NewUUIDColumn("id")
	OrdersUserId          = framework.NewStringColumn("user_id")
	OrdersOrderNumber     = framework.NewStringColumn("order_number")
	OrdersStatus          = framework.NewStringColumn("status")
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

// Orders include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
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

// Handler returns the underlying CrudHandler — useful for advanced wiring or
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

// Create persists row and back-fills server-generated fields onto it.
func (r *OrdersRepo) Create(ctx context.Context, row *Orders) error {
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
func (r *OrdersRepo) Get(ctx context.Context, id string, includes ...string) (*Orders, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Orders
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *OrdersRepo) Update(ctx context.Context, id string, row *Orders) error {
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

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *OrdersRepo) FirstOrCreate(ctx context.Context, row *Orders, match framework.Condition) (*Orders, error) {
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
func (r *OrdersRepo) BatchCreate(ctx context.Context, rows []*Orders) ([]*Orders, error) {
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
func (r *OrdersRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Orders) ([]*Orders, error) {
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
func (r *OrdersRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnOrdersCreated subscribes to entity.created events scoped to "orders".
// Returns a cancel func; call it to remove the handler.
func OnOrdersCreated(app *framework.App, fn func(ctx context.Context, row *Orders) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractOrdersRecord(ev, "orders")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnOrdersUpdated subscribes to entity.updated events scoped to "orders".
func OnOrdersUpdated(app *framework.App, fn func(ctx context.Context, row *Orders) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractOrdersRecord(ev, "orders")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnOrdersDeleted subscribes to entity.deleted events scoped to "orders". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnOrdersDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "orders" {
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

// extractOrdersRecord unmarshals an event payload's "record" field into a
// *Orders, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractOrdersRecord(ev framework.Event, entityName string) (*Orders, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Orders
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
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
		OwnerField:   "user_id",
		CRUD:         boolPtr(true),
		MCP:          true,
		CursorField:  "id",
		CursorFields: []string{"created_at", "id"},
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
