package main

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/devserver"
	"github.com/gofastr/gofastr/core-ui/elements"
	coresignal "github.com/gofastr/gofastr/core-ui/signal"
	"github.com/gofastr/gofastr/core/render"
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

// setupDevServer creates and configures the DevServer with all routes,
// themes, actions, and subsystems. Used by both main() and browser tests.
func setupDevServer() *devserver.DevServer {
	// Create app
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

	// Generate all CSS from Go using the theme system (dog-food!)
	cssStr := createStyleSheet(*application.Theme)

	// Create DevServer — routes and CSS chunks auto-built from registered screens
	ds := devserver.NewDevServer(application,
		devserver.WithCustomCSS(cssStr),
		devserver.WithStaticDir(staticDirPath()),
	)

	// Auto-compile actions from screens that implement InteractiveComponent
	ds.AutoCompileActions()

	// Compile actions for standalone components (not registered as screens)
	ds.CompileActions("home-counter", &CounterComponent{ID: "home-counter"})
	ds.CompileActions("add-to-cart", &InteractiveButton{Label: "Add to Cart"})
	ds.CompileActions("search-filter", &SearchFilterComponent{})

	// Serve embedded static files if no filesystem path found
	if ds.StaticDir() == "" {
		sub, _ := fs.Sub(staticFiles, "static")
		ds.SetStaticFS(sub)
	}

	return ds
}

// LiveFeedComponent shows a live activity feed that updates via SSE.
type LiveFeedComponent struct {
	Items []string
}

func (l *LiveFeedComponent) Render() render.HTML {
	var items []render.HTML
	for _, item := range l.Items {
		items = append(items, elements.ListItem(nil, render.Text(item)))
	}
	return elements.Div(
		elements.Attrs{"aria-label": "Live activity feed", "aria-live": "polite", "class": "live-feed"},
		elements.Heading(3, nil, render.Text("Live Feed")),
		elements.UnorderedList(elements.Attrs{"class": "feed-list"}, items...),
	)
}
