package main

// =============================================================================
// Home — top-level outline (matches pages/home-v2.html in the design bundle):
//
//   HERO        version tag · h1 with amber span · 2 ledes · 2 CTAs · install
//               RHS: code block (blog/main.go, hand-tokenized in code_block.go)
//   §01         server-rendered UI — SSR/islands model | screen mock
//   §02         explore grid — 6 route cards into the main areas of the site
//   §03         arch cards: core / framework / batteries / core-ui
//   §04         split pane: framework MCP (left) | Kiln (right + terminal mock)
//   §05         6 example cards, each with path + name + desc + run command
//   §06         v0.x status disclosure + roadmap dl
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
func (s *HomeScreen) ScreenTitle() string { return "Full-stack Go that doesn't get in your way" }
func (s *HomeScreen) ScreenDescription() string {
	return "An early (v0.x) full-stack Go framework that stays out of your way. Declare your domain in Go and get server-rendered screens, a REST API, MCP tools, migrations, and a typed query builder — plain Go you and your agents can read, edit, and own."
}
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *HomeScreen) Render() render.HTML {
	return render.Join(
		heroSection(),
		realAppSection(),
		exploreSection(),
		builtWithSection(),
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
		ui.StatusPill(ui.StatusPillConfig{Label: "early · v" + siteVersion, Tone: ui.StatusPillAccent, Dot: true}),
	)

	title := html.Heading(html.HeadingConfig{Level: 1, Class: "hero__title"},
		render.Text("Full-stack Go that doesn't get in the way of "),
		html.Span(html.TextConfig{Class: "amber"}, render.Text("you or your agents")),
		render.Text("."),
	)

	lede1 := html.Paragraph(html.TextConfig{Class: "hero__lede"},
		html.Strong(html.TextConfig{}, render.Text("GoFastr")),
		render.Text(" is a full-stack Go framework. Declare your domain in Go and get "),
		html.Strong(html.TextConfig{}, render.Text("server-rendered screens")),
		render.Text(", REST endpoints, MCP tools, an OpenAPI spec, SQL migrations, and a typed query builder — all as plain Go on disk that you own."),
	)
	lede2 := html.Paragraph(html.TextConfig{Class: "hero__lede"},
		render.Text("GoFastr is built for both the agentic web and AI-assisted development. The app you ship joins the agentic web: the agents your users bring call your data over MCP, with the same login and permissions your users have. While you build, "),
		html.Code(html.TextConfig{}, render.Text("gofastr dev")),
		render.Text(" hands your coding agent — Claude Code or Codex — the app's routes, config, and logs over MCP, to help build and debug it."),
	)

	ctas := html.Div(html.DivConfig{Class: "hero__ctas"},
		ui.LinkButton(ui.LinkButtonConfig{Label: "Get started", Href: "/get-started", Variant: ui.ButtonPrimary, Size: ui.ButtonSizeLarge}),
		ui.LinkButton(ui.LinkButtonConfig{Label: "Read the docs", Href: "/docs/", Variant: ui.ButtonGhost, Size: ui.ButtonSizeLarge}),
	)

	install := html.Div(html.DivConfig{Class: "hero__install", ExtraAttrs: html.Attrs{"tabindex": "0"}},
		html.Span(html.TextConfig{Class: "p"}, render.Text("$")),
		render.Text(" go install github.com/DonaldMurillo/gofastr/cmd/gofastr@latest"),
	)

	copy := render.Join(preAlphaTag, title, lede1, lede2, ctas, install)

	return html.Section(html.SectionConfig{Class: "hero", Label: "Hero"},
		container(ui.HeroSplit(ui.HeroSplitConfig{
			Copy:  copy,
			Media: heroCodeTabs(),
			Ratio: ui.HeroSplitMediaWide,
			Class: "hero-home",
		})),
	)
}

// heroCodeTabs — three real, buildable examples shown as tabs: core-only
// (stdlib primitives), framework + one entity, and the fuller "Donald's Way"
// (SEO + MCP + auth + a couple of interactive pages). Every API call here is
// real — sourced from examples/blog, examples/site, and examples/meridian —
// so the homepage never teaches an API that doesn't exist.
func heroCodeTabs() render.HTML {
	coreSrc := `package main

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

type Pong struct {
	Status string ` + "`json:\"status\"`" + `
}

func main() {
	r := router.New()

	// An HTML page.
	r.Get("/", render.HTMLHandler(func(req *http.Request) render.HTML {
		return render.Tag("h1", nil, render.Text("Hello from core."))
	}))

	// A typed JSON API route.
	r.Get("/api/ping", handler.HandlerAdapter(func(ctx context.Context, _ struct{}) (Pong, error) {
		return Pong{Status: "ok"}, nil
	}))

	http.ListenAndServe(":8080", r)
}`

	frameworkSrc := `package main

import (
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, _ := sql.Open("sqlite3", "./blog.db")

	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "blog"}),
	)

	app.Entity("posts", framework.EntityConfig{
		Public: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "published"}, Default: "draft"},
		},
	})

	// Auto-migrates and serves REST + OpenAPI on :8080.
	app.Start(":8080")
}`

	donaldSrc := `package main

import (
	"database/sql"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, _ := sql.Open("sqlite3", "./notes.db")

	// Two server-rendered pages. Each gets an auto-generated llm.md.
	ui := app.NewApp("Notes")
	ui.Register("/", &HomeScreen{}, nil)
	ui.Register("/notes", &NotesScreen{}, nil)

	// SEO for those pages.
	host := uihost.New(ui,
		uihost.WithDescription("A tiny notes app."),
		uihost.WithOpenGraph(uihost.OG{Title: "Notes", Type: "website"}),
		uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://notes.example"}),
	)

	// MCP for agents.
	fwApp := framework.NewUIHostApp(host,
		framework.WithDB(db),
		framework.WithAPIPrefix("/api"),
		framework.WithMCP(),
		framework.WithMCPIntrospection(),
	)

	// One entity → a REST API at /api/notes, MCP tools, and an auto llm.md.
	// OwnerField scopes rows per user, so agents get the same access as people.
	fwApp.Entity("notes", framework.EntityConfig{
		OwnerField: "user_id",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
	})

	// Login + sessions.
	authMgr := auth.New(auth.AuthConfig{
		UserStore:    auth.NewEntityUserStore(db, "auth_users"),
		SessionStore: auth.NewEntitySessionStore(db, "auth_sessions"),
	})
	authMgr.Use(auth.NewCorePlugin())
	authMgr.Init(fwApp)
	fwApp.Use(auth.SessionMiddleware(authMgr))

	fwApp.Start(":8080")
}`

	return ui.CodeTabs(
		ui.CodeTabsConfig{Name: "hero-examples", Label: "Example apps", LineNumbers: true},
		ui.CodeSample{Label: "core only", Language: "go", Filename: "main.go", Code: coreSrc},
		ui.CodeSample{Label: "framework", Language: "go", Filename: "main.go", Code: frameworkSrc},
		ui.CodeSample{Label: "Donald's Way", Language: "go", Filename: "main.go", Code: donaldSrc},
	)
}

// -----------------------------------------------------------------------------
// §01 — Server-rendered UI. Most Go frameworks stop at the API; GoFastr renders
// the pages too, on the server, with a small JS runtime that hydrates in place
// and turns in-page changes into island calls. The screen mock shows the shape;
// the left column explains the model. Blueprint is a one-line aside, not the
// section's story.
// -----------------------------------------------------------------------------

func realAppSection() render.HTML {
	li := func(children ...render.HTML) render.HTML { return html.ListItem(html.ListItemConfig{}, children...) }

	left := html.Div(html.DivConfig{Class: "realapp__left"},
		html.Paragraph(html.TextConfig{},
			render.Text("Most Go frameworks stop at the API. GoFastr renders the pages too — on the server, in Go, with no React or Vue on the client."),
		),
		html.UnorderedList(html.ListConfig{},
			li(render.Text("Every page is full HTML on first load — fast, and readable by crawlers and agents.")),
			li(render.Text("A small JS runtime hydrates that HTML in place. No re-render, no client router to ship.")),
			li(render.Text("In-page changes — sort, paginate, add a row — are island calls: the server returns new HTML and the runtime swaps one part.")),
			li(render.Text("You write screens in Go, composed from framework/ui components.")),
		),
	)

	grid := html.Div(html.DivConfig{Class: "realapp__grid"}, left, screenMock())

	head := sectionHead(
		"Server-rendered screens, not just an API.",
		render.Text("Below is the shape of a server-rendered screen — a data table with status badges and a create button, built from framework/ui components and served as plain HTML."),
	)

	return sectionWrap("01 / server-rendered UI", "Server-rendered UI", head, grid)
}

// screenMock is a static, faithful preview of a server-rendered list screen
// (Meridian's /customers): a page header plus a server-rendered data table with
// formatted cells and status badges. It is not wired to live data — it shows
// the SHAPE of a server-rendered screen, so the homepage can demonstrate "a
// real screen" without standing up the entity + RPC a live DataTable island
// requires.
func screenMock() render.HTML {
	badge := func(label string, tone ui.StatusVariant) render.HTML {
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: label, Variant: tone})
	}
	cell := func(c render.HTML) render.HTML { return html.TD(html.TDConfig{}, c) }
	th := func(s string) render.HTML { return html.TH(html.THConfig{}, render.Text(s)) }
	row := func(name, plan, mrr, status string, tone ui.StatusVariant) render.HTML {
		return html.TableRow(html.TableRowConfig{},
			cell(render.Text(name)),
			cell(render.Text(plan)),
			cell(html.Span(html.TextConfig{Class: "mock-mrr"}, render.Text(mrr))),
			cell(badge(status, tone)),
		)
	}

	table := html.Table(html.TableConfig{Class: "mock-table"},
		html.Thead(html.TableSectionConfig{},
			html.TableRow(html.TableRowConfig{}, th("Name"), th("Plan"), th("MRR"), th("Status")),
		),
		html.Tbody(html.TableSectionConfig{},
			row("Acme Corp", "pro", "$1,240", "active", ui.StatusSuccess),
			row("Globex", "enterprise", "$8,900", "active", ui.StatusSuccess),
			row("Initech", "free", "$0", "churned", ui.StatusNeutral),
			row("Umbrella", "pro", "$2,150", "active", ui.StatusSuccess),
		),
	)

	return html.Div(html.DivConfig{Class: "screen-mock"},
		html.Div(html.DivConfig{Class: "screen-mock__bar"},
			html.Span(html.TextConfig{Class: "dot"}),
			html.Span(html.TextConfig{Class: "dot"}),
			html.Span(html.TextConfig{Class: "dot"}),
			html.Span(html.TextConfig{Class: "screen-mock__url"}, render.Text("meridian.local/customers")),
		),
		html.Div(html.DivConfig{Class: "screen-mock__body"},
			html.Div(html.DivConfig{Class: "screen-mock__head"},
				html.Heading(html.HeadingConfig{Level: 3}, render.Text("Customers")),
				html.Span(html.TextConfig{Class: "mock-new"}, render.Text("+ New customer")),
			),
			table,
		),
		html.Paragraph(html.TextConfig{Class: "screen-mock__cap"},
			render.Text("/customers — a server-rendered screen from framework/ui, plain Go you own."),
		),
	)
}

// -----------------------------------------------------------------------------
// §02 — Explore the framework. A routing grid into the main areas: core
// primitives, composed patterns, the agentic/AI surface, the interactivity
// model, the code generator, and the example apps. Each card links to a real
// route on the site.
// -----------------------------------------------------------------------------

func exploreSection() render.HTML {
	codeText := func(s string) render.HTML { return html.Code(html.TextConfig{}, render.Text(s)) }
	card := func(href, eyebrow, title string, desc render.HTML) render.HTML {
		return html.LinkHTML(html.LinkHTMLConfig{
			Href:  href,
			Class: "ex-card",
			Content: render.Join(
				html.Span(html.TextConfig{Class: "path"}, render.Text(eyebrow)),
				html.Heading(html.HeadingConfig{Level: 3}, render.Text(title)),
				html.Paragraph(html.TextConfig{}, desc),
			),
		})
	}

	grid := html.Div(html.DivConfig{Class: "ex__grid"},
		card("/primitives", "core · core-ui", "The primitives",
			render.Join(render.Text("Router, query builder, schema, "), codeText("render"), render.Text(", the MCP server, HTML primitives, and signals — stdlib-first Go you can use on their own.")),
		),
		card("/framework", "framework · framework/ui", "Framework",
			render.Text("The opinionated layer: entities and CRUD, auth, access control, migrations — plus the framework/ui components and theming."),
		),
		card("/agents", "mcp · llm.md · well-known", "Agent-ready",
			render.Join(render.Text("Per-entity MCP tools, auto "), codeText("llm.md"), render.Text(", tools that read the running app, and the agent-discovery endpoints your app serves.")),
		),
		card("/interactivity", "ssr · islands · signals", "Interactivity",
			render.Text("The server-driven model: full SSR, island RPC, optimistic UI, and signals + SSE — no client framework to ship."),
		),
		card("/generator", "generate", "The code generator",
			render.Text("Scaffold a Go app from a declaration when you want a head start. It writes plain Go you own and edit."),
		),
		card("/examples", "examples/", "The example apps",
			render.Text("Runnable reference apps — a blog, a SaaS console, an API tour, semantic search, and this site — each in one command."),
		),
	)

	head := sectionHead(
		"Explore the framework.",
		render.Text("Six ways in — pick the one that matches what you're building."),
	)

	return sectionWrap("02 / explore", "Explore the framework", head, grid)
}

// -----------------------------------------------------------------------------
// §03 — Built with GoFastr. Real apps running on the framework: a production
// tool (external) and the generated flagship. Proof it ships real software,
// not just demos. Reuses the .ex-card / .ex__grid classes from the explore grid.
// -----------------------------------------------------------------------------

func builtWithSection() render.HTML {
	card := func(href, eyebrow, title string, desc render.HTML, external bool) render.HTML {
		attrs := html.Attrs{}
		if external {
			attrs["rel"] = "external"
		}
		return html.LinkHTML(html.LinkHTMLConfig{
			Href:       href,
			Class:      "ex-card",
			ExtraAttrs: attrs,
			Content: render.Join(
				html.Span(html.TextConfig{Class: "path"}, render.Text(eyebrow)),
				html.Heading(html.HeadingConfig{Level: 3}, render.Text(title)),
				html.Paragraph(html.TextConfig{}, desc),
			),
		})
	}

	grid := html.Div(html.DivConfig{Class: "ex__grid"},
		card("https://barcode.donaldmurillo.com/", "in production", "Barcode & QR Code Maker",
			render.Text("A live, no-signup tool to generate and read barcodes and QR codes as PNG, SVG, or PDF — with CSV/Excel batch export, a REST API, and an MCP server."), true),
		card("/examples#meridian", "examples/meridian", "Meridian — SaaS console",
			render.Text("The flagship: a billing console with customers, subscriptions, invoices, MRR, and charts — plus its marketing site, auth, and admin — generated from one gofastr.yml."), false),
	)

	head := sectionHead(
		"Built with GoFastr.",
		render.Text("A real app in production, and the flagship the framework is proven against."),
	)

	return sectionWrap("03 / built with gofastr", "Built with GoFastr", head, grid)
}

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
