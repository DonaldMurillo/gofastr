package freeze

import (
	"encoding/json"
	"fmt"
	"go/token"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/settings"
	"github.com/DonaldMurillo/gofastr/internal/dsnredact"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// AppSpecYAML converts a Kiln world into the current gofastr.yml scaffold
// contract. world.json remains the lossless authoring snapshot; the app spec
// is the one-shot, generator-ready graduation artifact.
func AppSpecYAML(w *world.World) ([]byte, error) {
	if w == nil {
		return nil, fmt.Errorf("freeze: nil world")
	}
	if err := validateGraduation(w); err != nil {
		return nil, err
	}
	doc := normalizeYAMLValue(appSpecMap(w)).(map[string]any)
	if err := validateYAMLRepresentable(doc, ""); err != nil {
		return nil, err
	}
	var out strings.Builder
	writeYAMLMap(&out, doc, 0, topLevelOrder)
	return []byte(out.String()), nil
}

func validateGraduation(w *world.World) error {
	if _, err := freezeEndpoints(w); err != nil {
		return err
	}
	for _, e := range sortedEntities(w) {
		if e.MultiTenant {
			return fmt.Errorf("freeze: entity %q sets multi_tenant, but an app spec cannot choose the app-specific tenant resolver; use owner_field for per-user scoping or wire tenant middleware in owned Go before freezing", e.Name)
		}
	}
	for path, page := range w.Pages {
		if page != nil {
			if err := world.ValidatePageTree(page.Tree); err != nil {
				return fmt.Errorf("freeze: page %q: %w", path, err)
			}
		}
	}
	if w.App.Auth.Enabled && !w.App.Auth.DevMode && w.App.Auth.JWTSecret == "" {
		return fmt.Errorf("freeze: production auth requires app.auth.jwt_secret; set it or keep auth.dev_mode true for local development")
	}
	// Seeds must graduate only when the live runtime would apply them.
	// Every add_seed runs through kiln/render.ApplySeeds
	// (live.applySideEffects), which refuses a seed whose entity is not
	// in the world, inserts into that entity's table
	// (world.Entity.TableName: the declared table, else the framework's
	// default), and validates that table and every row key as a SQL
	// identifier; an app spec that ships a seed the preview refused makes
	// the frozen app fail at first boot — the exact divergence this gate
	// exists to prevent.
	for i, s := range w.Seeds {
		if s == nil {
			continue
		}
		if s.Entity == "" {
			return fmt.Errorf("freeze: seed %d: empty entity", i)
		}
		ent, ok := w.Entities[s.Entity]
		if !ok || ent == nil {
			return fmt.Errorf("freeze: seed %d targets entity %q, which is not in the world: the live runtime refuses it too", i, s.Entity)
		}
		if _, err := query.SafeIdent(ent.TableName()); err != nil {
			return fmt.Errorf("freeze: seed %d (%s) table: %w", i, s.Entity, err)
		}
		for _, row := range s.Rows {
			for k := range row {
				if _, err := query.SafeIdent(k); err != nil {
					return fmt.Errorf("freeze: seed %d (%s) column: %w", i, s.Entity, err)
				}
			}
		}
	}
	if _, err := mergeSeeds(w.Seeds); err != nil {
		return err
	}
	if w.App.PWA.Enabled {
		switch w.App.PWA.Display {
		case "", "standalone", "fullscreen", "minimal-ui", "browser":
		default:
			return fmt.Errorf("freeze: unsupported PWA display %q", w.App.PWA.Display)
		}
		for label, value := range map[string]string{"start_url": w.App.PWA.StartURL, "scope": w.App.PWA.Scope} {
			if value != "" && !strings.HasPrefix(value, "/") {
				return fmt.Errorf("freeze: app.pwa.%s must start with /", label)
			}
			// HasPrefix("/") alone admits scheme-relative and backslash
			// forms: browsers resolve "//evil.example/pwa" and
			// "/\evil.example/" against the attacker's origin, and the
			// graduated manifest is what the operator's browser installs.
			// A same-origin path starts with exactly one slash.
			if strings.HasPrefix(value, "//") || strings.HasPrefix(value, `/\`) {
				return fmt.Errorf("freeze: app.pwa.%s must be a same-origin path, got %q", label, value)
			}
		}
	}
	return nil
}

func appSpecMap(w *world.World) map[string]any {
	doc := map[string]any{"app": appMap(w.App)}
	if entities := entityMaps(w); len(entities) > 0 {
		doc["entities"] = entities
	}
	if screens := screenMaps(w); len(screens) > 0 {
		doc["screens"] = screens
	}
	if nav := navMaps(w.Nav); len(nav) > 0 {
		doc["nav"] = nav
	}
	if seed := seedMaps(w.Seeds); len(seed) > 0 {
		doc["seed"] = seed
	}
	if endpoints := endpointMaps(w); len(endpoints) > 0 {
		doc["endpoints"] = endpoints
	}
	if hooks := hookMaps(w); len(hooks) > 0 {
		doc["hooks"] = hooks
	}
	if middleware := middlewareMaps(w); len(middleware) > 0 {
		doc["middleware"] = middleware
	}
	if plugins := namedStubMaps(w.Plugins); len(plugins) > 0 {
		doc["plugins"] = plugins
	}
	if helpers := namedStubMaps(w.Helpers); len(helpers) > 0 {
		doc["helpers"] = helpers
	}
	return doc
}

func appMap(a world.AppConfig) map[string]any {
	m := map[string]any{}
	putString(m, "name", a.Name)
	putString(m, "module", a.Module)
	if a.DBDriver != "" || a.DBURL != "" {
		db := map[string]any{}
		putString(db, "driver", a.DBDriver)
		putString(db, "url", dbURLRef(a.DBURL))
		m["db"] = db
	}
	putString(m, "static_dir", a.StaticDir)
	putString(m, "output_dir", a.OutputDir)
	// Kiln stores the resolved value, so an empty prefix is a deliberate
	// opt-out and must be emitted explicitly rather than re-defaulting to api.
	m["api_prefix"] = strings.Trim(a.APIPrefix, "/")
	if len(a.Theme) > 0 || len(a.ThemeDark) > 0 {
		m["theme"] = ThemeMap(a.Theme, a.ThemeDark)
	}
	if a.Auth.Enabled || a.Auth.BasePath != "" || a.Auth.JWTSecret != "" || !a.Auth.DevMode {
		m["auth"] = map[string]any{
			"enabled": a.Auth.Enabled, "dev_mode": a.Auth.DevMode,
			"base_path": a.Auth.BasePath, "jwt_secret": envRef(a.Auth.JWTSecret, "JWT_SECRET"),
		}
	}
	if a.Admin.Enabled || a.Admin.Path != "" || a.Admin.Role != "" || a.Admin.LoginPath != "" || a.Admin.SeedEmail != "" || a.Admin.SeedPassword != "" {
		m["admin"] = compact(map[string]any{
			"enabled": a.Admin.Enabled, "path": a.Admin.Path, "role": a.Admin.Role,
			"login_path": a.Admin.LoginPath, "seed_email": a.Admin.SeedEmail,
			"seed_password": envRef(a.Admin.SeedPassword, "ADMIN_SEED_PASSWORD"),
		})
	}
	if a.PWA.Enabled {
		m["pwa"] = compact(map[string]any{
			"enabled": true, "name": a.PWA.Name, "short_name": a.PWA.ShortName,
			"description": a.PWA.Description, "start_url": a.PWA.StartURL,
			"scope": a.PWA.Scope, "display": a.PWA.Display,
			"theme_color": a.PWA.ThemeColor, "background_color": a.PWA.BackgroundColor,
		})
	}
	if a.LLMMD {
		m["llm_md"] = true
	}
	return m
}

// ThemeMap builds the app spec `theme` section from the light and dark
// token sets: light entries verbatim, plus a nested "dark" sub-map when
// dark overrides exist. Exported so cmd/gofastr's appToMap (pack) and
// freeze's appMap share one serialization shape instead of a copied
// block; key order is irrelevant here because writeYAMLMap sorts via
// OrderedKeys.
func ThemeMap(theme, dark map[string]string) map[string]any {
	m := map[string]any{}
	for k, v := range theme {
		m[k] = v
	}
	if len(dark) > 0 {
		d := map[string]any{}
		for k, v := range dark {
			d[k] = v
		}
		m["dark"] = d
	}
	return m
}

func entityMaps(w *world.World) []any {
	entities := sortedEntities(w)
	out := make([]any, 0, len(entities))
	for _, e := range entities {
		m := compact(map[string]any{
			"name": e.Name, "table": e.Table, "soft_delete": e.SoftDelete,
			"multi_tenant": e.MultiTenant, "owner_field": e.OwnerField,
			"cross_owner_read": e.CrossOwnerRead, "search_fields": stringSlice(e.SearchFields),
			"mcp": e.MCP, "cursor_field": e.CursorField,
			"cursor_fields": stringSlice(e.CursorFields), "properties": e.Properties,
		})
		if e.Timestamps != nil {
			m["timestamps"] = *e.Timestamps
		}
		if e.CRUD != nil {
			m["crud"] = *e.CRUD
		}
		if e.Access != nil {
			m["access"] = compact(map[string]any{
				"read": e.Access.Read, "create": e.Access.Create,
				"update": e.Access.Update, "delete": e.Access.Delete,
			})
		}
		if len(e.Indices) > 0 {
			indices := make([]any, 0, len(e.Indices))
			for _, ix := range e.Indices {
				indices = append(indices, compact(map[string]any{
					"name": ix.Name, "columns": stringSlice(ix.Columns), "unique": ix.Unique,
				}))
			}
			m["indices"] = indices
		}
		if len(e.Fields) > 0 {
			fields := make([]any, 0, len(e.Fields))
			for _, f := range e.Fields {
				fm := compact(map[string]any{
					"name": f.Name, "type": f.Type, "required": f.Required,
					"unique": f.Unique, "auto_generate": f.AutoGenerate,
					"read_only": f.ReadOnly, "hidden": f.Hidden, "pattern": f.Pattern,
					"values": stringSlice(f.Values), "to": f.To, "many": f.Many,
				})
				if f.Default != nil {
					fm["default"] = f.Default
				}
				if f.Max != nil {
					fm["max"] = *f.Max
				}
				if f.Min != nil {
					fm["min"] = *f.Min
				}
				fields = append(fields, fm)
			}
			m["fields"] = fields
		}
		if len(e.Relations) > 0 {
			relations := make([]any, 0, len(e.Relations))
			for _, r := range e.Relations {
				target := r.Entity
				if target == "" {
					target = r.To
				}
				relations = append(relations, compact(map[string]any{
					"type": normalizedRelationType(r.Type), "name": r.Name,
					"entity": target, "foreign_key": r.ForeignKey, "through": r.Through,
					"local_key": r.LocalKey, "foreign_key_target": r.ForeignKeyTarget,
				}))
			}
			m["relations"] = relations
		}
		out = append(out, m)
	}
	return out
}

func screenMaps(w *world.World) []any {
	paths := make([]string, 0, len(w.Pages))
	for path := range w.Pages {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	// An unnamed page takes its name from its path and the root is
	// "home", so `/` and `/home` both froze to one screen and the
	// generator refused the pair (2026-10-27 K23). A derived name that a
	// named page or an earlier path already holds gains a suffix.
	taken := map[string]bool{}
	for _, p := range w.Pages {
		if p != nil && p.Name != "" {
			taken[identifier(p.Name)] = true
		}
	}
	out := make([]any, 0, len(paths))
	for _, path := range paths {
		p := w.Pages[path]
		if p == nil {
			continue
		}
		layout := ""
		if p.Layout != nil {
			layout = p.Layout.Name
		}
		name := p.Name
		if name == "" {
			base := identifier(strings.Trim(path, "/"))
			if base == "" {
				base = "home"
			}
			name = base
			for n := 2; taken[name]; n++ {
				name = fmt.Sprintf("%s_%d", base, n)
			}
			taken[name] = true
		}
		route := p.Path
		if route == "" {
			route = path
		}
		m := compact(map[string]any{
			"name": name, "route": route, "title": p.Title,
			"description": p.Description, "type": p.Type, "layout": layout,
			"body": []any{nodeMap(p.Tree)},
		})
		if p.Access.Auth || p.Access.Role != "" {
			m["access"] = compact(map[string]any{"auth": p.Access.Auth || p.Access.Role != "", "role": p.Access.Role})
		}
		out = append(out, m)
	}
	return out
}

func nodeMap(n world.Node) map[string]any {
	m := compact(map[string]any{"kind": n.Kind, "props": n.Props})
	if len(n.Children) > 0 {
		children := make([]any, 0, len(n.Children))
		for _, child := range n.Children {
			children = append(children, nodeMap(child))
		}
		m["children"] = children
	}
	return m
}

func navMaps(items []world.NavItem) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		m := compact(map[string]any{"label": item.Label, "href": item.Href, "icon": item.Icon, "role": item.Role})
		if children := navMaps(item.Items); len(children) > 0 {
			m["items"] = children
		}
		out = append(out, m)
	}
	return out
}

func seedMaps(seeds []*world.Seed) []any {
	merged, _ := mergeSeeds(seeds) // validateGraduation refused a conflict
	out := make([]any, 0, len(merged))
	for _, seed := range merged {
		m := compact(map[string]any{"entity": seed.Entity, "rows": seed.Rows, "count": seed.Count, "weights": seed.Weights})
		out = append(out, m)
	}
	return out
}

// mergeSeeds folds every seed for one entity into one block at the first
// one's place: rows in order, counts summed, weights joined. Each
// add_seed appends a seed, so a world seeding an entity twice is
// ordinary, but the generator refuses two blocks for one entity (its
// boot hook skips the second once the table has rows). Two seeds
// weighting the same column differently have no merge that keeps both,
// so that is an error naming the column.
func mergeSeeds(seeds []*world.Seed) ([]*world.Seed, error) {
	var out []*world.Seed
	at := map[string]*world.Seed{}
	for _, s := range seeds {
		if s == nil {
			continue
		}
		m := at[s.Entity]
		if m == nil {
			m = &world.Seed{Entity: s.Entity}
			at[s.Entity] = m
			out = append(out, m)
		}
		m.Rows = append(m.Rows, s.Rows...)
		m.Count += s.Count
		for _, col := range slices.Sorted(maps.Keys(s.Weights)) {
			wt := s.Weights[col]
			if prev, ok := m.Weights[col]; ok && !maps.Equal(prev, wt) {
				return nil, fmt.Errorf("freeze: two seeds for entity %q weight column %q differently, and the app spec takes one seed block per entity: give both seeds the same weights for %q", s.Entity, col, col)
			}
			if m.Weights == nil {
				m.Weights = map[string]map[string]int{}
			}
			m.Weights[col] = wt
		}
	}
	return out, nil
}

// hookMaps carries the world's entity lifecycle hooks into the
// graduation app spec, the same way endpointMaps carries routes: as a
// declaration naming an owned-Go handler to implement.
//
// They used to be dropped. The live preview registers every world hook on
// the framework HookRegistry, so an operator watches a before_create
// validation reject bad rows and then ships an app with no such
// validation -- no error, no stub, no TODO. Only world.json kept them,
// and the generated app never reads world.json.
func hookMaps(w *world.World) []any {
	out := make([]any, 0, len(w.Hooks))
	for i, h := range w.Hooks {
		if h == nil {
			continue
		}
		id := h.ID
		if id == "" {
			id = fmt.Sprintf("%s_%s_%d", identifier(h.Entity), identifier(h.When), i)
		}
		desc := "Kiln declarative action " + h.Action.Kind + "; implement the owned-Go handler after generation"
		if h.Condition != "" {
			desc += " (condition: " + h.Condition + ")"
		}
		out = append(out, compact(map[string]any{
			"id":          id,
			"entity":      h.Entity,
			"when":        h.When,
			"handler":     handlerName("hook", id, i),
			"description": desc,
		}))
	}
	return out
}

// endpointMaps writes the world's endpoints as the app spec's top-level
// endpoints: list. validateGraduation has already refused any freeze
// cannot carry.
func endpointMaps(w *world.World) []any {
	eps, _ := freezeEndpoints(w)
	out := make([]any, 0, len(eps))
	for _, e := range eps {
		out = append(out, compact(map[string]any{
			"name": e.Name, "entity": e.Entity, "method": e.Method, "path": e.Path,
			"handler": e.Handler, "description": e.Description, "mcp": e.MCP,
		}))
	}
	return out
}

// freezeEndpoints turns the world's endpoints into the shape the app
// spec's endpoints: list and gofastr.settings.yml hold: each hangs under
// an entity, its path relative to the entity's route, and links a typed
// handler that generate app stubs in entities/<entity>_endpoints.go.
// Entity endpoints come first, in entity order, then the scaffold's.
//
// A world route, or a scaffold endpoint with no entity or a path outside
// its entity's route, has no such spelling. Freeze refuses it rather than
// move its URL: the route belongs in owned Go after generation.
func freezeEndpoints(w *world.World) ([]settings.Endpoint, error) {
	for _, r := range w.Routes {
		if r != nil {
			return nil, fmt.Errorf("freeze: route %s %s hangs under no entity, and an app spec endpoint needs one; delete the route and register it by hand in owned Go after generation (fwApp.Router().Handle)", r.Method, r.Path)
		}
	}
	var out []settings.Endpoint
	add := func(entity, name, method, routePath, handler, description string, mcp bool, i int) error {
		where := fmt.Sprintf("endpoint %s %s", method, routePath)
		if entity == "" {
			return fmt.Errorf("freeze: %s names no entity, and an app spec endpoint hangs under one; give it an entity or register it by hand in owned Go after generation (fwApp.Router().Handle)", where)
		}
		ent, ok := w.Entities[entity]
		if !ok || ent == nil {
			return fmt.Errorf("freeze: %s hangs under entity %q, which is not in the world", where, entity)
		}
		// The app serves an entity, and the endpoints under it, at its
		// table: /api/<table>/...
		table := ent.TableName()
		rel := strings.TrimPrefix(routePath, "/")
		if strings.HasPrefix(routePath, "/") {
			if rel, ok = strings.CutPrefix(rel, table+"/"); !ok {
				return fmt.Errorf("freeze: %s is not under /%s/, the route of its entity %q; move it there or register it by hand in owned Go after generation (fwApp.Router().Handle)", where, table, entity)
			}
		}
		if name = identifier(name); name == "" {
			name = endpointName(method, rel, i)
		}
		// A handler already spelled as an exported Go name keeps its
		// spelling; anything else is folded from its words.
		if !token.IsIdentifier(handler) || !token.IsExported(handler) || !isASCII(handler) {
			if handler = identifier(handler); handler == "" {
				handler = name
			}
			handler = exportedName(handler)
		}
		e := settings.Endpoint{
			Name: name, Entity: entity, Method: strings.ToUpper(method), Path: rel,
			Handler:     "entities/" + entity + "_endpoints.go#" + handler,
			Description: description, MCP: mcp,
		}
		for _, prev := range out {
			if prev.Name == e.Name || prev.Handler == e.Handler {
				return fmt.Errorf("freeze: endpoints %s and %s both freeze to endpoint %s (%s); give each its own name", prev.Name, e.Name, e.Name, e.Handler)
			}
		}
		// The checks generate runs on the app spec's list: an endpoint
		// freeze writes is one generate takes.
		if err := settings.CheckEndpoints(append(slices.Clone(out), e)); err != nil {
			return fmt.Errorf("freeze: %s: %w", where, err)
		}
		out = append(out, e)
		return nil
	}
	for _, ent := range sortedEntities(w) {
		for i, ep := range ent.Endpoints {
			if err := add(ent.Name, ep.Name, ep.Method, ep.Path, "", ep.Description, ep.MCP, i); err != nil {
				return nil, err
			}
		}
	}
	for i, ep := range w.Endpoints {
		if ep == nil {
			continue
		}
		if err := add(ep.Entity, ep.Name, ep.Method, ep.Path, ep.Handler, ep.Description, ep.MCP, len(out)+i); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// exportedName spells a snake identifier as an exported Go name:
// refund_order is RefundOrder.
func exportedName(snake string) string {
	var b strings.Builder
	for _, word := range strings.Split(snake, "_") {
		if word != "" {
			b.WriteString(strings.ToUpper(word[:1]) + word[1:])
		}
	}
	return b.String()
}

func middlewareMaps(w *world.World) []any {
	out := make([]any, 0, len(w.Middleware)+len(w.MiddlewareStubs))
	for _, item := range w.Middleware {
		if item != nil {
			out = append(out, compact(map[string]any{"name": item.Name, "description": item.Description}))
		}
	}
	for _, item := range w.MiddlewareStubs {
		out = append(out, compact(map[string]any{"name": item.Name, "description": item.Description}))
	}
	return out
}

// handlerName maps an agent-authored surface name to the Go identifier
// of its owned-handler stub. identifier() strips to letters, digits and
// underscore, so a name with none of those ("🔴", "???", "   ")
// sanitizes to the empty string — and compact() would then drop the
// handler key entirely, shipping a stub surface the generator has
// nothing valid to name. Fall back to a deterministic positional name
// instead; colliding hostile names get distinct indices.
func handlerName(prefix, value string, i int) string {
	if id := identifier(value); id != "" {
		return id
	}
	return fmt.Sprintf("%s_%d", prefix, i+1)
}

func namedStubMaps(items []world.NamedStub) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, compact(map[string]any{"name": item.Name, "description": item.Description}))
	}
	return out
}

func sortedEntities(w *world.World) []*world.Entity {
	names := make([]string, 0, len(w.Entities))
	for name := range w.Entities {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*world.Entity, 0, len(names))
	for _, name := range names {
		if w.Entities[name] != nil {
			out = append(out, w.Entities[name])
		}
	}
	return out
}

func endpointName(method, path string, fallback int) string {
	name := identifier(strings.ToLower(method) + "_" + strings.Trim(path, "/"))
	if name == "" {
		return fmt.Sprintf("endpoint_%d", fallback+1)
	}
	return name
}

func identifier(value string) string {
	var b strings.Builder
	underscore := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if underscore && b.Len() > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			underscore = false
		} else {
			underscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out != "" && unicode.IsDigit(rune(out[0])) {
		out = "x_" + out
	}
	return out
}

func normalizedRelationType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "has_one", "has_many", "many_to_many":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "belongs_to"
	}
}

func stringSlice(values []string) any {
	if len(values) == 0 {
		return nil
	}
	return values
}

func putString(m map[string]any, key, value string) {
	if value != "" {
		m[key] = value
	}
}

func compact(m map[string]any) map[string]any {
	for key, value := range m {
		switch v := value.(type) {
		case nil:
			delete(m, key)
		case string:
			if v == "" {
				delete(m, key)
			}
		case bool:
			if !v {
				delete(m, key)
			}
		case int:
			if v == 0 {
				delete(m, key)
			}
		case []string:
			if len(v) == 0 {
				delete(m, key)
			}
		case []map[string]any:
			if len(v) == 0 {
				delete(m, key)
			}
		case map[string]any:
			if len(v) == 0 {
				delete(m, key)
			}
		default:
			rv := reflect.ValueOf(value)
			if (rv.Kind() == reflect.Map || rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) && rv.Len() == 0 {
				delete(m, key)
			}
		}
	}
	return m
}

// normalizeYAMLValue turns arbitrary JSON-clean maps/slices in props,
// properties, seed rows, and defaults into the two container types the
// deterministic writer understands.
func normalizeYAMLValue(value any) any {
	if value == nil {
		return nil
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Map:
		out := map[string]any{}
		iter := rv.MapRange()
		for iter.Next() {
			if iter.Key().Kind() != reflect.String {
				continue
			}
			out[iter.Key().String()] = normalizeYAMLValue(iter.Value().Interface())
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = normalizeYAMLValue(rv.Index(i).Interface())
		}
		return out
	case reflect.Pointer:
		if rv.IsNil() {
			return nil
		}
		return normalizeYAMLValue(rv.Elem().Interface())
	default:
		return value
	}
}

func writeYAMLMap(sb *strings.Builder, m map[string]any, indent int, order []string) {
	for _, key := range OrderedKeys(m, order) {
		writeYAMLEntry(sb, key, m[key], indent)
	}
}

func writeYAMLEntry(sb *strings.Builder, key string, value any, indent int) {
	pad := strings.Repeat(" ", indent)
	switch v := value.(type) {
	case map[string]any:
		// An empty map is a bare `key:` with nothing indented under it,
		// which core/yaml reads back as an empty map. It refuses the flow
		// form `{}`, so writing that made the frozen app spec unparseable.
		sb.WriteString(pad + key + ":\n")
		writeYAMLMap(sb, v, indent+2, orderFor(key))
	case []any:
		if len(v) == 0 {
			sb.WriteString(pad + key + ": []\n")
			return
		}
		if allScalars(v) {
			sb.WriteString(pad + key + ": ")
			writeFlowList(sb, v)
			sb.WriteByte('\n')
			return
		}
		sb.WriteString(pad + key + ":\n")
		for _, item := range v {
			writeYAMLListItem(sb, item, indent+2, orderFor(key))
		}
	default:
		sb.WriteString(pad + key + ": ")
		writeScalarInline(sb, value)
		sb.WriteByte('\n')
	}
}

func writeYAMLListItem(sb *strings.Builder, item any, indent int, order []string) {
	if m, ok := item.(map[string]any); ok {
		if len(m) == 0 {
			sb.WriteString(strings.Repeat(" ", indent) + "-\n")
			return
		}
		keys := OrderedKeys(m, order)
		// core/yaml expects the first list-map key to carry an inline value:
		// a scalar, or a flow list of scalars. validateYAMLRepresentable
		// guarantees one exists.
		if i := inlineLeadKey(keys, m); i > 0 {
			keys[0], keys[i] = keys[i], keys[0]
		}
		first := keys[0]
		sb.WriteString(strings.Repeat(" ", indent) + "- " + first + ": ")
		if lst, ok := m[first].([]any); ok {
			writeFlowList(sb, lst)
		} else {
			writeScalarInline(sb, m[first])
		}
		sb.WriteByte('\n')
		for _, key := range keys[1:] {
			writeYAMLEntry(sb, key, m[key], indent+2)
		}
		return
	}
	if lst, ok := item.([]any); ok && allScalars(lst) {
		sb.WriteString(strings.Repeat(" ", indent) + "- ")
		writeFlowList(sb, lst)
		sb.WriteByte('\n')
		return
	}
	sb.WriteString(strings.Repeat(" ", indent) + "- ")
	writeScalarInline(sb, item)
	sb.WriteByte('\n')
}

// inlineLeadKey returns the index of the first key whose value can head a
// list item inline: a scalar, or a list of scalars. Returns -1 if none.
func inlineLeadKey(keys []string, m map[string]any) int {
	for i, key := range keys {
		if isScalar(m[key]) {
			return i
		}
	}
	for i, key := range keys {
		if lst, ok := m[key].([]any); ok && allScalars(lst) {
			return i
		}
	}
	return -1
}

func writeNestedValue(sb *strings.Builder, value any, indent int, order []string) {
	switch v := value.(type) {
	case map[string]any:
		writeYAMLMap(sb, v, indent, order)
	case []any:
		for _, item := range v {
			writeYAMLListItem(sb, item, indent, order)
		}
	}
}

func writeFlowList(sb *strings.Builder, list []any) {
	sb.WriteByte('[')
	for i, value := range list {
		if i > 0 {
			sb.WriteString(", ")
		}
		writeScalarInline(sb, value)
	}
	sb.WriteByte(']')
}

func writeScalarInline(sb *strings.Builder, value any) {
	switch v := value.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		sb.WriteString(strconv.FormatBool(v))
	case int:
		sb.WriteString(strconv.Itoa(v))
	case int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		sb.WriteString(fmt.Sprint(v))
	case float32:
		sb.WriteString(formatYAMLFloat(float64(v)))
	case float64:
		sb.WriteString(formatYAMLFloat(v))
	case json.Number:
		// The journal keeps an integer past 2^53 as a json.Number. Write
		// it as the integer it is (core/yaml reads it back as an int64);
		// one past int64 range is written as the float it always was.
		if i, err := v.Int64(); err == nil {
			sb.WriteString(strconv.FormatInt(i, 10))
		} else if f, err := v.Float64(); err == nil {
			sb.WriteString(formatYAMLFloat(f))
		} else {
			sb.WriteString(quoteYAMLString(v.String()))
		}
	case string:
		sb.WriteString(quoteYAMLString(v))
	default:
		sb.WriteString(quoteYAMLString(fmt.Sprint(v)))
	}
}

func formatYAMLFloat(value float64) string {
	out := strconv.FormatFloat(value, 'g', -1, 64)
	if !strings.ContainsAny(out, ".eE") {
		out += ".0"
	}
	return out
}

func allScalars(values []any) bool {
	for _, value := range values {
		if !isScalar(value) {
			return false
		}
	}
	return true
}

func isScalar(value any) bool {
	switch value.(type) {
	case map[string]any, []any:
		return false
	default:
		return true
	}
}

func quoteYAMLString(value string) string {
	if !needsQuote(value) {
		return value
	}
	return strconv.Quote(value)
}

func needsQuote(value string) bool {
	if value == "" {
		return true
	}
	switch strings.ToLower(value) {
	case "true", "false", "null", "~":
		return true
	}
	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil && strings.ContainsAny(value, ".eE") {
		return true
	}
	if strings.ContainsAny(value[:1], "[]{}&*!|>'\"%@`#,?: -") ||
		strings.Contains(value, ": ") || strings.Contains(value, " #") ||
		// Commas split core/yaml flow lists; quotes and brackets anywhere
		// confuse its quote/flow scanning; a colon in a block list item
		// re-parses as `- key: value`. Quote them wherever they appear,
		// over-quoting is harmless, under-quoting corrupts silently.
		strings.ContainsAny(value, `,'"[]{}:`) ||
		strings.ContainsAny(value, "\n\t") || value != strings.TrimSpace(value) ||
		strings.HasSuffix(value, ":") {
		return true
	}
	return false
}

// validateYAMLRepresentable rejects values the core/yaml emitter cannot
// round-trip, so freeze fails loudly instead of writing a gofastr.yml that
// silently re-parses into different data. Kiln worlds carry arbitrary JSON
// (seed rows, props), so this is reachable, not theoretical.
func validateYAMLRepresentable(value any, path string) error {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if err := validateYAMLKey(key, path); err != nil {
				return err
			}
			if err := validateYAMLRepresentable(child, joinPath(path, key)); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range v {
			itemPath := fmt.Sprintf("%s[%d]", path, i)
			switch entry := item.(type) {
			case map[string]any:
				if len(entry) > 0 && inlineLeadKey(OrderedKeys(entry, nil), entry) == -1 {
					return fmt.Errorf("freeze: %s: core/yaml cannot represent a list item whose every value is nested (keys: %s); flatten one value to a scalar or scalar list", itemPath, strings.Join(slices.Sorted(maps.Keys(entry)), ", "))
				}
			case []any:
				if !allScalars(entry) {
					return fmt.Errorf("freeze: %s: core/yaml cannot represent a list nested inside a list unless all its values are scalars", itemPath)
				}
			}
			if err := validateYAMLRepresentable(item, itemPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateYAMLKey rejects map keys core/yaml cannot parse back: it cuts each
// line at the first colon and has no quoted-key syntax, comments start at
// "#", and "[]{}" is rejected key syntax.
func validateYAMLKey(key, path string) error {
	if key == "" {
		return fmt.Errorf("freeze: %s: empty map key cannot be represented in gofastr.yml", joinPath(path, key))
	}
	if strings.ContainsAny(key, ":#[]{}\n\t") ||
		key != strings.TrimSpace(key) ||
		strings.ContainsAny(key[:1], "-'\"") {
		return fmt.Errorf("freeze: %s: map key %q cannot be represented in gofastr.yml (no colons, comments, brackets, quotes, or surrounding whitespace)", joinPath(path, key), key)
	}
	return nil
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// OrderedKeys returns m's keys in `order` first (only those present in m),
// then the remaining keys sorted, so the emitted YAML keeps the documented
// key order while never depending on map iteration order.
//
// Exported because cmd/gofastr/internal/packcmd/pack.go carried a byte-identical private
// copy for its own YAML writer; that copy is deleted and calls this one.
func OrderedKeys(m map[string]any, order []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(m))
	for _, key := range order {
		if _, ok := m[key]; ok {
			out = append(out, key)
			seen[key] = true
		}
	}
	rest := make([]string, 0, len(m))
	for key := range m {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

var (
	topLevelOrder = []string{"app", "entities", "screens", "nav", "seed", "endpoints", "middleware", "plugins", "helpers"}
	appOrder      = []string{"name", "module", "db", "static_dir", "output_dir", "api_prefix", "theme", "auth", "admin", "pwa", "llm_md"}
	entityOrder   = []string{"name", "table", "crud", "mcp", "soft_delete", "multi_tenant", "owner_field", "cross_owner_read", "search_fields", "timestamps", "cursor_field", "cursor_fields", "properties", "access", "indices", "fields", "relations"}
	fieldOrder    = []string{"name", "type", "required", "unique", "default", "max", "min", "pattern", "values", "to", "many", "auto_generate", "read_only", "hidden"}
	screenOrder   = []string{"name", "route", "title", "description", "type", "layout", "access", "body"}
	blockOrder    = []string{"kind", "props", "children"}
	relationOrder = []string{"type", "name", "entity", "foreign_key", "through", "local_key", "foreign_key_target"}
	indexOrder    = []string{"name", "columns", "unique"}
	navOrder      = []string{"label", "href", "icon", "role", "items"}
	accessOrder   = []string{"auth", "role", "read", "create", "update", "delete"}
	endpointOrder = []string{"name", "entity", "method", "path", "handler", "description", "mcp"}
	stubOrder     = []string{"name", "description"}
	seedOrder     = []string{"entity", "rows", "count", "weights"}
	dbOrder       = []string{"driver", "url"}
	authOrder     = []string{"enabled", "dev_mode", "base_path", "jwt_secret"}
	adminOrder    = []string{"enabled", "path", "role", "login_path", "seed_email", "seed_password"}
	pwaOrder      = []string{"enabled", "name", "short_name", "description", "start_url", "scope", "display", "theme_color", "background_color"}
)

func orderFor(key string) []string {
	switch key {
	case "app":
		return appOrder
	case "entities":
		return entityOrder
	case "fields":
		return fieldOrder
	case "screens":
		return screenOrder
	case "body", "children":
		return blockOrder
	case "relations":
		return relationOrder
	case "indices":
		return indexOrder
	case "nav", "items":
		return navOrder
	case "access":
		return accessOrder
	case "endpoints":
		return endpointOrder
	case "middleware", "plugins", "helpers":
		return stubOrder
	case "seed":
		return seedOrder
	case "db":
		return dbOrder
	case "auth":
		return authOrder
	case "admin":
		return adminOrder
	case "pwa":
		return pwaOrder
	default:
		return nil
	}
}

// envRef replaces a live credential with a "${NAME}" reference for the
// frozen app spec.
//
// Freeze writes gofastr.yml into a project directory that is about to be
// committed, so the value must not be the secret itself, same rule the
// generated app already follows. An empty value stays empty: there is
// nothing to reference, and emitting "${JWT_SECRET}" for an unset secret
// would turn "not configured" into "misconfigured".
func envRef(value, name string) string {
	if value == "" {
		return ""
	}
	return "${" + name + "}"
}

// dbURLRef routes a credential-bearing DSN through envRef. A DSN with an
// embedded password (postgres://user:pw@host/db, a key/value password=
// pair) is live credential material: freeze writes into a directory that
// is about to be committed, so the password must not ride along verbatim.
// A DSN that carries no credentials (a SQLite file path, a URL with no
// userinfo) stays verbatim so local frozen apps keep booting. The
// credential predicate is dsnredact.HasSecret, the one canonical rule.
func dbURLRef(dsn string) string {
	if dsn == "" || !dsnredact.HasSecret(dsn) {
		return dsn
	}
	return envRef(dsn, "DATABASE_URL")
}
