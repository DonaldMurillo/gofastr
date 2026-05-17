package main

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	coresignal "github.com/DonaldMurillo/gofastr/core-ui/signal"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

//go:embed static
var staticFiles embed.FS

// staticDirPath returns a filesystem path for the embedded static directory,
// or falls back to a relative path. Used for serving images etc.
func staticDirPath() string {
	// Try relative to working dir first (works for `go run`)
	if info, err := os.Stat("static"); err == nil && info.IsDir() {
		abs, _ := filepath.Abs("static")
		return abs
	}
	// Try relative to the examples/core-ui-demo directory
	if info, err := os.Stat("examples/core-ui-demo/static"); err == nil && info.IsDir() {
		abs, _ := filepath.Abs("examples/core-ui-demo/static")
		return abs
	}
	return ""
}

// setupHost is a test convenience that returns just the UIHost. The host
// supports ServeHTTP directly, so existing handler-level tests can keep
// calling host.ServeHTTP without the framework App in front of them.
func setupHost() *uihost.UIHost {
	_, host := setupServer()
	return host
}

// setupServer creates a framework.App with the core-ui host mounted on it.
// Used by both main() and browser tests.
func setupServer() (*framework.App, *uihost.UIHost) {
	// Create core-ui app
	application := app.NewApp("GoFastr Demo")

	// Set theme
	theme := createTheme()
	application.WithTheme(theme)

	// Create layout
	layout := app.NewLayout("main").
		WithHeader(&HeaderComponent{}).
		WithFooter(&FooterComponent{})

	application.SetDefaultLayout(layout)

	// Register screens — screens self-declare title, description, type via ScreenSpec
	// DI Showcase: register a singleton StatsService that screens inject
	application.Provide(&StatsService{})

	application.Register("/", &HomeScreen{}, nil)
	application.Register("/products", &ProductListScreen{}, nil)
	application.Register("/products/:slug", &ProductDetailScreen{}, nil)
	application.Register("/about", &AboutScreen{}, nil)

	// Shared cart signal
	cartCount := coresignal.New(0)

	// Overlay demos (fetched as partials, shown as overlays)
	application.Register("/demo-drawer", &DemoDrawerScreen{}, nil)
	application.Register("/demo-sheet", &DemoSheetScreen{}, nil)
	application.Register("/confirm-dialog", &ConfirmDialogScreen{Message: "Are you sure you want to add this item to your cart?"}, nil)

	// Cart page (full page, not overlay)
	application.Register("/cart", &CartDrawer{CartCount: cartCount}, nil)
	application.Register("/signals", &SignalDemoScreen{}, nil)
	application.Register("/error-boundary", &ErrorBoundaryDemoScreen{}, nil)
	application.Register("/dashboard", &DashboardScreen{}, nil)
	application.Register("/todos", &TodosScreen{}, nil)

	// Generate all CSS from Go using the theme system (dog-food!)
	cssStr := createStyleSheet(*application.Theme)

	// Build the UI host — routes and CSS chunks auto-built from screens.
	host := uihost.New(application,
		uihost.WithCustomCSS(cssStr),
		uihost.WithStaticDir(staticDirPath()),
	)

	// Auto-compile actions from screens that implement InteractiveComponent
	host.AutoCompileActions()

	// Compile actions for standalone components (not registered as screens)
	host.CompileActions("home-counter", &CounterComponent{ID: "home-counter"})
	host.CompileActions("add-to-cart", &InteractiveButton{Label: "Add to Cart"})
	host.CompileActions("search-filter", &SearchFilterComponent{})

	// Serve embedded static files if no filesystem path found
	if host.StaticDir() == "" {
		sub, _ := fs.Sub(staticFiles, "static")
		host.SetStaticFS(sub)
	}

	// Wrap in a framework.App so we get the standard middleware chain,
	// graceful shutdown, and a place to attach future entity routes.
	fwApp := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "core-ui-demo"}))
	fwApp.Mount(host)

	// Register the overlay demos as hidden widgets so the home page
	// can open them via data-fui-open and the runtime drives backdrop,
	// scroll-lock, ESC, click-outside, focus trap. Custom skeletons
	// keep the legacy demo classes (`dialog`, `drawer`, `sheet`,
	// `data-overlay`) so existing chaos selectors continue to match.
	r := fwApp.Router
	dialog := preset.Modal("confirm-dialog").
		Hidden().
		Slot("body", &ConfirmDialogScreen{Message: "Are you sure you want to add this item to your cart?"}).
		Skeleton(demoDialogSkeleton).
		Build()
	widget.Mount(r, &dialog)

	drawer := preset.Drawer("demo-drawer").
		Hidden().
		Slot("body", &DemoDrawerScreen{}).
		Skeleton(demoDrawerSkeleton).
		Build()
	widget.Mount(r, &drawer)

	sheet := preset.Modal("demo-sheet").
		Hidden().
		Slot("body", &DemoSheetScreen{}).
		Mount(widget.Bottom).
		Skeleton(demoSheetSkeleton).
		Build()
	// Sheet still wants backdrop + ESC + click-outside; Mount(Bottom)
	// reset them, so re-enable explicitly.
	sheet.Backdrop = true
	sheet.CloseOnEscape = true
	sheet.CloseOnClickOutside = true
	widget.Mount(r, &sheet)

	// uihost already serves /__gofastr/runtime.js + /__gofastr/widgets,
	// the latter now delegating to widget.ServeWidgetList — no
	// MountRuntime call needed.
	return fwApp, host
}

// demoDialogSkeleton renders the widget chrome with legacy demo
// classes so chromedp selectors like `.dialog-overlay`, `.dialog`,
// `[data-overlay]`, `.overlay-close` continue to match. Framework
// attributes (`data-fui-widget`, `fui-pos-center`) ride alongside;
// the close × is a plain button carrying both legacy attrs
// (`data-overlay-close`, `class="overlay-close"`) AND the widget
// runtime's close hook (`data-fui-action="close"`).
const overlayCloseBtn = `<button class="overlay-close" type="button" aria-label="Close" ` +
	`data-overlay-close data-fui-action="close">×</button>`

func demoDialogSkeleton(slots map[string]render.HTML) render.HTML {
	body := slots["body"]
	return render.HTML(
		`<div class="fui-widget fui-pos-center dialog-overlay" data-fui-widget="confirm-dialog" data-overlay>` +
			`<div class="dialog">` + string(body) + overlayCloseBtn + `</div>` +
			`</div>`,
	)
}

func demoDrawerSkeleton(slots map[string]render.HTML) render.HTML {
	body := slots["body"]
	return render.HTML(
		`<div class="fui-widget fui-pos-edge-left drawer-backdrop" data-fui-widget="demo-drawer" data-overlay>` +
			`<nav class="drawer">` + string(body) +
			`<button class="drawer-close-btn" type="button" data-overlay-close data-fui-action="close">Close</button>` +
			overlayCloseBtn +
			`</nav>` +
			`</div>`,
	)
}

func demoSheetSkeleton(slots map[string]render.HTML) render.HTML {
	body := slots["body"]
	return render.HTML(
		`<div class="fui-widget fui-pos-bottom sheet-backdrop" data-fui-widget="demo-sheet" data-overlay>` +
			`<div class="sheet">` + `<div class="sheet-handle"></div>` + string(body) +
			`<button class="sheet-close-btn cta-button" type="button" data-overlay-close data-fui-action="close">Close</button>` +
			overlayCloseBtn +
			`</div>` +
			`</div>`,
	)
}

// _ ensures the demo overlay screens satisfy the Component contract
// when wired as widget slots.
var (
	_ component.Component = (*ConfirmDialogScreen)(nil)
	_ component.Component = (*DemoDrawerScreen)(nil)
	_ component.Component = (*DemoSheetScreen)(nil)
)

// LiveFeedComponent shows a live activity feed that updates via SSE.
type LiveFeedComponent struct {
	Items []string
}

func (l *LiveFeedComponent) Render() render.HTML {
	var items []render.HTML
	for _, item := range l.Items {
		items = append(items, html.ListItem(html.ListItemConfig{}, render.Text(item)))
	}
	return html.Div(
		html.DivConfig{AriaLabel: "Live activity feed", Class: "live-feed", Attrs: html.Attrs{"aria-live": "polite"}},
		html.Heading(html.HeadingConfig{Level: 3}, render.Text("Live Feed")),
		html.UnorderedList(html.ListConfig{Class: "feed-list"}, items...),
	)
}
