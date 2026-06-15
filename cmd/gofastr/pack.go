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
// pack.go — the inverse of generate. encodeBlueprintYAML serializes a Blueprint
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

// encodeBlueprintYAML serializes a Blueprint to gofastr.yml text.
func encodeBlueprintYAML(bp Blueprint) string {
	root := blueprintToMap(bp)
	var sb strings.Builder
	writeYAMLMap(&sb, root, 0, topLevelOrder)
	return sb.String()
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
	return m
}

func appToMap(a BlueprintApp) map[string]any {
	m := map[string]any{}
	putStr(m, "name", a.Name)
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
	return m
}

func entityToMap(e framework.EntityDeclaration) map[string]any {
	m := map[string]any{}
	putStr(m, "name", e.Name)
	putStr(m, "table", e.Table)
	if e.CRUD != nil {
		m["crud"] = *e.CRUD
	}
	putBool(m, "mcp", e.MCP)
	putBool(m, "soft_delete", e.SoftDelete)
	putBool(m, "multi_tenant", e.MultiTenant)
	putStr(m, "owner_field", e.OwnerField)
	if e.Timestamps != nil {
		m["timestamps"] = *e.Timestamps
	}
	putStr(m, "cursor_field", e.CursorField)
	putStrs(m, "cursor_fields", e.CursorFields)
	if len(e.Properties) > 0 {
		m["properties"] = anyMap(e.Properties)
	}
	if e.Access != nil {
		acc := map[string]any{}
		putStr(acc, "read", e.Access.Read)
		putStr(acc, "create", e.Access.Create)
		putStr(acc, "update", e.Access.Update)
		putStr(acc, "delete", e.Access.Delete)
		if len(acc) > 0 {
			m["access"] = acc
		}
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
			// owner_field — the author never wrote it, so packing it back
			// would diverge from the source blueprint.
			if e.OwnerField != "" && f.Name == e.OwnerField && f.Hidden {
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

func writeYAMLMap(sb *strings.Builder, m map[string]any, indent int, order []string) {
	for _, k := range orderedKeys(m, order) {
		writeYAMLEntry(sb, k, m[k], indent)
	}
}

func writeYAMLEntry(sb *strings.Builder, key string, val any, indent int) {
	pad := strings.Repeat(" ", indent)
	switch v := val.(type) {
	case map[string]any:
		if len(v) == 0 {
			sb.WriteString(pad + key + ": {}\n")
			return
		}
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
			sb.WriteString("\n")
			return
		}
		sb.WriteString(pad + key + ":\n")
		for _, item := range v {
			writeYAMLListItem(sb, item, indent+2, orderFor(key))
		}
	default:
		sb.WriteString(pad + key + ": ")
		writeScalarInline(sb, val)
		sb.WriteString("\n")
	}
}

func writeYAMLListItem(sb *strings.Builder, item any, indent int, order []string) {
	if m, ok := item.(map[string]any); ok && len(m) > 0 {
		var tmp strings.Builder
		writeYAMLMap(&tmp, m, indent+2, order)
		s := tmp.String()
		// Replace the first line's leading (indent+2) spaces with "<indent>- ".
		sb.WriteString(strings.Repeat(" ", indent) + "- " + s[indent+2:])
		return
	}
	sb.WriteString(strings.Repeat(" ", indent) + "- ")
	writeScalarInline(sb, item)
	sb.WriteString("\n")
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

// formatYAMLFloat keeps a float a float on re-parse — the parser only types a
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

func quoteYAMLString(s string) string {
	if !needsQuote(s) {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// needsQuote reports whether emitting s bare would re-parse as a non-string (or
// break the line/map grammar). Conservative but minimal — keeps the output close
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
	if strings.ContainsAny(s[:1], "[]{}&*!|>'\"%@`#,?: -") {
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
	topLevelOrder = []string{"app", "entities", "screens", "nav", "seed", "endpoints", "middleware", "plugins", "helpers"}
	appOrder      = []string{"name", "module", "db", "static_dir", "output_dir", "api_prefix", "theme", "auth", "admin"}
	entityOrder   = []string{"name", "table", "crud", "mcp", "soft_delete", "multi_tenant", "owner_field", "timestamps", "cursor_field", "cursor_fields", "properties", "access", "indices", "fields", "relations"}
	fieldOrder    = []string{"name", "type", "required", "unique", "default", "max", "min", "pattern", "values", "to", "many", "auto_generate", "read_only", "hidden"}
	screenOrder   = []string{"name", "route", "title", "description", "type", "layout", "access", "body"}
	blockOrder    = []string{"kind", "type", "text", "level", "entity", "fields", "search", "limit", "create", "empty_text", "class", "href", "mode", "island", "widget", "props", "children", "actions", "transitions"}
	relationOrder = []string{"type", "name", "entity", "foreign_key", "through", "local_key", "foreign_key_target"}
	indexOrder    = []string{"name", "columns", "unique"}
	navOrder      = []string{"label", "href", "icon", "role", "items"}
	accessOrder   = []string{"auth", "role", "read", "create", "update", "delete"}
	dbOrder       = []string{"driver", "url"}
	authOrder     = []string{"enabled", "dev_mode", "base_path", "jwt_secret"}
	adminOrder    = []string{"path", "role", "enabled", "login_path", "seed_email", "seed_password"}
	endpointOrder = []string{"name", "method", "path", "entity", "handler", "description", "mcp"}
	actionOrder   = []string{"name", "event", "client_js"}
	transitionOrder = []string{"label", "status", "variant", "stamp"}
	seedOrder     = []string{"entity", "rows"}
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
	default:
		return nil
	}
}

// =============================================================================
// AST readers — reconstruct a Blueprint from a generated app's Go source.
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
		// Detect a map by element shape — nested literals inside
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

// fieldVals returns the key→value expressions of a struct composite literal.
func fieldVals(e ast.Expr) map[string]ast.Expr {
	out := map[string]ast.Expr{}
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

// packReadEntities reconstructs the entity declarations from entities/register.go.
func packReadEntities(dir string) ([]framework.EntityDeclaration, error) {
	path := filepath.Join(dir, "entities", "register.go")
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
		decl := framework.EntityDeclaration{Name: astString(call.Args[0])}
		cfg := fieldVals(call.Args[1])
		if v, ok := cfg["Table"]; ok {
			decl.Table = astString(v)
		}
		if v, ok := cfg["SoftDelete"]; ok {
			decl.SoftDelete = astBool(v)
		}
		if v, ok := cfg["MultiTenant"]; ok {
			decl.MultiTenant = astBool(v)
		}
		if v, ok := cfg["OwnerField"]; ok {
			decl.OwnerField = astString(v)
		}
		if v, ok := cfg["MCP"]; ok {
			decl.MCP = astBool(v)
		}
		if v, ok := cfg["CursorField"]; ok {
			decl.CursorField = astString(v)
		}
		if v, ok := cfg["CursorFields"]; ok {
			decl.CursorFields = astStringSlice(v)
		}
		if v, ok := cfg["CRUD"]; ok {
			if b, ok := astPtrCallBool(v); ok {
				decl.CRUD = &b
			}
		}
		if v, ok := cfg["Timestamps"]; ok {
			if b, ok := astPtrCallBool(v); ok {
				decl.Timestamps = &b
			}
		}
		if v, ok := cfg["Properties"]; ok {
			if m, ok := astAny(v).(map[string]any); ok && len(m) > 0 {
				decl.Properties = m
			}
		}
		if v, ok := cfg["Access"]; ok {
			a := fieldVals(v)
			decl.Access = &framework.AccessDeclaration{
				Read:   astString(a["Read"]),
				Create: astString(a["Create"]),
				Update: astString(a["Update"]),
				Delete: astString(a["Delete"]),
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
		out = append(out, decl)
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
func returnValue(file *ast.File, fn string) ast.Expr {
	for _, stmt := range funcBody(file, fn) {
		if ret, ok := stmt.(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
			return ret.Results[0]
		}
	}
	return nil
}

// packReadSeed reconstructs seed data from blueprint/stubs.go BlueprintSeedData().
func packReadSeed(dir string) ([]BlueprintSeedEntity, error) {
	path := filepath.Join(dir, "blueprint", "stubs.go")
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	file, err := packParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	lit, ok := returnValue(file, "BlueprintSeedData").(*ast.CompositeLit)
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
// BlueprintSidebarConfig() ui.SidebarConfig{Items: []ui.SidebarItem{...}}.
func packReadNav(dir string) ([]BlueprintNavItem, error) {
	path := filepath.Join(dir, "blueprint", "app.go")
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	file, err := packParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg, ok := returnValue(file, "BlueprintSidebarConfig").(*ast.CompositeLit)
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
	appPath := filepath.Join(dir, "blueprint", "app.go")
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
	app.Name = consts["BlueprintAppName"]
	app.Module = consts["BlueprintModule"]
	app.DBDriver = consts["BlueprintDBDriver"]
	app.DBURL = consts["BlueprintDBURL"]
	app.StaticDir = consts["BlueprintStaticDir"]
	if v, ok := consts["BlueprintAPIPrefix"]; ok {
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

	// Admin config lives in main.go.
	if mainFile, err := packParseFile(filepath.Join(dir, "main.go")); err == nil {
		ast.Inspect(mainFile, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok || astSelLast(cl.Type) != "Config" {
				return true
			}
			cv := fieldVals(cl)
			if _, ok := cv["PathPrefix"]; !ok {
				return true // not an admin.Config
			}
			app.Admin.Enabled = true
			app.Admin.Path = astString(cv["PathPrefix"])
			app.Admin.Role = astString(cv["AdminRole"])
			app.Admin.LoginPath = astString(cv["LoginPath"])
			return true
		})
	}
	return app, nil
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

// packReadTheme parses BlueprintTheme()'s assignments back into the authored
// theme map (colors + font_heading/font_body) + the dark-scheme overrides.
func packReadTheme(file *ast.File) (map[string]string, map[string]string) {
	var light, dark map[string]string
	for _, stmt := range funcBody(file, "BlueprintTheme") {
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
			light[camelToKebab(path[2])] = astString(asn.Rhs[0])
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

func camelToKebab(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('-')
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
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

// packReadScreens reconstructs the authored screens. Routes/layout/access come
// from the site.Register* calls in app.go; titles + bodies come from the screen
// types in screens.go. Synthesized /new + /{id}/edit form screens (body is a
// resource Form call) are dropped — they weren't authored.
func packReadScreens(dir string) ([]BlueprintScreen, error) {
	appFile, err := packParseFile(filepath.Join(dir, "blueprint", "app.go"))
	if err != nil {
		return nil, err
	}
	regs := packReadScreenRegs(appFile)
	scrFile, err := packParseFile(filepath.Join(dir, "blueprint", "screens.go"))
	if err != nil {
		return nil, err
	}
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
			bodies[recv] = reverseRenderBody(fn)
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
	return camelToSnake(tn)
}

func camelToSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
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

// reverseRenderBody finds the screen's `return render.Tag("div", attrs, …)` and
// reverses each child expression into a block.
func reverseRenderBody(fn *ast.FuncDecl) []BlueprintBlock {
	if fn.Body == nil {
		return nil
	}
	for _, stmt := range fn.Body.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		call, ok := ret.Results[0].(*ast.CallExpr)
		if !ok || callSel(call) != "render.Tag" || len(call.Args) < 2 {
			continue
		}
		var out []BlueprintBlock
		for _, arg := range call.Args[2:] {
			if b, ok := reverseBlock(arg); ok {
				out = append(out, b)
			}
		}
		return out
	}
	return nil
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
func reverseBlock(e ast.Expr) (BlueprintBlock, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return BlueprintBlock{}, false
	}
	// Entity resource chains (blueprintResources["x"]…List/Detail/Form(ctx)).
	if b, ok := reverseEntityResource(call); ok {
		return b, true
	}
	switch callSel(call) {
	case "ui.Hero":
		return reverseHero(call), true
	case "ui.Section":
		return reverseSection(call), true
	case "ui.Card":
		c := cfgOf(call, 0)
		return block("card", props2("heading", astString(c["Heading"]), "text", astString(c["Description"]))), true
	case "ui.PageHeader":
		c := cfgOf(call, 0)
		return block("page_header", props2("title", astString(c["Title"]), "subtitle", astString(c["Subtitle"]), "eyebrow", astString(c["Eyebrow"]))), true
	case "ui.LinkButton":
		c := cfgOf(call, 0)
		return block("link_button", props2("label", astString(c["Label"]), "href", astString(c["Href"]), "variant", buttonVariant(c["Variant"]))), true
	case "ui.Grid":
		// A grid of pricing cards is a `pricing` block; a grid of stat cards
		// is a stat_row.
		if len(call.Args) > 1 {
			if first, ok := call.Args[1].(*ast.CallExpr); ok && callSel(first) == "ui.PricingCard" {
				return reversePricing(call), true
			}
		}
		b := BlueprintBlock{Kind: "stat_row"}
		for _, arg := range call.Args[1:] {
			if cb, ok := reverseBlock(arg); ok {
				b.Children = append(b.Children, cb)
			}
		}
		return b, true
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

func reverseEntityResource(call *ast.CallExpr) (BlueprintBlock, bool) {
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
	for {
		if idx, ok := node.(*ast.IndexExpr); ok {
			if identName(idx.X) == "blueprintResources" {
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
		// synthesized create/edit form screen — mark so it can be dropped.
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

func reverseSection(call *ast.CallExpr) BlueprintBlock {
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
				if cb, ok := reverseBlock(child); ok {
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
	// Value: blueprintStatValue(ctx, entity, agg, field, filter, format).
	if vc, ok := c["Value"].(*ast.CallExpr); ok && callSel(vc) == "" {
		if id, ok := vc.Fun.(*ast.Ident); ok && id.Name == "blueprintStatValue" && len(vc.Args) == 6 {
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
				// source from blueprintGroupBars/Slices(ctx, entity, group_by).
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

// runPack implements `gofastr pack [app-dir] [-o out.yml]` — the inverse of
// generate. It reconstructs the gofastr.yml from a generated app's Go source.
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
			info("Reconstructs gofastr.yml from a generated app's Go source (entities, app")
			info("config, theme, screens, nav, seed). The inverse of `gofastr generate`.")
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
	yml := encodeBlueprintYAML(bp)
	if out == "" {
		fmt.Print(yml)
		return
	}
	if err := os.WriteFile(out, []byte(yml), 0o644); err != nil {
		fail("pack: write %s: %v", out, err)
		osExit(1)
		return
	}
	success("Packed %s → %s (%d entities, %d screens)", dir, out, len(bp.Entities), len(bp.Screens))
}
