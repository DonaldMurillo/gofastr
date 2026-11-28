package packcmd

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/core/settings"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/dsnredact"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
	"github.com/DonaldMurillo/gofastr/kiln/freeze"
)

func decodeAppSpecString(yml string) (appspec.AppSpec, error) {
	node, err := coreyaml.Parse(yml)
	if err != nil {
		return appspec.AppSpec{}, err
	}
	return appspec.DecodeAppSpec(node)
}

func encodeAppSpecYAML(bp appspec.AppSpec) (string, error) {
	root := appSpecToMap(bp)
	if at := invalidUTF8At(reflect.ValueOf(root), "app spec"); at != "" {
		return "", fmt.Errorf("%s holds a byte that is not UTF-8; YAML cannot carry it", at)
	}
	var sb strings.Builder
	if err := writeYAMLMap(&sb, root, 0, topLevelOrder, ""); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func appSpecToMap(bp appspec.AppSpec) map[string]any {
	m := map[string]any{}
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
	if a.EntityLLMMD != nil {
		m["entity_llm_md"] = *a.EntityLLMMD
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
			if r.Hidden {
				putBool(rm, "hidden", r.Hidden)
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
	putStr(m, "filter", b.Filter)
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

func relationTypeToString(t framework.RelationType) string {
	return appgen.RelationKindFor(t).Yaml
}

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
	if h := e.Hints; h != (settings.EndpointHints{}) {
		hm := map[string]any{}
		putStr(hm, "title", h.Title)
		for _, hint := range []struct {
			key string
			v   settings.Hint
		}{{"read_only", h.ReadOnly}, {"destructive", h.Destructive}, {"idempotent", h.Idempotent}, {"open_world", h.OpenWorld}} {
			if hint.v != settings.HintUnset {
				hm[hint.key] = hint.v == settings.HintTrue
			}
		}
		m["hints"] = hm
	}
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

func anyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

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
	withoutDescription := bp
	withoutDescription.App.Description = ""
	if bp.App.Description == appgen.AppSpecSiteDescription(withoutDescription) {
		bp.App.Description = ""
	}
	if bp.Screens, err = packReadScreens(dir); err != nil {
		return bp, err
	}
	read := bp
	bp.Screens = slices.DeleteFunc(slices.Clone(bp.Screens), func(s appspec.AppSpecScreen) bool {
		return appspec.IsSynthesizedLoginScreen(read, s)
	})
	packReconcileAppHome(bp.Screens, packReadAppHome(dir))
	packResolveOmits(bp)
	packFoldRecordKeys(bp)
	if bp.Nav, err = packReadNav(dir); err != nil {
		return bp, err
	}
	seedEntities := make(map[string]framework.EntityDeclaration, len(bp.Entities))
	for _, d := range bp.Entities {
		seedEntities[d.Name] = d
	}
	if bp.Seed, err = packReadSeed(dir, seedEntities); err != nil {
		return bp, err
	}
	if err = packReadStubs(dir, &bp); err != nil {
		return bp, err
	}
	if bp.Endpoints, _, err = appgen.ExistingEndpoints(dir); err != nil {
		return bp, err
	}
	return bp, nil
}

const packOmitProp = "\x00omit"

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

const packUsage = "Usage: gofastr pack [app-dir] [-o out.yml]"

type packArgs struct {
	dir  string
	out  string
	help bool
}

// AppDir returns the project directory for a pack invocation.
func AppDir(args []string) (string, error) {
	opts, err := parsePackArgs(args)
	return opts.dir, err
}

func parsePackArgs(args []string) (packArgs, error) {
	var opts packArgs
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "-o", "--out":
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "-") {
				return packArgs{}, fmt.Errorf("%s needs a file name", arg)
			}
			i++
			opts.out = args[i]
		case "-h", "--help":
			return packArgs{help: true}, nil
		default:
			if name, value, ok := strings.Cut(arg, "="); ok && (name == "-o" || name == "--out") {
				if value == "" {
					return packArgs{}, fmt.Errorf("%s needs a file name", name)
				}
				opts.out = value
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return packArgs{}, fmt.Errorf("unknown flag %s (%s)", arg, packUsage)
			}
			if opts.dir != "" {
				return packArgs{}, fmt.Errorf("expected at most one app directory (%s)", packUsage)
			}
			if arg == "" {
				return packArgs{}, fmt.Errorf("app directory must not be empty (%s)", packUsage)
			}
			opts.dir = arg
		}
	}
	if opts.dir == "" {
		opts.dir = "."
	}
	return opts, nil
}

// RunPack reconstructs a best-effort app spec from a generated app's Go source.
func RunPack(args []string) {
	opts, err := parsePackArgs(args)
	if err != nil {
		cli.UsageFail("pack: %v", err)
		return
	}
	if opts.help {
		cli.Info(packUsage)
		cli.Info("Reconstructs a best-effort gofastr.yml from a generated app's Go source")
		cli.Info("(entities, app config, theme, screens, nav, seed). Lossy: not a round-trip")
		cli.Info("inverse of `gofastr generate`; hand-written handlers/hooks/business logic")
		cli.Info("are not recovered. See framework/ARCHITECTURE.md (\"pack is one-way\").")
		cli.Info("Pack then regenerates from the packed spec and checks the Go files at the")
		cli.Info("app root and directly under entities/ (tests aside): it names each one it")
		cli.Info("does not reproduce, could not read, or left out as hand-written.")
		return
	}
	dir, out := opts.dir, opts.out
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
	decoded, err := decodeAppSpecString(yml)
	if err != nil {
		packFail(toStderr, "pack: could not decode the packed YAML: %v", err)
		return
	}
	if bp.App.Admin.SeedEmail != "" && bp.App.Admin.SeedPassword == "" {
		packNote(toStderr, "pack: the app seeds admin %s, but %s holds no ADMIN_SEED_PASSWORD, so the packed spec seeds no admin; add seed_password under app.admin before generating from it", bp.App.Admin.SeedEmail, filepath.Join(dir, ".env"))
	}
	if c, err := checkPack(dir, decoded); err != nil {
		packNote(toStderr, "pack: could not check the packed spec against %s: %v", dir, err)
	} else {
		reportPackCheck(c, toStderr)
	}
	if secretsInAppSpec(bp) {
		packNote(toStderr, "pack: output contains secrets recovered from .env (jwt_secret, seed_password, db.url): do NOT commit it")
	}
	if out == "" {
		cli.Print(yml)
		return
	}
	if err := writeSecretFile(out, yml); err != nil {
		cli.Fail("pack: write %s: %v", out, err)
		cli.OsExit(1)
		return
	}
	cli.Success("Packed %s → %s (%d entities, %d screens)", dir, out, len(bp.Entities), len(bp.Screens))
}

func writeSecretFile(path, content string) error {
	return fileperm.ReplaceOwnerOnly(path, []byte(content))
}

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
