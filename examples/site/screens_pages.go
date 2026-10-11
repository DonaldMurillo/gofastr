package main

// =============================================================================
// screens_pages.go, the remaining site pages: get started, the docs index,
// examples, Kiln, philosophy and the 404. Every page composes stock
// framework/ui components and layout primitives on the stock theme; the
// site ships no stylesheet and no class names of its own. The pages that
// read as an article beside a rail (get started, the docs index,
// philosophy) render through the site's owned docpage package, the same
// shell the /docs/<slug> pages use.
//
// The pages share helpers from screen_home.go (container) and from
// code_block.go (codeBlock, kw, fn_, str_, pn, ty, com).
// =============================================================================

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/examples/site/docpage"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// codeText, the shared inline code span used by most pages: the kit's
// ui.InlineCode, so it carries the themed mono chip.
func codeText(s string) render.HTML { return ui.InlineCode(s) }

// cardCopy keeps dense index-card summaries scannable. Canonical docs may use
// em dashes for long-form prose; the index uses lighter sentence punctuation.
func cardCopy(s string) string { return strings.ReplaceAll(s, " — ", ": ") }

// boolPtr returns a pointer to b, used for *bool config fields like
// ui.CalloutConfig.Landmark where nil means "default" and false must be
// distinguishable from unset.
//
//go:fix inline
func boolPtr(b bool) *bool { return new(b) }

// tagAccent, the version pill used in multiple page heroes. Thin adapter
// over the framework's ui.StatusPill (accent tone + dot).
func tagAccent(label string) render.HTML {
	return ui.StatusPill(ui.StatusPillConfig{Label: label, Tone: ui.StatusPillAccent, Dot: true})
}

// experimentalPill is the sitewide "this is experimental" marker for
// Kiln surfaces. Neutral tone + dot so it reads as a status without
// competing with the accent pills. Kiln is the framework's most
// provisional surface: its in-memory IR, journal-freeze format, and
// blueprint graduation flow may still change. Used on the Kiln hero
// and the get-started "Try Kiln" card; list/index entries (footer,
// palette, docs catalog) carry the word inline instead.
func experimentalPill() render.HTML {
	return ui.StatusPill(ui.StatusPillConfig{Label: "Experimental", Dot: true})
}

// pageBody is the vertical rhythm every page below shares: a stack whose
// gap, not the sections' own margins, spaces the blocks.
func pageBody(children ...render.HTML) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.Gap2XL}, children...)
}

// railColumn wraps a sticky rail for docpage's nav slot. The slot
// stretches its child to the article's height; the rail sits at the top of
// that tall column and its own position: sticky keeps it in view while
// the article scrolls.
func railColumn(rail render.HTML) render.HTML {
	return ui.Stack(ui.StackConfig{}, rail)
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

// Render is a tutorial article: the step rail beside it, the steps in the
// reading column, and where to go next under them.
func (s *GetStartedScreen) Render() render.HTML {
	return docpage.Render(docpage.Config{Nav: railColumn(gsRail())},
		pageBody(gsHero(), gsBody(), gsNext()))
}

func gsRail() render.HTML {
	return ui.StepRail(ui.StepRailConfig{
		Title:       "The path",
		BelowHeader: true, // the site header is sticky
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
}

func gsHero() render.HTML {
	// The facts are a label/value list under the pitch, not a card grid.
	text := func(s string) render.HTML { return render.Text(s) }
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		ui.Hero(ui.HeroConfig{
			Eyebrow:   "Get started · v" + siteVersion,
			Title:     "From cold machine to a running app in four minutes.",
			Subtitle:  "Install the CLI, scaffold an app, declare an entity, run it. Every command in this guide is real. Paste it into a terminal and it works.",
			AriaLabel: "Get started",
		}),
		ui.DetailList(ui.DetailListConfig{Inline: true, Items: []ui.DetailItem{
			{Label: "Prereqs", Value: text("Go 1.27+, git")},
			{Label: "OS", Value: text("macOS, Linux, Windows (WSL)")},
			{Label: "Storage", Value: text("SQLite by default, Postgres opt-in")},
			{Label: "Time", Value: text("~4 minutes")},
		}}),
	)
}

func gsBody() render.HTML {
	// One step: a section whose id is the rail's anchor, the step number
	// and its time budget as the eyebrow.
	step := func(id, num, title, time string, body ...render.HTML) render.HTML {
		return ui.Section(ui.SectionConfig{
			ID:      id,
			Eyebrow: "Step " + num + " · " + time,
			Heading: title,
			Compact: true,
		}, body...)
	}
	p := func(parts ...render.HTML) render.HTML { return html.Paragraph(html.TextConfig{}, parts...) }
	t := render.Text

	termBlock := func(label string, lines ...render.HTML) render.HTML {
		return ui.TerminalBlock(ui.TerminalBlockConfig{Label: label}, lines...)
	}
	o := ui.TerminalOut
	ok := ui.TerminalOK

	callout := func(title, body string) render.HTML {
		return ui.Callout(
			ui.CalloutConfig{Title: title, Variant: ui.StatusInfo},
			p(t(body)),
		)
	}

	step1 := step("s1", "01", "Install", "~30s",
		p(t("One binary covers scaffold, migrate, dev, build, test, and the doc browser. Get it from GitHub:")),
		termBlock("$ install",
			render.Text("$ go install github.com/DonaldMurillo/gofastr/cmd/gofastr@"+siteInstallTarget()+"\n"),
			o("$ gofastr --version\n"),
			ok("gofastr v"+siteVersion+"\n"),
		),
		callout("If gofastr isn't found", "Make sure $GOPATH/bin (or ~/go/bin) is in your PATH. Run echo $PATH and add the missing entry to your shell rc."),
	)

	step2 := step("s2", "02", "Scaffold", "~45s",
		p(t("Scaffold a new project. It writes main.go, a sample posts entity in entities/entities.go, a home screen in screens.go at the root, a versioned migration, DESIGN.md, gofastr.isolation.yml, and the agent onboarding files (AGENTS.md + agents/, CLAUDE.md), then initializes git.")),
		termBlock("$ scaffold",
			render.Text("$ gofastr init blog\n"),
			ok("✓ Created project blog in ./blog/: main.go, screens.go, entities/entities.go, gofastr.isolation.yml, DESIGN.md, CLAUDE.md, AGENTS.md\n"),
			ok("→ next: cd blog && go mod tidy && gofastr dev\n"),
		),
		p(t("Open the scaffolded "), codeText("main.go"), t(". It's short, it's yours, and every registration in it is plain Go. Read it.")),
	)

	step3 := step("s3", "03", "The entity", "~60s",
		p(t("The scaffold already declared one. Open "), codeText("entities/entities.go"), t(". One declaration is the table, REST CRUD, validation, an OpenAPI spec, and a typed query builder:")),
		codeBlock("blog/entities/entities.go", []render.HTML{
			ln(render.Text("app."), fn_("Entity"), pn("("), str_(`"posts"`), pn(","), render.Text(" entity."), ty("EntityConfig"), pn("{")),
			ln(render.Text("  Fields"), pn(":"), render.Text(" []schema."), ty("Field"), pn("{")),
			ln(render.Text("    "), pn("{"), render.Text("Name"), pn(":"), render.Text(" "), str_(`"title"`), pn(","), render.Text(" Type"), pn(":"), render.Text(" schema."), ty("String"), pn(","), render.Text(" Required"), pn(":"), render.Text(" "), kw("true"), pn("},")),
			ln(render.Text("    "), pn("{"), render.Text("Name"), pn(":"), render.Text(" "), str_(`"body"`), pn(","), render.Text(" Type"), pn(":"), render.Text(" schema."), ty("Text"), pn("},")),
			ln(render.Text("    "), pn("{"), render.Text("Name"), pn(":"), render.Text(" "), str_(`"published"`), pn(","), render.Text(" Type"), pn(":"), render.Text(" schema."), ty("Bool"), pn("},")),
			ln(render.Text("  "), pn("},")),
			ln(render.Text("  Exposure"), pn(":"), render.Text(" &entity."), ty("ExposureConfig"), pn("{"), render.Text("CRUD"), pn(":"), render.Text(" boolPtr("), kw("true"), pn(")},")),
			ln(pn("})")),
		}),
		p(t("The matching versioned migration is next to it in the same file. "), codeText("gofastr docs migrations"), t(" covers how those run.")),
	)

	step4 := step("s4", "04", "Run it", "~60s",
		p(t("Resolve dependencies once, then start the dev server. It rebuilds on save, reloads the browser, and hands your coding agent the app over MCP.")),
		termBlock("$ run",
			render.Text("$ go mod tidy\n"),
			render.Text("$ gofastr dev\n"),
			ok("→ Watching . for changes (.go, .js, .css, .html, .md)...\n"),
			ok("→ blog server ready: http://localhost:8080\n"),
		),
		p(t("Open "), codeText("localhost:8080"), t(". The scaffolded home screen renders. Then hit the API from a second terminal:")),
		termBlock("$ probe",
			o("$ curl -s http://localhost:8080/posts\n"),
			ok("{\"error\":\"authentication required\",\"success\":false,…}   # 401\n"),
		),
		p(t("That 401 is the point: auto-CRUD refuses anonymous requests unless you opt out. Add "), codeText("Public: true"), t(" to the entity, save, and the dev server rebuilds. The same curl now answers "), codeText("{\"data\":[]}"), t(". Wiring real login instead is the auth battery ("), codeText("gofastr docs auth"), t(").")),
	)

	step5 := step("s5", "05", "First page", "~60s",
		p(t("Add a second server-rendered page. A screen is a Go struct whose Render returns the markup. The scaffolded home screen in "), codeText("screens.go"), t(" is the pattern:")),
		codeBlock("blog/about.go", []render.HTML{
			ln(kw("type"), render.Text(" "), ty("AboutScreen"), render.Text(" "), kw("struct"), pn("{}")),
			ln(render.Text("")),
			ln(kw("func"), render.Text(" (s "), pn("*"), ty("AboutScreen"), pn(")"), render.Text(" "), fn_("ScreenTitle"), pn("()"), render.Text(" "), ty("string"), pn(" {"), render.Text(" "), kw("return"), render.Text(" "), str_(`"About"`), pn(" }")),
			ln(kw("func"), render.Text(" (s "), pn("*"), ty("AboutScreen"), pn(")"), render.Text(" "), fn_("Render"), pn("()"), render.Text(" "), ty("render.HTML"), pn(" {")),
			ln(render.Text("  "), kw("return"), render.Text(" ui."), fn_("PageHeader"), pn("("), render.Text("ui."), ty("PageHeaderConfig"), pn("{"), render.Text("Title"), pn(":"), render.Text(" "), str_(`"About"`), pn("})")),
			ln(pn("}")),
		}),
		p(t("Register it in "), codeText("main.go"), t(" next to the home screen: "), codeText(`site.Register("/about", &AboutScreen{}, nil)`), t(". Save, and the dev server serves it.")),
		callout("Tip", "Run `gofastr docs` to browse all embedded docs offline, including entity-declarations, query-dsl, and hooks."),
	)

	li := func(s string) render.HTML { return html.ListItem(html.ListItemConfig{}, render.Text(s)) }
	step6 := step("s6", "06", "What you have", "now",
		p(t("Four minutes in, this is on disk and running:")),
		ui.Card(ui.CardConfig{Heading: "Running, on disk, yours", Variant: ui.CardOutlined},
			html.UnorderedList(html.ListConfig{},
				li("A server-rendered home screen (plus yours from step 5)"),
				li("A posts entity: REST CRUD, session-gated by default"),
				li("A versioned SQL migration, already applied"),
				li("An OpenAPI 3 spec (auth-gated; Swagger UI at /api/docs/)"),
				li("MCP under gofastr dev: posts_list/posts_create plus app_routes, and the framework docs through framework_docs_search (the generated main.go registers them), so your coding agent reads the running app"),
				li("AGENTS.md + agents/ + DESIGN.md, generated for the agent you build with"),
			),
		),
	)

	return ui.Stack(ui.StackConfig{Gap: ui.Gap2XL}, step1, step2, step3, step4, step5, step6)
}

func gsNext() render.HTML {
	card := func(title, desc, href string, badge ...render.HTML) render.HTML {
		return ui.Card(ui.CardConfig{
			Href:        href,
			Heading:     title,
			Description: desc,
			Variant:     ui.CardOutlined,
		}, badge...)
	}
	return ui.Section(ui.SectionConfig{ID: "next", Eyebrow: "Next", Heading: "Where next", Compact: true},
		ui.Grid(ui.GridConfig{Min: "12rem"},
			card("Browse the docs", fmt.Sprintf("%d docs grouped by what you're trying to do.", docCount()), "/docs/"),
			card("Read an example", fmt.Sprintf("%d full apps you can clone and modify.", len(exRowItems())), "/examples"),
			card("Try Kiln", "Build the app by chatting with an agent; freeze the result into a blueprint you commit.", "/kiln", experimentalPill()),
		),
	)
}

// =============================================================================
// /docs/  (concepts index)
// =============================================================================

type ConceptsIndexScreen struct{}

func (s *ConceptsIndexScreen) ScreenTitle() string { return "Docs" }
func (s *ConceptsIndexScreen) ScreenDescription() string {
	return "Every feature, grouped by what you're trying to do, not alphabetically."
}
func (s *ConceptsIndexScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// Render is the docs index on the docs page shell: the intent rail in the
// nav slot, the intent groups in the article column.
func (s *ConceptsIndexScreen) Render() render.HTML {
	return docpage.Render(docpage.Config{Nav: railColumn(cxRail())},
		pageBody(cxHero(), cxBody()))
}

func cxHero() render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.Hero(ui.HeroConfig{
			Eyebrow:   "Docs · v" + siteVersion,
			Title:     "Read by what you're trying to do.",
			Subtitle:  "The docs are grouped by intent. Pick the one that matches the question you're holding.",
			AriaLabel: "Docs",
		}),
		html.Paragraph(html.TextConfig{},
			ui.Muted(render.Text(fmt.Sprintf("%d docs · %d intents", docCount(), len(docIntents))))),
	)
}

// cxRail is the intent rail: ui.AnchoredRail, which bundles the links and
// the scrollspy. Driven by the shared docIntents catalog so the rail, the
// sections, and the per-doc pages can never disagree about what exists.
func cxRail() render.HTML {
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
	return ui.AnchoredRail(ui.AnchoredRailConfig{
		Label:           "By intent",
		Items:           items,
		ObserveSelector: "#docs-sections",
	})
}

func cxBody() render.HTML {
	sections := make([]render.HTML, 0, len(docIntents)+1)
	for _, it := range docIntents {
		sections = append(sections, intentSection(it))
	}
	// Flat A–Z reference at the bottom, every embedded doc, nothing hidden.
	sections = append(sections, allDocsSection())
	return ui.Stack(ui.StackConfig{ID: "docs-sections", Gap: ui.Gap2XL}, sections...)
}

// intentSection renders one intent group: a grid of linked cards, one per
// doc (no dead cards), then the intent's recommended reading path.
func intentSection(it docIntent) render.HTML {
	cards := make([]render.HTML, 0, len(it.Docs))
	slugByTitle := make(map[string]string, len(it.Docs))
	for _, d := range it.Docs {
		slugByTitle[d.Title] = d.Slug
		cards = append(cards, ui.Card(ui.CardConfig{
			Href:        "/docs/" + d.Slug,
			Heading:     d.Title,
			Description: cardCopy(d.Desc),
			Variant:     ui.CardOutlined,
		}))
	}
	path := []render.HTML{ui.Muted(render.Text("Recommended path:"))}
	for i, title := range it.Path {
		if i > 0 {
			path = append(path, ui.Muted(render.Text("→")))
		}
		if slug, ok := slugByTitle[title]; ok {
			path = append(path, ui.Link(ui.LinkConfig{Href: "/docs/" + slug, Text: title}))
		} else {
			path = append(path, render.Text(title))
		}
	}
	return ui.Section(ui.SectionConfig{
		ID:          it.Slug,
		Eyebrow:     fmt.Sprintf("%s · %d docs", it.Num, len(it.Docs)),
		Heading:     it.Title,
		Description: cardCopy(it.Lede),
		Compact:     true,
	},
		ui.Grid(ui.GridConfig{Min: "14rem", Gap: ui.GapMD}, cards...),
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM}, path...),
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
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage},
		pageBody(exHero(), exRows()))
}

func exHero() render.HTML {
	// The theme-layer showcase lives on this site rather than under
	// examples/<slug>, so it links from the hub hero instead of joining
	// exRowItems (whose row set the source-link gate pins). One link per
	// registered route — the first primary, the rest secondary — derived
	// from landingRoutes so a new theme cannot miss the hub.
	buttons := make([]render.HTML, 0, len(landingRoutes))
	for i, r := range landingRoutes {
		variant := ui.ButtonSecondary
		if i == 0 {
			variant = ui.ButtonPrimary
		}
		buttons = append(buttons, ui.LinkButton(ui.LinkButtonConfig{
			Label:   "Headless landing · " + r.Name + " theme",
			Href:    landingRoutePath(r.Segment),
			Variant: variant,
		}))
	}
	return ui.Hero(ui.HeroConfig{
		Eyebrow:   fmt.Sprintf("Examples · %d apps", len(exRowItems())),
		Title:     fmt.Sprintf("%d reference apps. Each runs in one command.", len(exRowItems())),
		Subtitle:  "Clone the one that looks like your problem; swap the entity declarations. Each app's full source is under examples/ in the repo. Run it with gofastr dev for rebuild-on-save, livereload, and the dev MCP tools; plain go run . works too.",
		Actions:   buttons,
		AriaLabel: "Examples",
	})
}

// exampleLoC is the non-test Go line count each row's badge cites, keyed
// by the example directory and rounded to two significant figures.
// TestExampleLoCBadgesMatchTree measures examples/<slug> and fails when a
// badge is more than 20% off, so the numbers are re-measured instead of
// remembered.
var exampleLoC = map[string]int{
	"blog":                 190,
	"site":                 12000,
	"api-tour":             160,
	"semantic-demo":        120,
	"spa":                  110,
	"embed-demo":           310,
	"static-site":          60,
	"backoffice":           310,
	"processmodule-demo":   330,
	"webmcp-remote-assist": 2000,
	"rtc-call":             700,
}

// locBadge renders the "~N LoC" badge for one example row; suffix adds a
// qualifier such as " server" for apps whose client lives elsewhere.
func locBadge(slug, suffix string) string {
	return fmt.Sprintf("~%d LoC%s", exampleLoC[slug], suffix)
}

// exRowItems builds every example row. Copy that cites an app count
// (the hero pill, the H1, the get-started card, the meta description)
// derives it from len() of this slice, so the number can't drift from
// the content again.
func exRowItems() []render.HTML {
	return []render.HTML{
		exRow("01", "examples/meridian", "Meridian: SaaS console", "flagship", "seeded + hand-evolved",
			"A billing & revenue console (customers, subscriptions, invoices, MRR + charts) plus its marketing site, auth, RBAC, and an admin back-office, seeded from one gofastr.yml and hand-evolved since, with writable screens (add/edit/delete).",
			[]string{"One blueprint seeded marketing + app + auth + admin", "Server-rendered DataTable / charts / forms, island RPCs", "Writable CRUD + RBAC, with a generated end-to-end test suite"},
			"cd examples/meridian && gofastr dev",
			// The exact, full blueprint that generates the app, embedded at
			// build time, shown verbatim in a scrolling block. Drift-guarded by
			// TestEmbeddedBlueprintsMatchSource.
			codeBlockScroll("examples/meridian/gofastr.yml", meridianBlueprintYAML, "yaml")),
		exRow("02", "examples/ecommerce", "ShopFront: storefront", "blueprint pipeline", "100% generated",
			"A complete storefront from one gofastr.yml: five related entities (categories, products, orders, order_items, reviews), a themed eight-screen UI, custom endpoints, and seed data. Nothing under app/ is hand-written.",
			[]string{"Second blueprint pipeline beside Meridian", "Owner-scoped orders and reviews", "Exercised by its own end-to-end test"},
			"cd examples/ecommerce && gofastr dev --dir app",
			codeBlock("examples/ecommerce/gofastr.yml", []render.HTML{
				ln(com("# 5 entities, 8 screens, auth, seeds: one YAML")),
				ln(render.Text("$ gofastr generate   "), com("// emits ./app: plain Go")),
				ln(render.Text("$ go run ./app")),
			})),
		exRow("03", "examples/blog", "Go-declared blog", "smallest", locBadge("blog", ""),
			"Users, posts, comments. Three entities. Start here: it's the end-to-end story in one file.",
			[]string{"Three entities declared in Go", "Auto-CRUD + Swagger UI + MCP", "SQLite by default; swap for Postgres in main.go"},
			"cd examples/blog && gofastr dev",
			codeBlock("examples/blog/main.go", []render.HTML{
				ln(render.Text("app."), fn_("Entity"), pn("("), str_(`"users"`), pn(","), render.Text(" …"), pn(")")),
				ln(render.Text("app."), fn_("Entity"), pn("("), str_(`"posts"`), pn(","), render.Text(" …"), pn(")")),
				ln(render.Text("app."), fn_("Entity"), pn("("), str_(`"comments"`), pn(","), render.Text(" …"), pn(")")),
				ln(render.Text("app."), fn_("Start"), pn("("), str_(`":8080"`), pn(")")),
			})),
		exRow("04", "examples/site", "This site (UI showcase)", "largest", locBadge("site", ""),
			"Every core-ui pattern + framework/ui component, one page each, plus the docs, SEO, multi-step wizard, and print-battery demos. The site you're reading right now.",
			[]string{"Every core-ui pattern + framework/ui component", "Docs, philosophy, examples, Kiln pages", "SEO interfaces, sitemap/robots, wizard, print"},
			"cd examples/site && gofastr dev",
			codeBlock("examples/site/main.go", []render.HTML{
				ln(render.Text("host "), pn(":="), render.Text(" uihost."), fn_("New"), pn("("), render.Text("site"), pn(", …)")),
				ln(render.Text("app "), pn(":="), render.Text(" framework."), fn_("NewUIHostApp"), pn("("), render.Text("host"), pn(")")),
				ln(render.Text("app."), fn_("Start"), pn("("), str_(`":8083"`), pn(")")),
			})),
		exRow("05", "examples/api-tour", "API tour", "annotated source", locBadge("api-tour", ""),
			"Every v2 API feature in one annotated main.go, with the curl commands to exercise each one in the file header.",
			[]string{"Cursor + offset pagination", "Eager loading (?include=…)", "Batch endpoints, SSE entity events, uploads"},
			"cd examples/api-tour && gofastr dev",
			codeBlock("examples/api-tour/main.go", []render.HTML{
				ln(render.Text("app."), fn_("Entity"), pn("("), str_(`"posts"`), pn(","), render.Text(" …"), pn(")")),
				ln(com("// cursor + offset paging, ?include=, batch, SSE")),
				ln(render.Text("app."), fn_("Start"), pn("("), str_(`":8080"`), pn(")")),
			})),
		exRow("06", "examples/semantic-demo", "Local semantic search", "no API key", locBadge("semantic-demo", ""),
			"A markdown corpus indexed locally via battery/semantic. No external API key; works offline.",
			[]string{"Brute-force cosine, hybrid keyword fusion", "Snapshot + WAL persistence", "Poll-watch for file changes"},
			"cd examples/semantic-demo && gofastr dev",
			codeBlock("examples/semantic-demo/main.go", []render.HTML{
				ln(render.Text("idx, _ "), pn(":="), render.Text(" semantic."), fn_("Open"), pn("("), render.Text("semantic."), ty("Options"), pn("{…})")),
				ln(render.Text("idx."), fn_("Add"), pn("("), render.Text("ctx, docs…"), pn(")"), render.Text("   "), com("// local vectors")),
				ln(render.Text("hits, _ "), pn(":="), render.Text(" idx."), fn_("Query"), pn("("), render.Text("ctx, semantic."), ty("Query"), pn("{"), render.Text("Text: "), str_(`"hooks"`), pn(", "), render.Text("Limit: 5"), pn("})")),
			})),
		exRow("07", "examples/spa", "Vue + GoFastr API", "BYO client", locBadge("spa", " server"),
			"For teams who already have a client app. Shows the framework is happy to just be your typed API.",
			[]string{"Same auto-CRUD entities", "Vue 3 + Vue Router from a CDN, no npm, no build step", "No SSR: the Go app serves JSON and the static files"},
			"cd examples/spa && gofastr dev",
			codeBlock("examples/spa/main.go", []render.HTML{
				ln(render.Text("app."), fn_("Entity"), pn("("), str_(`"articles"`), pn(","), render.Text(" …"), pn(")")),
				ln(com("// JSON API under /api: your Vue app is the client")),
				ln(render.Text("app."), fn_("Start"), pn("("), str_(`":3090"`), pn(")")),
			})),
		exRow("08", "examples/embed-demo", "Embeddable surfaces", "cross-origin", locBadge("embed-demo", ""),
			"Two servers on different ports: a GoFastr app, and a customer's website that pastes one script tag and gets a live, themed, authenticated panel from it.",
			[]string{"Single-use handshake nonce, exact origin allowlist", "Frame runtime with no SPA navigation", "Customer brand token applied to the app's own components"},
			"cd examples/embed-demo && gofastr dev",
			codeBlock("examples/embed-demo/main.go", []render.HTML{
				ln(render.Text("embeds, _ "), pn(":="), render.Text(" embed."), fn_("New"), pn("("), render.Text("embed."), ty("Config"), pn("{…})")),
				ln(render.Text("nonce, _ "), pn(":="), render.Text(" embeds."), fn_("MintNonce"), pn("("), str_(`"reports"`), render.Text(", user.ID, origin, "), render.Text("nil"), pn(")")),
				ln(com("// the customer pastes: <script src=\".../__gofastr/embed.js\" data-token=…>")),
			})),
		exRow("09", "examples/static-site", "Static file server", "no screens", locBadge("static-site", ""),
			"A plain file server on the framework router: static.Mount serves the pages/ directory, with HTML and CSS straight from disk, no screens, no runtime JS.",
			[]string{"static.Mount with SPA mode off: only real files serve", "index.html answers /", "API routes can mount beside it on the same router"},
			"cd examples/static-site && gofastr dev",
			codeBlock("examples/static-site/main.go", []render.HTML{
				ln(render.Text("static."), fn_("Mount"), pn("("), render.Text("app."), fn_("Router"), pn("(), static."), ty("Config"), pn("{")),
				ln(render.Text("  FS: os."), fn_("DirFS"), pn("("), render.Text("pagesDir"), pn("),")),
				ln(pn("})")),
				ln(render.Text("app."), fn_("Start"), pn("("), str_(`":3070"`), pn(")")),
			})),
		exRow("10", "examples/backoffice", "Entity admin", "battery/admin", locBadge("backoffice", ""),
			"Three entities and one admin.New call: the whole back-office (list, create, edit, delete) is generated with defaults, behind a demo login. No bespoke JavaScript anywhere in the app.",
			[]string{"admin.New with AllEntities: true generates every screen", "DataTable island paginates without a reload", "Delete is a data-cui-confirm button; forms are server-rendered"},
			"cd examples/backoffice && gofastr dev",
			codeBlock("examples/backoffice/main.go", []render.HTML{
				ln(render.Text("app."), fn_("Entity"), pn("("), str_(`"products"`), pn(","), render.Text(" …"), pn(")")),
				ln(render.Text("app."), fn_("RegisterBattery"), pn("("), render.Text("admin."), fn_("New"), pn("("), render.Text("admin."), ty("Config"), pn("{")),
				ln(render.Text("  Title: "), str_(`"Backoffice"`), pn(", "), render.Text("AllEntities: true"), pn(",")),
				ln(pn("}))")),
			})),
		exRow("11", "examples/processmodule-demo", "Process-isolated module", "moduleproto", locBadge("processmodule-demo", ""),
			"A third-party module as its own process: it speaks moduleproto over stdio, serves three routes and one tool to the host, and answers a reverse entity query. The child the process-module gate suite drives end to end.",
			[]string{"Depends only on the moduleproto package + the standard library", "Handshake, ready, health, http, drain, tool.list, tool.call", "Reverse host.entity.query proves the broker path"},
			"go run ./examples/processmodule-demo",
			codeBlock("examples/processmodule-demo/main.go", []render.HTML{
				ln(render.Text("codec, _ "), pn(":="), render.Text(" moduleproto."), fn_("NewCodec"), pn("("), render.Text("os.Stdin, os.Stdout, …"), pn(")")),
				ln(render.Text("peer "), pn(":="), render.Text(" moduleproto."), fn_("NewPeer"), pn("("), render.Text("codec, moduleproto.RoleChild"), pn(")")),
				ln(render.Text("peer."), fn_("Handle"), pn("("), render.Text("moduleproto.MethodHTTP, serveRoute"), pn(")")),
				ln(render.Text("peer."), fn_("Start"), pn("(); "), pn("<-"), render.Text("peer."), fn_("Done"), pn("()")),
			})),
		exRow("12", "examples/webmcp-remote-assist", "WebMCP remote assist", "WebMCP + WebRTC", locBadge("webmcp-remote-assist", ""),
			"One binary, one origin, two roles: a support console whose in-browser agent guides an operator through a camera session. Support-only WebMCP tool discovery, one typed command behind the button and the tools, and peer-to-peer video with server-side signaling only.",
			[]string{"Tools scoped to /support and authorization-wrapped", "Role cookies authorize; the WebMCP header only attributes", "Sequenced realtime state over the ws runtime module"},
			"cd examples/webmcp-remote-assist && gofastr dev",
			codeBlock("examples/webmcp-remote-assist/main.go", []render.HTML{
				ln(render.Text("tools "), pn(":="), render.Text(" webmcp."), fn_("New"), pn("("), render.Text("webmcp."), fn_("WithInstructions"), pn("(…))")),
				ln(render.Text("group."), fn_("Handle"), pn("("), render.Text("rt, sendInstruction, handler,")),
				ln(render.Text("  webmcp."), fn_("WithHTTPMiddleware"), pn("("), render.Text("requireSupport"), pn("))")),
				ln(render.Text("webmcp."), fn_("WithDocumentScope"), pn("("), render.Text("supportScope"), pn(")")),
			})),
		exRow("13", "examples/rtc-call", "WebRTC call rooms", "battery/rtc", locBadge("rtc-call", ""),
			"Anonymous users, rooms by name: the dogfood app for the rtc signaling battery. The lobby trades a display name for a cookie; the room page runs one peer connection per peer with chat on a negotiated data channel, and the Go process relays handshakes only, never media.",
			[]string{"rtc.New + RegisterPlugin: zero hand-rolled signaling", "Identity server-derived through Config.Authorize", "Peer-to-peer camera and chat; the server sees neither"},
			"cd examples/rtc-call && gofastr dev",
			codeBlock("examples/rtc-call/main.go", []render.HTML{
				ln(render.Text("sig "), pn(":="), render.Text(" rtc."), fn_("New"), pn("("), render.Text("rtc."), ty("Config"), pn("{ … }")),
				ln(render.Text("app."), fn_("RegisterPlugin"), pn("("), render.Text("sig"), pn(")")),
			})),
	}
}

// exBlueprints is row 14: the declarative examples that are blueprints
// only, no Go until `gofastr generate` runs. It shares exRowShell with
// the runnable rows but stays out of exRowItems, whose length is the
// "runs in one command" count. The source links sit in their own row
// below the points, never inline in prose, so axe's link-in-text-block
// rule has nothing to flag.
func exBlueprints() render.HTML {
	items := []struct{ slug, domain string }{
		{"lms", "courses, lessons, enrollments"},
		{"portfolio", "projects and case studies"},
		{"project-manager", "projects, tasks, teams"},
		{"real-estate", "listings, agents, inquiries"},
	}
	points := make([]render.HTML, 0, len(items))
	links := make([]render.HTML, 0, len(items))
	for _, it := range items {
		points = append(points, html.ListItem(html.ListItemConfig{}, render.Text(it.slug+": "+it.domain)))
		links = append(links, exSourceLink(
			"https://github.com/DonaldMurillo/gofastr/tree/main/examples/"+it.slug,
			"examples/"+it.slug+" ↗"))
	}
	body := exRowBody(exRowBodyConfig{
		Tag:     "gofastr.yml only",
		LoC:     "0 LoC until you generate",
		Desc:    "Each is one gofastr.yml with no Go beside it. Generate in the directory to get a runnable app you own; every one is validated by the CLI's blueprint test suite.",
		Points:  points,
		Command: "cd examples/lms && gofastr generate --from=gofastr.yml",
		Extra:   ui.Cluster(ui.ClusterConfig{Gap: ui.GapMD}, links...),
	})
	code := codeBlock("examples/lms/gofastr.yml", []render.HTML{
		ln(com("# entities, screens, nav, endpoints, seed: one YAML")),
		ln(render.Text("$ gofastr generate --from=gofastr.yml")),
		ln(render.Text("$ go run .")),
	})
	return exRowShell("14", "blueprints", "examples/…", "Four more blueprints", body, code)
}

func exRows() render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.Gap2XL}, append(exRowItems(), exBlueprints())...)
}

// exRowBodyConfig is the left column of one example row. Points are
// pre-built <li>s so a row can decide whether a point is text or a link.
type exRowBodyConfig struct {
	Tag, LoC, Desc, Command string
	Points                  []render.HTML
	Source                  string      // "View source" href; empty hides the link
	Extra                   render.HTML // optional trailing block (the blueprint row's source links)
}

// exSourceLink is an off-site link to an example's source tree.
func exSourceLink(href, text string) render.HTML {
	return ui.Link(ui.LinkConfig{Href: href, Text: text, ExtraAttrs: html.Attrs{"rel": "external"}})
}

// exRowBody renders the left column: the tag and size, the description,
// the points, the run command, and the optional source link.
func exRowBody(c exRowBodyConfig) render.HTML {
	parts := []render.HTML{
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
			tagAccent(c.Tag),
			ui.Muted(render.Text(c.LoC)),
		),
		html.Paragraph(html.TextConfig{}, render.Text(c.Desc)),
		html.UnorderedList(html.ListConfig{}, c.Points...),
		ui.CodeBlock(ui.CodeBlockConfig{Filename: "run", Code: c.Command, Language: "shell", ShowCopy: true}),
	}
	if c.Extra != "" {
		parts = append(parts, c.Extra)
	}
	if c.Source != "" {
		parts = append(parts, exSourceLink(c.Source, "View source ↗"))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapMD, TrimMargins: true}, parts...)
}

// exRowShell is the section every example row sits in: the number and
// path as the eyebrow, the title as the heading, then the body column
// beside the code sample (stacked on narrow screens).
func exRowShell(num, id, path, title string, body, code render.HTML) render.HTML {
	return ui.Section(ui.SectionConfig{
		ID:      id,
		Eyebrow: num + " · " + path,
		Heading: title,
		Compact: true,
	},
		// The code column is a stack so the sample keeps its own height
		// instead of stretching to the grid row's.
		ui.Grid(ui.GridConfig{Min: "22rem", Gap: ui.GapXL}, body, ui.Stack(ui.StackConfig{}, code)),
	)
}

// exRow renders one runnable example. code is the pre-built code sample (a
// snippet for most rows; the full embedded blueprint for Meridian); path
// names the directory for the "View source" link.
func exRow(num, path, title, tag, loc, desc string, points []string, cmd string, code render.HTML) render.HTML {
	slug := strings.TrimPrefix(path, "examples/")
	pointLis := make([]render.HTML, 0, len(points))
	for _, p := range points {
		pointLis = append(pointLis, html.ListItem(html.ListItemConfig{}, render.Text(p)))
	}
	body := exRowBody(exRowBodyConfig{
		Tag: tag, LoC: loc, Desc: desc,
		Points:  pointLis,
		Command: cmd,
		Source:  "https://github.com/DonaldMurillo/gofastr/tree/main/" + path,
	})
	return exRowShell(num, slug, path, title, body, code)
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
	return container(ui.Stack(ui.StackConfig{Gap: ui.Gap2XL},
		kHero(), kTimeline(), kCaps(), kCli(),
	))
}

// kInstallCmd is the one-line install shown in the hero and the CLI section.
const kInstallCmd = "go install github.com/DonaldMurillo/gofastr/cmd/kiln@latest"

func kHero() render.HTML {
	// The demo panel fills the hero's Media slot. The install command
	// sits under the hero rather than in it: Hero's narrow-screen
	// collapse is a bare 1fr track, so a long unbreakable line in the
	// hero widens the page past the viewport. In the Stack it fills the
	// column and scrolls inside its own frame, and the copy button
	// carries it to the clipboard intact.
	install := ui.CodeBlock(ui.CodeBlockConfig{
		Filename: "install",
		Language: "shell",
		Lines:    []render.HTML{ln(pn("$ "), render.Text(kInstallCmd))},
		ShowCopy: true,
	})
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL}, ui.Hero(ui.HeroConfig{
		Eyebrow:   "kiln: agent build mode",
		Title:     "Talk an app into being.",
		Subtitle:  "Kiln is experimental: a separate binary that mounts a chat panel on your running GoFastr app. The agent calls typed tools; the in-memory IR mutates; the schema migrates; the app re-renders, all in-process. Freeze the journal when done to emit the canonical entity files you commit.",
		AriaLabel: "Kiln",
		Media:     kDemo(),
		Actions: []render.HTML{
			ui.LinkButton(ui.LinkButtonConfig{Label: "Read the docs", Href: "/docs/kiln", Variant: ui.ButtonPrimary, Size: ui.ButtonSizeLarge}),
			experimentalPill(),
		},
	}), install)
}

// kMessage is one turn in the demo chat: who spoke, what they said, and
// the typed tool an agent turn called.
func kMessage(who, body, tool string) render.HTML {
	head := []render.HTML{html.Strong(html.TextConfig{}, render.Text(who))}
	if tool != "" {
		head = append(head, ui.Tag(ui.TagConfig{Label: "tool: " + tool, Variant: ui.StatusInfo}))
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignStart, NoWrap: true},
		ui.Avatar(ui.AvatarConfig{Name: who, Size: ui.AvatarSm}),
		ui.Stack(ui.StackConfig{Gap: ui.GapXS, TrimMargins: true},
			ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM}, head...),
			html.Paragraph(html.TextConfig{}, render.Text(body)),
		),
	)
}

func kDemo() render.HTML {
	// Left: the app under construction. An illustration, not a live
	// load, and the description says so.
	appCard := ui.Card(ui.CardConfig{
		Heading:      "Your app, being authored live",
		HeadingLevel: 2,
		Description:  "Illustration: your real app renders here as the agent edits it.",
		Action:       ui.Tag(ui.TagConfig{Label: "localhost:8765"}),
	})

	plan := ui.CodeBlock(ui.CodeBlockConfig{
		Filename: "Plan #4 · 3 ops",
		Lines: []render.HTML{
			ln(pn("+ "), fn_("add_entity"), render.Text("("), str_(`"posts"`), render.Text(")")),
			ln(pn("+ "), fn_("add_field"), render.Text("("), str_(`"posts"`), render.Text(", title)")),
			ln(pn("+ "), fn_("add_field"), render.Text("("), str_(`"posts"`), render.Text(", status)")),
		},
	})
	// Real OptimisticAction buttons: the framework's runtime fires the
	// POST, swaps the label to SuccessLabel on click, and rolls back if
	// the endpoint returns non-2xx. Endpoints are no-op handlers
	// registered in main.go.
	actions := ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
		ui.OptimisticAction(ui.OptimisticActionConfig{
			Endpoint:     "/__site/kiln/approve",
			IdleLabel:    "approve",
			SuccessLabel: "applying…",
			Variant:      ui.ButtonPrimary,
		}),
		ui.OptimisticAction(ui.OptimisticActionConfig{
			Endpoint:     "/__site/kiln/reject",
			IdleLabel:    "reject",
			SuccessLabel: "rejected",
			Variant:      ui.ButtonGhost,
		}),
	)
	ask := ui.TextField(ui.TextFieldConfig{
		Name:        "ask",
		ID:          "kiln-demo-ask",
		Label:       "Ask the agent (demo)",
		LabelHidden: true,
		Placeholder: "Ask the agent…",
		Disabled:    true,
	})

	panel := ui.Card(ui.CardConfig{
		Header: ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Justify: ui.JustifyBetween},
			ui.StatusPill(ui.StatusPillConfig{Label: "kiln", Tone: ui.StatusPillAccent, Dot: true}),
			ui.Muted(render.Text("session #a1b3")),
		),
		Footer: ask,
	},
		ui.Stack(ui.StackConfig{Gap: ui.GapLG},
			kMessage("you", "add a posts entity with title, body, status enum", ""),
			kMessage("claude-code", "Reading your existing schema. Three new tools required, no destructive changes.", "add_entity"),
			kMessage("you", "looks good, ship it", ""),
			kMessage("claude-code", "Proposing a plan. Approve below to apply.", "propose_plan"),
			plan,
			actions,
		),
	)

	return ui.Stack(ui.StackConfig{Gap: ui.GapMD}, appCard, panel)
}

func kTimeline() render.HTML {
	evt := func(t string, v ui.TimelineEventVariant, title, body string) ui.TimelineEvent {
		return ui.TimelineEvent{Title: title, Meta: t, Variant: v, Body: render.Text(body)}
	}
	return ui.Section(ui.SectionConfig{
		Eyebrow: "Anatomy of a session",
		Heading: "Seven events from prompt to commit",
		Compact: true,
	},
		ui.Timeline(ui.TimelineConfig{Events: []ui.TimelineEvent{
			evt("0s", ui.TimelineNeutral, "Agent connects", "kiln subscribes to its own SSE bus and spawns the configured CLI."),
			evt("3s", ui.TimelineInfo, "Agent calls world_get", "Reads the in-memory IR: current entities, fields, hooks, routes."),
			evt("8s", ui.TimelineInfo, "Agent calls add_entity", "Mutates the IR: posts(title, body, status). No DB write yet."),
			evt("12s", ui.TimelineInfo, "Agent calls propose_plan", "Lists destructive targets (none) and the three add_* operations."),
			evt("18s", ui.TimelineSuccess, "You click Approve", "Plan id is stamped onto the agent's retry call."),
			evt("19s", ui.TimelineNeutral, "Schema auto-migrates", "The plan applies and the schema auto-migrates in-process; the posts table is live."),
			evt("25s", ui.TimelineNeutral, "Journal freezable", "kiln freeze --dir build/ snapshots the world; graduate to Go via a gofastr.yml blueprint."),
		}}),
	)
}

func kCaps() render.HTML {
	list := func(items ...string) render.HTML {
		lis := make([]render.HTML, len(items))
		for i, it := range items {
			lis[i] = html.ListItem(html.ListItemConfig{}, render.Text(it))
		}
		return html.UnorderedList(html.ListConfig{}, lis...)
	}
	can := ui.Card(ui.CardConfig{
		Heading: "What the agent can do",
		Action:  ui.StatusBadge(ui.StatusBadgeConfig{Label: "Allowed", Variant: ui.StatusSuccess, Dot: true}),
	}, list(
		"Add entities, fields, hooks, routes",
		"Migrate up + seed data",
		"Edit pages and screens (non-destructively)",
		"Inspect logs, run queries, browse docs",
	))
	cant := ui.Card(ui.CardConfig{
		Heading: "Without an approved plan",
		Action:  ui.StatusBadge(ui.StatusBadgeConfig{Label: "Blocked", Variant: ui.StatusDanger, Dot: true}),
	}, list(
		"Drop entities, fields, hooks, routes",
		"Migrate down",
		"Touch credentials, secrets, .env",
		"Spawn external processes you didn't allow",
	))
	return ui.Section(ui.SectionConfig{
		Eyebrow: "Plan-gated destructive ops",
		Heading: "The agent acts within explicit limits",
		Compact: true,
	},
		ui.Grid(ui.GridConfig{Min: "20rem", Gap: ui.GapLG}, can, cant),
	)
}

func kCli() render.HTML {
	return ui.Section(ui.SectionConfig{
		Eyebrow:     "Setup",
		Heading:     "Two binaries. Two commands of setup.",
		Description: "Install the kiln binary alongside the gofastr CLI. Pick the agent CLI you already use; kiln spawns it as a subprocess with KILN_URL injected.",
		Compact:     true,
	},
		ui.Grid(ui.GridConfig{Min: "20rem", Gap: ui.GapLG},
			ui.TerminalBlock(ui.TerminalBlockConfig{Label: "install"},
				render.Text("$ "+kInstallCmd+"\n"),
				ui.TerminalOK("→ installed kiln v"+siteVersion+"\n"),
			),
			ui.TerminalBlock(ui.TerminalBlockConfig{Label: "serve"},
				render.Text("$ kiln serve --agent claude-code\n"),
				ui.TerminalOut("→ panel floats on http://localhost:8765\n"),
				ui.TerminalOut("→ MCP server live at /mcp\n"),
				ui.TerminalOK("→ ready · waiting for the agent.\n"),
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
	return "The convictions behind GoFastr: what we say no to, and why."
}
func (s *PhilosophyScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// Render is an essay on the docs page shell: the contents rail in the nav
// slot, the essay as themed prose (ui.Markdown) in the article column.
func (s *PhilosophyScreen) Render() render.HTML {
	return docpage.Render(docpage.Config{Nav: railColumn(phTOC())},
		pageBody(phHero(), phBody()))
}

func phHero() render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.Hero(ui.HeroConfig{
			Eyebrow:   "Essay · Volume 01 · 2026-05",
			Title:     "Why this framework exists.",
			Subtitle:  "Most web frameworks assume a human will hand-write every route, query, validator, migration, and form. AI agents already generate that code, but no framework treats their output as the canonical source. GoFastr inverts that. The agent's output is canonical source, same as the human's. The framework is what they both write to.",
			AriaLabel: "Philosophy",
		}),
		html.Paragraph(html.TextConfig{}, ui.Muted(render.Text("By Donald Murillo · Updated 2026-05-26"))),
	)
}

// phSections are the essay's h2s: the contents rail's entries, keyed by
// the ids ui.Markdown derives from the heading text.
var phSections = []ui.TOCItem{
	{ID: "why-this-exists", Label: "Why this exists"},
	{ID: "the-two-layers", Label: "The two layers"},
	{ID: "convictions", Label: "Convictions"},
	{ID: "where-agents-fit", Label: "Where agents fit"},
	{ID: "whats-next", Label: "What's next"},
	{ID: "a-note-on-this-site", Label: "A note on this site"},
	{ID: "notes-references", Label: "Notes & references"},
}

func phTOC() render.HTML {
	return ui.TableOfContents(ui.TOCConfig{
		Items:  phSections,
		Label:  "Sections",
		Target: "#philosophy-essay",
		Sticky: true,
	})
}

// phEssay is the essay body. It is prose, so it is markdown: ui.Markdown
// owns the reading rhythm, the pull quote, the lists and the roadmap table.
const phEssay = "## Why this exists\n\n" +
	"In 2026, you can describe an app and have it generated. The output is usually a tangle: hand-rolled handlers, magic ORMs, custom-DSL config files, and an opaque server runtime that fights both you and the agent. The next thing you do is throw most of it away.\n\n" +
	"The pattern is fixable. If the framework names what an entity is, a typed declaration that becomes SQL, REST, MCP tools, OpenAPI, and a typed Go model, then the agent's output is the declaration. Everything else is read-only generated code you can grep, debug, and step through.\n\n" +
	"> The right abstraction makes the simple case trivial and the complex case possible. The wrong abstraction makes both unreadable.\n\n" +
	"## The two layers\n\n" +
	"Two packages, no more. `core/` is stdlib-only Go primitives, router, query, schema, mcp, openapi, and more, each independently usable, with no dependencies outside the standard library. `framework/` is the opinionated entity layer composed on top. When the framework is in your way, you drop down to core and write plain Go.\n\n" +
	"No reflection magic. Generated code is regular Go you can read. The framework's job is to make the typed declaration so expressive that the generated code is shorter than the framework call that produced it.\n\n" +
	"## Convictions\n\n" +
	"1. **Declare once, generate the rest.** Database, REST, MCP, OpenAPI, typed Go: all from one source.\n" +
	"2. **No reflection magic.** If the framework looks like it's doing something opaque, open the generated file.\n" +
	"3. **Drop down to core.** If the framework is in your way, the layer below is stdlib-only Go with nothing else to fight.\n" +
	"4. **Batteries included, not embedded.** Auth, cache, email, queue, search, storage: narrow interfaces, swappable drivers.\n" +
	"5. **AI agents are authors too.** MCP tools, Kiln, agent notes. Every entity ships MCP tools from day one.\n" +
	"6. **Strong opinions, small scope.** Some things we explicitly will not do.\n\n" +
	"## Where agents fit\n\n" +
	"Agents drive the framework the same way humans do. The MCP tools are the REST endpoints in a different shape; the typed Kiln tools are the framework's mutate API exposed for code-generating agents. Destructive operations require an approved plan. The agent cannot drop your tables without you clicking Approve.\n\n" +
	"The framework also leaves clear breadcrumbs for the agent: doc files embedded in the binary and structured MCP introspection at /mcp. An agent that connects to a running GoFastr app can read its own state and reason about it.\n\n" +
	"## What's next\n\n" +
	"| When | What | Status |\n" +
	"| --- | --- | --- |\n" +
	"| Shipped | Two-layer core/ + framework/ split | ✓ shipped |\n" +
	"| Shipped | Auto-CRUD + MCP + OpenAPI | ✓ shipped |\n" +
	"| Shipped | Kiln agent build mode (experimental) | ✓ shipped |\n" +
	"| Q3 2026 | Lock framework/entity ABI | next |\n" +
	"| Q4 2026 | Land core-ui 1.0 | later |\n" +
	"| 2027 | First version we'd suggest shipping to customers | later |\n\n" +
	"## A note on this site\n\n" +
	"This site is built with GoFastr itself. Every interactive element is a registered component; the CSS is generated by the typed style.StyleSheet DSL against the theme; every page is server-rendered with the same runtime any consumer of the framework gets.\n\n" +
	"If something on this site doesn't work, the bug is in the framework, and the fix lands here first, then everywhere else.\n\n" +
	"## Notes & references\n\n" +
	"1. The framework's principles trace from net/http: pattern routing, middleware chains, explicit handler signatures.\n" +
	"2. MCP: Anthropic's Model Context Protocol; how agents call the app's tools.\n" +
	"3. The two-layer pattern echoes Rich Hickey's distinction between simple and easy.\n"

func phBody() render.HTML {
	return ui.Markdown(ui.MarkdownConfig{Source: phEssay, ID: "philosophy-essay"})
}

// =============================================================================
// /404  (registered as the custom NotFound handler, see main.go)
// =============================================================================

type NotFoundScreen struct{}

func (s *NotFoundScreen) ScreenTitle() string        { return "404: Not found" }
func (s *NotFoundScreen) ScreenDescription() string  { return "" }
func (s *NotFoundScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// Render is the component.Component fallback (path unknown). The uihost
// calls RenderNotFound with the real path via the NotFoundRenderer
// interface, so this is only hit if that path changes.
func (s *NotFoundScreen) Render() render.HTML { return s.renderFor("/…") }

// RenderNotFound implements uihost.NotFoundRenderer, it receives the
// unmatched request path so the page echoes the real URL, not a canned
// placeholder.
func (s *NotFoundScreen) RenderNotFound(path string) render.HTML {
	if path == "" {
		path = "/…"
	}
	return s.renderFor(path)
}

// renderFor composes the 404: the kit's empty state names the miss and
// offers the way out, the requested path echoes back as inline code
// (render.Text escapes it, so a hostile URL can't inject markup), and a
// terminal block replays what the router tried.
func (s *NotFoundScreen) renderFor(path string) render.HTML {
	o := ui.TerminalOut
	miss := ui.TerminalErr
	ok := ui.TerminalOK

	suggest := func(href, text string) render.HTML {
		return ui.Link(ui.LinkConfig{Href: href, Text: text, Variant: ui.LinkAction})
	}

	head := ui.EmptyState(ui.EmptyStateConfig{
		Title:        "404 · Router didn't match.",
		Description:  "The requested path didn't map to any registered screen. Below: what the router tried, and a few places you might've meant.",
		HeadingLevel: 1,
		Action: ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Justify: ui.JustifyCenter},
			ui.LinkButton(ui.LinkButtonConfig{Label: "Home", Href: "/"}),
			ui.LinkButton(ui.LinkButtonConfig{Label: "Docs index", Href: "/docs/", Variant: ui.ButtonSecondary}),
		),
	})

	requested := html.Paragraph(html.TextConfig{},
		render.Text("Requested: "), codeText(path),
		render.Text(". Press "), html.Kbd(html.TextConfig{}, render.Text("⌘K")), render.Text(" to search."),
	)

	trace := ui.TerminalBlock(ui.TerminalBlockConfig{Label: "router trace"},
		render.Text("$ router.Match\n"),
		o("→ trying  "+path+"\n"),
		miss("→ miss   no exact match\n"),
		o("→ trying  "+path+"/*\n"),
		miss("→ miss   no prefix subtree\n"),
		o("→ fallback handler:\n"),
		ok("→ 404 screen + suggestions\n"),
	)

	didYouMean := ui.Card(ui.CardConfig{Heading: "Did you mean", HeadingLevel: 2, Variant: ui.CardOutlined},
		ui.Stack(ui.StackConfig{Gap: ui.GapXS},
			suggest("/get-started", "Get started →"),
			suggest("/docs/", "Docs index →"),
			suggest("/examples", "Examples →"),
			suggest("/", "Home →"),
		),
	)

	return ui.Container(ui.ContainerConfig{Pad: ui.ContainerPadPage},
		ui.Stack(ui.StackConfig{Gap: ui.GapXL},
			head,
			requested,
			ui.Grid(ui.GridConfig{Min: "20rem", Gap: ui.GapLG}, ui.Stack(ui.StackConfig{}, trace), didYouMean),
		),
	)
}
