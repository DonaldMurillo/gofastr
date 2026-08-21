package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Tags struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ====== Tags column references ======

var (
	TagsID   = framework.NewUUIDColumn("id")
	TagsName = framework.NewStringColumn("name")
)

// TagsRepo is the typed repository for tags rows.
// Event helpers: OnTagsCreated/OnTagsUpdated/OnTagsDeleted in this package.
type TagsRepo struct {
	handler *framework.CrudHandler
}

// NewTagsRepo wires a typed repo against the App's "tags" entity. Panics if the
// entity hasn't been registered yet.
func NewTagsRepo(app *framework.App) *TagsRepo {
	entity, err := app.Registry.Get("tags")
	if err != nil {
		panic("entities: tags not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("tags")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &TagsRepo{handler: h}
}

// Handler returns the underlying CrudHandler, useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *TagsRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *TagsRepo) WithTx(tx *sql.Tx) *TagsRepo {
	h := *r.handler
	h.DB = tx
	return &TagsRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *TagsRepo) Create(ctx context.Context, row *Tags) error {
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
func (r *TagsRepo) Get(ctx context.Context, id string, includes ...string) (*Tags, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Tags
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *TagsRepo) Update(ctx context.Context, id string, row *Tags) error {
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
func (r *TagsRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *TagsRepo) Query() *framework.TypedQuery[Tags] {
	return framework.NewTypedQuery[Tags](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *TagsRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(TagsID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *TagsRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *TagsRepo) FirstOrCreate(ctx context.Context, row *Tags, match framework.Condition) (*Tags, error) {
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
func (r *TagsRepo) BatchCreate(ctx context.Context, rows []*Tags) ([]*Tags, error) {
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
func (r *TagsRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Tags) ([]*Tags, error) {
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
func (r *TagsRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnTagsCreated subscribes to entity.created events scoped to "tags".
// Returns a cancel func; call it to remove the handler.
func OnTagsCreated(app *framework.App, fn func(ctx context.Context, row *Tags) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractTagsRecord(ev, "tags")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnTagsUpdated subscribes to entity.updated events scoped to "tags".
func OnTagsUpdated(app *framework.App, fn func(ctx context.Context, row *Tags) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractTagsRecord(ev, "tags")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnTagsDeleted subscribes to entity.deleted events scoped to "tags". Callback
// receives the deleted row's id only, by the time the event fires the row
// has been removed (or soft-deleted).
func OnTagsDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "tags" {
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

// extractTagsRecord unmarshals an event payload's "record" field into a
// *Tags, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractTagsRecord(ev framework.Event, entityName string) (*Tags, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Tags
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}

// registerTags registers the "tags" entity with app.
func registerTags(app *framework.App) {
	app.Entity("tags", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Unique: true},
		},
		Public:     true,
		CRUD:       boolPtr(true),
		Properties: map[string]any{"icon": "tag", "label": "Tags"},
	})
	_ = Tags{}
}

func init() {
	registrars = append(registrars, registrar{order: 2, fn: registerTags})
}
