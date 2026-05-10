package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/framework"
	"github.com/gofastr/gofastr/framework/static"
	"github.com/gofastr/gofastr/framework/uihost"
)

func main() {
	var (
		buildStatic = flag.String("build-static", "", "output dir for SSG build; empty = serve")
		watch       = flag.Bool("watch", false, "with --build-static, rebuild on file changes")
		watchInt    = flag.Duration("watch-interval", 500*time.Millisecond, "polling interval for --watch")
	)
	flag.Parse()

	if *buildStatic != "" {
		runBuildStatic(*buildStatic, *watch, *watchInt)
		return
	}

	addr := ":8082"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	fwApp, _ := setupServer()

	fmt.Println("━─────────────────────────────────────────────")
	fmt.Println("  GoFastr Website")
	fmt.Println("  http://localhost" + addr)
	fmt.Println()
	fmt.Println("  Pages:  /  /docs/  /docs/:slug  /examples/  /components/  /about")
	fmt.Println("━─────────────────────────────────────────────")

	if err := fwApp.Start(addr); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

// setupServer wires the core-ui app, theme, screens, and host onto a
// framework.App. Both the server entrypoint and the SSG build call this so
// the configuration stays in one place.
func setupServer() (*framework.App, *uihost.UIHost) {
	site := app.NewApp("GoFastr")

	theme := createTheme()
	site.WithTheme(theme)

	layout := app.NewLayout("main").
		WithHeader(&HeaderComponent{}).
		WithFooter(&FooterComponent{})
	site.SetDefaultLayout(layout)

	site.Register("/", &HomeScreen{}, nil)
	site.Register("/docs/", &DocsIndexScreen{}, nil)
	site.Register("/docs/:slug", &DocsPageScreen{}, nil)
	site.Register("/examples/", &ExamplesScreen{}, nil)
	site.Register("/components/", &ComponentsIndexScreen{}, nil)
	site.Register("/components/accordion", &AccordionScreen{}, nil)
	site.Register("/about", &AboutScreen{}, nil)

	cssStr := createStyleSheet(*site.Theme)
	host := uihost.New(site, uihost.WithCustomCSS(cssStr))

	fwApp := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "website"}))
	fwApp.Mount(host)
	return fwApp, host
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
	_ = builder.Watch(ctx, []string{".", "../../docs"}, interval, func(err error) {
		fmt.Fprintf(os.Stderr, "  build error: %v\n", err)
	})
}
