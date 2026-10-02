package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// statusScreenComp renders its label.
type statusScreenComp string

func (s statusScreenComp) Render() render.HTML {
	return render.HTML("<p>" + string(s) + "</p>")
}

// statusFillComp renders its label as an outlet fill.
type statusFillComp string

func (f statusFillComp) Render() render.HTML {
	return render.HTML("<span>" + string(f) + "</span>")
}

// nfStatusComp is a screen whose own ScreenStatusCode says 410: the
// route resolved, its resource is gone. The 404-outlet outcome must
// outrank it (Decided 5 — the route's answer is the not-found page).
type nfStatusComp struct{}

func (nfStatusComp) Render() render.HTML { return render.HTML("<p>gone resource</p>") }
func (nfStatusComp) ScreenStatusCode() int {
	return http.StatusGone
}

// nfCustomScreen is a configured 404 body whose copy never depends on
// the path (the outlet's route DID resolve, so path-echo copy would be
// a lie; the wired body receives the path-less form).
type nfCustomScreen struct{}

func (nfCustomScreen) Render() render.HTML { return render.HTML("<p>custom-404</p>") }
func (nfCustomScreen) RenderNotFound(path string) render.HTML {
	return render.HTML("<p>custom-404</p>")
}

// outlet404App builds the app behind the 404-outlet status tests: a
// shell whose `toc` outlet is FallbackNotFound, one route that fills it
// (200) and one that leaves it unfilled (the 404 outcome).
func outlet404App() *app.App {
	a := app.NewApp("nf")
	toc := app.NewOutlet("toc", app.OutletOptions{Fallback: app.FallbackNotFound})
	aside := app.NewOutlet("aside", app.OutletOptions{Default: statusFillComp("help")})
	shell := app.NewLayout("shell", app.LayoutSpec{
		Outlets: []*app.Outlet{toc, aside},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(l.Place(toc), l.Primary(), l.Place(aside))
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/present", statusScreenComp("PRESENT")).
		Fill(toc, statusFillComp("TOC")), nil)
	a.RegisterScreen(app.NewScreen("/missing", nfStatusComp{}), nil)
	return a
}

// TestOutlet404BeatsComponentStatus: the status is decided once, before
// headers — a 404-outlet outcome outranks the component's own
// ScreenStatusCode (410 here), and the body is the not-found page the
// host wired (App.NotFound carries the host's not-found body).
func TestOutlet404BeatsComponentStatus(t *testing.T) {
	ds := New(outlet404App(), WithNotFoundScreen(&nfCustomScreen{}))

	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (the outlet outcome outranks ScreenStatusCode 410)", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "custom-404") {
		t.Errorf("the body must be the host's not-found screen:\n%s", body)
	}
	if strings.Contains(body, "gone resource") {
		t.Errorf("the screen's own content must not ship:\n%s", body)
	}
	if !strings.Contains(body, "help") {
		t.Errorf("the aside outlet renders its Default:\n%s", body)
	}

	// The control: the same shell with the outlet filled answers 200 and
	// carries the screen's content.
	w2 := httptest.NewRecorder()
	ds.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/present", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("filled outlet: status = %d, want 200", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), "PRESENT") || !strings.Contains(w2.Body.String(), "TOC") {
		t.Errorf("filled outlet renders the screen and its fill:\n%s", w2.Body.String())
	}
}

// TestOutlet404DefaultBodyIsPathless: without a configured 404 screen,
// the outcome renders the built-in default's PATH-LESS copy — the
// route resolved, so "No route matched <path>" would be a lie.
func TestOutlet404DefaultBodyIsPathless(t *testing.T) {
	ds := New(outlet404App())

	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "This page does not exist.") {
		t.Errorf("the default body's path-less copy must render:\n%s", body)
	}
	if strings.Contains(body, "No route matched") {
		t.Errorf("the outlet's route DID resolve; path-echo copy would be a lie:\n%s", body)
	}
}
