package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// parityRow is the record a lazy resolver looks up; the store never
// holds it.
var parityRow = app.NewKey[string]("parity-row")

// swallowsMissing reads the row in Load and drops the error, the way a
// screen treating the value as optional does. The page still answers
// 404: the resolver's cell decides the outcome, not the Load's return.
type swallowsMissing struct{}

func (*swallowsMissing) Load(ctx context.Context) error {
	_, _ = parityRow.Get(ctx)
	return nil
}
func (*swallowsMissing) Render() render.HTML         { return "<p>row body</p>" }
func (*swallowsMissing) SetParams(map[string]string) {}

// parityText renders its text, as a screen or an outlet fill.
type parityText string

func (p parityText) Render() render.HTML { return render.HTML("<p>" + string(p) + "</p>") }

// parityParamText is parityText on a dynamic route.
type parityParamText struct{ parityText }

func (*parityParamText) SetParams(map[string]string) {}

// assertDocsAnswer404 checks a path whose page answers 404: its llm.md
// and its negotiated markdown answer 404 too, without the screen's body.
func assertDocsAnswer404(t *testing.T, ds *UIHost, path, body string) {
	t.Helper()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("page %s = %d, want 404 (fixture)", path, rec.Code)
	}
	doc := strings.TrimSuffix(path, "/") + "/llm.md"
	rec = httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, doc, nil))
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), body) {
		t.Errorf("%s = %d, want 404 without %q:\n%s", doc, rec.Code, body, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", "text/markdown")
	rec = httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), body) {
		t.Errorf("%s as markdown = %d, want 404 without %q:\n%s", path, rec.Code, body, rec.Body.String())
	}
}

// A resolver failure Load swallowed still makes the page a 404, so the
// docs answer 404 too, on a static route and a dynamic one.
func TestLLMMDSwallowedResolver404(t *testing.T) {
	a := app.NewApp("parity")
	a.Register("/", &testHomeComp{}, nil)
	missing := parityRow.From(func(context.Context) (string, error) { return "", app.ErrNotFound })
	a.RegisterScreen(app.NewScreen("/row", &swallowsMissing{}).Resolve(missing), nil)
	a.RegisterScreen(app.NewScreen("/rows/{id}", &swallowsMissing{}).Resolve(missing), nil)
	ds := New(a, WithPublicLLMMD(), WithMarkdownNegotiation())

	assertDocsAnswer404(t, ds, "/row", "row body")
	assertDocsAnswer404(t, ds, "/rows/1", "row body")
}

// linksFromRequest writes a link off the request's path, the way
// entityui.List derives its record links.
type linksFromRequest struct{ component.ContextOnly }

func (linksFromRequest) RenderCtx(ctx context.Context) render.HTML {
	r := app.RequestFromContext(ctx)
	return render.HTML(`<a href="` + r.URL.Path + `/7?` + r.URL.RawQuery + `">row 7</a>`)
}

// parityDoc reads the catch-all remainder from the route match, the way
// examples/site resolves a docs article; only "foo" exists.
var parityDoc = app.NewKey[string]("parity-doc")

type docBySlug struct{}

func (*docBySlug) SetParams(map[string]string) {}
func (*docBySlug) Load(ctx context.Context) error {
	_, err := parityDoc.Get(ctx)
	return err
}
func (*docBySlug) Render() render.HTML { return "<p>article foo</p>" }

// A doc renders under its page's request and match, not the /llm.md
// URL's: links a screen builds off the request path point at the page,
// and a match the route middleware installed for the doc URL does not
// hand the page's resolvers "foo/llm.md".
func TestLLMMDRendersUnderPageURL(t *testing.T) {
	a := app.NewApp("parity")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/customers", linksFromRequest{}, nil)
	slug := parityDoc.From(func(ctx context.Context) (string, error) {
		m, _ := app.MatchFromContext(ctx)
		if m.Param("path") != "foo" {
			return "", app.ErrNotFound
		}
		return "foo", nil
	})
	a.RegisterScreen(app.NewScreen("/docs/{path...}", &docBySlug{}).Resolve(slug), nil)
	ds := New(a, WithPublicLLMMD())
	h := ds.RouteMatchMiddleware()(ds)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/customers/llm.md?sort=name", nil))
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, "/customers/7?sort=name") {
		t.Errorf("/customers/llm.md = %d, want the link off /customers with its query:\n%s", rec.Code, body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/foo/llm.md", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "article foo") {
		t.Errorf("/docs/foo/llm.md = %d, want the article's doc:\n%s", rec.Code, rec.Body.String())
	}
}

// showsPath renders the request path the screen sees.
type showsPath struct{ component.ContextOnly }

func (showsPath) RenderCtx(ctx context.Context) render.HTML {
	return render.HTML("<p>path=" + app.RequestFromContext(ctx).URL.Path + "</p>")
}

// A screen registered only at its trailing-slash URL sees that URL in
// its doc too, as its page does.
func TestLLMMDKeepsTrailingSlash(t *testing.T) {
	a := app.NewApp("parity")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/docs/", showsPath{}, nil)
	ds := New(a, WithPublicLLMMD())

	page := httptest.NewRecorder()
	ds.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/docs/", nil))
	doc := httptest.NewRecorder()
	ds.ServeHTTP(doc, httptest.NewRequest(http.MethodGet, "/docs/llm.md", nil))
	if !strings.Contains(page.Body.String(), "path=/docs/<") {
		t.Fatalf("page sees %q, want /docs/ (fixture)", page.Body.String())
	}
	if !strings.Contains(doc.Body.String(), "path=/docs/") || strings.Contains(doc.Body.String(), "path=/docs\n") {
		t.Errorf("/docs/llm.md = %d, want the page's path /docs/:\n%s", doc.Code, doc.Body.String())
	}
}

// showsEscapedPath renders the escaped request path, which keeps a
// key's encoded slash.
type showsEscapedPath struct{ component.ContextOnly }

func (showsEscapedPath) RenderCtx(ctx context.Context) render.HTML {
	return render.HTML("<p>esc=" + app.RequestFromContext(ctx).URL.EscapedPath() + "</p>")
}
func (*showsEscapedPath) SetParams(map[string]string) {}

// A record key holding "/" keeps its encoded slash in the doc's request,
// however the /llm.md suffix itself was spelled.
func TestLLMMDKeepsEncodedKey(t *testing.T) {
	a := app.NewApp("parity")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/things/{id}", &showsEscapedPath{}, nil)
	ds := New(a, WithPublicLLMMD())

	for _, doc := range []string{"/things/a%2Fb/llm.md", "/things/a%2Fb/llm%2emd"} {
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, doc, nil))
		if !strings.Contains(rec.Body.String(), "esc=/things/a%2Fb\n") {
			t.Errorf("%s = %d, want the page's escaped path /things/a%%2Fb:\n%s", doc, rec.Code, rec.Body.String())
		}
	}
}

// A required outlet nothing fills makes the page the not-found page, so
// the docs answer 404; a page whose outlet is filled keeps its doc.
func TestLLMMDRequiredOutlet404(t *testing.T) {
	a := app.NewApp("parity")
	a.Register("/", &testHomeComp{}, nil)
	toc := app.NewOutlet("toc", app.OutletOptions{Fallback: app.FallbackNotFound})
	help := app.NewLayout("help", app.LayoutSpec{Outlets: []*app.Outlet{toc}},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Place(toc), l.Primary())
		})
	a.RegisterScreen(app.NewScreen("/help/present", parityText("PRESENT")).Fill(toc, parityText("TOC")), help)
	a.RegisterScreen(app.NewScreen("/help/gone", parityText("GONE BODY")), help)
	a.RegisterScreen(app.NewScreen("/guide/{slug}", &parityParamText{"GUIDE BODY"}), help)
	var ds *UIHost

	// A fill's guard that redirects moves the whole page before Load:
	// the doc is the withheld one, never the screen's body.
	toLogin := app.FillPolicy(app.PolicyFunc(func(context.Context) app.Decision {
		return app.Decision{Kind: app.DecisionRedirect, URL: "/login"}
	}))
	a.RegisterScreen(app.NewScreen("/help/guarded", parityText("GUARDED BODY")).Fill(toc, parityText("TOC"), toLogin), help)
	ds = New(a, WithPublicLLMMD(), WithMarkdownNegotiation())
	for _, path := range []string{"/help/guarded/llm.md", "/help/guarded"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept", "text/markdown")
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), "GUARDED BODY") {
			t.Errorf("%s leaked the body a region guard redirects away from:\n%s", path, rec.Body.String())
		}
	}

	assertDocsAnswer404(t, ds, "/help/gone", "GONE BODY")
	assertDocsAnswer404(t, ds, "/guide/intro", "GUIDE BODY")

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/help/present/llm.md", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "PRESENT") {
		t.Errorf("filled page llm.md = %d, want 200 with its body:\n%s", rec.Code, rec.Body.String())
	}
}
