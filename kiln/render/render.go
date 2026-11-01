package render

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
	"uuid"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/effect"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Apply registers every world surface onto app: entities (with CRUD),
// pages, custom routes, hooks, seeds, and middleware. Hooks and routes
// carry declarative actions that effect evaluates; entity endpoints carry
// actions too, but rendering a custom entity endpoint needs run-then-
// respond semantics plus entity-row scope binding that kiln/effect does
// not provide, so applyEntities screams (slog.Warn) per dropped endpoint
// rather than mounting a handler that would 500. Those endpoints still
// graduate to owned-Go handler stubs via freeze.
func Apply(app *framework.App, w *world.World) error {
	if app == nil {
		return fmt.Errorf("kiln/render: nil app")
	}
	if w == nil {
		return fmt.Errorf("kiln/render: nil world")
	}
	if err := applyAppConfig(app, w.App); err != nil {
		return fmt.Errorf("kiln/render: app config: %w", err)
	}
	if err := applyEntities(app, w); err != nil {
		return fmt.Errorf("kiln/render: entities: %w", err)
	}
	if err := applyMiddleware(app, w); err != nil {
		return fmt.Errorf("kiln/render: middleware: %w", err)
	}
	if err := applyHooks(app, w); err != nil {
		return fmt.Errorf("kiln/render: hooks: %w", err)
	}
	if err := applyRoutes(app, w); err != nil {
		return fmt.Errorf("kiln/render: routes: %w", err)
	}
	if err := applyUIHostPages(app, w); err != nil {
		return fmt.Errorf("kiln/render: pages: %w", err)
	}
	return nil
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
				slog.Error("kiln render", "err", err)
				http.Error(rw, "render failed", http.StatusInternalServerError)
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
	// Sort by name for deterministic registration order, matters for
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
		// Entity endpoints carry declarative actions, but mounting one needs
		// run-then-respond semantics plus entity-row scope binding that
		// kiln/effect does not provide (routes can use effect.Resolve because
		// they only produce a response; an endpoint action like set_field
		// mutates and then must respond). Rather than silently dropping them,
		// scream per endpoint: they are NOT served by the live build-mode app,
		// but they still graduate to owned-Go handler stubs via freeze.
		for _, ep := range ent.Endpoints {
			slog.Warn("kiln/render: entity endpoint not mounted in build-mode preview",
				"entity", ent.Name, "method", ep.Method, "path", ep.Path, "name", ep.Name,
				"reason", "declarative entity endpoints graduate to owned-Go handler stubs via freeze; the live build-mode app does not serve them")
		}
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
	decl := framework.EntityDeclaration{Exposure:
	// Every kiln entity is public, even one declaring OwnerField or
	// MultiTenant, which hard rule 6 forbids anywhere else. Kiln has no
	// session concept to scope against, and this is a deliberate
	// exception rather than an oversight: kiln is the in-app build-mode
	// runtime for someone assembling a world over HTTP, not a way to
	// serve an application. A kiln world is a preview, treat its
	// database as readable by anyone who can reach the port, and freeze
	// to a real app (which does honour these fields) before it holds
	// anything that matters.
	&framework.ExposureDeclaration{Public: true, CRUD: e.CRUD, MCP: e.MCP}, Name: e.Name,
		Table: e.Table, Scope: &framework.ScopeDeclaration{SoftDelete: e.SoftDelete, MultiTenant: e.MultiTenant, OwnerField: e.OwnerField, CrossOwnerRead: e.CrossOwnerRead}, SearchFields: append([]string(nil), e.SearchFields...),
		Timestamps: e.Timestamps, Pagination: &framework.PaginationDeclaration{CursorField: e.CursorField, CursorFields: append([]string(nil), e.CursorFields...)}, Properties: e.Properties,
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
		var fk []string
		if r.ForeignKey != "" {
			fk = []string{r.ForeignKey}
		}
		decl.Relations = append(decl.Relations, framework.Relation{
			Type: relationType, Name: r.Name, Entity: target,
			ForeignKey: fk, Through: r.Through, LocalKey: r.LocalKey,
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
		decl.Exposure.Access = &framework.AccessDeclaration{
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
// the migration step: Apply registers the entities, the caller migrates,
// the caller seeds.
//
// A seed names its entity, and its rows go into that entity's table
// (world.Entity.TableName: the declared Table, else the framework's
// default), so w must carry the entities its seeds name. The INSERT used
// to target the entity name itself, which is a different table for
// {name:"post", table:"posts"} and for every camelCase name.
//
// Each seed's rows go in one transaction: a seed the database refuses
// part way lands none of its rows, so the caller can report the refusal
// without rebuilding the database (kiln/live used to drop every table to
// clear a half-applied seed, taking the CRUD rows with it).
func ApplySeeds(db *sql.DB, w *world.World) error {
	if db == nil || len(w.Seeds) == 0 {
		return nil
	}
	for i, s := range w.Seeds {
		if s == nil {
			continue
		}
		if err := applySeed(db, w, s); err != nil {
			return fmt.Errorf("seed %d (%s): %w", i, s.Entity, err)
		}
	}
	return nil
}

func applySeed(db *sql.DB, w *world.World, s *world.Seed) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := insertSeed(tx, w, s); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func insertSeed(tx *sql.Tx, w *world.World, s *world.Seed) error {
	if s.Entity == "" {
		return fmt.Errorf("empty entity")
	}
	ent, ok := w.Entities[s.Entity]
	if !ok || ent == nil {
		return fmt.Errorf("seed entity %q is not in the world", s.Entity)
	}
	table, err := query.SafeIdent(ent.TableName())
	if err != nil {
		return fmt.Errorf("seed entity table: %w", err)
	}
	auto, jsonCols, err := seedFields(ent)
	if err != nil {
		return err
	}
	for _, row := range s.Rows {
		if len(row) == 0 {
			continue
		}
		folded := make(map[string]bool, len(row))
		for k := range row {
			if _, err := query.SafeIdent(k); err != nil {
				return fmt.Errorf("seed column: %w", err)
			}
			// Two keys that fold to one column would both reach the
			// INSERT, and SQLite accepts that and keeps one silently.
			if folded[strings.ToLower(k)] { //gofastr:allow(asciifold) k cleared SafeIdent, an ASCII-only identifier pattern
				return fmt.Errorf("seed column %q is named twice", k)
			}
			folded[strings.ToLower(k)] = true //gofastr:allow(asciifold) k cleared SafeIdent, an ASCII-only identifier pattern
		}
		row = withAutoFields(row, auto)
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
			if jsonCols[c] {
				if args[i], err = seedJSONValue(row[c]); err != nil {
					return fmt.Errorf("seed column %q: %w", c, err)
				}
			}
		}
		q := buildInsert(table, cols, placeholders)
		if _, err := tx.Exec(q, args...); err != nil {
			return fmt.Errorf("insert %v: %w", row, err)
		}
	}
	return nil
}

// seedFields returns the entity's UUID and timestamp fields, the values
// crud's create path generates, which a seed row's raw INSERT would
// otherwise leave NULL (an implicit id is a TEXT primary key, and SQLite
// stores NULL in one), and its JSON columns, whose decoded values the
// INSERT must encode first. An AutoIncrement field is left to the
// database. Define panics on a config it refuses, so that is recovered
// into the seed's error.
func seedFields(ent *world.Entity) (auto []schema.Field, jsonCols map[string]bool, err error) {
	cfg, err := entityConfig(ent)
	if err != nil {
		return nil, nil, fmt.Errorf("seed entity %q: %w", ent.Name, err)
	}
	defer func() {
		if rec := recover(); rec != nil {
			auto, jsonCols, err = nil, nil, fmt.Errorf("seed entity %q: %s", ent.Name, textsafe.Recovered(rec))
		}
	}()
	jsonCols = map[string]bool{}
	for _, f := range framework.Define(ent.Name, cfg).GetFields() {
		if f.AutoGenerate == schema.AutoUUID || f.AutoGenerate == schema.AutoTimestamp {
			auto = append(auto, f)
		}
		if f.Type == schema.JSON {
			jsonCols[f.Name] = true
		}
	}
	return auto, jsonCols, nil
}

// seedJSONValue is a json column's seed value as the text the driver
// binds. database/sql refuses a map or a non-byte slice, so a decoded
// object or array is encoded, as crud's write path encodes it; a string
// is JSON text already and is stored as written (2026-10-27 K21).
func seedJSONValue(v any) (any, error) {
	switch v.(type) {
	case nil, string, []byte:
		return v, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(raw), nil
}

// withAutoFields returns row with a generated value for each auto field
// it leaves out or sets to null. A value the row gives is kept, under
// whatever case it was spelled in: a seed names its own ids so other
// seeds can reference them. Keys have cleared SafeIdent and are unique
// after folding (insertSeed), so the fold match is exact.
func withAutoFields(row map[string]any, auto []schema.Field) map[string]any {
	out := maps.Clone(row)
	for _, f := range auto {
		key := f.Name
		for k := range out {
			if strings.EqualFold(k, f.Name) {
				key = k
				break
			}
		}
		if out[key] != nil {
			continue
		}
		delete(out, key)
		if f.AutoGenerate == schema.AutoUUID {
			out[f.Name] = uuid.NewV4().String()
		} else {
			out[f.Name] = schema.FormatTimestamp(seedNow())
		}
	}
	return out
}

// StampSeed returns s with the generated values the journal must keep
// written into its rows: a UUID for each auto-UUID field (the implicit
// id included) and a timestamp for each declared auto-timestamp field,
// wherever a row leaves one out or sets it null. kiln/live journals the
// stamped seed, so replay inserts the ids the live database handed out
// and a seed that references one (a book naming its author's id) still
// lands after a restart; ApplySeeds alone generated fresh ids on every
// replay.
//
// The implicit created_at/updated_at are not written: they are not
// fields of the entity, and the app spec generator refuses a seed key
// that names no declared column, so a stamped one would fail the freeze.
// ApplySeeds stamps them at insert, and nothing references them. An
// empty row stays empty (insertSeed skips it), a seed whose entity is
// not in ents comes back as it was (ApplySeeds refuses it), and s itself
// is not changed.
func StampSeed(ents map[string]*world.Entity, s *world.Seed) (*world.Seed, error) {
	if s == nil {
		return nil, nil
	}
	ent := ents[s.Entity]
	if ent == nil {
		return s, nil
	}
	auto, _, err := seedFields(ent)
	if err != nil {
		return nil, err
	}
	var keep []schema.Field
	for _, f := range auto {
		if f.Name == "id" || slices.ContainsFunc(ent.Fields, func(d world.Field) bool { return d.Name == f.Name }) {
			keep = append(keep, f)
		}
	}
	if len(keep) == 0 {
		return s, nil
	}
	out := *s
	out.Rows = make([]map[string]any, len(s.Rows))
	for i, row := range s.Rows {
		if len(row) == 0 {
			out.Rows[i] = row
			continue
		}
		out.Rows[i] = withAutoFields(row, keep)
	}
	return &out, nil
}

// seedNow is the clock withAutoFields stamps timestamps from; a test
// pins it to check the stored layout.
var seedNow = time.Now

// buildInsert assumes table and cols have already cleared query.SafeIdent,
// insertSeed is the only caller and validates both up front. Seed entity
// names and row keys are agent-authored world IR, so validation (not just
// quoting) is the contract: kiln used to carry a private quoteIdent that
// escaped `"` and then wrote each rune with `append(out, byte(r))`,
// truncating to the low byte AFTER the escape test, so U+2022 and every
// other rune ending in 0x22 became a literal quote that closed the
// identifier. core/query is the one correct implementation; there is no
// second copy here any more.
func buildInsert(table string, cols, placeholders []string) string {
	return "INSERT INTO " + query.QuoteFolded(table) + " (" + joinIdents(cols) + ") VALUES (" + join(placeholders) + ")"
}

func joinIdents(cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = query.QuoteFolded(c)
	}
	return strings.Join(out, ", ")
}

func join(parts []string) string { return strings.Join(parts, ", ") }

func applyMiddleware(app *framework.App, w *world.World) error {
	for _, mw := range w.Middleware {
		if mw == nil {
			continue
		}
		f, ok := middlewareCatalog[mw.Name]
		if !ok {
			return fmt.Errorf("unknown middleware %q (catalog: %v)", mw.Name, slices.Sorted(maps.Keys(middlewareCatalog)))
		}
		built, err := f(mw.Cfg)
		if err != nil {
			return fmt.Errorf("middleware %q: %w", mw.Name, err)
		}
		app.Router().Use(built)
	}
	return nil
}
