package uihost

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

type adminComp struct{}

func (adminComp) Render() render.HTML { return "<p>admin secret</p>" }
func (adminComp) ScreenSEO(context.Context) SEO {
	return SEO{Description: "Admin console", Canonical: "/nfd-admin"}
}

var notFoundNoise = regexp.MustCompile(`session=[A-Za-z0-9_-]+|sess-[A-Za-z0-9_-]+`)

// The paths are unique to these tests: widgets mount on a global
// registry by path pattern, and another test's "/admin" widget would
// render on both 404s for that path.

// normalizeNF hides what legitimately differs between two 404s for
// two paths: the path itself and the per-response session id.
func normalizeNF(body, path string) string {
	body = strings.ReplaceAll(body, path, "{path}")
	return notFoundNoise.ReplaceAllString(body, "{session}")
}

func notFoundDecisionApp(t *testing.T) *UIHost {
	t.Helper()
	a := app.NewApp("nfdecide")
	a.SetDefaultLayout(app.NewLayout("root", app.LayoutSpec{}, func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }))
	hidden := app.PolicyFunc(func(context.Context) app.Decision { return decide.NotFound() })
	a.RegisterScreen(app.NewScreen("/nfd-admin", adminComp{}).WithTitle("Admin").WithPolicy(hidden), nil)
	a.Register("/origin", statusScreenComp("origin"), nil)
	return New(a, WithNotFoundScreen(nfMetaBody{}), WithDescription("Site wide"),
		WithPublicLLMMD(), WithMarkdownNegotiation())
}

type nfShape struct {
	name   string
	header map[string]string
	suffix string
}

var nfShapes = []nfShape{
	{name: "page"},
	{name: "partial", header: map[string]string{"X-Gofastr-Navigate": "1", "X-Gofastr-From": "/origin"}},
	{name: "problem", header: map[string]string{"Accept": "application/problem+json"}},
	{name: "markdown", header: map[string]string{"Accept": "text/markdown"}},
	{name: "llm.md", suffix: "/llm.md"},
}

func serveShape(h http.Handler, path string, s nfShape) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path+s.suffix, nil)
	for k, v := range s.header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func assertSameNotFound(t *testing.T, shape string, got, want *httptest.ResponseRecorder, gotPath, wantPath string) {
	t.Helper()
	if got.Code != http.StatusNotFound || got.Code != want.Code {
		t.Fatalf("%s: status %d, unmatched URL %d; want both 404", shape, got.Code, want.Code)
	}
	if g, w := got.Header().Get("Content-Type"), want.Header().Get("Content-Type"); g != w {
		t.Errorf("%s: Content-Type %q, unmatched URL %q", shape, g, w)
	}
	if g, w := normalizeNF(got.Body.String(), gotPath), normalizeNF(want.Body.String(), wantPath); g != w {
		t.Errorf("%s: body differs from the unmatched URL's\n got: %s\nwant: %s", shape, g, w)
	}
	if strings.Contains(got.Body.String(), "admin secret") || strings.Contains(got.Body.String(), "Admin console") {
		t.Errorf("%s: 404 leaks the route's content", shape)
	}
}

// A policy answering decide.NotFound renders, for every response shape,
// what a URL no route matches renders (#824).
func TestDecideNotFoundMatchesUnmatchedURL(t *testing.T) {
	ds := notFoundDecisionApp(t)
	for _, s := range nfShapes {
		got := serveShape(ds, "/nfd-admin", s)
		want := serveShape(ds, "/zz-nope", s)
		assertSameNotFound(t, s.name, got, want, "/nfd-admin", "/zz-nope")
	}
}

// Middleware that calls ServeNotFound answers what an unmatched URL gets,
// for every response shape.
func TestServeNotFoundMatchesUnmatchedURL(t *testing.T) {
	a := app.NewApp("nfserve")
	a.SetDefaultLayout(app.NewLayout("root", app.LayoutSpec{}, func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }))
	a.Register("/nfd-admin", adminComp{}, nil)
	a.Register("/origin", statusScreenComp("origin"), nil)
	ds := New(a, WithNotFoundScreen(nfMetaBody{}), WithDescription("Site wide"),
		WithPublicLLMMD(), WithMarkdownNegotiation())
	rt := router.New()
	rt.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/nfd-admin") {
				ds.ServeNotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	ds.Mount(rt)
	for _, s := range nfShapes {
		got := serveShape(rt, "/nfd-admin", s)
		want := serveShape(rt, "/zz-nope", s)
		assertSameNotFound(t, s.name, got, want, "/nfd-admin", "/zz-nope")
	}
}

// A region guard answering NotFound takes the whole page to 404, as a
// guard Redirect moves the whole page; it never renders the rest.
func TestRegionGuardNotFoundIsWholePage(t *testing.T) {
	a := app.NewApp("nfguard")
	toc := app.NewOutlet("toc", app.OutletOptions{Policy: app.PolicyFunc(func(context.Context) app.Decision { return decide.NotFound() })})
	a.SetDefaultLayout(app.NewLayout("shell", app.LayoutSpec{Outlets: []*app.Outlet{toc}},
		func(_ context.Context, l *app.LayoutTree) render.HTML { return render.Join(l.Place(toc), l.Primary()) }))
	a.RegisterScreen(app.NewScreen("/nfd-admin", adminComp{}).Fill(toc, statusFillComp("TOC")), nil)
	a.Register("/origin", statusScreenComp("origin"), nil)
	ds := New(a, WithNotFoundScreen(nfMetaBody{}))
	for _, path := range []string{"/nfd-admin", "/zz-nope"} {
		for _, s := range nfShapes[:2] {
			rec := serveShape(ds, path, s)
			body := rec.Body.String()
			if rec.Code != http.StatusNotFound || !strings.Contains(body, "themed-404") {
				t.Fatalf("%s %s: status %d, want the themed 404:\n%s", path, s.name, rec.Code, body)
			}
			if s.name == "page" && !strings.Contains(body, `data-cui-layout="shell"`) {
				t.Errorf("%s: the 404 lost the app layout (the bare fallback answered)", path)
			}
			if strings.Contains(body, "admin secret") || strings.Contains(body, "TOC") {
				t.Errorf("%s %s: 404 renders the guarded page", path, s.name)
			}
		}
	}
}

// A server action on a screen whose policy answers NotFound is refused
// with 404, page named or not, and never runs.
func TestDecideNotFoundRefusesActions(t *testing.T) {
	ran := 0
	comp := &actionTestComp{
		html:    "<p>secret</p>",
		actions: func() { component.On("purge", func(*component.ComponentContext) { ran++ }) },
	}
	hidden := app.PolicyFunc(func(context.Context) app.Decision { return decide.NotFound() })
	a := app.NewApp("nfaction")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	a.RegisterScreen(app.NewScreen("/secret", comp).WithTitle("Secret").WithPolicy(hidden), nil)
	ds := New(a)
	ds.AutoCompileActions()
	post := actionPoster(t, ds)
	for _, body := range []string{
		`{"action":"purge","params":{},"componentId":"secret"}`,
		`{"action":"purge","params":{},"componentId":"secret","page":"/secret"}`,
	} {
		if code := post(context.Background(), body); code != http.StatusNotFound || ran != 0 {
			t.Fatalf("%s: status %d, handler ran %d times; want 404, 0", body, code, ran)
		}
	}
}

// The error page renders through the same guarded layout: a guard's
// NotFound blocks its region there and the page keeps its layout.
func TestRegionGuardNotFoundOnErrorPage(t *testing.T) {
	a := app.NewApp("nfguarderr")
	toc := app.NewOutlet("toc", app.OutletOptions{Policy: app.PolicyFunc(func(context.Context) app.Decision { return decide.NotFound() })})
	a.SetDefaultLayout(app.NewLayout("shell", app.LayoutSpec{Outlets: []*app.Outlet{toc}},
		func(_ context.Context, l *app.LayoutTree) render.HTML { return render.Join(l.Place(toc), l.Primary()) }))
	key := app.NewKey[string]("nfguarderr-row")
	a.RegisterScreen(app.NewScreen("/boom", adminComp{}).
		Resolve(key.From(func(context.Context) (string, error) { return "", errors.New("db down") })).
		WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
			_, _ = key.Get(ctx)
			return app.Decision{Kind: app.DecisionAllow}
		})), nil)
	ds := New(a)
	rec := serveShape(ds, "/boom", nfShapes[0])
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `data-cui-layout="shell"`) {
		t.Fatalf("status %d, want the 500 page inside the layout:\n%s", rec.Code, rec.Body.String())
	}
}

// A static export skips a decide.NotFound route as it skips any page a
// policy refuses, instead of failing the build.
func TestStaticRenderSkipsDecideNotFound(t *testing.T) {
	ds := notFoundDecisionApp(t)
	_, err := ds.RenderStaticPage(context.Background(), "/nfd-admin")
	if blocked, ok := errors.AsType[*PolicyBlockedError](err); !ok || blocked.Decision != "not found" {
		t.Fatalf("RenderStaticPage err = %v, want *PolicyBlockedError not found", err)
	}
}
