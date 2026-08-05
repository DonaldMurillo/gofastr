package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

type Tasks struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Done   bool   `json:"done,omitempty"`
	UserId string `json:"userId,omitempty"`
	Owner  *Users `json:"owner,omitempty"`
}

// ====== Tasks column references ======

var (
	TasksID     = framework.NewUUIDColumn("id")
	TasksTitle  = framework.NewStringColumn("title")
	TasksDone   = framework.NewBoolColumn("done")
	TasksUserId = framework.NewUUIDColumn("user_id")
)

// Tasks include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).
const (
	TasksInclOwner = "owner"
)

// TasksRepo is the typed repository for tasks rows.
// Event helpers: OnTasksCreated/OnTasksUpdated/OnTasksDeleted in this package.
type TasksRepo struct {
	handler *framework.CrudHandler
}

// NewTasksRepo wires a typed repo against the App's "tasks" entity. Panics if the
// entity hasn't been registered yet.
func NewTasksRepo(app *framework.App) *TasksRepo {
	entity, err := app.Registry.Get("tasks")
	if err != nil {
		panic("entities: tasks not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("tasks")
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &TasksRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *TasksRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *TasksRepo) WithTx(tx *sql.Tx) *TasksRepo {
	h := *r.handler
	h.DB = tx
	return &TasksRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *TasksRepo) Create(ctx context.Context, row *Tasks) error {
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
func (r *TasksRepo) Get(ctx context.Context, id string, includes ...string) (*Tasks, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row Tasks
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *TasksRepo) Update(ctx context.Context, id string, row *Tasks) error {
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
func (r *TasksRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *TasksRepo) Query() *framework.TypedQuery[Tasks] {
	return framework.NewTypedQuery[Tasks](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *TasksRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(TasksID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *TasksRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *TasksRepo) FirstOrCreate(ctx context.Context, row *Tasks, match framework.Condition) (*Tasks, error) {
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
func (r *TasksRepo) BatchCreate(ctx context.Context, rows []*Tasks) ([]*Tasks, error) {
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
func (r *TasksRepo) BatchUpdate(ctx context.Context, ids []string, rows []*Tasks) ([]*Tasks, error) {
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
func (r *TasksRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

// OnTasksCreated subscribes to entity.created events scoped to "tasks".
// Returns a cancel func; call it to remove the handler.
func OnTasksCreated(app *framework.App, fn func(ctx context.Context, row *Tasks) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractTasksRecord(ev, "tasks")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnTasksUpdated subscribes to entity.updated events scoped to "tasks".
func OnTasksUpdated(app *framework.App, fn func(ctx context.Context, row *Tasks) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractTasksRecord(ev, "tasks")
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// OnTasksDeleted subscribes to entity.deleted events scoped to "tasks". Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func OnTasksDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != "tasks" {
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

// extractTasksRecord unmarshals an event payload's "record" field into a
// *Tasks, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extractTasksRecord(ev framework.Event, entityName string) (*Tasks, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v Tasks
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}

// registerTasks registers the "tasks" entity with app.
func registerTasks(app *framework.App) {
	app.Entity("tasks", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true, Max: floatPtr(200)},
			{Name: "done", Type: schema.Bool, Default: false},
			{Name: "user_id", Type: schema.Relation, To: "users"},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "owner", Entity: "users", ForeignKey: "user_id"},
		},
		OwnerField: "user_id",
		CRUD:       boolPtr(true),
		MCP:        true,
		Properties: map[string]any{"icon": "check-square", "label": "Tasks"},
	})
	_ = Tasks{}
}

func init() {
	registrars = append(registrars, registrar{order: 1, fn: registerTasks})
}
