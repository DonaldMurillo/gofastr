// =============================================================================
// docs_catalog.go — the docs section's single source of truth.
//
// Every entry maps a curated card (title + one-line description) to a REAL
// embedded framework doc (framework/docs/content/<slug>.md). The index page
// (/docs/) renders the catalog as a grid of links; each link resolves to
// /docs/<slug>, a DocPageScreen that renders the embedded markdown through
// framework/ui.Markdown. There are no dead cards and no 404 doc pages — the
// catalog and the registered routes are generated from the same slice.
//
// To add a doc: drop the .md under framework/docs/content/, add one line
// here, and registerScreens picks it up automatically.
// =============================================================================

package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/docs"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// docEntry is one card / one page. Slug is the embedded doc name (the
// filename without .md) and doubles as the URL segment: /docs/<slug>.
type docEntry struct {
	Slug  string
	Title string
	Desc  string
}

// docIntent groups entries under one of the six reading intents.
type docIntent struct {
	Num   string
	Slug  string
	Title string
	Lede  string
	Path  []string // recommended reading order (titles within this intent)
	Docs  []docEntry
}

// docIntents is the curated catalog. Each Slug below corresponds to a file
// in framework/docs/content/ — verified at startup by registerDocPages.
var docIntents = []docIntent{
	{
		Num: "01", Slug: "modeling", Title: "Modeling your domain",
		Lede: "Declare entities, fields, relations. The framework generates schema, CRUD, validators, and code.",
		Path: []string{"Entity declarations", "Filter DSL", "Cursor pagination"},
		Docs: []docEntry{
			{"entity-declarations", "Entity declarations", "JSON or Go — both produce the same tables, routes, and tools."},
			{"query-dsl", "Filter DSL", "?status=published&views_gte=10&sort=-created_at parses to a typed Where."},
			{"cursor-pagination", "Cursor pagination", "Keyset by EntityConfig.CursorField. Opt in by sending ?cursor=."},
			{"includes", "Eager loading", "?include=author.profile flattens the N+1."},
			{"hooks-and-transactions", "Hooks & transactions", "BeforeCreate / AfterUpdate hooks share the parent tx."},
			{"migrations", "Migrations", "Versioned, ordered, reversible — versus the auto-migrate dev mode."},
			{"multi-tenant", "Multi-tenant scope", "tenant_id column + automatic filter from request context."},
			{"codegen", "Code generation", "What lands on disk under .gofastr/ — and how to read it."},
		},
	},
	{
		Num: "02", Slug: "serving", Title: "Serving HTTP",
		Lede: "Routes, middleware, sessions, auth, idempotency, security headers — everything between the wire and your handler.",
		Path: []string{"Auth", "Access control", "Security defaults"},
		Docs: []docEntry{
			{"access-control", "Access control", "RolePolicy, RequirePermission, custom Policy implementations."},
			{"auth", "Auth", "Login, OAuth, magic-link, 2FA, password reset — each a plugin."},
			{"idempotency", "Idempotency", "An Idempotency-Key header replays mutations safely."},
			{"security", "Security defaults", "CSP, CSRF, rate limit, headers — all on by default."},
			{"health-checks", "Health checks", "/healthz + /readyz with plugin checks."},
			{"webhooks", "Webhooks", "Signed outbound delivery with retry-with-backoff."},
			{"notifications", "Notifications", "Multi-channel fan-out with per-channel templates."},
			{"batch-endpoints", "Batch endpoints", "Create / update / delete many rows in one request."},
			{"plugins", "Plugins", "The lifecycle every battery and feature plugs into."},
		},
	},
	{
		Num: "03", Slug: "ui", Title: "Building UI",
		Lede: "Server-rendered with islands. Signals, HTML primitives, composed patterns, the runtime, and theming.",
		Path: []string{"Getting started (UI)", "New components", "Widget builder"},
		Docs: []docEntry{
			{"ui-getting-started", "Getting started (UI)", "The path: scaffold → theme → screen → custom component."},
			{"ui-new-components", "New components", "The minimal-register + SSR-inline + hydrate contract."},
			{"widgets", "Widget builder", "Build islands that hydrate against a registered handler."},
			{"form-module", "Forms", "Server-validated forms with island-swapped error states."},
			{"image", "Image pipeline", "Pure-Go resize + WebP lossless encode."},
			{"runtime-minification", "Runtime modules", "Carved per-feature so pages without X don't ship X's JS."},
			{"print", "Print documents", "Server-rendered print-friendly documents + PDF."},
			{"dev-livereload", "Dev livereload", "SSE-driven reload while you edit — zero config."},
		},
	},
	{
		Num: "04", Slug: "persist", Title: "Persisting & migrating",
		Lede: "SQLite and Postgres, dialect-aware, with the migration CLI and per-test isolation.",
		Path: []string{"Audit log", "Isolation", "Factories"},
		Docs: []docEntry{
			{"audit-log", "Audit log", "WithAuditLog writes a row for every Create/Update/Delete."},
			{"isolation", "Isolation", "Linked git worktrees get isolated local DBs."},
			{"factories", "Factories", "Rails-style fixtures for tests."},
			{"search", "Full-text search", "Find records containing a term, dialect-aware."},
			{"uploads", "Uploads", "File + image fields with pluggable storage backends."},
			{"dotenv", "Env / .env", "core/dotenv auto-loaded by NewApp."},
		},
	},
	{
		Num: "05", Slug: "agents", Title: "Working with agents",
		Lede: "MCP tools, Kiln build mode, agent permissions, plan-gated destructive ops.",
		Path: []string{"Kiln overview", "Embed", "Agent notes"},
		Docs: []docEntry{
			{"kiln", "Kiln overview", "The agent-driven build-mode binary."},
			{"embed", "Embed", "Local semantic search via brute-force cosine — no API key."},
			{"agent-notes", "Agent notes", "Append-only review log for agents working on the framework."},
			{"audit-deps", "Audit deps", "Detect packages an agent shouldn't import."},
			{"blueprints", "Blueprints", "Reusable bundles of entities + screens an agent can apply."},
		},
	},
	{
		Num: "06", Slug: "ops", Title: "Operations",
		Lede: "Run it in production. Logging, metrics, feature flags, env, i18n.",
		Path: []string{"Logging", "Feature flags", "i18n"},
		Docs: []docEntry{
			{"log", "Logging", "Structured JSON logs with MCP query tools."},
			{"feature-flags", "Feature flags", "Rollout percentage, allow lists, env evaluator."},
			{"i18n", "i18n", "JSON catalogs, plurals, Accept-Language negotiation."},
			{"cron", "Cron", "Scheduled jobs with retry + jitter."},
			{"events", "Events", "In-process pub/sub for decoupled side effects."},
			{"admin", "Admin UI", "An opt-in listing + form per entity."},
		},
	},
}

// docCount is the total number of individual doc pages in the catalog —
// used by the index header so the headline number can't drift from the
// content again.
func docCount() int {
	n := 0
	for _, it := range docIntents {
		n += len(it.Docs)
	}
	return n
}

// findIntent returns the intent (and entry) that owns a slug, plus the
// flat-ordered position for prev/next. ok is false for unknown slugs.
func findDocEntry(slug string) (docIntent, docEntry, bool) {
	for _, it := range docIntents {
		for _, d := range it.Docs {
			if d.Slug == slug {
				return it, d, true
			}
		}
	}
	return docIntent{}, docEntry{}, false
}

// flatDocs returns every entry in catalog order, for prev/next links.
func flatDocs() []docEntry {
	out := []docEntry{}
	for _, it := range docIntents {
		out = append(out, it.Docs...)
	}
	return out
}

// =============================================================================
// /docs/<slug> — a single doc page rendered from embedded markdown.
// =============================================================================

type DocPageScreen struct{ Entry docEntry }

func (s *DocPageScreen) ScreenTitle() string        { return s.Entry.Title }
func (s *DocPageScreen) ScreenDescription() string  { return s.Entry.Desc }
func (s *DocPageScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *DocPageScreen) Render() render.HTML {
	intent, _, _ := findDocEntry(s.Entry.Slug)

	var content render.HTML
	if body, err := docs.Get(s.Entry.Slug); err == nil {
		content = ui.Markdown(ui.MarkdownConfig{Source: string(body)})
	} else {
		content = render.Tag("p", map[string]string{"class": "doc-head__lede"},
			render.Text("This doc isn't available yet. Browse the embedded docs with "),
			codeText("gofastr docs"), render.Text(" or open the "),
			html.Link(html.LinkConfig{Href: "/docs/", Text: "docs index"}), render.Text("."))
	}

	article := render.Tag("article", map[string]string{"class": "doc-content"},
		render.Tag("nav", map[string]string{"class": "doc-crumbs", "aria-label": "Breadcrumb"},
			html.Link(html.LinkConfig{Href: "/docs/", Text: "Docs"}),
			html.Span(html.TextConfig{Class: "sep"}, render.Text("/")),
			html.Link(html.LinkConfig{Href: "/docs/#" + intent.Slug, Text: intent.Title}),
			html.Span(html.TextConfig{Class: "sep"}, render.Text("/")),
			html.Span(html.TextConfig{Class: "current"}, render.Text(s.Entry.Title)),
		),
		content,
		docPrevNext(s.Entry.Slug),
	)

	return render.Tag("div", map[string]string{"class": "doc-shell doc-shell--notoc"},
		docCatalogSidebar(s.Entry.Slug),
		article,
	)
}

// docCatalogSidebar renders the full catalog grouped by intent, with the
// current slug marked active. Replaces the old hardcoded sidebar whose
// links pointed at routes that never existed.
func docCatalogSidebar(active string) render.HTML {
	groups := []render.HTML{}
	for _, it := range docIntents {
		items := []render.HTML{}
		for _, d := range it.Docs {
			cls := ""
			if d.Slug == active {
				cls = "active"
			}
			items = append(items, html.ListItem(html.ListItemConfig{},
				html.Link(html.LinkConfig{Href: "/docs/" + d.Slug, Text: d.Title, Class: cls}),
			))
		}
		groups = append(groups, html.Div(html.DivConfig{Class: "docnav__group"},
			html.Div(html.DivConfig{Class: "label"},
				html.Span(html.TextConfig{Class: "n"}, render.Text(it.Num)),
				render.Text(it.Title),
			),
			html.UnorderedList(html.ListConfig{}, items...),
		))
	}
	return render.Tag("aside", map[string]string{"class": "docnav"}, groups...)
}

// docPrevNext links the previous and next doc in catalog order.
func docPrevNext(slug string) render.HTML {
	flat := flatDocs()
	idx := -1
	for i, d := range flat {
		if d.Slug == slug {
			idx = i
			break
		}
	}
	prevHref, prevText := "/docs/", "Docs index"
	if idx > 0 {
		prevHref, prevText = "/docs/"+flat[idx-1].Slug, flat[idx-1].Title
	}
	children := []render.HTML{
		html.LinkHTML(html.LinkHTMLConfig{
			Href:  prevHref,
			Class: "prev-card",
			Content: render.Join(
				html.Span(html.TextConfig{Class: "dir"}, render.Text("← Previous")),
				html.Span(html.TextConfig{Class: "ttl"}, render.Text(prevText)),
			),
		}),
	}
	if idx >= 0 && idx < len(flat)-1 {
		children = append(children, html.LinkHTML(html.LinkHTMLConfig{
			Href:  "/docs/" + flat[idx+1].Slug,
			Class: "next-card",
			Content: render.Join(
				html.Span(html.TextConfig{Class: "dir"}, render.Text("Next →")),
				html.Span(html.TextConfig{Class: "ttl"}, render.Text(flat[idx+1].Title)),
			),
		}))
	}
	return html.Div(html.DivConfig{Class: "doc-foot"},
		html.Div(html.DivConfig{Class: "doc-foot__nav"}, children...),
	)
}
