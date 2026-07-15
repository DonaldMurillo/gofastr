// =============================================================================
// examples/site — the GoFastr product site AND the canonical feature gallery.
// The single example app: the product/marketing pages, the docs, and a
// one-page-per-primitive component showcase plus SEO / wizard / print demos.
//
// Boot: core-ui app + typed v2 theme + StyleSheet DSL output + UIHost on :8083.
// Plus a global ui.CommandPalette wired into the nav, a ui.ThemeToggle in the
// chrome, and a custom 404 screen rendered through the same layout. Dev
// livereload + SSE come for free via framework.NewApp.
// =============================================================================

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	gflog "github.com/DonaldMurillo/gofastr/battery/log"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/island"
	patternsSortablelist "github.com/DonaldMurillo/gofastr/core-ui/patterns/sortablelist"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/docs"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func main() {
	fwApp := setupServer()

	// `site --export <dir>` renders the whole site to static HTML + assets
	// and exits — the native replacement for the wget mirror pages.yml used
	// to ship. Declaration-driven (no crawling) and dumps the split runtime
	// modules the crawl broke. See framework.App.ExportStatic.
	if dir := exportDir(os.Args[1:]); dir != "" {
		if err := fwApp.ExportStatic(context.Background(), dir, exportBase(os.Args[1:])); err != nil {
			fmt.Fprintf(os.Stderr, "export: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("static site exported to %s\n", dir)
		return
	}

	// Port from $PORT (dev-watch sets it); default 8083 for plain `go run .`.
	addr := ":8083"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	fmt.Println("━─────────────────────────────────────────────")
	fmt.Println("  GoFastr — product site (v2)")
	fmt.Println("  http://localhost" + addr)
	fmt.Println("━─────────────────────────────────────────────")
	if err := fwApp.Start(addr); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

// exportDir scans args for `--export <dir>` or `--export=<dir>`, returning
// the target directory or "" when the flag is absent. Used by main to switch
// between serving and one-shot static export.
func exportDir(args []string) string {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--export" && i+1 < len(args):
			return args[i+1]
		case strings.HasPrefix(args[i], "--export="):
			return strings.TrimPrefix(args[i], "--export=")
		}
	}
	return ""
}

// exportBase extracts the --export-base <path> flag (the URL subpath the
// static site is served under, e.g. "/gofastr" for a GitHub Pages project
// site). Returns "" when absent (apex deploy). Paired with --export.
func exportBase(args []string) string {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--export-base" && i+1 < len(args):
			return args[i+1]
		case strings.HasPrefix(args[i], "--export-base="):
			return strings.TrimPrefix(args[i], "--export-base=")
		}
	}
	return ""
}

// ── Presence demo wiring (additive) ────────────────────────────────
// siteIslands holds the host's island manager so the presence demo
// screen (screen_presence.go) can read the live roster at render time.
// Set once in setupServer.
var siteIslands *island.Manager

// setupServer wires the whole site and returns the framework.App without
// binding a port — main() calls Start, tests drive app.Router() directly so
// the site is testable end-to-end.
func setupServer() *framework.App {
	site := app.NewApp("GoFastr")

	t := createTheme()
	site.WithTheme(t)

	// CommandPalette — the global ⌘K palette. We only need the widget
	// definition; the header's own search button opens it and binds the
	// shortcut, so the returned trigger is discarded.
	_, paletteBuilder := ui.CommandPalette(ui.CommandPaletteConfig{
		Name:        "site-command-palette",
		RPCPath:     "/__site/palette",
		Placeholder: "Search docs, examples, components…",
		// Static command list → client-side search. Works identically live
		// and on the static export (no search endpoint needed). Takes
		// precedence over RPCPath in the combobox.
		Commands: paletteCommands(),
	})

	layout := app.NewLayout("main").
		WithHeader(&HeaderComponent{}).
		WithFooter(&FooterComponent{})
	site.SetDefaultLayout(layout)

	registerScreens(site)

	// allowAIBots is the *bool passed into WithAgentReady below so the
	// robots.txt advertises explicit AI-crawler rules.
	allowAIBots := true
	// markdownNeg turns on Accept: text/markdown content negotiation (paired
	// with WithPublicLLMMD above) — lights up the scanner's Markdown check.
	markdownNeg := true

	host := uihost.New(site,
		uihost.WithCustomCSS(createStyleSheet(t)),
		uihost.WithNotFoundScreen(&NotFoundScreen{}),
		// Per-screen markdown surface (/llm-pages.md + /<screen>/llm.md). The
		// site has no per-user data shapes to leak, so publishing it is safe.
		// Content negotiation itself is turned on by the bundle's
		// ContentNegotiation field below.
		uihost.WithPublicLLMMD(),
		uihost.WithDescription("An early (v0.x) Go full-stack framework where AI agents are first-class authors."),
		uihost.WithOpenGraph(uihost.OG{
			Title: "GoFastr",
			URL:   "https://gofastr.dev",
			Type:  "website",
		}),
		// No global WithCanonicalURL — a fixed canonical on every page would
		// declare the homepage canonical site-wide. Pages that need one
		// implement ScreenCanonical (see /seo); the rest omit it.
		// Sitewide SEO endpoints backing the /seo demo page.
		uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://gofastr.dev"}),
		// Open to crawlers but keep internal runtime endpoints out of the index.
		uihost.WithRobots(uihost.RobotsConfig{Disallow: []string{"/__gofastr/"}}),
		// Agent-readiness bundle: /llms.txt + A2A agent card + AI-bot
		// robots rules + Link response headers on every HTML page.
		// BaseURL is intentionally omitted — resolveBaseURL reuses the
		// sitemap's canonical origin above, so one origin drives all
		// discovery URLs.
		uihost.WithAgentReady(uihost.AgentReadyConfig{
			Title:              "GoFastr",
			Summary:            "An early (v0.x) Go web framework where AI agents are first-class authors and consumers. MCP tools + framework docs are served at /mcp.",
			AllowAIBots:        &allowAIBots,
			ContentNegotiation: &markdownNeg,
			ContentSignals:     "ai-train=no, search=yes, ai-input=yes",
			AgentCard: &uihost.AgentCardConfig{
				Name:        "GoFastr",
				Description: "Framework docs + introspection agent for gofastr.dev.",
				MCPEndpoint: "/mcp",
			},
		}),
	)

	// ── Presence demo wiring (additive) ────────────────────────────
	// Expose the island manager to the presence screen and wire the live
	// roster push: when a topic's roster changes (join/leave), re-render the
	// AvatarGroup and push it to every session on that topic via the existing
	// island SSE lane. This reuses PushUpdate — no new transport.
	siteIslands = host.Islands
	host.Islands.OnPresenceChange = func(topic string) {
		rosterHTML := string(renderPresenceRoster(host.Islands.PresenceRoster(topic)))
		for _, sid := range host.Islands.PresenceSessions(topic) {
			host.Islands.PushUpdate(island.IslandUpdate{
				IslandID: "presence-roster-" + topic,
				HTML:     rosterHTML,
			}, sid)
		}
	}

	fwApp := framework.NewUIHostApp(host,
		framework.WithConfig(framework.AppConfig{Name: "site"}),
		// Expose the framework's own docs + app introspection as MCP tools
		// (the site is gofastr.dev), and auto-mount /mcp so the agent card
		// above advertises a live endpoint. Under `gofastr dev` /
		// dev-watch the framework auto-adds the mutating control tools;
		// the deployed site never sets GOFASTR_DEV, so its public /mcp
		// stays read-only.
		framework.WithMCPIntrospection(),
		framework.WithMCP(),
	)

	// Structured logging (battery/log). Its MCP debug tools (log_recent,
	// log_filter, …) auto-register in the dev loop only — access logs
	// carry client IPs and the deployed site's /mcp is public by design.
	fwApp.RegisterPlugin(gflog.New(gflog.Config{}))

	// The site's own readiness: the embedded docs corpus it serves (both as
	// pages and as framework_docs_* MCP tools) must load. Zero registered
	// checks would make app_readiness report ready=false ("unconfirmed"),
	// which reads as an outage to a connecting agent.
	fwApp.RegisterReadiness("docs-embed", func(ctx context.Context) error {
		topics, err := docs.List()
		if err != nil {
			return err
		}
		if len(topics) == 0 {
			return fmt.Errorf("embedded docs corpus is empty")
		}
		return nil
	})

	// Mount the palette widget AFTER the host so its routes land on the
	// same router instance. The palette's RPC handler runs an in-memory
	// fuzzy match over a curated route catalog — no DB roundtrip.
	widget.MountBuilder(fwApp.Router(), paletteBuilder)
	fwApp.Router().Post("/__site/palette", http.HandlerFunc(servePaletteSearch))

	// SectionMenu mobile drawers — the docs + components navs each mount a
	// preset.Drawer once (backdrop + click-outside/Escape close + scroll lock
	// + focus trap, all from the framework widget). The inline rails render
	// the same config per page.
	widget.MountBuilder(fwApp.Router(), interactive.SectionMenuDrawer(docsSectionMenuConfig("")))
	widget.MountBuilder(fwApp.Router(), interactive.SectionMenuDrawer(componentsSectionMenuConfig()))
	widget.MountBuilder(fwApp.Router(), interactive.SectionMenuDrawer(demoSectionMenuConfig()))
	// Kiln panel approve/reject — no-op endpoints. The OptimisticAction
	// runtime needs a real 2xx response to keep the optimistic label;
	// these record nothing because the page is a demo, but the round-trip
	// is genuine (network panel will show the POST).
	fwApp.Router().Post("/__site/kiln/approve", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	fwApp.Router().Post("/__site/kiln/reject", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	// ToggleAction demo — same deal: the toggle runtime keeps the
	// committed (or reverted) state only on a real 2xx, so the demo
	// buttons round-trip through this no-op.
	fwApp.Router().Post("/__site/toggle/noop", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	// Interactive demo endpoints — each returns JSON the runtime pushes
	// into a signal or triggers a widget open / SPA navigate.
	//
	// NOTE: The endpoints below are unauthenticated demo handlers for the
	// interactive examples. They have no CSRF protection, rate limiting,
	// or input sanitization. Do NOT copy these as a template for
	// production code.
	var demoCounter atomic.Int64
	fwApp.Router().Post("/__site/interactive/counter", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `%d`, demoCounter.Add(1))
	}))
	fwApp.Router().Post("/__site/interactive/open-drawer", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	fwApp.Router().Post("/__site/interactive/submit", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			body.Message = ""
		}
		w.Header().Set("Content-Type", "application/json")
		msg := "✓ Received: " + body.Message
		json.NewEncoder(w).Encode(msg)
	}))
	fwApp.Router().Post("/__site/interactive/navigate", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	// Sortable kanban demo (/components/sortablelist): a 3-column board
	// backed by the package-level kanbanBoard store. The move endpoint
	// accepts order/moved/container/version; the conflict endpoint
	// returns fresh <li> HTML for 409 reconciliation.
	fwApp.Router().Post("/__site/sortable/move", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		container := r.FormValue("container")
		moved := r.FormValue("moved")
		orderStr := r.FormValue("order")
		version := r.FormValue("version")

		kanbanBoard.Lock()
		defer kanbanBoard.Unlock()

		// Optimistic-concurrency check (only when version is sent).
		if version != "" {
			if version != fmt.Sprintf("v%d", kanbanBoard.Version) {
				w.WriteHeader(http.StatusConflict)
				return
			}
		}

		destIdx, destCol := kanbanColumnByID(container)
		if destCol == nil {
			http.Error(w, "unknown container", http.StatusBadRequest)
			return
		}

		// Build a card lookup across all columns.
		cardMap := map[string]kanbanCard{}
		for _, col := range kanbanBoard.Columns {
			for _, c := range col.Cards {
				cardMap[c.Key] = c
			}
		}

		// Parse the new destination order.
		var newCards []kanbanCard
		for _, k := range strings.Split(orderStr, ",") {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			if c, ok := cardMap[k]; ok {
				newCards = append(newCards, c)
			}
		}

		// Cross-container: remove the moved card from its source column.
		if moved != "" {
			for i := range kanbanBoard.Columns {
				if i == destIdx {
					continue
				}
				for j, c := range kanbanBoard.Columns[i].Cards {
					if c.Key == moved {
						kanbanBoard.Columns[i].Cards = append(
							kanbanBoard.Columns[i].Cards[:j],
							kanbanBoard.Columns[i].Cards[j+1:]...)
						break
					}
				}
			}
		}

		kanbanBoard.Columns[destIdx].Cards = newCards
		kanbanBoard.Version++
		w.WriteHeader(http.StatusNoContent)
	}))
	fwApp.Router().Get("/__site/sortable/conflict", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		container := r.URL.Query().Get("container")
		kanbanBoard.Lock()
		defer kanbanBoard.Unlock()
		_, col := kanbanColumnByID(container)
		if col == nil {
			http.Error(w, "unknown container", http.StatusNotFound)
			return
		}
		items := make([]patternsSortablelist.Item, len(col.Cards))
		for i, c := range col.Cards {
			items[i] = patternsSortablelist.Item{Key: c.Key, Label: c.Title}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, string(patternsSortablelist.RenderItems(patternsSortablelist.Config{
			Label:       col.Title,
			Group:       "kanban-demo",
			Container:   col.ID,
			RPCPath:     "/__site/sortable/move",
			Version:     fmt.Sprintf("v%d", kanbanBoard.Version),
			ConflictRPC: "/__site/sortable/conflict?container=" + col.ID,
			Items:       items,
		})))
	}))

	// Workspace example (/examples/workspace): GET handlers returning the
	// detail HTML fragment the pane-host's data-fui-signal (mode=html)
	// regions swap in. The runtime uses r.text() for non-JSON responses,
	// so we return trusted server-rendered HTML with a text/html type.
	// Read-only demo lookups — no CSRF needed (see the note above).
	fwApp.Router().Get("/__site/workspace/ticket", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t, ok := wsTicketByID(r.URL.Query().Get("id"))
		if !ok {
			http.Error(w, "unknown ticket", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, string(renderTicketDetail(t)))
	}))
	fwApp.Router().Get("/__site/workspace/customer", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := wsCustomers[r.URL.Query().Get("id")]
		if !ok {
			http.Error(w, "unknown customer", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, string(renderCustomerDetail(c)))
	}))
	fwApp.Router().Post("/__site/interactive/error", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "something went wrong")
	}))
	// Modal for the "RPC → Open Widget" demo.
	// Hidden by default — only appears when data-fui-rpc-open triggers it.
	modalBody := html.Div(html.DivConfig{Class: "demo-modal-body"},
		html.Paragraph(html.TextConfig{Class: "demo-modal-emoji"}, render.Text("🎉")),
		html.Heading(html.HeadingConfig{Level: 3, ID: "demo-modal-heading"}, render.Text("Congratulations!")),
		html.Paragraph(html.TextConfig{}, render.Text("This modal was triggered from an in-browser action. The server returned 2xx, so the runtime opened the widget. No JavaScript required.")),
	)
	widget.MountBuilder(fwApp.Router(), preset.Modal("demo-result-modal").
		LabelledBy("demo-modal-heading").
		Slot("body", app.NewStaticComponent(modalBody)).
		Hidden())

	// Overlay widgets backing the /components/{modal,drawer,bottomsheet,toast}
	// showcase pages. Each is mounted once here; the catalog demos render a
	// trigger button (data-fui-open / data-fui-toast) that opens them. Modal +
	// drawer are Hidden (lazy-fetched on open); the toast stack is auto-mount
	// (always inlined) so a toast has somewhere to land on any page.
	demoModalBody := html.Div(html.DivConfig{Class: "demo-modal-body"},
		html.Heading(html.HeadingConfig{Level: 3, ID: "site-demo-modal-heading"}, render.Text("Edit user")),
		html.Paragraph(html.TextConfig{}, render.Text("Center-mounted dialog with a backdrop, focus trap, and Escape-to-close. Open a deeplinked variant and the URL gains ?modal=user-edit&user_id=42 — refresh re-opens it.")),
	)
	widget.MountBuilder(fwApp.Router(), preset.Modal("site-demo-modal").
		LabelledBy("site-demo-modal-heading").
		DeepLink("modal", "user-edit").
		DeepLinkParam("user_id").
		Slot("body", app.NewStaticComponent(demoModalBody)).
		Hidden())

	demoDrawerBody := html.Div(html.DivConfig{Class: "demo-modal-body"},
		html.Heading(html.HeadingConfig{Level: 3, ID: "site-demo-drawer-heading"}, render.Text("Filters")),
		html.Paragraph(html.TextConfig{}, render.Text("Side-mounted sliding panel. Same dismiss affordances as Modal — backdrop, Escape, focus trap, scroll lock.")),
	)
	widget.MountBuilder(fwApp.Router(), preset.Drawer("site-demo-drawer").
		LabelledBy("site-demo-drawer-heading").
		Slot("body", app.NewStaticComponent(demoDrawerBody)).
		Hidden())

	demoSheetBody := html.Div(html.DivConfig{Class: "demo-modal-body"},
		html.Heading(html.HeadingConfig{Level: 3, ID: "site-demo-sheet-heading"}, render.Text("Share")),
		html.Paragraph(html.TextConfig{}, render.Text("Bottom-anchored sibling of Drawer. Drag the handle down past ~80px to dismiss; Escape and backdrop click also close it.")),
	)
	widget.MountBuilder(fwApp.Router(), preset.BottomSheet("site-demo-bottomsheet").
		LabelledBy("site-demo-sheet-heading").
		Slot("body", app.NewStaticComponent(demoSheetBody)).
		Hidden())

	// /components/sidebar showcase: mount the < 900px nav drawer the Sidebar's
	// hamburger opens. Without this the hamburger silently no-ops. Shares
	// sidebarShowcaseConfig with the inline render so DrawerName ("ui-sidebar-
	// drawer", the ui.Sidebar default) + nav content match. Page-scoped to the
	// one showcase route.
	widget.MountBuilder(fwApp.Router(), preset.Drawer("ui-sidebar-drawer").
		Slot("body", app.NewStaticComponent(ui.SidebarBody(sidebarShowcaseConfig))).
		Hidden().
		Pages("/components/sidebar"))

	// Toast stack anchored top-right; auto-mount so every page can fire one.
	widget.MountBuilder(fwApp.Router(), preset.ToastStack("site-toasts").Mount(widget.TopRight))
	// Server-path toast demo: any data-fui-rpc handler can attach the toast
	// header on a 2xx and the runtime fires it (no SSE, no extra request).
	fwApp.Router().Post("/__site/toast/push", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ui.AddToastSuccess(w, "Saved", "Pushed from the server via the X-Gofastr-Toast header.", 5000)
		w.WriteHeader(http.StatusNoContent)
	}))

	// Multi-step wizard round-trip demo (self-contained full-page form that
	// POSTs to itself — no site chrome). GET renders step 0; POST advances.
	fwApp.Router().Get(wizardDemoPath, http.HandlerFunc(WizardDemoHandler))
	fwApp.Router().Post(wizardDemoPath, http.HandlerFunc(WizardDemoHandler))

	// battery/print demo documents under /print/*.
	registerPrintDemos(fwApp)

	return fwApp
}

// paletteRoute is one entry in the command palette's search catalog.
type paletteRoute struct{ title, path string }

// paletteCatalog seeds the ⌘K palette. Lives in main so add_routes
// adds an entry here at the same time it adds a Register call below.
var paletteCatalog = []paletteRoute{
	{"Home", "/"},
	{"Get started", "/get-started"},
	{"Docs index", "/docs/"},
	{"Entity declarations — modeling the domain", "/docs/entity-declarations"},
	{"Examples — six reference apps", "/examples"},
	{"Workspace — master-detail pane-host example", "/examples/workspace"},
	{"Live presence — viewer roster demo", "/examples/presence?presence=presence-demo"},
	{"Kiln — agent build mode (experimental)", "/kiln"},
	{"Philosophy — the convictions essay", "/philosophy"},
	{"Components — gallery index", "/components/"},
	{"SEO — per-page meta, canonical, JSON-LD", "/seo"},
	{"Forms wizard — multi-step round-trip", "/forms/wizard"},
	{"Print — invoice / receipt documents", "/print/invoice/1"},
}

// paletteCommands maps the curated route catalog into static palette
// commands so ⌘K search is fully client-side — instant on the live server
// and functional on the static export with no search endpoint.
func paletteCommands() []ui.PaletteCommand {
	cmds := make([]ui.PaletteCommand, 0, len(paletteCatalog))
	for _, r := range paletteCatalog {
		cmds = append(cmds, ui.PaletteCommand{Label: r.title, Href: r.path, Meta: r.path})
	}
	return cmds
}

// servePaletteSearch returns matching options as <li role="option">
// fragments — the format ui.CommandPalette's combobox expects.
func servePaletteSearch(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	q := strings.ToLower(strings.TrimSpace(r.FormValue("q")))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	matched := 0
	for i, p := range paletteCatalog {
		if q != "" && !strings.Contains(strings.ToLower(p.title), q) && !strings.Contains(strings.ToLower(p.path), q) {
			continue
		}
		// data-fui-push-state navigates without a hard refresh on click.
		// data-value is what the combobox echoes back to the input.
		_, _ = fmt.Fprintf(w,
			`<li role="option" id="site-pal-%d" data-value=%q data-fui-push-state=%q><span>%s</span><span class="pal-meta">%s</span></li>`,
			i, p.title, p.path, htmlEscape(p.title), htmlEscape(p.path))
		matched++
	}
	if matched == 0 {
		_, _ = w.Write([]byte(`<li role="option" aria-disabled="true">No matches</li>`))
	}
}

// htmlEscape is a tiny inline escape for the palette options. We can
// trust the catalog values (compile-time constants); the function is
// defensive only.
func htmlEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// registerScreens wires every route — the main pages plus the per-
// component showcase pages. Lives in main.go so the route catalog and
// the palette seed stay editable side by side.
func registerScreens(site *app.App) {
	site.Register("/", &HomeScreen{}, nil)
	site.Register("/get-started", &GetStartedScreen{}, nil)
	site.Register("/docs/", &ConceptsIndexScreen{}, nil)
	// One /docs/<slug> page per catalog entry, each rendering the embedded
	// framework doc. Driven by docIntents so routes and cards stay in sync.
	for _, dp := range flatDocs() {
		site.Register("/docs/"+dp.Slug, &DocPageScreen{Entry: dp}, nil)
	}
	site.Register("/examples", &ExamplesScreen{}, nil)
	// Advanced, full-page interactive example: a master-detail workspace
	// on ui.PaneHost. Its /__site/workspace/* detail endpoints are mounted
	// in setupServer.
	site.Register("/examples/workspace", &WorkspaceScreen{}, nil)
	// ── Presence demo (additive) ───────────────────────────────────
	// /examples/presence?presence=presence-demo — a live avatar roster.
	// The ?presence= param is threaded into the SSE <meta> tag by
	// handlePage so the connection joins the topic.
	site.Register("/examples/presence", &PresenceScreen{}, nil)
	site.Register("/kiln", &KilnScreen{}, nil)
	site.Register("/philosophy", &PhilosophyScreen{}, nil)
	// SEO demo pages (per-concern interfaces + the ScreenSEO bundle).
	site.Register("/seo", &SEOScreen{}, nil)
	site.Register("/seo-bundle", &SEOBundleScreen{}, nil)

	// Components — registered through a ScreenGroup so every /components/*
	// page shares an inner layout. The inner layout puts the multi-level
	// ComponentsSidebar in the sidebar slot; the framework's runtime
	// detects sibling-nav inside the group (via data-fui-screen-group on
	// the layout wrapper) and swaps ONLY the inner content cell — the
	// sidebar stays in place across navigations, no full reload.
	componentsLayout := app.NewLayout("components").
		WithSidebar(&ComponentsSidebar{})
	componentsGroup := app.NewScreenGroup("/components", componentsLayout)
	componentsGroup.Screen(app.NewScreen("/components/", &ComponentsIndexScreen{}).
		WithTitle("Components").
		WithDescription("Every framework/ui and core-ui/patterns constructor, one page each."), nil)
	for _, c := range componentCatalog {
		componentsGroup.Screen(app.NewScreen("/components/"+c.Slug, &ComponentShowcaseScreen{Entry: c}).
			WithTitle(c.Name), nil)
	}
	site.Router.ScreenGroup(componentsGroup)
}

// _ = strconv is here so the import survives even if servePaletteSearch
// goes through a refactor that drops the explicit %d formatting; the
// palette options must always carry a unique id for combobox arrow-key
// navigation, so the strconv pattern stays load-bearing.
var _ = strconv.Itoa
