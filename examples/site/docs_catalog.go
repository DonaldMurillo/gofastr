// =============================================================================
// docs_catalog.go, the docs section's single source of truth.
//
// Every entry maps a curated card (title + one-line description) to a REAL
// embedded framework doc (framework/docs/content/<slug>.md). The index page
// (/docs/) renders the catalog as a grid of links; each link resolves to
// /docs/<slug>, a DocPageScreen that renders the embedded markdown through
// framework/ui.Markdown. There are no dead cards and no 404 doc pages, the
// catalog and the registered routes are generated from the same slice.
// Both routes live in one screen group on the docs layer (registerDocsGroup,
// docsLayout in layout.go).
//
// To add a doc: drop the .md under framework/docs/content/, add one line
// here, and registerDocsGroup picks it up automatically.
// =============================================================================

package main

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/examples/site/docpage"
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

// docIntent groups entries under one of the reading intents.
type docIntent struct {
	Num   string
	Slug  string
	Title string
	Lede  string
	Path  []string // recommended reading order (titles within this intent)
	Docs  []docEntry
}

// docIntents is the curated catalog. Each Slug below corresponds to a file
// in framework/docs/content/, verified at startup by registerDocPages.
var docIntents = []docIntent{
	{
		Num: "00", Slug: "start", Title: "Start here",
		Lede: "What GoFastr is, how a project is laid out, and the fastest maps into every feature.",
		Path: []string{"Overview", "Getting started (UI)", "Project structure"},
		Docs: []docEntry{
			{"overview", "Overview", "How the layers fit together, from barebones core/ to a full-stack app."},
			{"ui-getting-started", "Getting started (UI)", "Scaffold an app, run gofastr dev, customize the theme, and build screens."},
			{"tutorial-app-spec", "Tutorial: app spec app", "Optional one-file scaffolder: gofastr.yml to plain Go you own."},
			{"project-structure", "Project structure", "Start flat; extract domain packages only when real boundaries appear."},
			{"cli", "The gofastr CLI", "Install, scaffold, dev loop, verification, migrations, deploy, and releases."},
			{"backend-capability-map", "Backend capability map", "Pick a backend job and jump straight to the package, check, and doc."},
			{"ui-capability-map", "UI capability map", "Pick a UI shape and see state ownership, delivery tier, and live examples."},
			{"comparison", "Comparison", "Trade-offs vs PocketBase, Encore, Wasp, and hand-rolled Go."},
			{"stability", "API stability", "Pre-v1 breaking-change policy, deprecation rules, and the v1 promise."},
			{"upgrading", "Upgrading", "Move your app and CLI to a newer release with gofastr upgrade."},
		},
	},
	{
		Num: "01", Slug: "ui", Title: "Building UI",
		Lede: "Server-rendered screens in Go with interactive islands, themes, and zero JS build pipeline.",
		Path: []string{"Reactivity model", "Composition recipes", "Theming"},
		Docs: []docEntry{
			{"reactivity", "Reactivity model", "The four-rung ladder: client signals, RPC, polling, and SSE push."},
			{"ui-composition-recipes", "Composition recipes", "Real desktop and mobile page layouts built from framework/ui primitives."},
			{"ui-wiring", "Wiring UI into an app", "Mount core-ui and uihost onto a plain framework.App by hand."},
			{"layouts", "Screens and layouts", "Shared shells, nested screen groups, and partial navigation swaps."},
			{"ui-new-components", "UI components", "Catalog of built-in framework/ui components and live gallery links."},
			{"ui-headless", "Headless components", "Unstyled structure, roles, and behavior hooks for custom design systems."},
			{"theming", "Theming", "Light/dark palettes, semantic tokens, and owning theme/theme.go."},
			{"form-module", "Forms", "Server-validated forms, field controls, conditional sections, and wizards."},
			{"widgets", "Widgets", "Modals, drawers, popovers, and toasts via core-ui/widget."},
			{"interactive-patterns", "Interactive patterns", "data-cui-* behaviors and hand-written server islands."},
			{"optimistic-ui", "Optimistic UI", "Immediate client projection, rollback, and conflict reconciliation."},
			{"signal-store", "Signal store", "Typed, namespaced client state shared across components with SSR seeding."},
			{"live-dashboards", "Live dashboards", "Keeping dashboards fresh with polling, signals, and SSE push."},
			{"pane-host", "Pane host", "Master-detail split-pane workspace that adapts to mobile drawers."},
			{"presence", "Presence", "Live 'who's here' rosters over SSE, with cross-replica aggregation via WithFanout."},
			{"localdb", "Browser-local databases", "IndexedDB stores declared in Go, kept in the visitor's browser."},
			{"local-entities", "Local entities", "Forms and lists over browser-local IndexedDB stores with zero server table."},
			{"seo", "SEO", "Meta tags, Open Graph, JSON-LD, sitemaps, robots, and app icons."},
			{"pwa", "PWA", "Installable web manifest, versioned offline shell, and service worker."},
			{"accessibility", "Accessibility", "Built-in ARIA guarantees, live axe audits, and static a11y checks."},
			{"static-export", "Static-site export", "Render every route in-process to static HTML and assets."},
			{"site-search", "Site search", "Full-text search over rendered pages, live or in static exports."},
			{"image", "Image pipeline", "Pure-Go image resizing, variants, WebP, and BlurHash."},
			{"print", "Print documents", "Print-friendly HTML documents and optional Chromium PDF rendering."},
			{"porting", "Porting an existing frontend", "Serving an external design system or html/template pages."},
			{"runtime-contract", "Runtime contract", "Full SSR, hydration, island, and data-cui-* reference."},
			{"runtime-minification", "Runtime modules", "Feature-split vanilla JS modules loaded on demand."},
			{"compute", "Background compute", "Content-addressed, CSP-safe Web Workers and WASM off the main thread."},
		},
	},
	{
		Num: "02", Slug: "serving", Title: "Auth, HTTP & batteries",
		Lede: "Security defaults, auth, access control, and opt-in infrastructure batteries behind small interfaces.",
		Path: []string{"Auth", "Access control", "Security defaults"},
		Docs: []docEntry{
			{"auth", "Auth", "Sessions, login, OAuth, magic links, 2FA, passkeys, and password resets."},
			{"access-control", "Access control", "Roles, wildcard scopes, permissions, and custom policies."},
			{"security", "Security defaults", "CSP, CSRF, CORS, headers, and safe defaults enabled out of the box."},
			{"rate-limit", "Rate limiting", "Sliding-window and lockout HTTP rate limiting."},
			{"idempotency", "Idempotency", "Safe mutation retries with Idempotency-Key headers."},
			{"health-checks", "Health checks", "/healthz and /readyz probes with plugin readiness hooks."},
			{"cache", "Cache", "Memoize expensive reads behind a small Cache interface with TTLs."},
			{"endpoints", "Custom endpoints", "Hand-written typed HTTP routes wired into clients and SDKs."},
			{"api-versioning", "API prefix & versioning", "URL version prefixes via WithAPIPrefix, plus experimental projections and deprecation headers."},
			{"queue", "Job queue", "Durable background jobs with retries, jitter, and dead-letter replay."},
			{"cron", "Cron", "Scheduled jobs via in-process Scheduler or restart-safe DurableScheduler."},
			{"email", "Email", "SMTP and dev log senders plus template rendering."},
			{"notifications", "Notifications", "Multi-channel notification delivery with per-channel templates."},
			{"webhooks", "Webhooks", "Signed outbound webhook delivery with backoff and dead-lettering."},
			{"search", "Full-text search", "In-memory, SQLite FTS5, and Postgres full-text search backends."},
			{"storage", "Storage backends", "Local filesystem and S3-compatible file storage."},
			{"admin", "Admin UI", "Built-in back-office screens for data, queues, and audit logs."},
			{"feature-flags", "Feature flags", "Rollout percentages, allow lists, and environment evaluators."},
			{"i18n", "i18n", "JSON catalogs, CLDR plurals/formatting, RTL, and locale negotiation."},
			{"relay", "First-party relay", "Same-origin reverse proxy for third-party scripts under strict CSP."},
			{"analytics-recipes", "Analytics recipes", "First-party PostHog and Statsig recipes over battery/relay."},
			{"embed", "Embeddable surfaces", "Embed screens on external sites with origin allowlists and handshake nonces."},
			{"rtc", "WebRTC rooms", "Signaling rooms over StateChannel with peer-to-peer media."},
			{"plugins", "Plugins", "The lifecycle interface every battery and extension implements."},
			{"modules", "Modules", "Manifest-backed modules with runtime enable/disable."},
		},
	},
	{
		Num: "03", Slug: "data", Title: "Data & entities",
		Lede: "SQLite and Postgres persistence, migrations, core utilities, and opt-in entity declarations.",
		Path: []string{"Entity declarations", "Migrations", "Hooks & transactions"},
		Docs: []docEntry{
			{"entity-declarations", "Entity declarations", "Opt-in declarations that wire tables, REST CRUD, validation, OpenAPI, and MCP."},
			{"migrations", "Migrations", "Versioned, checksum-guarded SQL migrations plus dev auto-migrate."},
			{"hooks-and-transactions", "Hooks & transactions", "Before/After lifecycle hooks, TxFromContext, and App.InTx."},
			{"query-dsl", "Filter DSL", "?status=published&views_gte=10&sort=-created_at parses to a typed Where."},
			{"includes", "Eager loading", "Nested ?include= preloading without N+1 queries."},
			{"cursor-pagination", "Cursor pagination", "Opt-in keyset pagination with composite cursors."},
			{"batch-endpoints", "Batch endpoints", "Atomic multi-record POST, PATCH, and DELETE in one transaction."},
			{"states", "States", "Guarded state machines enforced across REST, MCP, and Go writes."},
			{"entityui", "Entity screens", "Generated list, detail, and create screens from entity Display/States."},
			{"events", "Events & SSE", "In-process event bus, transactional outbox, and entity SSE streams."},
			{"multi-tenant", "Multi-tenant scope", "Automatic tenant_id column and request-scoped filtering."},
			{"uploads", "Uploads", "Multipart file and image uploads with automatic image renditions."},
			{"sqlite-driver", "The SQLite driver", "Pure-Go SQLite (CGO_ENABLED=0) defaults and :memory: rules."},
			{"audit-log", "Audit log", "Transactional audit rows for Create, Update, and Delete writes."},
			{"data-export", "Data export & import", "Privileged NDJSON archive export/import and user erasure helpers."},
			{"backups", "Backups and restore", "SQLite and Postgres backup commands and restore verification."},
			{"dotenv", "Env / .env", "Automatic .env loading via core/dotenv."},
			{"core-packages", "Core packages", "Standalone core/* building blocks you can use without framework/."},
		},
	},
	{
		Num: "04", Slug: "agents", Title: "Agents & MCP",
		Lede: "Dev MCP and contract checks while you build; /mcp, llm.md, and discovery endpoints once you ship.",
		Path: []string{"Agent-readiness", "MCP server", "Contracts"},
		Docs: []docEntry{
			{"agent-ready", "Agent-readiness", "One-call bundle for /mcp, /llms.txt, agent cards, and markdown negotiation."},
			{"mcp", "MCP server", "Registering custom tools, resources, prompts, and per-caller gates."},
			{"contracts", "Contracts", "gofastr verify: static checks, autofixes, and semantic coverage."},
			{"a2a", "A2A task exchange", "Agent2Agent v1.0 skills, streaming tasks, and push notifications."},
			{"agent-host", "Agent host", "Chat-host surfaces and authoring MCP App HTML widgets."},
			{"web-bot-auth", "Web Bot Auth", "Publish signing JWKS and verify RFC 9421 bot signatures."},
			{"generative-ui", "Generative UI", "Why agents compose design-system primitives instead of emitting raw CSS."},
			{"semantic-search", "Semantic search", "Local hybrid vector + keyword retrieval with no external API key."},
			{"audit-deps", "Audit deps", "Find candidate global registration calls in a source tree."},
			{"local-runs", "Local run registry", "Machine-local registry of running GoFastr apps and their MCP URLs."},
		},
	},
	{
		Num: "05", Slug: "generators", Title: "Generators & SDKs",
		Lede: "One-shot app scaffolding, config-driven codegen, typed Go/TS SDKs, branded CLIs, and package unmounting.",
		Path: []string{"App specs", "Code generation", "Ship your API as SDKs"},
		Docs: []docEntry{
			{"app-spec", "App specs", "YAML schema reference for one-shot app scaffolding."},
			{"codegen", "Code generation", "Config-driven generators and --watch mode from gofastr.settings.yml."},
			{"sdk", "Ship your API as SDKs", "Generate typed Go and JS/TS clients plus live SDK docs."},
			{"app-cli", "Ship your API as a CLI", "Generate a branded customer CLI with scoped API-token auth."},
			{"settings", "Project settings", "gofastr.settings.yml for codegen, contracts, and isolation."},
			{"unmount", "Unmounting a package", "Copy a GoFastr package into your repo to customize it, or remount."},
		},
	},
	{
		Num: "06", Slug: "shipping", Title: "Testing, ops & shipping",
		Lede: "Dev loop, tests, single-binary deploys, horizontal scaling, and headless/local hosts.",
		Path: []string{"Deployment", "Horizontal scaling", "Testkit"},
		Docs: []docEntry{
			{"deploy", "Deployment", "Single-binary SSH deploys with systemd and automatic health rollback."},
			{"scaling", "Horizontal scaling", "Stateless defaults, shared secrets, and multi-replica fanout."},
			{"log", "Logging", "Structured JSON logs with file/webhook sinks and MCP query tools."},
			{"observability", "Observability", "Prometheus metrics and OpenTelemetry tracing hooks."},
			{"first-run", "First-run setup", "Interactive setup wizard and headless bootstrap for empty databases."},
			{"testkit", "Testkit", "Real HTTP and isolated Postgres integration test helpers."},
			{"factories", "Factories", "Rails-style test fixtures and factories."},
			{"dev-livereload", "Dev livereload", "Rebuild-on-save, browser reload, and dev MCP under gofastr dev."},
			{"strict-mode", "Strict mode", "Fail boot on missing SEO metadata or missing per-screen axe coverage."},
			{"isolation", "Worktree isolation", "Per-worktree ports and local databases for parallel coding agents."},
			{"release", "Release downloads", "Cross-built archives, checksums.txt, and install.sh / install.ps1."},
			{"application-hosts", "Application hosts", "Run an App headless with ServicesHost or plug in custom transports."},
			{"local-apps", "Local apps", "Per-user local state directory (SQLite, secret, identity) for local hosts."},
		},
	},
	{
		Num: "07", Slug: "experimental", Title: "Experimental & hosts",
		Lede: "Provisional hosts, agent protocols, and isolation layers—usable today, APIs still evolving.",
		Path: []string{"Desktop host", "OS service host", "Kiln overview"},
		Docs: []docEntry{
			{"desktop", "Desktop host", "Experimental: run your app in the OS WebView with CGO_ENABLED=0."},
			{"service", "OS service host", "Experimental: install your binary as a per-user launchd/systemd/Windows service."},
			{"telemetry", "Logs and traces on disk", "Experimental: zero-infra local log and trace files with CLI inspection."},
			{"kiln", "Kiln overview", "Experimental: live agent-driven app builder that freezes to plain Go."},
			{"acp", "ACP server", "Experimental: Agent Client Protocol server behind kiln acp."},
			{"webmcp", "WebMCP bridge", "Experimental: expose server tools to in-browser agents via modelContext."},
			{"plugin-platform", "Plugin platform", "Experimental: sandboxed opaque-origin iframe host for heavy third-party JS plugins."},
			{"process-modules", "Process modules", "Experimental: out-of-process isolated third-party modules with capability brokering."},
		},
	},
	{
		Num: "08", Slug: "reference", Title: "Reference, releases & internals",
		Lede: "Benchmarks, release migration guides, and internal contributor docs.",
		Path: []string{"Benchmarks", "Performance results"},
		Docs: []docEntry{
			{"benchmarks", "Benchmarks", "Benchmark methodology and tiered suites."},
			{"perf-results", "Performance results", "Latest throughput and latency measurements."},
			{"release-0-87", "What changed in v0.87.0", "Zinc theme tokens, entity Display/States, and admin migration notes."},
			{"release-0-86", "What changed in v0.86.0", "Headless components, fui-* classes, layouts, and owned styles migration notes."},
			{"harness-architecture", "Harness architecture", "Internal AI coding-harness design reference."},
			{"harness-e2e-testing", "Harness E2E testing", "How the harness drives end-to-end browser tests."},
		},
	},
}

// docCount is the total number of individual doc pages in the catalog,
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
// "nothing is hidden" index, it lists docs whether or not they're featured in
// one of the reading intents above. README (the docs folder's own index) is
// skipped since it isn't a page. Used at the bottom of /docs/.
func allDocsSection() render.HTML {
	topics, err := docs.List()
	if err != nil {
		return render.HTML("")
	}
	links := make([]render.HTML, 0, len(topics))
	for _, t := range topics {
		if t.Name == "README" {
			continue
		}
		links = append(links, ui.Link(ui.LinkConfig{Href: "/docs/" + t.Name, Text: t.Title}))
	}
	return ui.Section(ui.SectionConfig{
		ID:          "all-az",
		Eyebrow:     "∑ · " + itoa(len(links)) + " docs",
		Heading:     "Every doc · A–Z",
		Description: "The complete embedded reference: every page, alphabetical, featured or not. It is the same content as `gofastr docs`.",
		Compact:     true,
	}, ui.Grid(ui.GridConfig{Min: "12rem", Gap: ui.GapSM}, links...))
}

// =============================================================================
// /docs/ and /docs/<slug>: one screen group on the docs layer.
// =============================================================================

// docRef is the catalog entry a /docs/<slug> URL names, with the intent
// that owns it (the crumbs link back to the intent's section).
type docRef struct {
	Intent docIntent
	Entry  docEntry
}

// resolvedDoc is the doc the /docs/{path...} route names: the article
// screen declares the resolver, and the screen and its fills read it,
// so one lookup serves the body, the crumbs and the pager.
var resolvedDoc = app.NewKey[docRef]("doc")

// resolveDoc looks the catch-all remainder up in the catalog. An unknown
// slug is app.ErrNotFound: the screen's Load is the first reader, so the
// request renders the site's not-found page with status 404, the UX the
// per-slug loop had by simply not registering unknown paths.
func resolveDoc(ctx context.Context) (docRef, error) {
	m, _ := app.MatchFromContext(ctx)
	slug := m.Param("path")
	intent, entry, ok := findDocEntry(slug)
	if !ok {
		return docRef{}, fmt.Errorf("docs: no catalog entry for %q: %w", slug, app.ErrNotFound)
	}
	return docRef{Intent: intent, Entry: entry}, nil
}

// registerDocsGroup registers the docs section: the index and the one
// catch-all route that serves every doc page, both on the docs layer
// (layout.go), so a navigation between two docs swaps only the article
// and the crumbs/pager fills while the docs nav stays put.
func registerDocsGroup(site *app.App) {
	frame, out := docsLayout()
	group := app.NewScreenGroup("/docs", frame)
	// The index has its own in-page rail (the reading intents); it
	// fills the layer's right column with it and leaves the crumbs and
	// the pager empty.
	group.Screen(groupScreen("/docs/", &ConceptsIndexScreen{}).
		Fill(out.Rail, &DocsIndexRail{}), nil)
	// One catch-all route serves every doc page. The resolver turns the
	// slug into its catalog entry (404 for an unknown slug), and
	// StaticPaths enumerates every page so export, sitemap, llm.md, and
	// the strict coverage gate stay in sync with the catalog.
	group.Screen(groupScreen("/docs/{path...}", &DocPageScreen{}).
		Resolve(resolvedDoc.From(resolveDoc)).
		Fill(out.Crumbs, &DocCrumbs{}).
		Fill(out.Pager, &DocPager{}), nil)
	site.Router.ScreenGroup(group)
}

// DocPageScreen is one doc: the embedded markdown, nothing else. The
// docs layer around it brings the nav, and the crumbs and pager are its
// fills.
type DocPageScreen struct{ Entry docEntry }

func (s *DocPageScreen) ScreenTitle(ctx context.Context) string       { return s.Entry.Title }
func (s *DocPageScreen) ScreenDescription(ctx context.Context) string { return s.Entry.Desc }
func (s *DocPageScreen) ScreenType() app.ScreenType                   { return app.ScreenPage }

// SetParams is empty: the slug reaches the screen through the resolver
// (resolvedDoc), but the router refuses a dynamic route whose screen has
// no SetParams, so the screen declares it has read what it needs.
func (s *DocPageScreen) SetParams(map[string]string) {}

// Load reads the resolved doc; an unknown slug fails here with
// app.ErrNotFound, before anything renders.
func (s *DocPageScreen) Load(ctx context.Context) error {
	ref, err := resolvedDoc.Get(ctx)
	if err != nil {
		return err
	}
	s.Entry = ref.Entry
	return nil
}

// StaticPaths enumerates every catalog doc so static export, sitemap,
// llm.md, and the strict coverage gate keep producing one page per doc,
// the same URL set the per-slug registration loop emitted.
func (s *DocPageScreen) StaticPaths(ctx context.Context) []map[string]string {
	out := make([]map[string]string, 0, len(flatDocs()))
	for _, e := range flatDocs() {
		out = append(out, map[string]string{"path": e.Slug})
	}
	return out
}

func (s *DocPageScreen) Render() render.HTML {
	if body, err := docs.Get(s.Entry.Slug); err == nil {
		return render.HTML(siteDocLinks(string(ui.Markdown(ui.MarkdownConfig{Source: string(body)}))))
	}
	return html.Paragraph(html.TextConfig{},
		render.Text("This doc isn't available yet. Browse the embedded docs with "),
		codeText("gofastr docs"), render.Text(" or open the "),
		html.Link(html.LinkConfig{Href: "/docs/", Text: "docs index"}), render.Text("."))
}

// DocCrumbs fills a doc page's crumbs outlet: Docs, the doc's intent,
// then the doc.
type DocCrumbs struct{ ref docRef }

func (c *DocCrumbs) Load(ctx context.Context) error {
	ref, err := resolvedDoc.Get(ctx)
	if err != nil {
		return err
	}
	c.ref = ref
	return nil
}

func (c *DocCrumbs) Render() render.HTML {
	return docpage.Crumbs(nil, "", []ui.Crumb{
		{Text: "Docs", Href: "/docs/"},
		{Text: c.ref.Intent.Title, Href: "/docs/#" + c.ref.Intent.Slug},
		{Text: c.ref.Entry.Title, Current: true},
	})
}

// DocPager fills a doc page's pager outlet with the previous and next
// doc in catalog order (the index before the first doc; no next card
// after the last).
type DocPager struct{ cfg docpage.PagerConfig }

func (p *DocPager) Load(ctx context.Context) error {
	ref, err := resolvedDoc.Get(ctx)
	if err != nil {
		return err
	}
	p.cfg = docPrevNext(ref.Entry.Slug)
	return nil
}

func (p *DocPager) Render() render.HTML { return docpage.Pager(p.cfg) }

// docsSectionMenuConfig is the single source of truth for the docs nav,
// shared by the docs layer's rail and the mounted mobile drawer. Nothing
// is marked active here: the rail is static chrome the layer keeps
// across navigations, so the runtime's active-link sweep owns the
// current-page mark (a server mark on a kept rail would go stale on the
// first doc-to-doc navigation).
func docsSectionMenuConfig() interactive.SectionMenuConfig {
	groups := make([]interactive.SectionGroup, 0, len(docIntents))
	for _, it := range docIntents {
		items := make([]interactive.SectionItem, 0, len(it.Docs))
		for _, d := range it.Docs {
			items = append(items, interactive.SectionItem{Label: d.Title, Href: "/docs/" + d.Slug})
		}
		groups = append(groups, interactive.SectionGroup{Eyebrow: it.Num, Label: it.Title, Items: items})
	}
	return interactive.SectionMenuConfig{
		AriaLabel:         "Documentation sections",
		TriggerLabel:      "Sections",
		DrawerName:        "docs-section-menu",
		Filter:            true,
		FilterPlaceholder: fmt.Sprintf("Filter %d docs", len(flatDocs())),
		Lead:              &interactive.SectionItem{Label: "Docs index", Href: "/docs/"},
		Groups:            groups,
	}
}

// docPrevNext computes the previous/next doc in catalog order for the pager.
func docPrevNext(slug string) docpage.PagerConfig {
	flat := flatDocs()
	idx := -1
	for i, d := range flat {
		if d.Slug == slug {
			idx = i
			break
		}
	}
	p := docpage.PagerConfig{PrevHref: "/docs/", PrevLabel: "Docs index"}
	if idx > 0 {
		p.PrevHref, p.PrevLabel = "/docs/"+flat[idx-1].Slug, flat[idx-1].Title
	}
	if idx >= 0 && idx < len(flat)-1 {
		p.NextHref, p.NextLabel = "/docs/"+flat[idx+1].Slug, flat[idx+1].Title
	}
	return p
}
