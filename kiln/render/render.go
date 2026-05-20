package render

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
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
	if err := applyPages(app, w); err != nil {
		return Deferred{}, fmt.Errorf("kiln/render: pages: %w", err)
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

// entityConfig converts a world.Entity to a framework.EntityConfig by
// round-tripping through framework.EntityDeclaration. The two declarative
// shapes share the same JSON tags by design, so this is lossless for
// every field declared in v1.
func entityConfig(e *world.Entity) (framework.EntityConfig, error) {
	buf, err := json.Marshal(e)
	if err != nil {
		return framework.EntityConfig{}, fmt.Errorf("marshal: %w", err)
	}
	var decl framework.EntityDeclaration
	if err := json.Unmarshal(buf, &decl); err != nil {
		return framework.EntityConfig{}, fmt.Errorf("unmarshal as declaration: %w", err)
	}
	return decl.Config()
}

// widgetTag returns the script tag auto-injected into every Kiln-rendered
// page. Delegates to widget.RuntimeTag for content-hash cache-busting
// so a fresh build invalidates any stale runtime in the browser.
func widgetTag() string { return widget.RuntimeTag() }

func applyPages(app *framework.App, w *world.World) error {
	// Build a set of paths the entity CRUD layer has already registered
	// so we can skip a page that would collide. The framework's
	// auto-CRUD assumes it owns "/<table>" — if the agent declares a
	// page at the same URL, ServeMux panics. Pages take precedence
	// for HTML; the entity's CRUD remains reachable as JSON via its
	// other verbs (POST/PUT/DELETE) and via the framework's auto-MCP
	// surface.
	entityRoots := map[string]bool{}
	for _, ent := range app.Registry.All() {
		entityRoots["/"+ent.GetTable()] = true
	}

	paths := make([]string, 0, len(w.Pages))
	for p := range w.Pages {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, path := range paths {
		page := w.Pages[path]
		if page == nil {
			continue
		}
		// Conflict guard: if the path was already claimed by a CRUD list
		// route, register the page on a content-negotiated wrapper that
		// only renders HTML for browser GETs and falls through to the
		// CRUD handler for JSON requests. We replace the existing route
		// since router.Handle would panic otherwise — but Router has no
		// "replace" API, so the practical fix is: SKIP when there's a
		// CRUD entity at the same root, and emit a warning. Agents
		// should namespace pages (e.g., /posts/list) — the skill flags
		// this collision so they self-correct.
		if entityRoots[path] {
			fmt.Fprintf(os.Stderr, "[kiln/render] page %q skipped: collides with entity CRUD list endpoint. "+
				"Use a different page path (e.g. %q) or remove the entity.\n", path, path+"/list")
			continue
		}
		p := page
		// Recover from any other ServeMux registration panic (e.g. two
		// pages claim the same path, or a custom route does). Live's
		// rebuild logs and continues — a partial app is better than a
		// dead one for build-mode iteration.
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					fmt.Fprintf(os.Stderr, "[kiln/render] page %q registration panic: %v\n", path, rec)
				}
			}()
			app.Router().Get(path, http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
				rw.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(rw, renderFullPage(p))
			}))
		}()
	}
	return nil
}

// renderFullPage wraps the page tree in a full HTML document and injects
// the floating chat widget so every Kiln-served page surfaces the
// agent in its corner.
func renderFullPage(p *world.Page) string {
	title := p.Title
	if title == "" {
		title = p.Path
	}
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>` + escapeText(title) + `</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<link rel="stylesheet" href="/kiln/theme.css">
</head>
<body class="kiln-app">
<main class="kiln-page">
` + string(RenderNode(p.Tree)) + `
</main>
` + widgetTag() + `
</body>
</html>`
}

func escapeText(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch r {
		case '<':
			out = append(out, []byte("&lt;")...)
		case '>':
			out = append(out, []byte("&gt;")...)
		case '&':
			out = append(out, []byte("&amp;")...)
		case '"':
			out = append(out, []byte("&quot;")...)
		default:
			out = append(out, []byte(string(r))...)
		}
	}
	return string(out)
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
