package uihost

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Version-skew tests for the fills envelope (DESIGN-layout-outlets.md
// "Mixed versions during a deploy"): the envelope version is negotiated
// in both directions and a mismatch always ends in a full load.

// skewTestApp builds an app whose default shell is a tree layout with a
// toolbar outlet and a crumbs area, so every same-chain navigation
// keeps a layer that carries fills.
func skewTestApp() *app.App {
	application := app.NewApp("t")
	toolbar := app.NewOutlet("toolbar")
	shell := app.NewLayout("site", app.LayoutSpec{
		Outlets: []*app.Outlet{toolbar},
		Areas:   []app.AreaSpec{{Name: "crumbs"}},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			l.RouteArea("crumbs", func(ctx context.Context, m app.Match) render.HTML {
				return render.Text("crumbs:" + m.Path())
			}),
			l.Place(toolbar),
			l.Primary(),
		)
	})
	application.SetDefaultLayout(shell)
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}), nil)
	application.RegisterScreen(app.NewScreen("/other", &testHomeComp{}).
		Fill(toolbar, app.NewStaticComponent("OTHER-TOOLBAR")), nil)
	return application
}

// TestOldClientGetsReloadSwapWhenKeptLayerHasOutlets: a navigation
// request WITHOUT X-Gofastr-Fills whose kept layers have outlets or
// areas cannot receive their fills — the bare body answers only the
// primary and an old runtime would keep stale outlets with no repair.
// The server must answer X-Gofastr-Swap: !reload, a swap key no DOM
// holds, so the old runtime's missing-slot repair full-loads the
// destination. A fills-capable client keeps the ordinary swap and the
// envelope.
func TestOldClientGetsReloadSwapWhenKeptLayerHasOutlets(t *testing.T) {
	ds := New(skewTestApp())

	old := httptest.NewRequest("GET", "/other", nil)
	old.Header.Set("X-Gofastr-Navigate", "1")
	old.Header.Set("X-Gofastr-From", "/")
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, old)
	if w.Code != 200 {
		t.Fatalf("old-client partial: status %d", w.Code)
	}
	if got := w.Header().Get("X-Gofastr-Swap"); got != "!reload" {
		t.Fatalf("X-Gofastr-Swap = %q, want !reload for a fills-less client whose kept layers carry fills", got)
	}
	if got := w.Header().Get("X-Gofastr-Partial"); got != "true" {
		t.Fatalf("X-Gofastr-Partial = %q, want true (the repair discards the body)", got)
	}
	if got := w.Header().Get("X-Gofastr-Envelope"); got != "" {
		t.Fatalf("X-Gofastr-Envelope = %q, want absent for a fills-less client", got)
	}
	if body := w.Body.String(); !strings.Contains(body, "test-home") && body == "" {
		t.Fatalf("old-client partial must still carry a body the repair can discard, got %q", body)
	}

	// Control: the same navigation WITH the fills header keeps the
	// ordinary swap layer and gets the envelope.
	modern := httptest.NewRequest("GET", "/other", nil)
	modern.Header.Set("X-Gofastr-Navigate", "1")
	modern.Header.Set("X-Gofastr-From", "/")
	modern.Header.Set("X-Gofastr-Fills", "2")
	w2 := httptest.NewRecorder()
	ds.ServeHTTP(w2, modern)
	if w2.Code != 200 {
		t.Fatalf("fills-client partial: status %d", w2.Code)
	}
	if got := w2.Header().Get("X-Gofastr-Swap"); got != "l:site" {
		t.Fatalf("X-Gofastr-Swap = %q, want l:site for a fills-capable client", got)
	}
	body := w2.Body.String()
	if got := w2.Header().Get("X-Gofastr-Envelope"); got != "2" {
		t.Fatalf("X-Gofastr-Envelope = %q, want 2", got)
	}
	if !strings.Contains(body, `data-cui-fill="l:site#toolbar"`) {
		t.Errorf("envelope must carry the kept toolbar fill: %s", body)
	}
	if !strings.Contains(body, "OTHER-TOOLBAR") {
		t.Errorf("toolbar fill must carry the destination's fill: %s", body)
	}
}

// TestFillslessClientWithoutKeptFillsKeepsSwap: the !reload rule fires
// only when fills exist to lose — a fills-less client navigating
// between pages that share no tree layers (or whose kept layers carry
// no outlets/areas) keeps the ordinary partial answer, so a pre-layout
// page never pays a repair it does not need.
func TestFillslessClientWithoutKeptFillsKeepsSwap(t *testing.T) {
	ds := New(chainTestApp()) // template layouts, no outlets anywhere
	w := partialGet(t, ds, "/docs/intro", "/about")
	if got := w.Header().Get("X-Gofastr-Swap"); got != "l:site" {
		t.Fatalf("X-Gofastr-Swap = %q, want l:site (no fills to lose)", got)
	}
}

// TestStaleMarkupClientGetsReloadBody: a browser navigation fetch that
// names no markup generation (a runtime from before the data-cui-*
// rename) gets a 409 partial carrying a meta refresh to the
// destination, at the swap key its DOM holds; the generation the host
// renders, or a non-browser caller, gets the ordinary partial.
func TestStaleMarkupClientGetsReloadBody(t *testing.T) {
	ds := New(chainTestApp())
	get := func(markup, mode, target string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", target, nil)
		r.Header.Set("X-Gofastr-Navigate", "1")
		r.Header.Set("X-Gofastr-From", "/about")
		if markup != "" {
			r.Header.Set(runtime.MarkupHeader, markup)
		}
		if mode != "" {
			r.Header.Set("Sec-Fetch-Mode", mode)
		}
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, r)
		return w
	}

	stale := get("", "cors", "/docs/intro?q=it's")
	if stale.Code != 409 {
		t.Fatalf("stale client: status %d, want 409", stale.Code)
	}
	if got := stale.Header().Get("X-Gofastr-Swap"); got != "l:site" {
		t.Errorf("stale client: X-Gofastr-Swap = %q, want the l:site cell its DOM holds", got)
	}
	if got := stale.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("stale client: Cache-Control = %q, want no-store", got)
	}
	body := stale.Body.String()
	if !strings.Contains(body, `<meta http-equiv="refresh" content="0; url='/docs/intro?q=it%27s'">`) {
		t.Errorf("stale client body lacks the refresh to the destination (quote kept inside the URL): %s", body)
	}
	if strings.Contains(body, "data-cui-") {
		t.Errorf("stale client body carries markup its kernel cannot read: %s", body)
	}

	for _, tc := range []struct{ name, markup, mode string }{
		{"current generation", runtime.MarkupVersion, "cors"},
		{"no fetch metadata", "", ""},
	} {
		w := get(tc.markup, tc.mode, "/docs/intro")
		if w.Code != 200 || strings.Contains(w.Body.String(), "http-equiv") {
			t.Errorf("%s: status %d body %q, want the ordinary partial", tc.name, w.Code, w.Body.String())
		}
	}
	// An older generation named outright is stale too.
	if w := get("1", "same-origin", "/docs/intro"); w.Code != 409 {
		t.Errorf("generation 1: status %d, want 409", w.Code)
	}
}
