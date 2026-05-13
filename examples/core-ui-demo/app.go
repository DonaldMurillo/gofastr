package main

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	coresignal "github.com/DonaldMurillo/gofastr/core-ui/signal"
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
	return fwApp, host
}

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
