package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Subscriptions struct {
	ID         string                   `json:"id"`
	CustomerId string                   `json:"customerId,omitempty"`
	PlanId     string                   `json:"planId,omitempty"`
	Status     SubscriptionsStatusValue `json:"status,omitempty"`
	Mrr        string                   `json:"mrr,omitempty"`
	StartedOn  string                   `json:"startedOn,omitempty"`
	RenewsOn   string                   `json:"renewsOn,omitempty"`
	UserId     string                   `json:"userId,omitempty"`
	Customer   *Customers               `json:"customer,omitempty"`
	Plan       *Plans                   `json:"plan,omitempty"`
}

// SubscriptionsStatusValue is one value of the subscriptions field status.
type SubscriptionsStatusValue string

const (
	SubscriptionsStatusTrialing SubscriptionsStatusValue = "trialing"
	SubscriptionsStatusActive   SubscriptionsStatusValue = "active"
	SubscriptionsStatusPastDue  SubscriptionsStatusValue = "past_due"
	SubscriptionsStatusCanceled SubscriptionsStatusValue = "canceled"
)

// SubscriptionsMove names one of the subscriptions moves; pass it to SubscriptionsRepo.Transition.
type SubscriptionsMove string

const (
	SubscriptionsMoveActivate SubscriptionsMove = "activate"
	SubscriptionsMoveCancel   SubscriptionsMove = "cancel"
)

// SubscriptionsInput is the create payload for SubscriptionsRepo. A required field with no
// default is always sent; a nil field takes its default, and new(false)
// stores false.
type SubscriptionsInput struct {
	CustomerId string                    `json:"customerId"`
	PlanId     string                    `json:"planId"`
	Status     *SubscriptionsStatusValue `json:"status,omitempty"`
	Mrr        *string                   `json:"mrr,omitempty"`
	StartedOn  *string                   `json:"startedOn,omitempty"`
	RenewsOn   *string                   `json:"renewsOn,omitempty"`
	UserId     *string                   `json:"userId,omitempty"`
}

// SubscriptionsPatch is the update payload for SubscriptionsRepo: a nil field is left as
// stored, and a pointer to false, 0 or "" writes it to a bool, int,
// float, string or text field. "" for any other type counts as absent,
// so a patch cannot clear a date, uuid or enum.
type SubscriptionsPatch struct {
	CustomerId *string                   `json:"customerId,omitempty"`
	PlanId     *string                   `json:"planId,omitempty"`
	Status     *SubscriptionsStatusValue `json:"status,omitempty"`
	Mrr        *string                   `json:"mrr,omitempty"`
	StartedOn  *string                   `json:"startedOn,omitempty"`
	RenewsOn   *string                   `json:"renewsOn,omitempty"`
}

// ====== Subscriptions column references ======

var (
	SubscriptionsID         = framework.NewUUIDColumn("id")
	SubscriptionsCustomerId = framework.NewUUIDColumn("customer_id")
	SubscriptionsPlanId     = framework.NewUUIDColumn("plan_id")
	SubscriptionsStatus     = framework.NewEnumColumn[SubscriptionsStatusValue]("status")
	SubscriptionsMrr        = framework.NewFloatColumn("mrr")
	SubscriptionsStartedOn  = framework.NewTimestampColumn("started_on")
	SubscriptionsRenewsOn   = framework.NewTimestampColumn("renews_on")
	SubscriptionsUserId     = framework.NewStringColumn("user_id")
)

// Subscriptions include names: pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	SubscriptionsInclCustomer = "customer"
	SubscriptionsInclPlan     = "plan"
)

// SubscriptionsRepo is the typed repository for subscriptions rows.
// Event helpers: OnSubscriptionsCreated/OnSubscriptionsUpdated/OnSubscriptionsDeleted in this package.
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

// Handler returns the underlying CrudHandler: useful for advanced wiring or
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

// row decodes one CRUD result.
func (r *SubscriptionsRepo) row(out map[string]any) (*Subscriptions, error) {
	var row Subscriptions
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// rows decodes a batch result, keeping its order.
func (r *SubscriptionsRepo) rows(outs []map[string]any) ([]*Subscriptions, error) {
	rows := make([]*Subscriptions, len(outs))
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
func (r *SubscriptionsRepo) Create(ctx context.Context, in SubscriptionsInput) (*Subscriptions, error) {
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
func (r *SubscriptionsRepo) Get(ctx context.Context, id string, includes ...string) (*Subscriptions, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// Update writes patch's non-nil fields onto the row with this id and
// returns the row as stored afterwards.
func (r *SubscriptionsRepo) Update(ctx context.Context, id string, patch SubscriptionsPatch) (*Subscriptions, error) {
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

// FirstOrCreate returns the first row matching match, or creates in and
// returns the stored row when none does.
func (r *SubscriptionsRepo) FirstOrCreate(ctx context.Context, in SubscriptionsInput, match framework.Condition) (*Subscriptions, error) {
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
func (r *SubscriptionsRepo) BatchCreate(ctx context.Context, ins []SubscriptionsInput) ([]*Subscriptions, error) {
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
func (r *SubscriptionsRepo) BatchUpdate(ctx context.Context, ids []string, patches []SubscriptionsPatch) ([]*Subscriptions, error) {
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
func (r *SubscriptionsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// Transition runs move on the row with this id and returns the row as
// stored afterwards: the one way an enforced state field changes. It runs
// as CrudHandler.RunTransition does, so a System move skips the permission
// check and a row outside the move's From answers a conflict.
func (r *SubscriptionsRepo) Transition(ctx context.Context, id string, move SubscriptionsMove) (*Subscriptions, error) {
	out, err := r.handler.RunTransition(ctx, id, string(move))
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// OnSubscriptionsCreated subscribes to entity.created events scoped to "subscriptions".
// Returns a cancel func; call it to remove the handler.
func OnSubscriptionsCreated(app *framework.App, fn func(ctx context.Context, row *Subscriptions) error) func() {
	return onEntityEvent[Subscriptions](app, "subscriptions", framework.EntityCreated, fn)
}

// OnSubscriptionsUpdated subscribes to entity.updated events scoped to "subscriptions".
func OnSubscriptionsUpdated(app *framework.App, fn func(ctx context.Context, row *Subscriptions) error) func() {
	return onEntityEvent[Subscriptions](app, "subscriptions", framework.EntityUpdated, fn)
}

// OnSubscriptionsDeleted subscribes to entity.deleted events scoped to "subscriptions". Callback
// receives the deleted row's id only: by the time the event fires the row
// has been removed (or soft-deleted).
func OnSubscriptionsDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return onEntityDeleted(app, "subscriptions", fn)
}

// extractSubscriptionsRecord unmarshals an event payload's "record" field into a
// *Subscriptions, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractSubscriptionsRecord(ev framework.Event, entityName string) (*Subscriptions, bool) {
	return extractEntityRecord[Subscriptions](ev, entityName)
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
		Scope: &framework.ScopeConfig{
			OwnerField: "user_id",
		},
		Exposure: &framework.ExposureConfig{
			CRUD: boolPtr(true),
			MCP:  true,
		},
		States:     &framework.StatesConfig{Field: "status", Transitions: []framework.Transition{{Key: "activate", Label: "Activate", From: []string{"trialing", "past_due"}, To: "active", Variant: "primary"}, {Key: "cancel", Label: "Cancel", From: []string{"trialing", "active", "past_due"}, To: "canceled", Variant: "danger"}}, Advisory: true},
		Display:    &framework.DisplayConfig{TitleFields: []string{"customer_id", "plan_id"}, Description: "Who is on which plan, and what they bring in.", Nav: &framework.EntityNav{Group: "billing", Icon: "repeat", Order: 2}, Views: []framework.ListView{{Key: "active", Label: "Active", Where: "status = \"active\""}, {Key: "trialing", Label: "Trialing", Where: "status = \"trialing\""}, {Key: "past_due", Label: "Past due", Where: "status = \"past_due\""}}, Facets: []string{"status"}, Fields: map[string]framework.FieldDisplay{"mrr": {Label: "MRR", Input: "money"}}},
		Properties: map[string]any{"label": "Subscriptions"},
	})
	_ = Subscriptions{}
}

func init() {
	registrars = append(registrars, registrar{order: 2, fn: registerSubscriptions})
}
