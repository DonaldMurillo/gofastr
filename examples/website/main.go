package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
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
	fmt.Println("  Pages:  /  /docs/  /docs/:slug  /examples/  /components/  /framework-ui/  /about")
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
	site.Register("/components/tabs", &TabsScreen{}, nil)
	site.Register("/components/progress", &ProgressScreen{}, nil)
	site.Register("/components/skeleton", &SkeletonScreen{}, nil)
	site.Register("/components/breadcrumbs", &BreadcrumbsScreen{}, nil)
	site.Register("/components/pagination", &PaginationScreen{}, nil)
	site.Register("/framework-ui/", &FrameworkUIScreen{}, nil)
	site.Register("/framework-ui/datatable", &DataTableDemoScreen{}, nil)
	site.Register("/framework-ui/form", &FormDemoScreen{}, nil)
	site.Register("/framework-ui/theme", &ThemeSwapDemoScreen{}, nil)
	site.Register("/framework-ui/notification", &NotificationDemoScreen{}, nil)
	site.Register("/about", &AboutScreen{}, nil)

	cssStr := createStyleSheet(*site.Theme)
	hostOpts := []uihost.Option{uihost.WithCustomCSS(cssStr)}
	if devMode() {
		hostOpts = append(hostOpts, uihost.WithExtraScripts("/__livereload.js"))
	}
	host := uihost.New(site, hostOpts...)

	fwApp := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "website"}))
	fwApp.Mount(host)

	if devMode() {
		// Dev-only livereload — long-polled connection that drops on
		// server restart, paired with a tiny script that reloads when
		// the poll errors. CSP-safe (external file, no inline JS).
		// Gated by GOFASTR_DEV=1 so tests and production are clean.
		fwApp.Router.Get("/__livereload", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "text/plain")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}))
		fwApp.Router.Get("/__livereload.js", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/javascript")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(livereloadJS))
		}))
	}
	return fwApp, host
}

// devMode reports whether the dev-only livereload tooling should be
// wired up. Set GOFASTR_DEV=1 in the watcher's environment.
func devMode() bool {
	return os.Getenv("GOFASTR_DEV") == "1"
}

const livereloadJS = `(() => {
  const wait = (ms) => new Promise((r) => setTimeout(r, ms));
  const reachable = async () => {
    try { const r = await fetch('/__livereload', {method: 'HEAD'}); return r.ok; }
    catch { return false; }
  };
  const watch = async () => {
    try { await fetch('/__livereload'); } catch {}
    while (!(await reachable())) { await wait(200); }
    location.reload();
  };
  watch();
})();`

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
