package static

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// homeScreen renders a fixed HTML body.
type homeScreen struct{}

func (homeScreen) ScreenTitle() string            { return "Home" }
func (homeScreen) ScreenDescription() string      { return "" }
func (homeScreen) ScreenType() coreapp.ScreenType { return coreapp.ScreenPage }
func (homeScreen) Render() render.HTML {
	return html.Heading(html.HeadingConfig{Level: 1}, render.Text("Home"))
}

// loadingScreen sets a body string from Load(ctx) — ensures Load runs at SSG time.
type loadingScreen struct {
	Body string
}

func (l *loadingScreen) Load(ctx context.Context) error {
	l.Body = "loaded:" + ctx.Value(loadKey{}).(string)
	return nil
}
func (l *loadingScreen) ScreenTitle() string            { return "Loaded" }
func (l *loadingScreen) ScreenDescription() string      { return "" }
func (l *loadingScreen) ScreenType() coreapp.ScreenType { return coreapp.ScreenPage }
func (l *loadingScreen) Render() render.HTML            { return render.HTML("<p>" + l.Body + "</p>") }

type loadKey struct{}

// productScreen has a dynamic :slug param and supplies StaticPaths for SSG.
type productScreen struct {
	slug string
}

func (p *productScreen) SetParams(params map[string]string) { p.slug = params["slug"] }
func (p *productScreen) ScreenTitle() string                { return "Product " + p.slug }
func (p *productScreen) ScreenDescription() string          { return "" }
func (p *productScreen) ScreenType() coreapp.ScreenType     { return coreapp.ScreenPage }
func (p *productScreen) Render() render.HTML                { return render.HTML("<p>product-" + p.slug + "</p>") }
func (p *productScreen) StaticPaths(ctx context.Context) []map[string]string {
	return []map[string]string{
		{"slug": "alpha"},
		{"slug": "beta"},
	}
}

func TestBuildWritesIndexHTMLPerRoute(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/about", &homeScreen{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	res, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(res.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %v", res.Pages)
	}

	// Root → out/index.html ; /about → out/about/index.html.
	for _, rel := range []string{"index.html", "about/index.html"} {
		full := filepath.Join(out, rel)
		data, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
		if !strings.Contains(string(data), "<h1") {
			t.Errorf("%s does not contain rendered body", rel)
		}
		if strings.Contains(string(data), "gofastr-sse") {
			t.Errorf("%s should not include SSE meta tag — SSG pages are session-less", rel)
		}
		if !strings.Contains(string(data), `<script src="/__gofastr/runtime.js">`) {
			t.Errorf("%s missing runtime.js script tag", rel)
		}
	}
}

func TestBuildEmitsRuntimeJS(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "__gofastr", "runtime.js"))
	if err != nil {
		t.Fatalf("runtime.js missing: %v", err)
	}
	if len(data) == 0 {
		t.Error("runtime.js is empty")
	}
}

func TestBuildEmitsColorSchemeJS(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Every page loads /__gofastr/color-scheme.js synchronously at the top of
	// <head> to set data-color-scheme before first paint (FOUC prevention).
	// themeswitch.js early-returns when window.__gofastr_colorScheme is absent,
	// so a missing file silently kills the theme toggle on a static host.
	data, err := os.ReadFile(filepath.Join(out, "__gofastr", "color-scheme.js"))
	if err != nil {
		t.Fatalf("color-scheme.js missing: %v", err)
	}
	if len(data) == 0 {
		t.Error("color-scheme.js is empty")
	}
}

func TestBuildEmitsSplitRuntimeModules(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// runtime.js dynamically injects <script src="/__gofastr/runtime/<name>.js?v=<hash>">
	// for each split module (themeswitch, shortcut, copy, widgets, sse, …). A static host
	// ignores the ?v= query and resolves the file by path — so every module MUST exist on
	// disk, query-free, or the module load 404s and all client interactivity dies. This is
	// the regression the wget crawl had (it baked ?v= into the filename and 404'd).
	names := runtime.ModuleNames()
	if len(names) == 0 {
		t.Fatal("runtime.ModuleNames() returned no modules — test baseline assumption broken")
	}
	for _, name := range names {
		p := filepath.Join(out, "__gofastr", "runtime", name+".js")
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("split runtime module %q not dumped at %s: %v", name, p, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("split runtime module %q is empty", name)
		}
	}
}

func TestBuildPropagatesLoaderContext(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/p", &loadingScreen{}, nil)
	host := uihost.New(a)

	ctx := context.WithValue(context.Background(), loadKey{}, "marker")
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(ctx); err != nil {
		t.Fatalf("Build: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "p", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "loaded:marker") {
		t.Errorf("Load did not run with the caller's context: %s", data)
	}
}

func TestBuildExpandsDynamicRoutesViaStaticPaths(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/products/:slug", &productScreen{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	res, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(res.Pages) != 2 {
		t.Fatalf("expected 2 expanded pages, got %v", res.Pages)
	}
	for _, slug := range []string{"alpha", "beta"} {
		full := filepath.Join(out, "products", slug, "index.html")
		data, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("missing %s: %v", full, err)
		}
		if !strings.Contains(string(data), "product-"+slug) {
			t.Errorf("%s missing slug body: %s", full, data)
		}
	}
}

func TestBuildSkipsDynamicRoutesWithoutStaticPaths(t *testing.T) {
	// No StaticPaths method on this screen — should be skipped silently.
	type plainProduct struct{ productScreenWithoutPaths }
	a := coreapp.NewApp("SSGTest")
	a.Register("/items/:id", &plainProduct{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	res, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(res.Pages) != 0 {
		t.Errorf("expected 0 pages, got %v", res.Pages)
	}
}

type productScreenWithoutPaths struct{}

func (productScreenWithoutPaths) Render() render.HTML            { return render.HTML("ignored") }
func (productScreenWithoutPaths) ScreenTitle() string            { return "" }
func (productScreenWithoutPaths) ScreenDescription() string      { return "" }
func (productScreenWithoutPaths) ScreenType() coreapp.ScreenType { return coreapp.ScreenPage }

// ============================================================================
// SSG respects NoLLMMD opt-out
// ============================================================================

type noopScreen struct{}

func (noopScreen) Render() render.HTML            { return render.HTML("<p>noop</p>") }
func (noopScreen) ScreenTitle() string            { return "Noop" }
func (noopScreen) ScreenDescription() string      { return "" }
func (noopScreen) ScreenType() coreapp.ScreenType { return coreapp.ScreenPage }

func TestBuild_NoLLMMD_PerScreen(t *testing.T) {
	a := coreapp.NewApp("NoLLMMDTest")
	a.Register("/public", &noopScreen{}, nil)
	a.Register("/secret", &noopScreen{}, nil)

	// Opt out /secret
	screen, _, _ := a.Router.Resolve("/secret")
	screen.NoLLMMD = true

	host := uihost.New(a)
	out := t.TempDir()
	_, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// /public should have llm.md
	if _, err := os.ReadFile(filepath.Join(out, "public", "llm.md")); err != nil {
		t.Error("expected public/llm.md to exist")
	}
	// /secret should NOT have llm.md
	if _, err := os.ReadFile(filepath.Join(out, "secret", "llm.md")); err == nil {
		t.Error("expected secret/llm.md to NOT exist")
	}
}

// TestBuildExpandsDynamicRoutesForLLMMD asserts that per-page llm.md files
// for dynamic routes are written under the concrete expanded paths (matching
// the HTML SSG structure), not under literal ":param" directories. A literal
// ":slug" directory is invalid on Windows and never URL-reachable elsewhere.
func TestBuildExpandsDynamicRoutesForLLMMD(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/products/:slug", &productScreen{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// One llm.md per concrete slug, mirroring the index.html layout.
	for _, slug := range []string{"alpha", "beta"} {
		full := filepath.Join(out, "products", slug, "llm.md")
		if _, err := os.ReadFile(full); err != nil {
			t.Errorf("missing %s: %v", full, err)
		}
	}

	// The literal ":slug" path must not be written.
	if _, err := os.Stat(filepath.Join(out, "products", ":slug")); err == nil {
		t.Errorf("SSG wrote a literal :slug directory — dynamic routes must be expanded")
	}
}

func TestBuild_NoLLMMD_GlobalApp(t *testing.T) {
	a := coreapp.NewApp("NoLLMMDGlobalTest")
	a.NoLLMMD = true
	a.Register("/home", &noopScreen{}, nil)

	host := uihost.New(a)
	out := t.TempDir()
	_, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// No llm.md should exist for any page
	if _, err := os.ReadFile(filepath.Join(out, "home", "llm.md")); err == nil {
		t.Error("expected home/llm.md to NOT exist with global NoLLMMD")
	}
	// No llm-pages.md index
	if _, err := os.ReadFile(filepath.Join(out, "llm-pages.md")); err == nil {
		t.Error("expected llm-pages.md to NOT exist with global NoLLMMD")
	}
}

func TestBuildMarksPagesStaticAndInjectsNotice(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	rep, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(rep.Pages) == 0 {
		t.Fatal("no pages exported")
	}

	data, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	s := string(data)

	// 1. <html> stamped with the runtime's static-mode switch so
	//    server-backed dispatches (RPC, widget catalog, open) no-op
	//    instead of 404'ing against the serverless host.
	if !strings.Contains(s, "<html data-fui-static") {
		head := s
		if len(head) > 200 {
			head = head[:200]
		}
		t.Errorf("exported page must stamp <html data-fui-static; got head:\n%s", head)
	}
	// 2. The run-locally notice is injected (doctrine: one styling
	//    surface — framework/ui.Banner).
	if !strings.Contains(s, "Static preview") {
		t.Errorf("exported page must carry the run-locally notice")
	}
}

// TestRenderStaticPageHasNoStaticMarker pins that the marker is
// exporter-only: the host's own SSG-aware render (the input the Builder
// consumes) must NOT carry data-fui-static, so a live server using the
// same render path stays fully interactive.
func TestRenderStaticPageHasNoStaticMarker(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a)

	html, err := host.RenderStaticPage(context.Background(), "/")
	if err != nil {
		t.Fatalf("RenderStaticPage: %v", err)
	}
	if strings.Contains(html, "data-fui-static") {
		t.Error("RenderStaticPage must NOT carry the static marker (exporter-only)")
	}
}

func TestBuildBasePathRewritesURLs(t *testing.T) {
	// Screen carrying a mix of link/asset URLs to prove the rewrite
	// prefixes root-absolute internal URLs and leaves others alone.
	linkScreen := &renderScreen{Body: `<script src="/__gofastr/runtime.js"></script>` +
		`<a href="/about">about</a>` +
		`<a href="/">home</a>` +
		`<a href="https://example.com">ext</a>` +
		`<a href="//cdn.example.com/lib.js">protocol-relative</a>` +
		`<a href="#frag">frag</a>`}
	a := coreapp.NewApp("SSGTest")
	a.Register("/", linkScreen, nil)
	host := uihost.New(a)

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out, BasePath: "/gofastr"}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	s := string(data)

	// Prefixed: root-absolute assets and internal nav.
	for _, want := range []string{
		`src="/gofastr/__gofastr/runtime.js"`,
		`href="/gofastr/about"`,
		`href="/gofastr/"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("want %q in HTML (base rewrite); not found", want)
		}
	}
	// Untouched: external, protocol-relative, fragment.
	for _, want := range []string{
		`href="https://example.com"`,
		`href="//cdn.example.com/lib.js"`,
		`href="#frag"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("want %q in HTML UNCHANGED (base rewrite must skip it); not found", want)
		}
	}

	// runtime.js has the base baked into its constructed module URLs.
	rt, err := os.ReadFile(filepath.Join(out, "__gofastr", "runtime.js"))
	if err != nil {
		t.Fatalf("read runtime.js: %v", err)
	}
	if !strings.Contains(string(rt), "/gofastr/__gofastr/runtime/") {
		t.Error("emitted runtime.js must bake BasePath into the split-module URL")
	}
}

func TestRewriteBaseURLs_PrefixesCatalogJSON(t *testing.T) {
	b := &Builder{BasePath: "/gofastr"}
	// The component catalog seeds inline JSON with stylePath values the
	// runtime lazy-loads. These are quoted root-absolute URLs, not
	// attributes — the regex path misses them, so they need the quoted-
	// value prefix pass.
	in := `<script>{"ui-banner":{"stylePath":"/__gofastr/comp/ui-banner.css","version":"abc"}}</script>` +
		`<a href="/about">x</a>`
	out := b.rewriteBaseURLs(in)
	for _, want := range []string{
		`"stylePath":"/gofastr/__gofastr/comp/ui-banner.css"`,
		`href="/gofastr/about"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rewriteBaseURLs: want %q; got:\n%s", want, out)
		}
	}
	// Version field untouched (not a /__gofastr/ path).
	if !strings.Contains(out, `"version":"abc"`) {
		t.Errorf("rewriteBaseURLs must not touch non-path JSON values; got:\n%s", out)
	}
}

// data-fui-push-state carries the navigation target a combobox/palette option
// routes to on selection. On a subpath deploy these root-absolute paths must
// be base-prefixed just like href/src — otherwise selecting a command navigates
// to the apex path and 404s. External and protocol-relative values are left alone.
func TestRewriteBaseURLs_PrefixesPushState(t *testing.T) {
	b := &Builder{BasePath: "/gofastr"}
	in := `<li role="option" data-fui-push-state="/docs/">Docs</li>` +
		`<li role="option" data-fui-push-state="/">Home</li>` +
		`<li role="option" data-fui-push-state="/components/">Components</li>` +
		`<li role="option" data-fui-push-state="//cdn.example.com">proto-rel</li>` +
		`<li role="option" data-fui-push-state="https://example.com">ext</li>`
	out := b.rewriteBaseURLs(in)
	for _, want := range []string{
		`data-fui-push-state="/gofastr/docs/"`,
		`data-fui-push-state="/gofastr/"`,
		`data-fui-push-state="/gofastr/components/"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rewriteBaseURLs: want %q; got:\n%s", want, out)
		}
	}
	for _, want := range []string{
		`data-fui-push-state="//cdn.example.com"`,
		`data-fui-push-state="https://example.com"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rewriteBaseURLs must not touch external/proto-rel push-state; want %q unchanged; got:\n%s", want, out)
		}
	}
}

// renderScreen wraps a raw HTML body in a minimal Screen for tests.
type renderScreen struct{ Body string }

func (r *renderScreen) ScreenTitle() string          { return "Links" }
func (*renderScreen) ScreenDescription() string      { return "" }
func (*renderScreen) ScreenType() coreapp.ScreenType { return coreapp.ScreenPage }
func (r *renderScreen) Render() render.HTML          { return render.HTML(r.Body) }

// Hidden click-to-open widgets (command palette, section-menu drawers) are
// not SSR-inlined; the runtime fetches their chrome on open. The export must
// dump the catalog JSON + each widget's chrome HTML + CSS as query-free files
// so those overlays resolve against the static tree instead of 404'ing.
func TestBuildDumpsWidgetCatalogAndChrome(t *testing.T) {
	a := coreapp.NewApp("SSGWidgetTest")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a)
	def := widget.New("ssg-dump-test").
		Hidden().
		Slot("body", homeScreen{}).
		Build()
	widget.Mount(router.New(), &def)

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	cat, err := os.ReadFile(filepath.Join(out, "__gofastr", "widgets.json"))
	if err != nil {
		t.Fatalf("widgets.json missing: %v", err)
	}
	if !strings.Contains(string(cat), `"name":"ssg-dump-test"`) {
		t.Errorf("widgets.json missing the widget entry:\n%s", cat)
	}
	for _, rel := range []string{
		"core-ui/widget/ssg-dump-test/chrome",
		"core-ui/widget/ssg-dump-test/style.css",
	} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Errorf("missing widget asset %s: %v", rel, err)
		}
	}
}

type textScreen struct{ Text string }

func (s *textScreen) ScreenTitle() string            { return "T" }
func (s *textScreen) ScreenDescription() string      { return "" }
func (s *textScreen) ScreenType() coreapp.ScreenType { return coreapp.ScreenPage }
func (s *textScreen) Render() render.HTML {
	return html.Heading(html.HeadingConfig{Level: 1}, render.Text(s.Text))
}

// A static export is a closed page set: the emitted service worker must
// precache every exported page and asset so the installed PWA serves the
// whole site offline, and a content-only redeploy must change the worker
// bytes (or browsers would never rotate the stale cache).
func TestBuildStaticWorkerPrecachesExportedSite(t *testing.T) {
	build := func(text string) (Result, string) {
		a := coreapp.NewApp("SSGPWA")
		a.Register("/", &textScreen{Text: text}, nil)
		a.Register("/products/:slug", &productScreen{}, nil)
		host := uihost.New(a, uihost.WithPWA(uihost.PWAConfig{}))
		out := t.TempDir()
		res, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		sw, err := os.ReadFile(filepath.Join(out, "service-worker.js"))
		if err != nil {
			t.Fatalf("service-worker.js: %v", err)
		}
		return res, string(sw)
	}
	res, sw := build("one")
	if len(res.Pages) != 3 {
		t.Fatalf("expected 3 pages, got %v", res.Pages)
	}
	for _, p := range res.Pages {
		if !strings.Contains(sw, `"`+p+`"`) {
			t.Errorf("exported page %s missing from worker precache:\n%s", p, sw)
		}
	}
	for _, a := range []string{"/__gofastr/runtime.js", "/__gofastr/color-scheme.js"} {
		if !strings.Contains(sw, `"`+a+`"`) {
			t.Errorf("exported asset %s missing from worker precache", a)
		}
	}
	_, sw2 := build("two")
	if sw == sw2 {
		t.Error("content-only change must alter the worker bytes so the SW update cycle fires")
	}
}

// Review-driven builder contracts: widget assets and llm.md reach the
// precache, a reused OutDir stays deterministic, and a user-supplied
// manifest wins over the generated one.
func TestBuildStaticWorkerReviewContracts(t *testing.T) {
	a := coreapp.NewApp("SSGPWA2")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithPWA(uihost.PWAConfig{}))
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	sw1, err := os.ReadFile(filepath.Join(out, "service-worker.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"/llm.md"`, `"/llm-pages.md"`, `.css?v=`} {
		if !strings.Contains(string(sw1), want) {
			t.Errorf("worker precache missing %s", want)
		}
	}
	// Rebuild into the SAME OutDir with identical content: the worker must
	// be byte-identical (a hash that eats the previous build's PWA output
	// would rotate the cache on every no-op deploy).
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	sw2, err := os.ReadFile(filepath.Join(out, "service-worker.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sw1) != string(sw2) {
		t.Error("no-op rebuild into a reused OutDir must reproduce identical worker bytes")
	}
}

func TestBuildKeepsUserSuppliedManifest(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "manifest.webmanifest"), []byte(`{"name":"user-owned"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := coreapp.NewApp("SSGPWA3")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithPWA(uihost.PWAConfig{}), uihost.WithStaticDir(staticDir))
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(out, "manifest.webmanifest"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "user-owned") {
		t.Errorf("generated manifest clobbered the user-supplied one: %s", b)
	}
}
