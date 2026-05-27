package main

// =============================================================================
// screens_pages.go — the remaining v2 site pages. Each Render mirrors the
// corresponding prototype at /tmp/gofastr-design/gofastr/project/pages/*.html.
// Built with core-ui/html primitives so escaping + landmark roles are typed.
// Page-local CSS classes live in styles_pages.go and resolve through the
// shared theme.
//
// The pages share helpers from screen_home.go (container, sectionHead,
// sectionWrap) and from code_block.go (codeBlock, kw, fn_, str_, pn, ty, com).
// =============================================================================

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// codeText — shared inline <code> span used by most pages.
func codeText(s string) render.HTML { return render.Tag("code", nil, render.Text(s)) }

// tagAccent — the pre-alpha pill used in multiple page heroes.
func tagAccent(label string) render.HTML {
	return html.Span(html.TextConfig{Class: "tag accent"},
		html.Span(html.TextConfig{Class: "dot"}),
		render.Text(label),
	)
}

// =============================================================================
// /get-started
// =============================================================================

type GetStartedScreen struct{}

func (s *GetStartedScreen) ScreenTitle() string        { return "Get started — GoFastr" }
func (s *GetStartedScreen) ScreenDescription() string  { return "Cold machine to a running GoFastr app in four minutes." }
func (s *GetStartedScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *GetStartedScreen) Render() render.HTML {
	return render.Join(gsHero(), gsBody(), gsNext())
}

func gsHero() render.HTML {
	fact := func(label, value string) render.HTML {
		return html.Div(html.DivConfig{Class: "fact"},
			html.Span(html.TextConfig{Class: "l"}, render.Text(label)),
			html.Span(html.TextConfig{Class: "v"}, render.Text(value)),
		)
	}
	facts := html.Div(html.DivConfig{Class: "gs-facts"},
		fact("Prereqs", "Go 1.26+, git"),
		fact("OS", "macOS, Linux, Windows (WSL)"),
		fact("Storage", "SQLite by default, Postgres opt-in"),
		fact("Time", "~4 minutes"),
		html.Div(html.DivConfig{Class: "fact full"},
			html.Span(html.TextConfig{Class: "l"}, render.Text("Or, ask an agent")),
			html.Span(html.TextConfig{Class: "v"},
				render.Text("Skip the path — run "), codeText("kiln serve --agent claude-code"),
				render.Text(" and chat the app into existence.")),
		),
	)

	hero := html.Div(html.DivConfig{Class: "gs-hero__grid"},
		html.Div(html.DivConfig{Class: "mb-lg"},
			html.Div(html.DivConfig{Class: "mb-lg"}, tagAccent("Get started · v0.0.4")),
			html.Heading(html.HeadingConfig{Level: 1},
				render.Text("From cold machine to a running app in "),
				html.Span(html.TextConfig{Class: "amber"}, render.Text("four minutes")),
				render.Text("."),
			),
			render.Tag("p", map[string]string{"class": "lede"},
				render.Text("Install the CLI, scaffold an app, declare an entity, run it. Every command in this guide is real — paste it into a terminal and it works."),
			),
		),
		facts,
	)
	return html.Section(html.SectionConfig{Class: "gs-hero", Label: "Get started"}, container(hero))
}

func gsBody() render.HTML {
	railLink := func(n, anchor, text string, active bool) render.HTML {
		cls := ""
		if active {
			cls = "active"
		}
		return html.ListItem(html.ListItemConfig{},
			html.LinkHTML(html.LinkHTMLConfig{
				Href:  "#" + anchor,
				Class: cls,
				Content: render.Join(
					html.Span(html.TextConfig{Class: "n"}, render.Text(n)),
					render.Text(text),
				),
			}),
		)
	}
	rail := render.Tag("aside", map[string]string{"class": "step-rail"},
		render.Tag("h6", nil, render.Text("The path")),
		render.Tag("ol", nil,
			railLink("01", "s1", "Install", true),
			railLink("02", "s2", "Scaffold", false),
			railLink("03", "s3", "First entity", false),
			railLink("04", "s4", "Run it", false),
			railLink("05", "s5", "First page", false),
			railLink("06", "s6", "What you have", false),
		),
		html.Div(html.DivConfig{Class: "meta"}, render.Text("Stuck? Open the journal: pkg.go.dev/github.com/DonaldMurillo/gofastr")),
	)

	step := func(id, num, title, time string, body ...render.HTML) render.HTML {
		head := html.Div(html.DivConfig{Class: "step__head"},
			html.Span(html.TextConfig{Class: "step__num"}, render.Text(num)),
			html.Heading(html.HeadingConfig{Level: 3, Class: "step__title"}, render.Text(title)),
			html.Span(html.TextConfig{Class: "step__time"}, render.Text(time)),
		)
		inner := []render.HTML{head, html.Div(html.DivConfig{Class: "step__body"}, body...)}
		return html.Section(html.SectionConfig{ID: id, Class: "step", Label: title}, inner...)
	}

	termBlock := func(label string, lines ...render.HTML) render.HTML {
		return html.Div(html.DivConfig{Class: "term"},
			html.Div(html.DivConfig{Class: "term__head"},
				html.Span(html.TextConfig{Class: "dot"}),
				render.Text(label),
			),
			html.Div(html.DivConfig{Class: "term__body"}, lines...),
		)
	}
	o := func(s string) render.HTML { return html.Span(html.TextConfig{Class: "o"}, render.Text(s)) }
	ok := func(s string) render.HTML { return html.Span(html.TextConfig{Class: "ok"}, render.Text(s)) }

	callout := func(title, body string) render.HTML {
		return html.Div(html.DivConfig{Class: "callout"},
			html.Heading(html.HeadingConfig{Level: 5}, render.Text(title)),
			render.Tag("p", nil, render.Text(body)),
		)
	}

	step1 := step("s1", "01", "Install", "~30s",
		render.Tag("p", nil, render.Text("One binary covers scaffold, migrate, dev, build, test, and the doc browser. Get it from GitHub:")),
		termBlock("$ install",
			render.Text("$ go install github.com/DonaldMurillo/gofastr/cmd/gofastr@latest\n"),
			ok("→ installed gofastr v0.0.4 to ~/go/bin\n"),
		),
		render.Tag("p", nil, render.Text("Verify it's on your PATH with "), codeText("gofastr --version"), render.Text(".")),
		callout("If go install fails", "Make sure $GOPATH/bin (or ~/go/bin) is in your PATH. Run echo $PATH and add the missing entry to your shell rc."),
	)

	step2 := step("s2", "02", "Scaffold", "~45s",
		render.Tag("p", nil, render.Text("Scaffold a new project — it writes a working main.go, theme.go, and an empty entities directory.")),
		termBlock("$ scaffold",
			render.Text("$ gofastr init blog\n"),
			ok("→ wrote blog/main.go, blog/theme.go, blog/entities/\n"),
			ok("→ go.mod created with module \"blog\"\n"),
			ok("→ next: cd blog && go run .\n"),
		),
		render.Tag("p", nil, render.Text("Open the scaffolded main.go — it's about 30 lines. Read it.")),
	)

	step3 := step("s3", "03", "First entity", "~60s",
		render.Tag("p", nil, render.Text("Declare your first entity in Go. One call generates SQL, REST, MCP, OpenAPI, and a typed query builder.")),
		codeBlock("blog/main.go", []render.HTML{
			ln(render.Text("  app."), fn_("Entity"), pn("("), str_(`"posts"`), pn(","), render.Text(" framework."), ty("Entity"), pn("{")),
			ln(render.Text("    Fields"), pn(":"), render.Text(" framework."), ty("Fields"), pn("{")),
			ln(render.Text("      "), str_(`"title"`), pn(":"), render.Text(" f."), fn_("String"), pn("()."), fn_("Required"), pn("(),")),
			ln(render.Text("      "), str_(`"body"`), pn(":"), render.Text("  f."), fn_("Markdown"), pn("(),")),
			ln(render.Text("    "), pn("},")),
			ln(render.Text("    Timestamps"), pn(":"), render.Text(" "), kw("true"), pn(",")),
			ln(render.Text("  "), pn("})")),
		}),
		render.Tag("p", nil, render.Text("That's the whole declaration. No migrations file. No schema yaml. Just Go.")),
	)

	step4 := step("s4", "04", "Run it", "~15s",
		render.Tag("p", nil, render.Text("Start the app. The framework auto-migrates the SQLite schema, mounts /posts, /openapi.json, /_/mcp, and a livereload SSE stream.")),
		termBlock("$ run",
			render.Text("$ go run .\n"),
			ok("→ HTTP on http://localhost:8080\n"),
			ok("→ migrated posts (1 table)\n"),
			ok("→ /_/openapi.json + /_/mcp ready\n"),
		),
		render.Tag("p", nil, render.Text("In a second terminal, hit the API to prove it works:")),
		termBlock("$ probe",
			o("$ curl -s -X POST http://localhost:8080/posts \\\n"),
			o("    -H 'content-type: application/json' \\\n"),
			o("    -d '{\"title\":\"Hello\",\"body\":\"world\"}'\n"),
			ok("{\"id\":\"01J7…\",\"title\":\"Hello\",\"body\":\"world\",…}\n"),
		),
	)

	step5 := step("s5", "05", "First page", "~60s",
		render.Tag("p", nil, render.Text("Add a server-rendered page. Screens are normal Go structs that implement Render() and live alongside main.go.")),
		codeBlock("blog/screen_posts.go", []render.HTML{
			ln(kw("func"), render.Text(" (s "), pn("*"), ty("PostsScreen"), pn(")"), render.Text(" "), fn_("Render"), pn("()"), render.Text(" "), ty("render.HTML"), pn(" {")),
			ln(render.Text("  posts, "), pn("_"), render.Text(" := posts."), fn_("Query"), pn("("), render.Text("ctx"), pn(")."), fn_("List"), pn("(20)")),
			ln(render.Text("  "), kw("return"), render.Text(" html."), fn_("Div"), pn("("), render.Text("html."), ty("DivConfig"), pn("{},")),
			ln(render.Text("    /* render each post */")),
			ln(render.Text("  "), pn(")")),
			ln(pn("}")),
		}),
		callout("Tip", "Run `gofastr docs` to browse the embedded docs in a TUI — entity-declarations, query-dsl, hooks, all of it."),
	)

	step6 := step("s6", "06", "What you have", "now",
		render.Tag("p", nil, render.Text("In four minutes you've stood up an app with full HTTP + agent surface area:")),
		html.Div(html.DivConfig{Class: "result"},
			html.Heading(html.HeadingConfig{Level: 5}, render.Text("Running, on disk, queryable, agent-driven")),
			html.UnorderedList(html.ListConfig{},
				html.ListItem(html.ListItemConfig{}, render.Text("Versioned SQL migrations")),
				html.ListItem(html.ListItemConfig{}, render.Text("REST CRUD + cursor pagination")),
				html.ListItem(html.ListItemConfig{}, render.Text("OpenAPI 3 + Swagger UI")),
				html.ListItem(html.ListItemConfig{}, render.Text("MCP tools at /_/mcp")),
				html.ListItem(html.ListItemConfig{}, render.Text("Typed Go query builder")),
				html.ListItem(html.ListItemConfig{}, render.Text("Hot-reload dev SSE")),
			),
		),
	)

	body := html.Div(html.DivConfig{Class: "gs-body"},
		rail,
		html.Div(html.DivConfig{}, step1, step2, step3, step4, step5, step6),
	)
	return container(body)
}

func gsNext() render.HTML {
	card := func(meta, title, desc, href string) render.HTML {
		return html.LinkHTML(html.LinkHTMLConfig{
			Href:  href,
			Class: "ex-card",
			Content: render.Join(
				html.Span(html.TextConfig{Class: "path"}, render.Text(meta)),
				html.Heading(html.HeadingConfig{Level: 4}, render.Text(title)),
				render.Tag("p", nil, render.Text(desc)),
			),
		})
	}
	return html.Section(html.SectionConfig{Class: "next", Label: "What now"},
		container(
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Where next")),
			html.Div(html.DivConfig{Class: "next__grid"},
				card("/docs/", "Browse the docs", "53 docs grouped by what you're trying to do.", "/docs/"),
				card("/examples", "Read an example", "Six full apps you can clone and modify.", "/examples"),
				card("/kiln", "Try Kiln", "Skip the writing entirely — chat your app into being.", "/kiln"),
			),
		),
	)
}

// =============================================================================
// /docs/  (concepts index)
// =============================================================================

type ConceptsIndexScreen struct{}

func (s *ConceptsIndexScreen) ScreenTitle() string { return "Docs — GoFastr" }
func (s *ConceptsIndexScreen) ScreenDescription() string {
	return "Every feature, grouped by what you're trying to do — not alphabetically."
}
func (s *ConceptsIndexScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ConceptsIndexScreen) Render() render.HTML {
	return render.Join(cxHero(), cxBody())
}

func cxHero() render.HTML {
	stat := func(value, label string) render.HTML {
		return html.Div(html.DivConfig{},
			html.Span(html.TextConfig{Class: "v"}, render.Text(value)),
			html.Span(html.TextConfig{Class: "l"}, render.Text(label)),
		)
	}
	stats := html.Div(html.DivConfig{Class: "cx-stats"},
		stat("53", "docs"),
		stat("6", "intents"),
		stat("31", "packages"),
	)
	hero := html.Div(html.DivConfig{Class: "cx-hero__grid"},
		html.Div(html.DivConfig{},
			html.Div(html.DivConfig{Class: "mb-lg"}, tagAccent("Docs · v0.0.4")),
			html.Heading(html.HeadingConfig{Level: 1},
				render.Text("Read by what you're "),
				html.Span(html.TextConfig{Class: "amber"}, render.Text("trying to do")),
				render.Text("."),
			),
			render.Tag("p", map[string]string{"class": "lede"},
				render.Text("The framework's surface is grouped into six intents. Pick the one that matches the question you're holding."),
			),
		),
		stats,
	)
	return html.Section(html.SectionConfig{Class: "cx-hero", Label: "Docs"}, container(hero))
}

func cxBody() render.HTML {
	intents := []struct {
		num, slug, title, meta, lede string
		docs                         []docCardData
		path                         []string
	}{
		{
			"01", "modeling", "Modeling your domain", "9 docs · ~22 min", "Declare entities, fields, relations. The framework generates schema, CRUD, validators, and code.",
			[]docCardData{
				{"frame", "Entity declarations", "JSON or Go — both produce the same tables, routes, and tools.", "framework/entity"},
				{"frame", "Field types", "string, text, enum, relation, slug, markdown — and the file/image upload types.", "framework/field"},
				{"core", "Schema primitives", "The stdlib-only token under entity declarations.", "core/schema"},
				{"frame", "Migrations", "Versioned, ordered, reversible — versus the auto-migrate dev mode.", "framework/migrate"},
				{"frame", "Hooks & transactions", "BeforeCreate / AfterUpdate hooks share the parent tx.", "framework/hook"},
				{"frame", "Filter DSL", "?status=published&views_gte=10&sort=-created_at parses to a typed Where.", "framework/filter"},
				{"frame", "Cursor pagination", "Keyset by EntityConfig.CursorField. Opt in by sending ?cursor=.", "framework/pagination"},
				{"frame", "Eager loading", "?include=author.profile flattens N+1.", "framework/crud"},
				{"frame", "Multi-tenant scope", "tenant_id column + automatic filter from request context.", "framework/tenant"},
			},
			[]string{"Entity declarations", "Field types", "Filter DSL"},
		},
		{
			"02", "serving", "Serving HTTP", "9 docs · ~24 min", "Routes, middleware, sessions, auth, idempotency, security headers — everything between the wire and your handler.",
			[]docCardData{
				{"core", "Router", "Pattern-based net/http wrapper with Group + middleware chain.", "core/router"},
				{"core", "Middleware", "Compose RequestID + Logging + Recovery + Security + Timeout.", "core/middleware"},
				{"frame", "Access control", "RolePolicy, RequirePermission, custom Policy implementations.", "framework/access"},
				{"battery", "Auth", "Login, OAuth, magic-link, 2FA, password reset — each a plugin.", "battery/auth"},
				{"core", "Idempotency", "Idempotency-Key header replays safely.", "core/middleware"},
				{"core", "Security defaults", "CSP, CSRF, rate limit, headers — all on by default.", "core/middleware"},
				{"core", "Health checks", "/healthz + /readyz with plugin checks.", "core/handler"},
				{"battery", "Webhooks", "Signed outbound delivery with retry-with-backoff.", "battery/webhook"},
				{"frame", "Notifications", "Multi-channel fan-out with per-channel templates.", "battery/notify"},
			},
			[]string{"Router", "Middleware", "Access control"},
		},
		{
			"03", "ui", "Building UI", "9 docs · ~28 min", "Server-rendered with islands. Signals, HTML primitives, composed patterns, the runtime, and theming.",
			[]docCardData{
				{"ui", "Getting started (UI)", "The 15-minute path: scaffold → theme → screen → custom component.", "framework/ui"},
				{"ui", "Architecture", "SSR + hydration + islands — the three failure modes.", "core-ui"},
				{"ui", "Signals", "Reactive state shared between server + client.", "core-ui/signal"},
				{"ui", "HTML primitives", "Typed wrappers around standard HTML elements.", "core-ui/html"},
				{"ui", "Patterns", "Accordion, tabs, modal, drawer, popover, toast.", "core-ui/patterns"},
				{"ui", "Themes", "The typed theme: Colors, Spacing, Radii, Fonts, Motion.", "framework/ui/theme"},
				{"ui", "Widget builder", "Build islands that hydrate against a registered handler.", "core-ui/widget"},
				{"ui", "Runtime modules", "Carved per-feature so pages without X don't ship X's JS.", "core-ui/runtime"},
				{"ui", "Image pipeline", "Pure-Go resize + WebP lossless encode.", "framework/image"},
			},
			[]string{"Getting started (UI)", "Patterns", "Widget builder"},
		},
		{
			"04", "persist", "Persisting & migrating", "6 docs · ~12 min", "SQLite and Postgres, dialect-aware, with the migration CLI and per-test isolation.",
			[]docCardData{
				{"frame", "Migrations", "SQL files, versioned, CLI subcommands, dialect detection.", "framework/migrate"},
				{"frame", "Soft delete", "deleted_at column + automatic filter.", "framework/softdelete"},
				{"frame", "Audit log", "WithAuditLog writes a row for every Create/Update/Delete.", "framework/audit"},
				{"frame", "Isolation", "Linked git worktrees get isolated local DBs.", "framework/isolation"},
				{"frame", "Factories", "Rails-style fixtures for tests.", "framework/factory"},
				{"battery", "Cache", "Per-key + per-tag invalidation behind a small interface.", "battery/cache"},
			},
			[]string{"Migrations", "Soft delete", "Isolation"},
		},
		{
			"05", "agents", "Working with agents", "7 docs · ~22 min", "MCP tools, Kiln build mode, agent permissions, plan-gated destructive ops.",
			[]docCardData{
				{"frame", "MCP CRUD", "Every entity ships posts_list, posts_get, posts_create, posts_update, posts_delete.", "framework/crud"},
				{"core", "MCP primitives", "The stdlib-only token under MCP-CRUD.", "core/mcp"},
				{"frame", "Kiln overview", "The agent-driven build mode binary.", "kiln"},
				{"frame", "Kiln tools", "add_entity, add_field, propose_plan — the typed surface.", "kiln/protocol"},
				{"frame", "Agent notes", "Append-only review log for agents working on the framework.", "framework/docs"},
				{"frame", "Audit deps", "Detect packages an agent shouldn't import.", "framework/agentsinv"},
				{"battery", "Embed", "Local semantic search via brute-force cosine — no API key.", "battery/embed"},
			},
			[]string{"MCP CRUD", "Kiln overview", "Embed"},
		},
		{
			"06", "ops", "Operations", "5 docs · ~14 min", "Run it in production. Logging, metrics, feature flags, env, i18n.",
			[]docCardData{
				{"battery", "Logging", "Structured JSON logs with MCP query tools.", "battery/log"},
				{"core", "Metrics", "Counter + histogram + /metrics endpoint.", "core/middleware"},
				{"core", "Feature flags", "Rollout percentage, allow lists, env evaluator.", "core/featureflag"},
				{"core", "Env / .env", "core/dotenv auto-loaded by NewApp.", "core/dotenv"},
				{"core", "i18n", "JSON catalogs, plurals, Accept-Language negotiation.", "core/i18n"},
			},
			[]string{"Logging", "Metrics", "Feature flags"},
		},
	}

	railItems := []render.HTML{}
	for _, it := range intents {
		cls := ""
		if it.num == "01" {
			cls = "active"
		}
		railItems = append(railItems, html.ListItem(html.ListItemConfig{},
			html.LinkHTML(html.LinkHTMLConfig{
				Href:  "#" + it.slug,
				Class: cls,
				Content: render.Join(
					html.Span(html.TextConfig{Class: "n"}, render.Text(it.num)),
					render.Text(it.title),
					html.Span(html.TextConfig{Class: "ct"}, render.Text(itoa(len(it.docs)))),
				),
			}),
		))
	}
	rail := render.Tag("aside", map[string]string{"class": "intent-rail"},
		render.Tag("h6", nil, render.Text("By intent")),
		render.Tag("ul", nil, railItems...),
	)

	sections := []render.HTML{}
	for _, it := range intents {
		sections = append(sections, intentSection(it.num, it.slug, it.title, it.meta, it.lede, it.docs, it.path))
	}

	return container(html.Div(html.DivConfig{Class: "cx-body"},
		rail,
		html.Div(html.DivConfig{}, sections...),
	))
}

type docCardData struct{ pill, title, desc, meta string }

func intentSection(num, slug, title, meta, lede string, docs []docCardData, path []string) render.HTML {
	cards := []render.HTML{}
	for _, d := range docs {
		cards = append(cards, html.Div(html.DivConfig{Class: "doc"},
			html.Div(html.DivConfig{Class: "doc__head"},
				html.Span(html.TextConfig{Class: "pill " + d.pill}, render.Text(d.pill)),
			),
			html.Div(html.DivConfig{Class: "doc__title"}, render.Text(d.title)),
			html.Div(html.DivConfig{Class: "doc__desc"}, render.Text(d.desc)),
			html.Div(html.DivConfig{Class: "doc__meta"}, render.Text(d.meta)),
		))
	}
	stripChildren := []render.HTML{html.Span(html.TextConfig{Class: "l"}, render.Text("Recommended path"))}
	for i, p := range path {
		if i > 0 {
			stripChildren = append(stripChildren, html.Span(html.TextConfig{Class: "arrow"}, render.Text("→")))
		}
		stripChildren = append(stripChildren, html.Span(html.TextConfig{Class: "s"}, render.Text(p)))
	}
	return html.Section(html.SectionConfig{ID: slug, Class: "intent", Label: title},
		html.Div(html.DivConfig{Class: "intent__head"},
			html.Span(html.TextConfig{Class: "intent__num"}, render.Text(num)),
			html.Heading(html.HeadingConfig{Level: 2, Class: "intent__title"}, render.Text(title)),
			html.Span(html.TextConfig{Class: "intent__meta"}, render.Text(meta)),
		),
		render.Tag("p", map[string]string{"class": "intent__lede"}, render.Text(lede)),
		html.Div(html.DivConfig{Class: "docs"}, cards...),
		html.Div(html.DivConfig{Class: "path-strip"}, stripChildren...),
	)
}

// =============================================================================
// /docs/{slug}  (single doc page — uses Entities as the canonical demo)
// =============================================================================

type ConceptsDocScreen struct{}

func (s *ConceptsDocScreen) ScreenTitle() string        { return "Entities — GoFastr docs" }
func (s *ConceptsDocScreen) ScreenDescription() string  { return "Declare a domain entity once; the framework generates SQL, REST, MCP, OpenAPI, and a typed model." }
func (s *ConceptsDocScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ConceptsDocScreen) Render() render.HTML {
	return render.Tag("div", map[string]string{"class": "doc-shell"},
		docNavSidebar(),
		docArticle(),
		docInPageToc(),
	)
}

func docNavSidebar() render.HTML {
	group := func(n, label string, items ...render.HTML) render.HTML {
		return html.Div(html.DivConfig{Class: "docnav__group"},
			html.Div(html.DivConfig{Class: "label"},
				html.Span(html.TextConfig{Class: "n"}, render.Text(n)),
				render.Text(label),
			),
			html.UnorderedList(html.ListConfig{}, items...),
		)
	}
	li := func(href, text string, active bool) render.HTML {
		cls := ""
		if active {
			cls = "active"
		}
		return html.ListItem(html.ListItemConfig{},
			html.Link(html.LinkConfig{Href: href, Text: text, Class: cls}),
		)
	}
	return render.Tag("aside", map[string]string{"class": "docnav"},
		group("01", "Modeling your domain",
			li("/docs/entities", "Entities", true),
			li("/docs/field-types", "Field types", false),
			li("/docs/migrations", "Migrations", false),
			li("/docs/hooks", "Hooks & transactions", false),
		),
		group("02", "Serving HTTP",
			li("/docs/router", "Router", false),
			li("/docs/middleware", "Middleware", false),
			li("/docs/access", "Access control", false),
		),
		group("03", "Building UI",
			li("/docs/ui-getting-started", "Getting started", false),
			li("/docs/ui-architecture", "Architecture", false),
		),
	)
}

func docArticle() render.HTML {
	return render.Tag("article", map[string]string{"class": "doc-content"},
		render.Tag("nav", map[string]string{"class": "doc-crumbs", "aria-label": "Breadcrumb"},
			html.Link(html.LinkConfig{Href: "/docs/", Text: "Docs"}),
			html.Span(html.TextConfig{Class: "sep"}, render.Text("/")),
			html.Link(html.LinkConfig{Href: "/docs/#modeling", Text: "Modeling"}),
			html.Span(html.TextConfig{Class: "sep"}, render.Text("/")),
			html.Span(html.TextConfig{Class: "current"}, render.Text("Entities")),
		),
		html.Div(html.DivConfig{Class: "doc-head"},
			html.Heading(html.HeadingConfig{Level: 1},
				render.Text("Entities — the unit of "),
				html.Span(html.TextConfig{Class: "amber"}, render.Text("declaration")),
				render.Text("."),
			),
			html.Div(html.DivConfig{Class: "doc-head__meta"},
				tagAccent("Modeling"),
				render.Text("8 min read"),
				html.Span(html.TextConfig{Class: "sep"}, render.Text("·")),
				render.Text("Updated 2026-05-22"),
				html.Span(html.TextConfig{Class: "sep"}, render.Text("·")),
				render.Text("v0.0.4"),
			),
			render.Tag("p", map[string]string{"class": "doc-head__lede"},
				render.Text("An entity is the smallest unit GoFastr understands. Declare one, and the framework generates the SQL table, REST endpoints, MCP tools, OpenAPI schema, lifecycle hooks, and a typed Go query builder — all to disk, all readable."),
			),
		),
		docProse(),
		docFooter(),
	)
}

func docProse() render.HTML {
	return render.Tag("div", map[string]string{"class": "prose"},
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("What an entity declaration is")),
		render.Tag("p", nil,
			render.Text("Two equivalent forms — JSON loaded at runtime, or Go evaluated at process start. Both produce the exact same set of routes, OpenAPI ops, MCP tools, and migrations. Pick by who's reading the declaration: agents emit JSON, humans tend to prefer Go.")),
		codeBlock(".gofastr/posts.go", []render.HTML{
			ln(render.Text("app."), fn_("Entity"), pn("("), str_(`"posts"`), pn(","), render.Text(" framework."), ty("EntityConfig"), pn("{")),
			ln(render.Text("  SoftDelete"), pn(":"), render.Text(" "), kw("true"), pn(",")),
			ln(render.Text("  Fields"), pn(":"), render.Text(" []schema."), ty("Field"), pn("{")),
			ln(render.Text("    {Name"), pn(":"), render.Text(" "), str_(`"title"`), pn(","), render.Text(" Type"), pn(":"), render.Text(" schema."), ty("String"), pn(","), render.Text(" Required"), pn(":"), render.Text(" "), kw("true"), pn("},")),
			ln(render.Text("    {Name"), pn(":"), render.Text(" "), str_(`"body"`), pn(","), render.Text("  Type"), pn(":"), render.Text(" schema."), ty("Text"), pn("},")),
			ln(render.Text("  "), pn("},")),
			ln(render.Text("  MCP"), pn(":"), render.Text(" "), kw("true"), pn(",")),
			ln(pn("})")),
		}),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("What gets generated")),
		render.Tag("p", nil,
			render.Text("One declaration; many surfaces. The seven outputs land in three places: the database (a versioned migration), the request router (REST and MCP handlers), and "),
			codeText(".gofastr/entities/posts.go"), render.Text(" (the typed Go model + query builder).")),
		html.UnorderedList(html.ListConfig{},
			html.ListItem(html.ListItemConfig{}, render.Text("SQL table — emitted as plain up/down files in "), codeText(".gofastr/migrations/")),
			html.ListItem(html.ListItemConfig{}, render.Text("REST endpoints — list, get, create, update, delete, batch")),
			html.ListItem(html.ListItemConfig{}, render.Text("MCP tools — same auth, same validators")),
			html.ListItem(html.ListItemConfig{}, render.Text("OpenAPI 3 spec")),
			html.ListItem(html.ListItemConfig{}, render.Text("Typed Go model + query builder")),
		),
		render.Tag("div", map[string]string{"class": "note"},
			html.Heading(html.HeadingConfig{Level: 4}, render.Text("Not magic — readable")),
			render.Tag("p", nil, render.Text("Open the generated file. It's normal Go. You can step through it, add a print, vendor it. If something looks wrong, you can fix it — and your fix survives the next regeneration when you commit it.")),
		),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Field types")),
		render.Tag("p", nil,
			render.Text("Built-in field types cover the 90% case: "),
			codeText("string"), render.Text(", "),
			codeText("text"), render.Text(", "),
			codeText("enum"), render.Text(", "),
			codeText("relation"), render.Text(", "),
			codeText("slug"), render.Text(", "),
			codeText("markdown"), render.Text(", "),
			codeText("image"), render.Text(", "),
			codeText("file"), render.Text(". Each comes with validators, a default migration shape, and a Go type."),
		),
		render.Tag("blockquote", nil,
			render.Text("The right abstraction makes the simple case trivial and the complex case possible. The wrong abstraction makes both unreadable."),
		),
		render.Tag("p", nil,
			render.Text("Custom field types are a "), codeText("framework.Field"), render.Text(" implementation away. Define the validator, the migration shape, the Go type — register it, and your declarations can use it."),
		),
	)
}

func docFooter() render.HTML {
	prev := html.LinkHTML(html.LinkHTMLConfig{
		Href:  "/docs/",
		Class: "prev-card",
		Content: render.Join(
			html.Span(html.TextConfig{Class: "dir"}, render.Text("← Previous")),
			html.Span(html.TextConfig{Class: "ttl"}, render.Text("Docs index")),
		),
	})
	next := html.LinkHTML(html.LinkHTMLConfig{
		Href:  "/docs/field-types",
		Class: "next-card",
		Content: render.Join(
			html.Span(html.TextConfig{Class: "dir"}, render.Text("Next →")),
			html.Span(html.TextConfig{Class: "ttl"}, render.Text("Field types")),
		),
	})
	feedback := html.Div(html.DivConfig{Class: "feedback"},
		render.Text("Was this helpful?"),
		render.Tag("button", map[string]string{"type": "button"}, render.Text("yes")),
		render.Tag("button", map[string]string{"type": "button"}, render.Text("kind of")),
		render.Tag("button", map[string]string{"type": "button"}, render.Text("no")),
	)
	return html.Div(html.DivConfig{Class: "doc-foot"},
		html.Div(html.DivConfig{Class: "doc-foot__nav"}, prev, next),
		html.Div(html.DivConfig{Class: "doc-foot__chrome"},
			html.Link(html.LinkConfig{Href: "https://github.com/DonaldMurillo/gofastr/edit/main/framework/docs/content/entity-declarations.md", Text: "Edit on GitHub"}),
			html.Span(html.TextConfig{Class: "sep"}, render.Text("·")),
			html.Link(html.LinkConfig{Href: "https://github.com/DonaldMurillo/gofastr/blob/main/framework/entity/entity.go", Text: "View source"}),
			html.Span(html.TextConfig{Class: "sep"}, render.Text("·")),
			html.Link(html.LinkConfig{Href: "https://github.com/DonaldMurillo/gofastr/discussions", Text: "Discuss"}),
			feedback,
		),
	)
}

func docInPageToc() render.HTML {
	li := func(href, text string, active bool) render.HTML {
		cls := ""
		if active {
			cls = "active"
		}
		return html.ListItem(html.ListItemConfig{},
			html.Link(html.LinkConfig{Href: href, Text: text, Class: cls}),
		)
	}
	return render.Tag("aside", map[string]string{"class": "toc"},
		render.Tag("h6", nil, render.Text("On this page")),
		render.Tag("ol", nil,
			li("#what-an-entity-declaration-is", "What an entity declaration is", true),
			li("#what-gets-generated", "What gets generated", false),
			li("#field-types", "Field types", false),
		),
		html.Div(html.DivConfig{Class: "toc__foot"},
			render.Text("4 min · 740 words"),
		),
	)
}

// =============================================================================
// /examples
// =============================================================================

type ExamplesScreen struct{}

func (s *ExamplesScreen) ScreenTitle() string        { return "Examples — GoFastr" }
func (s *ExamplesScreen) ScreenDescription() string  { return "Six reference apps. Each runs in one command." }
func (s *ExamplesScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ExamplesScreen) Render() render.HTML {
	return render.Join(exHero(), exRows())
}

func exHero() render.HTML {
	return html.Section(html.SectionConfig{Class: "ex-hero", Label: "Examples"},
		container(
			html.Div(html.DivConfig{Class: "mb-lg"}, tagAccent("Examples · 6 apps")),
			html.Heading(html.HeadingConfig{Level: 1},
				render.Text("Six reference apps. "),
				html.Span(html.TextConfig{Class: "amber"}, render.Text("Each runs in one command")),
				render.Text("."),
			),
			render.Tag("p", map[string]string{"class": "lede"},
				render.Text("Clone the one that looks like your problem; swap the entity declarations. Each app's full source is under examples/ in the repo — copy what you need."),
			),
		),
	)
}

func exRows() render.HTML {
	rows := []render.HTML{
		exRow("01", "examples/blog", "JSON-declared blog", "smallest", "~120 LoC",
			"Posts, comments, tags. Three entities. Start here — it's the end-to-end story in one file.",
			[]string{"Three entities loaded from JSON", "Auto-CRUD + Swagger UI + MCP", "SQLite by default; swap for Postgres in main.go"},
			"cd examples/blog && go run ."),
		exRow("02", "examples/website", "Feature gallery", "largest", "~3000 LoC",
			"Every framework feature lit up at once. For contributors; less useful for first-timers.",
			[]string{"Every core-ui pattern", "Every framework/ui component", "CRUD demo, themes, dark mode, agents"},
			"cd examples/website && go run ."),
		exRow("03", "examples/api-tour", "API tour", "live docs", "~180 LoC",
			"Every REST endpoint as a chapter. Each chapter has a live curl example you run from the page.",
			[]string{"Cursor + offset pagination", "Eager loading (?include=…)", "Batch endpoints, SSE entity events, uploads"},
			"cd examples/api-tour && go run ."),
		exRow("04", "examples/embed-demo", "Local semantic search", "no API key", "~180 LoC",
			"A markdown corpus indexed locally via battery/embed. No external API key; works offline.",
			[]string{"Brute-force cosine, hybrid keyword fusion", "Snapshot + WAL persistence", "Poll-watch for file changes"},
			"cd examples/embed-demo && go run ."),
		exRow("05", "examples/spa", "Vue + GoFastr API", "BYO client", "~140 LoC server",
			"For teams who already have a client app. Shows the framework is happy to just be your typed API.",
			[]string{"Same auto-CRUD entities", "OpenAPI generates the TypeScript client", "No SSR — just the JSON surface"},
			"cd examples/spa && go run ."),
		exRow("06", "examples/static-site", "Static-site mode", "no server", "~90 LoC",
			"Same renderer, no server. gofastr build emits a CDN-friendly bundle of HTML + CSS + JS.",
			[]string{"Screens implement Load(ctx) once", "Build-time fetches replace SSR fetches", "Output drops straight on Cloudflare Pages or Netlify"},
			"cd examples/static-site && gofastr build"),
	}
	return container(render.Join(rows...))
}

func exRow(num, path, title, tag, loc, desc string, points []string, cmd string) render.HTML {
	pointLis := []render.HTML{}
	for _, p := range points {
		pointLis = append(pointLis, html.ListItem(html.ListItemConfig{}, render.Text(p)))
	}
	shot := html.Div(html.DivConfig{Class: "ex-shot"},
		html.Div(html.DivConfig{Class: "bar accent"}),
		html.Div(html.DivConfig{Class: "bar kw"}),
		html.Div(html.DivConfig{Class: "bar"}),
		html.Div(html.DivConfig{Class: "row"},
			html.Div(html.DivConfig{Class: "square"}),
			html.Div(html.DivConfig{Class: "square"}),
			html.Div(html.DivConfig{Class: "square"}),
		),
		html.Div(html.DivConfig{Class: "bar"}),
	)
	miniCode := codeBlock(path+"/main.go", []render.HTML{
		ln(kw("package"), render.Text(" main")),
		ln(),
		ln(kw("func"), render.Text(" "), fn_("main"), pn("()"), render.Text(" "), pn("{")),
		ln(render.Text("  app "), pn(":="), render.Text(" framework."), fn_("New"), pn("("), render.Text("…"), pn(")")),
		ln(render.Text("  app."), fn_("Entity"), pn("("), str_(`"posts"`), pn(","), render.Text(" …"), pn(")")),
		ln(render.Text("  app."), fn_("Serve"), pn("("), str_(`":8080"`), pn(")")),
		ln(pn("}")),
	})
	body := html.Div(html.DivConfig{Class: "ex-row__body"},
		html.Div(html.DivConfig{Class: "ex-row__meta"},
			tagAccent(tag),
			html.Span(html.TextConfig{Class: "lc"}, render.Text(loc)),
		),
		html.Heading(html.HeadingConfig{Level: 2, Class: "ex-row__title"},
			render.Text(path+" — "),
			html.Span(html.TextConfig{Class: "amber"}, render.Text(title)),
		),
		render.Tag("p", map[string]string{"class": "ex-row__desc"}, render.Text(desc)),
		html.UnorderedList(html.ListConfig{Class: "ex-row__points"}, pointLis...),
		html.Div(html.DivConfig{Class: "ex-row__cli"},
			html.Span(html.TextConfig{Class: "p"}, render.Text("$")),
			render.Text(cmd),
		),
	)
	right := html.Div(html.DivConfig{Class: "ex-row__right"}, miniCode, shot)
	grid := html.Div(html.DivConfig{Class: "ex-row__grid"},
		html.Span(html.TextConfig{Class: "ex-row__num"}, render.Text(num)),
		body,
		right,
	)
	return html.Section(html.SectionConfig{Class: "ex-row", Label: path}, grid)
}

// =============================================================================
// /kiln
// =============================================================================

type KilnScreen struct{}

func (s *KilnScreen) ScreenTitle() string { return "Kiln — GoFastr" }
func (s *KilnScreen) ScreenDescription() string {
	return "Build a GoFastr app live by chatting with an agent."
}
func (s *KilnScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *KilnScreen) Render() render.HTML {
	return render.Tag("div", map[string]string{"class": "kiln-page"},
		kHero(), kDemo(), kTimeline(), kCaps(), kCli(),
	)
}

func kHero() render.HTML {
	return html.Section(html.SectionConfig{Class: "k-hero", Label: "Kiln"},
		container(
			html.Div(html.DivConfig{Class: "k-hero__lockup"},
				html.Span(html.TextConfig{Class: "mark"}, render.Text("K")),
				html.Span(html.TextConfig{},
					html.Span(html.TextConfig{},
						render.Tag("strong", nil, render.Text("kiln"))),
					html.Span(html.TextConfig{Class: "muted"}, render.Text(" — agent build mode")),
				),
			),
			html.Heading(html.HeadingConfig{Level: 1},
				render.Text("Talk an app into "),
				html.Span(html.TextConfig{Class: "amber"}, render.Text("being")),
				render.Text("."),
			),
			render.Tag("p", map[string]string{"class": "lede"},
				render.Text("Kiln is a separate binary that mounts a chat panel on your running GoFastr app. The agent calls a typed tool surface; the in-memory IR mutates; the schema migrates; the app re-renders — all in-process. Freeze the journal when done to emit canonical entities/*.json you commit."),
			),
			html.Div(html.DivConfig{Class: "k-hero__ctas"},
				html.Link(html.LinkConfig{Href: "/docs/kiln", Text: "Read the docs", Class: "btn btn--primary btn--lg"}),
				html.Div(html.DivConfig{Class: "k-hero__cli"},
					html.Span(html.TextConfig{Class: "p"}, render.Text("$")),
					render.Text("go install github.com/DonaldMurillo/gofastr/cmd/kiln@latest"),
				),
			),
		),
	)
}

func kDemo() render.HTML {
	chrome := html.Div(html.DivConfig{Class: "k-demo__chrome"},
		html.Div(html.DivConfig{Class: "dots"},
			html.Span(html.TextConfig{}), html.Span(html.TextConfig{}), html.Span(html.TextConfig{}),
		),
		html.Div(html.DivConfig{Class: "url"}, render.Text("localhost:8080/_/kiln")),
	)
	ghost := html.Div(html.DivConfig{Class: "ghost"},
		html.Heading(html.HeadingConfig{Level: 3}, render.Text("Your app — being authored live")),
		html.Div(html.DivConfig{Class: "ghost-row m"}),
		html.Div(html.DivConfig{Class: "ghost-row s"}),
		html.Div(html.DivConfig{Class: "ghost-row m"}),
		html.Div(html.DivConfig{Class: "ghost-row"}),
		html.Div(html.DivConfig{Class: "ghost-row s"}),
		html.Div(html.DivConfig{Class: "ghost-row m"}),
	)
	km := func(who, role, body string, tool string) render.HTML {
		whoCls := "km__who"
		if role == "agent" {
			whoCls += " agent"
		} else {
			whoCls += " you"
		}
		children := []render.HTML{
			html.Span(html.TextConfig{Class: whoCls}, render.Text(who)),
			html.Div(html.DivConfig{Class: "km__body"}, render.Text(body)),
		}
		if tool != "" {
			children = append(children, html.Span(html.TextConfig{Class: "km__tool"}, render.Text(tool)))
		}
		return html.Div(html.DivConfig{Class: "km"}, children...)
	}
	kp := html.Div(html.DivConfig{Class: "kpanel"},
		html.Div(html.DivConfig{Class: "kpanel__head"},
			html.Span(html.TextConfig{Class: "dot"}),
			render.Text("kiln"),
			html.Span(html.TextConfig{Class: "session"}, render.Text("session #a1b3")),
		),
		html.Div(html.DivConfig{Class: "kpanel__chat"},
			km("you", "you", "add a posts entity with title, body, status enum", ""),
			km("claude-code", "agent", "Reading your existing schema. Three new tools required, no destructive changes.", "tool: add_entity"),
			km("you", "you", "looks good, ship it", ""),
			km("claude-code", "agent", "Proposing a plan. Approve below to apply.", "tool: propose_plan"),
		),
		html.Div(html.DivConfig{Class: "kpanel__plan"},
			html.Div(html.DivConfig{Class: "lbl"}, render.Text("Plan #4 · 3 ops")),
			html.Span(html.TextConfig{Class: "op add"}, render.Text("+ add_entity(\"posts\")")),
			html.Span(html.TextConfig{Class: "op add"}, render.Text("+ add_field(\"posts\", title)")),
			html.Span(html.TextConfig{Class: "op add"}, render.Text("+ migrate_up()")),
			html.Div(html.DivConfig{Class: "actions"},
				render.Tag("button", map[string]string{"type": "button", "class": "approve"}, render.Text("approve")),
				render.Tag("button", map[string]string{"type": "button", "class": "reject"}, render.Text("reject")),
			),
		),
		html.Div(html.DivConfig{Class: "kpanel__input"},
			render.Tag("input", map[string]string{"type": "text", "placeholder": "Ask the agent…"}, render.Raw("")),
		),
	)
	frame := html.Div(html.DivConfig{Class: "k-demo__frame"},
		chrome,
		html.Div(html.DivConfig{Class: "k-demo__body"}, ghost, kp),
	)
	return html.Section(html.SectionConfig{Class: "k-demo", Label: "Demo"}, container(frame))
}

func kTimeline() render.HTML {
	evt := func(t, variant, title, body string) render.HTML {
		cls := "tl-evt"
		if variant != "" {
			cls += " " + variant
		}
		return html.Div(html.DivConfig{Class: cls},
			html.Span(html.TextConfig{Class: "tl-evt__t"}, render.Text(t)),
			html.Div(html.DivConfig{Class: "tl-evt__dot"}, html.Span(html.TextConfig{}, render.Raw(""))),
			html.Div(html.DivConfig{},
				render.Tag("strong", nil, render.Text(title)),
				render.Tag("p", nil, render.Text(body)),
			),
		)
	}
	return html.Section(html.SectionConfig{Class: "timeline", Label: "Session timeline"},
		container(
			html.Div(html.DivConfig{Class: "mb-lg"}, tagAccent("Anatomy of a session")),
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Seven events from prompt to commit")),
			html.Div(html.DivConfig{Class: "tl-rail"},
				evt("0s", "", "Agent connects", "kiln subscribes to its own SSE bus and spawns the configured CLI."),
				evt("3s", "tool", "Agent calls world_get", "Reads the in-memory IR — current entities, fields, hooks, routes."),
				evt("8s", "tool", "Agent calls add_entity", "Mutates the IR: posts(title, body, status). No DB write yet."),
				evt("12s", "tool", "Agent calls propose_plan", "Lists destructive targets (none) and the three add_* operations."),
				evt("18s", "approve", "You click Approve", "Plan id is stamped onto the agent's retry call."),
				evt("19s", "", "Migration runs", "Up-migration generated and applied; entities/posts.json materialized."),
				evt("25s", "", "Journal freezable", "kiln freeze --dir build/ emits the canonical entities you commit."),
			),
		),
	)
}

func kCaps() render.HTML {
	can := html.Div(html.DivConfig{Class: "cap can"},
		html.Heading(html.HeadingConfig{Level: 3}, render.Text("What the agent can do")),
		html.UnorderedList(html.ListConfig{},
			html.ListItem(html.ListItemConfig{}, render.Text("Add entities, fields, hooks, routes")),
			html.ListItem(html.ListItemConfig{}, render.Text("Migrate up + seed data")),
			html.ListItem(html.ListItemConfig{}, render.Text("Edit pages and screens (non-destructively)")),
			html.ListItem(html.ListItemConfig{}, render.Text("Inspect logs, run queries, browse docs")),
		),
	)
	cant := html.Div(html.DivConfig{Class: "cap cant"},
		html.Heading(html.HeadingConfig{Level: 3}, render.Text("Without an approved plan")),
		html.UnorderedList(html.ListConfig{},
			html.ListItem(html.ListItemConfig{}, render.Text("Drop entities, fields, hooks, routes")),
			html.ListItem(html.ListItemConfig{}, render.Text("Migrate down")),
			html.ListItem(html.ListItemConfig{}, render.Text("Touch credentials, secrets, .env")),
			html.ListItem(html.ListItemConfig{}, render.Text("Spawn external processes you didn't allow")),
		),
	)
	return html.Section(html.SectionConfig{Class: "caps", Label: "Capabilities"},
		container(
			html.Div(html.DivConfig{Class: "mb-lg"}, tagAccent("Plan-gated destructive ops")),
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("The agent acts within explicit limits")),
			html.Div(html.DivConfig{Class: "caps__grid"}, can, cant),
		),
	)
}

func kCli() render.HTML {
	cmd := func(label string, lines ...render.HTML) render.HTML {
		return html.Div(html.DivConfig{Class: "cli-cmd"},
			html.Div(html.DivConfig{Class: "cli-cmd__head"}, render.Text(label)),
			html.Div(html.DivConfig{Class: "cli-cmd__body"}, lines...),
		)
	}
	p := func(s string) render.HTML { return html.Span(html.TextConfig{Class: "p"}, render.Text(s)) }
	o := func(s string) render.HTML { return html.Span(html.TextConfig{Class: "o"}, render.Text(s)) }
	ok := func(s string) render.HTML { return html.Span(html.TextConfig{Class: "ok"}, render.Text(s)) }
	return html.Section(html.SectionConfig{Class: "cli-sect", Label: "CLI"},
		container(
			html.Heading(html.HeadingConfig{Level: 2},
				render.Text("Two binaries. "),
				html.Span(html.TextConfig{Class: "amber"}, render.Text("Three lines of setup")),
				render.Text("."),
			),
			render.Tag("p", map[string]string{"class": "lede"},
				render.Text("Install the kiln binary alongside the gofastr CLI. Pick the agent CLI you already use; kiln spawns it as a subprocess with KILN_URL injected.")),
			html.Div(html.DivConfig{Class: "cli-block"},
				cmd("install",
					p("$"), render.Text(" go install github.com/DonaldMurillo/gofastr/cmd/kiln@latest\n"),
					ok("→ installed kiln v0.0.4\n"),
				),
				cmd("serve",
					p("$"), render.Text(" kiln serve --agent claude-code\n"),
					o("→ panel mounted at http://localhost:8765/_/kiln\n"),
					o("→ MCP server live at /_/mcp\n"),
					ok("→ ready · waiting for the agent.\n"),
				),
			),
		),
	)
}

// =============================================================================
// /philosophy
// =============================================================================

type PhilosophyScreen struct{}

func (s *PhilosophyScreen) ScreenTitle() string { return "Philosophy — GoFastr" }
func (s *PhilosophyScreen) ScreenDescription() string {
	return "The convictions behind GoFastr — what we say no to, and why."
}
func (s *PhilosophyScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *PhilosophyScreen) Render() render.HTML {
	return render.Join(phHero(), phBody())
}

func phHero() render.HTML {
	return html.Section(html.SectionConfig{Class: "ph-hero", Label: "Philosophy"},
		container(
			html.Div(html.DivConfig{Class: "ph-hero__grid"},
				html.Div(html.DivConfig{Class: "meta"},
					render.Text("Volume 01"), render.Raw("<br>"),
					render.Text("Essay"), render.Raw("<br>"),
					render.Text("2026-05"),
				),
				html.Heading(html.HeadingConfig{Level: 1},
					render.Text("Why this framework "),
					html.Span(html.TextConfig{Class: "amber"}, render.Text("exists")),
					render.Text("."),
				),
				html.Div(html.DivConfig{Class: "by"},
					render.Text("By Donald Murillo"), render.Raw("<br>"),
					render.Text("Updated 2026-05-26"),
				),
			),
		),
	)
}

func phBody() render.HTML {
	tocLi := func(href, text string) render.HTML {
		return html.ListItem(html.ListItemConfig{},
			html.Link(html.LinkConfig{Href: href, Text: text}),
		)
	}
	toc := render.Tag("aside", map[string]string{"class": "ph-toc"},
		render.Tag("h6", nil, render.Text("Sections")),
		render.Tag("ol", nil,
			tocLi("#why", "Why this exists"),
			tocLi("#two-layers", "The two layers"),
			tocLi("#convictions", "Convictions"),
			tocLi("#agents", "Where agents fit"),
			tocLi("#next", "What's next"),
			tocLi("#colophon", "A note on this site"),
		),
	)
	conv := func(num, title, desc string) render.HTML {
		return html.Div(html.DivConfig{Class: "conv"},
			html.Span(html.TextConfig{Class: "num"}, render.Text(num)),
			html.Div(html.DivConfig{},
				html.Div(html.DivConfig{Class: "title"}, render.Text(title)),
				html.Div(html.DivConfig{Class: "desc"}, render.Text(desc)),
			),
		)
	}
	roadRow := func(when, what, status, statusText string) render.HTML {
		return html.Div(html.DivConfig{Class: "roadmap__row"},
			html.Span(html.TextConfig{Class: "roadmap__when"}, render.Text(when)),
			html.Span(html.TextConfig{Class: "roadmap__what"}, render.Text(what)),
			html.Span(html.TextConfig{Class: "roadmap__status " + status}, render.Text(statusText)),
		)
	}
	article := render.Tag("article", map[string]string{"class": "ph-article"},
		render.Tag("p", map[string]string{"class": "lede"},
			render.Text("Most web frameworks assume a human will hand-write every route, query, validator, migration, and form. AI agents already generate that code — but no framework treats their output as the canonical source. GoFastr inverts that. The agent is a first-class author. The human is too. The framework is what they both write to."),
		),
		html.Section(html.SectionConfig{ID: "why", Label: "Why this exists"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Why this exists")),
			render.Tag("p", nil, render.Text("In 2026, you can describe an app and have it generated. The output is usually a tangle: hand-rolled handlers, magic ORMs, custom-DSL config files, and an opaque server runtime that fights both you and the agent. The next thing you do is throw most of it away.")),
			render.Tag("p", nil, render.Text("The pattern is fixable. If the framework names what an entity is — a typed declaration that becomes SQL, REST, MCP tools, OpenAPI, and a typed Go model — then the agent's output is the declaration. Everything else is read-only generated code you can grep, debug, and step through.")),
		),
		render.Tag("blockquote", map[string]string{"class": "pullquote"},
			render.Text("The right abstraction makes the simple case trivial and the complex case possible. The wrong abstraction makes both unreadable."),
		),
		html.Section(html.SectionConfig{ID: "two-layers", Label: "Two layers"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("The two layers")),
			render.Tag("p", nil,
				render.Text("Two packages, no more. "), codeText("core/"), render.Text(" is twelve stdlib-only Go primitives — router, query, schema, mcp, openapi — each independently usable. "), codeText("framework/"), render.Text(" is the opinionated entity layer composed on top. When the framework is in your way, you drop down to core and write plain Go.")),
			render.Tag("p", nil, render.Text("No reflection magic. Generated code is regular Go you can read. The framework's job is to make the typed declaration so expressive that the generated code is shorter than the framework call that produced it.")),
		),
		html.Section(html.SectionConfig{ID: "convictions", Label: "Convictions"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Convictions")),
			html.Div(html.DivConfig{Class: "conv-list"},
				conv("01", "Declare once, generate many surfaces", "Database, REST, MCP, OpenAPI, typed Go — all from one source."),
				conv("02", "No reflection magic", "If the framework looks like it's doing something opaque, open the generated file."),
				conv("03", "Drop down to core", "If the framework is in your way, the layer below is twelve packages of stdlib-only Go."),
				conv("04", "Batteries included, not embedded", "Auth, cache, email, queue, search, storage — narrow interfaces, swappable drivers."),
				conv("05", "AI agents are first-class authors", "MCP tools, Kiln, agent notes. Every entity ships an agent-facing surface from day one."),
				conv("06", "Strong opinions, small scope", "Some things we explicitly will not do."),
			),
		),
		html.Section(html.SectionConfig{ID: "agents", Label: "Where agents fit"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Where agents fit")),
			render.Tag("p", nil, render.Text("Agents drive the framework the same way humans do. The MCP tool surface is just the REST surface in a different shape; the typed Kiln tools are the framework's mutate API exposed for code-generating agents. Destructive operations require an approved plan — the agent cannot drop your tables without you clicking Approve.")),
			render.Tag("p", nil, render.Text("The framework also leaves clear breadcrumbs for the agent: doc files embedded in the binary, structured MCP introspection at /_/mcp, agent-notes for review history. An agent that connects to a running GoFastr app can read its own state and reason about it.")),
		),
		html.Section(html.SectionConfig{ID: "next", Label: "What's next"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("What's next")),
			html.Div(html.DivConfig{Class: "roadmap"},
				render.Tag("h6", nil, render.Text("Roadmap")),
				roadRow("Shipped", "Two-layer core/ + framework/ split", "shipped", "✓ shipped"),
				roadRow("Shipped", "Auto-CRUD + MCP + OpenAPI", "shipped", "✓ shipped"),
				roadRow("Shipped", "Kiln agent build mode (experimental)", "shipped", "✓ shipped"),
				roadRow("Q3 2026", "Lock framework/entity ABI", "next", "next"),
				roadRow("Q4 2026", "Land core-ui 1.0", "later", "later"),
				roadRow("2027", "First version we'd suggest shipping to customers", "later", "later"),
			),
		),
		html.Section(html.SectionConfig{ID: "colophon", Label: "Colophon"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("A note on this site")),
			render.Tag("p", nil, render.Text("This site is built with GoFastr itself. Every interactive element is a registered component; the CSS is generated by the typed style.StyleSheet DSL against the theme; every page is server-rendered with the same runtime any consumer of the framework gets.")),
			render.Tag("p", nil, render.Text("If something on this site doesn't work, the bug is in the framework — and the fix lands here first, then everywhere else.")),
		),
		html.Div(html.DivConfig{Class: "biblio"},
			render.Tag("h6", nil, render.Text("Notes & references")),
			html.DescriptionList(html.TextConfig{},
				html.DescriptionTerm(html.TextConfig{}, render.Text("01")),
				html.DescriptionDetail(html.TextConfig{}, render.Text("The framework's principles trace from net/http: pattern routing, middleware chains, explicit handler signatures.")),
				html.DescriptionTerm(html.TextConfig{}, render.Text("02")),
				html.DescriptionDetail(html.TextConfig{}, render.Text("MCP — Anthropic's Model Context Protocol; used as the agent-facing surface.")),
				html.DescriptionTerm(html.TextConfig{}, render.Text("03")),
				html.DescriptionDetail(html.TextConfig{}, render.Text("The two-layer pattern echoes Rich Hickey's distinction between simple and easy.")),
			),
		),
	)
	return container(html.Div(html.DivConfig{Class: "ph-body"}, toc, article))
}

// =============================================================================
// /404  (registered as the custom NotFound handler — see main.go)
// =============================================================================

type NotFoundScreen struct{}

func (s *NotFoundScreen) ScreenTitle() string        { return "404 — Not found" }
func (s *NotFoundScreen) ScreenDescription() string  { return "" }
func (s *NotFoundScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *NotFoundScreen) Render() render.HTML {
	o := func(s string) render.HTML { return html.Span(html.TextConfig{Class: "o"}, render.Text(s)) }
	p := func(s string) render.HTML { return html.Span(html.TextConfig{Class: "p"}, render.Text(s)) }
	e := func(s string) render.HTML { return html.Span(html.TextConfig{Class: "e"}, render.Text(s)) }
	ok := func(s string) render.HTML { return html.Span(html.TextConfig{Class: "ok"}, render.Text(s)) }

	left := html.Div(html.DivConfig{},
		html.Div(html.DivConfig{Class: "nf__num"},
			render.Text("4"),
			html.Span(html.TextConfig{}, render.Text("0")),
			render.Text("4"),
		),
		html.Heading(html.HeadingConfig{Level: 1, Class: "nf__title"},
			render.Text("Router didn't "),
			html.Span(html.TextConfig{Class: "amber"}, render.Text("match")),
			render.Text("."),
		),
		render.Tag("p", map[string]string{"class": "nf__lede"},
			render.Text("The requested path didn't map to any registered screen. Below: what the router tried, three near-hits, and a search box."),
		),
		html.Div(html.DivConfig{Class: "nf__path"},
			html.Span(html.TextConfig{Class: "u"}, render.Text("/")),
			render.Text("requested-but-missing/path"),
		),
	)

	right := html.Div(html.DivConfig{},
		html.Div(html.DivConfig{Class: "nf__term"},
			html.Div(html.DivConfig{Class: "nf__term-head"},
				html.Span(html.TextConfig{Class: "dot"}),
				render.Text("router trace"),
			),
			html.Div(html.DivConfig{Class: "nf__term-body"},
				p("$ router.Match\n"),
				o("→ trying  /requested-but-missing/path\n"),
				e("→ miss   no exact match\n"),
				o("→ trying  /requested-but-missing/*\n"),
				e("→ miss   no prefix subtree\n"),
				o("→ fallback handler:\n"),
				ok("→ examples-v2: 3 near-hits\n"),
			),
		),
		html.Div(html.DivConfig{Class: "nf__suggest"},
			render.Tag("h6", nil, render.Text("Did you mean")),
			html.UnorderedList(html.ListConfig{},
				html.ListItem(html.ListItemConfig{}, html.LinkHTML(html.LinkHTMLConfig{Href: "/get-started",
					Content: render.Join(render.Text("Get started"), html.Span(html.TextConfig{Class: "arrow"}, render.Text("→")))})),
				html.ListItem(html.ListItemConfig{}, html.LinkHTML(html.LinkHTMLConfig{Href: "/docs/",
					Content: render.Join(render.Text("Docs index"), html.Span(html.TextConfig{Class: "arrow"}, render.Text("→")))})),
				html.ListItem(html.ListItemConfig{}, html.LinkHTML(html.LinkHTMLConfig{Href: "/examples",
					Content: render.Join(render.Text("Examples"), html.Span(html.TextConfig{Class: "arrow"}, render.Text("→")))})),
				html.ListItem(html.ListItemConfig{}, html.LinkHTML(html.LinkHTMLConfig{Href: "/",
					Content: render.Join(render.Text("Home"), html.Span(html.TextConfig{Class: "arrow"}, render.Text("→")))})),
			),
		),
	)

	return html.Div(html.DivConfig{Class: "nf-page"},
		html.Div(html.DivConfig{Class: "nf"}, left, right),
	)
}
