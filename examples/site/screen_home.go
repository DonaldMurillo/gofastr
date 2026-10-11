package main

// =============================================================================
// Home, top-level outline:
//
//   HERO        status pill · h1 · lede · CTAs · install command · agent lede
//               RHS: ui.CodeTabs with the three README Quickstart programs
//   §01         the numbers: countable claims, measured or test-gated
//   §02         server-rendered UI: the SSR/islands model beside a sample
//               list screen built from real kit components
//   §03         explore grid, 6 route cards into the main areas of the site
//   §04         built with gofastr, production app + the Meridian flagship
//
// Everything is composed from framework/ui on the stock theme: HeroSplit,
// Section, Container, Stack, Cluster, Grid, Card, StatCard, DataTable,
// CodeTabs, CodeBlock, InlineCode. The page ships no CSS and no class of
// its own; the top bar and colophon are the site's owned siteheader /
// sitefooter packages.
// =============================================================================

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"strconv"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/docs"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type HomeScreen struct{}

// ScreenTitle returns the bare page name; core-ui/app appends " — GoFastr"
// (the app name) to form the <title>, so it must NOT be repeated here.
func (s *HomeScreen) ScreenTitle() string { return "Full-stack Go that doesn't get in your way" }
func (s *HomeScreen) ScreenDescription() string {
	return "An early (v0.x) full-stack Go framework that stays out of your way. Declare your domain in Go and get server-rendered screens, a REST API, MCP tools, migrations, and a typed query builder. It stays plain Go that you and your agents can read, edit, and own."
}
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *HomeScreen) Render() render.HTML {
	// One wide container owns the page column and its rhythm under the
	// header and above the footer; the Stack owns the space between
	// sections.
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage},
		ui.Stack(ui.StackConfig{Gap: ui.Gap2XL},
			heroSection(),
			numbersSection(),
			realAppSection(),
			exploreSection(),
			builtWithSection(),
		))
}

// container is the site's max-width wrapper: ui.Container at the theme's
// wide width (the stock Layout.WideWidth).
func container(children ...render.HTML) render.HTML {
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerWide}, children...)
}

// installCommand is the hero's one-line install, pinned to this build's
// release tag (or main on a development build).
func installCommand() string {
	return "go install github.com/DonaldMurillo/gofastr/cmd/gofastr@" + siteInstallTarget()
}

// -----------------------------------------------------------------------------
// HERO: ui.HeroSplit, copy on the left, the Quickstart programs on the right.
// -----------------------------------------------------------------------------

func heroSection() render.HTML {
	// The install line is a real code block: copyable, keyboard
	// scrollable on a phone, and escaped like any other sample.
	install := ui.CodeBlock(ui.CodeBlockConfig{
		Code:     installCommand(),
		Language: "shell",
		Filename: "terminal",
		ShowCopy: true,
	})
	agents := html.Paragraph(html.TextConfig{},
		render.Text("During development, "),
		ui.InlineCode("gofastr dev"),
		render.Text(" gives Claude Code or Codex the app's routes, config, and logs over MCP. In production, user agents call the same data under the same permissions."),
	)

	// Keep the primary path before the deeper agent story: the actions
	// sit right under the lede, so on phones the CTA lands in the first
	// viewport; the install line and the agent note follow.
	return ui.Hero(ui.HeroConfig{
		Eyebrow: "early · " + versionLabel(),
		Title:   "Full-stack Go that doesn't get in the way of you or your agents.",
		Lede: render.Join(
			html.Strong(html.TextConfig{}, render.Text("GoFastr")),
			render.Text(" is a full-stack Go framework. Declare your domain in Go and get "),
			html.Strong(html.TextConfig{}, render.Text("server-rendered screens")),
			render.Text(", REST endpoints, MCP tools, migrations, and typed queries. It stays plain Go on disk that you own."),
		),
		Actions: []render.HTML{
			ui.LinkButton(ui.LinkButtonConfig{Label: "Get started", Href: "/get-started", Variant: ui.ButtonPrimary, Size: ui.ButtonSizeLarge}),
			ui.LinkButton(ui.LinkButtonConfig{Label: "Read the docs", Href: "/docs/", Variant: ui.ButtonGhost, Size: ui.ButtonSizeLarge}),
		},
		Footer:    render.Join(install, agents),
		Media:     heroCodeTabs(),
		AriaLabel: "Hero",
	})
}

// The three hero programs: core-only (stdlib primitives), framework +
// one entity, and the fuller "Donald's Way" (screens + SEO + MCP + auth).
// All three are byte-identical to the README Quickstart programs, which
// CI extracts, compiles, boots, and curls
// (cmd/gofastr/readme_quickstart_test.go), so the homepage never teaches
// an API that doesn't exist or a program that doesn't run.
// TestHeroTabsMatchReadmeQuickstart pins the identity.
var heroCoreSrc = `package main

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

	// A server-rendered page.
	r.Get("/", render.HTMLHandler(func(req *http.Request) render.HTML {
		return render.Tag("h1", nil, render.Text("Hello from core."))
	}))

	// A typed JSON route: the adapter binds input and serializes output.
	r.Get("/api/ping", handler.HandlerAdapter(func(ctx context.Context, _ struct{}) (Pong, error) {
		return Pong{Status: "ok"}, nil
	}))

	http.ListenAndServe(":8080", r)
}`

var heroFrameworkSrc = `package main

import (
	"database/sql"
	"log"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func main() {
	db, _ := sql.Open("sqlite3", "app.db")
	app := framework.NewApp(framework.WithDB(db), framework.WithMCP()) // WithMCP serves the tools at /mcp

	// CRUD is auto-on when a DB is set (CRUD *bool: nil = auto).
	app.Entity("posts", framework.EntityConfig{
		Exposure: &framework.ExposureConfig{
			Public: true, // anonymous read AND write; omit it and CRUD requires a session (secure by default)
			MCP:    true, // emit posts_list/get/create/update/delete MCP tools
		},
		Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	})

	log.Fatal(app.Start(":8080")) // GET/POST /posts, /openapi.json, MCP: all live
}`

var heroDonaldSrc = `package main

import (
	"database/sql"
	"log"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// A screen is plain Go: Render returns server-rendered HTML.
type HomeScreen struct{}

func (s *HomeScreen) ScreenTitle() string { return "Notes" }
func (s *HomeScreen) Render() render.HTML {
	return html.Heading(html.HeadingConfig{Level: 1}, render.Text("My notes"))
}

func main() {
	db, _ := sql.Open("sqlite3", "notes.db")

	// Server-rendered screens. Each also serves an auto llm.md.
	ui := app.NewApp("Notes")
	ui.Register("/", &HomeScreen{}, nil)

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
	)

	// Scope.OwnerField scopes rows per user: anonymous → 401, cross-user → 404.
	fwApp.Entity("notes", framework.EntityConfig{
		Scope:    &framework.ScopeConfig{OwnerField: "user_id"},
		Exposure: &framework.ExposureConfig{MCP: true},
		Fields:   []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	})

	// Login + sessions.
	authMgr := auth.New(auth.AuthConfig{
		DevMode:      true, // dev only: mints a per-process JWT secret; set JWTSecret in prod
		UserStore:    auth.NewEntityUserStore(db, "auth_users"),
		SessionStore: auth.NewEntitySessionStore(db, "auth_sessions"),
	})
	authMgr.Use(auth.NewCorePlugin())
	if err := authMgr.Init(fwApp); err != nil {
		log.Fatal(err)
	}
	fwApp.Use(auth.SessionMiddleware(authMgr))

	log.Fatal(fwApp.Start(":8080"))
}`

func heroCodeTabs() render.HTML {
	return ui.CodeTabs(
		ui.CodeTabsConfig{Name: "hero-examples", Label: "Example apps", LineNumbers: true},
		ui.CodeSample{Label: "core only", Language: "go", Filename: "main.go", Code: heroCoreSrc},
		ui.CodeSample{Label: "framework", Language: "go", Filename: "main.go", Code: heroFrameworkSrc},
		ui.CodeSample{Label: "full-stack app", Language: "go", Filename: "main.go", Code: heroDonaldSrc},
	)
}

// -----------------------------------------------------------------------------
// §01: The numbers. A strip of countable claims, each one either measured by
// this running binary (runtime size, doc count) or pinned by a test in the
// repo (numbers_gate_test.go). No adjectives, a skeptical reader can verify
// every value.
// -----------------------------------------------------------------------------

// measuredRuntimeGz is the gzipped size of the core client runtime,
// measured at first render from the same embedded source this site
// serves, e.g. "12.0 KB". DefaultCompression matches the size-budget
// test (core-ui/runtime/budget_test.go), so the page and the gate report
// the same number.
var runtimeGzBytes = sync.OnceValue(func() int {
	src, err := runtime.RuntimeJS()
	if err != nil {
		return 0
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		return 0
	}
	if _, err := zw.Write([]byte(src)); err != nil {
		return 0
	}
	if err := zw.Close(); err != nil {
		return 0
	}
	return buf.Len()
})

// The byte count is kept separate from its rendering because the two levels
// this could be measured at differ by only 30 bytes, and both round to
// "12.0 KB", so a parity test written against the STRING cannot tell them
// apart. numbers_gate_test.go compares runtimeGzBytes directly.
var measuredRuntimeGz = sync.OnceValue(func() string {
	n := runtimeGzBytes()
	if n == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1024)
})

// embeddedDocCount counts the docs corpus this binary carries, minus the
// folder's own README (not a page). Same source as /docs and /llms.txt.
var embeddedDocCount = sync.OnceValue(func() string {
	topics, err := docs.List()
	if err != nil {
		return "n/a"
	}
	n := 0
	for _, t := range topics {
		if t.Name != "README" {
			n++
		}
	}
	return strconv.Itoa(n)
})

// numberClaim is one countable claim: the value, what it counts, and how a
// reader can check it.
type numberClaim struct{ value, label, check string }

func numberClaims() []numberClaim {
	return []numberClaim{
		{measuredRuntimeGz(), "of client JavaScript, gzipped",
			"The core runtime, measured from this running binary. Feature modules load on demand; a size-budget test fails the build if any of them grows."},
		{embeddedDocCount(), "docs embedded in every binary",
			"gofastr docs reads them offline. This site serves the same files under /docs and /llms.txt; agents query them over MCP."},
		{"5", "MCP tools per entity",
			"list, get, create, update, delete. Each dispatches through the app router, so the caller's login and permissions apply."},
		{"2", "databases",
			"SQLite and Postgres. No MySQL, no Mongo."},
		{"1", "binary to deploy",
			"go build emits it. No Node, no platform, no telemetry."},
		{"0", "npm packages",
			"There is no package.json in the repo. The client runtime is checked-in JS the binary serves."},
	}
}

// numbersSection: the six claims as three ui.MetricBands of two, each
// signal a label, its value, and the sentence that says how to check it.
// Pairs keep every band even: one row on a desktop, two columns on a
// phone, no odd signal spanning a row.
func numbersSection() render.HTML {
	claims := numberClaims()
	band := func(label string, cs []numberClaim) render.HTML {
		items := make([]ui.MetricBandItem, 0, len(cs))
		for _, c := range cs {
			items = append(items, ui.MetricBandItem{Label: c.label, Value: c.value, Hint: c.check})
		}
		return ui.MetricBand(ui.MetricBandConfig{Label: label, Items: items})
	}
	bands := ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		band("Measured by this binary", claims[:2]),
		band("Agents and databases", claims[2:4]),
		band("Deploy and dependencies", claims[4:]),
	)

	head := sectionHead(
		"Numbers you can check.",
		render.Text("Each value is measured by this running binary or enforced by a test in the repo. Nothing here is an adjective."),
	)

	return sectionWrap("01 / the numbers", "The numbers", head, bands)
}

// -----------------------------------------------------------------------------
// §02: Server-rendered UI. Most Go frameworks stop at the API; GoFastr renders
// the pages too, on the server, with a small JS runtime that hydrates in place
// and turns in-page changes into island calls. The sample screen shows the
// shape; the left column explains the model.
// -----------------------------------------------------------------------------

func realAppSection() render.HTML {
	li := func(children ...render.HTML) render.HTML { return html.ListItem(html.ListItemConfig{}, children...) }

	left := ui.Stack(ui.StackConfig{Gap: ui.GapMD},
		html.Paragraph(html.TextConfig{},
			render.Text("Most Go frameworks stop at the API. GoFastr renders the pages too: on the server, in Go, with no React or Vue on the client."),
		),
		html.UnorderedList(html.ListConfig{},
			li(render.Text("Every page is full HTML on first load: fast and readable by crawlers and agents.")),
			li(render.Text("A small JS runtime hydrates that HTML in place with no re-render. Cross-page nav swaps content client-side with a route cache; you never write the router.")),
			li(render.Text("In-page changes such as sort, paginate, or add a row are island calls. The server returns new HTML and the runtime swaps one part.")),
			li(render.Text("You write screens in Go, composed from framework/ui components.")),
		),
	)

	head := sectionHead(
		"Server-rendered screens, not just an API.",
		render.Text("Below is the shape of a server-rendered screen: a data table with status badges and a create button, built from framework/ui components and served as plain HTML."),
	)

	return sectionWrap("02 / server-rendered UI", "Server-rendered UI", head,
		ui.Grid(ui.GridConfig{Min: "24rem", Gap: ui.GapXL}, left, screenMock()))
}

// screenMock is a sample of a server-rendered list screen (Meridian's
// /customers), composed from the same kit components the real screen
// uses: a Card framing a static DataTable with formatted cells and
// status badges. It is not wired to live data, so the homepage shows
// the SHAPE of a real screen without standing up the entity + RPC a
// live DataTable island requires.
func screenMock() render.HTML {
	badge := func(label string, tone ui.StatusVariant) render.HTML {
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: label, Variant: tone})
	}
	row := func(name, plan, mrr, status string, tone ui.StatusVariant) ui.Row {
		return ui.Row{Cells: map[string]render.HTML{
			"name":   render.Text(name),
			"plan":   render.Text(plan),
			"mrr":    render.Text(mrr),
			"status": badge(status, tone),
		}}
	}

	table := ui.DataTable(ui.DataTableConfig{
		Caption:       "Customers",
		CaptionHidden: true,
		Flush:         true,
		Responsive:    ui.ResponsiveRows,
		Columns: []ui.Column{
			{Key: "name", Header: "Name", Phone: ui.PhoneTitle},
			{Key: "plan", Header: "Plan", Phone: ui.PhoneSubtitle},
			{Key: "mrr", Header: "MRR", Align: "end", Phone: ui.PhoneDetail},
			{Key: "status", Header: "Status", Phone: ui.PhoneEnd},
		},
		Rows: []ui.Row{
			row("Acme Corp", "pro", "$1,240", "active", ui.StatusSuccess),
			row("Globex", "enterprise", "$8,900", "active", ui.StatusSuccess),
			row("Initech", "free", "$0", "churned", ui.StatusNeutral),
			row("Umbrella", "pro", "$2,150", "active", ui.StatusSuccess),
		},
	})

	return ui.Card(ui.CardConfig{
		Heading:     "Customers",
		Description: "meridian.local/customers",
		Action:      ui.Button(ui.ButtonConfig{Label: "New customer", Variant: ui.ButtonPrimary, Size: ui.ButtonSizeSmall, Disabled: true}),
		Footer:      ui.Muted(render.Text("/customers: a server-rendered screen from framework/ui, in plain Go you own.")),
	}, table)
}

// -----------------------------------------------------------------------------
// §03: Explore the framework. A routing grid into the main areas: core
// primitives, composed patterns, the agentic/AI surface, the interactivity
// model, the code generator, and the example apps. Each card links to a real
// route on the site.
// -----------------------------------------------------------------------------

// routeCard is one linked card: the whole outlined Card is the link, its
// heading the destination, the body what is there, and the footer the
// packages or path it covers.
func routeCard(href, eyebrow, title string, desc render.HTML, external bool) render.HTML {
	var attrs html.Attrs
	if external {
		attrs = html.Attrs{"rel": "external"}
	}
	return ui.Card(ui.CardConfig{
		Href:       href,
		Heading:    title,
		Variant:    ui.CardOutlined,
		Footer:     ui.Muted(render.Text(eyebrow)),
		ExtraAttrs: attrs,
	}, ui.Stack(ui.StackConfig{TrimMargins: true}, html.Paragraph(html.TextConfig{}, desc)))
}

func exploreSection() render.HTML {
	code := ui.InlineCode
	grid := ui.Grid(ui.GridConfig{Min: "20rem", Gap: ui.GapLG},
		routeCard("/primitives", "core · core-ui", "The primitives",
			render.Join(render.Text("Router, query builder, schema, "), code("render"), render.Text(", the MCP server, HTML primitives, and signals. These are stdlib-first Go packages you can use on their own.")), false,
		),
		routeCard("/framework", "framework · framework/ui", "Framework",
			render.Text("The opinionated layer: entities and CRUD, auth, access control, migrations, framework/ui components, and theming."), false,
		),
		routeCard("/agents", "mcp · llm.md · well-known", "Agent-ready",
			render.Join(render.Text("Per-entity MCP tools, auto "), code("llm.md"), render.Text(", tools that read the running app, and the agent-discovery endpoints your app serves.")), false,
		),
		routeCard("/interactivity", "ssr · islands · signals", "Interactivity",
			render.Text("The server-driven model: full SSR, island RPC, optimistic UI, and signals + SSE. There is no client framework to ship."), false,
		),
		routeCard("/generator", "generate", "The code generator",
			render.Text("Scaffold a Go app from a declaration when you want a head start. It writes plain Go you own and edit."), false,
		),
		routeCard("/examples", "examples/", "The example apps",
			render.Text(fmt.Sprintf("%d runnable reference apps, including a blog, a SaaS console, a storefront, an API tour, an entity admin, a process-isolated module, a WebMCP support console, and this site. Each starts with one command.", len(exRowItems()))), false,
		),
	)

	head := sectionHead(
		"Explore the framework.",
		render.Text("Six ways in. Pick the one that matches what you're building."),
	)

	return sectionWrap("03 / explore", "Explore the framework", head, grid)
}

// -----------------------------------------------------------------------------
// §04: Built with GoFastr. Real apps running on the framework: a production
// tool (external) and the generated flagship. Proof it ships real software,
// not just demos. Same route cards as the explore grid.
// -----------------------------------------------------------------------------

func builtWithSection() render.HTML {
	grid := ui.Grid(ui.GridConfig{Min: "18rem", Gap: ui.GapLG},
		routeCard("https://barcode.donaldmurillo.com/", "in production", "Barcode & QR Code Maker",
			render.Text("A live tool, no signup required, to generate and read barcodes and QR codes as PNG, SVG, or PDF, with CSV/Excel batch export, a REST API, and an MCP server."), true),
		routeCard("/examples#meridian", "examples/meridian", "Meridian: SaaS console",
			render.Text("The flagship is a billing console with customers, subscriptions, invoices, MRR, and charts, plus its marketing site, auth, and admin. It was seeded from one gofastr.yml and has been hand-evolved since."), false),
	)

	head := sectionHead(
		"Built with GoFastr.",
		render.Text("A real app in production, and the flagship the framework is proven against."),
	)

	return sectionWrap("04 / built with gofastr", "Built with GoFastr", head, grid)
}

// -----------------------------------------------------------------------------
// Shared section helpers.
// -----------------------------------------------------------------------------

// sectionHead is a section's h2 over its lede, stacked with the kit's
// small gap. Base typography styles both; the lede reads muted.
func sectionHead(title string, lede render.HTML) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapSM, TrimMargins: true},
		html.Heading(html.HeadingConfig{Level: 2}, render.Text(title)),
		html.Paragraph(html.TextConfig{}, ui.Muted(lede)),
	)
}

// sectionWrap is a compact ui.Section: it owns the <section> landmark
// (named by ariaLabel), the decorative numeric eyebrow, and the scroll
// margin that keeps an anchored section clear of the sticky header. The
// caller's page container and Stack own the column and the spacing
// between sections.
func sectionWrap(num, ariaLabel string, head, body render.HTML) render.HTML {
	return ui.Section(ui.SectionConfig{
		Eyebrow: num,
		Label:   ariaLabel,
		Compact: true,
	}, head, body)
}
