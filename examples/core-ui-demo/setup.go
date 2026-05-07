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

// findCSS loads demo.css from embedded static files.
func findCSS() string {
	data, err := staticFiles.ReadFile("static/demo.css")
	if err != nil {
		return ""
	}
	return string(data)
}

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

	// Register screens — this IS the route table
	application.RegisterScreen(app.NewScreen("/", &HomeScreen{}).WithTitle("Home").WithDescription("GoFastr Demo Homepage"), nil)
	application.RegisterScreen(app.NewScreen("/products", &ProductListScreen{}).WithTitle("Products").WithDescription("Browse our products"), nil)
	application.RegisterScreen(app.NewScreen("/about", &AboutScreen{}).WithTitle("About").WithDescription("About GoFastr"), nil)
	application.RegisterScreen(app.NewDrawer("/cart", &CartDrawer{CartCount: coresignal.New(0)}).WithTitle("Cart").WithDescription("Your shopping cart"), nil)

	// Read custom CSS from embedded files
	cssStr := findCSS()

	// Create DevServer — routes are auto-built from registered screens
	ds := devserver.NewDevServer(application,
		devserver.WithCustomCSS(cssStr),
		devserver.WithStaticDir(staticDirPath()),
	)

	// Compile actions for interactive components
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
