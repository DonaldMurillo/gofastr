// =============================================================================
// examples/site — the public GoFastr product site. Distinct from
// examples/website (which stays as the feature gallery / contributor demo).
//
// Boot: core-ui app + typed v2 theme + StyleSheet DSL output + UIHost on :8083.
// Plus a global ui.CommandPalette wired into the nav, a ui.ThemeToggle in the
// chrome, and a custom 404 screen rendered through the same layout. Dev
// livereload + SSE come for free via framework.NewApp.
// =============================================================================

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func main() {
	fwApp := setupServer()

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

// setupServer wires the whole site and returns the framework.App without
// binding a port — main() calls Start, tests drive app.Router() directly
// (mirrors examples/website's setupServer so the site is testable).
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
	})
	paletteDef := paletteBuilder.Build()

	layout := app.NewLayout("main").
		WithHeader(&HeaderComponent{}).
		WithFooter(&FooterComponent{})
	site.SetDefaultLayout(layout)

	registerScreens(site)

	host := uihost.New(site,
		uihost.WithCustomCSS(createStyleSheet(t)),
		uihost.WithNotFoundScreen(&NotFoundScreen{}),
		uihost.WithDescription("A pre-alpha Go full-stack framework where AI agents are first-class authors."),
		uihost.WithOpenGraph(uihost.OG{
			Title: "GoFastr",
			URL:   "https://gofastr.dev",
			Type:  "website",
		}),
		uihost.WithCanonicalURL("https://gofastr.dev"),
	)

	fwApp := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "site"}),
	)
	fwApp.Mount(host)

	// Mount the palette widget AFTER the host so its routes land on the
	// same router instance. The palette's RPC handler runs an in-memory
	// fuzzy match over a curated route catalog — no DB roundtrip.
	widget.Mount(fwApp.Router(), &paletteDef)
	fwApp.Router().Post("/__site/palette", http.HandlerFunc(servePaletteSearch))
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
		var body struct{ Message string `json:"message"` }
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
	// Modal for the "RPC → Open Widget" demo.
	// Hidden by default — only appears when data-fui-rpc-open triggers it.
	modalBody := html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"style": "text-align:center;padding:var(--s-8,32px) 0"}},
		render.Tag("p", map[string]string{"style": "font-size:24px;margin:0 0 8px"}, render.Text("🎉")),
		html.Heading(html.HeadingConfig{Level: 3}, render.Text("Congratulations!")),
		render.Tag("p", nil, render.Text("This modal was triggered from an in-browser action. The server returned 2xx, so the runtime opened the widget. No JavaScript required.")),
	)
	modalDef := preset.Modal("interactive-result-modal").
		Slot("body", app.NewStaticComponent(modalBody)).
		Build()
	modalDef.Hidden = true
	widget.Mount(fwApp.Router(), &modalDef)

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
	{"Kiln — agent build mode", "/kiln"},
	{"Philosophy — the convictions essay", "/philosophy"},
	{"Components — gallery index", "/components/"},
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
	site.Register("/kiln", &KilnScreen{}, nil)
	site.Register("/philosophy", &PhilosophyScreen{}, nil)

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
		WithDescription("Every framework/ui and core-ui/patterns primitive, one page each."), nil)
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
