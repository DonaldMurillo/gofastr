package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/devserver"
	"github.com/gofastr/gofastr/core-ui/elements"
	coresignal "github.com/gofastr/gofastr/core-ui/signal"
	"github.com/gofastr/gofastr/core/render"
)

func main() {
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

	// Register screens
	application.RegisterScreen(app.NewScreen("/", &HomeScreen{}), nil)
	application.RegisterScreen(app.NewScreen("/products", &ProductListScreen{}), nil)
	application.RegisterScreen(app.NewScreen("/about", &AboutScreen{}), nil)
	application.RegisterScreen(app.NewDrawer("/cart", &CartDrawer{CartCount: coresignal.New(0)}), nil)

	// Read custom CSS
	cssBytes, err := os.ReadFile("static/demo.css")
	cssStr := ""
	if err == nil {
		cssStr = string(cssBytes)
	}

	// Create DevServer with all subsystems wired
	ds := devserver.NewDevServer(application,
		devserver.WithCustomCSS(cssStr),
		devserver.WithRouteGraph(&devserver.RouteGraph{
			Routes: []devserver.RouteInfo{
				{Path: "/", Title: "Home", Description: "GoFastr Demo Homepage", Preload: true},
				{Path: "/products", Title: "Products", Description: "Browse our products"},
				{Path: "/about", Title: "About", Description: "About GoFastr"},
				{Path: "/cart", Title: "Cart", Description: "Your shopping cart"},
			},
		}),
	)

	// Compile actions for interactive components
	ds.CompileActions("hero-cta", &InteractiveButton{Label: "Browse Products"})
	ds.CompileActions("add-to-cart", &InteractiveButton{Label: "Add to Cart"})

	// Start live island updater (simulates real-time content)
	go liveIslandUpdater(ds)

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	fmt.Println("━" + "─────────────────────────────────────────────")
	fmt.Println("  GoFastr Demo — Full DevServer")
	fmt.Println("  http://localhost:8080")
	fmt.Println()
	fmt.Println("  Pages:  /  /products  /about  /cart")
	fmt.Println("  SSE:    /__gofastr/sse?session=<id>")
	fmt.Println("  JS:     /__gofastr/runtime.js")
	fmt.Println("  Actions:/__gofastr/actions.js")
	fmt.Println("━" + "─────────────────────────────────────────────")

	if err := ds.StartContext(ctx, ":8080"); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

// liveIslandUpdater simulates real-time content streaming via SSE.
// In a real app, this would be triggered by database changes, webhooks, etc.
func liveIslandUpdater(ds *devserver.DevServer) {
	// Create a demo session and register a live island
	sess := ds.CreateSession()

	liveFeed := &LiveFeedComponent{Items: []string{
		"🚀 GoFastr v1.0 released!",
		"📦 New: Island streaming support",
		"⚡ Performance: 2x faster rendering",
	}}
	w := component.NewWidget("live-feed", liveFeed)
	isl := ds.RegisterWidget(sess.ID, w)

	// Periodically push updates
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	items := []string{
		"🎨 Theme system now supports dark mode",
		"🔒 ADA compliance: WCAG 2.1 AA certified",
		"📊 Route preloading reduces TTI by 40%",
		"🧩 Widget hydration is now lazy by default",
		"📡 SSE streaming handles 10K concurrent connections",
	}
	idx := 0

	for range ticker.C {
		liveFeed.Items = append(liveFeed.Items, items[idx%len(items)])
		idx++
		html := isl.Update()
		ds.PushUpdate(isl.ID, string(html), sess.ID)
	}
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

// Ensure unused imports are satisfied
var _ = fmt.Sprintf
var _ = elements.OnClick
