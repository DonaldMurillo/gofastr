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
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// codeText — shared inline <code> span used by most pages.
func codeText(s string) render.HTML { return html.Code(html.TextConfig{}, render.Text(s)) }

// boolPtr returns a pointer to b — used for *bool config fields like
// ui.CalloutConfig.Landmark where nil means "default" and false must be
// distinguishable from unset.
func boolPtr(b bool) *bool { return &b }

// tagAccent — the version pill used in multiple page heroes. Thin adapter
// over the framework's ui.StatusPill (accent tone + dot).
func tagAccent(label string) render.HTML {
	return ui.StatusPill(ui.StatusPillConfig{Label: label, Tone: ui.StatusPillAccent, Dot: true})
}

// experimentalPill is the sitewide "this is experimental" marker for
// Kiln surfaces. Neutral tone + dot so it reads as a status without
// competing with the amber brand pills. Kiln is the framework's most
// provisional surface — its in-memory IR, journal-freeze format, and
// blueprint graduation flow may still change. Used on the Kiln hero
// and the get-started "Try Kiln" card; list/index entries (footer,
// palette, docs catalog) carry the word inline instead.
func experimentalPill() render.HTML {
	return ui.StatusPill(ui.StatusPillConfig{Label: "Experimental", Dot: true})
}

// =============================================================================
// /get-started
// =============================================================================

type GetStartedScreen struct{}

func (s *GetStartedScreen) ScreenTitle() string { return "Get started" }
func (s *GetStartedScreen) ScreenDescription() string {
	return "Cold machine to a running GoFastr app in four minutes."
}
func (s *GetStartedScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *GetStartedScreen) Render() render.HTML {
	return render.Join(gsHero(), gsBody(), gsNext())
}

func gsHero() render.HTML {
	// Facts as a definition list — label-left in mono caption, value-
	// right in body type. The previous 2×2 card grid (FactBox tiles)
	// was a SaaS "feature card" pattern that fought the engineer-
	// voice brief.
	dt := func(s string) render.HTML {
		return html.DescriptionTerm(html.TextConfig{}, render.Text(s))
	}
	dd := func(body ...render.HTML) render.HTML {
		return html.DescriptionDetail(html.TextConfig{}, body...)
	}
	facts := html.DescriptionList(html.TextConfig{Class: "gs-facts"},
		dt("Prereqs"), dd(render.Text("Go 1.26+, git")),
		dt("OS"), dd(render.Text("macOS, Linux, Windows (WSL)")),
		dt("Storage"), dd(render.Text("SQLite by default, Postgres opt-in")),
		dt("Time"), dd(render.Text("~4 minutes")),
	)

	copy := html.Div(html.DivConfig{Class: "mb-lg"},
		html.Div(html.DivConfig{Class: "mb-lg"}, tagAccent("Get started · v"+siteVersion)),
		html.Heading(html.HeadingConfig{Level: 1},
			render.Text("From cold machine to a running app in four minutes."),
		),
		html.Paragraph(html.TextConfig{Class: "lede"},
			render.Text("Install the CLI, scaffold an app, declare an entity, run it. Every command in this guide is real — paste it into a terminal and it works."),
		),
	)
	return html.Section(html.SectionConfig{Class: "gs-hero", Label: "Get started"},
		container(ui.HeroSplit(ui.HeroSplitConfig{
			Copy:  copy,
			Media: facts,
			Class: "hero-gs",
		})),
	)
}

func gsBody() render.HTML {
	rail := ui.StepRail(ui.StepRailConfig{
		Title: "The path",
		Items: []ui.StepRailItem{
			{Number: "01", Anchor: "s1", Label: "Install"},
			{Number: "02", Anchor: "s2", Label: "Scaffold"},
			{Number: "03", Anchor: "s3", Label: "First entity"},
			{Number: "04", Anchor: "s4", Label: "Run it"},
			{Number: "05", Anchor: "s5", Label: "First page"},
			{Number: "06", Anchor: "s6", Label: "What you have"},
		},
		ActiveIndex: 0,
		Meta:        "Stuck? Ask in GitHub Discussions",
		MetaHref:    "https://github.com/DonaldMurillo/gofastr/discussions",
	})

	step := func(id, num, title, time string, body ...render.HTML) render.HTML {
		head := html.Div(html.DivConfig{Class: "step__head"},
			html.Span(html.TextConfig{Class: "step__num"}, render.Text(num)),
			html.Heading(html.HeadingConfig{Level: 2, Class: "step__title"}, render.Text(title)),
			html.Span(html.TextConfig{Class: "step__time"}, render.Text(time)),
		)
		inner := []render.HTML{head, html.Div(html.DivConfig{Class: "step__body"}, body...)}
		return html.Section(html.SectionConfig{ID: id, Class: "step", Label: title}, inner...)
	}

	// CLI mocks are now ui.TerminalBlock (label + dot header, mono body) with
	// the framework's line-tone helpers. Thin local aliases keep call sites
	// terse.
	termBlock := func(label string, lines ...render.HTML) render.HTML {
		return ui.TerminalBlock(ui.TerminalBlockConfig{Label: label}, lines...)
	}
	o := ui.TerminalOut
	ok := ui.TerminalOK

	// Inline tips inside the main content flow: render as a styled <div>, not
	// a complementary <aside> landmark, so they don't trip
	// landmark-complementary-is-top-level (a nested complementary landmark).
	callout := func(title, body string) render.HTML {
		return ui.Callout(
			ui.CalloutConfig{Title: title, Variant: ui.StatusInfo, Landmark: boolPtr(false)},
			html.Paragraph(html.TextConfig{}, render.Text(body)),
		)
	}

	step1 := step("s1", "01", "Install", "~30s",
		html.Paragraph(html.TextConfig{}, render.Text("One binary covers scaffold, migrate, dev, build, test, and the doc browser. Get it from GitHub:")),
		termBlock("$ install",
			render.Text("$ go install github.com/DonaldMurillo/gofastr/cmd/gofastr@latest\n"),
			o("$ gofastr --version\n"),
			ok("gofastr v"+siteVersion+"\n"),
		),
		callout("If gofastr isn't found", "Make sure $GOPATH/bin (or ~/go/bin) is in your PATH. Run echo $PATH and add the missing entry to your shell rc."),
	)

	step2 := step("s2", "02", "Scaffold", "~45s",
		html.Paragraph(html.TextConfig{}, render.Text("Scaffold a new project. It writes main.go, a sample posts entity in entities/, a home screen in screens/, a versioned migration, DESIGN.md, a gofastr.yml project config, and the agent onboarding files (AGENTS.md + agents/, CLAUDE.md) — then initializes git.")),
		termBlock("$ scaffold",
			render.Text("$ gofastr init blog\n"),
			ok("→ created blog/ — main.go, entities/, screens/, gofastr.yml, DESIGN.md, AGENTS.md, CLAUDE.md\n"),
			ok("→ next: cd blog && go mod tidy && gofastr dev\n"),
		),
		html.Paragraph(html.TextConfig{}, render.Text("Open the scaffolded "), codeText("main.go"), render.Text(" — it's short, it's yours, and every registration in it is plain Go. Read it.")),
	)

	step3 := step("s3", "03", "The entity", "~60s",
		html.Paragraph(html.TextConfig{}, render.Text("The scaffold already declared one. Open "), codeText("entities/entities.go"), render.Text(" — one declaration is the table, REST CRUD, validation, an OpenAPI spec, and a typed query builder:")),
		codeBlock("blog/entities/entities.go", []render.HTML{
			ln(render.Text("app."), fn_("Entity"), pn("("), str_(`"posts"`), pn(","), render.Text(" entity."), ty("EntityConfig"), pn("{")),
			ln(render.Text("  Fields"), pn(":"), render.Text(" []schema."), ty("Field"), pn("{")),
			ln(render.Text("    "), pn("{"), render.Text("Name"), pn(":"), render.Text(" "), str_(`"title"`), pn(","), render.Text(" Type"), pn(":"), render.Text(" schema."), ty("String"), pn(","), render.Text(" Required"), pn(":"), render.Text(" "), kw("true"), pn("},")),
			ln(render.Text("    "), pn("{"), render.Text("Name"), pn(":"), render.Text(" "), str_(`"body"`), pn(","), render.Text(" Type"), pn(":"), render.Text(" schema."), ty("Text"), pn("},")),
			ln(render.Text("    "), pn("{"), render.Text("Name"), pn(":"), render.Text(" "), str_(`"published"`), pn(","), render.Text(" Type"), pn(":"), render.Text(" schema."), ty("Bool"), pn("},")),
			ln(render.Text("  "), pn("},")),
			ln(render.Text("  CRUD"), pn(":"), render.Text(" boolPtr("), kw("true"), pn("),")),
			ln(pn("})")),
		}),
		html.Paragraph(html.TextConfig{}, render.Text("The matching versioned migration is next to it in the same file — "), codeText("gofastr docs migrations"), render.Text(" covers how those run.")),
	)

	step4 := step("s4", "04", "Run it", "~60s",
		html.Paragraph(html.TextConfig{}, render.Text("Resolve dependencies once, then start the dev server — it rebuilds on save, reloads the browser, and hands your coding agent the app over MCP.")),
		termBlock("$ run",
			render.Text("$ go mod tidy\n"),
			render.Text("$ gofastr dev\n"),
			ok("→ Watching . for changes (.go, .js, .css, .html, .md)...\n"),
			ok("→ blog server ready — http://localhost:8080\n"),
		),
		html.Paragraph(html.TextConfig{}, render.Text("Open "), codeText("localhost:8080"), render.Text(" — the scaffolded home screen renders. Then hit the API from a second terminal:")),
		termBlock("$ probe",
			o("$ curl -s http://localhost:8080/posts\n"),
			ok("{\"error\":\"authentication required\",\"success\":false,…}   # 401\n"),
		),
		html.Paragraph(html.TextConfig{}, render.Text("That 401 is the point: auto-CRUD refuses anonymous requests unless you opt out. Add "), codeText("Public: true"), render.Text(" to the entity, save, and the dev server rebuilds — the same curl now answers "), codeText("{\"data\":[]}"), render.Text(". Wiring real login instead is the auth battery ("), codeText("gofastr docs auth"), render.Text(").")),
	)

	step5 := step("s5", "05", "First page", "~60s",
		html.Paragraph(html.TextConfig{}, render.Text("Add a second server-rendered page. A screen is a Go struct whose Render returns the markup — the scaffolded home screen in "), codeText("screens/home.go"), render.Text(" is the pattern:")),
		codeBlock("blog/screens/about.go", []render.HTML{
			ln(kw("type"), render.Text(" "), ty("AboutScreen"), render.Text(" "), kw("struct"), pn("{}")),
			ln(render.Text("")),
			ln(kw("func"), render.Text(" (s "), pn("*"), ty("AboutScreen"), pn(")"), render.Text(" "), fn_("ScreenTitle"), pn("()"), render.Text(" "), ty("string"), pn(" {"), render.Text(" "), kw("return"), render.Text(" "), str_(`"About"`), pn(" }")),
			ln(kw("func"), render.Text(" (s "), pn("*"), ty("AboutScreen"), pn(")"), render.Text(" "), fn_("Render"), pn("()"), render.Text(" "), ty("render.HTML"), pn(" {")),
			ln(render.Text("  "), kw("return"), render.Text(" ui."), fn_("PageHeader"), pn("("), render.Text("ui."), ty("PageHeaderConfig"), pn("{"), render.Text("Title"), pn(":"), render.Text(" "), str_(`"About"`), pn("})")),
			ln(pn("}")),
		}),
		html.Paragraph(html.TextConfig{}, render.Text("Register it in "), codeText("main.go"), render.Text(" next to the home screen — "), codeText(`site.Register("/about", &screens.AboutScreen{}, nil)`), render.Text(" — save, and the dev server serves it.")),
		callout("Tip", "Run `gofastr docs` to browse the embedded docs offline — entity-declarations, query-dsl, hooks, all of it."),
	)

	step6 := step("s6", "06", "What you have", "now",
		html.Paragraph(html.TextConfig{}, render.Text("Four minutes in, this is on disk and running:")),
		html.Div(html.DivConfig{Class: "result"},
			html.Heading(html.HeadingConfig{Level: 3}, render.Text("Running, on disk, yours")),
			html.UnorderedList(html.ListConfig{},
				html.ListItem(html.ListItemConfig{}, render.Text("A server-rendered home screen (plus yours from step 5)")),
				html.ListItem(html.ListItemConfig{}, render.Text("A posts entity: REST CRUD, session-gated by default")),
				html.ListItem(html.ListItemConfig{}, render.Text("A versioned SQL migration, already applied")),
				html.ListItem(html.ListItemConfig{}, render.Text("An OpenAPI 3 spec (auth-gated; Swagger UI at /api/docs/)")),
				html.ListItem(html.ListItemConfig{}, render.Text("MCP under gofastr dev: posts_list/posts_create plus app_routes, framework_docs_search — your coding agent reads the running app")),
				html.ListItem(html.ListItemConfig{}, render.Text("AGENTS.md + agents/ + DESIGN.md, generated for the agent you build with")),
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
	card := func(meta, title, desc, href string, badge ...render.HTML) render.HTML {
		return html.LinkHTML(html.LinkHTMLConfig{
			Href:  href,
			Class: "ex-card",
			Content: render.Join(
				html.Span(html.TextConfig{Class: "path"}, render.Text(meta)),
				html.Heading(html.HeadingConfig{Level: 3}, render.Text(title)),
				render.Join(badge...),
				html.Paragraph(html.TextConfig{}, render.Text(desc)),
			),
		})
	}
	return html.Section(html.SectionConfig{Class: "next", Label: "What now"},
		container(
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Where next")),
			html.Div(html.DivConfig{Class: "next__grid"},
				card("/docs/", "Browse the docs", fmt.Sprintf("%d docs grouped by what you're trying to do.", docCount()), "/docs/"),
				card("/examples", "Read an example", fmt.Sprintf("%d full apps you can clone and modify.", len(exRowItems())), "/examples"),
				card("/kiln", "Try Kiln", "Build the app by chatting with an agent; freeze the result into a blueprint you commit.", "/kiln", experimentalPill()),
			),
		),
	)
}

// =============================================================================
// /docs/  (concepts index)
// =============================================================================

type ConceptsIndexScreen struct{}

func (s *ConceptsIndexScreen) ScreenTitle() string { return "Docs" }
func (s *ConceptsIndexScreen) ScreenDescription() string {
	return "Every feature, grouped by what you're trying to do — not alphabetically."
}
func (s *ConceptsIndexScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ConceptsIndexScreen) Render() render.HTML {
	return render.Join(cxHero(), cxBody())
}

func cxHero() render.HTML {
	// Stats as a single mono inline line — "53 docs · 6 intents · 31
	// packages". Replaces the previous KPI tile band, which was a
	// SaaS-dashboard pattern at odds with the engineer-voice brief
	// (code is the hero; metadata should be unobtrusive).
	copy := html.Div(html.DivConfig{},
		html.Div(html.DivConfig{Class: "mb-lg"}, tagAccent("Docs · v"+siteVersion)),
		html.Heading(html.HeadingConfig{Level: 1},
			render.Text("Read by what you're trying to do."),
		),
		html.Paragraph(html.TextConfig{Class: "lede"},
			render.Text("The docs are grouped by intent. Pick the one that matches the question you're holding."),
		),
		html.Paragraph(html.TextConfig{Class: "cx-stats-line"},
			render.Text(fmt.Sprintf("%d docs · %d intents", docCount(), len(docIntents))),
		),
	)
	return html.Section(html.SectionConfig{Class: "cx-hero", Label: "Docs"},
		container(copy),
	)
}

func cxBody() render.HTML {
	// Rail via ui.AnchoredRail — bundles markup + scrollspy. Driven by the
	// shared docIntents catalog so the rail, the sections, and the per-doc
	// pages can never disagree about what exists.
	items := make([]ui.RailItem, len(docIntents))
	for i, it := range docIntents {
		items[i] = ui.RailItem{
			Eyebrow: it.Num,
			Text:    it.Title,
			Anchor:  it.Slug,
			Count:   len(it.Docs),
		}
	}
	// Trailing rail entry for the flat A–Z reference section.
	items = append(items, ui.RailItem{Eyebrow: "∑", Text: "A–Z", Anchor: "all-az", Count: docCount()})
	rail := ui.AnchoredRail(ui.AnchoredRailConfig{
		Label:           "By intent",
		Items:           items,
		ObserveSelector: "#docs-sections",
		TargetSelector:  ".intent[id]",
		Class:           "intent-rail-spy",
	})

	sections := []render.HTML{}
	for _, it := range docIntents {
		sections = append(sections, intentSection(it))
	}
	// Flat A–Z reference at the bottom — every embedded doc, nothing hidden.
	sections = append(sections, allDocsSection())

	return container(html.Div(html.DivConfig{Class: "cx-body"},
		rail,
		render.Tag("div", map[string]string{"id": "docs-sections"}, sections...),
	))
}

// intentSection renders one intent group. Every doc card is an <a> to its
// /docs/<slug> page — no dead cards.
func intentSection(it docIntent) render.HTML {
	cards := []render.HTML{}
	for _, d := range it.Docs {
		cards = append(cards, html.LinkHTML(html.LinkHTMLConfig{
			Href:  "/docs/" + d.Slug,
			Class: "doc",
			Content: render.Join(
				html.Div(html.DivConfig{Class: "doc__title"}, render.Text(d.Title)),
				html.Div(html.DivConfig{Class: "doc__desc"}, render.Text(d.Desc)),
				html.Div(html.DivConfig{Class: "doc__meta"}, render.Text("/docs/"+d.Slug)),
			),
		}))
	}
	stripChildren := []render.HTML{html.Span(html.TextConfig{Class: "l"}, render.Text("Recommended path"))}
	for i, p := range it.Path {
		if i > 0 {
			stripChildren = append(stripChildren, html.Span(html.TextConfig{Class: "arrow"}, render.Text("→")))
		}
		stripChildren = append(stripChildren, html.Span(html.TextConfig{Class: "s"}, render.Text(p)))
	}
	return html.Section(html.SectionConfig{ID: it.Slug, Class: "intent", Label: it.Title},
		html.Div(html.DivConfig{Class: "intent__head"},
			html.Span(html.TextConfig{Class: "intent__num"}, render.Text(it.Num)),
			html.Heading(html.HeadingConfig{Level: 2, Class: "intent__title"}, render.Text(it.Title)),
			html.Span(html.TextConfig{Class: "intent__meta"}, render.Text(fmt.Sprintf("%d docs", len(it.Docs)))),
		),
		html.Paragraph(html.TextConfig{Class: "intent__lede"}, render.Text(it.Lede)),
		html.Div(html.DivConfig{Class: "docs"}, cards...),
		html.Div(html.DivConfig{Class: "path-strip"}, stripChildren...),
	)
}

// =============================================================================
// /examples
// =============================================================================

type ExamplesScreen struct{}

func (s *ExamplesScreen) ScreenTitle() string { return "Examples" }
func (s *ExamplesScreen) ScreenDescription() string {
	return fmt.Sprintf("%d reference apps. Each runs in one command.", len(exRowItems()))
}
func (s *ExamplesScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ExamplesScreen) Render() render.HTML {
	return render.Join(exHero(), exRows())
}

func exHero() render.HTML {
	return html.Section(html.SectionConfig{Class: "ex-hero", Label: "Examples"},
		container(
			html.Div(html.DivConfig{Class: "mb-lg"}, tagAccent(fmt.Sprintf("Examples · %d apps", len(exRowItems())))),
			html.Heading(html.HeadingConfig{Level: 1},
				render.Text(fmt.Sprintf("%d reference apps. Each runs in one command.", len(exRowItems()))),
			),
			html.Paragraph(html.TextConfig{Class: "lede"},
				render.Text("Clone the one that looks like your problem; swap the entity declarations. Each app's full source is under examples/ in the repo — copy what you need."),
			),
		),
	)
}

// exRowItems builds every example row. Copy that cites an app count
// (the hero pill, the H1, the get-started card, the meta description)
// derives it from len() of this slice, so the number can't drift from
// the content again.
func exRowItems() []render.HTML {
	return []render.HTML{
		exRow("01", "examples/meridian", "Meridian — SaaS console", "flagship", "100% generated",
			"A billing & revenue console (customers, subscriptions, invoices, MRR + charts) plus its marketing site, auth, RBAC, and an admin back-office — generated from one gofastr.yml, with writable screens (add/edit/delete) and zero hand-written app code.",
			[]string{"One blueprint → marketing + app + auth + admin", "Server-rendered DataTable / charts / forms, island RPCs", "Writable CRUD + RBAC, with a generated end-to-end test suite"},
			"cd examples/meridian && gofastr generate --from=gofastr.yml && go run .",
			// The exact, full blueprint that generates the app — embedded at
			// build time, shown verbatim in a scrolling block. Drift-guarded by
			// TestEmbeddedBlueprintsMatchSource.
			codeBlockScroll("examples/meridian/gofastr.yml", meridianBlueprintYAML, "yaml")),
		exRow("02", "examples/ecommerce", "ShopFront — storefront", "blueprint pipeline", "100% generated",
			"A complete storefront from one gofastr.yml — five related entities (categories, products, orders, order_items, reviews), a themed eight-screen UI, custom endpoints, and seed data. Nothing under app/ is hand-written.",
			[]string{"Second blueprint pipeline beside Meridian", "Owner-scoped orders and reviews", "Exercised by its own end-to-end test"},
			"cd examples/ecommerce && go run ./app",
			codeBlock("examples/ecommerce/gofastr.yml", []render.HTML{
				ln(com("# 5 entities, 8 screens, auth, seeds — one YAML")),
				ln(render.Text("$ gofastr generate   "), com("// emits ./app — plain Go")),
				ln(render.Text("$ go run ./app")),
			})),
		exRow("03", "examples/blog", "Go-declared blog", "smallest", "~120 LoC",
			"Users, posts, comments. Three entities. Start here — it's the end-to-end story in one file.",
			[]string{"Three entities declared in Go", "Auto-CRUD + Swagger UI + MCP", "SQLite by default; swap for Postgres in main.go"},
			"cd examples/blog && go run .",
			codeBlock("examples/blog/main.go", []render.HTML{
				ln(render.Text("app."), fn_("Entity"), pn("("), str_(`"users"`), pn(","), render.Text(" …"), pn(")")),
				ln(render.Text("app."), fn_("Entity"), pn("("), str_(`"posts"`), pn(","), render.Text(" …"), pn(")")),
				ln(render.Text("app."), fn_("Entity"), pn("("), str_(`"comments"`), pn(","), render.Text(" …"), pn(")")),
				ln(render.Text("app."), fn_("Start"), pn("("), str_(`":8080"`), pn(")")),
			})),
		exRow("04", "examples/site", "This site (UI showcase)", "largest", "~6000 LoC",
			"Every core-ui pattern + framework/ui component, one page each — plus the docs, SEO, multi-step wizard, and print-battery demos. The site you're reading right now.",
			[]string{"Every core-ui pattern + framework/ui component", "Docs, philosophy, examples, Kiln pages", "SEO interfaces, sitemap/robots, wizard, print"},
			"cd examples/site && go run .",
			codeBlock("examples/site/main.go", []render.HTML{
				ln(render.Text("host "), pn(":="), render.Text(" uihost."), fn_("New"), pn("("), render.Text("site"), pn(", …)")),
				ln(render.Text("app "), pn(":="), render.Text(" framework."), fn_("NewUIHostApp"), pn("("), render.Text("host"), pn(")")),
				ln(render.Text("app."), fn_("Start"), pn("("), str_(`":8083"`), pn(")")),
			})),
		exRow("05", "examples/api-tour", "API tour", "annotated source", "~180 LoC",
			"Every v2 API feature in one annotated main.go — with the curl commands to exercise each one in the file header.",
			[]string{"Cursor + offset pagination", "Eager loading (?include=…)", "Batch endpoints, SSE entity events, uploads"},
			"cd examples/api-tour && go run .",
			codeBlock("examples/api-tour/main.go", []render.HTML{
				ln(render.Text("app."), fn_("Entity"), pn("("), str_(`"posts"`), pn(","), render.Text(" …"), pn(")")),
				ln(com("// cursor + offset paging, ?include=, batch, SSE")),
				ln(render.Text("app."), fn_("Start"), pn("("), str_(`":8080"`), pn(")")),
			})),
		exRow("06", "examples/embed-demo", "Local semantic search", "no API key", "~180 LoC",
			"A markdown corpus indexed locally via battery/embed. No external API key; works offline.",
			[]string{"Brute-force cosine, hybrid keyword fusion", "Snapshot + WAL persistence", "Poll-watch for file changes"},
			"cd examples/embed-demo && go run .",
			codeBlock("examples/embed-demo/main.go", []render.HTML{
				ln(render.Text("idx, _ "), pn(":="), render.Text(" embed."), fn_("Open"), pn("("), render.Text("embed."), ty("Options"), pn("{…})")),
				ln(render.Text("idx."), fn_("Add"), pn("("), render.Text("ctx, docs…"), pn(")"), render.Text("   "), com("// local vectors")),
				ln(render.Text("hits, _ "), pn(":="), render.Text(" idx."), fn_("Query"), pn("("), render.Text("ctx, embed."), ty("Query"), pn("{"), render.Text("Text: "), str_(`"hooks"`), pn(", "), render.Text("Limit: 5"), pn("})")),
			})),
		exRow("07", "examples/spa", "Vue + GoFastr API", "BYO client", "~140 LoC server",
			"For teams who already have a client app. Shows the framework is happy to just be your typed API.",
			[]string{"Same auto-CRUD entities", "Vue 3 + Vue Router from a CDN — no npm, no build step", "No SSR — the Go app serves JSON and the static files"},
			"cd examples/spa && go run .",
			codeBlock("examples/spa/main.go", []render.HTML{
				ln(render.Text("app."), fn_("Entity"), pn("("), str_(`"posts"`), pn(","), render.Text(" …"), pn(")")),
				ln(com("// JSON API only — your Vue app is the client")),
				ln(render.Text("app."), fn_("Start"), pn("("), str_(`":8080"`), pn(")")),
			})),
		exRow("08", "examples/static-site", "Static file server", "no screens", "~60 LoC",
			"A plain file server on the framework router: static.Mount serves the pages/ directory — HTML and CSS straight from disk, no screens, no runtime JS.",
			[]string{"static.Mount with SPA mode off — only real files serve", "index.html answers /", "API routes can mount beside it on the same router"},
			"cd examples/static-site && go run .",
			codeBlock("examples/static-site/main.go", []render.HTML{
				ln(render.Text("static."), fn_("Mount"), pn("("), render.Text("app."), fn_("Router"), pn("(), static."), ty("Config"), pn("{")),
				ln(render.Text("  FS: os."), fn_("DirFS"), pn("("), render.Text("pagesDir"), pn("),")),
				ln(pn("})")),
				ln(render.Text("app."), fn_("Start"), pn("("), str_(`":3070"`), pn(")")),
			})),
	}
}

func exRows() render.HTML {
	return container(render.Join(exRowItems()...))
}

// exRow renders one example. code is the pre-built code sample (a snippet for
// most rows; the full embedded blueprint for Meridian); path names the
// directory for the "View source" link.
func exRow(num, path, title, tag, loc, desc string, points []string, cmd string, code render.HTML) render.HTML {
	slug := strings.TrimPrefix(path, "examples/")
	srcURL := "https://github.com/DonaldMurillo/gofastr/tree/main/" + path

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
	miniCode := code
	body := html.Div(html.DivConfig{Class: "ex-row__body"},
		html.Div(html.DivConfig{Class: "ex-row__meta"},
			tagAccent(tag),
			html.Span(html.TextConfig{Class: "lc"}, render.Text(loc)),
		),
		html.Heading(html.HeadingConfig{Level: 2, Class: "ex-row__title"},
			render.Text(path+" — "),
			html.Span(html.TextConfig{Class: "amber"}, render.Text(title)),
		),
		html.Paragraph(html.TextConfig{Class: "ex-row__desc"}, render.Text(desc)),
		html.UnorderedList(html.ListConfig{Class: "ex-row__points"}, pointLis...),
		html.Div(html.DivConfig{Class: "ex-row__cli"},
			html.Span(html.TextConfig{Class: "p"}, render.Text("$")),
			render.Text(cmd),
		),
		html.Div(html.DivConfig{Class: "ex-row__src"},
			html.LinkHTML(html.LinkHTMLConfig{
				Href:       srcURL,
				ExtraAttrs: html.Attrs{"rel": "external"},
				Content:    render.Text("View source ↗"),
			}),
		),
	)
	right := html.Div(html.DivConfig{Class: "ex-row__right"}, miniCode, shot)
	grid := html.Div(html.DivConfig{Class: "ex-row__grid"},
		html.Span(html.TextConfig{Class: "ex-row__num"}, render.Text(num)),
		body,
		right,
	)
	return html.Section(html.SectionConfig{ID: slug, Class: "ex-row", Label: path}, grid)
}

// =============================================================================
// /kiln
// =============================================================================

type KilnScreen struct{}

func (s *KilnScreen) ScreenTitle() string { return "Kiln" }
func (s *KilnScreen) ScreenDescription() string {
	return "Build a GoFastr app live by chatting with an agent."
}
func (s *KilnScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *KilnScreen) Render() render.HTML {
	return html.Div(html.DivConfig{Class: "kiln-page"},
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
						html.Strong(html.TextConfig{}, render.Text("kiln"))),
					html.Span(html.TextConfig{Class: "muted"}, render.Text(" — agent build mode")),
				),
				experimentalPill(),
			),
			html.Heading(html.HeadingConfig{Level: 1},
				render.Text("Talk an app into "),
				html.Span(html.TextConfig{Class: "amber"}, render.Text("being")),
				render.Text("."),
			),
			html.Paragraph(html.TextConfig{Class: "lede"},
				render.Text("Kiln is experimental — a separate binary that mounts a chat panel on your running GoFastr app. The agent calls typed tools; the in-memory IR mutates; the schema migrates; the app re-renders — all in-process. Freeze the journal when done to emit the canonical entity files you commit."),
			),
			html.Div(html.DivConfig{Class: "k-hero__ctas"},
				ui.LinkButton(ui.LinkButtonConfig{Label: "Read the docs", Href: "/docs/kiln", Variant: ui.ButtonPrimary, Size: ui.ButtonSizeLarge}),
				// tabindex: the command scrolls horizontally at narrow
				// widths (wrapping breaks copy-paste), so the scroll
				// region must be keyboard-reachable — same treatment as
				// the home hero's install line.
				html.Div(html.DivConfig{Class: "k-hero__cli", ExtraAttrs: html.Attrs{"tabindex": "0"}},
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
		html.Div(html.DivConfig{Class: "url"}, render.Text("localhost:8765")),
	)
	// Left pane is a stylized wireframe of the app under construction —
	// NOT a live load. The bars are decorative (aria-hidden) and the
	// caption says so, so it doesn't read as a skeleton stuck loading.
	ghost := html.Div(html.DivConfig{Class: "ghost"},
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Your app — being authored live")),
		html.Paragraph(html.TextConfig{Class: "ghost__cap"},
			render.Text("Illustration — your real app renders here as the agent edits it.")),
		render.Tag("div", map[string]string{"class": "ghost__wire", "aria-hidden": "true"},
			html.Div(html.DivConfig{Class: "ghost-row m"}),
			html.Div(html.DivConfig{Class: "ghost-row s"}),
			html.Div(html.DivConfig{Class: "ghost-row m"}),
			html.Div(html.DivConfig{Class: "ghost-row"}),
			html.Div(html.DivConfig{Class: "ghost-row s"}),
			html.Div(html.DivConfig{Class: "ghost-row m"}),
		),
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
			html.Span(html.TextConfig{Class: "op add"}, render.Text("+ add_field(\"posts\", status)")),
			// Real OptimisticAction buttons — the framework's runtime fires
			// the POST, swaps the label to SuccessLabel on click, rolls back
			// if the endpoint returns non-2xx. Endpoints are no-op handlers
			// registered in main.go.
			html.Div(html.DivConfig{Class: "actions"},
				ui.OptimisticAction(ui.OptimisticActionConfig{
					Endpoint:     "/__site/kiln/approve",
					IdleLabel:    "approve",
					SuccessLabel: "applying…",
					Variant:      ui.ButtonPrimary,
					Class:        "approve",
				}),
				ui.OptimisticAction(ui.OptimisticActionConfig{
					Endpoint:     "/__site/kiln/reject",
					IdleLabel:    "reject",
					SuccessLabel: "rejected",
					Variant:      ui.ButtonGhost,
					Class:        "reject",
				}),
			),
		),
		html.Div(html.DivConfig{Class: "kpanel__input"},
			render.Tag("input", map[string]string{"type": "text", "placeholder": "Ask the agent…", "aria-label": "Ask the agent (demo)", "disabled": "disabled"}, render.Raw("")),
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
				html.Strong(html.TextConfig{}, render.Text(title)),
				html.Paragraph(html.TextConfig{}, render.Text(body)),
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
				evt("19s", "", "Schema auto-migrates", "The plan applies and the schema auto-migrates in-process; the posts table is live."),
				evt("25s", "", "Journal freezable", "kiln freeze --dir build/ snapshots the world; graduate to Go via a gofastr.yml blueprint."),
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
				html.Span(html.TextConfig{Class: "amber"}, render.Text("Two commands of setup")),
				render.Text("."),
			),
			html.Paragraph(html.TextConfig{Class: "lede"},
				render.Text("Install the kiln binary alongside the gofastr CLI. Pick the agent CLI you already use; kiln spawns it as a subprocess with KILN_URL injected.")),
			html.Div(html.DivConfig{Class: "cli-block"},
				cmd("install",
					p("$"), render.Text(" go install github.com/DonaldMurillo/gofastr/cmd/kiln@latest\n"),
					ok("→ installed kiln v"+siteVersion+"\n"),
				),
				cmd("serve",
					p("$"), render.Text(" kiln serve --agent claude-code\n"),
					o("→ panel floats on http://localhost:8765\n"),
					o("→ MCP server live at /mcp\n"),
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

func (s *PhilosophyScreen) ScreenTitle() string { return "Philosophy" }
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
					render.Text("Why this framework exists."),
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
	toc := html.Aside(html.AsideConfig{Class: "ph-toc", Label: "Table of contents"},
		html.Div(html.DivConfig{Class: "ph-toc__label"}, render.Text("Sections")),
		html.OrderedList(html.ListConfig{},
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
	article := html.Article(html.ArticleConfig{Class: "ph-article"},
		html.Paragraph(html.TextConfig{Class: "lede"},
			render.Text("Most web frameworks assume a human will hand-write every route, query, validator, migration, and form. AI agents already generate that code — but no framework treats their output as the canonical source. GoFastr inverts that. The agent's output is canonical source, same as the human's. The framework is what they both write to."),
		),
		html.Section(html.SectionConfig{ID: "why", Label: "Why this exists"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Why this exists")),
			html.Paragraph(html.TextConfig{}, render.Text("In 2026, you can describe an app and have it generated. The output is usually a tangle: hand-rolled handlers, magic ORMs, custom-DSL config files, and an opaque server runtime that fights both you and the agent. The next thing you do is throw most of it away.")),
			html.Paragraph(html.TextConfig{}, render.Text("The pattern is fixable. If the framework names what an entity is — a typed declaration that becomes SQL, REST, MCP tools, OpenAPI, and a typed Go model — then the agent's output is the declaration. Everything else is read-only generated code you can grep, debug, and step through.")),
		),
		html.Blockquote(html.TextConfig{Class: "pullquote"},
			render.Text("The right abstraction makes the simple case trivial and the complex case possible. The wrong abstraction makes both unreadable."),
		),
		html.Section(html.SectionConfig{ID: "two-layers", Label: "Two layers"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("The two layers")),
			html.Paragraph(html.TextConfig{},
				render.Text("Two packages, no more. "), codeText("core/"), render.Text(" is stdlib-only Go primitives — router, query, schema, mcp, openapi, and more — each independently usable, with no dependencies outside the standard library. "), codeText("framework/"), render.Text(" is the opinionated entity layer composed on top. When the framework is in your way, you drop down to core and write plain Go.")),
			html.Paragraph(html.TextConfig{}, render.Text("No reflection magic. Generated code is regular Go you can read. The framework's job is to make the typed declaration so expressive that the generated code is shorter than the framework call that produced it.")),
		),
		html.Section(html.SectionConfig{ID: "convictions", Label: "Convictions"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Convictions")),
			html.Div(html.DivConfig{Class: "conv-list"},
				conv("01", "Declare once, generate the rest", "Database, REST, MCP, OpenAPI, typed Go — all from one source."),
				conv("02", "No reflection magic", "If the framework looks like it's doing something opaque, open the generated file."),
				conv("03", "Drop down to core", "If the framework is in your way, the layer below is stdlib-only Go with nothing else to fight."),
				conv("04", "Batteries included, not embedded", "Auth, cache, email, queue, search, storage — narrow interfaces, swappable drivers."),
				conv("05", "AI agents are authors too", "MCP tools, Kiln, agent notes. Every entity ships MCP tools from day one."),
				conv("06", "Strong opinions, small scope", "Some things we explicitly will not do."),
			),
		),
		html.Section(html.SectionConfig{ID: "agents", Label: "Where agents fit"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Where agents fit")),
			html.Paragraph(html.TextConfig{}, render.Text("Agents drive the framework the same way humans do. The MCP tools are the REST endpoints in a different shape; the typed Kiln tools are the framework's mutate API exposed for code-generating agents. Destructive operations require an approved plan — the agent cannot drop your tables without you clicking Approve.")),
			html.Paragraph(html.TextConfig{}, render.Text("The framework also leaves clear breadcrumbs for the agent: doc files embedded in the binary and structured MCP introspection at /mcp. An agent that connects to a running GoFastr app can read its own state and reason about it.")),
		),
		html.Section(html.SectionConfig{ID: "next", Label: "What's next"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("What's next")),
			html.Div(html.DivConfig{Class: "roadmap"},
				html.Heading(html.HeadingConfig{Level: 3}, render.Text("Roadmap")),
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
			html.Paragraph(html.TextConfig{}, render.Text("This site is built with GoFastr itself. Every interactive element is a registered component; the CSS is generated by the typed style.StyleSheet DSL against the theme; every page is server-rendered with the same runtime any consumer of the framework gets.")),
			html.Paragraph(html.TextConfig{}, render.Text("If something on this site doesn't work, the bug is in the framework — and the fix lands here first, then everywhere else.")),
		),
		html.Div(html.DivConfig{Class: "biblio"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Notes & references")),
			html.DescriptionList(html.TextConfig{},
				html.DescriptionTerm(html.TextConfig{}, render.Text("01")),
				html.DescriptionDetail(html.TextConfig{}, render.Text("The framework's principles trace from net/http: pattern routing, middleware chains, explicit handler signatures.")),
				html.DescriptionTerm(html.TextConfig{}, render.Text("02")),
				html.DescriptionDetail(html.TextConfig{}, render.Text("MCP — Anthropic's Model Context Protocol; how agents call the app's tools.")),
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

// Render is the component.Component fallback (path unknown). The uihost
// calls RenderNotFound with the real path via the NotFoundRenderer
// interface, so this is only hit if that path changes.
func (s *NotFoundScreen) Render() render.HTML { return s.renderFor("/…") }

// RenderNotFound implements uihost.NotFoundRenderer — it receives the
// unmatched request path so the page echoes the real URL, not a canned
// placeholder.
func (s *NotFoundScreen) RenderNotFound(path string) render.HTML {
	if path == "" {
		path = "/…"
	}
	return s.renderFor(path)
}

func (s *NotFoundScreen) renderFor(path string) render.HTML {
	o := func(s string) render.HTML { return html.Span(html.TextConfig{Class: "o"}, render.Text(s)) }
	p := func(s string) render.HTML { return html.Span(html.TextConfig{Class: "p"}, render.Text(s)) }
	e := func(s string) render.HTML { return html.Span(html.TextConfig{Class: "e"}, render.Text(s)) }
	ok := func(s string) render.HTML { return html.Span(html.TextConfig{Class: "ok"}, render.Text(s)) }

	// Display path: leading "/" rendered in the accent span, remainder as
	// text (render.Text escapes, so a hostile URL can't inject markup).
	rest := strings.TrimPrefix(path, "/")

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
		html.Paragraph(html.TextConfig{Class: "nf__lede"},
			render.Text("The requested path didn't map to any registered screen. Below: what the router tried, and a few places you might've meant. Press "),
			html.Kbd(html.TextConfig{}, render.Text("⌘K")),
			render.Text(" to search."),
		),
		html.Div(html.DivConfig{Class: "nf__path"},
			html.Span(html.TextConfig{Class: "u"}, render.Text("/")),
			render.Text(rest),
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
				o("→ trying  "+path+"\n"),
				e("→ miss   no exact match\n"),
				o("→ trying  "+path+"/*\n"),
				e("→ miss   no prefix subtree\n"),
				o("→ fallback handler:\n"),
				ok("→ 404 screen + suggestions\n"),
			),
		),
		html.Div(html.DivConfig{Class: "nf__suggest"},
			html.Heading(html.HeadingConfig{Level: 6}, render.Text("Did you mean")),
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
