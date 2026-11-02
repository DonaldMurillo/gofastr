package packcmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/core/settings"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/dsnredact"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
	"github.com/DonaldMurillo/gofastr/kiln/freeze"
)

// decodeAppSpecString parses a gofastr.yml from a string into a App spec.
func decodeAppSpecString(yml string) (appspec.AppSpec, error) {
	node, err := coreyaml.Parse(yml)
	if err != nil {
		return appspec.AppSpec{}, err
	}
	return appspec.DecodeAppSpec(node)
}

// =============================================================================
// pack.go: a lossy app→app spec snapshot. encodeAppSpecYAML serializes a App spec
// back to gofastr.yml; the AST readers (further down) reconstruct a App spec
// from a generated app's Go source so `gofastr pack <dir>` recovers the
// authoring YAML. The invariant the round-trip test gates:
//
//	parse(meridian.yml)  deep-equals  parse(pack(generate(meridian.yml)))
//
// i.e. encodeAppSpecYAML is the exact inverse of decodeAppSpec (modulo
// comments + formatting). When a new app spec construct is added, BOTH the
// decoder and this serializer must learn it or the round-trip test fails.
// =============================================================================

// encodeAppSpecYAML serializes a App spec to gofastr.yml text. It returns
// an error — naming the key and the map it came from — for any map key that
// cannot survive the decodeAppSpec round trip: core/yaml never unquotes
// keys, so quoting would mangle them just as surely as emitting them raw
// would forge structure. Refusing is the honest inverse.
func encodeAppSpecYAML(bp appspec.AppSpec) (string, error) {
	root := appSpecToMap(bp)
	// YAML text is Unicode: a string holding a byte that is not UTF-8 has
	// no spelling a YAML 1.2 decoder reads back (core/yaml reads "\xff"
	// as U+00FF, as the spec says), so refuse it as an unrepresentable key
	// is refused.
	if at := invalidUTF8At(reflect.ValueOf(root), "app spec"); at != "" {
		return "", fmt.Errorf("%s holds a byte that is not UTF-8; YAML cannot carry it", at)
	}
	var sb strings.Builder
	if err := writeYAMLMap(&sb, root, 0, topLevelOrder, ""); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// ----- App spec -> nested map[string]any (mirrors the decoders) -------------

func appSpecToMap(bp appspec.AppSpec) map[string]any {
	m := map[string]any{}
	// A fragment has no app: section: writing the decoder's zero App
	// (api_prefix: "") would turn it into a whole app that mounts its CRUD
	// API at the root, and the next decode would run the checks a
	// fragment skips.
	if app := appToMap(bp.App); len(app) > 0 && !bp.IsFragment() {
		m["app"] = app
	}
	if len(bp.Entities) > 0 {
		ents := make([]any, len(bp.Entities))
		for i, e := range bp.Entities {
			ents[i] = entityToMap(e)
		}
		m["entities"] = ents
	}
	if len(bp.Screens) > 0 {
		scr := make([]any, len(bp.Screens))
		for i, s := range bp.Screens {
			scr[i] = screenToMap(s)
		}
		m["screens"] = scr
	}
	if len(bp.Nav) > 0 {
		m["nav"] = navItemsToAny(bp.Nav)
	}
	if len(bp.Seed) > 0 {
		seed := make([]any, len(bp.Seed))
		for i, s := range bp.Seed {
			sm := map[string]any{"entity": s.Entity}
			putInt(sm, "count", s.Count)
			if len(s.Weights) > 0 {
				// Values are int here and int after decodeSeedWeights
				// re-parses them, unlike rows, whose native parser types
				// (int64) pass through anyMap unchanged.
				w := make(map[string]any, len(s.Weights))
				for col, vals := range s.Weights {
					vw := make(map[string]any, len(vals))
					for val, weight := range vals {
						vw[val] = weight
					}
					w[col] = vw
				}
				sm["weights"] = w
			}
			if len(s.Rows) > 0 {
				rows := make([]any, len(s.Rows))
				for j, r := range s.Rows {
					rows[j] = anyMap(r)
				}
				sm["rows"] = rows
			}
			seed[i] = sm
		}
		m["seed"] = seed
	}
	if len(bp.Endpoints) > 0 {
		eps := make([]any, len(bp.Endpoints))
		for i, e := range bp.Endpoints {
			eps[i] = endpointToMap(e)
		}
		m["endpoints"] = eps
	}
	if len(bp.Hooks) > 0 {
		hooks := make([]any, len(bp.Hooks))
		for i, h := range bp.Hooks {
			hm := map[string]any{}
			putStr(hm, "id", h.ID)
			putStr(hm, "entity", h.Entity)
			putStr(hm, "when", h.When)
			putStr(hm, "handler", h.Handler)
			putStr(hm, "description", h.Description)
			hooks[i] = hm
		}
		m["hooks"] = hooks
	}
	if len(bp.Middleware) > 0 {
		m["middleware"] = stubsToAny(bp.Middleware)
	}
	if len(bp.Plugins) > 0 {
		m["plugins"] = stubsToAny(bp.Plugins)
	}
	if len(bp.Helpers) > 0 {
		m["helpers"] = stubsToAny(bp.Helpers)
	}
	return m
}

// stubsToAny mirrors decodeNamedStubs: a stub with no description is a bare
// scalar (the form hand-authored app specs use), one with a description is
// a name/description map. Both decode back to the same AppSpecNamedStub.
func stubsToAny(stubs []appspec.AppSpecNamedStub) []any {
	out := make([]any, len(stubs))
	for i, s := range stubs {
		if s.Description == "" {
			out[i] = s.Name
			continue
		}
		sm := map[string]any{"name": s.Name}
		putStr(sm, "description", s.Description)
		out[i] = sm
	}
	return out
}

func appToMap(a appspec.AppSpecApp) map[string]any {
	m := map[string]any{}
	putStr(m, "name", a.Name)
	putStr(m, "description", a.Description)
	putStr(m, "base_url", a.BaseURL)
	putStr(m, "module", a.Module)
	if a.DBDriver != "" || a.DBURL != "" {
		db := map[string]any{}
		putStr(db, "driver", a.DBDriver)
		putStr(db, "url", a.DBURL)
		m["db"] = db
	}
	putStr(m, "static_dir", a.StaticDir)
	putStr(m, "output_dir", a.OutputDir)
	// api_prefix defaults to "api"; only the value that survives a re-parse
	// matters, so emit it whenever it isn't the bare default.
	if a.APIPrefix != "api" {
		m["api_prefix"] = a.APIPrefix
	}
	putBool(m, "public_openapi", a.PublicOpenAPI)
	if len(a.Theme) > 0 || len(a.ThemeDark) > 0 {
		m["theme"] = freeze.ThemeMap(a.Theme, a.ThemeDark)
	}
	if a.Auth.Enabled || a.Auth.BasePath != "" || a.Auth.JWTSecret != "" || !a.Auth.DevMode {
		auth := map[string]any{}
		putBool(auth, "enabled", a.Auth.Enabled)
		// dev_mode defaults to true; emit it explicitly so a false survives.
		auth["dev_mode"] = a.Auth.DevMode
		putStr(auth, "base_path", a.Auth.BasePath)
		putStr(auth, "jwt_secret", a.Auth.JWTSecret)
		m["auth"] = auth
	}
	if a.Admin.Enabled || a.Admin.Path != "" || a.Admin.Role != "" || a.Admin.LoginPath != "" || a.Admin.SeedEmail != "" || a.Admin.SeedPassword != "" {
		admin := map[string]any{}
		putBool(admin, "enabled", a.Admin.Enabled)
		putStr(admin, "path", a.Admin.Path)
		putStr(admin, "role", a.Admin.Role)
		putStr(admin, "login_path", a.Admin.LoginPath)
		putStr(admin, "seed_email", a.Admin.SeedEmail)
		putStr(admin, "seed_password", a.Admin.SeedPassword)
		m["admin"] = admin
	}
	if a.PWA.Enabled {
		pwa := map[string]any{}
		putBool(pwa, "enabled", a.PWA.Enabled)
		putStr(pwa, "name", a.PWA.Name)
		putStr(pwa, "short_name", a.PWA.ShortName)
		putStr(pwa, "description", a.PWA.Description)
		putStr(pwa, "start_url", a.PWA.StartURL)
		putStr(pwa, "scope", a.PWA.Scope)
		putStr(pwa, "display", a.PWA.Display)
		putStr(pwa, "theme_color", a.PWA.ThemeColor)
		putStr(pwa, "background_color", a.PWA.BackgroundColor)
		m["pwa"] = pwa
	}
	if a.LLMMD {
		m["llm_md"] = true
	}
	if a.LocalePrefixes {
		m["locale_prefixes"] = true
	}
	return m
}

func entityToMap(e framework.EntityDeclaration) map[string]any {
	m := map[string]any{}
	putStr(m, "name", e.Name)
	putStr(m, "table", e.Table)
	putStrs(m, "primary_key", e.PrimaryKey)
	if e.Scope != nil {
		scope := map[string]any{}
		putBool(scope, "soft_delete", e.Scope.SoftDelete)
		putBool(scope, "multi_tenant", e.Scope.MultiTenant)
		putStr(scope, "tenant_field", e.Scope.TenantField)
		putStr(scope, "owner_field", e.Scope.OwnerField)
		putStr(scope, "cross_owner_read", e.Scope.CrossOwnerRead)
		m["scope"] = scope
	}
	if e.Exposure != nil {
		exposure := map[string]any{}
		if e.Exposure.CRUD != nil {
			exposure["crud"] = *e.Exposure.CRUD
		}
		putBool(exposure, "mcp", e.Exposure.MCP)
		putBool(exposure, "public", e.Exposure.Public)
		if e.Exposure.Access != nil {
			acc := map[string]any{}
			putStr(acc, "read", e.Exposure.Access.Read)
			putStr(acc, "create", e.Exposure.Access.Create)
			putStr(acc, "update", e.Exposure.Access.Update)
			putStr(acc, "delete", e.Exposure.Access.Delete)
			exposure["access"] = acc
		}
		if e.Exposure.ReadScope != nil {
			rs := map[string]any{}
			putStr(rs, "unrestricted", e.Exposure.ReadScope.Unrestricted)
			if len(e.Exposure.ReadScope.Filter) > 0 {
				preds := make([]any, len(e.Exposure.ReadScope.Filter))
				for i, p := range e.Exposure.ReadScope.Filter {
					pm := map[string]any{}
					putStr(pm, "field", p.Field)
					putStr(pm, "op", p.Op)
					putStr(pm, "value", p.Value)
					putStrs(pm, "values", p.Values)
					preds[i] = pm
				}
				rs["filter"] = preds
			}
			exposure["read_scope"] = rs
		}
		m["exposure"] = exposure
	}
	putStrs(m, "search_fields", e.SearchFields)
	if e.Display != nil {
		m["display"] = displayToMap(e.Display)
	}
	if e.States != nil {
		m["states"] = statesToMap(e.States)
	}
	if e.Timestamps != nil {
		m["timestamps"] = *e.Timestamps
	}
	if e.Pagination != nil {
		pagination := map[string]any{}
		putStr(pagination, "cursor_field", e.Pagination.CursorField)
		putStrs(pagination, "cursor_fields", e.Pagination.CursorFields)
		if e.Pagination.MaxListLimit != 0 {
			pagination["max_list_limit"] = e.Pagination.MaxListLimit
		}
		m["pagination"] = pagination
	}
	if len(e.Properties) > 0 {
		m["properties"] = anyMap(e.Properties)
	}
	if len(e.Renames) > 0 {
		rn := make(map[string]any, len(e.Renames))
		for k, v := range e.Renames {
			rn[k] = v
		}
		m["renames"] = rn
	}
	if len(e.Indices) > 0 {
		idx := make([]any, len(e.Indices))
		for i, ix := range e.Indices {
			im := map[string]any{}
			putStr(im, "name", ix.Name)
			putStrs(im, "columns", ix.Columns)
			putBool(im, "unique", ix.Unique)
			idx[i] = im
		}
		m["indices"] = idx
	}
	if len(e.Fields) > 0 {
		fields := make([]any, 0, len(e.Fields))
		for _, f := range e.Fields {
			// Drop the hidden owner column the generator synthesizes from
			// owner_field: the author never wrote it, so packing it back
			// would diverge from the source app spec.
			if e.Scope != nil && e.Scope.OwnerField != "" && f.Name == e.Scope.OwnerField && f.Hidden {
				continue
			}
			fields = append(fields, fieldToMap(f))
		}
		if len(fields) > 0 {
			m["fields"] = fields
		}
	}
	if len(e.Relations) > 0 {
		rels := make([]any, len(e.Relations))
		for i, r := range e.Relations {
			rm := map[string]any{}
			putStr(rm, "type", relationTypeToString(r.Type))
			putStr(rm, "name", r.Name)
			putStr(rm, "entity", r.Entity)
			// One column packs as a scalar, as foreign_key always read.
			if len(r.ForeignKey) == 1 {
				putStr(rm, "foreign_key", r.ForeignKey[0])
			} else {
				putStrs(rm, "foreign_key", r.ForeignKey)
			}
			putStr(rm, "through", r.Through)
			putStr(rm, "local_key", r.LocalKey)
			putStr(rm, "foreign_key_target", r.ForeignKeyTarget)
			if r.OnDelete != "" {
				putStr(rm, "on_delete", string(r.OnDelete))
			}
			if r.CascadeWrite {
				putBool(rm, "cascade_write", r.CascadeWrite)
			}
			rels[i] = rm
		}
		m["relations"] = rels
	}
	return m
}

func fieldToMap(f framework.FieldDeclaration) map[string]any {
	m := map[string]any{}
	putStr(m, "name", f.Name)
	putStr(m, "type", f.Type)
	putBool(m, "required", f.Required)
	putBool(m, "unique", f.Unique)
	if f.Default != nil {
		m["default"] = f.Default
	}
	if f.Max != nil {
		m["max"] = *f.Max
	}
	if f.Min != nil {
		m["min"] = *f.Min
	}
	putStr(m, "pattern", f.Pattern)
	putStrs(m, "values", f.Values)
	putStr(m, "to", f.To)
	putBool(m, "many", f.Many)
	putStr(m, "auto_generate", f.AutoGenerate)
	putBool(m, "read_only", f.ReadOnly)
	putBool(m, "hidden", f.Hidden)
	putBool(m, "no_query", f.NoQuery)
	return m
}

func screenToMap(s appspec.AppSpecScreen) map[string]any {
	m := map[string]any{}
	putStr(m, "name", s.Name)
	putStr(m, "route", s.Route)
	putStr(m, "title", s.Title)
	putStr(m, "description", s.Description)
	putStr(m, "type", s.Type)
	putStr(m, "layout", s.Layout)
	if s.Access.Auth || s.Access.Role != "" {
		acc := map[string]any{}
		putBool(acc, "auth", s.Access.Auth)
		putStr(acc, "role", s.Access.Role)
		m["access"] = acc
	}
	if len(s.Body) > 0 {
		m["body"] = blocksToAny(s.Body)
	}
	return m
}

func blocksToAny(blocks []appspec.AppSpecBlock) []any {
	out := make([]any, len(blocks))
	for i, b := range blocks {
		out[i] = blockToMap(b)
	}
	return out
}

func blockToMap(b appspec.AppSpecBlock) map[string]any {
	m := map[string]any{}
	putStr(m, "kind", b.Kind)
	putStr(m, "type", b.Type)
	putStr(m, "text", b.Text)
	putInt(m, "level", b.Level)
	putStr(m, "entity", b.Entity)
	putStrs(m, "fields", b.Fields)
	putInt(m, "limit", b.Limit)
	putBool(m, "create", b.Create)
	putBool(m, "bulk", b.Bulk)
	putStr(m, "empty_text", b.EmptyText)
	putStr(m, "class", b.Class)
	putStr(m, "href", b.Href)
	putStr(m, "mode", b.Mode)
	putStr(m, "island", b.Island)
	putStr(m, "widget", b.Widget)
	if len(b.Props) > 0 {
		m["props"] = anyMap(b.Props)
	}
	if len(b.Children) > 0 {
		m["children"] = blocksToAny(b.Children)
	}
	if len(b.Actions) > 0 {
		acts := make([]any, len(b.Actions))
		for i, a := range b.Actions {
			acts[i] = actionToMap(a)
		}
		m["actions"] = acts
	}
	return m
}

func navItemsToAny(items []appspec.AppSpecNavItem) []any {
	out := make([]any, len(items))
	for i, n := range items {
		nm := map[string]any{}
		putStr(nm, "label", n.Label)
		putStr(nm, "href", n.Href)
		putStr(nm, "icon", n.Icon)
		putStr(nm, "role", n.Role)
		if len(n.Items) > 0 {
			nm["items"] = navItemsToAny(n.Items)
		}
		out[i] = nm
	}
	return out
}

func actionToMap(a appspec.AppSpecAction) map[string]any {
	m := map[string]any{}
	putStr(m, "name", a.Name)
	putStr(m, "event", a.Event)
	putStr(m, "client_js", a.ClientJS)
	return m
}

// relationTypeToString returns the app spec YAML spelling of t ("has_one",
// "belongs_to", ...), via the shared relationKinds table.
func relationTypeToString(t framework.RelationType) string {
	return appgen.RelationKindFor(t).Yaml
}

// displayToMap and statesToMap serialize the entity's display and states
// groups back into their YAML shape. Keys are the snake_case names the
// decoder reads; a form item keeps its three shapes (bare field string,
// row list, section map).
func displayToMap(d *fwentity.DisplayConfig) map[string]any {
	m := map[string]any{}
	putStr(m, "singular", d.Singular)
	putStr(m, "plural", d.Plural)
	putStrs(m, "title_fields", d.TitleFields)
	putStr(m, "description", d.Description)
	putStrs(m, "columns", d.Columns)
	if d.Nav != nil {
		nav := map[string]any{}
		putStr(nav, "group", d.Nav.Group)
		putStr(nav, "icon", d.Nav.Icon)
		putInt(nav, "order", d.Nav.Order)
		putBool(nav, "hide", d.Nav.Hide)
		putBool(nav, "hide_count", d.Nav.HideCount)
		m["nav"] = nav
	}
	if len(d.Views) > 0 {
		views := make([]any, len(d.Views))
		for i, v := range d.Views {
			vm := map[string]any{}
			putStr(vm, "key", v.Key)
			putStr(vm, "label", v.Label)
			putStr(vm, "where", v.Where)
			putStr(vm, "sort", v.Sort)
			putStr(vm, "as", v.As)
			putBool(vm, "default", v.Default)
			views[i] = vm
		}
		m["views"] = views
	}
	putStrs(m, "facets", d.Facets)
	if d.Form != nil && (len(d.Form.Main) > 0 || len(d.Form.Side) > 0) {
		form := map[string]any{}
		if len(d.Form.Main) > 0 {
			form["main"] = formItemsToAny(d.Form.Main)
		}
		if len(d.Form.Side) > 0 {
			form["side"] = formItemsToAny(d.Form.Side)
		}
		m["form"] = form
	}
	if d.Card != nil {
		card := map[string]any{}
		putStr(card, "title", d.Card.Title)
		putStr(card, "subtitle", d.Card.Subtitle)
		putStr(card, "badge", d.Card.Badge)
		putStrs(card, "meta", d.Card.Meta)
		m["card"] = card
	}
	if len(d.Fields) > 0 {
		fields := make(map[string]any, len(d.Fields))
		for name, h := range d.Fields {
			hm := map[string]any{}
			putStr(hm, "label", h.Label)
			putStr(hm, "help", h.Help)
			putStr(hm, "placeholder", h.Placeholder)
			putBool(hm, "locked", h.Locked)
			putBool(hm, "omit", h.Omit)
			putStr(hm, "show_when", h.ShowWhen)
			putStr(hm, "input", h.Input)
			fields[name] = hm
		}
		m["fields"] = fields
	}
	if len(d.PageSizes) > 0 {
		sizes := make([]any, len(d.PageSizes))
		for i, n := range d.PageSizes {
			sizes[i] = n
		}
		m["page_sizes"] = sizes
	}
	putBool(m, "no_duplicate", d.NoDuplicate)
	putBool(m, "no_bulk", d.NoBulk)
	return m
}

func formItemsToAny(items []fwentity.FormItem) []any {
	out := make([]any, len(items))
	for i, it := range items {
		switch {
		case it.Field != "":
			out[i] = it.Field
		case len(it.Row) > 0:
			out[i] = map[string]any{"row": strsToAny(it.Row)}
		default:
			im := map[string]any{}
			putStr(im, "section", it.Section)
			putStr(im, "help", it.Help)
			putBool(im, "collapsed", it.Collapsed)
			if len(it.Items) > 0 {
				im["items"] = formItemsToAny(it.Items)
			}
			out[i] = im
		}
	}
	return out
}

func strsToAny(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// endpointToMap writes one endpoint with the keys of the settings
// file's endpoints: section.
func endpointToMap(e settings.Endpoint) map[string]any {
	m := map[string]any{}
	putStr(m, "name", e.Name)
	putStr(m, "entity", e.Entity)
	putStr(m, "method", e.Method)
	putStr(m, "path", e.Path)
	putStr(m, "handler", e.Handler)
	putStr(m, "description", e.Description)
	putBool(m, "mcp", e.MCP)
	putBool(m, "public", e.Public)
	return m
}

func statesToMap(st *fwentity.StatesConfig) map[string]any {
	m := map[string]any{}
	putStr(m, "field", st.Field)
	putStrs(m, "initial", st.Initial)
	if len(st.Transitions) > 0 {
		moves := make([]any, len(st.Transitions))
		for i, tr := range st.Transitions {
			tm := map[string]any{}
			putStr(tm, "key", tr.Key)
			putStr(tm, "label", tr.Label)
			putStrs(tm, "from", tr.From)
			putStr(tm, "to", tr.To)
			putStr(tm, "stamp", tr.Stamp)
			putStr(tm, "variant", tr.Variant)
			putStr(tm, "permission", tr.Permission)
			putBool(tm, "system", tr.System)
			moves[i] = tm
		}
		m["transitions"] = moves
	}
	putBool(m, "advisory", st.Advisory)
	return m
}

// anyMap deep-copies a map[string]any so list/map children are []any/map[string]any
// (the shape the generic writer expects). Values are already the parser's native
// types (string/int64/float64/bool/nil/[]any/map[string]any).
func anyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ----- omit-zero setters -----------------------------------------------------

func putStr(m map[string]any, k, v string) {
	if v != "" {
		m[k] = v
	}
}

func putBool(m map[string]any, k string, v bool) {
	if v {
		m[k] = true
	}
}

func putInt(m map[string]any, k string, v int) {
	if v != 0 {
		m[k] = int64(v)
	}
}

func putStrs(m map[string]any, k string, v []string) {
	if len(v) > 0 {
		s := make([]any, len(v))
		for i, x := range v {
			s[i] = x
		}
		m[k] = s
	}
}

// ----- generic YAML writer ---------------------------------------------------

func writeYAMLMap(sb *strings.Builder, m map[string]any, indent int, order []string, path string) error {
	for _, k := range freeze.OrderedKeys(m, order) {
		if err := writeYAMLEntry(sb, k, m[k], indent, path); err != nil {
			return err
		}
	}
	return nil
}

func writeYAMLEntry(sb *strings.Builder, key string, val any, indent int, path string) error {
	if reason := yamlKeyRejectReason(key); reason != "" {
		where := "the top-level map"
		if path != "" {
			where = strconv.Quote(path)
		}
		return fmt.Errorf("cannot emit %q as a map key under %s: %s", key, where, reason)
	}
	child := joinYAMLPath(path, key)
	pad := strings.Repeat(" ", indent)
	switch v := val.(type) {
	case map[string]any:
		if len(v) == 0 {
			// core/yaml rejects flow mappings outright (parseScalar →
			// flowMapError), so `key: {}` writes a file decodeAppSpec
			// cannot read back. A bare `key:` line is the exact inverse:
			// parseMap reads it as an empty map node, which is what the
			// grouped decoders produce for an authored bare `scope:` /
			// `pagination:` / `exposure:` / `access:` and what mapValue
			// gives for an empty map inside properties, props, or seed
			// rows. Trailing-at-EOF is the same case: parseMap's
			// p.pos >= len(p.lines) branch also yields the empty map node.
			sb.WriteString(pad + key + ":\n")
			return nil
		}
		sb.WriteString(pad + key + ":\n")
		return writeYAMLMap(sb, v, indent+2, orderFor(key), child)
	case []any:
		if len(v) == 0 {
			sb.WriteString(pad + key + ": []\n")
			return nil
		}
		if allScalars(v) {
			sb.WriteString(pad + key + ": ")
			writeFlowList(sb, v)
			sb.WriteString("\n")
			return nil
		}
		sb.WriteString(pad + key + ":\n")
		for _, item := range v {
			if err := writeYAMLListItem(sb, item, indent+2, orderFor(key), child); err != nil {
				return err
			}
		}
		return nil
	default:
		sb.WriteString(pad + key + ": ")
		writeScalarInline(sb, val)
		sb.WriteString("\n")
		return nil
	}
}

func writeYAMLListItem(sb *strings.Builder, item any, indent int, order []string, path string) error {
	if m, ok := item.(map[string]any); ok {
		if len(m) == 0 {
			// Empty-map list item, the seed-row shape of the `key: {}`
			// defect above: `- {}` is the flow mapping parseList refuses,
			// and the fallthrough used to print the Go value ("- map[]"),
			// which re-parses as a STRING. parseList reads a bare `-` item
			// as an empty map node — the exact inverse of an authored bare
			// `-` row.
			sb.WriteString(strings.Repeat(" ", indent) + "-\n")
			return nil
		}
		var tmp strings.Builder
		if err := writeYAMLMap(&tmp, m, indent+2, order, path); err != nil {
			return err
		}
		s := tmp.String()
		// Replace the first line's leading (indent+2) spaces with "<indent>- ".
		sb.WriteString(strings.Repeat(" ", indent) + "- " + s[indent+2:])
		return nil
	}
	sb.WriteString(strings.Repeat(" ", indent) + "- ")
	// parseList cuts ANY unquoted block-list item at the first ':' into a
	// map entry (`- postgres://u@h/db` re-parses as {postgres: "//u@h/db"}),
	// because ':' is the item grammar's key/value delimiter. Block VALUES
	// follow the laxer rule — parseScalar only rejects ": " — and
	// needsQuote encodes exactly that, so it cannot carry this rule without
	// quoting every URL-shaped block value in every real pack. Quote here,
	// in the one context the list rule governs.
	if s, ok := item.(string); ok && strings.Contains(s, ":") {
		sb.WriteString(quoteYAMLStringEscaped(s))
	} else {
		writeScalarInline(sb, item)
	}
	sb.WriteString("\n")
	return nil
}

// joinYAMLPath builds the dotted key path used to name the map an entry came
// from in refusal errors (e.g. "seed.rows", "entities.properties").
func joinYAMLPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// yamlKeyRejectReason returns why key cannot round-trip through core/yaml as
// a map key, or "" if it can. Every case below is verified against the
// parser, never assumed:
//
//   - core/yaml cuts a line at the FIRST ':' and never unquotes keys
//     (yaml.go parseMap), so quoting is not an escape hatch — a quoted key
//     re-parses mangled, and a raw key containing ':' re-parses truncated;
//   - lexLines strips ' #'-comments, trims edge whitespace, normalizes CRLF,
//     and rejects tabs anywhere in a line;
//   - parseMap rejects flow indicators in keys outright;
//   - parseBlock dispatches a "- " prefix to the list grammar, so such a key
//     re-parses as a list item instead of a map entry.
//
// A key failing any of these would re-parse as a different key or as
// structure the source never declared — the seed-row/entity-property forgery
// this guard exists to stop — so pack refuses rather than emit a file that
// lies about the app it snapshots.
func yamlKeyRejectReason(key string) string {
	switch {
	case key == "":
		return "empty key: the parser drops the entry on re-parse"
	case strings.ContainsAny(key, "\n\r"):
		return "line breaks re-parse as new YAML structure — the key-injection vector"
	case strings.Contains(key, ":"):
		return "core/yaml cuts keys at the first ':' and never unquotes, so the key would re-parse truncated"
	case strings.ContainsAny(key, "\"'"):
		return "a quote anywhere in a key desyncs core/yaml's comment scanner from the " +
			"quoted value, and a leading quote sends the whole entry down the " +
			"quoted-scalar path — either way the file cannot be read back"
	case strings.Contains(key, "#"):
		// Conservative rather than parser-exact: '#' only opens a comment at
		// position 0 or after a space, so an interior a#b would in fact
		// survive. Refused anyway — one simple predicate beats a
		// position-dependent one, and no real key carries a '#'.
		return "'#' may open a comment on re-parse, depending on position"
	case strings.ContainsAny(key, "[]{}"):
		return "core/yaml rejects flow indicators in keys, so the file could not be read back"
	case strings.HasPrefix(key, "- "):
		return "a '- ' prefix re-parses the entry as a list item"
	case strings.Contains(key, "\t"):
		return "core/yaml rejects tabs anywhere in a line, so the file could not be read back"
	case key != strings.TrimSpace(key):
		return "edge whitespace is trimmed on re-parse, so the key would change"
	}
	return ""
}

func writeFlowList(sb *strings.Builder, list []any) {
	sb.WriteByte('[')
	for i, v := range list {
		if i > 0 {
			sb.WriteString(", ")
		}
		writeScalarInline(sb, v)
	}
	sb.WriteByte(']')
}

func writeScalarInline(sb *strings.Builder, v any) {
	switch t := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		if t {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case int:
		sb.WriteString(strconv.Itoa(t))
	case int64:
		sb.WriteString(strconv.FormatInt(t, 10))
	case float64:
		sb.WriteString(formatYAMLFloat(t))
	case string:
		sb.WriteString(quoteYAMLString(t))
	default:
		sb.WriteString(quoteYAMLString(fmt.Sprint(t)))
	}
}

// formatYAMLFloat keeps a float a float on re-parse: the parser only types a
// scalar as float64 when it contains ".eE", so 99.0 must print as "99.0".
func formatYAMLFloat(f float64) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func allScalars(list []any) bool {
	for _, v := range list {
		switch v.(type) {
		case map[string]any, []any:
			return false
		}
	}
	return true
}

// quoteYAMLString double-quotes s for core/yaml's parseQuoted, which runs
// strconv.Unquote on the quoted region. Unquote replaces invalid UTF-8 bytes
// with U+FFFD, so before this escaped them, a value like "a\xffb'" came back
// as "a\ufffdb'" — corruption this serializer introduced, not parser noise.
// Invalid bytes and non-printable runes are therefore emitted as the Go
// escape forms Unquote decodes back to the exact bytes (\xNN, \uNNNN,
// \UNNNNNN); printable multi-byte runes (café, 日本語) pass through
// literally, and an apostrophe needs no escape inside double quotes.
func quoteYAMLString(s string) string {
	if !needsQuote(s) {
		return s
	}
	return quoteYAMLStringEscaped(s)
}

// quoteYAMLStringEscaped is quoteYAMLString's always-quote form, for callers
// that have a context-specific reason to quote beyond needsQuote's
// value-context rules (writeYAMLListItem: a ':' anywhere in a block-list
// item is the map-cut delimiter). Same escaping contract as
// quoteYAMLString: exact bytes back through parseQuoted's strconv.Unquote.
func quoteYAMLStringEscaped(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == utf8.RuneError && size == 1:
			b.WriteString(fmt.Sprintf(`\x%02x`, s[i-1]))
		case !unicode.IsPrint(r):
			switch {
			case r < 0x80:
				b.WriteString(fmt.Sprintf(`\x%02x`, r))
			case r < 0x10000:
				b.WriteString(fmt.Sprintf(`\u%04x`, r))
			default:
				b.WriteString(fmt.Sprintf(`\U%08x`, r))
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// needsQuote reports whether emitting s bare would re-parse as a non-string (or
// break the line/map grammar). Conservative but minimal: keeps the output close
// to hand-written YAML while guaranteeing string values survive the round-trip.
func needsQuote(s string) bool {
	if s == "" {
		return true
	}
	switch strings.ToLower(s) {
	case "true", "false", "null", "~":
		return true
	}
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil && strings.ContainsAny(s, ".eE") {
		return true
	}
	// Indicators that are only special as the FIRST character of a scalar:
	// block/flow openers, anchors (&), aliases (*), tags (!), block scalars
	// (| >), quotes, directives (%), reserved (@ `), a leading comment (#),
	// complex keys (?), and a leading ":" or "-".
	if strings.ContainsAny(s[:1], "&*!|>'\"%@`#?: -") {
		return true
	}
	// Flow indicators are special ANYWHERE rather than only at the front, because
	// writeFlowList emits scalars inside `[a, b]`. An interior comma there is
	// an ITEM SEPARATOR: "open,closed" emitted bare re-parses as two items,
	// which breaks the exact-inverse contract this file's banner claims for
	// encodeAppSpecYAML/decodeAppSpec and lets a `values:` enum set gain a
	// member the app never declared (unlike search_fields/fields/filters/
	// columns, `values:` is not re-checked against the declaration on decode).
	// Testing only s[:1] was the bug. Quoting these in block context too costs
	// a pair of quotes and keeps ONE predicate for both contexts.
	if strings.ContainsAny(s, ",[]{}") {
		return true
	}
	// A quote character ANYWHERE, not only leading. In a flow list the
	// apostrophe in a bare `60'` OPENS a quoted region (splitInline tracks
	// quote state across the whole list): [60', 90'] re-parses as ONE member
	// "60', 90'" — two enum values silently become one — and a lone [60']
	// fails to parse at all. Values can be quoted, so quote them rather than
	// refuse; quoteYAMLString double-quotes the value, escaping `"` (plus
	// backslash and the control/no-UTF-8 cases) — the apostrophe itself needs
	// no escape inside double quotes and passes through verbatim. Mirror
	// image of the KEY-side rule in yamlKeyRejectReason, which refuses
	// because core/yaml never unquotes keys (#323 vs #317).
	if strings.ContainsAny(s, "'\"") {
		return true
	}
	if strings.Contains(s, ": ") || strings.Contains(s, " #") {
		return true
	}
	if strings.ContainsAny(s, "\n\t") {
		return true
	}
	if s != strings.TrimSpace(s) || strings.HasSuffix(s, ":") {
		return true
	}
	return false
}

// ----- key orders (readability; semantics are order-independent) -------------
var (
	// Ordered to match appSpecToMap's emission order, which is also the
	// order the app spec docs present these keys in. A key the serializer
	// emits but this list omits ships unsorted, which
	// TestPackSerializerCoversEveryAppSpecField catches.
	topLevelOrder  = []string{"app", "entities", "screens", "nav", "seed", "endpoints", "hooks", "middleware", "plugins", "helpers"}
	appOrder       = []string{"name", "description", "base_url", "module", "db", "static_dir", "output_dir", "api_prefix", "public_openapi", "theme", "auth", "admin", "pwa", "llm_md", "locale_prefixes"}
	entityOrder    = []string{"name", "table", "primary_key", "scope", "pagination", "exposure", "search_fields", "timestamps", "properties", "renames", "indices", "fields", "relations", "display", "states"}
	fieldOrder     = []string{"name", "type", "required", "unique", "default", "max", "min", "pattern", "values", "to", "many", "auto_generate", "read_only", "hidden", "no_query"}
	screenOrder    = []string{"name", "route", "title", "description", "type", "layout", "access", "body"}
	blockOrder     = []string{"kind", "type", "text", "level", "entity", "fields", "limit", "create", "bulk", "empty_text", "class", "href", "mode", "island", "widget", "props", "children", "actions"}
	relationOrder  = []string{"type", "name", "entity", "foreign_key", "through", "local_key", "foreign_key_target", "on_delete", "cascade_write"}
	indexOrder     = []string{"name", "columns", "unique"}
	navOrder       = []string{"label", "href", "icon", "role", "items"}
	accessOrder    = []string{"auth", "role", "read", "create", "update", "delete"}
	readScopeOrder = []string{"unrestricted", "filter"}
	predicateOrder = []string{"field", "op", "value", "values"}
	dbOrder        = []string{"driver", "url"}
	authOrder      = []string{"enabled", "dev_mode", "base_path", "jwt_secret"}
	adminOrder     = []string{"path", "role", "enabled", "login_path", "seed_email", "seed_password"}
	endpointOrder  = []string{"name", "entity", "method", "path", "handler", "description", "mcp", "public"}
	actionOrder    = []string{"name", "event", "client_js"}
	stubOrder      = []string{"name", "description"}
	seedOrder      = []string{"entity", "count", "weights", "rows"}
)

func orderFor(key string) []string {
	switch key {
	case "app":
		return appOrder
	case "entities":
		return entityOrder
	case "screens":
		return screenOrder
	case "body", "children":
		return blockOrder
	case "fields":
		return fieldOrder
	case "relations":
		return relationOrder
	case "indices":
		return indexOrder
	case "nav", "items":
		return navOrder
	case "access":
		return accessOrder
	case "read_scope":
		return readScopeOrder
	case "filter":
		return predicateOrder
	case "db":
		return dbOrder
	case "auth":
		return authOrder
	case "admin":
		return adminOrder
	case "endpoints":
		return endpointOrder
	case "actions":
		return actionOrder
	case "seed":
		return seedOrder
	case "middleware", "plugins", "helpers":
		return stubOrder
	default:
		return nil
	}
}

// =============================================================================
// AST readers: reconstruct a App spec from a generated app's Go source.
// The generator emits a known, finite grammar; these readers reverse it.
// =============================================================================

func packParseFile(path string) (*ast.File, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	return packResolvePhrases(file), nil
}

// returnValue returns the first expression of the named func's return stmt.
//
// A `return <name>` is followed back to the composite literal that `name` was
// assigned. Once a sidebar had to resolve its auth control per request, its
// builder stopped being a single `return ui.SidebarConfig{…}` and became
// `cfg := ui.SidebarConfig{…}` … `return cfg`, at which point this returned
// an *ast.Ident, every caller's type assertion to *ast.CompositeLit failed,
// and pack silently reconstructed an empty nav. Silently, because a failed
// assertion here returns "no nav found", which is indistinguishable from an
// app that declares none.
func returnValue(file *ast.File, fn string) ast.Expr {
	body := appgen.FuncBody(file, fn)
	for _, stmt := range body {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		ident, ok := ret.Results[0].(*ast.Ident)
		if !ok {
			return ret.Results[0]
		}
		if lit := assignedLiteral(body, ident.Name); lit != nil {
			return lit
		}
		return ret.Results[0]
	}
	return nil
}

// assignedLiteral finds the composite literal assigned to name in body, taking
// the LAST assignment so a later rebind wins; the same answer the running
// program produces.
func assignedLiteral(body []ast.Stmt, name string) ast.Expr {
	var found ast.Expr
	for _, stmt := range body {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			continue
		}
		lhs, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || lhs.Name != name {
			continue
		}
		if lit, ok := assign.Rhs[0].(*ast.CompositeLit); ok {
			found = lit
		}
	}
	return found
}

// packHookStubComment matches the line renderAppSpecStubs writes above a
// hook stub: `// <handler> implements hook "<id>" (entity <e>, <when>): <desc>`.
var packHookStubComment = regexp.MustCompile(`^// (\w+) implements hook ("(?:[^"\\]|\\.)*") \(entity [^,]*, [^)]*\): ?(.*)$`)

// packHelperStubComment matches the body of a helper stub:
// `// TODO: implement helper "<name>".`
var packHelperStubComment = regexp.MustCompile(`^// TODO: implement helper ("(?:[^"\\]|\\.)*")\.$`)

// packStubName turns the CamelCase root a stub was emitted under back into
// a spec name. The emitter applied toCamelCase, which is lossy, so pack
// keeps whichever spelling toCamelCase maps back to the same root.
func packStubName(camel string) string {
	if snake := appspec.ToSnakeCase(camel); appspec.ToCamelCase(snake) == camel {
		return snake
	}
	return camel
}

// packReadStubs reconstructs middleware:, plugins:, helpers: and hooks:
// from their wiring (app.go's Use and RegisterPlugin calls, main.go's
// RegisterHook calls) and the stubs renderAppSpecStubs wrote in stubs.go.
// The wiring decides what exists and in which order; stubs.go only adds
// the names and descriptions the wiring cannot carry. A stub whose body
// the author has since written still packs, since its wiring is unchanged.
func packReadStubs(dir string, bp *appspec.AppSpec) error {
	stubs, stubComments := map[string]*ast.FuncDecl{}, map[string]string{}
	var helpers []appspec.AppSpecNamedStub
	stubsPath := filepath.Join(dir, "stubs.go")
	if _, err := os.Stat(stubsPath); err == nil {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, stubsPath, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("parse %s: %w", stubsPath, err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			key := fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) == 1 {
				key = astSelLast(fn.Recv.List[0].Type) + "." + key
			}
			stubs[key] = fn
			if fn.Doc != nil {
				for _, c := range fn.Doc.List {
					if m := packHookStubComment.FindStringSubmatch(c.Text); m != nil && m[1] == fn.Name.Name {
						stubComments[fn.Name.Name] = c.Text
					}
				}
			}
			if fn.Recv == nil && fn.Type.Params.NumFields() == 0 && fn.Type.Results.NumFields() == 0 && fn.Body != nil {
				name := packStubName(fn.Name.Name)
				for _, cg := range file.Comments {
					if cg.Pos() < fn.Body.Lbrace || cg.End() > fn.Body.Rbrace {
						continue
					}
					if m := packHelperStubComment.FindStringSubmatch(cg.List[0].Text); m != nil {
						if s, err := strconv.Unquote(m[1]); err == nil && appspec.ToCamelCase(s) == fn.Name.Name {
							name = s
						}
					}
				}
				helpers = append(helpers, appspec.AppSpecNamedStub{Name: name})
			}
		}
	}
	bp.Helpers = helpers

	if appFile, err := packParseFile(filepath.Join(dir, "app.go")); err == nil {
		ast.Inspect(appFile, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			switch astSelLast(call.Fun) {
			case "Use":
				id, ok := call.Args[0].(*ast.Ident)
				if !ok {
					return true
				}
				root, isStub := strings.CutSuffix(id.Name, "Middleware")
				if isStub && stubs[id.Name] != nil {
					bp.Middleware = append(bp.Middleware, appspec.AppSpecNamedStub{Name: packStubName(root)})
				}
			case "RegisterPlugin":
				lit, ok := call.Args[0].(*ast.CompositeLit)
				if !ok {
					return true
				}
				typ := astSelLast(lit.Type)
				root, isStub := strings.CutSuffix(typ, "Plugin")
				if !isStub || stubs[typ+".Init"] == nil {
					return true
				}
				name := packStubName(root)
				if fn := stubs[typ+".Name"]; fn != nil && len(fn.Body.List) == 1 {
					if ret, ok := fn.Body.List[0].(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
						if s := appspec.AstString(ret.Results[0]); s != "" {
							name = s
						}
					}
				}
				bp.Plugins = append(bp.Plugins, appspec.AppSpecNamedStub{Name: name})
			}
			return true
		})
	}

	mainFile, err := packParseFile(filepath.Join(dir, "main.go"))
	if err != nil {
		return nil
	}
	lifecycles := map[string]string{}
	for when, expr := range appspec.AppSpecHookTypes {
		lifecycles[strings.TrimPrefix(expr, "framework.")] = when
	}
	// A hook owns its stub, so a handler registered twice by hand packs as
	// its first registration; the check then names main.go.
	seen := map[string]bool{}
	ast.Inspect(mainFile, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || astSelLast(call.Fun) != "RegisterHook" || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		reg, ok := sel.X.(*ast.CallExpr)
		if !ok || astSelLast(reg.Fun) != "HookRegistry" || len(reg.Args) != 1 {
			return true
		}
		handler, ok := call.Args[1].(*ast.Ident)
		when, known := lifecycles[astSelLast(call.Args[0])]
		if !ok || !known || stubs[handler.Name] == nil || seen[handler.Name] {
			return true
		}
		seen[handler.Name] = true
		hook := appspec.AppSpecHook{ID: handler.Name, Entity: appspec.AstString(reg.Args[0]), When: when, Handler: handler.Name}
		if m := packHookStubComment.FindStringSubmatch(stubComments[handler.Name]); m != nil {
			if id, err := strconv.Unquote(m[2]); err == nil {
				hook.ID = id
			}
			hook.Description = m[3]
		}
		bp.Hooks = append(bp.Hooks, hook)
		return true
	})
	return nil
}

// packReadSeed reconstructs seed data from stubs.go seedData().
func packReadSeed(dir string) ([]appspec.AppSpecSeedEntity, error) {
	path := filepath.Join(dir, "stubs.go")
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	file, err := packParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	lit, ok := returnValue(file, "seedData").(*ast.CompositeLit)
	if !ok {
		return nil, nil
	}
	var out []appspec.AppSpecSeedEntity
	for _, el := range lit.Elts {
		sv := appspec.FieldVals(el)
		se := appspec.AppSpecSeedEntity{Entity: appspec.AstString(sv["Entity"])}
		if rowsLit, ok := sv["Rows"].(*ast.CompositeLit); ok {
			for _, rowEl := range rowsLit.Elts {
				if m, ok := appspec.AstAny(rowEl).(map[string]any); ok {
					se.Rows = append(se.Rows, m)
				}
			}
		}
		out = append(out, se)
	}
	return out, nil
}

// packReadNav reconstructs the sidebar nav from app.go
// sidebarConfig() ui.SidebarConfig{Items: []ui.SidebarItem{...}}.
func packReadNav(dir string) ([]appspec.AppSpecNavItem, error) {
	path := filepath.Join(dir, "app.go")
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	file, err := packParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg, ok := returnValue(file, "sidebarConfig").(*ast.CompositeLit)
	if !ok {
		return nil, nil
	}
	return packNavItems(appspec.FieldVals(cfg)["Items"]), nil
}

// packNavItems reads one []ui.SidebarItem literal, and each item's
// Children the same way.
func packNavItems(e ast.Expr) []appspec.AppSpecNavItem {
	itemsLit, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	var out []appspec.AppSpecNavItem
	for _, el := range itemsLit.Elts {
		iv := appspec.FieldVals(el)
		role := ""
		if rolesLit, ok := iv["Roles"].(*ast.CompositeLit); ok && len(rolesLit.Elts) > 0 {
			role = appspec.AstString(rolesLit.Elts[0])
		}
		out = append(out, appspec.AppSpecNavItem{
			Label: appspec.AstString(iv["Label"]),
			Href:  appspec.AstString(iv["Href"]),
			Icon:  packIconName(iv["Icon"]),
			Role:  role,
			Items: packNavItems(iv["Children"]),
		})
	}
	return out
}

// packOutputDir recovers app.output_dir from main.go's import of the
// entities package: renderAppSpecMain imports <module>/<output_dir>/entities,
// so whatever sits between the module and /entities is the output dir.
func packOutputDir(mainFile *ast.File, module string) string {
	if module == "" {
		return ""
	}
	for _, imp := range mainFile.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		rest, ok := strings.CutPrefix(p, module+"/")
		if !ok {
			continue
		}
		if dir, ok := strings.CutSuffix(rest, "/entities"); ok {
			return dir
		}
	}
	return ""
}

// packIconName reads the icon name out of the ui.Icon("name",
// ui.IconConfig{}) call renderNavItemGo emits.
func packIconName(e ast.Expr) string {
	call, ok := e.(*ast.CallExpr)
	if !ok || astSelLast(call.Fun) != "Icon" || len(call.Args) == 0 {
		return ""
	}
	return appspec.AstString(call.Args[0])
}

// ----- P3: app config + theme + auth + admin --------------------------------

// packReadApp reconstructs the app config (consts + theme + auth + admin) from
// app.go and main.go.
func packReadApp(dir string) (appspec.AppSpecApp, error) {
	app := appspec.AppSpecApp{APIPrefix: "api"}
	appPath := filepath.Join(dir, "app.go")
	appFile, err := packParseFile(appPath)
	if err != nil {
		return app, fmt.Errorf("parse %s: %w", appPath, err)
	}
	// Consts.
	consts := map[string]string{}
	for _, d := range appFile.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			consts[vs.Names[0].Name] = appspec.AstString(vs.Values[0])
		}
	}
	app.Name = consts["appName"]
	app.Module = consts["appModule"]
	app.DBDriver = consts["dbDriver"]
	app.DBURL = consts["dbURL"]
	app.StaticDir = consts["staticDir"]
	if v, ok := consts["apiPrefix"]; ok {
		app.APIPrefix = strings.Trim(v, "/")
	}

	// Theme.
	app.Theme, app.ThemeDark = packReadTheme(appFile)

	// Auth + admin-seed, found anywhere in app.go.
	ast.Inspect(appFile, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.CompositeLit:
			if astSelLast(t.Type) == "AuthConfig" {
				app.Auth.Enabled = true
				av := appspec.FieldVals(t)
				app.Auth.DevMode = appspec.AstBool(av["DevMode"])
				app.Auth.BasePath = appspec.AstString(av["BasePath"])
				app.Auth.JWTSecret = appspec.AstString(av["JWTSecret"])
			}
		case *ast.CallExpr:
			switch astSelLast(t.Fun) {
			case "HashPassword":
				if len(t.Args) == 1 {
					app.Admin.SeedPassword = appspec.AstString(t.Args[0])
				}
			case "CreateUser":
				if len(t.Args) >= 2 {
					app.Admin.SeedEmail = appspec.AstString(t.Args[1])
				}
			}
		}
		return true
	})

	// Admin, PWA, LLM-markdown and locale-prefix config live in main.go.
	if mainFile, err := packParseFile(filepath.Join(dir, "main.go")); err == nil {
		app.OutputDir = packOutputDir(mainFile, app.Module)
		ast.Inspect(mainFile, func(n ast.Node) bool {
			switch t := n.(type) {
			case *ast.CompositeLit:
				switch astSelLast(t.Type) {
				case "Config":
					cv := appspec.FieldVals(t)
					if _, ok := cv["PathPrefix"]; !ok {
						return true // not an admin.Config
					}
					app.Admin.Enabled = true
					app.Admin.Path = appspec.AstString(cv["PathPrefix"])
					app.Admin.Role = appspec.AstString(cv["AdminRole"])
					app.Admin.LoginPath = appspec.AstString(cv["LoginPath"])
				case "PWAConfig":
					cv := appspec.FieldVals(t)
					app.PWA.Enabled = true
					app.PWA.Name = appspec.AstString(cv["Name"])
					app.PWA.ShortName = appspec.AstString(cv["ShortName"])
					app.PWA.Description = appspec.AstString(cv["Description"])
					app.PWA.StartURL = appspec.AstString(cv["StartURL"])
					app.PWA.Scope = appspec.AstString(cv["Scope"])
					app.PWA.Display = packPWADisplay(astSelLast(cv["Display"]))
					app.PWA.ThemeColor = appspec.AstString(cv["ThemeColor"])
					app.PWA.BackgroundColor = appspec.AstString(cv["BackgroundColor"])
				}
			case *ast.CallExpr:
				switch astSelLast(t.Fun) {
				case "WithPublicLLMMD":
					app.LLMMD = true
				case "WithLocalePrefixes":
					app.LocalePrefixes = true
				}
			}
			return true
		})
	}

	// Secrets moved out of committed source into the generated .env
	// (JWT_SECRET, DATABASE_URL, ADMIN_SEED_PASSWORD). Recover them from
	// there so pack round-trips the authored app spec. Absent .env → the
	// fields stay as recovered from source (empty for the env-backed ones).
	env := packReadDotEnv(filepath.Join(dir, ".env"))
	if v := env["JWT_SECRET"]; v != "" {
		app.Auth.JWTSecret = v
	}
	if v := env["DATABASE_URL"]; v != "" {
		app.DBURL = v
	}
	if v := env["ADMIN_SEED_PASSWORD"]; v != "" {
		app.Admin.SeedPassword = v
	}
	return app, nil
}

// packReadDotEnv parses a generated .env into a map using the SAME
// parser the generated app boots with (core/dotenv); a private
// re-implementation here silently diverged on quoted values, so pack
// read back a different secret than the app ran with. Missing or
// unparseable file → empty map.
func packReadDotEnv(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return map[string]string{}
	}
	defer f.Close()
	out, err := dotenv.Parse(f)
	if err != nil {
		return map[string]string{}
	}
	return out
}

// packPWADisplay maps a generated uihost.PWADisplay* constant name back
// to the app spec's display string, the reverse of
// appSpecPWADisplayConst. Unknown/absent → "" (the omitted default).
func packPWADisplay(constName string) string {
	switch constName {
	case "PWADisplayStandalone":
		return "standalone"
	case "PWADisplayFullscreen":
		return "fullscreen"
	case "PWADisplayMinimalUI":
		return "minimal-ui"
	case "PWADisplayBrowser":
		return "browser"
	}
	return ""
}

// astSelLast returns the trailing identifier of a selector/ident type expr
// (auth.AuthConfig → "AuthConfig", admin.Config → "Config").
func astSelLast(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// packReadTheme parses appTheme()'s assignments back into the authored
// theme map (colors + font_heading/font_body) + the dark-scheme overrides.
func packReadTheme(file *ast.File) (map[string]string, map[string]string) {
	var light, dark map[string]string
	for _, stmt := range appgen.FuncBody(file, "appTheme") {
		asn, ok := stmt.(*ast.AssignStmt)
		if !ok || len(asn.Lhs) != 1 || len(asn.Rhs) != 1 {
			continue
		}
		path := selectorPath(asn.Lhs[0])
		switch {
		case len(path) == 4 && path[1] == "Colors" && path[3] == "Value":
			if light == nil {
				light = map[string]string{}
			}
			light[toKebabCase(path[2])] = appspec.AstString(asn.Rhs[0])
		case len(path) == 4 && path[1] == "Fonts" && path[3] == "Value":
			if light == nil {
				light = map[string]string{}
			}
			light["font_"+strings.ToLower(path[2])] = firstFontFamily(appspec.AstString(asn.Rhs[0]))
		case len(path) == 2 && path[1] == "DarkColors":
			if m, ok := appspec.AstAny(asn.Rhs[0]).(map[string]any); ok {
				dark = map[string]string{}
				for k, v := range m {
					if s, ok := v.(string); ok {
						dark[k] = s
					}
				}
			}
		}
	}
	return light, dark
}

// selectorPath flattens a.b.c.d into ["a","b","c","d"].
func selectorPath(e ast.Expr) []string {
	switch t := e.(type) {
	case *ast.Ident:
		return []string{t.Name}
	case *ast.SelectorExpr:
		return append(selectorPath(t.X), t.Sel.Name)
	}
	return nil
}

// firstFontFamily extracts the authored family from a generated font stack:
// "'Hanken Grotesk', ui-sans-serif, …" → "Hanken Grotesk".
func firstFontFamily(stack string) string {
	first := stack
	if i := strings.IndexByte(stack, ','); i >= 0 {
		first = stack[:i]
	}
	first = strings.TrimSpace(first)
	first = strings.Trim(first, "'\"")
	return first
}

// ----- P6: screens (the hard reverse of screens.go + app.go registrations) ---

type screenReg struct {
	typeName string
	route    string
	layout   string
	authed   bool
	role     string
}

// packReadScreens reconstructs the authored screens. It reads the per-screen
// layout (screen_*.go + the screenRegistrar seam) when present, and falls back
// to the legacy aggregated layout (screens.go structs + app.go registrations)
// so apps generated before the per-screen split still pack. In both layouts
// synthesized /new + /{id}/edit form screens (body is a resource Form call)
// are dropped: they weren't authored.
func packReadScreens(dir string) ([]appspec.AppSpecScreen, error) {
	if screens, err := packReadPerScreenFiles(dir); err != nil {
		return nil, err
	} else if screens != nil {
		return screens, nil
	}
	// The screens_register.go seam is emitted for every per-file-layout app,
	// even one with zero screens. Its presence means the layout is per-file
	// and simply has no authored screens; do NOT fall through to the legacy
	// reader (which expects a screens.go that this layout never writes).
	if _, err := os.Stat(filepath.Join(dir, "screens_register.go")); err == nil {
		return nil, nil
	}
	return packReadLegacyScreens(dir)
}

// packReadPerScreenFiles reads the per-screen generated files (screen_*.go)
// and returns the authored screens in declaration order. The walk and every
// leaf read go through an *os.Root over dir, so a symlinked leaf pointing
// outside the project is skipped, never parsed (2026-09-05 red-probe round;
// same walk shape as packReadPerEntityFiles). Returns (nil, nil) when the
// project uses the legacy aggregated layout (no screen_*.go files), so the
// caller falls back to packReadLegacyScreens.
func packReadPerScreenFiles(dir string) ([]appspec.AppSpecScreen, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil // no output dir yet
	}
	defer root.Close()
	entries, err := appgen.RootReadDirEntries(root, ".")
	if err != nil {
		return nil, nil // no output dir yet
	}
	parseRoot := func(rel string) (*ast.File, error) {
		src, rerr := root.ReadFile(rel)
		if rerr != nil {
			return nil, rerr
		}
		file, err := parser.ParseFile(token.NewFileSet(), rel, src, 0)
		if err != nil {
			return nil, err
		}
		return packResolvePhrases(file), nil
	}
	var screenNames []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// The seam (screens_register.go) and the shared nodeComponent helper
		// (screen_shared.go) carry no screen type or mount; skip them. The
		// per-screen files all start with "screen_".
		if name == "screen_shared.go" || !strings.HasPrefix(name, "screen_") {
			continue
		}
		screenNames = append(screenNames, name)
	}
	if len(screenNames) == 0 {
		return nil, nil
	}
	// Helpers (package-local zero-arg fns returning one expr) may live in
	// app.go or any screen file; index them all so reverseEntityResource can
	// follow one hop when a screen body calls a shared helper.
	helperFiles := []*ast.File{}
	if appFile, aerr := parseRoot("app.go"); aerr == nil {
		helperFiles = append(helperFiles, appFile)
	}
	titles := map[string]string{}
	descs := map[string]string{}
	bodies := map[string][]appspec.AppSpecBlock{}
	var orderedRegs []screenOrderReg
	for _, name := range screenNames {
		file, perr := parseRoot(name)
		if perr != nil {
			continue // unreadable leaf (symlink outside the root) or unparsable: skip
		}
		helperFiles = append(helperFiles, file)
		fnDecls := map[string]*ast.FuncDecl{}
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Name != nil {
				fnDecls[fn.Name.Name] = fn
			}
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			recv := appgen.RecvTypeName(fn.Recv.List[0].Type)
			switch fn.Name.Name {
			case "ScreenTitle":
				titles[recv] = returnString(fn)
			case "ScreenDescription":
				descs[recv] = returnString(fn)
			}
		}
		orderedRegs = append(orderedRegs, packScreenRegistrarsFromFile(file, fnDecls)...)
	}
	helpers := packHelperReturns(helperFiles...)
	// Render bodies need the helpers map; rescan now that helpers are known.
	for _, name := range screenNames {
		file, _ := parseRoot(name)
		if file == nil {
			continue
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			if fn.Name.Name == "Render" || fn.Name.Name == "RenderCtx" {
				recv := appgen.RecvTypeName(fn.Recv.List[0].Type)
				bodies[recv] = reverseRenderBody(fn, helpers)
			}
		}
	}
	sort.SliceStable(orderedRegs, func(i, j int) bool { return orderedRegs[i].order < orderedRegs[j].order })
	var out []appspec.AppSpecScreen
	for _, r := range orderedRegs {
		body := bodies[r.reg.typeName]
		if isSynthesizedBody(body) {
			continue
		}
		s := appspec.AppSpecScreen{
			Name:   typeNameToScreenName(r.reg.typeName),
			Route:  paramToBrace(r.reg.route),
			Title:  titles[r.reg.typeName],
			Layout: r.reg.layout,
			Body:   body,
		}
		if d := descs[r.reg.typeName]; d != "" {
			s.Description = d
		}
		if r.reg.authed {
			s.Access = appspec.AppSpecAccess{Auth: true, Role: r.reg.role}
		}
		out = append(out, s)
	}
	return out, nil
}

type screenOrderReg struct {
	order int
	reg   screenReg
}

// packScreenRegistrarsFromFile finds every screenRegistrar{order: N, fn: X}
// literal in a per-screen file and resolves X (a named mount func declared in
// the same file) to the site.Register / site.RegisterScreen call in its body,
// yielding one screenOrderReg per mounted screen. Resource-only mount funcs
// (no screen call) contribute nothing.
func packScreenRegistrarsFromFile(file *ast.File, fnDecls map[string]*ast.FuncDecl) []screenOrderReg {
	var out []screenOrderReg
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		id, ok := cl.Type.(*ast.Ident)
		if !ok || id.Name != "screenRegistrar" {
			return true
		}
		var order int
		var fnName string
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			switch appgen.IdentName(kv.Key) {
			case "order":
				if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.INT {
					if n, err := strconv.Atoi(lit.Value); err == nil {
						order = n
					}
				}
			case "fn":
				fnName = appgen.IdentName(kv.Value)
			}
		}
		if fnName == "" {
			return false
		}
		fn := fnDecls[fnName]
		if fn == nil || fn.Body == nil {
			return false
		}
		for _, stmt := range fn.Body.List {
			es, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue // skip the appResources assignment, etc.
			}
			call, ok := es.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			r := screenReg{}
			switch sel.Sel.Name {
			case "Register": // site.Register(route, &XScreen{}, layout)
				if len(call.Args) == 3 {
					r.route = appspec.AstString(call.Args[0])
					r.typeName = compositeTypeName(call.Args[1])
					r.layout = layoutVarToName(call.Args[2])
				}
			case "RegisterScreen":
				if len(call.Args) == 2 {
					r.layout = layoutVarToName(call.Args[1])
					packWalkScreenChain(screenArg(call.Args[0], fn), &r)
				}
			default:
				continue
			}
			if r.typeName != "" {
				out = append(out, screenOrderReg{order: order, reg: r})
				return false
			}
		}
		return false
	})
	return out
}

// screenArg resolves a RegisterScreen argument that names a local variable
// holding the screen: the generator's drawer-intercept mount builds the
// screen, sets its Intercept field, then registers the variable. Anything
// else is the expression itself.
func screenArg(e ast.Expr, fn *ast.FuncDecl) ast.Expr {
	id, ok := e.(*ast.Ident)
	if !ok || fn.Body == nil {
		return e
	}
	for _, stmt := range fn.Body.List {
		as, ok := stmt.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			continue
		}
		if lhs, ok := as.Lhs[0].(*ast.Ident); ok && lhs.Name == id.Name {
			return as.Rhs[0]
		}
	}
	return e
}

// packReadLegacyScreens reads the pre-per-screen aggregated layout: screen
// types in screens.go, registrations in app.go's RegisterGenerated.
func packReadLegacyScreens(dir string) ([]appspec.AppSpecScreen, error) {
	appFile, err := packParseFile(filepath.Join(dir, "app.go"))
	if err != nil {
		return nil, err
	}
	regs := packReadScreenRegs(appFile)
	// A legacy app with no screens never emitted screens.go; that's zero
	// authored screens, not an error.
	if _, statErr := os.Stat(filepath.Join(dir, "screens.go")); os.IsNotExist(statErr) {
		return nil, nil
	}
	scrFile, err := packParseFile(filepath.Join(dir, "screens.go"))
	if err != nil {
		return nil, err
	}
	helpers := packHelperReturns(appFile, scrFile)
	titles := map[string]string{}
	descs := map[string]string{}
	bodies := map[string][]appspec.AppSpecBlock{}
	for _, d := range scrFile.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}
		recv := appgen.RecvTypeName(fn.Recv.List[0].Type)
		switch fn.Name.Name {
		case "ScreenTitle":
			titles[recv] = returnString(fn)
		case "ScreenDescription":
			descs[recv] = returnString(fn)
		case "Render", "RenderCtx":
			bodies[recv] = reverseRenderBody(fn, helpers)
		}
	}
	var out []appspec.AppSpecScreen
	for _, r := range regs {
		body := bodies[r.typeName]
		if isSynthesizedBody(body) {
			continue
		}
		s := appspec.AppSpecScreen{
			Name:   typeNameToScreenName(r.typeName),
			Route:  paramToBrace(r.route),
			Title:  titles[r.typeName],
			Layout: r.layout,
			Body:   body,
		}
		if d := descs[r.typeName]; d != "" {
			s.Description = d
		}
		if r.authed {
			s.Access = appspec.AppSpecAccess{Auth: true, Role: r.role}
		}
		out = append(out, s)
	}
	return out, nil
}

// packReadScreenRegs reads site.Register / site.RegisterScreen(...) calls.
func packReadScreenRegs(file *ast.File) []screenReg {
	var regs []screenReg
	for _, stmt := range appgen.FuncBody(file, "RegisterGenerated") {
		es, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := es.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		switch sel.Sel.Name {
		case "Register": // site.Register(route, &XScreen{}, layout)
			if len(call.Args) == 3 {
				regs = append(regs, screenReg{
					route:    appspec.AstString(call.Args[0]),
					typeName: compositeTypeName(call.Args[1]),
					layout:   layoutVarToName(call.Args[2]),
				})
			}
		case "RegisterScreen": // site.RegisterScreen(app.NewScreen(route, &X{}).WithTitle(..).WithPolicy(appSpecAuthPolicy(login, role)), layout)
			if len(call.Args) == 2 {
				r := screenReg{layout: layoutVarToName(call.Args[1])}
				packWalkScreenChain(call.Args[0], &r)
				regs = append(regs, r)
			}
		}
	}
	return regs
}

func packWalkScreenChain(e ast.Expr, r *screenReg) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		switch sel.Sel.Name {
		case "NewScreen":
			if len(call.Args) == 2 {
				r.route = appspec.AstString(call.Args[0])
				r.typeName = compositeTypeName(call.Args[1])
			}
		case "WithPolicy":
			// arg is appSpecAuthPolicy(login, role)
			if len(call.Args) == 1 {
				if pc, ok := call.Args[0].(*ast.CallExpr); ok && len(pc.Args) == 2 {
					r.authed = true
					r.role = appspec.AstString(pc.Args[1])
				}
			}
		}
		packWalkScreenChain(sel.X, r)
	}
}

func layoutVarToName(e ast.Expr) string {
	switch appgen.IdentName(e) {
	case "marketingLayout":
		return "marketing"
	case "appLayout":
		return "app"
	}
	return ""
}

// compositeTypeName: &XScreen{} → "XScreen".
func compositeTypeName(e ast.Expr) string {
	if u, ok := e.(*ast.UnaryExpr); ok {
		e = u.X
	}
	if cl, ok := e.(*ast.CompositeLit); ok {
		return appgen.IdentName(cl.Type)
	}
	return ""
}

func returnString(fn *ast.FuncDecl) string {
	if fn.Body == nil {
		return ""
	}
	for _, stmt := range fn.Body.List {
		if ret, ok := stmt.(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
			return appspec.AstString(ret.Results[0])
		}
	}
	return ""
}

func typeNameToScreenName(tn string) string {
	tn = strings.TrimSuffix(tn, "Screen")
	return appspec.ToSnakeCase(tn)
}

func paramToBrace(route string) string {
	parts := strings.Split(route, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

func isSynthesizedBody(body []appspec.AppSpecBlock) bool {
	return len(body) == 1 && (body[0].Kind == "entity_create" || body[0].Kind == "entity_edit")
}

// reverseRenderBody finds the screen's root call and reverses each child
// expression into a block. helpers maps package-local zero-arg function
// names to their single return expression, so a resource chain extracted
// into a shared helper (screen + island endpoint reusing one config) still
// reverses.
//
// Three root shapes are accepted. The generator emits the screen stack,
// `ui.Stack(ui.StackConfig{Gap: ui.GapXL}, children…)`, for a screen with
// blocks, wrapped in `html.Div(html.DivConfig{…data-component…}, stack)`
// when the screen has actions. Apps generated before that used
// `html.Div(html.DivConfig{…}, children…)`, and older ones (and
// hand-written screens that never moved) `render.Tag("div", attrs,
// children…)`. pack has to read all three or it silently returns no
// blocks for the others, which is how a round-trip loses screens.
func reverseRenderBody(fn *ast.FuncDecl, helpers map[string]ast.Expr) []appspec.AppSpecBlock {
	if fn.Body == nil {
		return nil
	}
	for _, stmt := range fn.Body.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		call, ok := ret.Results[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		// Index of the first child argument: render.Tag takes (tag, attrs)
		// first, html.Div and ui.Stack take a single config.
		firstChild := -1
		switch callSel(call) {
		case "render.Tag":
			if len(call.Args) >= 2 {
				firstChild = 2
			}
		case "html.Div":
			if len(call.Args) >= 1 {
				firstChild = 1
			}
		case "ui.Stack":
			// Only the generator's own screen stack: a hand-written root
			// stack with its own Align or Justify would lose them.
			if isScreenStack(call) {
				firstChild = 1
			}
		}
		if firstChild < 0 {
			continue
		}
		// An html.Div whose one child is the screen stack is the actions
		// wrapper: the blocks are the stack's children.
		if callSel(call) == "html.Div" && len(call.Args) == 2 {
			if inner, ok := call.Args[1].(*ast.CallExpr); ok && isScreenStack(inner) {
				call, firstChild = inner, 1
			}
		}
		var out []appspec.AppSpecBlock
		for _, arg := range call.Args[firstChild:] {
			if b, ok := reverseBlock(arg, helpers); ok {
				out = append(out, b)
			}
		}
		return out
	}
	return nil
}

// isScreenStack reports whether call is the generator's screen stack,
// `ui.Stack(ui.StackConfig{Gap: ui.GapXL}, …)` (appSpecScreenStackOpen),
// as opposed to a stack block an author placed on the screen.
func isScreenStack(call *ast.CallExpr) bool {
	if callSel(call) != "ui.Stack" || len(call.Args) < 1 {
		return false
	}
	lit, ok := call.Args[0].(*ast.CompositeLit)
	if !ok || len(lit.Elts) != 1 {
		return false
	}
	kv, ok := lit.Elts[0].(*ast.KeyValueExpr)
	if !ok {
		return false
	}
	key, ok := kv.Key.(*ast.Ident)
	if !ok || key.Name != "Gap" {
		return false
	}
	sel, ok := kv.Value.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "GapXL"
}

// packHelperReturns indexes top-level zero-arg functions with a single
// `return <expr>` across the parsed files, for reverseEntityResource to
// follow one hop when a screen body calls a local helper.
func packHelperReturns(files ...*ast.File) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil ||
				(fn.Type.Params != nil && len(fn.Type.Params.List) > 0) ||
				len(fn.Body.List) != 1 {
				continue
			}
			ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				continue
			}
			out[fn.Name.Name] = ret.Results[0]
		}
	}
	return out
}

// callSel returns "pkg.Method" for a simple selector call, else "".
func callSel(call *ast.CallExpr) string {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if x := appgen.IdentName(sel.X); x != "" {
			return x + "." + sel.Sel.Name
		}
	}
	return ""
}

// reverseBlock turns one emitted UI expression back into an AppSpecBlock.
func reverseBlock(e ast.Expr, helpers map[string]ast.Expr) (appspec.AppSpecBlock, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return appspec.AppSpecBlock{}, false
	}
	// Entity builder chains (appUI.List("x")…): the entityui spelling the
	// generator emits now. The older appResources chains are read by
	// reverseEntityResource for apps generated before entityui.
	if b, ok := reverseEntityUI(e, helpers); ok {
		return b, true
	}
	// Entity resource chains (appResources["x"]…List/Detail/Form(ctx)):
	// the pre-entityui spelling, still read for apps generated then.
	if b, ok := reverseEntityResource(call, helpers); ok {
		return b, true
	}
	switch callSel(call) {
	case "ui.Hero":
		return reverseHero(call), true
	case "ui.Section":
		return reverseSection(call, helpers), true
	case "ui.Card":
		// A titled chart is emitted as ui.Card(Heading, <chart>): reverse
		// it back to the chart block, not a card, with the heading as its
		// title. A plain card has no chart child.
		if len(call.Args) > 1 {
			if child, ok := call.Args[1].(*ast.CallExpr); ok {
				if b, ok := reverseChartCall(child); ok {
					if b.Props == nil {
						b.Props = map[string]any{}
					}
					putStr(b.Props, "title", appspec.AstString(cfgOf(call, 0)["Heading"]))
					return b, true
				}
			}
		}
		c := cfgOf(call, 0)
		b := block("card", props2("heading", appspec.AstString(c["Heading"]), "text", appspec.AstString(c["Description"])))
		// The generator passes a card's children after the config:
		// ui.Card(cfg, children...).
		for _, arg := range call.Args[1:] {
			if child, ok := reverseBlock(arg, helpers); ok {
				b.Children = append(b.Children, child)
			}
		}
		return b, true
	case "ui.BarChart", "ui.PieChart", "lineChart":
		// Untitled chart (no wrapping ui.Card).
		if b, ok := reverseChartCall(call); ok {
			return b, true
		}
	case "ui.PageHeader":
		c := cfgOf(call, 0)
		return block("page_header", props2("title", appspec.AstString(c["Title"]), "subtitle", appspec.AstString(c["Subtitle"]), "eyebrow", appspec.AstString(c["Eyebrow"]))), true
	case "ui.LinkButton":
		c := cfgOf(call, 0)
		return block("link_button", props2("label", appspec.AstString(c["Label"]), "href", appspec.AstString(c["Href"]), "variant", buttonVariant(c["Variant"]))), true
	case "ui.Stack":
		return reverseLayoutBlock("stack", call, helpers), true
	case "ui.Cluster":
		return reverseLayoutBlock("cluster", call, helpers), true
	case "ui.Grid":
		// A grid of pricing cards is a `pricing` block; a grid of stat cards
		// is a stat_row. Every other grid preserves the explicit layout block.
		if len(call.Args) > 1 {
			if first, ok := call.Args[1].(*ast.CallExpr); ok && callSel(first) == "ui.PricingCard" {
				return reversePricing(call), true
			}
		}
		allStats := len(call.Args) > 1
		for _, arg := range call.Args[1:] {
			child, ok := arg.(*ast.CallExpr)
			if !ok || callSel(child) != "ui.StatCard" {
				allStats = false
				break
			}
		}
		if allStats {
			b := reverseLayoutBlock("stat_row", call, helpers)
			b.Props = nil
			return b, true
		}
		return reverseLayoutBlock("grid", call, helpers), true
	case "ui.StatCard":
		return reverseStatCard(call), true
	case "ui.Markdown":
		return appspec.AppSpecBlock{Kind: "markdown", Text: appspec.AstString(cfgOf(call, 0)["Source"])}, true
	case "ui.AuthCard":
		return reverseAuthCard(call), true
	case "html.Heading":
		c := cfgOf(call, 0)
		lvl := 1
		if n, ok := appspec.AstInt(c["Level"]); ok {
			lvl = n
		}
		return appspec.AppSpecBlock{Type: "heading", Level: lvl, Class: appspec.AstString(c["Class"]), Text: renderTextArg(call.Args[len(call.Args)-1])}, true
	case "render.Join":
		if b, ok := reverseEntityForm(call); ok {
			return b, true
		}
	case "html.Paragraph":
		return appspec.AppSpecBlock{Type: "paragraph", Class: appspec.AstString(cfgOf(call, 0)["Class"]), Text: renderTextArg(call.Args[len(call.Args)-1])}, true
	case "render.Tag":
		return reverseRenderTag(call)
	}
	return appspec.AppSpecBlock{}, false
}

// reverseEntityForm reads the entity_form block appSpecEntityFormExpr
// emits: render.Join(ui.PageHeader{Title}, ui.Form(cfg, <one control per
// field>)), whose ExtraAttrs carry the data-entity-form and
// data-entity-mode markers. Each control names its field in a Name key,
// directly or inside ui.FormField's Input func, in the order the form
// lists them, which is the allowlist that reproduces the form.
func reverseEntityForm(call *ast.CallExpr) (appspec.AppSpecBlock, bool) {
	if len(call.Args) != 2 {
		return appspec.AppSpecBlock{}, false
	}
	header, ok := call.Args[0].(*ast.CallExpr)
	if !ok || callSel(header) != "ui.PageHeader" {
		return appspec.AppSpecBlock{}, false
	}
	form, ok := call.Args[1].(*ast.CallExpr)
	if !ok || callSel(form) != "ui.Form" || len(form.Args) == 0 {
		return appspec.AppSpecBlock{}, false
	}
	merge, ok := cfgOf(form, 0)["ExtraAttrs"].(*ast.CallExpr)
	if !ok || callSel(merge) != "html.MergeAttrs" || len(merge.Args) == 0 {
		return appspec.AppSpecBlock{}, false
	}
	markers := map[string]string{}
	if lit, ok := merge.Args[0].(*ast.CompositeLit); ok {
		for _, el := range lit.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				markers[appspec.AstString(kv.Key)] = appspec.AstString(kv.Value)
			}
		}
	}
	if markers["data-entity-form"] == "" {
		return appspec.AppSpecBlock{}, false
	}
	b := appspec.AppSpecBlock{
		Kind:   "entity_form",
		Entity: markers["data-entity-form"],
		Mode:   markers["data-entity-mode"],
		Text:   appspec.AstString(cfgOf(header, 0)["Title"]),
	}
	for _, arg := range form.Args[1:] {
		name := ""
		ast.Inspect(arg, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if name != "" || !ok {
				return name == ""
			}
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Name" {
				name = appspec.AstString(kv.Value)
			}
			return name == ""
		})
		if name == "" {
			return appspec.AppSpecBlock{}, false
		}
		b.Fields = append(b.Fields, name)
	}
	return b, true
}

func reverseLayoutBlock(kind string, call *ast.CallExpr, helpers map[string]ast.Expr) appspec.AppSpecBlock {
	b := appspec.AppSpecBlock{Kind: kind, Props: map[string]any{}}
	cfg := cfgOf(call, 0)
	if kind == "grid" {
		putStr(b.Props, "min", appspec.AstString(cfg["Min"]))
	}
	if gap := reverseGap(appspec.AstSelName(cfg["Gap"])); gap != "" && gap != "md" {
		b.Props["gap"] = gap
	}
	if kind == "stack" || kind == "cluster" {
		// The generator emits Align/Justify only when the block set them,
		// so a present field is a prop, start included.
		if align := reverseAlign(appspec.AstSelName(cfg["Align"])); align != "" {
			b.Props["align"] = align
		}
		if justify := reverseJustify(appspec.AstSelName(cfg["Justify"])); justify != "" {
			b.Props["justify"] = justify
		}
	}
	if kind == "cluster" && appspec.AstBool(cfg["NoWrap"]) {
		b.Props["no_wrap"] = true
	}
	for _, arg := range call.Args[1:] {
		if child, ok := reverseBlock(arg, helpers); ok {
			b.Children = append(b.Children, child)
		}
	}
	if len(b.Props) == 0 {
		b.Props = nil
	}
	return b
}

func reverseGap(name string) string {
	return map[string]string{
		"GapNone": "none", "GapXS": "xs", "GapSM": "sm", "GapMD": "md",
		"GapLG": "lg", "GapXL": "xl", "Gap2XL": "2xl",
	}[name]
}

func reverseAlign(name string) string {
	return map[string]string{
		"AlignStart": "start", "AlignCenter": "center", "AlignEnd": "end",
		"AlignBaseline": "baseline", "AlignStretch": "stretch",
	}[name]
}

func reverseJustify(name string) string {
	return map[string]string{
		"JustifyStart": "start", "JustifyCenter": "center", "JustifyEnd": "end",
		"JustifyBetween": "between", "JustifyAround": "around",
	}[name]
}

func reverseEntityResource(call *ast.CallExpr, helpers map[string]ast.Expr) (appspec.AppSpecBlock, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return appspec.AppSpecBlock{}, false
	}
	method := sel.Sel.Name
	if method != "List" && method != "Detail" && method != "Form" {
		return appspec.AppSpecBlock{}, false
	}
	b := appspec.AppSpecBlock{}
	node := sel.X
	hops := 0
	for {
		// Bound the helper-hop walk so a self-referential or mutually
		// recursive zero-arg helper (a() returns a()) can't loop
		// forever. 32 is far beyond any real helper chain depth;
		// returning not-reversible is the safe fallback (the screen
		// just isn't packed; it stays as authored Go).
		if hops > 32 {
			return appspec.AppSpecBlock{}, false
		}
		if idx, ok := node.(*ast.IndexExpr); ok {
			if appgen.IdentName(idx.X) == "appResources" {
				b.Entity = appspec.AstString(idx.Index)
				break
			}
			return appspec.AppSpecBlock{}, false
		}
		c, ok := node.(*ast.CallExpr)
		if !ok {
			return appspec.AppSpecBlock{}, false
		}
		s, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			// customersList().List(ctx): the chain was extracted into a
			// package-local zero-arg helper (shared by the screen and the
			// island endpoint). Follow the hop and keep walking.
			if id, isIdent := c.Fun.(*ast.Ident); isIdent && len(c.Args) == 0 {
				if body, found := helpers[id.Name]; found {
					hops++
					node = body
					continue
				}
			}
			return appspec.AppSpecBlock{}, false
		}
		switch s.Sel.Name {
		case "WithColumns":
			for _, a := range c.Args {
				b.Fields = append(b.Fields, appspec.AstString(a))
			}
		case "WithLimit":
			if len(c.Args) == 1 {
				if n, ok := appspec.AstInt(c.Args[0]); ok {
					b.Limit = n
				}
			}
		case "WithCreate":
			b.Create = true
		case "WithHeading":
			if len(c.Args) == 1 {
				b.Text = appspec.AstString(c.Args[0])
			}
		case "WithEmpty":
			if len(c.Args) == 1 {
				b.EmptyText = appspec.AstString(c.Args[0])
			}
		case "WithEdit":
			// detail-only affordance; not a list flag
		}
		node = s.X
	}
	switch method {
	case "List":
		b.Kind = "entity_list"
	case "Detail":
		b.Kind = "entity_detail"
	case "Form":
		// synthesized create/edit form screen: mark so it can be dropped.
		if len(call.Args) == 2 && appspec.AstString(call.Args[1]) == "" {
			b.Kind = "entity_create"
		} else {
			b.Kind = "entity_edit"
		}
	}
	return b, true
}

// reverseEntityUI turns one appUI.List/Record/Create builder chain back
// into its AppSpecBlock: Columns→fields, PageSize→limit, As→mode,
// Heading→text, Empty→empty_text, Bulk→bulk, and a list without NoCreate
// is create:true (the builder's default). Key, Base, Delete and Duplicate
// are structural and do not round-trip. Returns false for anything that is
// not an appUI chain.
func reverseEntityUI(e ast.Expr, helpers map[string]ast.Expr) (appspec.AppSpecBlock, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return appspec.AppSpecBlock{}, false
	}
	b := appspec.AppSpecBlock{}
	noCreate, deletes := false, false
	var omit []string
	node := ast.Expr(call)
	hops := 0
	for {
		// Same hop bound as reverseEntityResource: a self-referential or
		// mutually recursive helper must break the walk, not loop forever.
		if hops > 32 {
			return appspec.AppSpecBlock{}, false
		}
		c, ok := node.(*ast.CallExpr)
		if !ok {
			return appspec.AppSpecBlock{}, false
		}
		s, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			if id, isIdent := c.Fun.(*ast.Ident); isIdent && len(c.Args) == 0 {
				if body, found := helpers[id.Name]; found {
					hops++
					node = body
					continue
				}
			}
			return appspec.AppSpecBlock{}, false
		}
		if appgen.IdentName(s.X) == "appUI" {
			switch s.Sel.Name {
			case "List":
				b.Kind = "entity_list"
				b.Create = !noCreate
			case "Record":
				// A detail offers Delete; an edit form is the record page
				// without it, its fields allowlist spelled as Omit.
				b.Kind = "entity_detail"
				if !deletes {
					b.Kind, b.Mode = "entity_form", "edit"
					if len(omit) > 0 {
						b.Props = map[string]any{packOmitProp: omit}
					}
				}
			case "Create":
				// synthesized create screen: marked so pack can drop it.
				b.Kind = "entity_create"
			default:
				return appspec.AppSpecBlock{}, false
			}
			if len(c.Args) < 1 {
				return appspec.AppSpecBlock{}, false
			}
			b.Entity = appspec.AstString(c.Args[0])
			return b, true
		}
		switch s.Sel.Name {
		case "Columns":
			for _, a := range c.Args {
				b.Fields = append(b.Fields, appspec.AstString(a))
			}
		case "As":
			if len(c.Args) == 1 {
				b.Mode = appspec.AstString(c.Args[0])
			}
		case "PageSize":
			if len(c.Args) == 1 {
				if n, ok := appspec.AstInt(c.Args[0]); ok {
					b.Limit = n
				}
			}
		case "NoCreate":
			noCreate = true
		case "Bulk":
			b.Bulk = true
		case "Heading":
			if len(c.Args) >= 1 {
				b.Text = appspec.AstString(c.Args[0])
			}
		case "Empty":
			if len(c.Args) == 1 {
				b.EmptyText = appspec.AstString(c.Args[0])
			}
		case "Delete":
			deletes = true
		case "Omit":
			for _, a := range c.Args {
				omit = append(omit, appspec.AstString(a))
			}
		case "Key", "Base", "Duplicate", "RenderCtx", "Actions", "Related", "RelatedAt":
			// structural: no app spec key (RenderCtx turns the builder
			// into the HTML the screen's stack takes; Actions, Related and
			// RelatedAt are app-side extensions pack does not declare)
		default:
			return appspec.AppSpecBlock{}, false
		}
		node = s.X
		hops++
	}
}

func reverseHero(call *ast.CallExpr) appspec.AppSpecBlock {
	c := cfgOf(call, 0)
	p := map[string]any{}
	putStr(p, "eyebrow", appspec.AstString(c["Eyebrow"]))
	putStr(p, "title", appspec.AstString(c["Title"]))
	putStr(p, "subtitle", appspec.AstString(c["Subtitle"]))
	// Actions: []render.HTML{LinkButton(cta), LinkButton(secondary)}.
	if actions, ok := c["Actions"].(*ast.CompositeLit); ok {
		for i, el := range actions.Elts {
			ac, ok := el.(*ast.CallExpr)
			if !ok || callSel(ac) != "ui.LinkButton" {
				continue
			}
			lc := cfgOf(ac, 0)
			label, href := appspec.AstString(lc["Label"]), appspec.AstString(lc["Href"])
			if i == 0 {
				putStr(p, "cta_text", label)
				putStr(p, "cta_href", href)
			} else {
				putStr(p, "secondary_text", label)
				putStr(p, "secondary_href", href)
			}
		}
	}
	return appspec.AppSpecBlock{Kind: "hero", Props: p}
}

func reverseSection(call *ast.CallExpr, helpers map[string]ast.Expr) appspec.AppSpecBlock {
	c := cfgOf(call, 0)
	b := appspec.AppSpecBlock{Kind: "section", Props: props2("heading", appspec.AstString(c["Heading"]), "eyebrow", appspec.AstString(c["Eyebrow"]), "description", appspec.AstString(c["Description"]))}
	// The section's children are wrapped in ui.Grid/ui.Stack(cfg, children…).
	for _, arg := range call.Args[1:] {
		wrap, ok := arg.(*ast.CallExpr)
		if !ok {
			continue
		}
		if cs := callSel(wrap); cs == "ui.Grid" || cs == "ui.Stack" {
			for _, child := range wrap.Args[1:] {
				if cb, ok := reverseBlock(child, helpers); ok {
					b.Children = append(b.Children, cb)
				}
			}
		}
	}
	return b
}

func reverseStatCard(call *ast.CallExpr) appspec.AppSpecBlock {
	c := cfgOf(call, 0)
	p := map[string]any{}
	putStr(p, "label", appspec.AstString(c["Label"]))
	// Value: statValue(ctx, entity, agg, field, filter, format) — the
	// app-local helper a hand-maintained app kept — or the generator's
	// appUI.StatValue spelling; both take the same six args.
	if vc, ok := c["Value"].(*ast.CallExpr); ok && len(vc.Args) == 6 {
		bare, _ := vc.Fun.(*ast.Ident)
		if (bare != nil && bare.Name == "statValue" && callSel(vc) == "") || callSel(vc) == "appUI.StatValue" {
			src := map[string]any{}
			putStr(src, "entity", appspec.AstString(vc.Args[1]))
			putStr(src, "agg", appspec.AstString(vc.Args[2]))
			putStr(src, "field", appspec.AstString(vc.Args[3]))
			putStr(src, "filter", appspec.AstString(vc.Args[4]))
			p["source"] = src
			putStr(p, "format", appspec.AstString(vc.Args[5]))
		}
	}
	return appspec.AppSpecBlock{Kind: "stat_card", Props: p}
}

func reversePricing(call *ast.CallExpr) appspec.AppSpecBlock {
	plans := []any{}
	for _, arg := range call.Args[1:] {
		pc, ok := arg.(*ast.CallExpr)
		if !ok || callSel(pc) != "ui.PricingCard" {
			continue
		}
		c := cfgOf(pc, 0)
		plan := map[string]any{}
		putStr(plan, "name", appspec.AstString(c["Name"]))
		putStr(plan, "price", appspec.AstString(c["Price"]))
		putStr(plan, "period", appspec.AstString(c["Period"]))
		putStr(plan, "description", appspec.AstString(c["Description"]))
		if feats := appspec.AstStringSlice(c["Features"]); len(feats) > 0 {
			fa := make([]any, len(feats))
			for i, f := range feats {
				fa[i] = f
			}
			plan["features"] = fa
		}
		putStr(plan, "cta_text", appspec.AstString(c["CTALabel"]))
		putStr(plan, "cta_href", appspec.AstString(c["CTAHref"]))
		if appspec.AstBool(c["Featured"]) {
			plan["featured"] = true
		}
		plans = append(plans, plan)
	}
	return appspec.AppSpecBlock{Kind: "pricing", Props: map[string]any{"plans": plans}}
}

func reverseAuthCard(call *ast.CallExpr) appspec.AppSpecBlock {
	c := cfgOf(call, 0)
	// Body: ui.Form(ui.FormConfig{Action: …}, hidden-next, fields…).
	action, next := "", ""
	if form, ok := c["Body"].(*ast.CallExpr); ok && callSel(form) == "ui.Form" {
		action = appspec.AstString(cfgOf(form, 0)["Action"])
		for _, arg := range form.Args[1:] {
			// The hidden next input: the generator emits it as
			// html.Input(html.InputConfig{Type: "hidden", Name: "next", …}).
			if in, ok := arg.(*ast.CallExpr); ok && callSel(in) == "html.Input" {
				ic := cfgOf(in, 0)
				if appspec.AstString(ic["Name"]) == "next" {
					next = appspec.AstString(ic["Value"])
				}
			}
		}
	}
	footerHref := htmlAttr(rawString(c["Footer"]), "href")
	if footerHref == "" {
		// Since v0.61 the emitter renders the auth footer as the URL-checked
		// ui.Link component instead of a raw anchor; read Href from its config.
		if link, ok := c["Footer"].(*ast.CallExpr); ok && callSel(link) == "ui.Link" {
			footerHref = appspec.AstString(cfgOf(link, 0)["Href"])
		}
	}
	kind, hrefKey := "login_form", "register_href"
	if strings.Contains(action, "register") {
		kind, hrefKey = "signup_form", "login_href"
	}
	p := map[string]any{}
	putStr(p, "action", action)
	putStr(p, "next", next)
	putStr(p, hrefKey, footerHref)
	return appspec.AppSpecBlock{Kind: kind, Text: appspec.AstString(c["Title"]), Props: p}
}

// rawString unwraps render.Raw("…") → its string content.
func rawString(e ast.Expr) string {
	if call, ok := e.(*ast.CallExpr); ok && callSel(call) == "render.Raw" && len(call.Args) == 1 {
		return appspec.AstString(call.Args[0])
	}
	return ""
}

// htmlAttr extracts the value of attr from an HTML snippet (attr="value").
func htmlAttr(html, attr string) string {
	needle := attr + `="`
	i := strings.Index(html, needle)
	if i < 0 {
		return ""
	}
	rest := html[i+len(needle):]
	if j := strings.IndexByte(rest, '"'); j >= 0 {
		return rest[:j]
	}
	return ""
}

func reverseRenderTag(call *ast.CallExpr) (appspec.AppSpecBlock, bool) {
	tag := appspec.AstString(call.Args[0])
	switch tag {
	case "p":
		return appspec.AppSpecBlock{Type: "paragraph", Text: renderTextArg(call.Args[len(call.Args)-1])}, true
	case "div":
		// chart wrapper: render.Tag("div", {class:"mrd-chart"}, Heading(title), ui.BarChart/PieChart(...)).
		for _, arg := range call.Args[2:] {
			ac, ok := arg.(*ast.CallExpr)
			if !ok {
				continue
			}
			switch callSel(ac) {
			case "ui.BarChart", "ui.PieChart", "ui.LineChart":
				kind := map[string]string{"ui.BarChart": "bar_chart", "ui.PieChart": "pie_chart", "ui.LineChart": "line_chart"}[callSel(ac)]
				p := map[string]any{}
				// title from the sibling heading.
				for _, h := range call.Args[2:] {
					if hc, ok := h.(*ast.CallExpr); ok && callSel(hc) == "html.Heading" {
						putStr(p, "title", renderTextArg(hc.Args[len(hc.Args)-1]))
					}
				}
				// source from groupBars/groupSlices(ctx, entity, group_by).
				cc := cfgOf(ac, 0)
				if dataCall, ok := cc["Bars"].(*ast.CallExpr); ok {
					p["source"] = chartSource(dataCall)
				} else if dataCall, ok := cc["Slices"].(*ast.CallExpr); ok {
					p["source"] = chartSource(dataCall)
				}
				return appspec.AppSpecBlock{Kind: kind, Props: p}, true
			}
		}
	}
	return appspec.AppSpecBlock{}, false
}

// reverseChartCall recognizes the three chart emission shapes and returns
// the corresponding app spec block (without a title; the caller sets it
// from a wrapping ui.Card heading when present):
//
//	ui.BarChart(ui.BarChartConfig{Ctx: ctx, Bars: groupBars(ctx, e, g), …})
//	ui.PieChart(ui.PieChartConfig{Ctx: ctx, Slices: groupSlices(ctx, e, g)})
//	lineChart(ctx, e, g)
func reverseChartCall(call *ast.CallExpr) (appspec.AppSpecBlock, bool) {
	switch callSel(call) {
	case "ui.BarChart", "ui.PieChart":
		kind := map[string]string{"ui.BarChart": "bar_chart", "ui.PieChart": "pie_chart"}[callSel(call)]
		p := map[string]any{}
		cc := cfgOf(call, 0)
		if dataCall, ok := cc["Bars"].(*ast.CallExpr); ok {
			p["source"] = chartSource(dataCall)
		} else if dataCall, ok := cc["Slices"].(*ast.CallExpr); ok {
			p["source"] = chartSource(dataCall)
		}
		return appspec.AppSpecBlock{Kind: kind, Props: p}, true
	case "lineChart":
		// lineChart(ctx, entity, group_by)
		return appspec.AppSpecBlock{Kind: "line_chart", Props: map[string]any{"source": chartSource(call)}}, true
	}
	return appspec.AppSpecBlock{}, false
}

func chartSource(dataCall *ast.CallExpr) map[string]any {
	src := map[string]any{}
	if len(dataCall.Args) == 3 {
		putStr(src, "entity", appspec.AstString(dataCall.Args[1]))
		putStr(src, "group_by", appspec.AstString(dataCall.Args[2]))
	}
	return src
}

// ----- small reverse helpers -------------------------------------------------

func cfgOf(call *ast.CallExpr, i int) map[string]ast.Expr {
	if i < len(call.Args) {
		return appspec.FieldVals(call.Args[i])
	}
	return map[string]ast.Expr{}
}

func block(kind string, props map[string]any) appspec.AppSpecBlock {
	return appspec.AppSpecBlock{Kind: kind, Props: props}
}

func props2(kv ...string) map[string]any {
	m := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			m[kv[i]] = kv[i+1]
		}
	}
	return m
}

func renderTextArg(e ast.Expr) string {
	if call, ok := e.(*ast.CallExpr); ok && callSel(call) == "render.Text" && len(call.Args) == 1 {
		return appspec.AstString(call.Args[0])
	}
	return ""
}

func buttonVariant(e ast.Expr) string {
	switch appspec.AstSelName(e) {
	case "ButtonPrimary":
		return "primary"
	case "ButtonSecondary":
		return "secondary"
	case "ButtonDanger":
		return "danger"
	case "ButtonGhost":
		return "ghost"
	}
	return ""
}

// packAppSpec reconstructs a full App spec from a generated app directory.
// secretsInAppSpec reports whether the packed app spec carries any of
// the three values packReadDotEnv recovers from .env.
// secretsInAppSpec decides whether pack prints its do-NOT-commit
// warning. It defers to dsnredact.HasSecret for the DSN rather than
// testing for "@": the generator already uses the same predicate to
// decide what to redact, and the two disagreeing means pack stays silent
// about a secret the generator considered worth hiding. A keyword/value
// DSN ("host=db user=app password=hunter2") is the case that fell
// through -- it carries a password and no "@" at all.
func secretsInAppSpec(bp appspec.AppSpec) bool {
	return bp.App.Auth.JWTSecret != "" ||
		bp.App.Admin.SeedPassword != "" ||
		dsnredact.HasSecret(bp.App.DBURL)
}

func packAppSpec(dir string) (appspec.AppSpec, error) {
	var bp appspec.AppSpec
	app, err := packReadApp(dir)
	if err != nil {
		return bp, err
	}
	bp.App = app
	if bp.Entities, err = appgen.PackReadEntities(dir); err != nil {
		return bp, err
	}
	if bp.Screens, err = packReadScreens(dir); err != nil {
		return bp, err
	}
	packResolveOmits(bp)
	packFoldRecordKeys(bp)
	if bp.Nav, err = packReadNav(dir); err != nil {
		return bp, err
	}
	if bp.Seed, err = packReadSeed(dir); err != nil {
		return bp, err
	}
	if err = packReadStubs(dir, &bp); err != nil {
		return bp, err
	}
	// Endpoints live in the settings file generate app wrote them to.
	if bp.Endpoints, err = appgen.ExistingEndpoints(dir); err != nil {
		return bp, err
	}
	return bp, nil
}

// packOmitProp carries an edit form's omitted fields from the screen
// reader to packResolveOmits, which knows the entity's fields.
const packOmitProp = "\x00omit"

// packResolveOmits turns each edit form's omitted fields back into the
// fields allowlist the app spec wrote: the entity's fields it did not omit.
func packResolveOmits(bp appspec.AppSpec) {
	fieldsOf := map[string][]framework.FieldDeclaration{}
	for _, d := range bp.Entities {
		fieldsOf[d.Name] = d.Fields
	}
	var walk func([]appspec.AppSpecBlock)
	walk = func(blocks []appspec.AppSpecBlock) {
		for i := range blocks {
			b := &blocks[i]
			if omit, ok := b.Props[packOmitProp].([]string); ok {
				delete(b.Props, packOmitProp)
				if len(b.Props) == 0 {
					b.Props = nil
				}
				for _, f := range fieldsOf[b.Entity] {
					if !slices.Contains(omit, f.Name) {
						b.Fields = append(b.Fields, f.Name)
					}
				}
			}
			walk(b.Children)
		}
	}
	for i := range bp.Screens {
		walk(bp.Screens[i].Body)
	}
}

// packFoldRecordKeys turns a composite-key record screen's mounted route
// back into the route the app spec wrote: the run of one segment per key
// column, as appSpecScreenPattern expanded it, is the record's {id}.
func packFoldRecordKeys(bp appspec.AppSpec) {
	for i := range bp.Screens {
		s := &bp.Screens[i]
		keys := appspec.ScreenRecordKeys(bp, *s)
		if keys == nil {
			continue
		}
		run := "/{" + strings.Join(keys, "}/{") + "}"
		if j := strings.Index(s.Route+"/", run+"/"); j >= 0 {
			s.Route = s.Route[:j] + "/{id}" + s.Route[j+len(run):]
		}
	}
}

// RunPack implements `gofastr pack [app-dir] [-o out.yml]`: a lossy
// best-effort snapshot, not a round-trip inverse of generate. It reconstructs
// a gofastr.yml from a generated app's Go source.
func RunPack(args []string) {
	dir := "."
	out := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o", "--out":
			// A missing value or an unknown flag would print the YAML to
			// stdout and exit 0 while the caller expected a file.
			if i+1 >= len(args) {
				packFail(true, "pack: %s needs a file name", args[i])
				return
			}
			out = args[i+1]
			i++
		case "-h", "--help":
			cli.Info("Usage: gofastr pack [app-dir] [-o out.yml]")
			cli.Info("Reconstructs a best-effort gofastr.yml from a generated app's Go source")
			cli.Info("(entities, app config, theme, screens, nav, seed). Lossy: not a round-trip")
			cli.Info("inverse of `gofastr generate`; hand-written handlers/hooks/business logic")
			cli.Info("are not recovered. See framework/ARCHITECTURE.md (\"pack is one-way\").")
			cli.Info("Pack then regenerates from the packed spec and checks the Go files at the")
			cli.Info("app root and directly under entities/ (tests aside): it names each one it")
			cli.Info("does not reproduce, could not read, or left out as hand-written.")
			return
		default:
			if strings.HasPrefix(args[i], "-") {
				packFail(true, "pack: unknown flag %s (usage: gofastr pack [app-dir] [-o out.yml])", args[i])
				return
			}
			dir = args[i]
		}
	}
	// With no -o the YAML is stdout, so everything else goes to stderr.
	toStderr := out == ""
	bp, err := packAppSpec(dir)
	if err != nil {
		packFail(toStderr, "pack: %v", err)
		return
	}
	yml, err := encodeAppSpecYAML(bp)
	if err != nil {
		packFail(toStderr, "pack: %v", err)
		return
	}
	// The seed password lives only in .env. Without it the packed spec
	// seeds no admin, and the check below then names app.go and main.go;
	// say why first.
	if bp.App.Admin.SeedEmail != "" && bp.App.Admin.SeedPassword == "" {
		packNote(toStderr, "pack: the app seeds admin %s, but %s holds no ADMIN_SEED_PASSWORD, so the packed spec seeds no admin; add seed_password under app.admin before generating from it", bp.App.Admin.SeedEmail, filepath.Join(dir, ".env"))
	}
	// The check regenerates from the packed spec and names every file it
	// does not reproduce, so the loss is reported instead of silent.
	if c, err := checkPack(dir, bp); err != nil {
		packNote(toStderr, "pack: could not check the packed spec against %s: %v", dir, err)
	} else {
		reportPackCheck(c, toStderr)
	}
	// packReadDotEnv rehydrates JWT_SECRET / ADMIN_SEED_PASSWORD /
	// DATABASE_URL from the app's .env so the packed app spec round-trips
	// (see the comment on packReadDotEnv). That reverses the generator's
	// own "secrets never land in committed source" rule, which
	// TestAppSpecNeverInlinesSecrets pins, and unlike `.env`, an
	// arbitrary `-o` path is not covered by the generated .gitignore. Warn
	// loudly and write 0600 rather than 0644.
	if secretsInAppSpec(bp) {
		packNote(toStderr, "pack: output contains secrets recovered from .env (jwt_secret, seed_password, db.url): do NOT commit it")
	}
	if out == "" {
		fmt.Print(yml)
		return
	}
	// The 0600 argument to os.WriteFile only applies when the file is
	// CREATED. Overwriting an existing 0644 file keeps 0644, and this
	// output carries the jwt_secret, seed password, and credentialed DSN
	// recovered from .env -- so a second `pack -o` over an earlier run's
	// file published all three. The mode is set on a temporary file
	// before the write, and the file is renamed over the old one, the
	// same way the generated .env is written.
	if err := writeSecretFile(out, yml); err != nil {
		cli.Fail("pack: write %s: %v", out, err)
		cli.OsExit(1)
		return
	}
	cli.Success("Packed %s → %s (%d entities, %d screens)", dir, out, len(bp.Entities), len(bp.Screens))
}

// writeSecretFile replaces path with content at owner-only permissions
// (fileperm.ReplaceOwnerOnly): the mode is set before any content lands,
// and a crash mid-write leaves the previous file, never a truncated one.
func writeSecretFile(path, content string) error {
	return fileperm.ReplaceOwnerOnly(path, []byte(content))
}

// invalidUTF8At returns the path of the first string under v (map keys
// and values, slices, struct fields) that is not valid UTF-8, or "".
func invalidUTF8At(v reflect.Value, path string) string {
	switch v.Kind() {
	case reflect.String:
		if !utf8.ValidString(v.String()) {
			return path
		}
	case reflect.Interface, reflect.Pointer:
		if !v.IsNil() {
			return invalidUTF8At(v.Elem(), path)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if at := invalidUTF8At(v.Index(i), fmt.Sprintf("%s[%d]", path, i)); at != "" {
				return at
			}
		}
	case reflect.Map:
		keys := v.MapKeys()
		slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) })
		for _, k := range keys {
			kp := fmt.Sprintf("%s.%v", path, k)
			if at := invalidUTF8At(k, kp); at != "" {
				return at
			}
			if at := invalidUTF8At(v.MapIndex(k), kp); at != "" {
				return at
			}
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if at := invalidUTF8At(v.Field(i), path+"."+v.Type().Field(i).Name); at != "" {
				return at
			}
		}
	}
	return ""
}
