package sdkdocs

import (
	"context"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
	"github.com/DonaldMurillo/gofastr/framework/sdk"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// ---------------------------------------------------------------------------
// Shared chrome: nav rail, drift banner, layout
// ---------------------------------------------------------------------------

const drawerName = "sdkdocs-section-menu"

func (s *site) menuConfig(active string) interactive.SectionMenuConfig {
	guide := []interactive.SectionItem{
		{Label: "Authentication", Href: s.cfg.BasePath + "/auth", Active: active == "auth"},
		{Label: "Errors", Href: s.cfg.BasePath + "/errors", Active: active == "errors"},
	}
	var ents []interactive.SectionItem
	for _, e := range s.includedEntities() {
		ents = append(ents, interactive.SectionItem{
			Label:  e.Config.Name,
			Href:   s.cfg.BasePath + "/entities/" + e.Config.Table,
			Active: active == "entity:"+e.Config.Table,
		})
	}
	groups := []interactive.SectionGroup{{Label: "Guides", Items: guide}}
	if len(ents) > 0 {
		groups = append(groups, interactive.SectionGroup{Label: "Entities", Items: ents})
	}
	return interactive.SectionMenuConfig{
		AriaLabel:    "SDK documentation",
		TriggerLabel: "SDK docs",
		DrawerName:   drawerName,
		Lead:         &interactive.SectionItem{Label: "Overview", Href: s.cfg.BasePath, Active: active == ""},
		Groups:       groups,
	}
}

func sectionMenuDrawer(s *site) *widget.Builder {
	return interactive.SectionMenuDrawer(s.menuConfig(""))
}

// page wraps a screen body in the standard doc chrome (nav rail, crumbs,
// drift banner).
func (s *site) page(active, title string, body ...render.HTML) render.HTML {
	crumbs := []ui.DocCrumb{{Label: "API & SDKs", Href: s.cfg.BasePath}}
	if title != "" {
		crumbs = append(crumbs, ui.DocCrumb{Label: title})
	}
	content := make([]render.HTML, 0, len(body)+1)
	if banner := s.driftBanner(); banner != "" {
		content = append(content, banner)
	}
	content = append(content, body...)
	return ui.DocLayout(ui.DocLayoutConfig{
		Nav:    interactive.SectionMenu(s.menuConfig(active)),
		Crumbs: crumbs,
	}, content...)
}

func (s *site) driftBanner() render.HTML {
	_, drift, provenance := s.resolved()
	switch {
	case drift:
		return ui.Banner(ui.BannerConfig{
			Variant: ui.BannerWarn,
			Title:   "SDK downloads may be out of date",
			Body:    "The API schema changed since these SDKs were generated. Re-run `gofastr generate sdk` and redeploy.",
		})
	case provenance:
		return ui.Banner(ui.BannerConfig{
			Variant: ui.BannerInfo,
			Title:   "SDK downloads have unknown provenance",
			Body:    "The artifact manifest is missing or from a different gofastr version, so freshness can't be verified.",
		})
	default:
		return ""
	}
}

func (s *site) exampleOrigin() string {
	base := s.cfg.BaseURL
	if base == "" {
		base = "https://your-app.example.com"
	}
	return strings.TrimRight(base, "/") + s.cfg.APIPrefix
}

func text(t string) render.HTML { return render.Text(t) }

func para(children ...render.HTML) render.HTML {
	return html.Paragraph(html.TextConfig{}, children...)
}

func code(t string) render.HTML {
	return html.Code(html.TextConfig{}, render.Text(t))
}

func heading(level int, t string) render.HTML {
	return html.Heading(html.HeadingConfig{Level: level}, render.Text(t))
}

// ---------------------------------------------------------------------------
// / — overview: downloads + install + quickstart
// ---------------------------------------------------------------------------

type indexScreen struct{ site *site }

func (sc *indexScreen) ScreenTitle() string { return sc.site.cfg.AppName + " SDKs" }
func (sc *indexScreen) ScreenDescription() string {
	return "Client SDKs and API reference for " + sc.site.cfg.AppName
}

func (sc *indexScreen) Render() render.HTML {
	s := sc.site
	m, _, _ := s.resolved()

	body := []render.HTML{
		heading(1, s.cfg.AppName+" SDKs & API"),
		para(
			text("Typed clients for the "+s.cfg.AppName+" HTTP API — a standalone Go module and a zero-dependency JS/TS client — plus a live reference for every documented entity. The machine-readable spec is at "),
			code("/openapi.json"),
			text(" (may require auth unless the app opts into a public spec)."),
		),
		heading(2, "Downloads"),
	}
	body = append(body, s.downloads(m)...)
	body = append(body,
		heading(2, "Install"),
		s.installTabs(),
	)
	if ents := s.includedEntities(); len(ents) > 0 {
		body = append(body,
			heading(2, "Quickstart"),
			s.quickstartTabs(ents[0]),
		)
	}
	return s.page("", "", body...)
}

func (s *site) downloads(m *sdk.Manifest) []render.HTML {
	if s.cfg.Artifacts == nil || m == nil {
		return []render.HTML{
			para(text("Downloads are not published on this deployment yet. Generate them from the app source and point "),
				code("sdkdocs.Config.Artifacts"), text(" at the dist directory:")),
			ui.TerminalBlock(ui.TerminalBlockConfig{Label: "$ generate"},
				text("gofastr generate sdk")),
		}
	}
	stamp := fmt.Sprintf("Version %s · generated with gofastr %s", m.SDKVersion, m.GofastrVersion)
	if !m.GeneratedAt.IsZero() {
		stamp += " · " + m.GeneratedAt.UTC().Format("2006-01-02")
	}
	out := []render.HTML{para(html.Small(html.TextConfig{}, text(stamp)))}

	type dl struct{ key, label, href string }
	for _, d := range []dl{
		{"go", "Go SDK (zip)", s.cfg.BasePath + "/sdk/go.zip"},
		{"js", "client.js", s.cfg.BasePath + "/sdk/client.js"},
		{"js-types", "client.d.ts", s.cfg.BasePath + "/sdk/client.d.ts"},
	} {
		a, ok := m.Artifacts[d.key]
		if !ok {
			continue
		}
		out = append(out, para(
			ui.LinkButton(ui.LinkButtonConfig{Label: d.label, Href: d.href, Variant: ui.ButtonSecondary}),
			html.Small(html.TextConfig{}, text(" "+humanBytes(a.Bytes))),
		))
	}
	return out
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func (s *site) installTabs() render.HTML {
	// Docs + downloads mount on the app host, NOT under the API prefix —
	// only API example URLs get exampleOrigin().
	docsBase := s.exampleHost() + s.cfg.BasePath
	goInstall := fmt.Sprintf(`curl -LO %s/sdk/go.zip
unzip go.zip
go mod edit -replace %s=./%s
go mod tidy`, docsBase, s.goModuleHint(), s.goDirHint())
	jsInstall := fmt.Sprintf(`# Two plain files — download them, or import straight from this app:
curl -LO %s/sdk/client.js
curl -LO %s/sdk/client.d.ts

# or, in the browser / Deno, with no download at all:
# import { Client } from "%s/sdk/client.js";`,
		docsBase, docsBase, docsBase)
	curl := fmt.Sprintf(`# No SDK required — it's a plain HTTP API.
curl -H "Authorization: Bearer $API_TOKEN" %s/<entity>`, s.exampleOrigin())

	return ui.CodeTabs(ui.CodeTabsConfig{Name: "sdk-install", Label: "Install"},
		ui.CodeSample{Label: "Go", Language: "shell", Code: goInstall},
		ui.CodeSample{Label: "JS / TS", Language: "shell", Code: jsInstall},
		ui.CodeSample{Label: "curl", Language: "shell", Code: curl},
	)
}

// goModuleHint/goDirHint read the manifest when present so the install
// snippet matches the generated module path exactly.
func (s *site) goModuleHint() string {
	if m, _, _ := s.resolved(); m != nil {
		if a, ok := m.Artifacts["go"]; ok && a.Module != "" {
			return a.Module
		}
	}
	return "local/<app>-sdk"
}

func (s *site) goDirHint() string {
	if m, _, _ := s.resolved(); m != nil && m.App != "" {
		return m.App + "-sdk"
	}
	return "<app>-sdk"
}

func (s *site) quickstartTabs(e *entity.Entity) render.HTML {
	origin := s.exampleOrigin()
	structName := upperFirst(casing.ToCamel(e.Config.Name))
	prop := casing.ToCamel(e.Config.Table)

	goCode := fmt.Sprintf(`c := client.NewClient(%q, nil)
c.Token = os.Getenv("API_TOKEN")

page, err := c.List%s(ctx, nil)
if err != nil {
	log.Fatal(err)
}
fmt.Println(page.Total)`, origin, structName)

	tsCode := fmt.Sprintf(`import { Client } from "./client.js";

const api = new Client({ baseURL: %q, token: process.env.API_TOKEN });
const page = await api.%s.list({ limit: 25, sort: "-created_at" });
console.log(page.total);`, origin, prop)

	curlCode := fmt.Sprintf(`curl -H "Authorization: Bearer $API_TOKEN" \
  "%s/%s?limit=25&sort=-created_at"`, origin, e.Config.Table)

	return ui.CodeTabs(ui.CodeTabsConfig{Name: "sdk-quickstart", Label: "Quickstart"},
		ui.CodeSample{Label: "Go", Language: "go", Code: goCode},
		ui.CodeSample{Label: "TypeScript", Language: "ts", Code: tsCode},
		ui.CodeSample{Label: "curl", Language: "shell", Code: curlCode},
	)
}

// ---------------------------------------------------------------------------
// /auth
// ---------------------------------------------------------------------------

type authScreen struct{ site *site }

func (sc *authScreen) ScreenTitle() string { return "API authentication" }
func (sc *authScreen) ScreenDescription() string {
	return "Bearer-token authentication for the " + sc.site.cfg.AppName + " API"
}

func (sc *authScreen) Render() render.HTML {
	s := sc.site
	origin := s.exampleOrigin()

	body := []render.HTML{
		heading(1, "Authentication"),
		para(
			text("Programmatic clients authenticate with a bearer API token sent as "),
			code("Authorization: Bearer <token>"),
			text(". Both SDKs attach it automatically once configured."),
		),
	}
	if s.cfg.HasAPITokens {
		body = append(body,
			heading(2, "Mint a token"),
			para(text("Log in with a session first, then create a scoped token. The plaintext token is shown once — store it like a password.")),
			ui.CodeTabs(ui.CodeTabsConfig{Name: "sdk-auth-mint"},
				ui.CodeSample{Label: "curl", Language: "shell", Code: fmt.Sprintf(`curl -X POST %s%s/tokens \
  -H "Content-Type: application/json" \
  --cookie "$SESSION_COOKIE" \
  -d '{"name": "ci", "scopes": ["read"], "ttl_seconds": 2592000}'`, strings.TrimRight(s.exampleHost(), "/"), s.cfg.AuthBasePath)},
			),
		)
	}
	body = append(body,
		heading(2, "Use the token"),
		ui.CodeTabs(ui.CodeTabsConfig{Name: "sdk-auth-use"},
			ui.CodeSample{Label: "Go", Language: "go", Code: `c := client.NewClient(baseURL, nil)
c.Token = os.Getenv("API_TOKEN")`},
			ui.CodeSample{Label: "TypeScript", Language: "ts", Code: `const api = new Client({ baseURL, token: process.env.API_TOKEN });`},
			ui.CodeSample{Label: "curl", Language: "shell", Code: fmt.Sprintf(`curl -H "Authorization: Bearer $API_TOKEN" %s/<entity>`, origin)},
		),
		para(text("Tokens carry scopes; a request outside the token's scopes is refused with 403. Rotate by minting a new token and deleting the old one.")),
	)
	return s.page("auth", "Authentication", body...)
}

// exampleHost is the origin WITHOUT the API prefix (auth mounts beside the
// API, not under it).
func (s *site) exampleHost() string {
	base := s.cfg.BaseURL
	if base == "" {
		base = "https://your-app.example.com"
	}
	return strings.TrimRight(base, "/")
}

// ---------------------------------------------------------------------------
// /errors
// ---------------------------------------------------------------------------

type errorsScreen struct{ site *site }

func (sc *errorsScreen) ScreenTitle() string { return "API errors" }
func (sc *errorsScreen) ScreenDescription() string {
	return "Error envelope and status codes for the " + sc.site.cfg.AppName + " API"
}

func (sc *errorsScreen) Render() render.HTML {
	s := sc.site
	rows := []ui.Row{
		errRow("400", "Validation failed or malformed request — the envelope carries a fields map."),
		errRow("401", "No session or bearer token."),
		errRow("403", "Authenticated but not permitted (RBAC / token scopes)."),
		errRow("404", "No such record or route."),
		errRow("413", "Request body too large."),
		errRow("429", "Rate limited — retry later."),
	}
	return s.page("errors", "Errors",
		heading(1, "Errors"),
		para(text("Every non-2xx JSON response uses one envelope. Validation errors key the "),
			code("fields"), text(" map by the "), html.Strong(html.TextConfig{}, text("snake_case column name")),
			text(" — not the camelCase response casing. Both SDKs surface the envelope (Go: "),
			code("*APIError"), text("; JS: "), code("ApiError"), text(").")),
		ui.CodeBlock(ui.CodeBlockConfig{
			Language: "json",
			ShowCopy: true,
			Lines: ui.HighlightLines(`{
  "error": "validation failed",
  "success": false,
  "code": 400,
  "fields": { "author_name": ["is required"] }
}`, "json"),
		}),
		heading(2, "Status codes"),
		ui.DataTable(ui.DataTableConfig{
			Caption: "API status codes",
			Columns: []ui.Column{
				{Key: "status", Header: "Status"},
				{Key: "meaning", Header: "Meaning"},
			},
			Rows: rows,
		}),
		para(text("Batch endpoints are the one exception: a rolled-back batch answers 400 with the normal batch envelope ("),
			code("committed: false"), text(" plus per-item errors), and both SDKs return it as a result, not an error.")),
	)
}

func errRow(status, meaning string) ui.Row {
	return ui.Row{Cells: map[string]render.HTML{
		"status":  code(status),
		"meaning": text(meaning),
	}}
}

// ---------------------------------------------------------------------------
// /entities/:name
// ---------------------------------------------------------------------------

type entityScreen struct {
	site   *site
	params map[string]string
}

func (sc *entityScreen) SetParams(params map[string]string) { sc.params = params }

// StaticPaths exports one page per included entity (keyed by table — the
// URL segment).
func (sc *entityScreen) StaticPaths(_ context.Context) []map[string]string {
	var out []map[string]string
	for _, e := range sc.site.includedEntities() {
		out = append(out, map[string]string{"name": e.Config.Table})
	}
	return out
}

func (sc *entityScreen) ScreenTitle() string {
	if e, ok := sc.current(); ok {
		return e.Config.Name + " — API reference"
	}
	return "API reference"
}

func (sc *entityScreen) ScreenDescription() string {
	return "Entity API reference for " + sc.site.cfg.AppName
}

func (sc *entityScreen) current() (*entity.Entity, bool) {
	return sc.site.lookup(sc.params["name"])
}

func (sc *entityScreen) Render() render.HTML {
	s := sc.site
	e, ok := sc.current()
	if !ok {
		// The visibility policy 404s before render; this is belt and
		// braces for direct RenderPage calls.
		return s.page("", "Not found", heading(1, "Not found"))
	}
	cfg := e.Config
	table := cfg.Table

	body := []render.HTML{
		heading(1, cfg.Name),
		para(text("Base path "), code(s.cfg.APIPrefix+"/"+table), text(". Responses are "),
			text(map[bool]string{true: "snake_case", false: "camelCase"}[s.cfg.SnakeCase]),
			text("; filter and sort query parameters always use the snake_case column names shown below.")),
		heading(2, "Fields"),
		sc.fieldsTable(cfg),
		heading(2, "Endpoints"),
		sc.endpointsTable(cfg),
		heading(2, "Listing, filtering, sorting"),
		sc.listParamsNotes(cfg),
		heading(2, "Examples"),
		sc.exampleTabs(cfg),
	}
	return s.page("entity:"+table, cfg.Name, body...)
}

// fieldsTable ports the non-Hidden walk from crud.EntityLLMMD: hidden
// columns must never appear in public documentation.
func (sc *entityScreen) fieldsTable(cfg entity.EntityConfig) render.HTML {
	var rows []ui.Row
	for _, f := range cfg.Fields {
		if f.Hidden {
			continue
		}
		var notes []string
		if f.AutoGenerate != schema.AutoNone {
			notes = append(notes, "auto-generated")
		} else if f.ReadOnly {
			notes = append(notes, "read-only")
		}
		if f.Unique {
			notes = append(notes, "unique")
		}
		if len(f.Values) > 0 {
			notes = append(notes, "one of: "+strings.Join(f.Values, ", "))
		}
		if f.Default != nil {
			notes = append(notes, fmt.Sprintf("default %v", f.Default))
		}
		required := ""
		if f.Required {
			required = "yes"
		}
		rows = append(rows, ui.Row{Cells: map[string]render.HTML{
			"field":    code(f.Name),
			"type":     text(fieldTypeLabel(f.Type)),
			"required": text(required),
			"notes":    text(strings.Join(notes, "; ")),
		}})
	}
	return ui.DataTable(ui.DataTableConfig{
		Caption: cfg.Name + " fields",
		Columns: []ui.Column{
			{Key: "field", Header: "Field"},
			{Key: "type", Header: "Type"},
			{Key: "required", Header: "Required"},
			{Key: "notes", Header: "Notes"},
		},
		Rows: rows,
	})
}

func (sc *entityScreen) endpointsTable(cfg entity.EntityConfig) render.HTML {
	base := sc.site.cfg.APIPrefix + "/" + cfg.Table
	type op struct{ method, path, desc string }
	ops := []op{
		{"GET", base, "List (offset or cursor pagination)"},
		{"GET", base + "/{id}", "Get one record"},
		{"POST", base, "Create"},
		{"PUT", base + "/{id}", "Full update"},
		{"PATCH", base + "/{id}", "Partial update — only keys present in the body change"},
		{"DELETE", base + "/{id}", "Delete"},
		{"POST", base + "/_batch", "Atomic batch create ({\"items\": […]})"},
		{"PATCH", base + "/_batch", "Atomic batch update (items carry id)"},
		{"DELETE", base + "/_batch", "Atomic batch delete ({\"ids\": […]})"},
		{"GET", base + "/_events", "Live SSE feed: entity.created / updated / deleted"},
	}
	for _, ep := range cfg.Endpoints {
		path := ep.Path
		if !strings.HasPrefix(path, "/") {
			path = base + "/" + path
		}
		desc := ep.Description
		if desc == "" {
			desc = "Custom endpoint" + map[bool]string{true: " (" + ep.Name + ")", false: ""}[ep.Name != ""]
		}
		ops = append(ops, op{strings.ToUpper(ep.Method), path, desc + " — use the SDK's Do escape hatch"})
	}
	var rows []ui.Row
	for _, o := range ops {
		rows = append(rows, ui.Row{Cells: map[string]render.HTML{
			"method": ui.Tag(ui.TagConfig{Label: o.method, Variant: methodVariant(o.method)}),
			"path":   code(o.path),
			"desc":   text(o.desc),
		}})
	}
	return ui.DataTable(ui.DataTableConfig{
		Caption: cfg.Name + " endpoints",
		Columns: []ui.Column{
			{Key: "method", Header: "Method"},
			{Key: "path", Header: "Path"},
			{Key: "desc", Header: "Description"},
		},
		Rows: rows,
	})
}

func methodVariant(method string) ui.StatusVariant {
	switch method {
	case "GET":
		return ui.StatusInfo
	case "POST":
		return ui.StatusSuccess
	case "DELETE":
		return ui.StatusDanger
	default:
		return ui.StatusWarning
	}
}

func (sc *entityScreen) listParamsNotes(cfg entity.EntityConfig) render.HTML {
	items := []render.HTML{
		para(code("?page=2&limit=25"), text(" — offset pagination; "), code("?cursor=&limit=25"), text(" — keyset pagination (pass the returned cursor to continue).")),
		para(code("?sort=-created_at"), text(" — leading "), code("-"), text(" for descending; snake_case column names.")),
		para(code("?<column>=x"), text(" — equality filter; suffix operators: "), code("_gt _gte _lt _lte _like _in"), text(".")),
		para(code("?include=relation"), text(" — eager-load declared relations; "), code("?fields=col1,col2"), text(" — project columns.")),
	}
	if len(cfg.SearchFields) > 0 {
		items = append(items, para(code("?q=term"), text(" — free-text search over: "+strings.Join(cfg.SearchFields, ", ")+".")))
	}
	if cfg.SoftDelete {
		items = append(items, para(code("?trashed=only|with"), text(" — include soft-deleted rows.")))
	}
	return render.Join(items...)
}

func (sc *entityScreen) exampleTabs(cfg entity.EntityConfig) render.HTML {
	s := sc.site
	origin := s.exampleOrigin()
	structName := upperFirst(casing.ToCamel(cfg.Name))
	prop := casing.ToCamel(cfg.Table)
	filterField := exampleFilterField(cfg)

	goCode := fmt.Sprintf(`params := url.Values{}
params.Set(%q, "10")
params.Set("sort", "-%s")
page, err := c.List%s(ctx, params)

created, err := c.Create%s(ctx, client.%sInput{ /* … */ })
_ = c.Watch%s(ctx, func(event string, data []byte) error {
	fmt.Println(event)
	return nil
})`, filterField+"_gte", filterField, structName, structName, structName, structName)

	tsCode := fmt.Sprintf(`const page = await api.%s.list({
  sort: "-%s",
  filters: { %q: 10 },
});

const created = await api.%s.create({ /* … */ });
await api.%s.watch((event) => console.log(event), { signal });`,
		prop, filterField, filterField+"_gte", prop, prop)

	curlCode := fmt.Sprintf(`curl -H "Authorization: Bearer $API_TOKEN" \
  "%s/%s?%s_gte=10&sort=-%s"`, origin, cfg.Table, filterField, filterField)

	return ui.CodeTabs(ui.CodeTabsConfig{Name: "sdk-ent-" + cfg.Table},
		ui.CodeSample{Label: "Go", Language: "go", Code: goCode},
		ui.CodeSample{Label: "TypeScript", Language: "ts", Code: tsCode},
		ui.CodeSample{Label: "curl", Language: "shell", Code: curlCode},
	)
}

// exampleFilterField picks a plausible filterable column for the examples:
// preferably numeric/temporal (so a _gte comparison reads sensibly), else
// the first visible non-auto field, else created_at.
func exampleFilterField(cfg entity.EntityConfig) string {
	first := ""
	for _, f := range cfg.Fields {
		if f.Hidden || f.AutoGenerate != schema.AutoNone || f.Type == schema.Relation {
			continue
		}
		switch f.Type {
		case schema.Int, schema.Float, schema.Decimal, schema.Timestamp, schema.Date:
			return f.Name
		}
		if first == "" {
			first = f.Name
		}
	}
	if first != "" {
		return first
	}
	return "created_at"
}

// upperFirst turns casing.ToCamel's lowerCamel wire form into the
// PascalCase Go identifier the generated client exports.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// fieldTypeLabel mirrors crud/llmmd.go's labels — human-readable field
// types for the reference table.
func fieldTypeLabel(t schema.FieldType) string {
	switch t {
	case schema.String:
		return "string"
	case schema.Text:
		return "text"
	case schema.Int:
		return "integer"
	case schema.Float:
		return "float"
	case schema.Decimal:
		return "decimal (string on the wire)"
	case schema.Bool:
		return "boolean"
	case schema.Enum:
		return "enum"
	case schema.UUID:
		return "uuid"
	case schema.Timestamp:
		return "timestamp"
	case schema.Date:
		return "date"
	case schema.JSON:
		return "json"
	case schema.Relation:
		return "relation"
	case schema.Image:
		return "image (url)"
	case schema.File:
		return "file (url)"
	default:
		return "string"
	}
}
