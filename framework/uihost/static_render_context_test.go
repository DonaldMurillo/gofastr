package uihost

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// pathAwareChrome is a layout component that renders for the page being
// served, the way a docs sidebar or a header whose language follows the page
// does: it reads the path from the request and the route match.
type pathAwareChrome struct{}

func (pathAwareChrome) Render() render.HTML { return render.Text("chrome for nothing") }

func (pathAwareChrome) RenderCtx(ctx context.Context) render.HTML {
	path := "none"
	if r := app.RequestFromContext(ctx); r != nil && r.URL != nil {
		path = r.URL.Path
	}
	match := "none"
	if m, ok := app.MatchFromContext(ctx); ok {
		match = m.ScreenID() + " slug=" + m.Param("slug")
	}
	return render.Text("request:" + path + " match:" + match)
}

// slugScreen is a screen on a dynamic route; the router requires one to
// accept its params.
type slugScreen struct{ slug string }

func (s *slugScreen) SetParams(params map[string]string) { s.slug = params["slug"] }
func (s *slugScreen) Render() render.HTML                { return render.Text("doc " + s.slug) }

// A static export must render layout chrome with the same context a live
// request gets. It rendered with neither a request nor a route match, so a
// sidebar for the section being read, or a header whose language follows
// the page, rendered for no page at all: every exported page carried the
// chrome of the site root.
func TestRenderStaticPageCarriesTheRequestAndMatch(t *testing.T) {
	application := app.NewApp("StaticContext")
	application.SetDefaultLayout(app.NewLayout("main").WithHeader(pathAwareChrome{}))
	application.RegisterScreen(app.NewScreen("/docs/{slug}", &slugScreen{}).WithTitle("Doc"), nil)

	page, err := New(application).RenderStaticPage(context.Background(), "/docs/guide")
	if err != nil {
		t.Fatalf("RenderStaticPage: %v", err)
	}
	if !strings.Contains(page, "request:/docs/guide") {
		t.Fatalf("static chrome rendered without the request path: %s", excerpt(page, "request:"))
	}
	if !strings.Contains(page, "match:/docs/:slug slug=guide") {
		t.Fatalf("static chrome rendered without the route match: %s", excerpt(page, "match:"))
	}
}

func excerpt(page, marker string) string {
	i := strings.Index(page, marker)
	if i < 0 {
		return "(marker absent)"
	}
	end := i + 80
	if end > len(page) {
		end = len(page)
	}
	return page[i:end]
}

// A caller that already supplies a request and a route match keeps them: the
// static render only fills in what is missing. Without these guards a host
// rendering a page under another page's request, as a preview does, would
// have its context replaced.
func TestRenderStaticPageKeepsASuppliedRequestAndMatch(t *testing.T) {
	application := app.NewApp("StaticContextSupplied")
	application.SetDefaultLayout(app.NewLayout("main").WithHeader(pathAwareChrome{}))
	application.RegisterScreen(app.NewScreen("/docs/{slug}", &slugScreen{}).WithTitle("Doc"), nil)

	req := httptest.NewRequest("GET", "/somewhere/else", nil)
	ctx := app.WithRequest(context.Background(), req)
	supplied, ok := application.Router.MatchFor("/docs/other")
	if !ok {
		t.Fatal("no match for /docs/other")
	}
	ctx = app.WithMatch(ctx, supplied)

	page, err := New(application).RenderStaticPage(ctx, "/docs/guide")
	if err != nil {
		t.Fatalf("RenderStaticPage: %v", err)
	}
	if !strings.Contains(page, "request:/somewhere/else") {
		t.Fatalf("the supplied request was replaced: %s", excerpt(page, "request:"))
	}
	if !strings.Contains(page, "match:/docs/:slug slug=other") {
		t.Fatalf("the supplied route match was replaced: %s", excerpt(page, "match:"))
	}
}

// The runtime loads a component's CSS when no link carries its marker, so
// the links the static export writes must carry it, or every static page
// loads each component stylesheet twice, the second copy after app.css.
func TestRenderStaticPageMarksComponentStylesheets(t *testing.T) {
	registry.IsolateForTest(t)
	st := registerTestStyle(t, "static")
	ds := newTestUIHostFor(st)

	page, err := ds.RenderStaticPage(context.Background(), "/")
	if err != nil {
		t.Fatalf("RenderStaticPage: %v", err)
	}
	want := `href="/__gofastr/comp/` + st.Name() + `.css?v=`
	if !strings.Contains(page, want) {
		t.Fatalf("no component stylesheet link in the static page: %s", excerpt(page, "<link"))
	}
	if !strings.Contains(page, `data-fui-style="`+st.Name()+`" id="fui-css-`+st.Name()+`"`) {
		t.Fatalf("the component stylesheet link lacks the runtime's marker: %s", excerpt(page, "/__gofastr/comp/"))
	}
}
