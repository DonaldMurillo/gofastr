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

// ReviewsInput is the create payload for ReviewsRepo. A required field with no
// default is always sent; a nil field takes its default, and new(false)
// stores false.
type ReviewsInput struct {
	ProductId  string  `json:"productId"`
	AuthorName string  `json:"authorName"`
	Rating     int     `json:"rating"`
	Title      *string `json:"title,omitempty"`
	Body       *string `json:"body,omitempty"`
	Verified   *bool   `json:"verified,omitempty"`
}

// ReviewsPatch is the update payload for ReviewsRepo: a nil field is left as
// stored, and a pointer to false, 0 or "" writes it to a bool, int,
// float, string or text field. "" for any other type counts as absent,
// so a patch cannot clear a date, uuid or enum.
type ReviewsPatch struct {
	ProductId  *string `json:"productId,omitempty"`
	AuthorName *string `json:"authorName,omitempty"`
	Rating     *int    `json:"rating,omitempty"`
	Title      *string `json:"title,omitempty"`
	Body       *string `json:"body,omitempty"`
	Verified   *bool   `json:"verified,omitempty"`
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

// Reviews include names: pass to framework.TypedQuery.Include or repo.Get(..., includes...).
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

// Handler returns the underlying CrudHandler: useful for advanced wiring or
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

// row decodes one CRUD result.
func (r *ReviewsRepo) row(out map[string]any) (*Reviews, error) {
	var row Reviews
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// rows decodes a batch result, keeping its order.
func (r *ReviewsRepo) rows(outs []map[string]any) ([]*Reviews, error) {
	rows := make([]*Reviews, len(outs))
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
func (r *ReviewsRepo) Create(ctx context.Context, in ReviewsInput) (*Reviews, error) {
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
func (r *ReviewsRepo) Get(ctx context.Context, id string, includes ...string) (*Reviews, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// Update writes patch's non-nil fields onto the row with this id and
// returns the row as stored afterwards.
func (r *ReviewsRepo) Update(ctx context.Context, id string, patch ReviewsPatch) (*Reviews, error) {
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

// FirstOrCreate returns the first row matching match, or creates in and
// returns the stored row when none does.
func (r *ReviewsRepo) FirstOrCreate(ctx context.Context, in ReviewsInput, match framework.Condition) (*Reviews, error) {
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
func (r *ReviewsRepo) BatchCreate(ctx context.Context, ins []ReviewsInput) ([]*Reviews, error) {
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
func (r *ReviewsRepo) BatchUpdate(ctx context.Context, ids []string, patches []ReviewsPatch) ([]*Reviews, error) {
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
func (r *ReviewsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnReviewsCreated subscribes to entity.created events scoped to "reviews".
// Returns a cancel func; call it to remove the handler.
func OnReviewsCreated(app *framework.App, fn func(ctx context.Context, row *Reviews) error) func() {
	return onEntityEvent[Reviews](app, "reviews", framework.EntityCreated, fn)
}

// OnReviewsUpdated subscribes to entity.updated events scoped to "reviews".
func OnReviewsUpdated(app *framework.App, fn func(ctx context.Context, row *Reviews) error) func() {
	return onEntityEvent[Reviews](app, "reviews", framework.EntityUpdated, fn)
}

// OnReviewsDeleted subscribes to entity.deleted events scoped to "reviews". Callback
// receives the deleted row's id only: by the time the event fires the row
// has been removed (or soft-deleted).
func OnReviewsDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return onEntityDeleted(app, "reviews", fn)
}

// extractReviewsRecord unmarshals an event payload's "record" field into a
// *Reviews, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractReviewsRecord(ev framework.Event, entityName string) (*Reviews, bool) {
	return extractEntityRecord[Reviews](ev, entityName)
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
		Exposure: &framework.ExposureConfig{
			CRUD:   boolPtr(true),
			MCP:    true,
			Access: framework.AccessControl{Create: "reviews:write", Update: "reviews:write", Delete: "reviews:admin"},
		},
		Indices: []framework.Index{
			{Name: "idx_reviews_product", Columns: []string{"product_id"}},
		},
	})
	_ = Reviews{}
}

func init() {
	registrars = append(registrars, registrar{order: 4, fn: registerReviews})
}
