package uihost

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
)

type sweepNavRouteFill struct{ component.ContextOnly }

func (sweepNavRouteFill) RenderCtx(ctx context.Context) render.HTML {
	return render.Text("[P:" + store.Route.Path.Value(ctx) + "]")
}

func TestSweepNavPartSeedOmitsRouteFamily(t *testing.T) {
	application := app.NewApp("Parts App")
	aside := app.NewOutlet("aside", app.OutletOptions{
		Deferred: true,
		Loading:  &app.Loading{Show: app.LoadingComponent(render.Text("LOADING"))},
	})
	shell := app.NewLayout("shell", app.LayoutSpec{Outlets: []*app.Outlet{aside}},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Primary(), l.Place(aside))
		})
	application.SetDefaultLayout(shell)
	application.RegisterScreen(app.NewScreen("/", &partsPage{html: "HOME"}).Fill(aside, sweepNavRouteFill{}), nil)
	srv := httptest.NewServer(New(application))
	t.Cleanup(srv.Close)
	client := partsClient(t)
	page, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	req.Header.Set("X-Gofastr-Part", "l:shell#aside")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), "[P:/]") {
		t.Fatalf("part body = %q, want the route-reading fill", body)
	}
	if strings.Contains(string(body), "route.path") {
		t.Errorf("part seed repeats the route family the page answer already sent: %q", body)
	}
}

func TestSweepNavDeferredPageCarriesLoading(t *testing.T) {
	ds := newPartsHost()
	req := httptest.NewRequest("GET", "/other", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	req.Header.Set("X-Gofastr-Fills", "2")
	req.Header.Set("X-Gofastr-From", "/")
	req.Header.Set("X-Gofastr-Defer", "1")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "LOADING") || strings.Contains(body, "[ASIDE-OTHER]") {
		t.Errorf("deferred page request: want the outlet's loading content in place of its fill, got %q", body)
	}
}

type sweepNavQueryScreen struct{ component.ContextOnly }

func (s *sweepNavQueryScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Text("q=" + app.QueryFromContext(ctx).Get("q"))
}

var sweepNavFillValue = store.New("sweepnav").String("fillval", "none")

type sweepNavSeedFill struct{ component.ContextOnly }

func (sweepNavSeedFill) RenderCtx(ctx context.Context) render.HTML {
	sweepNavFillValue.Seed(ctx, "FillSeeded")
	return sweepNavFillValue.Bind(ctx, "span", map[string]string{"id": "fv"})
}

func TestSweepNavEnvelopeSeedScansFills(t *testing.T) {
	application := app.NewApp("Seed App")
	aside := app.NewOutlet("aside", app.OutletOptions{})
	shell := app.NewLayout("shell", app.LayoutSpec{Outlets: []*app.Outlet{aside}},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Primary(), l.Place(aside))
		})
	application.SetDefaultLayout(shell)
	application.RegisterScreen(app.NewScreen("/", &partsPage{html: "HOME"}).Fill(aside, &partsFill{label: "A"}), nil)
	application.RegisterScreen(app.NewScreen("/other", &partsPage{html: "OTHER"}).Fill(aside, sweepNavSeedFill{}), nil)
	ds := New(application)
	req := httptest.NewRequest("GET", "/other", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	req.Header.Set("X-Gofastr-Fills", "2")
	req.Header.Set("X-Gofastr-From", "/")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Header().Get("X-Gofastr-Envelope") != "2" {
		t.Fatalf("setup: want the fills envelope, got %q", body)
	}
	i := strings.Index(body, `id="gofastr-signals-partial"`)
	if i < 0 || !strings.Contains(body[i:], "FillSeeded") || strings.Index(body[i:], "FillSeeded") > strings.Index(body[i:], "</script>") {
		t.Errorf("envelope seed misses a value only a fill seeded:\n%s", body)
	}
}

func TestSweepNavPartialScreenReadsQuery(t *testing.T) {
	application := app.NewApp("t")
	application.Register("/list", &sweepNavQueryScreen{}, nil)
	ds := New(application)
	req := httptest.NewRequest("GET", "/list?q=needle", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "q=needle") {
		t.Errorf("partial screen did not see the request query: %q", rec.Body.String())
	}
}

func TestSweepNavPartialCarriesRuntimeStrings(t *testing.T) {
	application := app.NewApp("t")
	application.Register("/plain", &testHomeComp{}, nil)
	ds := New(application)
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "runtime.load_failed", i18n.Message{Text: "Échec du chargement de {{path}}"})
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	ctx = i18n.WithTranslator(ctx, i18n.NewTranslator(cat, "en"))
	req := httptest.NewRequest("GET", "/plain", nil).WithContext(ctx)
	req.Header.Set("X-Gofastr-Navigate", "1")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "data-cui-strings") || !strings.Contains(rec.Body.String(), "chargement") {
		t.Errorf("a French partial must carry the runtime's French words: %q", rec.Body.String())
	}
}

func TestSweepNavOverlayCSSOnlyWithIntercept(t *testing.T) {
	ds := newTestUIHostWithMultipleRoutes()
	overlay := app.InterceptOverlayCSS()
	if strings.Contains(ds.AppCSSFor(theme.Default()), overlay) {
		t.Error("an app with no intercepting route ships the overlay CSS")
	}
	ds.App.RegisterScreen(app.NewScreen("/about/new", &testHomeComp{}), nil)
	scr, _ := ds.App.Router.ScreenByPattern("/about/new")
	scr.Intercept = &app.Intercept{From: "/about", As: app.ScreenDrawer}
	if !strings.Contains(ds.AppCSSFor(theme.Default()), overlay) {
		t.Error("an app with an intercepting route lacks the overlay CSS")
	}
}

func TestSweepNavMarkdownNotFoundNeedsNegotiation(t *testing.T) {
	ds := newISOHost(WithLLMsTxt("t", "s", nil))
	rec := doGet(ds, "/definitely-not-here", "text/markdown")
	if ct := rec.Header().Get("Content-Type"); strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("markdown 404 served without WithMarkdownNegotiation (Content-Type %q)", ct)
	}
}

func TestSweepNavBareNotFoundEscapesPath(t *testing.T) {
	ds := New(app.NewApp("x"))
	rec := httptest.NewRecorder()
	ds.serveNotFound(rec, httptest.NewRequest(http.MethodGet, "/nope", nil), "/<b>x</b>​")
	body := rec.Body.String()
	if strings.Contains(body, "<b>x") || strings.Contains(body, "​") {
		t.Errorf("bare 404 must escape the path and strip invisibles: %q", body)
	}
}

func TestSweepNavErrorPageVariesOnAccept(t *testing.T) {
	ds := New(app.NewApp("x"))
	rec := httptest.NewRecorder()
	ds.serveError(rec, httptest.NewRequest(http.MethodGet, "/boom", nil), "/boom")
	if rec.Code != http.StatusInternalServerError || !varyHasAccept(rec) {
		t.Errorf("HTML 500: status %d, Vary %q; want 500 with Vary: Accept", rec.Code, rec.Header().Values("Vary"))
	}
}

func TestSweepNavMethodNotAllowedProblemShape(t *testing.T) {
	f := newMethodFixture(t)
	w := f.do(t, http.MethodPost, "/counted", map[string]string{"Accept": "application/problem+json"})
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("405 problem document Cache-Control = %q, want no-store", cc)
	}
	if body := w.Body.String(); strings.Contains(body, "/counted") || strings.Contains(body, "POST") {
		t.Errorf("405 problem document echoes the path or method: %q", body)
	}
}

func TestSweepNavMethodNotAllowedPageInLayout(t *testing.T) {
	f := newMethodFixture(t)
	w := f.do(t, http.MethodPost, "/counted", nil)
	body := w.Body.String()
	if !strings.Contains(body, "<header") {
		t.Errorf("405 page is not wrapped in the root layout: %q", truncate(body, 400))
	}
	if !strings.Contains(body, "<code>GET, HEAD</code>") {
		t.Errorf("405 page does not name the allowed methods: %q", truncate(body, 400))
	}
}

func TestSweepNavMinimalPageEscapesLangAndTitle(t *testing.T) {
	var b strings.Builder
	writeMinimalPage(&b, `en"x`, "<t>", render.Text("body"))
	if strings.Contains(b.String(), `en"x`) || strings.Contains(b.String(), "<t>") {
		t.Errorf("minimal page must escape lang and title: %q", b.String())
	}
}

func TestSweepNavResolverFailureLoggedNotRendered(t *testing.T) {
	a := app.NewApp("resolver")
	failing := parityRow.From(func(context.Context) (string, error) { return "", errors.New("db down SECRET") })
	a.RegisterScreen(app.NewScreen("/rows/{id}", &swallowsMissing{}).Resolve(failing), nil)
	ds := New(a)
	logs := captureLogs(t)
	rec := httptest.NewRecorder()
	renderdiag.ProductionStatus(t, ds).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rows/%0aFORGED", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("resolver failure: status %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "SECRET") {
		t.Errorf("the error page carries the resolver's error text: %q", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "route resolver failed") || !strings.Contains(logs.String(), "SECRET") {
		t.Errorf("the resolver failure must be logged with its error:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "\nFORGED") {
		t.Errorf("the request path forged a log line:\n%s", logs.String())
	}
}

type sweepNavPanicParams struct{}

func (sweepNavPanicParams) SetParams(map[string]string) {}
func (sweepNavPanicParams) Render() render.HTML         { panic("boom") }

type sweepNavParams struct{}

func (sweepNavParams) SetParams(map[string]string) {}
func (sweepNavParams) Render() render.HTML         { return render.Text("user") }

func TestSweepNavPanicLogScrubsPath(t *testing.T) {
	a := app.NewApp("panic-path")
	a.SetDefaultLayout(bareLayout("ctl"))
	a.Register("/boom/{x}", sweepNavPanicParams{}, nil)
	ds := New(a)
	logs := captureLogs(t)
	rec := httptest.NewRecorder()
	renderdiag.ProductionStatus(t, ds).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom/%0aFORGED", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	if strings.Contains(logs.String(), "\nFORGED") {
		t.Errorf("the request path forged a log line:\n%s", logs.String())
	}
}

type sweepNavSubmitActs struct{}

func (s *sweepNavSubmitActs) Render() render.HTML { return render.Text("form") }

func (s *sweepNavSubmitActs) Actions() {
	component.On("submit", func(ctx *component.ComponentContext) { _ = ctx })
}

func TestSweepNavCompileActionsFirstWins(t *testing.T) {
	ds := New(app.NewApp("t"))
	first := ds.CompileActions("shared", &actScreen{})
	second := ds.CompileActions("shared", &sweepNavSubmitActs{})
	if second != first || strings.Contains(ds.GetActionJS(), `"submit"`) {
		t.Errorf("a second CompileActions under one id replaced the first:\n%s", ds.GetActionJS())
	}
}

func TestSweepNavServerActionIsPostOnly(t *testing.T) {
	ds := actionsHost()
	rec := httptest.NewRecorder()
	ds.handleServerAction(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/action", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET to the action handler: status %d, want 405", rec.Code)
	}
	rec = httptest.NewRecorder()
	methodNotAllowed(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/action", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" {
		t.Errorf("POST-only sibling: status %d, Allow %q; want 405 and POST", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestSweepNavServerActionRefusesDeclaredOversize(t *testing.T) {
	ds := actionsHost()
	req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", strings.NewReader(`{"action":"click","componentId":"act"}`))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = maxMutatingBodyBytes + 1
	rec := httptest.NewRecorder()
	ds.handleServerAction(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("declared oversize action body: status %d, want 413", rec.Code)
	}
}

func TestSweepNavServerActionHandsParamsNoStore(t *testing.T) {
	var got map[string]string
	ic := &actionTestComp{html: "<p>p</p>", actions: func() {
		component.On("mutate", func(cc *component.ComponentContext) { got = cc.Params })
	}}
	a := app.NewApp("action-params")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)
	ds.CompileActions("test-comp", ic)
	mint := httptest.NewRecorder()
	ds.ServeHTTP(mint, httptest.NewRequest(http.MethodPost, "/__gofastr/session", nil))
	req := httptest.NewRequest(http.MethodPost, "/__gofastr/action",
		strings.NewReader(`{"action":"mutate","params":{"id":"42"},"componentId":"test-comp"}`))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range mint.Result().Cookies() {
		req.AddCookie(c)
	}
	cat := i18n.NewMapCatalog()
	cat.Set("fr", reasonActionProcessed.Key(), i18n.Message{Text: "Action traitée"})
	ctx := i18n.WithContext(req.Context(), i18n.Locale{Tag: "fr"})
	req = req.WithContext(i18n.WithTranslator(ctx, i18n.NewTranslator(cat, "en")))
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || got["id"] != "42" {
		t.Fatalf("action: status %d, handler params %v; want 200 and id=42", rec.Code, got)
	}
	if !strings.Contains(rec.Body.String(), "Action traitée") {
		t.Errorf("action message is not in the caller's locale: %q", rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("action result Cache-Control = %q, want no-store", cc)
	}
}

func TestSweepNavActionJSQuotesComponentID(t *testing.T) {
	js := actionsToJS(`a"b`, component.ExtractActions(&actScreen{}))
	if !strings.Contains(js, `const id = "a\"b";`) {
		t.Errorf("component id is not a quoted JS string:\n%s", js)
	}
}

func TestSweepNavActionOutputSortedByID(t *testing.T) {
	ds := New(app.NewApp("t"))
	ids := []string{"h", "c", "f", "a", "g", "b", "e", "d"}
	page := ""
	for _, id := range ids {
		ds.CompileActions(id, &actScreen{})
		page += `<div data-component="` + id + `"></div>`
	}
	tags := ds.actionScriptTags(page)
	js := ds.GetActionJS()
	last := -1
	for i, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		if !strings.Contains(tags[i], "/__gofastr/widget/"+id+".js") {
			t.Errorf("script tag %d = %q, want id %q", i, tags[i], id)
		}
		at := strings.Index(js, "// Component: "+id+"\n")
		if at < last {
			t.Errorf("GetActionJS emits %q out of id order", id)
		}
		last = at
	}
}

func TestSweepNavPartialNotesRouteForTracing(t *testing.T) {
	prev := otel.GetTracerProvider()
	spans := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	application := app.NewApp("t")
	application.Register("/users/{id}", sweepNavParams{}, nil)
	h := middleware.Tracing()(New(application))
	req := httptest.NewRequest("GET", "/users/42", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	h.ServeHTTP(httptest.NewRecorder(), req)
	ended := spans.Ended()
	if len(ended) == 0 {
		t.Fatal("no span ended")
	}
	if got := ended[len(ended)-1].Name(); got != "HTTP GET /users/:id" {
		t.Errorf("partial navigation span = %q, want the screen route", got)
	}
}
