package freeze

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// BlueprintYAML converts a Kiln world into the current gofastr.yml scaffold
// contract. world.json remains the lossless authoring snapshot; the blueprint
// is the one-shot, generator-ready graduation artifact.
func BlueprintYAML(w *world.World) ([]byte, error) {
	if w == nil {
		return nil, fmt.Errorf("freeze: nil world")
	}
	if err := validateGraduation(w); err != nil {
		return nil, err
	}
	doc := normalizeYAMLValue(blueprintMap(w)).(map[string]any)
	var out strings.Builder
	writeYAMLMap(&out, doc, 0, topLevelOrder)
	return []byte(out.String()), nil
}

func validateGraduation(w *world.World) error {
	for _, e := range sortedEntities(w) {
		if e.MultiTenant {
			return fmt.Errorf("freeze: entity %q sets multi_tenant, but a blueprint cannot choose the app-specific tenant resolver; use owner_field for per-user scoping or wire tenant middleware in owned Go before freezing", e.Name)
		}
	}
	for path, page := range w.Pages {
		if page != nil {
			if err := validateNodeGraduation(page.Tree); err != nil {
				return fmt.Errorf("freeze: page %q: %w", path, err)
			}
		}
	}
	if w.App.Auth.Enabled && !w.App.Auth.DevMode && w.App.Auth.JWTSecret == "" {
		return fmt.Errorf("freeze: production auth requires app.auth.jwt_secret; set it or keep auth.dev_mode true for local development")
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
		}
	}
	return nil
}

func validateNodeGraduation(n world.Node) error {
	for key := range n.Props {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "class" || normalized == "style" || strings.HasPrefix(normalized, "on") {
			return fmt.Errorf("node kind %q uses forbidden app-local styling or handler prop %q; compose a design-system kind instead", n.Kind, key)
		}
	}
	for _, child := range n.Children {
		if err := validateNodeGraduation(child); err != nil {
			return err
		}
	}
	return nil
}

func blueprintMap(w *world.World) map[string]any {
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
		putString(db, "url", a.DBURL)
		m["db"] = db
	}
	putString(m, "static_dir", a.StaticDir)
	putString(m, "output_dir", a.OutputDir)
	// Kiln stores the resolved value, so an empty prefix is a deliberate
	// opt-out and must be emitted explicitly rather than re-defaulting to api.
	m["api_prefix"] = strings.Trim(a.APIPrefix, "/")
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
		m["auth"] = map[string]any{
			"enabled": a.Auth.Enabled, "dev_mode": a.Auth.DevMode,
			"base_path": a.Auth.BasePath, "jwt_secret": a.Auth.JWTSecret,
		}
	}
	if a.Admin.Enabled || a.Admin.Path != "" || a.Admin.Role != "" || a.Admin.LoginPath != "" || a.Admin.SeedEmail != "" || a.Admin.SeedPassword != "" {
		m["admin"] = compact(map[string]any{
			"enabled": a.Admin.Enabled, "path": a.Admin.Path, "role": a.Admin.Role,
			"login_path": a.Admin.LoginPath, "seed_email": a.Admin.SeedEmail,
			"seed_password": a.Admin.SeedPassword,
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
		if len(e.Endpoints) > 0 {
			endpoints := make([]any, 0, len(e.Endpoints))
			for i, ep := range e.Endpoints {
				name := ep.Name
				if name == "" {
					name = endpointName(ep.Method, ep.Path, i)
				}
				endpoints = append(endpoints, compact(map[string]any{
					"name": name, "method": strings.ToUpper(ep.Method), "path": ep.Path,
					"description": ep.Description, "mcp": ep.MCP,
					"handler": identifier(e.Name + "_" + name),
				}))
			}
			m["endpoints"] = endpoints
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
			name = identifier(strings.Trim(path, "/"))
			if name == "" {
				name = "home"
			}
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
	out := make([]any, 0, len(seeds))
	for _, seed := range seeds {
		if seed == nil {
			continue
		}
		m := compact(map[string]any{"entity": seed.Entity, "rows": seed.Rows, "count": seed.Count, "weights": seed.Weights})
		out = append(out, m)
	}
	return out
}

func endpointMaps(w *world.World) []any {
	out := make([]any, 0, len(w.Endpoints)+len(w.Routes))
	for i, ep := range w.Endpoints {
		if ep == nil {
			continue
		}
		name := ep.Name
		if name == "" {
			name = endpointName(ep.Method, ep.Path, i)
		}
		handler := ep.Handler
		if handler == "" {
			handler = identifier(name)
		}
		out = append(out, compact(map[string]any{
			"name": name, "method": strings.ToUpper(ep.Method), "path": ep.Path,
			"entity": ep.Entity, "handler": handler,
			"description": ep.Description, "mcp": ep.MCP,
		}))
	}
	for i, route := range w.Routes {
		if route == nil {
			continue
		}
		name := endpointName(route.Method, route.Path, len(out)+i)
		out = append(out, map[string]any{
			"name": name, "method": strings.ToUpper(route.Method), "path": route.Path,
			"handler":     identifier(name),
			"description": "Kiln declarative action " + route.Action.Kind + "; implement the owned-Go handler after generation",
		})
	}
	return out
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
	for _, key := range orderedKeys(m, order) {
		writeYAMLEntry(sb, key, m[key], indent)
	}
}

func writeYAMLEntry(sb *strings.Builder, key string, value any, indent int) {
	pad := strings.Repeat(" ", indent)
	switch v := value.(type) {
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
	if m, ok := item.(map[string]any); ok && len(m) > 0 {
		keys := orderedKeys(m, order)
		// core/yaml expects the first list-map key to have a scalar value.
		// Prefer one even for arbitrary seed-row maps.
		for i, key := range keys {
			if isScalar(m[key]) {
				keys[0], keys[i] = keys[i], keys[0]
				break
			}
		}
		first := keys[0]
		sb.WriteString(strings.Repeat(" ", indent) + "- " + first + ":")
		if isScalar(m[first]) {
			sb.WriteByte(' ')
			writeScalarInline(sb, m[first])
			sb.WriteByte('\n')
		} else {
			sb.WriteByte('\n')
			writeNestedValue(sb, m[first], indent+4, orderFor(first))
		}
		for _, key := range keys[1:] {
			writeYAMLEntry(sb, key, m[key], indent+2)
		}
		return
	}
	sb.WriteString(strings.Repeat(" ", indent) + "- ")
	writeScalarInline(sb, item)
	sb.WriteByte('\n')
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
		strings.ContainsAny(value, "\n\t") || value != strings.TrimSpace(value) ||
		strings.HasSuffix(value, ":") {
		return true
	}
	return false
}

func orderedKeys(m map[string]any, order []string) []string {
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
	entityOrder   = []string{"name", "table", "crud", "mcp", "soft_delete", "multi_tenant", "owner_field", "cross_owner_read", "search_fields", "timestamps", "cursor_field", "cursor_fields", "properties", "access", "indices", "fields", "relations", "endpoints"}
	fieldOrder    = []string{"name", "type", "required", "unique", "default", "max", "min", "pattern", "values", "to", "many", "auto_generate", "read_only", "hidden"}
	screenOrder   = []string{"name", "route", "title", "description", "type", "layout", "access", "body"}
	blockOrder    = []string{"kind", "props", "children"}
	relationOrder = []string{"type", "name", "entity", "foreign_key", "through", "local_key", "foreign_key_target"}
	indexOrder    = []string{"name", "columns", "unique"}
	navOrder      = []string{"label", "href", "icon", "role", "items"}
	accessOrder   = []string{"auth", "role", "read", "create", "update", "delete"}
	endpointOrder = []string{"name", "method", "path", "entity", "handler", "description", "mcp"}
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
