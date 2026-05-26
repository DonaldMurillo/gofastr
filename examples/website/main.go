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

	"github.com/DonaldMurillo/gofastr/battery/log"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/config"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/static"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// WebsiteConfig binds the website's runtime env vars into a typed
// struct, replacing the previous ad-hoc os.Getenv calls. Loaded once
// in main; passed to setupServer.
//
// Dev-mode livereload is auto-wired by framework.NewApp / uihost.New
// when GOFASTR_DEV=1 (set by `gofastr dev`) — no example code needed.
// See framework/dev/livereload.go.
type WebsiteConfig struct {
	// Port the HTTP server listens on. Defaults to 8082.
	Port int `config:"PORT" default:"8082"`
}

// Addr returns the listen address in `:port` form.
func (c WebsiteConfig) Addr() string { return ":" + strconv.Itoa(c.Port) }

// loadWebsiteConfig binds the env into a WebsiteConfig. Exposed for
// tests; main calls it via config.MustLoad.
func loadWebsiteConfig(src config.Source) (WebsiteConfig, error) {
	var cfg WebsiteConfig
	if src == nil {
		if err := config.Load(&cfg); err != nil {
			return cfg, err
		}
	} else {
		if err := config.Load(&cfg, src); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

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

	var webCfg WebsiteConfig
	config.MustLoad(&webCfg)
	addr := webCfg.Addr()

	fwApp, _ := setupServer()

	fmt.Println("━─────────────────────────────────────────────")
	fmt.Println("  GoFastr Website")
	fmt.Println("  http://localhost" + addr)
	fmt.Println()
	fmt.Println("  Pages:  /  /docs/  /docs/:slug  /examples/  /components/  /framework-ui/  /about")
	fmt.Println("  LLM:    /llm.md  /llm-pages.md  /articles/llm.md")
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
	site.Register("/components/forms", &FormsDemoScreen{}, nil)
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
	site.Register("/components/icon", &IconScreen{}, nil)
	site.Register("/components/pollingindicator", &PollingIndicatorScreen{}, nil)
	site.Register("/components/nestedlist", &NestedListScreen{}, nil)
	site.Register("/components/seo", &SEODemoScreen{}, nil)
	site.Register("/components/scrollspy", &ScrollSpyScreen{}, nil)
	site.Register("/components/optimisticaction", &OptimisticActionScreen{}, nil)
	site.Register("/components/networkretrybanner", &NetworkRetryBannerScreen{}, nil)
	site.Register("/components/seo-bundle", &SEOBundleScreen{}, nil)
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
	site.Register("/framework-ui/image-pipeline", &ImagePipelineScreen{}, nil)
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
		uihost.WithHeadHTML(`<meta name="csrf-token" content="demo-csrf-token-abc123">`),
		uihost.WithPreconnect("https://fonts.googleapis.com", "https://fonts.gstatic.com"),
		uihost.WithSitemap(uihost.SitemapConfig{
			BaseURL:      "https://gofastr.dev",
			ExcludePaths: []string{"/__gofastr"},
		}),
		uihost.WithRobots(uihost.RobotsConfig{
			Disallow: []string{"/__gofastr/"},
		}),
	}
	host := uihost.New(site, hostOpts...)

	fwApp := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "website"}),
		// MCP introspection: expose app_routes / app_plugins /
		// app_batteries / app_config / app_readiness for an agent
		// debugging the running site. The .claude/skills/log-debug,
		// app-introspect, and gofastr-mcp-debug skills auto-load on
		// matching agent prompts and document the curl recipes.
		framework.WithMCPIntrospection(),
	)

	// battery/log: structured JSON server log. Writes to a per-app file
	// in the OS state dir (e.g. ~/.local/state/website/server.log),
	// installs panic-recovery + access-log middleware, and swaps the
	// App's logger so framework middleware (Logging, slowquery, etc.)
	// also flows through these sinks. Router late-binding means the
	// plugin's middleware wraps routes registered by Mount below too.
	//
	// EnableMCP adds an in-memory ring buffer + log_recent / log_filter
	// / log_metrics / log_set_level tools on /mcp so agents can debug
	// the running site live.
	fwApp.RegisterPlugin(log.New(log.Config{
		EnableMCP:   true,
		MCPRingSize: 2000,
		// AllowMCPMutation registers `log_set_level`. Safe here only
		// because this is a localhost demo with no auth on /mcp. A
		// production deploy with /mcp publicly reachable MUST leave
		// this false (or gate /mcp behind authentication first).
		AllowMCPMutation: true,
	}))

	// Mount the MCP JSON-RPC endpoint so the introspection + log tools
	// are reachable from outside the process. POST /mcp speaks JSON-RPC
	// 2.0; see core/mcp/transport.go for the wire format.
	fwApp.Router().Handle("POST", "/mcp", fwApp.MCP)

	fwApp.Mount(host)

	// Wire a sqlite-backed entity so CRUD + /llm.md entity docs are live.
	if err := setupDemoEntity(fwApp); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: demo entity setup failed (CRUD /llm.md unavailable): %v\n", err)
	}

	// Island RPC endpoints — see the matching screen files for how the
	// demos wire IslandSignal + IslandEndpoint into the rendered HTML.
	fwApp.Router().Get("/islands/pagination-demo/page", http.HandlerFunc(PaginationIslandHandler))
	fwApp.Router().Get("/islands/datatable-demo/state", http.HandlerFunc(DataTableIslandHandler))
	fwApp.Router().Get("/islands/customers/state", http.HandlerFunc(CustomersIslandHandler))
	fwApp.Router().Post("/islands/customers/delete", http.HandlerFunc(CustomersDeleteHandler))
	fwApp.Router().Post("/customers/save", http.HandlerFunc(CustomersSaveHandler))
	fwApp.Router().Post("/islands/css-demo/reveal-card", http.HandlerFunc(CSSLoadingRevealCardHandler))
	fwApp.Router().Post("/islands/css-demo/reveal-palette", http.HandlerFunc(CSSLoadingRevealPaletteHandler))

	// Forms demo: repeater island endpoint
	fwApp.Router().Get("/islands/forms/repeater", http.HandlerFunc(FormsRepeaterIslandHandler))

	// Forms demo: end-to-end wizard at /components/forms/wizard-demo.
	// Self-contained POST round-trip — used by the wizard E2E tests to
	// exercise Next → Back → Submit across three steps.
	fwApp.Router().Get(wizardDemoPath, http.HandlerFunc(WizardDemoHandler))
	fwApp.Router().Post(wizardDemoPath, http.HandlerFunc(WizardDemoHandler))

	// OptimisticAction demo endpoints: success (204) + failure (500) + slow (~400ms 204).
	fwApp.Router().Post("/demo/optimistic-success", http.HandlerFunc(OptimisticDemoSuccess))
	fwApp.Router().Delete("/demo/optimistic-success", http.HandlerFunc(OptimisticDemoSuccess))
	fwApp.Router().Post("/demo/optimistic-failure", http.HandlerFunc(OptimisticDemoFailure))
	fwApp.Router().Post("/demo/optimistic-slow", http.HandlerFunc(OptimisticDemoSlow))
	fwApp.Router().Post("/demo/csrf-record", http.HandlerFunc(OptimisticDemoCSRFRecord))
	fwApp.Router().Get("/demo/csrf-last", http.HandlerFunc(OptimisticDemoCSRFLast))

	// NetworkRetryBanner: health endpoint that returns 204.
	fwApp.Router().Get("/demo/network-health", http.HandlerFunc(NetworkHealthOK))
	fwApp.Router().Get("/demo/network-health-slow", http.HandlerFunc(NetworkHealthSlow))
	fwApp.Router().Get("/demo/network-health-stats", http.HandlerFunc(NetworkHealthStats))
	fwApp.Router().Post("/demo/network-health-stats-reset", http.HandlerFunc(NetworkHealthStatsReset))

	// /components/{modal,drawer,toast} demos — register hidden widgets
	// + a ToastBus + a tiny push endpoint that the live demo buttons hit.
	registerComponentDemos(fwApp)

	// /components/new — backing widgets + RPC handlers for the new-
	// components demo screen (ConfirmAction modal, CommandPalette,
	// combobox/tree/feed/filter handlers).
	registerNewComponentsDemos(fwApp)

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
	_ = builder.Watch(ctx, []string{".", "../../framework/docs/content"}, interval, func(err error) {
		fmt.Fprintf(os.Stderr, "  build error: %v\n", err)
	})
}
