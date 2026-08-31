package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/dotenv"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
)

// decodeBlueprintString parses a gofastr.yml from a string into a Blueprint.
func decodeBlueprintString(yml string) (Blueprint, error) {
	node, err := coreyaml.Parse(yml)
	if err != nil {
		return Blueprint{}, err
	}
	return decodeBlueprint(node)
}

// =============================================================================
// pack.go: a lossy app→blueprint snapshot. encodeBlueprintYAML serializes a Blueprint
// back to gofastr.yml; the AST readers (further down) reconstruct a Blueprint
// from a generated app's Go source so `gofastr pack <dir>` recovers the
// authoring YAML. The invariant the round-trip test gates:
//
//	parse(meridian.yml)  deep-equals  parse(pack(generate(meridian.yml)))
//
// i.e. encodeBlueprintYAML is the exact inverse of decodeBlueprint (modulo
// comments + formatting). When a new blueprint construct is added, BOTH the
// decoder and this serializer must learn it or the round-trip test fails.
// =============================================================================

// encodeBlueprintYAML serializes a Blueprint to gofastr.yml text. It returns
// an error — naming the key and the map it came from — for any map key that
// cannot survive the decodeBlueprint round trip: core/yaml never unquotes
// keys, so quoting would mangle them just as surely as emitting them raw
// would forge structure. Refusing is the honest inverse.
func encodeBlueprintYAML(bp Blueprint) (string, error) {
	root := blueprintToMap(bp)
	var sb strings.Builder
	if err := writeYAMLMap(&sb, root, 0, topLevelOrder, ""); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// ----- Blueprint -> nested map[string]any (mirrors the decoders) -------------

func blueprintToMap(bp Blueprint) map[string]any {
	m := map[string]any{}
	if app := appToMap(bp.App); len(app) > 0 {
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
			em := map[string]any{}
			putStr(em, "name", e.Name)
			putStr(em, "method", e.Method)
			putStr(em, "path", e.Path)
			putStr(em, "entity", e.Entity)
			putStr(em, "handler", e.Handler)
			putStr(em, "description", e.Description)
			putBool(em, "mcp", e.MCP)
			eps[i] = em
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
// scalar (the form hand-authored blueprints use), one with a description is
// a name/description map. Both decode back to the same BlueprintNamedStub.
func stubsToAny(stubs []BlueprintNamedStub) []any {
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

func appToMap(a BlueprintApp) map[string]any {
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
		theme := map[string]any{}
		for k, v := range a.Theme {
			theme[k] = v
		}
		if len(a.ThemeDark) > 0 {
			dark := map[string]any{}
			for k, v := range a.ThemeDark {
				dark[k] = v
			}
			theme["dark"] = dark
		}
		m["theme"] = theme
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
	return m
}

func entityToMap(e framework.EntityDeclaration) map[string]any {
	m := map[string]any{}
	putStr(m, "name", e.Name)
	putStr(m, "table", e.Table)
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
			// would diverge from the source blueprint.
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
			putStr(rm, "foreign_key", r.ForeignKey)
			putStr(rm, "through", r.Through)
			putStr(rm, "local_key", r.LocalKey)
			putStr(rm, "foreign_key_target", r.ForeignKeyTarget)
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

func screenToMap(s BlueprintScreen) map[string]any {
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

func blocksToAny(blocks []BlueprintBlock) []any {
	out := make([]any, len(blocks))
	for i, b := range blocks {
		out[i] = blockToMap(b)
	}
	return out
}

func blockToMap(b BlueprintBlock) map[string]any {
	m := map[string]any{}
	putStr(m, "kind", b.Kind)
	putStr(m, "type", b.Type)
	putStr(m, "text", b.Text)
	putInt(m, "level", b.Level)
	putStr(m, "entity", b.Entity)
	putStrs(m, "fields", b.Fields)
	putStr(m, "search", b.Search)
	putStrs(m, "filters", b.Filters)
	putInt(m, "limit", b.Limit)
	putBool(m, "create", b.Create)
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
	if len(b.Transitions) > 0 {
		ts := make([]any, len(b.Transitions))
		for i, t := range b.Transitions {
			tm := map[string]any{}
			putStr(tm, "label", t.Label)
			putStr(tm, "status", t.Status)
			putStr(tm, "variant", t.Variant)
			putStr(tm, "stamp", t.Stamp)
			ts[i] = tm
		}
		m["transitions"] = ts
	}
	return m
}

func navItemsToAny(items []BlueprintNavItem) []any {
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

func actionToMap(a BlueprintAction) map[string]any {
	m := map[string]any{}
	putStr(m, "name", a.Name)
	putStr(m, "event", a.Event)
	putStr(m, "client_js", a.ClientJS)
	return m
}

func relationTypeToString(t framework.RelationType) string {
	switch t {
	case framework.RelHasOne:
		return "has_one"
	case framework.RelHasMany:
		return "has_many"
	case framework.RelManyToOne:
		return "belongs_to"
	case framework.RelManyToMany:
		return "many_to_many"
	default:
		return "belongs_to"
	}
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
	for _, k := range orderedKeys(m, order) {
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
			sb.WriteString(pad + key + ": {}\n")
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
	if m, ok := item.(map[string]any); ok && len(m) > 0 {
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
	writeScalarInline(sb, item)
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
	// encodeBlueprintYAML/decodeBlueprint and lets a `values:` enum set gain a
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

func orderedKeys(m map[string]any, order []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(m))
	for _, k := range order {
		if _, ok := m[k]; ok {
			out = append(out, k)
			seen[k] = true
		}
	}
	rest := make([]string, 0, len(m))
	for k := range m {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// ----- key orders (readability; semantics are order-independent) -------------
var (
	topLevelOrder   = []string{"app", "entities", "screens", "nav", "seed", "endpoints", "middleware", "plugins", "helpers"}
	appOrder        = []string{"name", "description", "base_url", "module", "db", "static_dir", "output_dir", "api_prefix", "public_openapi", "theme", "auth", "admin", "pwa", "llm_md"}
	entityOrder     = []string{"name", "table", "scope", "pagination", "exposure", "search_fields", "timestamps", "properties", "renames", "indices", "fields", "relations"}
	fieldOrder      = []string{"name", "type", "required", "unique", "default", "max", "min", "pattern", "values", "to", "many", "auto_generate", "read_only", "hidden", "no_query"}
	screenOrder     = []string{"name", "route", "title", "description", "type", "layout", "access", "body"}
	blockOrder      = []string{"kind", "type", "text", "level", "entity", "fields", "search", "filters", "limit", "create", "empty_text", "class", "href", "mode", "island", "widget", "props", "children", "actions", "transitions"}
	relationOrder   = []string{"type", "name", "entity", "foreign_key", "through", "local_key", "foreign_key_target"}
	indexOrder      = []string{"name", "columns", "unique"}
	navOrder        = []string{"label", "href", "icon", "role", "items"}
	accessOrder     = []string{"auth", "role", "read", "create", "update", "delete"}
	readScopeOrder  = []string{"unrestricted", "filter"}
	predicateOrder  = []string{"field", "op", "value", "values"}
	dbOrder         = []string{"driver", "url"}
	authOrder       = []string{"enabled", "dev_mode", "base_path", "jwt_secret"}
	adminOrder      = []string{"path", "role", "enabled", "login_path", "seed_email", "seed_password"}
	endpointOrder   = []string{"name", "method", "path", "entity", "handler", "description", "mcp"}
	actionOrder     = []string{"name", "event", "client_js"}
	transitionOrder = []string{"label", "status", "variant", "stamp"}
	stubOrder       = []string{"name", "description"}
	seedOrder       = []string{"entity", "count", "weights", "rows"}
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
	case "transitions":
		return transitionOrder
	case "seed":
		return seedOrder
	case "middleware", "plugins", "helpers":
		return stubOrder
	default:
		return nil
	}
}

// =============================================================================
// AST readers: reconstruct a Blueprint from a generated app's Go source.
// The generator emits a known, finite grammar; these readers reverse it.
// =============================================================================

func packParseFile(path string) (*ast.File, error) {
	fset := token.NewFileSet()
	return parser.ParseFile(fset, path, nil, 0)
}

// funcBody returns the statements of the named top-level func, or nil.
func funcBody(file *ast.File, name string) []ast.Stmt {
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == name && fn.Body != nil {
			return fn.Body.List
		}
	}
	return nil
}

// ----- AST scalar helpers ----------------------------------------------------

func astString(e ast.Expr) string {
	if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		if s, err := strconv.Unquote(lit.Value); err == nil {
			return s
		}
	}
	return ""
}

func astBool(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "true"
}

// astSelName returns the trailing identifier of `pkg.Name` (e.g. schema.String
// → "String", framework.RelManyToOne → "RelManyToOne"), or a bare ident's name.
func astSelName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// astPtrCallFloat unwraps floatPtr(123) → 123.
func astPtrCallFloat(e ast.Expr) (float64, bool) {
	if call, ok := e.(*ast.CallExpr); ok && len(call.Args) == 1 {
		return astFloat(call.Args[0])
	}
	return 0, false
}

// astPtrCallBool unwraps boolPtr(true) → true.
func astPtrCallBool(e ast.Expr) (bool, bool) {
	if call, ok := e.(*ast.CallExpr); ok && len(call.Args) == 1 {
		return astBool(call.Args[0]), true
	}
	return false, false
}

func astFloat(e ast.Expr) (float64, bool) {
	if lit, ok := e.(*ast.BasicLit); ok && (lit.Kind == token.FLOAT || lit.Kind == token.INT) {
		f, err := strconv.ParseFloat(lit.Value, 64)
		return f, err == nil
	}
	return 0, false
}

func astStringSlice(e ast.Expr) []string {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(cl.Elts))
	for _, el := range cl.Elts {
		out = append(out, astString(el))
	}
	return out
}

// astAny mirrors the YAML parser's native types: string, int64, float64, bool,
// nil, map[string]any, []any. Used for field Default, Properties, and seed rows.
func astAny(e ast.Expr) any {
	switch t := e.(type) {
	case *ast.BasicLit:
		switch t.Kind {
		case token.STRING:
			if s, err := strconv.Unquote(t.Value); err == nil {
				return s
			}
		case token.INT:
			if i, err := strconv.ParseInt(t.Value, 10, 64); err == nil {
				return i
			}
		case token.FLOAT:
			if f, err := strconv.ParseFloat(t.Value, 64); err == nil {
				return f
			}
		}
		return t.Value
	case *ast.Ident:
		switch t.Name {
		case "true":
			return true
		case "false":
			return false
		case "nil":
			return nil
		}
		return t.Name
	case *ast.CompositeLit:
		// Detect a map by element shape: nested literals inside
		// []map[string]any{...} have an elided (nil) Type, so KeyValueExpr
		// elements are the reliable signal.
		isMap := false
		if _, ok := t.Type.(*ast.MapType); ok {
			isMap = true
		} else if len(t.Elts) > 0 {
			_, isMap = t.Elts[0].(*ast.KeyValueExpr)
		}
		if isMap {
			m := map[string]any{}
			for _, el := range t.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					m[astString(kv.Key)] = astAny(kv.Value)
				}
			}
			return m
		}
		// array/slice literal
		out := make([]any, 0, len(t.Elts))
		for _, el := range t.Elts {
			out = append(out, astAny(el))
		}
		return out
	}
	return nil
}

// unwrapBuilderCalls peels trailing fluent builder method calls off a config
// expression such as EntityConfig{...}.WithTimestamps(true), returning the
// underlying value and a map of methodName→args for each builder peeled. The
// generator emits some EntityConfig settings (Timestamps) as method calls
// rather than struct fields, so pack recovers them from here rather than from
// the composite literal's fields.
func unwrapBuilderCalls(e ast.Expr) (ast.Expr, map[string][]ast.Expr) {
	builders := map[string][]ast.Expr{}
	for {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return e, builders
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return e, builders
		}
		builders[sel.Sel.Name] = call.Args
		e = sel.X
	}
}

// fieldVals returns the key→value expressions of a struct composite literal.
// A value wrapped in fluent builder calls is unwrapped to the literal first.
func fieldVals(e ast.Expr) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	e, _ = unwrapBuilderCalls(e)
	if ptr, ok := e.(*ast.UnaryExpr); ok && ptr.Op == token.AND {
		e = ptr.X
	}
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return out
	}
	for _, el := range cl.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok {
				out[id.Name] = kv.Value
			}
		}
	}
	return out
}

var schemaTypeToYAML = map[string]string{
	"String": "string", "Text": "text", "Int": "int", "Float": "float",
	"Decimal": "decimal", "Bool": "bool", "Enum": "enum", "UUID": "uuid",
	"Timestamp": "timestamp", "Date": "date", "JSON": "json",
	"Relation": "relation", "Image": "image", "File": "file",
}

func relationTypeFromConstName(name string) framework.RelationType {
	switch name {
	case "RelHasOne":
		return framework.RelHasOne
	case "RelHasMany":
		return framework.RelHasMany
	case "RelManyToMany":
		return framework.RelManyToMany
	default:
		return framework.RelManyToOne
	}
}

// packReadEntities reconstructs the entity declarations from the generated
// entities package. It supports two layouts:
//
//   - Per-entity (current generator): each entities/<name>.go holds a
//     register<Camel> func whose body calls app.Entity(name, config), plus an
//     init() that appends registrar{order: N, fn: register<Camel>}.
//     Declaration order is recovered from the order field.
//   - Legacy aggregated (older generator): entities/register.go holds a
//     RegisterAll whose body is the app.Entity calls in declaration order.
//
// Returns nil (not an error) when there is no entities package.
func packReadEntities(dir string) ([]framework.EntityDeclaration, error) {
	entDir := filepath.Join(dir, "entities")
	if _, err := os.Stat(entDir); err != nil {
		return nil, nil // no entities package
	}
	decls, err := packReadPerEntityFiles(entDir)
	if err != nil {
		return nil, err
	}
	if decls != nil {
		return decls, nil
	}
	// Legacy aggregated layout: inline RegisterAll in register.go.
	return packReadLegacyRegister(filepath.Join(entDir, "register.go"))
}

// packReadPerEntityFiles reads the per-entity generated files and returns the
// declarations in declaration order. Returns (nil, nil) when the package uses
// the legacy aggregated layout (no per-entity register funcs found), so the
// caller falls back to register.go.
func packReadPerEntityFiles(entDir string) ([]framework.EntityDeclaration, error) {
	entries, err := os.ReadDir(entDir)
	if err != nil {
		return nil, err
	}
	// Fixed package files that never hold an entity registration.
	skip := map[string]bool{"register.go": true, "shared.go": true, "doc.go": true}
	type ordered struct {
		order int
		decl  framework.EntityDeclaration
	}
	var found []ordered
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || skip[entry.Name()] {
			continue
		}
		path := filepath.Join(entDir, entry.Name())
		file, err := packParseFile(path)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		call, ok := packFindEntityCall(file)
		if !ok {
			continue // not an entity file
		}
		found = append(found, ordered{order: packEntityOrder(file), decl: packEntityDeclFromCall(call)})
	}
	if len(found) == 0 {
		return nil, nil
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].order < found[j].order })
	out := make([]framework.EntityDeclaration, len(found))
	for i, f := range found {
		out[i] = f.decl
	}
	return out, nil
}

// packFindEntityCall locates the register<Camel> func's app.Entity(name,
// config) call in a parsed entity file. Returns ok=false if the file has no
// such registration (e.g. a shared helper or legacy aggregated file).
func packFindEntityCall(file *ast.File) (*ast.CallExpr, bool) {
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "register") || fn.Name.Name == "register" {
			continue
		}
		for _, stmt := range fn.Body.List {
			es, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			call, ok := es.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Entity" || len(call.Args) != 2 {
				continue
			}
			return call, true
		}
	}
	return nil, false
}

// packEntityOrder extracts the declaration order from an entity file's
// init() self-registration: registrar{order: N, ...}. Returns 0 when the
// marker is absent (treated as first-declared).
func packEntityOrder(file *ast.File) int {
	order := 0
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		id, ok := cl.Type.(*ast.Ident)
		if !ok || id.Name != "registrar" {
			return true
		}
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "order" {
				continue
			}
			if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.INT {
				if n, err := strconv.Atoi(lit.Value); err == nil {
					order = n
				}
			}
		}
		return false
	})
	return order
}

// packEntityDeclFromCall rebuilds an EntityDeclaration from an
// app.Entity(name, config) call expression.
func packEntityDeclFromCall(call *ast.CallExpr) framework.EntityDeclaration {
	decl := framework.EntityDeclaration{Name: astString(call.Args[0])}
	_, builders := unwrapBuilderCalls(call.Args[1])
	cfg := fieldVals(call.Args[1])
	if v, ok := cfg["Table"]; ok {
		decl.Table = astString(v)
	}
	// An all-zero group normalizes to nil: a hand-written (or legacy
	// generator's) empty `Scope: &framework.ScopeConfig{}` literal must
	// pack back to the same declaration as writing nothing at all.
	if v, ok := cfg["Scope"]; ok {
		s := fieldVals(v)
		scope := &framework.ScopeDeclaration{
			SoftDelete: astBool(s["SoftDelete"]), MultiTenant: astBool(s["MultiTenant"]),
			TenantField: astString(s["TenantField"]), OwnerField: astString(s["OwnerField"]),
			CrossOwnerRead: astString(s["CrossOwnerRead"]),
		}
		if *scope != (framework.ScopeDeclaration{}) {
			decl.Scope = scope
		}
	}
	if v, ok := cfg["Pagination"]; ok {
		p := fieldVals(v)
		pagination := &framework.PaginationDeclaration{
			CursorField: astString(p["CursorField"]), CursorFields: astStringSlice(p["CursorFields"]),
		}
		if limit, ok := astInt(p["MaxListLimit"]); ok {
			pagination.MaxListLimit = limit
		}
		if pagination.CursorField != "" || len(pagination.CursorFields) > 0 || pagination.MaxListLimit != 0 {
			decl.Pagination = pagination
		}
	}
	if v, ok := cfg["SearchFields"]; ok {
		decl.SearchFields = astStringSlice(v)
	}
	// Timestamps is emitted as a .WithTimestamps(bool) builder call, not a
	// struct field — recover it from the unwrapped builder args. (Fall back
	// to a struct field for forward-compat if a future generator inlines it.)
	if args, ok := builders["WithTimestamps"]; ok && len(args) == 1 {
		b := astBool(args[0])
		decl.Timestamps = &b
	} else if v, ok := cfg["Timestamps"]; ok {
		if b, ok := astPtrCallBool(v); ok {
			decl.Timestamps = &b
		}
	}
	if v, ok := cfg["Exposure"]; ok {
		x := fieldVals(v)
		exposure := &framework.ExposureDeclaration{MCP: astBool(x["MCP"]), Public: astBool(x["Public"])}
		if b, ok := astPtrCallBool(x["CRUD"]); ok {
			exposure.CRUD = &b
		}
		if accessExpr, ok := x["Access"]; ok {
			a := fieldVals(accessExpr)
			exposure.Access = &framework.AccessDeclaration{
				Read: astString(a["Read"]), Create: astString(a["Create"]),
				Update: astString(a["Update"]), Delete: astString(a["Delete"]),
			}
		}
		if rsExpr, ok := x["ReadScope"]; ok {
			r := fieldVals(rsExpr)
			rs := &framework.ReadScopeDeclaration{Unrestricted: astString(r["Unrestricted"])}
			if filterExpr, ok := r["Filter"]; ok {
				inner, _ := unwrapBuilderCalls(filterExpr)
				if cl, ok := inner.(*ast.CompositeLit); ok {
					for _, item := range cl.Elts {
						p := fieldVals(item)
						rs.Filter = append(rs.Filter, framework.RowPredicateDeclaration{
							Field: astString(p["Field"]), Op: astString(p["Op"]),
							Value: astString(p["Value"]), Values: astStringSlice(p["Values"]),
						})
					}
				}
			}
			if rs.Unrestricted != "" || len(rs.Filter) > 0 {
				exposure.ReadScope = rs
			}
		}
		if exposure.CRUD != nil || exposure.MCP || exposure.Public || exposure.Access != nil || exposure.ReadScope != nil {
			decl.Exposure = exposure
		}
	}
	if v, ok := cfg["Properties"]; ok {
		if m, ok := astAny(v).(map[string]any); ok && len(m) > 0 {
			decl.Properties = m
		}
	}
	if v, ok := cfg["Fields"]; ok {
		decl.Fields = packReadFields(v)
	}
	if v, ok := cfg["Indices"]; ok {
		decl.Indices = packReadIndices(v)
	}
	if v, ok := cfg["Relations"]; ok {
		decl.Relations = packReadRelations(v)
	}
	return decl
}

// packReadLegacyRegister reads the legacy aggregated register.go whose
// RegisterAll body is the app.Entity calls in declaration order. Returns
// (nil, nil) when register.go is absent.
func packReadLegacyRegister(path string) ([]framework.EntityDeclaration, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, nil // no entities package
	}
	file, err := packParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var out []framework.EntityDeclaration
	for _, stmt := range funcBody(file, "RegisterAll") {
		es, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := es.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Entity" || len(call.Args) != 2 {
			continue
		}
		out = append(out, packEntityDeclFromCall(call))
	}
	return out, nil
}

func packReadFields(e ast.Expr) []framework.FieldDeclaration {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := make([]framework.FieldDeclaration, 0, len(cl.Elts))
	for _, el := range cl.Elts {
		fv := fieldVals(el)
		f := framework.FieldDeclaration{
			Name:     astString(fv["Name"]),
			Type:     schemaTypeToYAML[astSelName(fv["Type"])],
			Required: astBool(fv["Required"]),
			Unique:   astBool(fv["Unique"]),
			ReadOnly: astBool(fv["ReadOnly"]),
			Hidden:   astBool(fv["Hidden"]),
			NoQuery:  astBool(fv["NoQuery"]),
			Pattern:  astString(fv["Pattern"]),
			To:       astString(fv["To"]),
			Many:     astBool(fv["Many"]),
		}
		if v, ok := fv["Default"]; ok {
			f.Default = astAny(v)
		}
		if v, ok := fv["Values"]; ok {
			f.Values = astStringSlice(v)
		}
		if v, ok := fv["Max"]; ok {
			if n, ok := astPtrCallFloat(v); ok {
				f.Max = &n
			}
		}
		if v, ok := fv["Min"]; ok {
			if n, ok := astPtrCallFloat(v); ok {
				f.Min = &n
			}
		}
		if v, ok := fv["AutoGenerate"]; ok {
			f.AutoGenerate = autoGenerateToYAML(astSelName(v))
		}
		out = append(out, f)
	}
	return out
}

func autoGenerateToYAML(constName string) string {
	switch constName {
	case "AutoUUID":
		return "uuid"
	case "AutoTimestamp":
		return "timestamp"
	case "AutoIncrement":
		return "increment"
	default:
		return ""
	}
}

func packReadIndices(e ast.Expr) []framework.Index {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := make([]framework.Index, 0, len(cl.Elts))
	for _, el := range cl.Elts {
		iv := fieldVals(el)
		out = append(out, framework.Index{
			Name:    astString(iv["Name"]),
			Columns: astStringSlice(iv["Columns"]),
			Unique:  astBool(iv["Unique"]),
			// Expression is the key for function/constant indices. Reading it
			// back matters because `gofastr generate` re-emits what this
			// returns: without it a declared unique expression index came out
			// the far side with no key at all, silently dropping the
			// constraint. See renderIndexLiteral.
			Expression: astString(iv["Expression"]),
		})
	}
	return out
}

func packReadRelations(e ast.Expr) []framework.Relation {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := make([]framework.Relation, 0, len(cl.Elts))
	for _, el := range cl.Elts {
		rv := fieldVals(el)
		out = append(out, framework.Relation{
			Type:             relationTypeFromConstName(astSelName(rv["Type"])),
			Name:             astString(rv["Name"]),
			Entity:           astString(rv["Entity"]),
			ForeignKey:       astString(rv["ForeignKey"]),
			Through:          astString(rv["Through"]),
			LocalKey:         astString(rv["LocalKey"]),
			ForeignKeyTarget: astString(rv["ForeignKeyTarget"]),
		})
	}
	return out
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
	body := funcBody(file, fn)
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

// packReadSeed reconstructs seed data from stubs.go seedData().
func packReadSeed(dir string) ([]BlueprintSeedEntity, error) {
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
	var out []BlueprintSeedEntity
	for _, el := range lit.Elts {
		sv := fieldVals(el)
		se := BlueprintSeedEntity{Entity: astString(sv["Entity"])}
		if rowsLit, ok := sv["Rows"].(*ast.CompositeLit); ok {
			for _, rowEl := range rowsLit.Elts {
				if m, ok := astAny(rowEl).(map[string]any); ok {
					se.Rows = append(se.Rows, m)
				}
			}
		}
		out = append(out, se)
	}
	return out, nil
}

// packReadNav reconstructs the sidebar nav from blueprint/app.go
// sidebarConfig() ui.SidebarConfig{Items: []ui.SidebarItem{...}}.
func packReadNav(dir string) ([]BlueprintNavItem, error) {
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
	itemsExpr := fieldVals(cfg)["Items"]
	itemsLit, ok := itemsExpr.(*ast.CompositeLit)
	if !ok {
		return nil, nil
	}
	var out []BlueprintNavItem
	for _, el := range itemsLit.Elts {
		iv := fieldVals(el)
		role := ""
		if rolesLit, ok := iv["Roles"].(*ast.CompositeLit); ok && len(rolesLit.Elts) > 0 {
			role = astString(rolesLit.Elts[0])
		}
		out = append(out, BlueprintNavItem{
			Label: astString(iv["Label"]),
			Href:  astString(iv["Href"]),
			Icon:  astString(iv["Icon"]),
			Role:  role,
		})
	}
	return out, nil
}

// ----- P3: app config + theme + auth + admin --------------------------------

// packReadApp reconstructs the app config (consts + theme + auth + admin) from
// blueprint/app.go and main.go.
func packReadApp(dir string) (BlueprintApp, error) {
	app := BlueprintApp{APIPrefix: "api"}
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
			consts[vs.Names[0].Name] = astString(vs.Values[0])
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
				av := fieldVals(t)
				app.Auth.DevMode = astBool(av["DevMode"])
				app.Auth.BasePath = astString(av["BasePath"])
				app.Auth.JWTSecret = astString(av["JWTSecret"])
			}
		case *ast.CallExpr:
			switch astSelLast(t.Fun) {
			case "HashPassword":
				if len(t.Args) == 1 {
					app.Admin.SeedPassword = astString(t.Args[0])
				}
			case "CreateUser":
				if len(t.Args) >= 2 {
					app.Admin.SeedEmail = astString(t.Args[1])
				}
			}
		}
		return true
	})

	// Admin, PWA, and LLM-markdown config live in main.go.
	if mainFile, err := packParseFile(filepath.Join(dir, "main.go")); err == nil {
		ast.Inspect(mainFile, func(n ast.Node) bool {
			switch t := n.(type) {
			case *ast.CompositeLit:
				switch astSelLast(t.Type) {
				case "Config":
					cv := fieldVals(t)
					if _, ok := cv["PathPrefix"]; !ok {
						return true // not an admin.Config
					}
					app.Admin.Enabled = true
					app.Admin.Path = astString(cv["PathPrefix"])
					app.Admin.Role = astString(cv["AdminRole"])
					app.Admin.LoginPath = astString(cv["LoginPath"])
				case "PWAConfig":
					cv := fieldVals(t)
					app.PWA.Enabled = true
					app.PWA.Name = astString(cv["Name"])
					app.PWA.ShortName = astString(cv["ShortName"])
					app.PWA.Description = astString(cv["Description"])
					app.PWA.StartURL = astString(cv["StartURL"])
					app.PWA.Scope = astString(cv["Scope"])
					app.PWA.Display = packPWADisplay(astSelLast(cv["Display"]))
					app.PWA.ThemeColor = astString(cv["ThemeColor"])
					app.PWA.BackgroundColor = astString(cv["BackgroundColor"])
				}
			case *ast.CallExpr:
				if astSelLast(t.Fun) == "WithPublicLLMMD" {
					app.LLMMD = true
				}
			}
			return true
		})
	}

	// Secrets moved out of committed source into the generated .env
	// (JWT_SECRET, DATABASE_URL, ADMIN_SEED_PASSWORD). Recover them from
	// there so pack round-trips the authored blueprint. Absent .env → the
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
// to the blueprint's display string, the reverse of
// blueprintPWADisplayConst. Unknown/absent → "" (the omitted default).
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
	for _, stmt := range funcBody(file, "appTheme") {
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
			light[toKebabCase(path[2])] = astString(asn.Rhs[0])
		case len(path) == 4 && path[1] == "Fonts" && path[3] == "Value":
			if light == nil {
				light = map[string]string{}
			}
			light["font_"+strings.ToLower(path[2])] = firstFontFamily(astString(asn.Rhs[0]))
		case len(path) == 2 && path[1] == "DarkColors":
			if m, ok := astAny(asn.Rhs[0]).(map[string]any); ok {
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
func packReadScreens(dir string) ([]BlueprintScreen, error) {
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
// and returns the authored screens in declaration order. Returns (nil, nil)
// when the project uses the legacy aggregated layout (no screen_*.go files),
// so the caller falls back to packReadLegacyScreens.
func packReadPerScreenFiles(dir string) ([]BlueprintScreen, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // no output dir yet
	}
	var screenPaths []string
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
		screenPaths = append(screenPaths, filepath.Join(dir, name))
	}
	if len(screenPaths) == 0 {
		return nil, nil
	}
	// Helpers (package-local zero-arg fns returning one expr) may live in
	// app.go or any screen file; index them all so reverseEntityResource can
	// follow one hop when a screen body calls a shared helper.
	helperFiles := []*ast.File{}
	if appFile, err := packParseFile(filepath.Join(dir, "app.go")); err == nil {
		helperFiles = append(helperFiles, appFile)
	}
	titles := map[string]string{}
	descs := map[string]string{}
	bodies := map[string][]BlueprintBlock{}
	var orderedRegs []screenOrderReg
	for _, path := range screenPaths {
		file, err := packParseFile(path)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
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
			recv := recvTypeName(fn.Recv.List[0].Type)
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
	for _, path := range screenPaths {
		file, _ := packParseFile(path)
		if file == nil {
			continue
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			if fn.Name.Name == "Render" || fn.Name.Name == "RenderCtx" {
				recv := recvTypeName(fn.Recv.List[0].Type)
				bodies[recv] = reverseRenderBody(fn, helpers)
			}
		}
	}
	sort.SliceStable(orderedRegs, func(i, j int) bool { return orderedRegs[i].order < orderedRegs[j].order })
	var out []BlueprintScreen
	for _, r := range orderedRegs {
		body := bodies[r.reg.typeName]
		if isSynthesizedBody(body) {
			continue
		}
		s := BlueprintScreen{
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
			s.Access = BlueprintAccess{Auth: true, Role: r.reg.role}
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
			switch identName(kv.Key) {
			case "order":
				if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.INT {
					if n, err := strconv.Atoi(lit.Value); err == nil {
						order = n
					}
				}
			case "fn":
				fnName = identName(kv.Value)
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
					r.route = astString(call.Args[0])
					r.typeName = compositeTypeName(call.Args[1])
					r.layout = layoutVarToName(call.Args[2])
				}
			case "RegisterScreen":
				if len(call.Args) == 2 {
					r.layout = layoutVarToName(call.Args[1])
					packWalkScreenChain(call.Args[0], &r)
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

// packReadLegacyScreens reads the pre-per-screen aggregated layout: screen
// types in screens.go, registrations in app.go's RegisterGenerated.
func packReadLegacyScreens(dir string) ([]BlueprintScreen, error) {
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
	bodies := map[string][]BlueprintBlock{}
	for _, d := range scrFile.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}
		recv := recvTypeName(fn.Recv.List[0].Type)
		switch fn.Name.Name {
		case "ScreenTitle":
			titles[recv] = returnString(fn)
		case "ScreenDescription":
			descs[recv] = returnString(fn)
		case "Render", "RenderCtx":
			bodies[recv] = reverseRenderBody(fn, helpers)
		}
	}
	var out []BlueprintScreen
	for _, r := range regs {
		body := bodies[r.typeName]
		if isSynthesizedBody(body) {
			continue
		}
		s := BlueprintScreen{
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
			s.Access = BlueprintAccess{Auth: true, Role: r.role}
		}
		out = append(out, s)
	}
	return out, nil
}

// packScreenFileOrders returns the declaration orders carried by the
// screenRegistrar{order: N, …} literals in one per-screen file. Used by
// additive generation to continue screen orders after the existing set.
func packScreenFileOrders(file *ast.File) []int {
	var orders []int
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		id, ok := cl.Type.(*ast.Ident)
		if !ok || id.Name != "screenRegistrar" {
			return true
		}
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if identName(kv.Key) != "order" {
				continue
			}
			if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.INT {
				if n, err := strconv.Atoi(lit.Value); err == nil {
					orders = append(orders, n)
				}
			}
		}
		return false
	})
	return orders
}

// packHasPerScreenFiles reports whether dir uses the per-screen layout
// (≥1 screen_*.go file other than the fixed seam/shared helpers).
func packHasPerScreenFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name != "screen_shared.go" && strings.HasPrefix(name, "screen_") {
			return true
		}
	}
	return false
}

// packHasAggregatedScreens reports whether dir uses the pre-per-screen
// aggregated layout (a screens.go file with no per-screen files beside it).
// --add cannot extend that layout and must refuse.
func packHasAggregatedScreens(dir string) bool {
	if packHasPerScreenFiles(dir) {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "screens.go")); err != nil {
		return false
	}
	return true
}

// packReadScreenRegs reads site.Register / site.RegisterScreen(...) calls.
func packReadScreenRegs(file *ast.File) []screenReg {
	var regs []screenReg
	for _, stmt := range funcBody(file, "RegisterGenerated") {
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
					route:    astString(call.Args[0]),
					typeName: compositeTypeName(call.Args[1]),
					layout:   layoutVarToName(call.Args[2]),
				})
			}
		case "RegisterScreen": // site.RegisterScreen(app.NewScreen(route, &X{}).WithTitle(..).WithPolicy(blueprintAuthPolicy(login, role)), layout)
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
				r.route = astString(call.Args[0])
				r.typeName = compositeTypeName(call.Args[1])
			}
		case "WithPolicy":
			// arg is blueprintAuthPolicy(login, role)
			if len(call.Args) == 1 {
				if pc, ok := call.Args[0].(*ast.CallExpr); ok && len(pc.Args) == 2 {
					r.authed = true
					r.role = astString(pc.Args[1])
				}
			}
		}
		packWalkScreenChain(sel.X, r)
	}
}

func layoutVarToName(e ast.Expr) string {
	switch identName(e) {
	case "marketingLayout":
		return "marketing"
	case "appLayout":
		return "app"
	}
	return ""
}

func identName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// compositeTypeName: &XScreen{} → "XScreen".
func compositeTypeName(e ast.Expr) string {
	if u, ok := e.(*ast.UnaryExpr); ok {
		e = u.X
	}
	if cl, ok := e.(*ast.CompositeLit); ok {
		return identName(cl.Type)
	}
	return ""
}

func recvTypeName(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		return identName(star.X)
	}
	return identName(e)
}

func returnString(fn *ast.FuncDecl) string {
	if fn.Body == nil {
		return ""
	}
	for _, stmt := range fn.Body.List {
		if ret, ok := stmt.(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
			return astString(ret.Results[0])
		}
	}
	return ""
}

func typeNameToScreenName(tn string) string {
	tn = strings.TrimSuffix(tn, "Screen")
	return toSnakeCase(tn)
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

func isSynthesizedBody(body []BlueprintBlock) bool {
	return len(body) == 1 && (body[0].Kind == "entity_create" || body[0].Kind == "entity_edit")
}

// reverseRenderBody finds the screen's root call and reverses each child
// expression into a block. helpers maps package-local zero-arg function
// names to their single return expression, so a resource chain extracted
// into a shared helper (screen + island endpoint reusing one config) still
// reverses.
//
// Two root shapes are accepted. The generator emits
// `html.Div(html.DivConfig{…}, children…)`, the design system's 1:1 tag
// primitive. Apps generated before that switch (and hand-written screens
// that never moved) use `render.Tag("div", attrs, children…)`. pack has to
// read both or it silently returns no blocks for the other one, which is
// how a round-trip loses screens.
func reverseRenderBody(fn *ast.FuncDecl, helpers map[string]ast.Expr) []BlueprintBlock {
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
		// first, html.Div takes a single config.
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
		}
		if firstChild < 0 {
			continue
		}
		var out []BlueprintBlock
		for _, arg := range call.Args[firstChild:] {
			if b, ok := reverseBlock(arg, helpers); ok {
				out = append(out, b)
			}
		}
		return out
	}
	return nil
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
		if x := identName(sel.X); x != "" {
			return x + "." + sel.Sel.Name
		}
	}
	return ""
}

// reverseBlock turns one emitted UI expression back into a BlueprintBlock.
func reverseBlock(e ast.Expr, helpers map[string]ast.Expr) (BlueprintBlock, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return BlueprintBlock{}, false
	}
	// Entity resource chains (appResources["x"]…List/Detail/Form(ctx)).
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
					putStr(b.Props, "title", astString(cfgOf(call, 0)["Heading"]))
					return b, true
				}
			}
		}
		c := cfgOf(call, 0)
		return block("card", props2("heading", astString(c["Heading"]), "text", astString(c["Description"]))), true
	case "ui.BarChart", "ui.PieChart", "lineChart":
		// Untitled chart (no wrapping ui.Card).
		if b, ok := reverseChartCall(call); ok {
			return b, true
		}
	case "ui.PageHeader":
		c := cfgOf(call, 0)
		return block("page_header", props2("title", astString(c["Title"]), "subtitle", astString(c["Subtitle"]), "eyebrow", astString(c["Eyebrow"]))), true
	case "ui.LinkButton":
		c := cfgOf(call, 0)
		return block("link_button", props2("label", astString(c["Label"]), "href", astString(c["Href"]), "variant", buttonVariant(c["Variant"]))), true
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
		return BlueprintBlock{Kind: "markdown", Text: astString(cfgOf(call, 0)["Source"])}, true
	case "ui.AuthCard":
		return reverseAuthCard(call), true
	case "html.Heading":
		c := cfgOf(call, 0)
		lvl := 1
		if n, ok := astInt(c["Level"]); ok {
			lvl = n
		}
		return BlueprintBlock{Type: "heading", Level: lvl, Text: renderTextArg(call.Args[len(call.Args)-1])}, true
	case "render.Tag":
		return reverseRenderTag(call)
	}
	return BlueprintBlock{}, false
}

func reverseLayoutBlock(kind string, call *ast.CallExpr, helpers map[string]ast.Expr) BlueprintBlock {
	b := BlueprintBlock{Kind: kind, Props: map[string]any{}}
	cfg := cfgOf(call, 0)
	if kind == "grid" {
		putStr(b.Props, "min", astString(cfg["Min"]))
	}
	if gap := reverseGap(astSelName(cfg["Gap"])); gap != "" && gap != "md" {
		b.Props["gap"] = gap
	}
	if kind == "stack" || kind == "cluster" {
		if align := reverseAlign(astSelName(cfg["Align"])); align != "" && align != "start" {
			b.Props["align"] = align
		}
		if justify := reverseJustify(astSelName(cfg["Justify"])); justify != "" && justify != "start" {
			b.Props["justify"] = justify
		}
	}
	if kind == "cluster" && astBool(cfg["NoWrap"]) {
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

func reverseEntityResource(call *ast.CallExpr, helpers map[string]ast.Expr) (BlueprintBlock, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return BlueprintBlock{}, false
	}
	method := sel.Sel.Name
	if method != "List" && method != "Detail" && method != "Form" {
		return BlueprintBlock{}, false
	}
	b := BlueprintBlock{}
	node := sel.X
	hops := 0
	for {
		// Bound the helper-hop walk so a self-referential or mutually
		// recursive zero-arg helper (a() returns a()) can't loop
		// forever. 32 is far beyond any real helper chain depth;
		// returning not-reversible is the safe fallback (the screen
		// just isn't packed; it stays as authored Go).
		if hops > 32 {
			return BlueprintBlock{}, false
		}
		if idx, ok := node.(*ast.IndexExpr); ok {
			if identName(idx.X) == "appResources" {
				b.Entity = astString(idx.Index)
				break
			}
			return BlueprintBlock{}, false
		}
		c, ok := node.(*ast.CallExpr)
		if !ok {
			return BlueprintBlock{}, false
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
			return BlueprintBlock{}, false
		}
		switch s.Sel.Name {
		case "WithColumns":
			for _, a := range c.Args {
				b.Fields = append(b.Fields, astString(a))
			}
		case "WithSearch":
			if len(c.Args) == 1 {
				b.Search = astString(c.Args[0])
			}
		case "WithFilters":
			for _, a := range c.Args {
				if key := astString(fieldVals(a)["Key"]); key != "" {
					b.Filters = append(b.Filters, key)
				}
			}
		case "WithLimit":
			if len(c.Args) == 1 {
				if n, ok := astInt(c.Args[0]); ok {
					b.Limit = n
				}
			}
		case "WithCreate":
			b.Create = true
		case "WithHeading":
			if len(c.Args) == 1 {
				b.Text = astString(c.Args[0])
			}
		case "WithEmpty":
			if len(c.Args) == 1 {
				b.EmptyText = astString(c.Args[0])
			}
		case "WithTransitions":
			for _, a := range c.Args {
				tv := fieldVals(a)
				b.Transitions = append(b.Transitions, BlueprintTransition{
					Label:   astString(tv["Label"]),
					Status:  astString(tv["Status"]),
					Variant: astString(tv["Variant"]),
					Stamp:   astString(tv["Stamp"]),
				})
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
		if len(call.Args) == 2 && astString(call.Args[1]) == "" {
			b.Kind = "entity_create"
		} else {
			b.Kind = "entity_edit"
		}
	}
	return b, true
}

func reverseHero(call *ast.CallExpr) BlueprintBlock {
	c := cfgOf(call, 0)
	p := map[string]any{}
	putStr(p, "eyebrow", astString(c["Eyebrow"]))
	putStr(p, "title", astString(c["Title"]))
	putStr(p, "subtitle", astString(c["Subtitle"]))
	// Actions: []render.HTML{LinkButton(cta), LinkButton(secondary)}.
	if actions, ok := c["Actions"].(*ast.CompositeLit); ok {
		for i, el := range actions.Elts {
			ac, ok := el.(*ast.CallExpr)
			if !ok || callSel(ac) != "ui.LinkButton" {
				continue
			}
			lc := cfgOf(ac, 0)
			label, href := astString(lc["Label"]), astString(lc["Href"])
			if i == 0 {
				putStr(p, "cta_text", label)
				putStr(p, "cta_href", href)
			} else {
				putStr(p, "secondary_text", label)
				putStr(p, "secondary_href", href)
			}
		}
	}
	return BlueprintBlock{Kind: "hero", Props: p}
}

func reverseSection(call *ast.CallExpr, helpers map[string]ast.Expr) BlueprintBlock {
	c := cfgOf(call, 0)
	b := BlueprintBlock{Kind: "section", Props: props2("heading", astString(c["Heading"]), "eyebrow", astString(c["Eyebrow"]), "description", astString(c["Description"]))}
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

func reverseStatCard(call *ast.CallExpr) BlueprintBlock {
	c := cfgOf(call, 0)
	p := map[string]any{}
	putStr(p, "label", astString(c["Label"]))
	// Value: statValue(ctx, entity, agg, field, filter, format).
	if vc, ok := c["Value"].(*ast.CallExpr); ok && callSel(vc) == "" {
		if id, ok := vc.Fun.(*ast.Ident); ok && id.Name == "statValue" && len(vc.Args) == 6 {
			src := map[string]any{}
			putStr(src, "entity", astString(vc.Args[1]))
			putStr(src, "agg", astString(vc.Args[2]))
			putStr(src, "field", astString(vc.Args[3]))
			putStr(src, "filter", astString(vc.Args[4]))
			p["source"] = src
			putStr(p, "format", astString(vc.Args[5]))
		}
	}
	return BlueprintBlock{Kind: "stat_card", Props: p}
}

func reversePricing(call *ast.CallExpr) BlueprintBlock {
	plans := []any{}
	for _, arg := range call.Args[1:] {
		pc, ok := arg.(*ast.CallExpr)
		if !ok || callSel(pc) != "ui.PricingCard" {
			continue
		}
		c := cfgOf(pc, 0)
		plan := map[string]any{}
		putStr(plan, "name", astString(c["Name"]))
		putStr(plan, "price", astString(c["Price"]))
		putStr(plan, "period", astString(c["Period"]))
		putStr(plan, "description", astString(c["Description"]))
		if feats := astStringSlice(c["Features"]); len(feats) > 0 {
			fa := make([]any, len(feats))
			for i, f := range feats {
				fa[i] = f
			}
			plan["features"] = fa
		}
		putStr(plan, "cta_text", astString(c["CTALabel"]))
		putStr(plan, "cta_href", astString(c["CTAHref"]))
		if astBool(c["Featured"]) {
			plan["featured"] = true
		}
		plans = append(plans, plan)
	}
	return BlueprintBlock{Kind: "pricing", Props: map[string]any{"plans": plans}}
}

func reverseAuthCard(call *ast.CallExpr) BlueprintBlock {
	c := cfgOf(call, 0)
	// Body: ui.Form(ui.FormConfig{Action: …}, hidden-next, fields…).
	action, next := "", ""
	if form, ok := c["Body"].(*ast.CallExpr); ok && callSel(form) == "ui.Form" {
		action = astString(cfgOf(form, 0)["Action"])
		for _, arg := range form.Args[1:] {
			raw := rawString(arg)
			if strings.Contains(raw, `name="next"`) {
				next = htmlAttr(raw, "value")
			}
		}
	}
	footerHref := htmlAttr(rawString(c["Footer"]), "href")
	if footerHref == "" {
		// Since v0.61 the emitter renders the auth footer as the URL-checked
		// ui.Link component instead of a raw anchor; read Href from its config.
		if link, ok := c["Footer"].(*ast.CallExpr); ok && callSel(link) == "ui.Link" {
			footerHref = astString(cfgOf(link, 0)["Href"])
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
	return BlueprintBlock{Kind: kind, Text: astString(c["Title"]), Props: p}
}

// rawString unwraps render.Raw("…") → its string content.
func rawString(e ast.Expr) string {
	if call, ok := e.(*ast.CallExpr); ok && callSel(call) == "render.Raw" && len(call.Args) == 1 {
		return astString(call.Args[0])
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

func reverseRenderTag(call *ast.CallExpr) (BlueprintBlock, bool) {
	tag := astString(call.Args[0])
	switch tag {
	case "p":
		return BlueprintBlock{Type: "paragraph", Text: renderTextArg(call.Args[len(call.Args)-1])}, true
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
				return BlueprintBlock{Kind: kind, Props: p}, true
			}
		}
	}
	return BlueprintBlock{}, false
}

// reverseChartCall recognizes the three chart emission shapes and returns
// the corresponding blueprint block (without a title; the caller sets it
// from a wrapping ui.Card heading when present):
//
//	ui.BarChart(ui.BarChartConfig{Bars: groupBars(ctx, e, g), …})
//	ui.PieChart(ui.PieChartConfig{Slices: groupSlices(ctx, e, g)})
//	lineChart(ctx, e, g)
func reverseChartCall(call *ast.CallExpr) (BlueprintBlock, bool) {
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
		return BlueprintBlock{Kind: kind, Props: p}, true
	case "lineChart":
		// lineChart(ctx, entity, group_by)
		return BlueprintBlock{Kind: "line_chart", Props: map[string]any{"source": chartSource(call)}}, true
	}
	return BlueprintBlock{}, false
}

func chartSource(dataCall *ast.CallExpr) map[string]any {
	src := map[string]any{}
	if len(dataCall.Args) == 3 {
		putStr(src, "entity", astString(dataCall.Args[1]))
		putStr(src, "group_by", astString(dataCall.Args[2]))
	}
	return src
}

// ----- small reverse helpers -------------------------------------------------

func cfgOf(call *ast.CallExpr, i int) map[string]ast.Expr {
	if i < len(call.Args) {
		return fieldVals(call.Args[i])
	}
	return map[string]ast.Expr{}
}

func block(kind string, props map[string]any) BlueprintBlock {
	return BlueprintBlock{Kind: kind, Props: props}
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

func astInt(e ast.Expr) (int, bool) {
	if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.INT {
		if n, err := strconv.Atoi(lit.Value); err == nil {
			return n, true
		}
	}
	return 0, false
}

func renderTextArg(e ast.Expr) string {
	if call, ok := e.(*ast.CallExpr); ok && callSel(call) == "render.Text" && len(call.Args) == 1 {
		return astString(call.Args[0])
	}
	return ""
}

func buttonVariant(e ast.Expr) string {
	switch astSelName(e) {
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

// packBlueprint reconstructs a full Blueprint from a generated app directory.
// secretsInBlueprint reports whether the packed blueprint carries any of
// the three values packReadDotEnv recovers from .env.
// secretsInBlueprint decides whether pack prints its do-NOT-commit
// warning. It defers to dsnHasSecret for the DSN rather than testing for
// "@": the generator already uses dsnHasSecret to decide what to redact,
// and the two disagreeing means pack stays silent about a secret the
// generator considered worth hiding. A keyword/value DSN
// ("host=db user=app password=hunter2") is the case that fell through --
// it carries a password and no "@" at all.
func secretsInBlueprint(bp Blueprint) bool {
	return bp.App.Auth.JWTSecret != "" ||
		bp.App.Admin.SeedPassword != "" ||
		dsnHasSecret(bp.App.DBURL)
}

func packBlueprint(dir string) (Blueprint, error) {
	var bp Blueprint
	app, err := packReadApp(dir)
	if err != nil {
		return bp, err
	}
	bp.App = app
	if bp.Entities, err = packReadEntities(dir); err != nil {
		return bp, err
	}
	if bp.Screens, err = packReadScreens(dir); err != nil {
		return bp, err
	}
	if bp.Nav, err = packReadNav(dir); err != nil {
		return bp, err
	}
	if bp.Seed, err = packReadSeed(dir); err != nil {
		return bp, err
	}
	return bp, nil
}

// runPack implements `gofastr pack [app-dir] [-o out.yml]`: a lossy
// best-effort snapshot, not a round-trip inverse of generate. It reconstructs
// a gofastr.yml from a generated app's Go source.
func runPack(args []string) {
	dir := "."
	out := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o", "--out":
			if i+1 < len(args) {
				out = args[i+1]
				i++
			}
		case "-h", "--help":
			info("Usage: gofastr pack [app-dir] [-o out.yml]")
			info("Reconstructs a best-effort gofastr.yml from a generated app's Go source")
			info("(entities, app config, theme, screens, nav, seed). Lossy: not a round-trip")
			info("inverse of `gofastr generate`; hand-written handlers/hooks/business logic")
			info("are not recovered. See framework/ARCHITECTURE.md (\"pack is one-way\").")
			return
		default:
			if !strings.HasPrefix(args[i], "-") {
				dir = args[i]
			}
		}
	}
	bp, err := packBlueprint(dir)
	if err != nil {
		fail("pack: %v", err)
		osExit(1)
		return
	}
	yml, err := encodeBlueprintYAML(bp)
	if err != nil {
		fail("pack: %v", err)
		osExit(1)
		return
	}
	// packReadDotEnv rehydrates JWT_SECRET / ADMIN_SEED_PASSWORD /
	// DATABASE_URL from the app's .env so the packed blueprint round-trips
	// (see the comment on packReadDotEnv). That reverses the generator's
	// own "secrets never land in committed source" rule, which
	// TestBlueprintNeverInlinesSecrets pins, and unlike `.env`, an
	// arbitrary `-o` path is not covered by the generated .gitignore. Warn
	// loudly and write 0600 rather than 0644.
	if secretsInBlueprint(bp) {
		warn("pack: output contains secrets recovered from .env (jwt_secret, seed_password, db.url): do NOT commit it")
	}
	if out == "" {
		fmt.Print(yml)
		return
	}
	// The 0600 argument to os.WriteFile only applies when the file is
	// CREATED. Overwriting an existing 0644 file keeps 0644, and this
	// output carries the jwt_secret, seed password, and credentialed DSN
	// recovered from .env -- so a second `pack -o` over an earlier run's
	// file published all three. Open, chmod the handle, then write, the
	// same order the generated .env uses.
	if err := writeSecretFile(out, yml); err != nil {
		fail("pack: write %s: %v", out, err)
		osExit(1)
		return
	}
	success("Packed %s → %s (%d entities, %d screens)", dir, out, len(bp.Entities), len(bp.Screens))
}

// writeSecretFile writes content at owner-only permissions, tightening a
// pre-existing file's mode before any content lands. Chmod goes through
// the open handle so it cannot be redirected by a symlink swapped in
// between the open and the mode change.
func writeSecretFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
