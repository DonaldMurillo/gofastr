package uihost

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestSweepCorePhraseAroundValueCannotPoseAsPlaceholder(t *testing.T) {
	got := string(phraseAround(context.Background(), pageMethodBody, "path",
		render.HTML("<code>/x</code>"), map[string]any{"method": "{{path}}"}))
	if !strings.Contains(got, "<code>/x</code>.") || strings.Index(got, "<code>") < strings.Index(got, "allowed") {
		t.Fatalf("a param value holding the node's placeholder moved the node: %s", got)
	}
}

func TestSweepCorePutBuilderDropsLargeBuilders(t *testing.T) {
	big := new(strings.Builder)
	big.Grow(128 * 1024)
	PutBuilder(big)
	for range 4 {
		if GetBuilder() == big {
			t.Fatal("a builder over 64 KiB went back into the pool")
		}
	}
}

func TestSweepCoreGetBuilderIsEmpty(t *testing.T) {
	for range 4 {
		b := GetBuilder()
		b.WriteString("stale")
		PutBuilder(b)
		if got := GetBuilder(); got.Len() != 0 {
			t.Fatalf("GetBuilder returned a builder holding %q", got.String())
		}
	}
}

func TestSweepCoreRoutePatternsSkipRedirects(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/items/{id}", &slugScreen{}, nil)
	a.Redirect("/old", "/")
	got := New(a).RoutePatterns()
	if slices.Contains(got, "/old") {
		t.Errorf("RoutePatterns names a redirect: %v", got)
	}
	if !slices.Contains(got, "/") || len(got) != 2 {
		t.Errorf("RoutePatterns = %v, want the two screens", got)
	}
}

func TestSweepCoreAuthenticatedCheckRefusesGrant(t *testing.T) {
	widget.SetAuthenticatedCheck(nil)
	t.Cleanup(func() { widget.SetAuthenticatedCheck(nil) })
	a := app.NewApp("x")
	a.Register("/", &testHomeComp{}, nil)
	New(a).Mount(router.New())
	check := widget.AuthenticatedCheck()

	req := httptest.NewRequest(http.MethodGet, "/core-ui/widget/w/state", nil)
	req = req.WithContext(handler.SetUser(req.Context(), r4GateUser{}))
	if !check(req) {
		t.Fatal("a signed-in caller was refused")
	}
	if anon := httptest.NewRequest(http.MethodGet, "/core-ui/widget/w/state", nil); check(anon) {
		t.Error("a caller with no user passed the authenticated gate")
	}
	req.Header.Set(embedGrantHeader, "emg_any")
	if check(req) {
		t.Error("a request carrying an embed grant passed the authenticated gate")
	}
}

func TestSweepCoreInfrastructurePostRoutesRefuseGET(t *testing.T) {
	a := app.NewApp("x")
	a.SetDefaultLayout(bareLayout("main"))
	a.Register("/", &testHomeComp{}, nil)
	r := router.New()
	New(a).Mount(r)
	for _, p := range []string{"/__gofastr/session", "/__gofastr/action"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" {
			t.Errorf("GET %s = %d Allow=%q, want 405 Allow=POST", p, rec.Code, rec.Header().Get("Allow"))
		}
	}
}

func TestSweepCoreStaticRenderRefusesNonPages(t *testing.T) {
	a := outlet404App()
	a.RegisterScreen(app.NewScreen("/to-login", &testHomeComp{}).WithPolicy(app.PolicyFunc(func(context.Context) app.Decision {
		return decide.Redirect("/login")
	})), nil)
	a.RegisterScreen(app.NewScreen("/forbidden", &testHomeComp{}).WithPolicy(app.PolicyFunc(func(context.Context) app.Decision {
		return decide.Block(http.StatusForbidden)
	})), nil)
	ds := New(a)

	var blocked *PolicyBlockedError
	for _, p := range []string{"/to-login", "/forbidden"} {
		if _, err := ds.RenderStaticPage(context.Background(), p); !errors.As(err, &blocked) {
			t.Errorf("RenderStaticPage(%s) err = %v, want *PolicyBlockedError", p, err)
		}
	}
	var outlet *NotFoundOutletError
	if _, err := ds.RenderStaticPage(context.Background(), "/missing"); !errors.As(err, &outlet) {
		t.Errorf("a 404-outlet page err = %v, want *NotFoundOutletError", err)
	}
	var status *StatusPageError
	b := app.NewApp("y")
	b.Register("/record", drawsMissing{}, nil)
	if _, err := New(b).RenderStaticPage(context.Background(), "/record"); !errors.As(err, &status) || status.Status != http.StatusNotFound {
		t.Errorf("a page that set 404 err = %v, want *StatusPageError 404", err)
	}
	if _, err := ds.RenderStaticPage(context.Background(), "/present"); err != nil {
		t.Errorf("a plain page: %v", err)
	}
}

func TestSweepCoreStaticNotFoundPageNeedsRootLayout(t *testing.T) {
	bare := app.NewApp("x")
	bare.Register("/", &testHomeComp{}, nil)
	if _, ok := New(bare).RenderStaticNotFoundPage(context.Background(), "/404.html"); ok {
		t.Error("an app with no root layout exported a 404 page")
	}
	a := app.NewApp("x")
	a.SetDefaultLayout(bareLayout("main"))
	a.Register("/", &testHomeComp{}, nil)
	page, ok := New(a).RenderStaticNotFoundPage(context.Background(), "/404.html")
	if !ok || !strings.Contains(page, "This page does not exist.") || strings.Contains(page, "gofastr-sse") {
		t.Errorf("static 404 page ok=%v, want the path-less body and no live channel:\n%s", ok, page)
	}
}

func TestSweepCoreHasScreen(t *testing.T) {
	if _, known := (&UIHost{}).HasScreen("/x"); known {
		t.Error("a host with no app claims to know its screens")
	}
	a := app.NewApp("x")
	a.Register("/orders/{id}", &slugScreen{}, nil)
	ds := New(a)
	if has, known := ds.HasScreen("/orders/42"); !has || !known {
		t.Errorf("HasScreen(/orders/42) = %v, %v; want true, true", has, known)
	}
	if has, known := ds.HasScreen("/issues/42"); has || !known {
		t.Errorf("HasScreen(/issues/42) = %v, %v; want false, true", has, known)
	}
}

func TestSweepCoreFromPathKeepsQueryAsSent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Gofastr-From", "/docs/a%20b?q=%41")
	if got := fromPath(req); got != "/docs/a b?q=%41" {
		t.Errorf("fromPath = %q, want the path in routing form and the query untouched", got)
	}
}

func sweepServe(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestSweepCoreSocialHeadURLsAllowListed(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &testHomeComp{}, nil)
	ds := New(a,
		WithOpenGraph(OG{Title: "t", Image: "javascript:sweep-og-image", URL: "data:text/html,sweep-og-url"}),
		WithTwitterCard(TwitterCard{Card: "summary", Image: "javascript:sweep-tw-image"}),
		WithCanonicalURL("javascript:sweep-canonical"))
	body := sweepServe(ds, http.MethodGet, "/").Body.String()
	for _, marker := range []string{"sweep-og-image", "sweep-og-url", "sweep-tw-image", "sweep-canonical"} {
		if strings.Contains(body, marker) {
			t.Errorf("an unsafe head URL (%s) reached the page", marker)
		}
	}
	if !strings.Contains(body, `property="og:title"`) {
		t.Error("the safe og:title was dropped with the unsafe URLs")
	}
}

func TestSweepCoreRuntimeJSImmutableAtItsHash(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &testHomeComp{}, nil)
	ds := New(a)
	rec := sweepServe(ds, http.MethodGet, "/__gofastr/runtime.js?v="+widget.RuntimeHash())
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("runtime.js at its hash: Cache-Control %q, want immutable", cc)
	}
	if cc := sweepServe(ds, http.MethodGet, "/__gofastr/runtime.js?v=stale").Header().Get("Cache-Control"); strings.Contains(cc, "immutable") {
		t.Errorf("runtime.js at a stale hash is immutable: %q", cc)
	}
}

func TestSweepCoreMountRefusesFillOutsideTheChain(t *testing.T) {
	stray := app.NewOutlet("sweep-stray", app.OutletOptions{})
	_ = app.NewLayout("sweep-other", app.LayoutSpec{Outlets: []*app.Outlet{stray}}, func(_ context.Context, l *app.LayoutTree) render.HTML {
		return l.Primary()
	})
	a := app.NewApp("x")
	a.SetDefaultLayout(bareLayout("main"))
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).Fill(stray, statusFillComp("x")), nil)
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "sweep-stray") {
			t.Fatalf("Mount with a fill outside the screen's layout chain: recover() = %v, want a panic naming the outlet", r)
		}
	}()
	New(a).Mount(router.New())
}

func TestSweepCoreWidgetCatalogWithheldWithoutSession(t *testing.T) {
	widget.Mount(router.New(), &widget.Definition{Name: "sweepcore-catalog"})
	a := app.NewApp("x")
	a.Register("/", &testHomeComp{}, nil)
	rec := sweepServe(New(a), http.MethodGet, "/__gofastr/widgets?page=/")
	if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "sweepcore-catalog") {
		t.Errorf("no-session catalog = %d %q, want 401 naming no widget", rec.Code, rec.Body.String())
	}
}

func TestSweepCoreNoLiveChannelOmitsTheSSEMeta(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &testHomeComp{}, nil)
	if body := sweepServe(New(a), http.MethodGet, "/").Body.String(); !strings.Contains(body, `name="gofastr-sse"`) {
		t.Fatal("a default page carries no gofastr-sse meta")
	}
	if body := sweepServe(New(a, WithNoLiveChannel()), http.MethodGet, "/").Body.String(); strings.Contains(body, `name="gofastr-sse"`) {
		t.Error("WithNoLiveChannel still emits the gofastr-sse meta")
	}
}

func TestSweepCorePageLoadingRendersIntoThePage(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &testHomeComp{}, nil)
	body := sweepServe(New(a, WithPageLoading(statusFillComp("sweep-page-loading"))), http.MethodGet, "/").Body.String()
	if !strings.Contains(body, "data-fui-page-loading") || !strings.Contains(body, "sweep-page-loading") {
		t.Error("WithPageLoading's component is not in the page")
	}
}

func TestSweepCoreLLMMDOptOuts(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &testHomeComp{}, nil)
	hidden := app.NewScreen("/hidden", &testHomeComp{}).WithTitle("Hidden")
	hidden.NoLLMMD = true
	a.RegisterScreen(hidden, nil)
	things := app.NewScreen("/things/{slug}", &slugScreen{}).WithTitle("Thing")
	things.NoLLMMD = true
	a.RegisterScreen(things, nil)
	ds := New(a, WithPublicLLMMD())
	for _, p := range []string{"/hidden/llm.md", "/things/42/llm.md"} {
		if rec := sweepServe(ds, http.MethodGet, p); rec.Code == http.StatusOK {
			t.Errorf("%s of a NoLLMMD screen = 200:\n%s", p, rec.Body.String())
		}
	}
	if rec := sweepServe(ds, http.MethodGet, "/llm.md"); rec.Code != http.StatusOK {
		t.Errorf("/llm.md of a listed screen = %d", rec.Code)
	}

	off := app.NewApp("x")
	off.Register("/", &testHomeComp{}, nil)
	off.NoLLMMD = true
	ds = New(off, WithPublicLLMMD())
	for _, p := range []string{"/llm-pages.md", "/llm.md"} {
		if rec := sweepServe(ds, http.MethodGet, p); rec.Code == http.StatusOK {
			t.Errorf("%s with the app's NoLLMMD = 200", p)
		}
	}
}

func TestSweepCorePatternLLMMDWithheldByPolicy(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &testHomeComp{}, nil)
	a.RegisterScreen(app.NewScreen("/vault/{slug}", &slugScreen{}).WithTitle("sweep-vault-title").
		WithPolicy(app.PolicyFunc(func(context.Context) app.Decision { return decide.Block(http.StatusForbidden) })), nil)
	body := sweepServe(New(a, WithPublicLLMMD()), http.MethodGet, "/vault/:slug/llm.md").Body.String()
	if strings.Contains(body, "sweep-vault-title") || !strings.Contains(body, "Content withheld") {
		t.Errorf("a refused pattern doc carries the screen's metadata:\n%s", body)
	}
}

func TestSweepCoreStaticDirDirectoryFallsThroughToScreen(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := app.NewApp("x")
	a.Register("/docs", &testHomeComp{}, nil)
	rec := sweepServe(New(a, WithStaticDir(dir)), http.MethodGet, "/docs")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Home Page") {
		t.Errorf("GET /docs with a docs/ directory in the static dir = %d, want the screen", rec.Code)
	}
}

func TestSweepCoreTrailingSlashLocationIsEscaped(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/a b/", &testHomeComp{}, nil)
	rec := sweepServe(New(a), http.MethodGet, "/a%20b")
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusMovedPermanently || loc != "/a%20b/" {
		t.Errorf("trailing-slash redirect = %d %q, want 301 to the escaped slash form", rec.Code, loc)
	}
}

func TestSweepCoreStaticPageLinksEachComponentSheet(t *testing.T) {
	registry.IsolateForTest(t)
	ds := newTestUIHostForMany(registerTestStyle(t, "sweep-a"), registerTestStyle(t, "sweep-b"))
	page, err := ds.RenderStaticPage(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, "comp-bundle.css") {
		t.Errorf("a static page links the query-addressed bundle: %s", excerpt(page, "comp-bundle"))
	}
}
