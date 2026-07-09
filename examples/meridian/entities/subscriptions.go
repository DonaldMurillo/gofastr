package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Subscriptions struct {
	ID         string     `json:"id"`
	CustomerId string     `json:"customerId,omitempty"`
	PlanId     string     `json:"planId,omitempty"`
	Status     string     `json:"status,omitempty"`
	Mrr        string     `json:"mrr,omitempty"`
	StartedOn  string     `json:"startedOn,omitempty"`
	RenewsOn   string     `json:"renewsOn,omitempty"`
	UserId     string     `json:"userId,omitempty"`
	Customer   *Customers `json:"customer,omitempty"`
	Plan       *Plans     `json:"plan,omitempty"`
}

// ====== Subscriptions column references ======

var (
	SubscriptionsID         = framework.NewUUIDColumn("id")
	SubscriptionsCustomerId = framework.NewUUIDColumn("customer_id")
	SubscriptionsPlanId     = framework.NewUUIDColumn("plan_id")
	SubscriptionsStatus     = framework.NewStringColumn("status")
	SubscriptionsMrr        = framework.NewFloatColumn("mrr")
	SubscriptionsStartedOn  = framework.NewTimestampColumn("started_on")
	SubscriptionsRenewsOn   = framework.NewTimestampColumn("renews_on")
	SubscriptionsUserId     = framework.NewStringColumn("user_id")
)

// Subscriptions include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	SubscriptionsInclCustomer = "customer"
	SubscriptionsInclPlan     = "plan"
)

// SubscriptionsRepo is the typed repository for subscriptions rows.
type SubscriptionsRepo struct {
	handler *framework.CrudHandler
}

// NewSubscriptionsRepo wires a typed repo against the App's "subscriptions" entity. Panics if the
// entity hasn't been registered yet.
func NewSubscriptionsRepo(app *framework.App) *SubscriptionsRepo {
	entity, err := app.Registry.Get("subscriptions")
	if err != nil {
		panic("entities: subscriptions not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("subscriptions")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &SubscriptionsRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *SubscriptionsRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *SubscriptionsRepo) WithTx(tx *sql.Tx) *SubscriptionsRepo {
	h := *r.handler
	h.DB = tx
	return &SubscriptionsRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *SubscriptionsRepo) Create(ctx context.Context, row *Subscriptions) error {
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
func (r *SubscriptionsRepo) Get(ctx context.Context, id string, includes ...string) (*Subscriptions, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Subscriptions
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *SubscriptionsRepo) Update(ctx context.Context, id string, row *Subscriptions) error {
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
func (r *SubscriptionsRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *SubscriptionsRepo) Query() *framework.TypedQuery[Subscriptions] {
	return framework.NewTypedQuery[Subscriptions](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *SubscriptionsRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(SubscriptionsID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *SubscriptionsRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *SubscriptionsRepo) FirstOrCreate(ctx context.Context, row *Subscriptions, match framework.Condition) (*Subscriptions, error) {
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
func (r *SubscriptionsRepo) BatchCreate(ctx context.Context, rows []*Subscriptions) ([]*Subscriptions, error) {
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
func (r *SubscriptionsRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Subscriptions) ([]*Subscriptions, error) {
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
func (r *SubscriptionsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnSubscriptionsCreated subscribes to entity.created events scoped to "subscriptions".
// Returns a cancel func; call it to remove the handler.
func OnSubscriptionsCreated(app *framework.App, fn func(ctx context.Context, row *Subscriptions) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractSubscriptionsRecord(ev, "subscriptions")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnSubscriptionsUpdated subscribes to entity.updated events scoped to "subscriptions".
func OnSubscriptionsUpdated(app *framework.App, fn func(ctx context.Context, row *Subscriptions) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractSubscriptionsRecord(ev, "subscriptions")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnSubscriptionsDeleted subscribes to entity.deleted events scoped to "subscriptions". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnSubscriptionsDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "subscriptions" {
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

// extractSubscriptionsRecord unmarshals an event payload's "record" field into a
// *Subscriptions, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractSubscriptionsRecord(ev framework.Event, entityName string) (*Subscriptions, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Subscriptions
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}

// registerSubscriptions registers the "subscriptions" entity with app.
func registerSubscriptions(app *framework.App) {
	app.Entity("subscriptions", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "customer_id", Type: schema.Relation, Required: true, To: "customers"},
			{Name: "plan_id", Type: schema.Relation, Required: true, To: "plans"},
			{Name: "status", Type: schema.Enum, Default: "trialing", Values: []string{"trialing", "active", "past_due", "canceled"}},
			{Name: "mrr", Type: schema.Decimal, Default: "0", Min: floatPtr(0)},
			{Name: "started_on", Type: schema.Date},
			{Name: "renews_on", Type: schema.Date},
			{Name: "user_id", Type: schema.String, Hidden: true},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "customer", Entity: "customers", ForeignKey: "customer_id"},
			{Type: framework.RelManyToOne, Name: "plan", Entity: "plans", ForeignKey: "plan_id"},
		},
		OwnerField: "user_id",
		CRUD:       boolPtr(true),
		MCP:        true,
		Properties: map[string]any{"label": "Subscriptions"},
	})
	_ = Subscriptions{}
}

func init() {
	registrars = append(registrars, registrar{order: 2, fn: registerSubscriptions})
}
