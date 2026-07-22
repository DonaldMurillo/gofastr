package main

// Area hubs — one taught landing page per top-level area from the homepage
// explore grid (Primitives, Framework, Agents, Interactivity, Generator). Each
// hub reads top to bottom: a short intro with the area's pieces named up front,
// then one "concept → show it → reference link" block per piece, then where to
// go next. Examples keeps its own screen (/examples).
//
// The teaching machinery (TeachHubScreen, hubConcept, renderHubConcept) is
// shared; each area supplies its own concepts. Every code sample is verified
// real API — the site's standing rule is that no snippet teaches an API that
// doesn't exist.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// registerHubs registers every area hub on the site. Each is a taught page.
func registerHubs(site *app.App) {
	site.Register("/primitives", primitivesHub(), nil)
	site.Register("/framework", frameworkHub(), nil)
	site.Register("/agents", agentsHub(), nil)
	site.Register("/interactivity", interactivityHub(), nil)
	site.Register("/generator", generatorHub(), nil)
}

// -----------------------------------------------------------------------------
// Teaching hub — a taught area page. It reads top to bottom: a short intro with
// the area's pieces named up front, then one "concept → show it → reference
// link" block per piece, then where to go next. This is the exemplar for
// /interactivity; the same shape repeats for every area, only the pieces change.
// -----------------------------------------------------------------------------

// hubConcept is one taught idea: a name, a plain explanation, an optional real
// code sample, and the one reference that owns the full detail.
type hubConcept struct {
	Title    string
	Body     []render.HTML
	Code     string // raw source; auto-highlighted. Empty = no code block.
	CodeLang string
	CodeFile string
	RefSlug  string // links to /docs/<RefSlug> unless RefHref is set
	RefHref  string // external reference URL (e.g. pkg.go.dev); overrides RefSlug
	RefText  string // visible link label; defaults to the target path + " →"
}

// refLink resolves a concept's reference href and label.
func (c hubConcept) refLink() (href, label string) {
	href = c.RefHref
	if href == "" {
		href = "/docs/" + c.RefSlug
	}
	label = c.RefText
	if label == "" {
		label = href + " →"
	}
	return href, label
}

// hubNextLink is a card at the bottom of a hub: where to go once the area clicks.
type hubNextLink struct {
	Title string
	Href  string
	Hint  string
}

// TeachHubScreen renders a taught area page from its concepts.
type TeachHubScreen struct {
	Name     string // area name — the eyebrow pill and the page <title>
	Title    string // the h1
	Desc     string // meta description
	Lede     render.HTML
	Moves    []string // the pieces of this area, named up front
	Concepts []hubConcept
	Next     []hubNextLink
}

func (h *TeachHubScreen) ScreenTitle() string        { return h.Name }
func (h *TeachHubScreen) ScreenDescription() string  { return h.Desc }
func (h *TeachHubScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (h *TeachHubScreen) Render() render.HTML {
	movePills := make([]render.HTML, 0, len(h.Moves))
	for _, m := range h.Moves {
		movePills = append(movePills, ui.StatusPill(ui.StatusPillConfig{Label: m, Tone: ui.StatusPillAccent}))
	}
	hero := html.Section(html.SectionConfig{Class: "ex-hero", Label: h.Name},
		container(
			html.Div(html.DivConfig{Class: "mb-lg"}, tagAccent(h.Name)),
			html.Heading(html.HeadingConfig{Level: 1}, render.Text(h.Title)),
			html.Paragraph(html.TextConfig{Class: "lede"}, h.Lede),
			ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM}, movePills...),
		),
	)

	blocks := make([]render.HTML, 0, len(h.Concepts))
	for _, c := range h.Concepts {
		blocks = append(blocks, renderHubConcept(c))
	}
	concepts := html.Section(html.SectionConfig{Class: "section-v2", Label: "Concepts"},
		container(ui.Stack(ui.StackConfig{Gap: ui.GapXL}, blocks...)),
	)

	return render.Join(hero, concepts, hubNext(h.Next))
}

// renderHubConcept renders one concept block: heading, prose, an optional real
// code sample, then a single link to the one reference doc that owns it.
func renderHubConcept(c hubConcept) render.HTML {
	kids := []render.HTML{html.Heading(html.HeadingConfig{Level: 2}, render.Text(c.Title))}
	kids = append(kids, c.Body...)
	if c.Code != "" {
		kids = append(kids, ui.CodeBlock(ui.CodeBlockConfig{
			Lines:    ui.HighlightLines(c.Code, c.CodeLang),
			Language: c.CodeLang,
			Filename: c.CodeFile,
			ShowCopy: true,
		}))
	}
	href, label := c.refLink()
	kids = append(kids, ui.LinkButton(ui.LinkButtonConfig{
		Label:   label,
		Href:    href,
		Variant: ui.ButtonGhost,
	}))
	return ui.Stack(ui.StackConfig{Gap: ui.GapSM}, kids...)
}

// hubNext renders the "where next" card row at the bottom of a taught hub.
func hubNext(links []hubNextLink) render.HTML {
	cards := make([]render.HTML, 0, len(links))
	for _, l := range links {
		cards = append(cards, html.LinkHTML(html.LinkHTMLConfig{
			Href:  l.Href,
			Class: "ex-card",
			Content: render.Join(
				html.Heading(html.HeadingConfig{Level: 3}, render.Text(l.Title)),
				html.Paragraph(html.TextConfig{}, render.Text(l.Hint)),
			),
		}))
	}
	return html.Section(html.SectionConfig{Class: "next", Label: "Where next"},
		container(
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Where next")),
			html.Div(html.DivConfig{Class: "next__grid"}, cards...),
		),
	)
}

// interactivityHub is the taught /interactivity page. Code samples are filled
// from verified real API usage; prose stands on its own without them.
func interactivityHub() *TeachHubScreen {
	p := func(s string) render.HTML { return html.Paragraph(html.TextConfig{}, render.Text(s)) }
	return &TeachHubScreen{
		Name:  "Interactivity",
		Title: "The server drives the UI",
		Desc:  "The server-driven UI model in GoFastr: islands, signals, server push over SSE, server-validated forms, and optimistic actions — no client framework to ship.",
		Lede:  render.Text("You write Go. The browser gets HTML. There's no client framework to ship. Almost everything you'll do is one of five moves:"),
		Moves: []string{"islands", "signals", "push (SSE)", "forms", "optimistic actions"},
		Concepts: []hubConcept{
			{
				Title:    "Islands",
				Body:     []render.HTML{p("A click changes one part of the page. The click calls the server, the server returns new HTML for that part, and the runtime swaps it in. Use it for sort, paginate, expand, add a row — anything that isn't a whole new page.")},
				CodeFile: "customers.go",
				CodeLang: "go",
				Code: `// Sort headers fire an RPC instead of navigating. The handler
// returns the new table HTML; the runtime swaps this island in place.
ui.DataTable(ui.DataTableConfig{
    Rows:           rows,
    IslandSignal:   "customers",
    IslandEndpoint: "/customers/table",
})`,
				RefSlug: "interactive-patterns",
			},
			{
				Title:    "Signals",
				Body:     []render.HTML{p("Typed client state you declare once. Many parts of the page read the same value; change it in one place and every reader updates. Use it for a cart count, a selected row, a filter shared across widgets.")},
				CodeFile: "signals.go",
				CodeLang: "go",
				Code: `// One declaration — namespaced, typed, seeded into the client store.
company := store.New("app").String("company", "Acme Corp")

// Any number of readers; change it once and every binding updates.
company.Bind(ctx, "span", map[string]string{"id": "co-name"})`,
				RefSlug: "signal-store",
			},
			{
				Title:    "Push (SSE)",
				Body:     []render.HTML{p("For background events, not user clicks. The server sends an update over a Server-Sent Events stream and connected pages react. Use it for a live who's-here roster, a job that finished, a number that changed somewhere else.")},
				CodeFile: "presence.go",
				CodeLang: "go",
				Code: `// On a background change, re-render and push to each open session.
host.Islands.OnPresenceChange = func(topic string) {
    roster := renderRoster(host.Islands.PresenceRoster(topic))
    for _, sid := range host.Islands.PresenceSessions(topic) {
        host.Islands.PushUpdate(island.IslandUpdate{
            IslandID: "roster-" + topic,
            HTML:     string(roster),
        }, sid)
    }
}`,
				RefSlug: "runtime-contract",
			},
			{
				Title:    "Forms",
				Body:     []render.HTML{p("The server validates. On error it returns the form with the messages already in place, swapped like any island — no full reload, and no client-side validation to keep in sync with the server's.")},
				CodeFile: "login.go",
				CodeLang: "go",
				Code: `// errs is the server's validation result (ui.FieldErrors). On error
// the form comes back with each message in place, swapped like an island.
ui.Form(ui.FormConfig{Method: "POST", Action: "/login", SubmitLabel: "Sign in", Errors: errs},
    ui.FormFieldFor(errs, "email", ui.FormFieldConfig{Label: "Email", For: "email", Input: emailInput}),
)`,
				RefSlug: "form-module",
			},
			{
				Title:    "Optimistic actions",
				Body:     []render.HTML{p("The button updates before the server answers. The runtime fires the request, shows the success label right away, and rolls back if the server returns an error. Use it for approve, archive, like — actions that almost always succeed.")},
				CodeFile: "actions.go",
				CodeLang: "go",
				Code: `ui.OptimisticAction(ui.OptimisticActionConfig{
    Endpoint:     "/posts/42/archive",
    IdleLabel:    "Archive",
    SuccessLabel: "Archived",
    Variant:      ui.ButtonPrimary,
})`,
				RefSlug: "interactive-patterns",
			},
		},
		Next: []hubNextLink{
			{Title: "Build a real app", Href: "/get-started", Hint: "The four-minute guide, from install to a running page."},
			{Title: "Next area: Framework", Href: "/framework", Hint: "Entities, auth, migrations, and the framework/ui components."},
		},
	}
}

// hubP is the plain-paragraph helper shared by the taught hubs.
func hubP(s string) render.HTML { return html.Paragraph(html.TextConfig{}, render.Text(s)) }

// primitivesHub is the taught /primitives page — core + core-ui, the
// stdlib-first building blocks. Snippets verified against the real packages.
func primitivesHub() *TeachHubScreen {
	pkg := "https://pkg.go.dev/github.com/DonaldMurillo/gofastr/"
	return &TeachHubScreen{
		Name:  "Primitives",
		Title: "Stdlib-first building blocks",
		Desc:  "The core layer of GoFastr: router, typed handlers, render, schema, the MCP server, and a client store — plain stdlib-first Go you can use on their own.",
		Lede:  render.Text("core and core-ui are small Go packages, each usable on its own — no framework required. When you want more, the framework composes them for you."),
		Moves: []string{"router", "typed handlers", "render", "schema", "MCP server", "client store"},
		Concepts: []hubConcept{
			{
				Title:    "Router",
				Body:     []render.HTML{hubP("Built on net/http. Make a router and register routes with path patterns — the same shapes as the standard library.")},
				CodeFile: "main.go",
				CodeLang: "go",
				Code: `r := router.New()
r.Get("/", render.HTMLHandler(func(req *http.Request) render.HTML {
    return render.Tag("h1", nil, render.Text("Hello from core."))
}))
http.ListenAndServe(":8080", r)`,
				RefHref: pkg + "core/router",
				RefText: "pkg.go.dev · core/router →",
			},
			{
				Title:    "Typed handlers",
				Body:     []render.HTML{hubP("A handler that takes a typed input struct and returns a typed output struct. HandlerAdapter does the JSON binding, panic recovery, and response encoding for you.")},
				CodeFile: "main.go",
				CodeLang: "go",
				Code: `type Pong struct{ Status string }

r.Get("/api/ping", handler.HandlerAdapter(
    func(ctx context.Context, _ struct{}) (Pong, error) {
        return Pong{Status: "ok"}, nil
    }))`,
				RefHref: pkg + "core/handler",
				RefText: "pkg.go.dev · core/handler →",
			},
			{
				Title:    "Render HTML from Go",
				Body:     []render.HTML{hubP("Return HTML from a Go function. The html primitives map one-to-one to tags, and render.Text escapes text for you — no template language.")},
				CodeFile: "screen.go",
				CodeLang: "go",
				Code: `render.HTMLHandler(func(req *http.Request) render.HTML {
    return html.Div(html.DivConfig{Class: "card"},
        html.Heading(html.HeadingConfig{Level: 2}, render.Text("Every doc · A–Z")),
    )
})`,
				RefSlug: "ui-getting-started",
			},
			{
				Title:    "Schema",
				Body:     []render.HTML{hubP("Describe a field once — name, type, required, unique, enum values, default. The same []schema.Field drives the table, the validation, and the API.")},
				CodeFile: "schema.go",
				CodeLang: "go",
				Code: `Fields: []schema.Field{
    {Name: "title", Type: schema.String, Required: true},
    {Name: "status", Type: schema.Enum,
        Values: []string{"draft", "published"}, Default: "draft"},
}`,
				RefSlug: "entity-declarations",
			},
			{
				Title:    "MCP server",
				Body:     []render.HTML{hubP("The MCP server is a core package too — not something only entities produce. Register a tool with a name, a JSON schema, and a Go function.")},
				CodeFile: "mcp.go",
				CodeLang: "go",
				Code: `s := mcp.NewServer()
s.RegisterTool("greet", "Say hello", map[string]any{
    "type":       "object",
    "properties": map[string]any{"name": map[string]any{"type": "string"}},
}, func(ctx context.Context, p map[string]any) (any, error) {
    return "hello " + p["name"].(string), nil
})`,
				RefHref: pkg + "core/mcp",
				RefText: "pkg.go.dev · core/mcp →",
			},
			{
				Title:    "Client store",
				Body:     []render.HTML{hubP("Typed client state, declared in Go and namespaced so two features don't collide. This is the same store the interactivity signals read from.")},
				CodeFile: "store.go",
				CodeLang: "go",
				Code: `company := store.New("org").String("company", "Acme Corp")

cart := store.New("cart")
count := cart.Int("count", 0)`,
				RefSlug: "signal-store",
			},
		},
		Next: []hubNextLink{
			{Title: "Next area: Framework", Href: "/framework", Hint: "The opinionated layer built on these primitives."},
			{Title: "Next area: Interactivity", Href: "/interactivity", Hint: "How the client store powers server-driven UI."},
		},
	}
}

// frameworkHub is the taught /framework page — framework + framework/ui, the
// opinionated layer on top of core. Code samples are verified real API.
func frameworkHub() *TeachHubScreen {
	return &TeachHubScreen{
		Name:  "Framework",
		Title: "The framework layer, on top of core",
		Desc:  "The framework layer of GoFastr: entities and CRUD on the backend, composed components on the front — auth, access control, migrations, and theming.",
		Lede:  render.Text("framework and framework/ui sit on top of the primitives. Declare an entity and get the database, API, and tools; compose screens from components that already match your theme."),
		Moves: []string{"entities", "auth", "access control", "migrations", "components", "theming"},
		Concepts: []hubConcept{
			{
				Title:    "Entities",
				Body:     []render.HTML{hubP("Declare an entity in Go — fields, types, relations. One call gives you the table, REST endpoints, validation, an OpenAPI entry, and MCP tools.")},
				CodeFile: "main.go",
				CodeLang: "go",
				Code: `app.Entity("posts", framework.EntityConfig{
    Public: true,
    Fields: []schema.Field{
        {Name: "title", Type: schema.String, Required: true},
        {Name: "body", Type: schema.Text},
        {Name: "status", Type: schema.Enum,
            Values: []string{"draft", "published"}, Default: "draft"},
    },
})`,
				RefSlug: "entity-declarations",
			},
			{
				Title:    "Auth",
				Body:     []render.HTML{hubP("Login, sessions, OAuth, magic-link, 2FA, password reset. Each is a plugin you add to an auth manager; the middleware puts the signed-in user on the request.")},
				CodeFile: "auth.go",
				CodeLang: "go",
				Code: `authMgr := auth.New(auth.AuthConfig{
    DevMode:      true, // dev only: mints a per-process JWT secret; set JWTSecret in prod
    UserStore:    auth.NewEntityUserStore(db, "auth_users"),
    SessionStore: auth.NewEntitySessionStore(db, "auth_sessions"),
})
authMgr.Use(auth.NewCorePlugin())
if err := authMgr.Init(fwApp); err != nil {
    log.Fatal(err) // without a JWT secret (or DevMode) Init fails closed
}
fwApp.Use(auth.SessionMiddleware(authMgr))`,
				RefSlug: "auth",
			},
			{
				Title:    "Access control",
				Body:     []render.HTML{hubP("Roles and permissions, plus per-user owner scoping. It fails closed: a request with no matching policy is denied, not allowed.")},
				CodeFile: "entities.go",
				CodeLang: "go",
				Code: `app.Entity("notes", framework.EntityConfig{
    OwnerField: "user_id", // every read and write is scoped to the signed-in user
    Access: framework.AccessControl{
        Read: "notes:read", Create: "notes:write",
        Update: "notes:write", Delete: "notes:admin",
    },
})`,
				RefSlug: "access-control",
			},
			{
				Title:    "Migrations",
				Body:     []render.HTML{hubP("Versioned, ordered, reversible schema changes. In dev the framework can auto-migrate from the declared entities; in production you run the ordered migrations.")},
				CodeFile: "main.go",
				CodeLang: "go",
				Code: `// Derive the schema from the declared entities and apply it.
if err := framework.AutoMigrate(db, app.Registry); err != nil {
    log.Fatal(err)
}`,
				RefSlug: "migrations",
			},
			{
				Title:    "Components",
				Body:     []render.HTML{hubP("framework/ui composes intent, not tags: PageHeader, DataTable, Form, Card, charts. Each ships its own CSS through the theme, so a page inherits your look for free.")},
				CodeFile: "screen.go",
				CodeLang: "go",
				Code: `ui.PageHeader(ui.PageHeaderConfig{
    Eyebrow:  "Settings",
    Title:    "Workspace settings",
    Subtitle: "Tune defaults for everyone on this workspace.",
    Actions:  ui.Button(ui.ButtonConfig{Label: "Save changes", Variant: ui.ButtonPrimary}),
})`,
				RefHref: "/components",
				RefText: "/components — the live gallery →",
			},
			{
				Title:    "Theming",
				Body:     []render.HTML{hubP("One typed theme drives every color, space, and font as CSS variables. Change a token and every component updates — you never edit a component's CSS to reskin an app.")},
				CodeFile: "theme.go",
				CodeLang: "go",
				Code: `t := theme.Default(theme.Overrides{
    Primary:  "oklch(0.82 0.155 78)", // amber accent
    Surface:  "oklch(0.17 0.006 75)",
    RadiusMd: 6,
})
site.WithTheme(t)`,
				RefSlug: "theming",
			},
		},
		Next: []hubNextLink{
			{Title: "Next area: Agents", Href: "/agents", Hint: "The MCP tools an entity gets, and the discovery your app serves."},
			{Title: "Next area: Interactivity", Href: "/interactivity", Hint: "Server-driven UI: islands, signals, forms."},
		},
	}
}

// agentsHub is the taught /agents page — the two MCP layers. Code samples are
// filled from verified real API.
func agentsHub() *TeachHubScreen {
	codeText := func(s string) render.HTML { return html.Code(html.TextConfig{}, render.Text(s)) }
	return &TeachHubScreen{
		Name:  "Agents",
		Title: "Built for two kinds of agents",
		Desc:  "GoFastr's agent surface: production MCP tools your users' agents call with their own login, and dev MCP that hands your coding agent the running app's routes, config, and logs.",
		Lede:  render.Text("Two kinds of agents, two layers of MCP. In production, the agents your users bring call your data with the same login and permissions the users have. While you build, gofastr dev hands your coding agent the running app."),
		Moves: []string{"production MCP", "dev MCP", "auto llm.md", "discovery", "local search"},
		Concepts: []hubConcept{
			{
				Title:    "Production MCP tools",
				Body:     []render.HTML{hubP("Turn on MCP and every CRUD entity gets tools an agent can call. The tools run through the same auth, owner, and tenant checks as your HTTP API — an agent gets exactly the access its user has, and nothing more.")},
				CodeFile: "main.go",
				CodeLang: "go",
				Code: `fwApp := framework.NewUIHostApp(host,
    framework.WithConfig(framework.AppConfig{Name: "app"}),
    framework.WithMCP(),              // entities get MCP tools at /mcp
    framework.WithMCPIntrospection(),
)`,
				RefSlug: "agent-ready",
			},
			{
				Title: "Dev MCP",
				Body: []render.HTML{html.Paragraph(html.TextConfig{},
					codeText("gofastr dev"),
					render.Text(" hands your coding agent — Claude Code, Codex — the running app over MCP: it reads routes, config, readiness, embedded docs, and recent logs, and it can write app data through the same entity tools your API serves. It's livereload for agents; opt out with "),
					codeText("GOFASTR_DEV_MCP=0"),
					render.Text(".")),
				},
				CodeLang: "shell",
				Code: `# GOFASTR_DEV=1 mounts the dev MCP tools for your coding agent:
#   app_routes, app_config, app_readiness, framework_docs_*, log_recent
#   + every CRUD entity's data tools (posts_list, posts_create, …)
gofastr dev`,
				RefSlug: "dev-livereload",
			},
			{
				Title: "Auto llm.md",
				Body: []render.HTML{html.Paragraph(html.TextConfig{},
					render.Text("Every screen and every entity ships an "),
					codeText("llm.md"),
					render.Text(" automatically — a plain-text description an agent reads to understand the page or the API. On by default; opt one out with "),
					codeText("NoLLMMD"),
					render.Text(".")),
				},
				RefSlug: "agent-ready",
			},
			{
				Title:    "Discovery endpoints",
				Body:     []render.HTML{hubP("Turn on the agent-ready host options and your app serves the files agent scanners look for — an llms.txt and the .well-known endpoints — so an agent can find what your app does.")},
				CodeLang: "",
				Code: `GET /llms.txt
GET /.well-known/agent-card.json
GET /.well-known/agent.json`,
				RefSlug: "agent-ready",
			},
			{
				Title:    "Local semantic search",
				Body:     []render.HTML{hubP("battery/embed indexes a corpus locally and answers similarity queries — no external API key, works offline.")},
				CodeFile: "search.go",
				CodeLang: "go",
				Code: `idx, _ := embed.Open(embed.Options{
    Embedder: embed.NewStubEmbedder(128),
    Keyword:  embed.NewMemoryKeyword(),
})
idx.Add(ctx, docs...)
hits, _ := idx.Query(ctx, embed.Query{Text: "how do hooks work", K: 5, Hybrid: true})`,
				RefSlug: "embed",
			},
		},
		Next: []hubNextLink{
			{Title: "Next area: Primitives", Href: "/primitives", Hint: "The core/mcp package the tools are built on."},
			{Title: "Build a real app", Href: "/get-started", Hint: "The four-minute guide, from install to a running page."},
		},
	}
}

// generatorHub is the taught /generator page — scaffold plain Go from a
// declaration. Commands verified against cmd/gofastr.
func generatorHub() *TeachHubScreen {
	codeText := func(s string) render.HTML { return html.Code(html.TextConfig{}, render.Text(s)) }
	return &TeachHubScreen{
		Name:  "Generator",
		Title: "Scaffold plain Go from a declaration",
		Desc:  "GoFastr's code generators: app code from a blueprint, client SDKs, and a customer CLI. Each writes plain Go you own and edit — not a runtime you configure.",
		Lede:  render.Text("When you want a head start, generate the code. It writes plain Go to disk — you own it, read it, and edit it. One shot: nothing to keep regenerating against."),
		Moves: []string{"code generation", "SDKs", "customer CLI", "blueprints"},
		Concepts: []hubConcept{
			{
				Title:    "Code generation",
				Body:     []render.HTML{hubP("gofastr generate turns a declaration into Go on disk. Config-driven generators write under gen/; every run is reproducible, and --dry-run shows you the plan first.")},
				CodeLang: "shell",
				Code: `# preview without writing
gofastr generate --config gofastr.codegen.yml --dry-run --json

# write the generated Go
gofastr generate --config gofastr.codegen.yml`,
				RefSlug: "codegen",
			},
			{
				Title:    "SDKs",
				Body:     []render.HTML{hubP("Generate a client for your API: a standalone Go module and a JS/TS ESM client. The app can host them behind a live docs page for your customers.")},
				CodeLang: "shell",
				Code:     `gofastr generate sdk --target=go,js --out=gen/sdk`,
				RefSlug:  "sdk",
			},
			{
				Title:    "Customer CLI",
				Body:     []render.HTML{hubP("Generate a branded terminal client for your API, with scoped API-token auth. Your customers get a real CLI; you own the generated code under cmd/.")},
				CodeLang: "shell",
				Code: `gofastr generate cli --binary=meridian --only=customers,invoices

# customers sign in with a scoped token, read from stdin
echo "$TOKEN" | ./meridian login --url https://app.example.com --with-token`,
				RefSlug: "app-cli",
			},
			{
				Title: "Blueprints",
				Body: []render.HTML{html.Paragraph(html.TextConfig{},
					render.Text("A "), codeText("gofastr.yml"), render.Text(" is a bundle of entities and screens. "),
					codeText("gofastr generate --from=gofastr.yml"),
					render.Text(" writes the app as plain package main to your module root — a one-shot scaffold you then own, not a tree you keep regenerating.")),
				},
				CodeFile: "gofastr.yml",
				CodeLang: "yaml",
				Code: `entities:
  - name: plans
    crud: true
    fields:
      - name: name
        type: string
        required: true
      - name: slug
        type: string
        unique: true`,
				RefSlug: "blueprints",
			},
		},
		Next: []hubNextLink{
			{Title: "Next area: Framework", Href: "/framework", Hint: "What the generated code is built from."},
			{Title: "Build a real app", Href: "/get-started", Hint: "The four-minute guide, by hand this time."},
		},
	}
}
