package render

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/effect"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Apply registers every world surface onto app. Equivalent to calling
// ApplyDetailed and discarding the deferred report.
func Apply(app *framework.App, w *world.World) error {
	_, err := ApplyDetailed(app, w)
	return err
}

// Deferred reports any surfaces Apply skipped or downgraded. Reserved for
// future cases (e.g., unsupported action kinds); Phase 3 wires hooks and
// routes through the expression evaluator so a full world is rendered.
type Deferred struct {
	Hooks  []*world.Hook
	Routes []*world.Route
}

// ApplyDetailed is Apply but returns the Deferred report.
func ApplyDetailed(app *framework.App, w *world.World) (Deferred, error) {
	if app == nil {
		return Deferred{}, fmt.Errorf("kiln/render: nil app")
	}
	if w == nil {
		return Deferred{}, fmt.Errorf("kiln/render: nil world")
	}
	if err := applyAppConfig(app, w.App); err != nil {
		return Deferred{}, fmt.Errorf("kiln/render: app config: %w", err)
	}
	if err := applyEntities(app, w); err != nil {
		return Deferred{}, fmt.Errorf("kiln/render: entities: %w", err)
	}
	if err := applyMiddleware(app, w); err != nil {
		return Deferred{}, fmt.Errorf("kiln/render: middleware: %w", err)
	}
	if err := applyHooks(app, w); err != nil {
		return Deferred{}, fmt.Errorf("kiln/render: hooks: %w", err)
	}
	if err := applyRoutes(app, w); err != nil {
		return Deferred{}, fmt.Errorf("kiln/render: routes: %w", err)
	}
	if err := applyUIHostPages(app, w); err != nil {
		return Deferred{}, fmt.Errorf("kiln/render: pages: %w", err)
	}
	return Deferred{}, nil
}

func applyHooks(app *framework.App, w *world.World) error {
	for _, h := range w.Hooks {
		if h == nil {
			continue
		}
		if h.Entity == "" {
			return fmt.Errorf("hook %q: missing entity", h.ID)
		}
		hookType, err := mapHookType(h.When)
		if err != nil {
			return fmt.Errorf("hook %q: %w", h.ID, err)
		}
		hk := h // capture
		app.HookRegistry(h.Entity).RegisterHook(hookType, func(ctx context.Context, data any) error {
			return effect.RunHook(ctx, hk, effect.Scope{Entity: data})
		})
	}
	return nil
}

func mapHookType(when string) (framework.HookType, error) {
	switch when {
	case "before_create":
		return framework.BeforeCreate, nil
	case "after_create":
		return framework.AfterCreate, nil
	case "before_update":
		return framework.BeforeUpdate, nil
	case "after_update":
		return framework.AfterUpdate, nil
	case "before_delete":
		return framework.BeforeDelete, nil
	case "after_delete":
		return framework.AfterDelete, nil
	case "before_list":
		return framework.BeforeList, nil
	case "after_list":
		return framework.AfterList, nil
	}
	return 0, fmt.Errorf("unknown hook event %q", when)
}

func applyRoutes(app *framework.App, w *world.World) error {
	for _, r := range w.Routes {
		if r == nil || r.Method == "" || r.Path == "" {
			continue
		}
		rt := r // capture
		app.Router().Handle(rt.Method, rt.Path, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			scope := effect.Scope{
				Ctx: map[string]any{
					"path":   req.URL.Path,
					"method": req.Method,
				},
			}
			resp, err := effect.Resolve(req.Context(), rt.Action, scope)
			if err != nil {
				http.Error(rw, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := resp.WriteTo(rw); err != nil {
				return
			}
		}))
	}
	return nil
}

func applyAppConfig(app *framework.App, c world.AppConfig) error {
	app.Config.Name = c.Name
	app.Config.DebugEndpoints = c.DebugEndpoints
	app.Config.APIPrefix = strings.Trim(c.APIPrefix, "/")
	app.Config.NoLLMMD = !c.LLMMD
	switch c.JSONCase {
	case "", "camel", "camelCase":
		app.Config.JSONCase = framework.CaseCamel
	case "snake", "snake_case":
		app.Config.JSONCase = framework.CaseSnake
	default:
		return fmt.Errorf("unknown json_case %q (want camel|snake)", c.JSONCase)
	}
	return nil
}

func applyEntities(app *framework.App, w *world.World) error {
	// Sort by name for deterministic registration order — matters for
	// migrate file naming and for deterministic test output.
	names := make([]string, 0, len(w.Entities))
	for n := range w.Entities {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		ent := w.Entities[name]
		if ent == nil {
			continue
		}
		cfg, err := entityConfig(ent)
		if err != nil {
			return fmt.Errorf("entity %q: %w", name, err)
		}
		// Endpoints carry declarative actions; framework.Endpoint expects a
		// Go handler we don't have until Phase 3. Strip them here so the
		// entity registers cleanly; Apply's caller sees the unevaluated
		// endpoints via Deferred (extension: TBD when Phase 3 lands).
		cfg.Endpoints = nil
		app.Entity(ent.Name, cfg)
	}
	return nil
}

// entityConfig converts the JSON-only world shape into the framework's
// current declaration contract. It is intentionally explicit: relation
// types are strings in the authoring IR and enums in framework code, so a
// JSON round-trip silently drifted as the framework surface evolved.
func entityConfig(e *world.Entity) (framework.EntityConfig, error) {
	decl := framework.EntityDeclaration{
		// Kiln has no session hook, so preserve its pre-existing open CRUD explicitly.
		// TODO(kiln): grow an auth/access concept in the IR.
		Public:         true,
		Name:           e.Name,
		Table:          e.Table,
		SoftDelete:     e.SoftDelete,
		MultiTenant:    e.MultiTenant,
		OwnerField:     e.OwnerField,
		CrossOwnerRead: e.CrossOwnerRead,
		SearchFields:   append([]string(nil), e.SearchFields...),
		Timestamps:     e.Timestamps,
		CRUD:           e.CRUD,
		MCP:            e.MCP,
		CursorField:    e.CursorField,
		CursorFields:   append([]string(nil), e.CursorFields...),
		Properties:     e.Properties,
	}
	for _, f := range e.Fields {
		decl.Fields = append(decl.Fields, framework.FieldDeclaration{
			Name: f.Name, Type: f.Type, Required: f.Required, Unique: f.Unique,
			Default: f.Default, AutoGenerate: f.AutoGenerate, ReadOnly: f.ReadOnly,
			Hidden: f.Hidden, Max: f.Max, Min: f.Min, Pattern: f.Pattern,
			Values: append([]string(nil), f.Values...), To: f.To, Many: f.Many,
		})
	}
	for _, r := range e.Relations {
		relationType, err := relationType(r.Type)
		if err != nil {
			return framework.EntityConfig{}, err
		}
		target := r.Entity
		if target == "" {
			target = r.To // pre-parity journal compatibility
		}
		decl.Relations = append(decl.Relations, framework.Relation{
			Type: relationType, Name: r.Name, Entity: target,
			ForeignKey: r.ForeignKey, Through: r.Through, LocalKey: r.LocalKey,
			ForeignKeyTarget: r.ForeignKeyTarget,
		})
	}
	for _, ix := range e.Indices {
		decl.Indices = append(decl.Indices, framework.Index{
			Name: ix.Name, Columns: append([]string(nil), ix.Columns...),
			Unique: ix.Unique,
		})
	}
	if e.Access != nil {
		decl.Access = &framework.AccessDeclaration{
			Read: e.Access.Read, Create: e.Access.Create,
			Update: e.Access.Update, Delete: e.Access.Delete,
		}
	}
	return decl.Config()
}

func relationType(value string) (framework.RelationType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "has_one":
		return framework.RelHasOne, nil
	case "has_many":
		return framework.RelHasMany, nil
	case "", "belongs_to", "many_to_one":
		return framework.RelManyToOne, nil
	case "many_to_many":
		return framework.RelManyToMany, nil
	default:
		return 0, fmt.Errorf("unknown relation type %q", value)
	}
}

// ApplySeeds inserts the world's seed rows into db. It's a separate call
// because seeding requires migrations to have run first, and Kiln owns
// the migration step (Phase 4) — Apply registers the entities, the
// caller migrates, the caller seeds.
func ApplySeeds(db *sql.DB, w *world.World) error {
	if db == nil || len(w.Seeds) == 0 {
		return nil
	}
	for i, s := range w.Seeds {
		if s == nil {
			continue
		}
		if err := insertSeed(db, s); err != nil {
			return fmt.Errorf("seed %d (%s): %w", i, s.Entity, err)
		}
	}
	return nil
}

func insertSeed(db *sql.DB, s *world.Seed) error {
	if s.Entity == "" {
		return fmt.Errorf("empty entity")
	}
	for _, row := range s.Rows {
		if len(row) == 0 {
			continue
		}
		cols := make([]string, 0, len(row))
		for k := range row {
			cols = append(cols, k)
		}
		sort.Strings(cols)
		placeholders := make([]string, len(cols))
		args := make([]any, len(cols))
		for i, c := range cols {
			placeholders[i] = "?"
			args[i] = row[c]
		}
		query := buildInsert(s.Entity, cols, placeholders)
		if _, err := db.Exec(query, args...); err != nil {
			return fmt.Errorf("insert %v: %w", row, err)
		}
	}
	return nil
}

func buildInsert(table string, cols, placeholders []string) string {
	return "INSERT INTO " + quoteIdent(table) + " (" + joinIdents(cols) + ") VALUES (" + join(placeholders) + ")"
}

func joinIdents(cols []string) string {
	out := make([]byte, 0, 32)
	for i, c := range cols {
		if i > 0 {
			out = append(out, ',', ' ')
		}
		out = append(out, []byte(quoteIdent(c))...)
	}
	return string(out)
}

func join(parts []string) string {
	out := make([]byte, 0, 32)
	for i, p := range parts {
		if i > 0 {
			out = append(out, ',', ' ')
		}
		out = append(out, []byte(p)...)
	}
	return string(out)
}

// quoteIdent applies SQL identifier quoting. SQLite accepts double quotes;
// Kiln's runtime DB target is SQLite (Phase 4), so this is sufficient.
func quoteIdent(s string) string {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '"')
	for _, r := range s {
		if r == '"' {
			out = append(out, '"', '"')
			continue
		}
		out = append(out, byte(r))
	}
	out = append(out, '"')
	return string(out)
}

func applyMiddleware(app *framework.App, w *world.World) error {
	for _, mw := range w.Middleware {
		if mw == nil {
			continue
		}
		f, ok := middlewareCatalog[mw.Name]
		if !ok {
			return fmt.Errorf("unknown middleware %q (catalog: %v)", mw.Name, middlewareNames())
		}
		built, err := f(mw.Cfg)
		if err != nil {
			return fmt.Errorf("middleware %q: %w", mw.Name, err)
		}
		app.Router().Use(built)
	}
	return nil
}
