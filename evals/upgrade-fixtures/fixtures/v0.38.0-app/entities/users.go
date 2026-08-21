package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Users struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

// ====== Users column references ======

var (
	UsersID    = framework.NewUUIDColumn("id")
	UsersName  = framework.NewStringColumn("name")
	UsersEmail = framework.NewStringColumn("email")
)

// UsersRepo is the typed repository for users rows.
// Event helpers: OnUsersCreated/OnUsersUpdated/OnUsersDeleted in this package.
type UsersRepo struct {
	handler *framework.CrudHandler
}

// NewUsersRepo wires a typed repo against the App's "users" entity. Panics if the
// entity hasn't been registered yet.
func NewUsersRepo(app *framework.App) *UsersRepo {
	entity, err := app.Registry.Get("users")
	if err != nil {
		panic("entities: users not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("users")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &UsersRepo{handler: h}
}

// Handler returns the underlying CrudHandler, useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *UsersRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *UsersRepo) WithTx(tx *sql.Tx) *UsersRepo {
	h := *r.handler
	h.DB = tx
	return &UsersRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *UsersRepo) Create(ctx context.Context, row *Users) error {
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
func (r *UsersRepo) Get(ctx context.Context, id string, includes ...string) (*Users, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Users
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *UsersRepo) Update(ctx context.Context, id string, row *Users) error {
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
func (r *UsersRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *UsersRepo) Query() *framework.TypedQuery[Users] {
	return framework.NewTypedQuery[Users](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *UsersRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(UsersID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *UsersRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *UsersRepo) FirstOrCreate(ctx context.Context, row *Users, match framework.Condition) (*Users, error) {
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
func (r *UsersRepo) BatchCreate(ctx context.Context, rows []*Users) ([]*Users, error) {
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
func (r *UsersRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Users) ([]*Users, error) {
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
func (r *UsersRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnUsersCreated subscribes to entity.created events scoped to "users".
// Returns a cancel func; call it to remove the handler.
func OnUsersCreated(app *framework.App, fn func(ctx context.Context, row *Users) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractUsersRecord(ev, "users")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnUsersUpdated subscribes to entity.updated events scoped to "users".
func OnUsersUpdated(app *framework.App, fn func(ctx context.Context, row *Users) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractUsersRecord(ev, "users")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnUsersDeleted subscribes to entity.deleted events scoped to "users". Callback
// receives the deleted row's id only, by the time the event fires the row
// has been removed (or soft-deleted).
func OnUsersDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "users" {
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

// extractUsersRecord unmarshals an event payload's "record" field into a
// *Users, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractUsersRecord(ev framework.Event, entityName string) (*Users, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Users
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}

// registerUsers registers the "users" entity with app.
func registerUsers(app *framework.App) {
	app.Entity("users", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "email", Type: schema.String, Required: true, Unique: true},
		},
		Access:     framework.AccessControl{Read: "users:read", Create: "users:write", Update: "users:write", Delete: "users:admin"},
		CRUD:       boolPtr(true),
		Properties: map[string]any{"icon": "user", "label": "Users"},
	})
	_ = Users{}
}

func init() {
	registrars = append(registrars, registrar{order: 0, fn: registerUsers})
}
