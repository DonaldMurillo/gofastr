package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// guardedMetaComp is a protected screen whose every metadata surface
// carries a marker the refused visitor must never see.
type guardedMetaComp struct{ kind app.ScreenType }

func (guardedMetaComp) Render() render.HTML                      { return "<p>guarded-body</p>" }
func (guardedMetaComp) ScreenTitle(context.Context) string       { return "Guarded Title Marker" }
func (guardedMetaComp) ScreenDescription(context.Context) string { return "guarded-desc-marker" }
func (guardedMetaComp) HeadHTML() string                         { return `<meta name="x" content="guarded-head-marker">` }
func (g guardedMetaComp) ScreenType() app.ScreenType             { return g.kind }

var guardedMarkers = []string{"guarded-body", "Guarded Title Marker", "guarded-desc-marker", "guarded-head-marker", "guarded-option-marker"}

// A RenderAlt answer is the alt's page: the protected screen's
// registration-time title, description and HeadHTML rode it to the
// visitor the policy refused.
func TestRenderAltHidesGuardedMeta(t *testing.T) {
	refuse := app.PolicyFunc(func(context.Context) app.Decision {
		return decide.RenderAlt(func() component.Component { return &textComp{tag: "anon-alt"} })
	})
	guard := func(s *app.Screen) { s.WithPolicy(refuse) }
	a := app.NewApp("x")
	a.Register("/secret", guardedMetaComp{kind: app.ScreenPage}, nil, guard)
	a.Register("/secret-dialog", guardedMetaComp{kind: app.ScreenDialog}, nil, guard)
	// An option-set title and an article flag are registration metadata too.
	a.Register("/secret-opt", &textComp{tag: "real"}, nil, guard, app.AsArticle(), func(s *app.Screen) {
		s.WithTitle("guarded-option-marker").WithDescription("guarded-option-marker")
	})
	ds := New(a)
	for _, path := range []string{"/secret", "/secret-dialog", "/secret-opt"} {
		// The static export bakes the same head into a file.
		page, err := ds.RenderStaticPage(context.Background(), path)
		if err != nil {
			t.Fatalf("%s static: %v", path, err)
		}
		for _, m := range guardedMarkers {
			if strings.Contains(page, m) {
				t.Errorf("%s static: page carries %q", path, m)
			}
		}
		for _, nav := range []bool{false, true} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if nav {
				req.Header.Set("X-Gofastr-Navigate", "1")
			}
			rec := httptest.NewRecorder()
			ds.ServeHTTP(rec, req)
			body := rec.Body.String()
			if !strings.Contains(body, "anon-alt") {
				t.Fatalf("%s nav=%v: alt did not render:\n%s", path, nav, body)
			}
			title, _ := url.PathUnescape(rec.Header().Get("X-Gofastr-Title"))
			for _, m := range guardedMarkers {
				if strings.Contains(body, m) || strings.Contains(title, m) {
					t.Errorf("%s nav=%v: answer carries %q", path, nav, m)
				}
			}
		}
	}
}

// A recovery screen middleware renders at a guarded path is not that
// route's screen: its head carried the route's description and HeadHTML.
func TestRenderScreenHidesRouteMeta(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/secret", guardedMetaComp{kind: app.ScreenPage}, nil)
	ds := New(a)
	for _, nav := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, "/secret", nil)
		if nav {
			req.Header.Set("X-Gofastr-Navigate", "1")
		}
		rec := httptest.NewRecorder()
		ds.RenderScreen(rec, req, &textComp{tag: "recover"}, ScreenResponse{Status: http.StatusUnauthorized})
		body := rec.Body.String()
		if !strings.Contains(body, "recover") {
			t.Fatalf("nav=%v: recovery screen did not render:\n%s", nav, body)
		}
		for _, m := range guardedMarkers {
			if strings.Contains(body, m) {
				t.Errorf("nav=%v: answer carries %q", nav, m)
			}
		}
	}
}

// keyedHeadComp's HeadHTML exists only on the loaded instance.
type keyedHeadComp struct{ id string }

func (k *keyedHeadComp) SetParams(p map[string]string) { k.id = p["id"] }
func (k *keyedHeadComp) Render() render.HTML           { return "<p>keyed</p>" }
func (k *keyedHeadComp) HeadHTML() string {
	return `<meta name="k" content="key-` + k.id + `">`
}

// HeadHTML is the rendered page's: the registration instance of a
// dynamic route knows no params.
func TestHeadHTMLReadsLoadedComponent(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/k/:id", &keyedHeadComp{}, nil)
	ds := New(a)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/k/abc", nil))
	if !strings.Contains(rec.Body.String(), `content="key-abc"`) {
		t.Errorf("head misses the loaded HeadHTML:\n%s", rec.Body.String())
	}
}
