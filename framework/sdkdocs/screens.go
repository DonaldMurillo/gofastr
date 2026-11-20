package sdkdocs

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/openapi"
	"github.com/DonaldMurillo/gofastr/framework/sdk"
	"github.com/DonaldMurillo/gofastr/framework/sdkdocs/internal/docpage"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/internal/casing"
)

func drawerName(basePath string) string {
	return "sdkdocs-section-menu-" + hex.EncodeToString([]byte(basePath))
}

func (s *site) menuConfig(active string) interactive.SectionMenuConfig {
	guide := []interactive.SectionItem{
		{Label: "Authentication", Href: s.cfg.BasePath + "/auth", Active: active == "auth"},
		{Label: "Errors", Href: s.cfg.BasePath + "/errors", Active: active == "errors"},
	}
	var ents []interactive.SectionItem
	for _, e := range s.firstIncludedEntityPerTable() {
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
		DrawerName:   drawerName(s.cfg.BasePath),
		Lead:         &interactive.SectionItem{Label: "Overview", Href: s.cfg.BasePath, Active: active == "overview"},
		Groups:       groups,
	}
}

func sectionMenuDrawer(s *site) *widget.Builder {
	return interactive.SectionMenuDrawerFunc(func(context.Context) interactive.SectionMenuConfig { return s.menuConfig("") })
}

func (s *site) page(active, title string, body ...render.HTML) render.HTML {
	crumbs := []ui.Crumb{{Text: "API & SDKs", Href: s.cfg.BasePath}}
	if title != "" {
		crumbs = append(crumbs, ui.Crumb{Text: title})
	}
	content := make([]render.HTML, 0, len(body)+1)
	if banner := s.driftBanner(); banner != "" {
		content = append(content, banner)
	}
	content = append(content, body...)
	return docpage.Render(docpage.Config{
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
			Body:    "The artifact manifest is missing, unreadable or uses a different manifest schema version, so freshness can't be verified.",
		})
	default:
		return ""
	}
}

func (s *site) exampleOrigin() string {
	return sdk.ExampleHost(s.cfg.BaseURL) + s.cfg.APIPrefix
}

func (s *site) entityPrefix(e *entity.Entity) string {
	if e.Version != "" {
		return strings.TrimRight(e.Version, "/")
	}
	return s.cfg.APIPrefix
}

func (s *site) entityBase(e *entity.Entity) string {
	return s.entityPrefix(e) + "/" + e.Config.Table
}

func (s *site) entityOrigin(e *entity.Entity) string {
	return sdk.ExampleHost(s.cfg.BaseURL) + s.entityPrefix(e)
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

type indexScreen struct{ site *site }

func (sc *indexScreen) ScreenTitle(ctx context.Context) string { return sc.site.cfg.AppName + " SDKs" }
func (sc *indexScreen) ScreenDescription(ctx context.Context) string {
	return "Client SDKs and API reference for " + sc.site.cfg.AppName
}

func (sc *indexScreen) Render() render.HTML {
	s := sc.site
	m, _, _ := s.resolved()

	body := []render.HTML{
		heading(1, s.cfg.AppName+" SDKs & API"),
		para(
			text("Typed clients for the "+s.cfg.AppName+" HTTP API: a standalone Go module and a zero-dependency JS/TS client, plus a live reference for every documented entity. The machine-readable spec is at "),
			code("/openapi.json"),
			text(" (may require auth unless the app opts into a public spec)."),
		),
		heading(2, "Downloads"),
	}
	body = append(body, s.downloads(m)...)
	body = append(body,
		heading(2, "Install"),
		s.installTabs(m),
	)
	if ents := s.includedEntities(); len(ents) > 0 && (m == nil || m.Source != sdk.SourceOpenAPI) {
		body = append(body,
			heading(2, "Quickstart"),
			s.quickstartTabs(ents[0]),
		)
	}
	return s.page("overview", "", body...)
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
	if m.Source == sdk.SourceOpenAPI && s.cfg.OpenAPI == nil {
		return []render.HTML{
			para(text("Downloads are withheld on this deployment: the SDKs were generated from an OpenAPI document, and the site serves them only beside the document they describe. Pass it as "),
				code("sdkdocs.Config.OpenAPI"), text(".")),
		}
	}
	if !s.artifactsDocumented(m) {
		return []render.HTML{
			para(text("Downloads are withheld on this deployment: the generated SDKs cover entities this reference does not document. Regenerate them for the documented entities:")),
			ui.TerminalBlock(ui.TerminalBlockConfig{Label: "$ generate"},
				text("gofastr generate sdk --only=<entities>")),
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

func (s *site) installTabs(m *sdk.Manifest) render.HTML {
	docsBase := sdk.ExampleHost(s.cfg.BaseURL) + s.cfg.BasePath
	goInstall := "# Generate the Go SDK with: gofastr generate sdk --target=go"
	if m != nil {
		a := m.Artifacts["go"]
		if module := fenceWord(a.Module); module != "" {
			goInstall = fmt.Sprintf(`curl -LO %s/sdk/go.zip
unzip go.zip
go mod edit -replace %s=./%s
go mod tidy`, docsBase, module, a.Directory)
		}
	}
	jsInstall := fmt.Sprintf(`# Two plain files: download them, or import straight from this app:
curl -LO %s/sdk/client.js
curl -LO %s/sdk/client.d.ts

# or, in the browser / Deno, with no download at all:
# import { Client } from "%s/sdk/client.js";`,
		docsBase, docsBase, docsBase)
	curl := fmt.Sprintf(`# No SDK required: it's a plain HTTP API.
curl -H "%s" %s/<entity>`, s.tokenHeaderLine("$API_TOKEN"), s.exampleOrigin())

	return ui.CodeTabs(ui.CodeTabsConfig{Name: "sdk-install", Label: "Install"},
		ui.CodeSample{Label: "Go", Language: "shell", Code: goInstall},
		ui.CodeSample{Label: "JS / TS", Language: "shell", Code: jsInstall},
		ui.CodeSample{Label: "curl", Language: "shell", Code: curl},
	)
}

func (s *site) quickstartTabs(e *entity.Entity) render.HTML {
	origin := s.entityOrigin(e)
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
const page = await api.%s.list({ limit: 25 });
console.log(page.total);`, origin, prop)

	curlCode := fmt.Sprintf(`curl -H "%s" \
  "%s/%s?limit=25"`, s.tokenHeaderLine("$API_TOKEN"), origin, e.Config.Table)

	return ui.CodeTabs(ui.CodeTabsConfig{Name: "sdk-quickstart", Label: "Quickstart"},
		ui.CodeSample{Label: "Go", Language: "go", Code: goCode},
		ui.CodeSample{Label: "TypeScript", Language: "ts", Code: tsCode},
		ui.CodeSample{Label: "curl", Language: "shell", Code: curlCode},
	)
}

type authScreen struct{ site *site }

func (sc *authScreen) ScreenTitle(ctx context.Context) string { return "API authentication" }
func (sc *authScreen) ScreenDescription(ctx context.Context) string {
	return "API-token authentication for the " + sc.site.cfg.AppName + " API"
}

func (sc *authScreen) Render() render.HTML {
	s := sc.site
	origin := s.exampleOrigin()

	body := []render.HTML{
		heading(1, "Authentication"),
		para(
			text("Programmatic clients authenticate with an API token sent as "),
			code(s.tokenHeaderLine("<token>")),
			text(". Both SDKs attach it automatically once configured."),
		),
	}
	if s.cfg.HasAPITokens {
		body = append(body,
			heading(2, "Mint a token"),
			para(text("Log in with a session first, then create a scoped token. The plaintext token is shown once: store it like a password.")),
			ui.CodeTabs(ui.CodeTabsConfig{Name: "sdk-auth-mint"},
				ui.CodeSample{Label: "curl", Language: "shell", Code: fmt.Sprintf(`curl -X POST %s%s/tokens \
  -H "Content-Type: application/json" \
  --cookie "$SESSION_COOKIE" \
  -d '{"name": "ci", "scopes": ["read"], "ttl_seconds": 2592000}'`, sdk.ExampleHost(s.cfg.BaseURL), s.cfg.AuthBasePath)},
			),
		)
	}
	body = append(body,
		heading(2, "Use the token"),
		ui.CodeTabs(ui.CodeTabsConfig{Name: "sdk-auth-use"},
			ui.CodeSample{Label: "Go", Language: "go", Code: `c := client.NewClient(baseURL, nil)
c.Token = os.Getenv("API_TOKEN")`},
			ui.CodeSample{Label: "TypeScript", Language: "ts", Code: `const api = new Client({ baseURL, token: process.env.API_TOKEN });`},
			ui.CodeSample{Label: "curl", Language: "shell", Code: fmt.Sprintf(`curl -H "%s" %s/<entity>`, s.tokenHeaderLine("$API_TOKEN"), origin)},
		),
		para(text("Tokens carry scopes; a request outside the token's scopes is refused with 403. Rotate by minting a new token and deleting the old one.")),
	)
	return s.page("auth", "Authentication", body...)
}

type errorsScreen struct{ site *site }

func (sc *errorsScreen) ScreenTitle(ctx context.Context) string { return "API errors" }
func (sc *errorsScreen) ScreenDescription(ctx context.Context) string {
	return "Error envelope and status codes for the " + sc.site.cfg.AppName + " API"
}

func (sc *errorsScreen) Render() render.HTML {
	s := sc.site
	rows := []ui.Row{
		errRow("400", "Validation failed or the request was malformed."),
		errRow("401", "No session or bearer token."),
		errRow("403", "Authenticated but not permitted (RBAC / token scopes)."),
		errRow("404", "No such record or route."),
		errRow("409", "A unique value is taken, or the record is referenced by, or references, another record."),
		errRow("413", "Request body too large."),
		errRow("415", "A write whose body is not JSON (send Content-Type: application/json)."),
		errRow("429", "Rate limited: retry later."),
	}
	return s.page("errors", "Errors",
		heading(1, "Errors"),
		para(text("Every non-2xx JSON response uses one envelope: the CRUD routes, an endpoint the app declares beside them and that endpoint's gate (401 without a session, 403 for a cross-site form post) alike. "),
			code("error"), text(" is the message in the request's locale, "), code("reason"),
			text(" its stable code (branch on this, not the text), "), code("code"), text(" the HTTP status, and "),
			code("success"), text(" is always false. For field-level refusals, "), code("fields"),
			text(" and "), code("field_reasons"), text(" use response wire keys (camelCase by default, with the configured response casing and any explicit "),
			code("WireName"), text(" overrides applied). Each message in "), code("fields"),
			text(" has a matching code in "), code("field_reasons"), text(" in the same order. Both SDKs surface the envelope (Go: "),
			code("*APIError"), text("; JS: "), code("ApiError"), text(").")),
		ui.CodeBlock(ui.CodeBlockConfig{
			Language: "json",
			ShowCopy: true,
			Lines: ui.HighlightLines(`{
  "error": "validation failed",
  "reason": "validation_failed",
  "success": false,
  "code": 400,
  "fields": { "authorName": ["is required"] },
  "field_reasons": { "authorName": ["required"] }
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
		para(text("A path under the API prefix that matches no route answers 404 as an RFC 9457 problem document ("),
			code("application/problem+json"), text(" with "), code("title"), text(", "), code("detail"),
			text(" and "), code("reason"), text("), the standard format for a miss no route owns.")),
	)
}

func errRow(status, meaning string) ui.Row {
	return ui.Row{Cells: map[string]render.HTML{
		"status":  code(status),
		"meaning": text(meaning),
	}}
}

type entityScreen struct {
	site   *site
	params map[string]string
}

func (sc *entityScreen) SetParams(params map[string]string) { sc.params = params }

func (sc *entityScreen) StaticPaths(_ context.Context) []map[string]string {
	var out []map[string]string
	for _, e := range sc.site.firstIncludedEntityPerTable() {
		out = append(out, map[string]string{"name": e.Config.Table})
	}
	return out
}

func (sc *entityScreen) ScreenTitle(ctx context.Context) string {
	if e, ok := sc.current(); ok {
		return e.Config.Name + ": API reference"
	}
	return "API reference"
}

func (sc *entityScreen) ScreenDescription(ctx context.Context) string {
	return "Entity API reference for " + sc.site.cfg.AppName
}

func (sc *entityScreen) current() (*entity.Entity, bool) {
	return sc.site.lookup(sc.params["name"])
}

func (sc *entityScreen) Render() render.HTML {
	s := sc.site
	versions := s.lookupAll(sc.params["name"])
	if len(versions) == 0 {
		return s.page("", "Not found", heading(1, "Not found"))
	}
	primary := versions[0]

	body := []render.HTML{heading(1, primary.Config.Name)}
	for _, e := range versions {
		h, suffix := 2, ""
		if len(versions) > 1 {
			label := e.Version
			if label == "" {
				label = "default"
			}
			h, suffix = 3, versionSlug(e)
			body = append(body, html.Heading(html.HeadingConfig{Level: 2, ID: "heading-api-version-" + suffix}, render.Text("API version "+label)))
		}
		body = append(body, sc.entitySection(e, h, suffix)...)
	}
	return s.page("entity:"+primary.Config.Table, primary.Config.Name, body...)
}

func versionSlug(e *entity.Entity) string {
	if e.Version == "" {
		return "default"
	}
	var b strings.Builder
	b.WriteByte('v')
	for i := 0; i < len(e.Version); i++ {
		switch c := e.Version[i]; {
		case c == '/':
			b.WriteByte('-')
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "_%02x", c)
		}
	}
	return b.String()
}

func sectionID(id, suffix string) string {
	if suffix == "" {
		return ""
	}
	return id + "-" + suffix
}

func sectionHeading(level int, id, suffix, t string) render.HTML {
	return html.Heading(html.HeadingConfig{Level: level, ID: sectionID("heading-"+id, suffix)}, render.Text(t))
}

func (sc *entityScreen) entitySection(e *entity.Entity, h int, suffix string) []render.HTML {
	s := sc.site
	cfg := e.Config
	return []render.HTML{
		para(text("Base path "), code(s.entityBase(e)), text(". Responses are "),
			text(map[bool]string{true: "snake_case", false: "camelCase"}[s.cfg.SnakeCase]),
			text("; filter and sort query parameters always use the snake_case column names shown below.")),
		sectionHeading(h, "fields", suffix, "Fields"),
		sc.fieldsTable(cfg, sectionID("table-"+cfg.Table+"-fields", suffix)),
		sectionHeading(h, "endpoints", suffix, "Endpoints"),
		sc.endpointsTable(e, sectionID("table-"+cfg.Table+"-endpoints", suffix)),
		sectionHeading(h, "listing", suffix, "Listing, filtering, sorting"),
		sc.listParamsNotes(cfg),
		sectionHeading(h, "examples", suffix, "Examples"),
		sc.exampleTabs(e),
	}
}

func (sc *entityScreen) fieldsTable(cfg entity.EntityConfig, id string) render.HTML {
	var rows []ui.Row
	for _, f := range cfg.Fields {
		if f.Hidden {
			continue
		}
		var notes []string
		if f.NoQuery {
			notes = append(notes, "not filterable/sortable")
		}
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
			"type":     text(schema.FieldTypeLabel(f.Type, true)),
			"required": text(required),
			"notes":    text(strings.Join(notes, "; ")),
		}})
	}
	return ui.DataTable(ui.DataTableConfig{
		ID:      id,
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

func (sc *entityScreen) endpointsTable(e *entity.Entity, id string) render.HTML {
	cfg := e.Config
	base := sc.site.entityBase(e)
	keys := e.KeyColumns()
	record := base + crud.KeyPath(keys)
	type op struct{ method, path, desc string }
	ops := []op{
		{"GET", base, "List (offset or cursor pagination)"},
		{"GET", record, "Get one record"},
	}
	readOnly := sc.site.readOnly(e)
	if !readOnly {
		ops = append(ops,
			op{"POST", base, "Create"},
			op{"PUT", record, "Full update"},
			op{"PATCH", record, "Partial update: only keys present in the body change"},
			op{"DELETE", record, "Delete"},
			op{"POST", base + "/_batch", "Atomic batch create ({\"items\": […]})"},
			op{"PATCH", base + "/_batch", "Atomic batch update (items carry " + strings.Join(keys, " and ") + ")"},
			op{"DELETE", base + "/_batch", "Atomic batch delete ({\"ids\": […]})"},
		)
	}
	ops = append(ops, op{"GET", base + "/_events", "Live SSE feed: entity.created / updated / deleted"})
	if !readOnly {
		for _, t := range crud.RoutableTransitions(cfg.States) {
			desc := "State move " + t.Key + ": " + cfg.States.Field + " " + strings.Join(t.From, "|") + " → " + t.To
			if t.Stamp != "" {
				desc += ", stamps " + t.Stamp
			}
			ops = append(ops, op{"POST", record + "/transitions/" + t.Key, desc})
		}
	}
	for _, ep := range cfg.Endpoints {
		method := openapi.EntityEndpointMethod(ep.Method)
		if ep.Handler == nil || method == "" {
			continue
		}
		path := openapi.EntityEndpointRoutePath(e, ep.Path, sc.site.cfg.APIPrefix)
		desc := ep.Description
		if desc == "" {
			desc = "Custom endpoint" + map[bool]string{true: " (" + ep.Name + ")", false: ""}[ep.Name != ""]
		}
		if ep.SDK && ep.Name != "" {
			desc += ": Go c." + upperFirst(casing.ToCamel(ep.Name)) + ", TypeScript api." + casing.ToCamel(cfg.Table) + "." + casing.ToCamel(ep.Name)
		} else {
			desc += ": use the SDK's Do escape hatch"
		}
		ops = append(ops, op{method, path, desc})
	}
	var rows []ui.Row
	for _, o := range ops {
		rows = append(rows, ui.Row{Cells: map[string]render.HTML{
			"method": ui.Tag(ui.TagConfig{Label: o.method, Variant: methodVariant(o.method)}),
			"path":   code(o.path),

			"desc": text(o.desc),
		}})
	}
	return ui.DataTable(ui.DataTableConfig{
		ID:      id,
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
		para(code("?page=2&limit=25"), text(": offset pagination; "), code("?cursor=&limit=25"), text(": keyset pagination (pass the returned cursor to continue).")),
		para(code("?sort=-<column>"), text(": replace "), code("<column>"), text(" with a filterable/sortable column shown in the fields table; leading "), code("-"), text(" means descending. Use snake_case column names.")),
		para(code("?<column>=x"), text(": equality filter; suffix operators: "), code("_ne _gt _gte _lt _lte _like _in"), text(".")),
		para(code("?include=relation"), text(": eager-load declared relations; "), code("?fields=col1,col2"), text(": project columns.")),
	}
	if len(cfg.SearchFields) > 0 {
		items = append(items, para(code("?q=term"), text(": free-text search over: "+strings.Join(cfg.SearchFields, ", ")+".")))
	}
	if cfg.Scope.SoftDelete {
		items = append(items, para(code("?trashed=true"), text(": include soft-deleted rows (signed-in callers only).")))
	}
	return render.Join(items...)
}

func (sc *entityScreen) exampleTabs(e *entity.Entity) render.HTML {
	s := sc.site
	cfg := e.Config
	origin := s.entityOrigin(e)
	structName := upperFirst(casing.ToCamel(cfg.Name))
	prop := casing.ToCamel(cfg.Table)
	tabsName := "sdk-ent-" + cfg.Table
	if e.Version != "" {
		tabsName += "-" + versionSlug(e)
	}

	goCreate, tsCreate := "", ""
	if !s.readOnly(e) {
		goCreate = fmt.Sprintf("created, err := c.Create%s(ctx, client.%sInput{ /* … */ })\n", structName, structName)
		tsCreate = fmt.Sprintf("const created = await api.%s.create({ /* … */ });\n", prop)
	}
	goParams, goArg, tsArgs, query := "", "nil", "", ""
	sample := exampleFilter(cfg)
	jsValue, err := json.Marshal(sample.value)
	if sample.field != "" && err == nil {
		speller := filter.SpellerFor(cfg.Fields)
		filterKey := speller.Key(sample.field, sample.op)
		if slices.Contains(filter.ListControlParams(), filterKey) {
			filterKey = filter.BracketKey(sample.field, sample.op)
		}
		raw := fmt.Sprint(sample.value)
		params := url.Values{filterKey: {raw}}
		goParams = fmt.Sprintf("params := url.Values{}\nparams.Set(%q, %q)\n", filterKey, raw)
		goArg = "params"
		tsSort := ""
		if !speller.Named("sort") {
			sortBy := "-" + sample.field
			params.Set("sort", sortBy)
			goParams += fmt.Sprintf("params.Set(\"sort\", %q)\n", sortBy)
			tsSort = fmt.Sprintf("  sort: %q,\n", sortBy)
		}
		tsArgs = fmt.Sprintf("{\n%s  filters: { %q: %s },\n}", tsSort, filterKey, jsValue)
		query = "?" + params.Encode()
	}
	goCode := fmt.Sprintf(`%spage, err := c.List%s(ctx, %s)

%s_ = c.Watch%s(ctx, func(event string, data []byte) error {
	fmt.Println(event)
	return nil
})`, goParams, structName, goArg, goCreate, structName)

	tsCode := fmt.Sprintf(`const page = await api.%s.list(%s);

%sawait api.%s.watch((event) => console.log(event), { signal });`,
		prop, tsArgs, tsCreate, prop)

	curlCode := fmt.Sprintf(`curl -H "%s" \
  "%s/%s%s"`, s.tokenHeaderLine("$API_TOKEN"), origin, cfg.Table, query)

	if s.openAPIManifest() {
		// The downloads are the OpenAPI document's SDKs, which have no
		// entity methods: plain HTTP is the example that holds.
		return ui.CodeTabs(ui.CodeTabsConfig{Name: tabsName},
			ui.CodeSample{Label: "curl", Language: "shell", Code: curlCode},
		)
	}
	return ui.CodeTabs(ui.CodeTabsConfig{Name: tabsName},
		ui.CodeSample{Label: "Go", Language: "go", Code: goCode},
		ui.CodeSample{Label: "TypeScript", Language: "ts", Code: tsCode},
		ui.CodeSample{Label: "curl", Language: "shell", Code: curlCode},
	)
}

type filterExample struct {
	field string
	op    filter.FilterOp
	value any
}

func exampleFilter(cfg entity.EntityConfig) filterExample {
	key := "id"
	if len(cfg.PrimaryKey) > 0 {
		key = cfg.PrimaryKey[0]
	}
	var firstEqual, firstRange, createdAt, primary filterExample
	for _, f := range cfg.Fields {
		if f.Hidden || f.NoQuery || !filter.OpSuitsType(filter.OpEq, f.Type) {
			continue
		}
		value := exampleFilterValue(f)
		if value == nil {
			continue
		}
		sample := filterExample{f.Name, filter.OpEq, value}
		if f.Name == "created_at" {
			createdAt = sample
		}
		if f.Name == key {
			primary = sample
		}
		if f.AutoGenerate != schema.AutoNone {
			continue
		}
		if filter.OpSuitsType(filter.OpGte, f.Type) {
			sample.op = filter.OpGte
			switch f.Type {
			case schema.Int, schema.Float, schema.Decimal, schema.Timestamp, schema.Date:
				return sample
			default:
				if firstRange.field == "" {
					firstRange = sample
				}
			}
		} else if firstEqual.field == "" {
			firstEqual = sample
		}
	}
	for _, sample := range []filterExample{firstRange, firstEqual, createdAt, primary} {
		if sample.field != "" {
			return sample
		}
	}
	return filterExample{}
}

func exampleFilterValue(f schema.Field) any {
	switch f.Type {
	case schema.String, schema.Text, schema.Image, schema.File:
		return "example"
	case schema.Int, schema.Float, schema.Decimal:
		return 10
	case schema.Bool:
		return true
	case schema.Enum:
		for _, value := range f.Values {
			if _, err := filter.CoerceValue(f.Name, f.Type, value); err == nil {
				return value
			}
		}
		return nil
	case schema.UUID:
		return "00000000-0000-4000-8000-000000000001"
	case schema.Timestamp:
		return "2026-01-01T00:00:00Z"
	case schema.Date:
		return "2026-01-01"
	default:
		return nil
	}
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// openAPIManifest reports downloads generated from an OpenAPI document.
func (s *site) openAPIManifest() bool {
	m, _, _ := s.resolved()
	return m != nil && m.Source == sdk.SourceOpenAPI
}

// tokenHeaderLine is the header the SDKs send token in: the scheme an
// OpenAPI manifest records, else a bearer token. The recorded name and
// prefix are held to the bytes inert in the double-quoted shell snippets
// they land in.
func (s *site) tokenHeaderLine(token string) string {
	header, prefix := "Authorization", "Bearer "
	if m, _, _ := s.resolved(); m != nil && m.Source == sdk.SourceOpenAPI && m.TokenHeader != "" {
		header, prefix = fenceWord(strings.ReplaceAll(m.TokenHeader, "/", "")), ""
		if p := strings.TrimSpace(fenceWord(strings.ReplaceAll(m.TokenPrefix, "/", ""))); p != "" {
			prefix = p + " "
		}
	}
	return header + ": " + prefix + token
}
