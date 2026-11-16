package uihost

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
	"github.com/DonaldMurillo/gofastr/internal/retired"
	"github.com/DonaldMurillo/gofastr/internal/retired/names"
)

type sweepPageRetiredScreen struct{}

func (sweepPageRetiredScreen) Render() render.HTML {
	return render.HTML(`<div class="ui-sweep-page-gone">old</div>`)
}

func TestSweepPageStandaloneScansRetiredMarkup(t *testing.T) {
	note := &names.Note{Version: "v9.0.0", Change: "gone"}
	retired.UseForTest(t, retired.New([]names.Entry{{Kind: "class", Name: "ui-sweep-page-gone", Note: note}}))
	a := app.NewApp("x")
	a.Register("/", sweepPageRetiredScreen{}, nil)
	ds := New(a)
	var got []string
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(renderdiag.WithRetiredReporter(req.Context(), func(m string) { got = append(got, m) }))
	ds.ServeHTTP(httptest.NewRecorder(), req)
	if len(got) == 0 || !strings.Contains(got[0], "ui-sweep-page-gone") {
		t.Fatalf("standalone router did not scan the page for retired markup: %q", got)
	}
}

func TestSweepPageNamesTheTraceSpan(t *testing.T) {
	prev := otel.GetTracerProvider()
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	a := app.NewApp("x")
	a.Register("/thing/{id}", &describedScreen{}, nil)
	ds := New(a)
	middleware.Tracing()(ds).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/thing/7", nil))
	spans := rec.Ended()
	if len(spans) == 0 {
		t.Fatal("no span ended")
	}
	if got := spans[len(spans)-1].Name(); got != "HTTP GET /thing/:id" {
		t.Errorf("page span = %q, want it named for the screen route", got)
	}
}

type sweepPageLoadingBar struct{}

func (sweepPageLoadingBar) Render() render.HTML { return render.HTML(`<i>loading</i>`) }

func TestSweepPageLoadingRidesTheBody(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &describedScreen{}, nil)
	ds := New(a, WithPageLoading(sweepPageLoadingBar{}))
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), `<body><div data-fui-page-loading><i>loading</i></div>`) {
		t.Fatalf("page-loading component is not the body's first child:\n%s", rec.Body.String())
	}
}

type sweepPageCtxKey struct{}

type sweepPageCtxSlot struct{ component.ContextOnly }

func (sweepPageCtxSlot) RenderCtx(ctx context.Context) render.HTML {
	v, _ := ctx.Value(sweepPageCtxKey{}).(string)
	return render.HTML(`<span id="sweepctx">` + v + `</span>`)
}

func TestSweepPageWidgetChromeSeesTheRequest(t *testing.T) {
	widget.IsolateForTest(t)
	widget.MountBuilder(router.New(), widget.New("sweep-page-ctx").Slot("body", sweepPageCtxSlot{}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), sweepPageCtxKey{}, "alice"))
	if got := injectWidgetSSR(`<html><body></body></html>`, req); !strings.Contains(got, `<span id="sweepctx">alice</span>`) {
		t.Fatalf("inlined widget chrome did not render with the request context:\n%s", got)
	}
}

func TestSweepPageHeadCarriesToastStackSheets(t *testing.T) {
	widget.IsolateForTest(t)
	a := app.NewApp("x")
	a.Register("/", &describedScreen{}, nil)
	ds := New(a, WithNoInlineFirstPaint())
	routes := router.New()
	ds.Mount(routes)
	ds.ValidateBoot()
	d, ok := widget.Lookup(DefaultToastStack)
	if !ok {
		t.Fatal("no default toast stack")
	}
	live, held := registry.ScanSplit(widget.RenderChromeCtx(context.Background(), d))
	names := append(live, held...)
	if len(names) == 0 {
		t.Fatal("the toast stack names no component sheet")
	}
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	head := body[:strings.Index(body, "</head>")]
	for _, n := range names {
		if !strings.Contains(head, n) {
			t.Errorf("the head does not carry the toast stack's %s sheet", n)
		}
	}
}

type sweepPageDescribedHead struct{}

func (sweepPageDescribedHead) Render() render.HTML                          { return render.HTML("<p>x</p>") }
func (sweepPageDescribedHead) ScreenDescription(ctx context.Context) string { return "typed" }
func (sweepPageDescribedHead) HeadHTML() string {
	return `<meta name="description" content="from-head">`
}

func TestSweepPageTypedDescriptionBeatsHeadHTML(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", sweepPageDescribedHead{}, nil)
	rec := httptest.NewRecorder()
	New(a).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if n := strings.Count(body, `name="description"`); n != 1 || !strings.Contains(body, `content="typed"`) {
		t.Fatalf("want the typed description alone, got %d description tags:\n%s", n, body)
	}
}

func TestSweepPageHreflangRejectsUnsafeURL(t *testing.T) {
	page := renderBundleSEOPage(t, SEO{Hreflangs: []HreflangLink{{Lang: "fr", URL: "javascript:alert(1)"}}})
	if strings.Contains(page, "javascript:") {
		t.Fatalf("an unsafe hreflang URL reached the head:\n%s", page)
	}
}

func TestSweepPageHeadCarriesRuntimeStrings(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "runtime.notifications", i18n.Message{Text: "Avis"})
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	ctx = i18n.WithTranslator(ctx, i18n.NewTranslator(cat, "en"))
	got := injectHeadData(store.WithValues(ctx), "<html><head></head><body></body></html>")
	if !strings.Contains(got, `<script type="application/json" id="gofastr-strings">`) || !strings.Contains(got, `"runtime.notifications":"Avis"`) {
		t.Fatalf("the head carries no translated runtime strings:\n%s", got)
	}
}

func TestSweepPageStreamedSeedDropsRepeats(t *testing.T) {
	ctx := store.WithValues(context.Background())
	html := string(testCompany.Bind(ctx, "span", nil))
	sent := map[string]string{}
	if first := partialSeedIslandDelta(ctx, html, sent); !strings.Contains(first, "uihosttest.company") {
		t.Fatalf("first unit carries no seed: %q", first)
	}
	if again := partialSeedIslandDelta(ctx, html, sent); again != "" {
		t.Errorf("a repeated unchanged value was sent again: %q", again)
	}
	if whole := partialSeedIslandDelta(ctx, html, nil); whole == "" {
		t.Error("an unfiltered island dropped the seed")
	}
}

func sweepPageExportHost() *UIHost {
	a := app.NewApp("x")
	a.Register("/", &describedScreen{}, nil)
	return New(a)
}

func TestSweepPageExportHeadIsSelfContained(t *testing.T) {
	ds := sweepPageExportHost()
	ctx := context.Background()
	page := ds.injectChromeMode(ctx, ds.documentShell(ctx, "/", "t", "<p>x</p>"), "/", "", "", false)
	for _, want := range []string{
		`<head>`+colorSchemeMeta+`<script src="/__gofastr/color-scheme.js"></script>`,
		`<link rel="stylesheet" href="/__gofastr/app.css" id="cui-app-css">`,
		`id="gofastr-catalog"`,
		`id="gofastr-runtime-modules"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("export head lacks %s:\n%s", want, page)
		}
	}
	if err := ds.RegisterExternalScript("/late.js"); err == nil {
		t.Error("an export render did not close the script rail")
	}
}

func TestSweepPageColorSchemeOpensTheHead(t *testing.T) {
	for _, opts := range [][]Option{nil, {WithNoInlineFirstPaint()}} {
		a := app.NewApp("x")
		a.Register("/", &describedScreen{}, nil)
		rec := httptest.NewRecorder()
		New(a, opts...).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if !strings.Contains(rec.Body.String(), "<head>"+colorSchemeMeta+"<script") {
			t.Errorf("the static color-scheme meta and then the bootstrap do not open the head:\n%s", rec.Body.String())
		}
	}
}

func TestSweepPageShellEscapesTitle(t *testing.T) {
	got := sweepPageExportHost().documentShell(context.Background(), "", `a<b"`, "")
	if !strings.Contains(got, `<title>a&lt;b&#34;</title>`) {
		t.Fatalf("shell title not escaped: %s", got)
	}
}

func TestSweepPageMarksScreenHeadAsPageMeta(t *testing.T) {
	rec := httptest.NewRecorder()
	sweepPageExportHost().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), `<meta data-cui-page-meta="" name="description"`) {
		t.Fatalf("the screen's description is not marked page meta:\n%s", rec.Body.String())
	}
}

func TestSweepPageLoadsOnlyRenderedActionScripts(t *testing.T) {
	ds := newTestUIHost()
	ds.CompileActions("btn-1", &testClickButton{Label: "Click me"})
	shell := func(body string) string { return "<html><head></head><body>" + body + "</body></html>" }
	ctx := context.Background()
	if got := ds.injectChrome(ctx, shell(`<div data-component="btn-1"></div>`), "/", "", ""); !strings.Contains(got, `src="/__gofastr/widget/btn-1.js?v=`) {
		t.Errorf("a page rendering btn-1 does not load its action script:\n%s", got)
	}
	if got := ds.injectChrome(ctx, shell(`<p>none</p>`), "/", "", ""); strings.Contains(got, "/__gofastr/widget/btn-1.js") {
		t.Errorf("a page without btn-1 loads its action script:\n%s", got)
	}
}

func TestSweepPageHeadLinkNeedsSafeHref(t *testing.T) {
	page := renderHeadPage(t, WithHeadHTML(`<link rel="icon" href="javascript:alert(1)">`))
	if strings.Contains(page, "javascript:") {
		t.Fatalf("a head link with an unsafe href survived:\n%s", page)
	}
}

func TestSweepPagePruneKeepsNonASCIISelectors(t *testing.T) {
	css := ".fui-a\u00e9{color:red}"
	got, ok := pruneCSS(css, func(string) bool { return false })
	if !ok || got != css {
		t.Fatalf("pruneCSS(%q) = %q, %v; a selector the scanner cannot read must ship", css, got, ok)
	}
}

func TestSweepPageBundleCacheIsBounded(t *testing.T) {
	registry.IsolateForTest(t)
	ds := sweepPageExportHost()
	var names []string
	for i := range 9 {
		names = append(names, registerTestStyle(t, fmt.Sprintf("sweep-page-bound-%d", i)).Name())
	}
	for mask := 1; mask < 1<<len(names); mask++ {
		var set []string
		for i, n := range names {
			if mask&(1<<i) != 0 {
				set = append(set, n)
			}
		}
		ds.bundleBlockFor(set)
	}
	count := 0
	ds.firstPaint.bundles.Range(func(any, any) bool { count++; return true })
	if count > maxCachedBundles {
		t.Fatalf("%d bundles cached, bound is %d", count, maxCachedBundles)
	}
}

func TestSweepPageExtraScriptSrcsFollowsScope(t *testing.T) {
	ds := sweepPageExportHost()
	for _, err := range []error{
		ds.RegisterExternalScript("/b.js"),
		ds.RegisterDocumentScript("/a.js", func(p string) bool { return p == "/x" }),
		ds.RegisterExternalScript("/b.js"),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := ds.ExtraScriptSrcs([]string{"/y"}); !slices.Equal(got, []string{"/b.js"}) {
		t.Errorf("out of scope: %q", got)
	}
	if got := ds.ExtraScriptSrcs([]string{"/y", "/x"}); !slices.Equal(got, []string{"/a.js", "/b.js"}) {
		t.Errorf("in scope: %q, want sorted /a.js /b.js", got)
	}
}

func TestSweepPagePruneSkipsURLInPrelude(t *testing.T) {
	got, ok := pruneCSS(`@import url(a{b.css);.fui-x{color:red}`, func(string) bool { return false })
	if !ok || got != `@import url(a{b.css);` {
		t.Fatalf("pruneCSS = %q, %v; an unquoted url() in a prelude is one token", got, ok)
	}
}

func TestSweepPageSubsetLoadsFullBundle(t *testing.T) {
	if got := runtimeModulePreloadLinks(`<style data-cui-bundle-href="/x"></style>`); !strings.Contains(got, "fullbundle") {
		t.Fatalf("a subset page does not load the fullbundle behaviour: %q", got)
	}
}

func TestSweepPageCarryQueryMatchesDecodedKeys(t *testing.T) {
	if got := carryQuery("/p?a%20b=1", "a+b=2&c=3"); got != "/p?a%20b=1&c=3" {
		t.Fatalf("carryQuery = %q; the target's own key wins by its decoded name", got)
	}
}

func TestSweepPageRenderScreenWrapsTheDefaultLayout(t *testing.T) {
	a := app.NewApp("x")
	a.SetDefaultLayout(chromeTestLayout("main", &testHeaderComp{}, nil, nil))
	rec := httptest.NewRecorder()
	New(a).RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/gone", nil), &describedScreen{}, ScreenResponse{Status: http.StatusGone})
	if body := rec.Body.String(); !strings.Contains(body, "Test Site") || !strings.Contains(body, `class="layout-body"`) {
		t.Fatalf("the recovery screen is not in the default layout:\n%s", body)
	}
}

func TestSweepPageStaleReloadEscapesItsTargets(t *testing.T) {
	rec := httptest.NewRecorder()
	writeStaleMarkupReload(rec, httptest.NewRequest(http.MethodGet, "/k/a%20b", nil), "/k/a b", "k\x01\u00e9")
	if got := rec.Header().Get("X-Gofastr-Swap"); got != "k%01%C3%A9" {
		t.Errorf("swap key = %q, want it percent-encoded", got)
	}
	if !strings.Contains(rec.Body.String(), `url='/k/a%20b'`) {
		t.Errorf("refresh target is not the escaped route path:\n%s", rec.Body.String())
	}
}

type sweepPageTitled struct{}

func (sweepPageTitled) Render() render.HTML                    { return render.HTML("<p>x</p>") }
func (sweepPageTitled) ScreenTitle(ctx context.Context) string { return "a\u202eb" }

func TestSweepPageRenderScreenPartialTitleHasNoInvisibles(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/gone", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	rec := httptest.NewRecorder()
	sweepPageExportHost().RenderScreen(rec, req, sweepPageTitled{}, ScreenResponse{Status: http.StatusGone})
	if got := rec.Header().Get("X-Gofastr-Title"); got == "" || strings.Contains(got, "%E2%80%AE") {
		t.Fatalf("X-Gofastr-Title = %q, want the title without its bidi override", got)
	}
}

func TestSweepPageCatalogFreezesTheRegistry(t *testing.T) {
	registry.IsolateForTest(t)
	ds := sweepPageExportHost()
	registerTestStyle(t, "sweep-page-early")
	catalogJSON(ds, ds.activeTheme(), "")
	defer func() {
		if recover() == nil {
			t.Fatal("a style registered after the catalog was built was accepted")
		}
	}()
	registerTestStyle(t, "sweep-page-late")
}
