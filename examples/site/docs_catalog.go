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
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
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
		Num: "00", Slug: "start", Title: "Start here",
		Lede: "What GoFastr is, how a project is laid out, and a map of every feature — the newcomer narrative before the per-feature references.",
		Path: []string{"Overview", "Tutorial: blueprint app", "Project structure"},
		Docs: []docEntry{
			{"overview", "Overview", "What the framework is, the two layers, and a linked map of every capability."},
			{"cli", "The gofastr CLI", "Every subcommand mapped to its doc — init, dev, migrate, generate, audit, upgrade."},
			{"tutorial-blueprint-app", "Tutorial: blueprint app", "The optional blueprint scaffolder end to end: one gofastr.yml becomes a UI + API you own, in about twenty minutes."},
			{"project-structure", "Project structure", "Start flat; grow into internal/<domain> as real boundaries appear. Structure follows the app."},
			{"comparison", "Comparison", "Where GoFastr sits relative to other full-stack frameworks."},
			{"upgrading", "Upgrading", "Move an app (and the CLI) to a newer release — plus gofastr upgrade, the guided helper."},
			{"stability", "API stability", "Compatibility windows, deprecation rules, and the public v1 promise."},
		},
	},
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
			{"codegen", "Code generation", "What lands on disk under gen/ — and how to read it."},
			{"app-cli", "Ship your API as a CLI", "gofastr generate cli — a branded terminal client for your customers, with scoped API-token auth."},
			{"sdk", "Ship your API as SDKs", "gofastr generate sdk — a downloadable Go module + JS/TS client, hosted by the app behind a live docs site."},
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
			{"api-versioning", "API prefix & versioning", "Mount the API under a prefix and evolve it across versions."},
			{"cache", "Cache", "Memoize expensive reads with a pluggable backend and TTLs."},
			{"security", "Security defaults", "CSP, CSRF, rate limit, headers — all on by default."},
			{"health-checks", "Health checks", "/healthz + /readyz with plugin checks."},
			{"webhooks", "Webhooks", "Signed outbound delivery with retry-with-backoff."},
			{"notifications", "Notifications", "Multi-channel fan-out with per-channel templates."},
			{"batch-endpoints", "Batch endpoints", "Create / update / delete many rows in one request."},
			{"plugins", "Plugins", "The lifecycle every battery and feature plugs into."},
			{"modules", "Modules", "Runtime enable/disable for batteries with manifests."},
		},
	},
	{
		Num: "03", Slug: "ui", Title: "Building UI",
		Lede: "Start from a product job, then choose the state boundary, server-rendered islands, primitives, runtime, and theme.",
		Path: []string{"UI capability map", "Getting started (UI)", "Composition recipes"},
		Docs: []docEntry{
			{"ui-capability-map", "UI capability map", "Live dashboards, optimistic boards, master/detail, reactive state, static export, and SPA integration — mapped to proof and delivery semantics."},
			{"live-dashboards", "Live dashboards", "Compose SSE island push, store.Computed, a bounded feed, and the connection-health banner — and the boundaries that decide when each is the right shape."},
			{"ui-getting-started", "Getting started (UI)", "The path: scaffold → theme → screen → custom component."},
			{"ui-composition-recipes", "Composition recipes", "Choose a page shape before composing framework-owned primitives."},
			{"ui-wiring", "Wiring UI into an app", "framework.App + core-ui app + uihost, end to end in one annotated main.go."},
			{"ui-new-components", "New components", "The minimal-register + SSR-inline + hydrate contract."},
			{"theming", "Theming", "The token catalog, dark mode, ui.Themed, and the --ui-* override vars."},
			{"widgets", "Widget builder", "Build islands that hydrate against a registered handler."},
			{"form-module", "Forms", "Server-validated forms with island-swapped error states."},
			{"image", "Image pipeline", "Pure-Go resize + WebP lossless encode."},
			{"runtime-minification", "Runtime modules", "Carved per-feature so pages without X don't ship X's JS."},
			{"print", "Print documents", "Server-rendered print-friendly documents + PDF."},
			{"dev-livereload", "Dev livereload", "SSE-driven reload while you edit — zero config."},
			{"static-export", "Static-site export", "Render the whole app to static HTML + assets for apex or project-path hosting."},
			{"pwa", "PWA", "uihost.WithPWA: installable manifest, versioned offline shell, and a safe service worker."},
			{"seo", "SEO", "Meta tags, Open Graph, JSON-LD, sitemap, robots, and the one-image icon surface."},
			{"accessibility", "Accessibility", "Built-in ARIA guarantees, the guided audit command, and the build gate."},
			{"strict-mode", "Strict mode", "WithStrict: missing SEO and missing per-screen axe tests fail boot instead of shipping."},
			{"reactivity", "Reactivity model", "The four-rung ladder — client signals, RPC, polling, SSE push — and the stateless-interactive-layer contract."},
			{"interactive-patterns", "Interactive patterns", "The data-fui-* vocabulary: RPC islands, signals, open-widget, optimistic actions, polling."},
			{"optimistic-ui", "Optimistic UI", "The mutation lifecycle, rollback vs authoritative refresh, and seven composed recipes — toggle, inline edit, create, delete, kanban, group mutex, and slow/failure."},
			{"pane-host", "Pane host", "Master-detail split-pane layout that collapses to an overlay drawer on narrow screens."},
			{"plugin-platform", "Plugin platform", "Host third-party JS plugins in a sandboxed opaque-origin iframe with a capability-gated postMessage protocol."},
			{"process-modules", "Process modules", "Third-party modules as isolated child processes: crash/upgrade/revoke without touching the host, capability-brokered data, sandbox trust tiers."},
			{"runtime-contract", "Runtime contract", "The SSR/hydration/island/SSE model and the full data-fui-* attribute reference."},
			{"signal-store", "Signal store", "Typed, namespaced client state that fans out to many consumers from one declaration."},
			{"compute", "Background compute", "Registered Web Workers + WASM modules, content-addressed and CSP-safe, off the main thread."},
		},
	},
	{
		Num: "04", Slug: "persist", Title: "Persisting & migrating",
		Lede: "SQLite and Postgres, dialect-aware, with the migration CLI and per-test isolation.",
		Path: []string{"Audit log", "Isolation", "Factories"},
		Docs: []docEntry{
			{"audit-log", "Audit log", "WithAuditLog writes a row for every Create/Update/Delete."},
			{"data-export", "Data export & import", "Dump every entity's rows to a portable archive and restore it with validation."},
			{"presence", "Presence", "Live 'who's here' rosters over the SSE lane — server-derived identity, single-replica."},
			{"isolation", "Isolation", "Linked git worktrees get isolated local DBs."},
			{"testkit", "Testkit", "Public helpers for host apps that need an isolated Postgres in integration tests."},
			{"factories", "Factories", "Rails-style fixtures for tests."},
			{"search", "Full-text search", "Find records containing a term, dialect-aware."},
			{"uploads", "Uploads", "File + image fields with pluggable storage backends."},
			{"dotenv", "Env / .env", "core/dotenv auto-loaded by NewApp."},
		},
	},
	{
		Num: "05", Slug: "agents", Title: "Working with agents",
		Lede: "MCP tools, Kiln build mode (experimental), agent permissions, plan-gated destructive ops.",
		Path: []string{"Agent-readiness", "Kiln overview", "Embed", "Agent notes"},
		Docs: []docEntry{
			{"agent-ready", "Agent-readiness", "Discovery surface scanners (isitagentready.com) look for: llms.txt, A2A card, MCP/OAuth well-knowns, markdown negotiation."},
			{"kiln", "Kiln overview", "Experimental — the agent-driven build-mode binary."},
			{"embed", "Embed", "Local semantic search via brute-force cosine — no API key."},
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
			{"observability", "Observability", "Metrics and tracing for the hot paths."},
			{"deploy", "Deployment", "Ship it: build, configure, and run GoFastr in production."},
			{"first-run", "First-run setup", "A setup wizard (or headless env bootstrap) gates the app until first-boot state exists."},
			{"scaling", "Horizontal scaling", "What's process-local by default and the replica-safe alternative for each."},
			{"queue", "Job queue", "Durable background jobs via battery/queue."},
			{"feature-flags", "Feature flags", "Rollout percentage, allow lists, env evaluator."},
			{"i18n", "i18n", "JSON catalogs, plurals, Accept-Language negotiation."},
			{"cron", "Cron", "Scheduled jobs with retry + jitter."},
			{"events", "Events", "In-process pub/sub for decoupled side effects."},
			{"admin", "Admin UI", "An opt-in listing + form per entity."},
		},
	},
	{
		Num: "07", Slug: "reference", Title: "Reference & internals",
		Lede: "Performance numbers and the deeper design + tooling docs — surfaced so nothing is hidden.",
		Path: []string{"Benchmarks", "Performance results"},
		Docs: []docEntry{
			{"benchmarks", "Benchmarks", "What's measured, how, and the methodology behind the numbers."},
			{"perf-results", "Performance results", "Latest throughput / latency results across the hot paths."},
			{"harness-architecture", "Harness architecture", "The AI coding-harness design (contributor/internal reference)."},
			{"harness-e2e-testing", "Harness E2E testing", "How the harness drives end-to-end browser tests."},
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

// allDocsSection renders the flat A–Z reference: every embedded doc, sorted by
// name (docs.List already sorts), each linked to /docs/<slug>. This is the
// "nothing is hidden" index — it lists docs whether or not they're featured in
// one of the reading intents above. README (the docs folder's own index) is
// skipped since it isn't a page. Used at the bottom of /docs/.
func allDocsSection() render.HTML {
	topics, err := docs.List()
	if err != nil {
		return render.HTML("")
	}
	cards := make([]render.HTML, 0, len(topics))
	for _, t := range topics {
		if t.Name == "README" {
			continue
		}
		cards = append(cards, html.LinkHTML(html.LinkHTMLConfig{
			Href:  "/docs/" + t.Name,
			Class: "doc",
			Content: render.Join(
				html.Div(html.DivConfig{Class: "doc__title"}, render.Text(t.Title)),
				html.Div(html.DivConfig{Class: "doc__meta"}, render.Text("/docs/"+t.Name)),
			),
		}))
	}
	return html.Section(html.SectionConfig{ID: "all-az", Class: "intent", Label: "All docs A–Z"},
		html.Div(html.DivConfig{Class: "intent__head"},
			html.Span(html.TextConfig{Class: "intent__num"}, render.Text("∑")),
			html.Heading(html.HeadingConfig{Level: 2, Class: "intent__title"}, render.Text("Every doc · A–Z")),
			html.Span(html.TextConfig{Class: "intent__meta"}, render.Text(itoa(len(cards))+" docs")),
		),
		html.Paragraph(html.TextConfig{Class: "intent__lede"},
			render.Text("The complete embedded reference: every page, alphabetical, featured or not. It is the same content as `gofastr docs`.")),
		html.Div(html.DivConfig{Class: "docs"}, cards...),
	)
}

// =============================================================================
// /docs/<slug> — a single doc page rendered from embedded markdown.
// =============================================================================

type DocPageScreen struct{ Entry docEntry }

func (s *DocPageScreen) ScreenTitle() string        { return s.Entry.Title }
func (s *DocPageScreen) ScreenDescription() string  { return s.Entry.Desc }
func (s *DocPageScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// SetParams resolves the doc entry from the catch-all remainder
// (/docs/{path...}). A known slug loads its catalog entry; an unknown
// slug leaves a placeholder so Load can reject it.
func (s *DocPageScreen) SetParams(p map[string]string) {
	slug := p["path"]
	if _, entry, ok := findDocEntry(slug); ok {
		s.Entry = entry
	} else {
		s.Entry = docEntry{Slug: slug}
	}
}

// Load rejects unknown doc slugs so handlePage serves the site's 404
// (NotFoundScreen) — the same UX the per-slug loop produced by simply
// not registering unknown paths.
func (s *DocPageScreen) Load(ctx context.Context) error {
	if _, _, ok := findDocEntry(s.Entry.Slug); !ok {
		return fmt.Errorf("docs: no catalog entry for %q", s.Entry.Slug)
	}
	return nil
}

// StaticPaths enumerates every catalog doc so static export, sitemap,
// llm.md, and the strict coverage gate keep producing one page per doc
// — the same URL set the per-slug registration loop emitted.
func (s *DocPageScreen) StaticPaths(ctx context.Context) []map[string]string {
	out := make([]map[string]string, 0, len(flatDocs()))
	for _, e := range flatDocs() {
		out = append(out, map[string]string{"path": e.Slug})
	}
	return out
}

func (s *DocPageScreen) Render() render.HTML {
	intent, _, _ := findDocEntry(s.Entry.Slug)

	var content render.HTML
	if body, err := docs.Get(s.Entry.Slug); err == nil {
		content = ui.Markdown(ui.MarkdownConfig{Source: string(body)})
	} else {
		content = html.Paragraph(html.TextConfig{Class: "doc-head__lede"},
			render.Text("This doc isn't available yet. Browse the embedded docs with "),
			codeText("gofastr docs"), render.Text(" or open the "),
			html.Link(html.LinkConfig{Href: "/docs/", Text: "docs index"}), render.Text("."))
	}

	return ui.DocLayout(ui.DocLayoutConfig{
		Nav: docCatalogSidebar(s.Entry.Slug),
		Crumbs: []ui.DocCrumb{
			{Label: "Docs", Href: "/docs/"},
			{Label: intent.Title, Href: "/docs/#" + intent.Slug},
			{Label: s.Entry.Title},
		},
		Pager: docPrevNext(s.Entry.Slug),
	}, content)
}

// docCatalogSidebar renders the grouped doc rail for the current slug.
func docCatalogSidebar(active string) render.HTML {
	return interactive.SectionMenu(docsSectionMenuConfig(active))
}

// docsSectionMenuConfig is the single source of truth for the docs nav —
// shared by the per-page inline rail (with the active slug) and the mounted
// mobile drawer (active="" — the runtime stamps aria-current client-side).
func docsSectionMenuConfig(active string) interactive.SectionMenuConfig {
	groups := make([]interactive.SectionGroup, 0, len(docIntents))
	for _, it := range docIntents {
		items := make([]interactive.SectionItem, 0, len(it.Docs))
		for _, d := range it.Docs {
			items = append(items, interactive.SectionItem{
				Label:  d.Title,
				Href:   "/docs/" + d.Slug,
				Active: d.Slug == active,
			})
		}
		groups = append(groups, interactive.SectionGroup{Eyebrow: it.Num, Label: it.Title, Items: items})
	}
	return interactive.SectionMenuConfig{
		AriaLabel:    "Documentation sections",
		TriggerLabel: "Sections",
		DrawerName:   "docs-section-menu",
		Lead:         &interactive.SectionItem{Label: "Docs index", Href: "/docs/", Active: active == ""},
		Groups:       groups,
	}
}

// docPrevNext computes the previous/next doc in catalog order for the pager.
func docPrevNext(slug string) *ui.DocPager {
	flat := flatDocs()
	idx := -1
	for i, d := range flat {
		if d.Slug == slug {
			idx = i
			break
		}
	}
	p := ui.DocPager{PrevHref: "/docs/", PrevLabel: "Docs index"}
	if idx > 0 {
		p.PrevHref, p.PrevLabel = "/docs/"+flat[idx-1].Slug, flat[idx-1].Title
	}
	if idx >= 0 && idx < len(flat)-1 {
		p.NextHref, p.NextLabel = "/docs/"+flat[idx+1].Slug, flat[idx+1].Title
	}
	return &p
}
