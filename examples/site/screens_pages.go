package main

// =============================================================================
// screens_pages.go, the remaining site pages: get started, the docs index,
// examples, Kiln, philosophy and the 404. Every page composes stock
// framework/ui components and layout primitives on the stock theme; the
// site ships no stylesheet and no class names of its own. The pages that
// read as an article beside a rail (get started, philosophy) render
// through the site's owned docpage package; the docs index and the
// /docs/<slug> pages render inside the docs layer, which draws the same
// shell once (docsLayout, layout.go).
//
// The pages share helpers from screen_home.go (container) and from
// code_block.go (codeBlock, kw, fn_, str_, pn, ty, com).
// =============================================================================

import (
	"context"
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

// tagAccent, the version pill used in multiple page heroes. Thin adapter
// over the framework's ui.StatusPill (accent tone + dot).
func tagAccent(label string) render.HTML {
	return ui.StatusPill(ui.StatusPillConfig{Label: label, Tone: ui.StatusPillAccent, Dot: true})
}

// experimentalPill is the sitewide "this is experimental" marker for
// Kiln surfaces. Neutral tone + dot so it reads as a status without
// competing with the accent pills. Kiln is the framework's most
// provisional surface: its in-memory IR, journal-freeze format, and
// app spec graduation flow may still change. Used on the Kiln hero
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

func (s *GetStartedScreen) ScreenTitle(ctx context.Context) string { return "Get started" }
func (s *GetStartedScreen) ScreenDescription(ctx context.Context) string {
	return "Cold machine to a running GoFastr app in four minutes."
}
func (s *GetStartedScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// Render is a tutorial article: the step rail beside it, the steps in the
// reading column, and where to go next under them.
func (s *GetStartedScreen) Render() render.HTML {
	return docpage.Render(docpage.Config{Nav: railColumn(gsRail())},
		pageBody(gsHero(), gsBody(), gsNext()))
}

// gsPath is the tutorial's steps in order, the anchor and the heading
// of each: the rail and the step sections both read it, so a rail entry
// always names the section it scrolls to.
var gsPath = []struct{ ID, Title string }{
	{"s1", "Install"},
	{"s2", "Scaffold"},
	{"s3", "Run it"},
	{"s4", "First page"},
	{"s5", "First entity (optional)"},
	{"s6", "What you have"},
}

// gsRail lists the steps and marks the one in view; on phones it folds
// into a pill naming that step (nothing else on this page holds the
// bottom of the screen).
func gsRail() render.HTML {
	items := make([]ui.RailItem, len(gsPath))
	for i, st := range gsPath {
		items[i] = ui.RailItem{Eyebrow: fmt.Sprintf("%02d", i+1), Text: st.Title, Anchor: st.ID}
	}
	return ui.AnchoredRail(ui.AnchoredRailConfig{
		Label:           "The path",
		Items:           items,
		ObserveSelector: "#gs-steps",
		PhoneSheet:      true,
	})
}

func gsHero() render.HTML {
	// The facts are a label/value list under the pitch, not a card grid.
	text := func(s string) render.HTML { return render.Text(s) }
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		ui.Hero(ui.HeroConfig{
			Eyebrow:   "Get started · v" + siteVersion,
			Title:     "From zero to a running app in four minutes.",
			Subtitle:  "Install the CLI, scaffold plain Go you own, run gofastr dev, and add your first screen. Prefer barebones? Pass --no-entity (skips the entity, auth screens, and database) or import core/ directly.",
			AriaLabel: "Get started",
		}),
		ui.DetailList(ui.DetailListConfig{Inline: true, Items: []ui.DetailItem{
			{Label: "Prereqs", Value: text("Go 1.27.4+, git")},
			{Label: "OS", Value: text("macOS, Linux, Windows (WSL)")},
			{Label: "Storage", Value: text("SQLite by default, Postgres opt-in")},
			{Label: "Time", Value: text("~4 minutes")},
		}}),
	)
}

func gsBody() render.HTML {
	// One step: a section whose id is the rail's anchor, the step number
	// and its time budget as the eyebrow.
	step := func(n int, time string, body ...render.HTML) render.HTML {
		return ui.Section(ui.SectionConfig{
			ID:      gsPath[n-1].ID,
			Eyebrow: fmt.Sprintf("Step %02d · %s", n, time),
			Heading: gsPath[n-1].Title,
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

	step1 := step(1, "~30s",
		p(t("One binary covers scaffold, migrate, dev, build, test, and the doc browser. Get it from GitHub:")),
		termBlock("$ install",
			render.Text("$ go install github.com/DonaldMurillo/gofastr/cmd/gofastr@"+siteInstallTarget()+"\n"),
			o("$ gofastr --version\n"),
			ok("gofastr v"+siteVersion+"\n"),
		),
		callout("If gofastr isn't found", "Make sure $GOPATH/bin (or ~/go/bin) is in your PATH. Run echo $PATH and add the missing entry to your shell rc."),
	)

	step2 := step(2, "~45s",
		p(t("Scaffold a flat project in plain Go: main.go, a home screen in screens.go, DESIGN.md, agent onboarding files (AGENTS.md, CLAUDE.md, .agents/), plus auth screens, SQLite, and a sample entity (pass --no-entity to skip the entity, auth screens, and database).")),
		termBlock("$ scaffold",
			render.Text("$ gofastr init blog\n"),
			ok("✓ Created project blog in ./blog/: main.go, screens.go, screens_auth.go, entities/entities.go, gofastr.settings.yml, DESIGN.md, CLAUDE.md, AGENTS.md\n"),
			ok("→ next: cd blog && go mod tidy && gofastr dev\n"),
		),
		p(t("Open "), codeText("main.go"), t(". Everything in it is normal Go you can read, edit, or replace with plain net/http.")),
	)

	step3 := step(3, "~60s",
		p(t("Resolve dependencies once, then start the dev server. It rebuilds on save, reloads the browser, and hands your coding agent the app over MCP.")),
		termBlock("$ run",
			render.Text("$ go mod tidy\n"),
			render.Text("$ gofastr dev\n"),
			ok("→ Watching . for changes (.go, .js, .css, .html, .md)...\n"),
			ok("→ blog server ready: http://localhost:8080\n"),
		),
		p(t("Open "), codeText("localhost:8080"), t(" to see the scaffolded home screen. Everything works with or without "), codeText("--no-entity"), t(".")),
	)

	step4 := step(4, "~60s",
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
		callout("Tip", "Run `gofastr docs` to browse all embedded docs offline, including ui-getting-started, entity-declarations, and hooks-and-transactions."),
	)

	step5 := step(5, "~60s",
		callout("Used --no-entity?", "You can skip straight to step 6—entities, auth screens, and database wiring are all opt-in."),
		p(t("If you kept the full-stack scaffold, "), codeText("entities/entities.go"), t(" holds a sample entity. One declaration wires the table, REST API, validation, OpenAPI, and MCP tools:")),
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
		p(t("Hit the API from a second terminal:")),
		termBlock("$ probe",
			o("$ curl -s http://localhost:8080/api/posts\n"),
			ok("{\"error\":\"authentication required\",\"success\":false,…}   # 401\n"),
		),
		p(t("That 401 is the point: auto-CRUD refuses anonymous requests unless you opt out. Add "), codeText("Public: true"), t(" to the entity, save, and the dev server rebuilds—the same curl now answers "), codeText("{\"data\":[]}"), t(". Or keep the gate and sign in: create an account at "), codeText("/register"), t(", sign in at "), codeText("/login"), t(", and the browser session reaches the same route ("), codeText("gofastr docs auth"), t("). The matching versioned migration lives next to the entity ("), codeText("gofastr docs migrations"), t(").")),
	)

	li := func(s string) render.HTML { return html.ListItem(html.ListItemConfig{}, render.Text(s)) }
	step6 := step(6, "now",
		p(t("Four minutes in, here is what is on disk and running:")),
		ui.Card(ui.CardConfig{Heading: "Running, on disk, yours", Variant: ui.CardOutlined},
			html.UnorderedList(html.ListConfig{},
				li("Server-rendered home and about screens in plain Go"),
				li("Sign-in and registration at /login and /register over battery/auth (skipped with --no-entity)"),
				li("A session-gated sample entity at /api/posts with a versioned migration (skipped with --no-entity)"),
				li("An OpenAPI 3 spec at /openapi.json and interactive reference at /api/docs/"),
				li("Dev MCP tools under gofastr dev so Claude Code or Codex can read routes, logs, and framework docs"),
				li("AGENTS.md, .agents/, and DESIGN.md ready for your coding agent"),
			),
		),
	)

	return ui.Stack(ui.StackConfig{ID: "gs-steps", Gap: ui.Gap2XL}, step1, step2, step3, step4, step5, step6)
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
		ui.Grid(ui.GridConfig{Min: "16rem"},
			card("Browse the docs", fmt.Sprintf("%d docs grouped by what you're trying to do.", docCount()), "/docs/"),
			card("Read an example", fmt.Sprintf("%d full apps you can clone and modify.", len(exRowItems())), "/examples"),
			card("Try Kiln", "Build the app by chatting with an agent; freeze the result into an app spec you commit.", "/kiln", experimentalPill()),
			card("Stuck?", "Ask in GitHub Discussions.", "https://github.com/DonaldMurillo/gofastr/discussions"),
		),
	)
}

// =============================================================================
// /docs/  (concepts index)
// =============================================================================

type ConceptsIndexScreen struct{}

func (s *ConceptsIndexScreen) ScreenTitle(ctx context.Context) string { return "Docs" }
func (s *ConceptsIndexScreen) ScreenDescription(ctx context.Context) string {
	return "Every feature, grouped by what you're trying to do, not alphabetically."
}
func (s *ConceptsIndexScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// Render is the docs index's article: the hero and the intent groups. It
// renders in the docs layer (docsLayout), which brings the docs nav; the
// intent rail is this screen's fill of the layer's rail outlet
// (DocsIndexRail).
func (s *ConceptsIndexScreen) Render() render.HTML {
	return pageBody(cxHero(), cxBody())
}

// DocsIndexRail fills the docs layer's rail outlet on /docs/ with the
// intent rail; the doc pages leave the outlet empty.
type DocsIndexRail struct{}

func (r *DocsIndexRail) Render() render.HTML { return cxRail() }

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
		Label:           "On this page",
		Items:           items,
		ObserveSelector: "#docs-sections",
		// On phones the rail folds into a bar under the header: the
		// docs nav's pill holds the bottom of the screen.
		PhoneSheet:    true,
		PhoneSheetTop: true,
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

func (s *ExamplesScreen) ScreenTitle(ctx context.Context) string { return "Examples" }
func (s *ExamplesScreen) ScreenDescription(ctx context.Context) string {
	return fmt.Sprintf("%d reference apps. Each runs in one command.", len(exRowItems()))
}
func (s *ExamplesScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ExamplesScreen) Render() render.HTML {
	// The examples layer (sectionNavLayout) owns the frame and its
	// padding; the page is just its content.
	return pageBody(exHero(), exRows())
}

func exHero() render.HTML {
	// The theme-layer showcases and the in-site demo apps are listed in
	// the examples nav beside this page (examplesSectionMenuConfig,
	// derived from landingRoutes, so a new theme cannot miss it); the
	// hero points there instead of repeating them as buttons.
	return ui.Hero(ui.HeroConfig{
		Eyebrow: fmt.Sprintf("Examples · %d apps", len(exRowItems())),
		Title:   fmt.Sprintf("%d reference apps. Each runs in one command.", len(exRowItems())),
		Lede: render.Join(
			render.Text("Meridian is a whole SaaS app to clone and reshape; this site shows every component. Each app's full source is under "),
			ui.InlineCode("examples/"),
			render.Text(" in the repo. Run it with "),
			ui.InlineCode("gofastr dev"),
			render.Text(" for rebuild-on-save, livereload, and the dev MCP tools; plain "),
			ui.InlineCode("go run ."),
			render.Text(" works too. The in-site demo apps and the theme-layer showcases are in the menu."),
		),
		AriaLabel: "Examples",
	})
}

// exampleLoC is the non-test Go line count each row's badge cites, keyed
// by the example directory and rounded to two significant figures.
// TestExampleLoCBadgesMatchTree measures examples/<slug> and fails when a
// badge is more than 20% off, so the numbers are re-measured instead of
// remembered.
var exampleLoC = map[string]int{
	"site": 12000,
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
			[]string{"One app spec seeded marketing + app + auth + admin", "Server-rendered DataTable / charts / forms, island RPCs", "Writable CRUD + RBAC, with a generated end-to-end test suite"},
			"cd examples/meridian && gofastr dev",
			// The exact, full app spec that generates the app, embedded at
			// build time, shown verbatim in a scrolling block. Drift-guarded by
			// TestEmbeddedAppSpecsMatchSource.
			codeBlockScroll("examples/meridian/gofastr.yml", meridianAppSpecYAML, "yaml")),
		exRow("02", "examples/site", "This site (UI showcase)", "largest", locBadge("site", ""),
			"Every core-ui pattern + framework/ui component, one page each, plus the docs, SEO, multi-step wizard, and print-battery demos. The site you're reading right now.",
			[]string{"Every core-ui pattern + framework/ui component", "Docs, philosophy, examples, Kiln pages", "SEO interfaces, sitemap/robots, wizard, print"},
			"cd examples/site && gofastr dev",
			codeBlock("examples/site/main.go", []render.HTML{
				ln(render.Text("host "), pn(":="), render.Text(" uihost."), fn_("New"), pn("("), render.Text("site"), pn(", …)")),
				ln(render.Text("app "), pn(":="), render.Text(" framework."), fn_("NewUIHostApp"), pn("("), render.Text("host"), pn(")")),
				ln(render.Text("app."), fn_("Start"), pn("("), str_(`":8083"`), pn(")")),
			})),
	}
}

func exRows() render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.Gap2XL}, exRowItems()...)
}

// exRowBodyConfig is the left column of one example row. Points are
// pre-built <li>s so a row can decide whether a point is text or a link.
type exRowBodyConfig struct {
	Tag, LoC, Desc, Command string
	Points                  []render.HTML
	Source                  string      // "View source" href; empty hides the link
	Extra                   render.HTML // optional trailing block (the app spec row's source links)
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
// snippet for most rows; the full embedded app spec for Meridian); path
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

func (s *KilnScreen) ScreenTitle(ctx context.Context) string { return "Kiln" }
func (s *KilnScreen) ScreenDescription(ctx context.Context) string {
	return "Build a GoFastr app live by chatting with an agent."
}
func (s *KilnScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *KilnScreen) Render() render.HTML {
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerWide, Pad: ui.ContainerPadPage},
		pageBody(kHero(), kTimeline(), kCaps(), kCli()))
}

// kInstallCmd is the one-line install shown in the hero and the CLI section.
const kInstallCmd = "go install github.com/DonaldMurillo/gofastr/cmd/kiln@latest"

func kHero() render.HTML {
	// The demo panel fills the hero's Media slot; the install command
	// sits under the actions in the hero's Footer, scrolling inside its
	// own frame on a phone, and the copy button carries it intact.
	install := ui.CodeBlock(ui.CodeBlockConfig{
		Filename: "install",
		Language: "shell",
		Lines:    []render.HTML{ln(pn("$ "), render.Text(kInstallCmd))},
		ShowCopy: true,
	})
	return ui.Hero(ui.HeroConfig{
		Eyebrow:   "kiln: agent build mode",
		Title:     "Talk an app into being.",
		Subtitle:  "Kiln is experimental: a separate binary that mounts a chat panel on your running GoFastr app. The agent calls typed tools; the in-memory IR mutates; the schema migrates; the app re-renders, all in-process. Freeze the journal when done to emit the canonical entity files you commit.",
		AriaLabel: "Kiln",
		Media:     kDemo(),
		Actions: []render.HTML{
			ui.LinkButton(ui.LinkButtonConfig{Label: "Read the docs", Href: "/docs/kiln", Variant: ui.ButtonPrimary, Size: ui.ButtonSizeLarge}),
			experimentalPill(),
		},
		Footer: install,
	})
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
			evt("25s", ui.TimelineNeutral, "Journal freezable", "kiln freeze --dir build/ snapshots the world; graduate to Go via a gofastr.yml app spec."),
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

func (s *PhilosophyScreen) ScreenTitle(ctx context.Context) string { return "Philosophy" }
func (s *PhilosophyScreen) ScreenDescription(ctx context.Context) string {
	return "The convictions behind GoFastr: layered plain Go you own, server-rendered UI, agent rails, and what we deliberately leave out."
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
			Eyebrow:   "Philosophy",
			Title:     "Simple for humans. Optimized for agents.",
			Subtitle:  "Go from idea to a full-stack app, fastr—without locking yourself into a black box. GoFastr stays plain Go on disk so you can be as barebones or as batteries-included as the app needs, and your coding agents can work in the same repo without leaving a mess.",
			AriaLabel: "Philosophy",
		}),
		html.Paragraph(html.TextConfig{}, ui.Muted(render.Text("By Donald Murillo"))),
	)
}

// phSections are the essay's h2s: the contents rail's entries, keyed by
// the ids ui.Markdown derives from the heading text.
var phSections = []ui.TOCItem{
	{ID: "why-this-exists", Label: "Why this exists"},
	{ID: "layers-not-lock-in", Label: "Layers, not lock-in"},
	{ID: "convictions", Label: "Convictions"},
	{ID: "where-agents-fit", Label: "Where agents fit"},
	{ID: "where-it-stands", Label: "Where it stands"},
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
	"Building a prototype or a small-to-medium web app shouldn't require stitching together a React SPA, a Node build pipeline, a separate backend repo, hand-rolled auth, and a pile of glue code before you ship your first screen. And when coding agents help write that glue, opaque frameworks and magic conventions turn messy fast.\n\n" +
	"GoFastr takes the opposite bet: one Go binary, plain Go files you own on disk, server-rendered UI with interactive islands (no JS build step), and built-in rails for both humans and agents.\n\n" +
	"> The right abstraction makes the simple case fast and the custom case plain Go. The wrong abstraction makes you fight it in both.\n\n" +
	"## Layers, not lock-in\n\n" +
	"GoFastr doesn't force an all-or-nothing buy-in. `core/` and `core-ui/` are stdlib-first building blocks—router, typed handlers, HTML rendering, SQL query builder, schema, migrations, MCP, signals—that work completely standalone. `framework/` and `battery/` compose them when you want full-stack speed: UI components, auth, queues, search, optional CRUD entities, and contract checks.\n\n" +
	"Stay barebones or go full stack per feature. Use `framework/` where it saves hours; drop down to `core/`, `net/http`, or `database/sql` anywhere in the same app.\n\n" +
	"## Convictions\n\n" +
	"1. **Barebones or batteries-included, your call.** Start with a 30-line `core/` server or `gofastr init` a full app; mix both freely.\n" +
	"2. **Plain Go you own.** No reflection magic, no hidden runtime schema store, no `DO NOT EDIT` scaffold lock-in. Even `gofastr generate app` scaffolds once and gets out of the way.\n" +
	"3. **Server-rendered UI without a JS framework.** Go renders HTML on first load; a tiny runtime handles client-side navigation, server islands, signals, polling, and SSE.\n" +
	"4. **Batteries are opt-in packages.** Auth, queues, search, email, storage, admin, webhooks—narrow interfaces, import only what you use.\n" +
	"5. **Optimized for agents on both sides.** `gofastr dev` and `gofastr verify` keep coding agents grounded while building; opt-in `/mcp`, `llm.md`, and `/llms.txt` serve user agents in production under the same permissions.\n" +
	"6. **Sharp boundaries on what we leave out.** SQLite (`CGO_ENABLED=0`) and Postgres only—no MySQL, no MongoDB, no hosted cloud platform, and no Node/npm build step.\n\n" +
	"## Where agents fit\n\n" +
	"While you build, `gofastr dev` exposes your running app's routes, config, docs, and logs to Claude Code or Codex over MCP, and `gofastr verify` runs static and semantic contract checks so security, routing, and UI mistakes fail loudly instead of slipping into main.\n\n" +
	"Once you ship, opt-in options expose `/mcp` tools behind the same sessions, RBAC, and owner/tenant scopes as your HTTP API, alongside per-screen `llm.md`, `/llms.txt`, and `.well-known` discovery.\n\n" +
	"## Where it stands\n\n" +
	"| Area | Focus | Status |\n" +
	"| --- | --- | --- |\n" +
	"| Core + UI + Batteries | Layered Go stack, SSR + islands, auth, SQLite/Postgres | ✓ shipped |\n" +
	"| Agent loop + Contracts | Dev/prod MCP, `llm.md`, `gofastr verify` contract catalog | ✓ shipped |\n" +
	"| Production dogfooding | Barcode Maker, this docs site, Meridian flagship | ✓ live |\n" +
	"| v1.0 freeze | Lock public `framework` + `core-ui` APIs after external adoption | in progress |\n\n" +
	"## A note on this site\n\n" +
	"This site is built with GoFastr itself. Every page is server-rendered from `framework/ui` components on the stock theme with zero separate frontend build, and every code sample in the hero, `/get-started`, and `/framework` is compiled and tested in CI.\n\n" +
	"If something feels clunky here, we fix it in the framework so every app gets better.\n\n" +
	"## Notes & references\n\n" +
	"1. The HTTP and handler model builds directly on Go's `net/http` standard library.\n" +
	"2. MCP: the Model Context Protocol, powering both dev-time introspection and production agent tools.\n" +
	"3. The barebones-to-framework layering follows Rich Hickey's distinction between simple and easy.\n"

func phBody() render.HTML {
	return ui.Markdown(ui.MarkdownConfig{Source: phEssay, ID: "philosophy-essay"})
}

// =============================================================================
// /404  (registered as the custom NotFound handler, see main.go)
// =============================================================================

type NotFoundScreen struct{}

func (s *NotFoundScreen) ScreenTitle(ctx context.Context) string       { return "404: Not found" }
func (s *NotFoundScreen) ScreenDescription(ctx context.Context) string { return "" }
func (s *NotFoundScreen) ScreenType() app.ScreenType                   { return app.ScreenPage }

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
