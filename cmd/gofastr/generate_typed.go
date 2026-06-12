package main

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework"
)

// renderColumns builds entities/columns.go — typed column constants and
// include-name constants for every entity. Codegen-only file; the framework
// itself defines NewStringColumn etc. so the generator just instantiates.
func renderColumns(decls []framework.EntityDeclaration) string {
	var sb strings.Builder
	sb.WriteString(`package entities

import (
	"github.com/DonaldMurillo/gofastr/framework"
)

`)
	for _, decl := range decls {
		struct_ := toCamelCase(decl.Name)
		sb.WriteString("// ====== " + struct_ + " column references ======\n\n")
		sb.WriteString("var (\n")
		// Always emit an ID column (PK) — the framework auto-adds it on Define.
		sb.WriteString(fmt.Sprintf("\t%sID = framework.NewUUIDColumn(\"id\")\n", struct_))
		for _, field := range decl.Fields {
			if field.Name == "id" {
				continue
			}
			sb.WriteString(fmt.Sprintf("\t%s%s = %s(%q)\n",
				struct_, toCamelCase(field.Name),
				columnConstructor(field.Type), field.Name))
		}
		sb.WriteString(")\n\n")

		// Include name constants per relation.
		if len(decl.Relations) > 0 {
			sb.WriteString("// " + struct_ + " include names — pass to framework.TypedQuery.Include or repo.Get(..., includes...).\n")
			sb.WriteString("const (\n")
			for _, rel := range decl.Relations {
				sb.WriteString(fmt.Sprintf("\t%sIncl%s = %q\n",
					struct_, toCamelCase(rel.Name), rel.Name))
			}
			sb.WriteString(")\n\n")
		}
	}
	return sb.String()
}

// columnConstructor maps a schema field type to the framework constructor
// that produces a typed column for it.
func columnConstructor(value string) string {
	switch strings.ToLower(value) {
	case "int", "integer":
		return "framework.NewIntColumn"
	case "float", "number", "decimal":
		return "framework.NewFloatColumn"
	case "bool", "boolean":
		return "framework.NewBoolColumn"
	case "timestamp", "datetime", "date":
		return "framework.NewTimestampColumn"
	case "uuid", "relation":
		return "framework.NewUUIDColumn"
	default:
		// String, Text, Enum, JSON, Image, File all get StringColumn — they're
		// all stored as TEXT and queried by string equality / LIKE.
		return "framework.NewStringColumn"
	}
}

// renderEvents builds entities/events.go — typed event subscription helpers
// for every entity. OnPostsCreated/OnPostsUpdated take *T callbacks;
// OnPostsDeleted gets the id string. Each returns a cancel function from
// EventBus.Subscribe so callers can unsubscribe.
func renderEvents(decls []framework.EntityDeclaration) string {
	var sb strings.Builder
	sb.WriteString(`package entities

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework"
)

`)
	for _, decl := range decls {
		struct_ := toCamelCase(decl.Name)
		sb.WriteString(fmt.Sprintf(`// On%sCreated subscribes to entity.created events scoped to %q.
// Returns a cancel func; call it to remove the handler.
func On%sCreated(app *framework.App, fn func(ctx context.Context, row *%s) error) func() {
	return app.Events().Subscribe(framework.EntityCreated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extract%sRecord(ev, %q)
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// On%sUpdated subscribes to entity.updated events scoped to %q.
func On%sUpdated(app *framework.App, fn func(ctx context.Context, row *%s) error) func() {
	return app.Events().Subscribe(framework.EntityUpdated, func(ctx context.Context, ev framework.Event) error {
		row, ok := extract%sRecord(ev, %q)
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// On%sDeleted subscribes to entity.deleted events scoped to %q. Callback
// receives the deleted row's id only — by the time the event fires the row
// has been removed (or soft-deleted).
func On%sDeleted(app *framework.App, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != %q {
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

`,
			struct_, decl.Name, struct_, struct_, struct_, decl.Name,
			struct_, decl.Name, struct_, struct_, struct_, decl.Name,
			struct_, decl.Name, struct_, decl.Name,
		))
	}

	// Shared extractor — once per file, not per entity.
	if len(decls) > 0 {
		first := toCamelCase(decls[0].Name)
		// Emit one extractor per struct type. They're all identical shape but
		// differ in the returned *T; codegen can't share via interface without
		// reflection so we generate one per entity.
		for _, decl := range decls {
			struct_ := toCamelCase(decl.Name)
			sb.WriteString(fmt.Sprintf(`// extract%sRecord unmarshals an event payload's "record" field into a
// *%s, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extract%sRecord(ev framework.Event, entityName string) (*%s, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entityName {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v %s
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}

`, struct_, struct_, struct_, struct_, struct_))
		}
		_ = first
	}
	return sb.String()
}

// renderRepos builds entities/repo.go — typed repositories per entity, each
// wrapping a CrudHandler so generated callers get Create/Get/Update/Delete +
// Query + WithTx without re-implementing CRUD plumbing.
func renderRepos(decls []framework.EntityDeclaration) string {
	var sb strings.Builder
	sb.WriteString(`package entities

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/framework"
)

`)
	for _, decl := range decls {
		struct_ := toCamelCase(decl.Name)
		sb.WriteString(fmt.Sprintf(`// %sRepo is the typed repository for %s rows.
type %sRepo struct {
	handler *framework.CrudHandler
}

// New%sRepo wires a typed repo against the App's %q entity. Panics if the
// entity hasn't been registered yet.
func New%sRepo(app *framework.App) *%sRepo {
	entity, err := app.Registry.Get(%q)
	if err != nil {
		panic("entities: %s not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry(%q)
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &%sRepo{handler: h}
}

// Handler returns the underlying CrudHandler — useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *%sRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *%sRepo) WithTx(tx *sql.Tx) *%sRepo {
	h := *r.handler
	h.DB = tx
	return &%sRepo{handler: &h}
}

// Create persists row and back-fills server-generated fields onto it.
func (r *%sRepo) Create(ctx context.Context, row *%s) error {
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
func (r *%sRepo) Get(ctx context.Context, id string, includes ...string) (*%s, error) {
	out, err := r.handler.GetOne(ctx, id, includes)
	if err != nil {
		return nil, err
	}
	var row %s
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// Update merges fields from row into the persisted record by id and refreshes
// row with the post-update state.
func (r *%sRepo) Update(ctx context.Context, id string, row *%s) error {
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
func (r *%sRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *%sRepo) Query() *framework.TypedQuery[%s] {
	return framework.NewTypedQuery[%s](r.handler)
}

// Exists reports whether a row with the given id is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *%sRepo) Exists(ctx context.Context, id string) (bool, error) {
	return r.Query().Where(%sID.Eq(id)).Exists(ctx)
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *%sRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate looks up a row by the given match condition and returns it
// if found. Otherwise inserts row (filling in its server-generated fields)
// and returns it.
func (r *%sRepo) FirstOrCreate(ctx context.Context, row *%s, match framework.Condition) (*%s, error) {
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
func (r *%sRepo) BatchCreate(ctx context.Context, rows []*%s) ([]*%s, error) {
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
func (r *%sRepo) BatchUpdate(ctx context.Context, ids []string, rows []*%s) ([]*%s, error) {
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
func (r *%sRepo) BatchDelete(ctx context.Context, ids []string) error {
	_, err := r.handler.BatchDeleteMany(ctx, ids)
	return err
}

`,
			// Repo struct doc + type
			struct_, decl.Name,
			struct_,
			// Constructor
			struct_, decl.Name, struct_, struct_, decl.Name, decl.Name, decl.Name, struct_,
			// Handler accessor
			struct_,
			// WithTx
			struct_, struct_, struct_,
			// Create
			struct_, struct_,
			// Get
			struct_, struct_, struct_,
			// Update
			struct_, struct_,
			// Delete
			struct_,
			// Query
			struct_, struct_, struct_,
			// Exists
			struct_, struct_,
			// Count
			struct_,
			// FirstOrCreate
			struct_, struct_, struct_,
			// BatchCreate
			struct_, struct_, struct_,
			// BatchUpdate
			struct_, struct_, struct_,
			// BatchDelete
			struct_,
		))
	}
	return sb.String()
}
