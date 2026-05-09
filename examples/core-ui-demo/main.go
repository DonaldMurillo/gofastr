package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/framework/static"
	"github.com/gofastr/gofastr/framework/uihost"
)

func main() {
	var (
		buildStatic = flag.String("build-static", "", "output dir for static-site generation; empty = serve")
		watch       = flag.Bool("watch", false, "with --build-static, rebuild on file changes")
		watchInt    = flag.Duration("watch-interval", 500*time.Millisecond, "polling interval for --watch")
	)
	flag.Parse()

	if *buildStatic != "" {
		runBuildStatic(*buildStatic, *watch, *watchInt)
		return
	}

	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	fwApp, host := setupServer()

	// Start live island updater (simulates real-time content)
	go liveIslandUpdater(host)

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
		_ = fwApp.Shutdown(context.Background())
	}()

	fmt.Println("━" + "─────────────────────────────────────────────")
	fmt.Println("  GoFastr Demo — framework.App + uihost")
	fmt.Println("  http://localhost" + addr)
	fmt.Println()
	fmt.Println("  Pages:  /  /products  /about  /cart  /todos")
	fmt.Println("  SSE:    /__gofastr/sse?session=<id>")
	fmt.Println("  JS:     /__gofastr/runtime.js")
	fmt.Println("  Actions:/__gofastr/actions.js")
	fmt.Println("━" + "─────────────────────────────────────────────")

	if err := fwApp.Start(addr); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

// liveIslandUpdater simulates real-time content streaming via SSE.
func liveIslandUpdater(host *uihost.UIHost) {
	sess := host.CreateSession()

	liveFeed := &LiveFeedComponent{Items: []string{
		"🚀 GoFastr v1.0 released!",
		"📦 New: Island streaming support",
		"⚡ Performance: 2x faster rendering",
	}}
	w := component.NewWidget("live-feed", liveFeed)
	isl := host.RegisterWidget(sess.ID, w)

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
		host.PushUpdate(isl.ID, string(html), sess.ID)
	}
}

func runBuildStatic(out string, watch bool, interval time.Duration) {
	_, host := setupServer()
	builder := &static.Builder{
		Host:   host,
		OutDir: out,
		Logger: func(format string, args ...any) {
			fmt.Printf("  "+format+"\n", args...)
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nStopping watcher...")
		cancel()
	}()

	if !watch {
		res, err := builder.Build(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "build-static: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\nBuilt %d page(s) and %d asset(s) into %s\n", len(res.Pages), len(res.Assets), out)
		return
	}

	fmt.Printf("Watching for changes (interval=%s)...\n", interval)
	_ = builder.Watch(ctx, []string{"."}, interval, func(err error) {
		fmt.Fprintf(os.Stderr, "  build error: %v\n", err)
	})
}

// Ensure unused imports are satisfied
var _ = fmt.Sprintf
var _ = elements.OnClick
var _ = render.HTML("")
