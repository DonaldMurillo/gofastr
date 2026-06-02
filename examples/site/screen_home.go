package main

// =============================================================================
// Home — top-level outline (matches pages/home-v2.html in the design bundle):
//
//   HERO        pre-alpha tag · h1 with amber span · 2 ledes · 2 CTAs · install
//               RHS: code block (blog/main.go, hand-tokenized in code_block.go)
//   §01         release-notes-style list (7 rows, number · name · desc · file)
//   §02         arch cards: core / framework / batteries / core-ui
//   §03         split pane: framework MCP (left) | Kiln (right + terminal mock)
//   §04         6 example cards, each with path + name + desc + run command
//   §05         pre-alpha disclosure + roadmap dl
//
// Sections live inside <main> only — .nav and .foot are owned by the
// HeaderComponent / FooterComponent. Built with core-ui/html primitives
// (Heading, Link, UnorderedList, ListItem, DescriptionList…) so attribute
// escaping and landmark roles come from typed builders. Page-local layout
// classes (.hero__grid, .arch-card, .agents__split, etc.) are styled in
// styles.go via the typed StyleSheet DSL — no raw CSS strings.
//
// Framework component fit-check: ui.Container / ui.Card / ui.Tag were
// considered. Each ships its own visual chrome that diverges from the v2
// tokens, so adopting them here would either fight the design or require
// shadowing their CSS. The right time to extract is when a second consumer
// needs the same pattern — the porting target then is framework/ui/SiteCard,
// framework/ui/AccentTag, etc.
// =============================================================================

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type HomeScreen struct{}

// ScreenTitle returns the bare page name; core-ui/app appends " — GoFastr"
// (the app name) to form the <title>, so it must NOT be repeated here.
func (s *HomeScreen) ScreenTitle() string { return "Full-stack Go, with agents at the table" }
func (s *HomeScreen) ScreenDescription() string {
	return "A pre-alpha Go full-stack framework where AI agents are first-class authors. Declare your domain in Go; get REST, MCP tools, OpenAPI, migrations, and a typed client — to disk, in plain Go."
}
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *HomeScreen) Render() render.HTML {
	return render.Join(
		heroSection(),
		generatesSection(),
		architectureSection(),
		agentsSection(),
		examplesSection(),
		alphaSection(),
	)
}

// container is the site's max-width wrapper. Delegates to
// ui.Container(ContainerWide) — the wide cap is overridden to 1240px
// via the --ui-container-wide token in styles.go's tokens block.
func container(children ...render.HTML) render.HTML {
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerWide}, children...)
}

// -----------------------------------------------------------------------------
// HERO — two-col grid: copy on the left, code block on the right.
// -----------------------------------------------------------------------------

func heroSection() render.HTML {
	preAlphaTag := html.Div(
		html.DivConfig{Class: "mb-lg"},
		ui.StatusPill(ui.StatusPillConfig{Label: "pre-alpha · v0.0.4", Tone: ui.StatusPillAccent, Dot: true}),
	)

	title := html.Heading(html.HeadingConfig{Level: 1, Class: "hero__title"},
		render.Text("Full-stack Go, with "),
		html.Span(html.TextConfig{Class: "amber"}, render.Text("agents at the table")),
		render.Text("."),
	)

	lede1 := html.Paragraph(html.TextConfig{Class: "hero__lede"},
		html.Strong(html.TextConfig{}, render.Text("GoFastr")),
		render.Text(" is a Go full-stack framework. You wire your app in Go like you'd wire "),
		html.Code(html.TextConfig{}, render.Text("net/http")),
		render.Text(". The framework generates REST endpoints, MCP tools, an OpenAPI spec, SQL migrations, and a typed query builder — to disk, in plain Go you can read and step through."),
	)
	lede2 := html.Paragraph(html.TextConfig{Class: "hero__lede"},
		render.Text("The same surface is wired for AI agents from day one. Every entity ships with an MCP tool surface; "),
		html.Strong(html.TextConfig{}, render.Text("Kiln")),
		render.Text(" is a separate binary that lets an agent author your app in chat while you watch the code change."),
	)

	ctas := html.Div(html.DivConfig{Class: "hero__ctas"},
		ui.LinkButton(ui.LinkButtonConfig{Label: "Get started", Href: "/get-started", Variant: ui.ButtonPrimary, Size: ui.ButtonSizeLarge}),
		ui.LinkButton(ui.LinkButtonConfig{Label: "Read the docs", Href: "/docs/", Variant: ui.ButtonGhost, Size: ui.ButtonSizeLarge}),
	)

	install := html.Div(html.DivConfig{Class: "hero__install"},
		html.Span(html.TextConfig{Class: "p"}, render.Text("$")),
		render.Text(" go install github.com/DonaldMurillo/gofastr/cmd/gofastr@latest"),
	)

	copy := render.Join(preAlphaTag, title, lede1, lede2, ctas, install)

	return html.Section(html.SectionConfig{Class: "hero", Label: "Hero"},
		container(ui.HeroSplit(ui.HeroSplitConfig{
			Copy:      copy,
			Media:     heroCodeBlock(),
			Ratio:     ui.HeroSplitMediaWide,
			AriaLabel: "Hero",
			Class:     "hero-home",
		})),
	)
}

// heroCodeBlock — the hand-tokenized blog/main.go shown in the hero. Built
// line by line so a reader of this file can match the visual output 1:1.
// Token helpers (kw, fn_, str_, pn, ty, com) live in code_block.go.
func heroCodeBlock() render.HTML {
	lines := []render.HTML{
		ln(kw("package"), render.Text(" main")),
		ln(),
		ln(kw("import"), render.Text(" "), pn("(")),
		ln(render.Text("  "), str_(`"github.com/DonaldMurillo/gofastr/framework"`)),
		ln(render.Text("  "), str_(`"github.com/DonaldMurillo/gofastr/battery/auth"`)),
		ln(render.Text("  f "), str_(`"github.com/DonaldMurillo/gofastr/framework/field"`)),
		ln(pn(")")),
		ln(),
		ln(kw("func"), render.Text(" "), fn_("main"), pn("()"), render.Text(" "), pn("{")),
		ln(render.Text("  app "), pn(":="), render.Text(" framework."), fn_("New"), pn("("), render.Text("framework."), ty("Config"), pn("{")),
		ln(render.Text("    Name"), pn(":"), render.Text(" "), str_(`"blog"`), pn(",")),
		ln(render.Text("    DB"), pn(":"), render.Text("   framework."), fn_("SQLite"), pn("("), str_(`"./blog.db"`), pn("),")),
		ln(render.Text("  "), pn("})")),
		ln(),
		ln(render.Text("  app."), fn_("Use"), pn("("), render.Text("auth."), fn_("Memory"), pn("())"), render.Text("  "), com("// swap for auth.Postgres() in prod")),
		ln(),
		ln(render.Text("  "), com("// One call. Generates SQL, REST, MCP, OpenAPI, Go types.")),
		ln(render.Text("  app."), fn_("Entity"), pn("("), str_(`"posts"`), pn(","), render.Text(" framework."), ty("Entity"), pn("{")),
		ln(render.Text("    Fields"), pn(":"), render.Text(" framework."), ty("Fields"), pn("{")),
		ln(render.Text("      "), str_(`"title"`), pn(":"), render.Text("  f."), fn_("String"), pn("()."), fn_("Required"), pn("(),")),
		ln(render.Text("      "), str_(`"slug"`), pn(":"), render.Text("   f."), fn_("Slug"), pn("()."), fn_("From"), pn("("), str_(`"title"`), pn("),")),
		ln(render.Text("      "), str_(`"body"`), pn(":"), render.Text("   f."), fn_("Markdown"), pn("(),")),
		ln(render.Text("      "), str_(`"status"`), pn(":"), render.Text(" f."), fn_("Enum"), pn("("), str_(`"draft"`), pn(","), render.Text(" "), str_(`"published"`), pn(")."), fn_("Default"), pn("("), str_(`"draft"`), pn("),")),
		ln(render.Text("    "), pn("},")),
		ln(render.Text("    Timestamps"), pn(":"), render.Text(" "), kw("true"), pn(",")),
		ln(render.Text("    Expose"), pn(":"), render.Text("     framework."), ty("Expose"), pn("{"), render.Text("REST"), pn(":"), render.Text(" "), kw("true"), pn(","), render.Text(" MCP"), pn(":"), render.Text(" "), kw("true"), pn("},")),
		ln(render.Text("  "), pn("})")),
		ln(),
		ln(render.Text("  app."), fn_("Serve"), pn("("), str_(`":8080"`), pn(")")),
		ln(pn("}")),
	}
	return codeBlock("blog/main.go", lines)
}

// -----------------------------------------------------------------------------
// §01 — release-notes-style list of generated surfaces.
// -----------------------------------------------------------------------------

func generatesSection() render.HTML {
	row := func(n, name string, desc, file render.HTML) render.HTML {
		return html.Div(html.DivConfig{Class: "gen-row"},
			html.Div(html.DivConfig{Class: "n"}, render.Text(n)),
			html.Div(html.DivConfig{Class: "name"}, render.Text(name)),
			html.Div(html.DivConfig{Class: "desc"}, desc),
			html.Div(html.DivConfig{Class: "file"},
				html.Code(html.TextConfig{}, file),
			),
		)
	}
	codeText := func(s string) render.HTML { return html.Code(html.TextConfig{}, render.Text(s)) }

	list := html.Div(html.DivConfig{Class: "gen-list"},
		row("01", "SQL table + migration",
			render.Join(
				render.Text("Versioned, ordered, reversible. "),
				codeText("up"), render.Text("/"), codeText("down"),
				render.Text(" SQL emitted as plain files."),
			),
			render.Text(".gofastr/migrations/")),
		row("02", "REST endpoints",
			render.Text("List, get, create, update, delete, batch. Cursor + offset pagination. Filter DSL on the query string."),
			render.Text("GET POST /posts")),
		row("03", "MCP tool surface",
			render.Join(
				codeText("posts_list"), render.Text(", "),
				codeText("posts_get"), render.Text(", "),
				codeText("posts_create"), render.Text(", "),
				codeText("posts_update"), render.Text(", "),
				codeText("posts_delete"),
				render.Text(". Same auth + access rules as REST."),
			),
			render.Text("core/mcp")),
		row("04", "OpenAPI 3 spec",
			render.Join(
				render.Text("Schema, parameters, responses. Swagger UI auto-mounted at "),
				codeText("/api/docs"), render.Text("."),
			),
			render.Text("/openapi.json")),
		row("05", "Typed Go model + query builder",
			render.Join(
				codeText(`posts.Query(ctx).Where(posts.Status.Eq("published")).List(20)`),
				render.Text(". No reflection, no "), codeText("interface{}"), render.Text("."),
			),
			render.Text(".gofastr/entities/posts.go")),
		row("06", "Lifecycle hook slots",
			render.Join(
				codeText("BeforeCreate"), render.Text(", "),
				codeText("AfterUpdate"), render.Text(", "),
				codeText("BeforeDelete"),
				render.Text(" — your code runs in the parent transaction."),
			),
			render.Text("framework/hook")),
		row("07", "Admin UI (opt-in)",
			render.Join(
				render.Text("A listing + form per entity. Off by default. Add "),
				codeText("Admin: true"), render.Text(" to enable."),
			),
			render.Text("/admin/posts")),
	)

	head := sectionHead(
		"One entity call generates the surfaces your app and your agent both need.",
		render.Join(
			render.Text("Output lands on disk under "),
			codeText(".gofastr/"),
			render.Text(" as regular Go and SQL. There is no reflection at runtime; if something the framework does feels like magic, open the generated file and read it."),
		),
	)

	return sectionWrap("01 / what it generates", "What it generates", head, list)
}

// -----------------------------------------------------------------------------
// §02 — Architecture cards (custom .arch-card, not framework/ui.Card — the
// latter ships its own padding/border tokens that fight v2).
// -----------------------------------------------------------------------------

func architectureSection() render.HTML {
	card := func(title, pkg, lede string, members render.HTML) render.HTML {
		return html.Article(html.ArticleConfig{Class: "arch-card"},
			html.Heading(html.HeadingConfig{Level: 4}, render.Text(title)),
			html.Div(html.DivConfig{Class: "pkg"}, render.Text(pkg)),
			html.Paragraph(html.TextConfig{}, render.Text(lede)),
			html.Div(html.DivConfig{Class: "members"}, members),
		)
	}
	b := func(s string) render.HTML { return html.Strong(html.TextConfig{}, render.Text(s)) }
	br := render.Raw("<br>")

	core := card("The floor", "core/", "Twelve primitives. Stdlib only. Works standalone.",
		render.Join(b("router"), render.Text("  "), b("handler"), render.Text("  "), b("middleware"), br,
			b("query"), render.Text("  "), b("mcp"), render.Text("  "), b("schema"), render.Text("  "), b("migrate"), br,
			b("render"), render.Text("  "), b("stream"), render.Text("  "), b("openapi"), br,
			b("static"), render.Text("  "), b("upload"),
		))

	framework := card("The frame", "framework/", "The opinionated entity system, composed on top of core. ~25 packages.",
		render.Join(b("entity"), render.Text("  "), b("crud"), render.Text("  "), b("filter"), render.Text("  "), b("dsl"), br,
			b("hook"), render.Text("  "), b("migrate"), render.Text("  "), b("tenant"), render.Text("  "), b("access"), br,
			b("file"), render.Text("  "), b("cron"), render.Text("  "), b("event"), render.Text("  "), b("log"), br,
			b("ui"), render.Text("  …and more"),
		))

	batteries := card("Pluggable", "battery/", "One interface per concern. In-memory dev driver, production driver behind the same shape. Swap without forking.",
		render.Join(b("auth"), render.Text("  "), b("cache"), render.Text("  "), b("email"), render.Text("  "), b("embed"), br,
			b("notify"), render.Text("  "), b("queue"), render.Text("  "), b("search"), render.Text("  "), b("storage"), br,
			b("webhook"), render.Text("  "), b("log"), render.Text("  "), b("admin"), render.Text("  "), b("print"),
		))

	coreUI := card("UI runtime", "core-ui/", "Server-driven. Signals + HTML primitives + islands + SSE. ~10 KB gz vanilla JS. No React.",
		render.Join(b("signal"), render.Text("  "), b("html"), render.Text("  "), b("island"), render.Text("  "), b("stream"), br,
			b("accordion"), render.Text("  "), b("tabs"), render.Text("  "), b("modal"), br,
			b("drawer"), render.Text("  "), b("popover"), render.Text("  "), b("toast"),
		))

	grid := html.Div(html.DivConfig{Class: "arch__grid"}, core, framework, batteries, coreUI)

	head := sectionHead(
		"Two layers. Twelve batteries. Drop down whenever.",
		render.Join(
			html.Code(html.TextConfig{}, render.Text("core/")),
			render.Text(" is stdlib-only Go primitives. "),
			html.Code(html.TextConfig{}, render.Text("framework/")),
			render.Text(" is the opinionated entity layer composed on top. If the framework is in your way, you drop down to core — the imports change, your code doesn't."),
		),
	)

	return sectionWrap("02 / architecture", "Architecture", head, grid)
}

// -----------------------------------------------------------------------------
// §03 — Agents split pane.
// -----------------------------------------------------------------------------

func agentsSection() render.HTML {
	codeText := func(s string) render.HTML { return html.Code(html.TextConfig{}, render.Text(s)) }
	li := func(children ...render.HTML) render.HTML {
		return html.ListItem(html.ListItemConfig{}, children...)
	}

	left := html.Div(html.DivConfig{Class: "pane pane--left"},
		html.Span(html.TextConfig{Class: "pane__lbl"}, render.Text("framework")),
		html.Heading(html.HeadingConfig{Level: 4}, render.Text("Every entity is an MCP surface")),
		html.Paragraph(html.TextConfig{},
			render.Text("For each entity you declare, the framework registers MCP tools that map 1:1 to your REST surface. An agent connects to "),
			codeText("/mcp"),
			render.Text(" and calls them with the same auth context a human would have over HTTP."),
		),
		html.UnorderedList(html.ListConfig{},
			li(codeText("posts_list"), render.Text(" — same access rules as "), codeText("GET /posts")),
			li(codeText("posts_create"), render.Text(" — same validators, same hooks")),
			li(codeText("posts_update"), render.Text(", "), codeText("posts_delete"), render.Text(" — same audit log")),
			li(render.Text("Scope per tool with "), codeText(`Expose.MCP.Tools: []string{"list","get"}`)),
		),
	)

	// Faux terminal — Kiln's boot banner. Static markup; not wired to a real
	// stream yet. Once /kiln lands as a real page it can host a live SSE
	// island instead and this becomes the placeholder fallback.
	term := ui.TerminalBlock(ui.TerminalBlockConfig{Label: " $ kiln serve --agent claude-code"},
		ui.TerminalOut("→ panel floats on  http://localhost:8765\n"),
		ui.TerminalOut("→ MCP server live  at  /mcp\n"),
		ui.TerminalOut("→ journal           in  .kiln/journal\n"),
		ui.TerminalOK("→ ready · waiting for the agent."),
	)

	right := html.Div(html.DivConfig{Class: "pane pane--right"},
		html.Span(html.TextConfig{Class: "pane__lbl"}, render.Text("kiln (separate binary)")),
		html.Heading(html.HeadingConfig{Level: 4}, render.Text("An agent that authors your app")),
		html.Paragraph(html.TextConfig{},
			render.Text("Kiln mounts a floating chat panel on the running app. The agent calls a typed tool surface ("),
			codeText("add_entity"), render.Text(", "),
			codeText("add_field"), render.Text(", "),
			codeText("propose_plan"),
			render.Text(", …). Plans render as diffs you approve. The journal is committable."),
		),
		term,
	)

	split := html.Div(html.DivConfig{Class: "agents__split"}, left, right)

	head := sectionHead(
		"The agent's view of your app is the same as yours.",
		render.Text("Same database, same routes, same files on disk. The MCP tool surface is just the REST surface in a different shape. Destructive changes require an approved plan; the agent cannot drop your tables without you clicking approve."),
	)

	return sectionWrap("03 / agents", "Agents", head, split)
}

// -----------------------------------------------------------------------------
// §04 — Examples grid.
// -----------------------------------------------------------------------------

func examplesSection() render.HTML {
	codeText := func(s string) render.HTML { return html.Code(html.TextConfig{}, render.Text(s)) }
	// Each card deep-links to its row on /examples (anchored by slug), so
	// "clone the one that looks like your problem" actually lands on it.
	exCard := func(path, title string, desc render.HTML, cmd string) render.HTML {
		slug := path[len("examples/"):]
		return html.LinkHTML(html.LinkHTMLConfig{
			Href:  "/examples#" + slug,
			Class: "ex-card",
			Content: render.Join(
				html.Span(html.TextConfig{Class: "path"}, render.Text(path)),
				html.Heading(html.HeadingConfig{Level: 4}, render.Text(title)),
				html.Paragraph(html.TextConfig{}, desc),
				html.Code(html.TextConfig{Class: "cmd"}, render.Text(cmd)),
			),
		})
	}

	// Order matches /examples (01–06) so the two pages tell the same story.
	grid := html.Div(html.DivConfig{Class: "ex__grid"},
		exCard("examples/blog", "JSON-declared blog",
			render.Text("Posts, comments, tags. Three entities. The smallest end-to-end example — start here."),
			"cd examples/blog && go run ."),
		exCard("examples/site", "This site",
			render.Text("The site you're reading — every framework/ui + core-ui primitive showcased one page each, plus the docs, SEO, wizard, and print demos."),
			"cd examples/site && go run ."),
		exCard("examples/api-tour", "API tour",
			render.Join(render.Text("Every REST endpoint as a chapter. Each chapter has a live "), codeText("curl"), render.Text(" example you run from the page.")),
			"cd examples/api-tour && go run ."),
		exCard("examples/embed-demo", "Local semantic search",
			render.Join(render.Text("A markdown corpus indexed locally via "), codeText("battery/embed"), render.Text(". No external API key, ~180 LoC total.")),
			"cd examples/embed-demo && go run ."),
		exCard("examples/spa", "Vue + GoFastr API",
			render.Text("For teams who already have a client app. Shows the framework is happy to just be your typed API."),
			"cd examples/spa && go run ."),
		exCard("examples/static-site", "Static-site mode",
			render.Join(render.Text("Same renderer, no server. "), codeText("gofastr build"), render.Text(" emits a CDN-friendly bundle.")),
			"cd examples/static-site && gofastr build"),
	)

	head := sectionHead(
		"Six reference apps. Each runs in one command.",
		render.Text("Clone the one that looks like your problem; swap the entity declarations."),
	)

	return sectionWrap("04 / examples", "Examples", head, grid)
}

// -----------------------------------------------------------------------------
// §05 — Pre-alpha disclosure + roadmap (no section__head; bespoke 2-col grid).
// -----------------------------------------------------------------------------

func alphaSection() render.HTML {
	codeText := func(s string) render.HTML { return html.Code(html.TextConfig{}, render.Text(s)) }

	copy := html.Div(html.DivConfig{Class: "alpha__copy"},
		ui.StatusPill(ui.StatusPillConfig{Label: "pre-alpha · v0.0.4", Tone: ui.StatusPillAccent, Dot: true, Class: "mb-md"}),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Built in public. Use it to learn, not to ship.")),
		html.Paragraph(html.TextConfig{},
			render.Text("APIs change between commits. "),
			codeText("core-ui/"),
			render.Text(" is the active research frontier. The journal is open; rough edges are visible by design. We say no to things — you should know what we're saying no to."),
		),
	)

	dt := func(s string) render.HTML {
		return html.DescriptionTerm(html.TextConfig{}, render.Text(s))
	}
	dd := func(children ...render.HTML) render.HTML {
		return html.DescriptionDetail(html.TextConfig{}, children...)
	}
	when := func(s string) render.HTML { return html.Span(html.TextConfig{Class: "when"}, render.Text(s)) }

	list := html.DescriptionList(html.TextConfig{Class: "alpha__list"},
		dt("honest about"),
		dd(render.Text("Breaking changes between commits — see "),
			html.Link(html.LinkConfig{
				Href: "https://github.com/DonaldMurillo/gofastr/commits/main",
				Text: "the journal",
			})),
		dd(render.Text("Half-built batteries, marked WIP in source")),
		dd(render.Text("No upgrade guide between minor versions yet")),

		dt("will not do"),
		dd(render.Text("Reflection-driven magic that hides what's running")),
		dd(render.Text("Pricing pages, logo clouds, testimonial carousels")),
		dd(render.Text("Lock-in — drop down to "), codeText("core/"), render.Text(" any time")),

		dt("roadmap, in order"),
		dd(render.Text("Lock "), codeText("framework/entity"), render.Text(" ABI"), when("Q3 '26")),
		dd(render.Text("Land "), codeText("core-ui"), render.Text(" 1.0"), when("Q4 '26")),
		dd(render.Text("First version we'd suggest shipping"), when("2027")),
	)

	grid := html.Div(html.DivConfig{Class: "alpha__grid"}, copy, list)

	return ui.Section(ui.SectionConfig{
		Eyebrow: "05 / state of the project",
		Label:   "State of the project",
		Class:   "section-v2",
	}, container(grid))
}

// -----------------------------------------------------------------------------
// Shared section helpers.
// -----------------------------------------------------------------------------

// sectionHead — h2 + lede paragraph, two-column at desktop, stacked at mobile
// (the responsive collapse lives in styles.go's @media block).
func sectionHead(title string, lede render.HTML) render.HTML {
	return html.Header(html.HeaderConfig{Class: "section__head"},
		html.Heading(html.HeadingConfig{Level: 2}, render.Text(title)),
		html.Paragraph(html.TextConfig{}, lede),
	)
}

// sectionWrap — site adapter over ui.Section. The framework component owns
// the <section> landmark, the decorative numeric eyebrow, and the
// scroll-margin that keeps an anchored section clear of the sticky header;
// the site only supplies the eyebrow text, accessible name, the v2 framing
// class, and its max-width container.
func sectionWrap(num, ariaLabel string, head, body render.HTML) render.HTML {
	return ui.Section(ui.SectionConfig{
		Eyebrow: num,
		Label:   ariaLabel,
		Class:   "section-v2",
	}, container(head, body))
}
