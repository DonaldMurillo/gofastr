package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Reviews struct {
	ID         string    `json:"id"`
	ProductId  string    `json:"productId,omitempty"`
	AuthorName string    `json:"authorName,omitempty"`
	Rating     int       `json:"rating,omitempty"`
	Title      string    `json:"title,omitempty"`
	Body       string    `json:"body,omitempty"`
	Verified   bool      `json:"verified,omitempty"`
	Product    *Products `json:"product,omitempty"`
}

// ====== Reviews column references ======

var (
	ReviewsID         = framework.NewUUIDColumn("id")
	ReviewsProductId  = framework.NewUUIDColumn("product_id")
	ReviewsAuthorName = framework.NewStringColumn("author_name")
	ReviewsRating     = framework.NewIntColumn("rating")
	ReviewsTitle      = framework.NewStringColumn("title")
	ReviewsBody       = framework.NewStringColumn("body")
	ReviewsVerified   = framework.NewBoolColumn("verified")
)

// Reviews include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	ReviewsInclProduct = "product"
)

// ReviewsRepo is the typed repository for reviews rows.
// Event helpers: OnReviewsCreated/OnReviewsUpdated/OnReviewsDeleted in this package.
type ReviewsRepo struct {
	handler *framework.CrudHandler
}

// NewReviewsRepo wires a typed repo against the App's "reviews" entity. Panics if the
// entity hasn't been registered yet.
func NewReviewsRepo(app *framework.App) *ReviewsRepo {
	entity, err := app.Registry.Get("reviews")
	if err != nil {
		panic("entities: reviews not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("reviews")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &ReviewsRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *ReviewsRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *ReviewsRepo) WithTx(tx *sql.Tx) *ReviewsRepo {
	h := *r.handler
	h.DB = tx
	return &ReviewsRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *ReviewsRepo) Create(ctx context.Context, row *Reviews) error {
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
func (r *ReviewsRepo) Get(ctx context.Context, id string, includes ...string) (*Reviews, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Reviews
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *ReviewsRepo) Update(ctx context.Context, id string, row *Reviews) error {
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
func (r *ReviewsRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *ReviewsRepo) Query() *framework.TypedQuery[Reviews] {
	return framework.NewTypedQuery[Reviews](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *ReviewsRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(ReviewsID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *ReviewsRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *ReviewsRepo) FirstOrCreate(ctx context.Context, row *Reviews, match framework.Condition) (*Reviews, error) {
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
func (r *ReviewsRepo) BatchCreate(ctx context.Context, rows []*Reviews) ([]*Reviews, error) {
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
func (r *ReviewsRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Reviews) ([]*Reviews, error) {
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
func (r *ReviewsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnReviewsCreated subscribes to entity.created events scoped to "reviews".
// Returns a cancel func; call it to remove the handler.
func OnReviewsCreated(app *framework.App, fn func(ctx context.Context, row *Reviews) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractReviewsRecord(ev, "reviews")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnReviewsUpdated subscribes to entity.updated events scoped to "reviews".
func OnReviewsUpdated(app *framework.App, fn func(ctx context.Context, row *Reviews) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractReviewsRecord(ev, "reviews")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnReviewsDeleted subscribes to entity.deleted events scoped to "reviews". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnReviewsDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "reviews" {
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

// extractReviewsRecord unmarshals an event payload's "record" field into a
// *Reviews, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractReviewsRecord(ev framework.Event, entityName string) (*Reviews, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Reviews
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}

// registerReviews registers the "reviews" entity with app.
func registerReviews(app *framework.App) {
	app.Entity("reviews", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "product_id", Type: schema.Relation, Required: true, To: "products"},
			{Name: "author_name", Type: schema.String, Required: true, Max: floatPtr(100)},
			{Name: "rating", Type: schema.Int, Required: true, Max: floatPtr(5), Min: floatPtr(1)},
			{Name: "title", Type: schema.String, Max: floatPtr(200)},
			{Name: "body", Type: schema.Text},
			{Name: "verified", Type: schema.Bool, Default: false},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "product", Entity: "products", ForeignKey: "product_id"},
		},
		CRUD: boolPtr(true),
		MCP:  true,
		Indices: []framework.Index{
			{Name: "idx_reviews_product", Columns: []string{"product_id"}},
		},
	})
	_ = Reviews{}
}

func init() {
	registrars = append(registrars, registrar{order: 4, fn: registerReviews})
}
