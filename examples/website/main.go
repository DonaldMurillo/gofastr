package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/static"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
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
	site.Register("/components/modal", &ModalScreen{}, nil)
	site.Register("/components/drawer", &DrawerScreen{}, nil)
	site.Register("/components/toast", &ToastScreen{}, nil)
	site.Register("/components/menu", &MenuScreen{}, nil)
	site.Register("/components/sidebar", &SidebarScreen{}, nil)
	site.Register("/components/layout", &LayoutScreen{}, nil)
	site.Register("/components/card", &CardScreen{}, nil)
	site.Register("/components/image", &OptimizedImageScreen{}, nil)
	site.Register("/components/toggle", &ToggleScreen{}, nil)
	site.Register("/components/tooltip", &TooltipScreen{}, nil)
	site.Register("/components/popover", &PopoverScreen{}, nil)
	site.Register("/components/tag", &TagScreen{}, nil)
	site.Register("/components/spinner", &SpinnerScreen{}, nil)
	site.Register("/components/divider", &DividerScreen{}, nil)
	site.Register("/components/fileupload", &FileUploadScreen{}, nil)
	site.Register("/components/kbd", &KbdScreen{}, nil)
	site.Register("/components/avatargroup", &AvatarGroupScreen{}, nil)
	site.Register("/components/copybutton", &CopyButtonScreen{}, nil)
	site.Register("/components/shortcuthint", &ShortcutHintScreen{}, nil)
	site.Register("/components/segmented", &SegmentedScreen{}, nil)
	site.Register("/components/confirmaction", &ConfirmActionScreen{}, nil)
	site.Register("/components/filterchipbar", &FilterChipBarScreen{}, nil)
	site.Register("/components/infinitescroll", &InfiniteScrollScreen{}, nil)
	site.Register("/components/combobox", &ComboboxScreen{}, nil)
	site.Register("/components/tree", &TreeScreen{}, nil)
	site.Register("/components/commandpalette", &CommandPaletteScreen{}, nil)
	site.Register("/components/banner", &BannerScreen{}, nil)
	site.Register("/components/timeline", &TimelineScreen{}, nil)
	site.Register("/components/steps", &StepsScreen{}, nil)
	site.Register("/components/rating", &RatingScreen{}, nil)
	site.Register("/components/colorpicker", &ColorPickerScreen{}, nil)
	site.Register("/components/slider", &SliderScreen{}, nil)
	site.Register("/components/numberinput", &NumberInputScreen{}, nil)
	site.Register("/components/textarea", &TextAreaScreen{}, nil)
	site.Register("/components/multiselect", &MultiSelectScreen{}, nil)
	site.Register("/components/dropzone", &DropzoneScreen{}, nil)
	site.Register("/components/container", &ContainerScreen{}, nil)
	site.Register("/components/disclosure", &DisclosureScreen{}, nil)
	site.Register("/components/timepicker", &TimePickerScreen{}, nil)
	site.Register("/components/rangeslider", &RangeSliderScreen{}, nil)
	site.Register("/components/taginput", &TagInputScreen{}, nil)
	site.Register("/components/toolbar", &ToolbarScreen{}, nil)
	site.Register("/components/sparkline", &SparklineScreen{}, nil)
	site.Register("/components/piechart", &PieChartScreen{}, nil)
	site.Register("/components/barchart", &BarChartScreen{}, nil)
	site.Register("/components/linechart", &LineChartScreen{}, nil)
	site.Register("/components/jsonviewer", &JSONViewerScreen{}, nil)
	site.Register("/components/diffviewer", &DiffViewerScreen{}, nil)
	site.Register("/components/markdown", &MarkdownScreen{}, nil)
	site.Register("/components/animatedcounter", &AnimatedCounterScreen{}, nil)
	site.Register("/components/toc", &TOCScreen{}, nil)
	site.Register("/components/lightbox", &LightboxScreen{}, nil)
	site.Register("/components/notificationbell", &NotificationBellScreen{}, nil)
	site.Register("/components/sortablelist", &SortableListScreen{}, nil)
	site.Register("/components/globalsearch", &GlobalSearchScreen{}, nil)
	site.Register("/components/bottomsheet", &BottomSheetScreen{}, nil)
	site.Register("/components/gallery", &GalleryScreen{}, nil)
	site.Register("/components/carousel", &CarouselScreen{}, nil)
	site.Register("/components/skiplink", &SkipLinkScreen{}, nil)
	site.Register("/components/themetoggle", &ThemeToggleScreen{}, nil)
	site.Register("/components/sticky", &StickyScreen{}, nil)
	site.Register("/components/backtotop", &BackToTopScreen{}, nil)
	site.Register("/components/select", &SelectScreen{}, nil)
	site.Register("/components/aspectratio", &AspectRatioScreen{}, nil)
	site.Register("/customers", &CustomersListScreen{}, nil)
	site.Register("/customers/new", &CustomersFormScreen{}, nil)
	site.Register("/customers/:id", &CustomersFormScreen{}, nil)
	site.Register("/framework-ui/", &FrameworkUIScreen{}, nil)
	site.Register("/framework-ui/datatable", &DataTableDemoScreen{}, nil)
	site.Register("/framework-ui/form", &FormDemoScreen{}, nil)
	site.Register("/framework-ui/theme", &ThemeSwapDemoScreen{}, nil)
	site.Register("/framework-ui/notification", &NotificationDemoScreen{}, nil)
	site.Register("/framework-ui/css-loading", &CSSLoadingDemoScreen{}, nil)
	site.Register("/framework-ui/themed", &ThemedDemoScreen{}, nil)
	site.Register("/about", &AboutScreen{}, nil)

	cssStr := createStyleSheet(*site.Theme)
	hostOpts := []uihost.Option{
		uihost.WithCustomCSS(cssStr),
		uihost.WithFavicon("/static/favicon.ico"),
		uihost.WithDescription("GoFastr demo website — SSR framework with islands, signals, and themes"),
		uihost.WithThemeColor("#f7f5ee"),
		uihost.WithOpenGraph(uihost.OG{
			Title: "GoFastr",
			URL:   "https://gofastr.dev",
			Type:  "website",
		}),
		uihost.WithTwitterCard(uihost.TwitterCard{
			Card:  "summary_large_image",
			Title: "GoFastr",
		}),
		uihost.WithCanonicalURL("https://gofastr.dev"),
		uihost.WithPreconnect("https://fonts.googleapis.com", "https://fonts.gstatic.com"),
	}
	if devMode() {
		hostOpts = append(hostOpts, uihost.WithExtraScripts("/__livereload.js"))
	}
	host := uihost.New(site, hostOpts...)

	fwApp := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "website"}))
	fwApp.Mount(host)

	// Island RPC endpoints — see the matching screen files for how the
	// demos wire IslandSignal + IslandEndpoint into the rendered HTML.
	fwApp.Router.Get("/islands/pagination-demo/page", http.HandlerFunc(PaginationIslandHandler))
	fwApp.Router.Get("/islands/datatable-demo/state", http.HandlerFunc(DataTableIslandHandler))
	fwApp.Router.Get("/islands/customers/state", http.HandlerFunc(CustomersIslandHandler))
	fwApp.Router.Post("/islands/customers/delete", http.HandlerFunc(CustomersDeleteHandler))
	fwApp.Router.Post("/customers/save", http.HandlerFunc(CustomersSaveHandler))
	fwApp.Router.Post("/islands/css-demo/reveal-card", http.HandlerFunc(CSSLoadingRevealCardHandler))
	fwApp.Router.Post("/islands/css-demo/reveal-palette", http.HandlerFunc(CSSLoadingRevealPaletteHandler))

	// /components/{modal,drawer,toast} demos — register hidden widgets
	// + a ToastBus + a tiny push endpoint that the live demo buttons hit.
	registerComponentDemos(fwApp)

	// /components/new — backing widgets + RPC handlers for the new-
	// components demo screen (ConfirmAction modal, CommandPalette,
	// combobox/tree/feed/filter handlers).
	registerNewComponentsDemos(fwApp)

	if devMode() {
		// Dev-only livereload — SSE-driven, not polling. The server
		// opens a long-lived EventSource that fires one "ready" event
		// with the build-id and then idles. The browser's EventSource
		// auto-reconnects when the connection drops (which happens on
		// every server restart), so the client uses the second
		// `onopen` (= reconnect after a drop) as the reload signal.
		// One persistent connection per page; no polling. The
		// connection is fine because cross-page nav is SPA-style and
		// doesn't open a new EventSource per route.
		// Gated by GOFASTR_DEV=1.
		buildID := strconv.FormatInt(time.Now().UnixNano(), 10)
		fwApp.Router.Get("/__livereload", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering
			fl, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "streaming unsupported", http.StatusInternalServerError)
				return
			}
			// One immediate "ready" event so the client's onopen
			// fires consistently. After that, idle until the request
			// context cancels (server shutdown / client disconnect).
			fmt.Fprintf(w, "event: ready\ndata: %s\n\n", buildID)
			fl.Flush()
			// Heartbeat every 25s so intermediaries don't time the
			// connection out. SSE comments are ignored by the client.
			ticker := time.NewTicker(25 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-r.Context().Done():
					return
				case <-ticker.C:
					fmt.Fprintf(w, ": ping\n\n")
					fl.Flush()
				}
			}
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

// livereloadJS — SSE-based change detection.
//
// The browser opens a single EventSource to /__livereload. The server
// fires one "ready" event on connect and then idles. EventSource
// transparently reconnects when the connection drops (server restart),
// so the second `onopen` is the reload signal. No polling, one
// persistent connection, near-zero idle traffic.
const livereloadJS = `(() => {
  let everConnected = false;
  const connect = () => {
    const es = new EventSource('/__livereload');
    es.addEventListener('open', () => {
      if (everConnected) {
        // Reconnect after a drop = server restarted with new code.
        location.reload();
        return;
      }
      everConnected = true;
    });
    es.addEventListener('error', () => {
      // EventSource auto-retries; if it ever closes (readyState=2)
      // we'd reconnect ourselves, but typically retry is automatic.
    });
  };
  connect();
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
