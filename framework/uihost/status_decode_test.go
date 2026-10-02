package uihost

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// routedGoneComp answers a non-default status from the component.
type routedGoneComp struct{}

func (routedGoneComp) Render() render.HTML { return render.HTML(`<p id="gone">GONE</p>`) }
func (routedGoneComp) ScreenStatusCode() int {
	return http.StatusGone
}

// TestComponentStatusBeatsDefault200 (the routed-page arm): a routed
// screen whose component implements ScreenStatusCode under a tree
// layout with fills answers its status — 410 here — on BOTH the full
// GET and the fills-negotiated partial, decided once before any body
// byte, with the fills still shipping on the partial. The RenderScreen
// API twin lives in renderscreen_test.go; this pins the routed path
// writePartialResult serves. The mutation it catches: writing the 200
// header unconditionally (or before the status decision).
func TestComponentStatusBeatsDefault200(t *testing.T) {
	a := app.NewApp("t")
	toolbar := app.NewOutlet("toolbar")
	shell := app.NewLayout("shell", app.LayoutSpec{
		Outlets: []*app.Outlet{toolbar},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(l.Place(toolbar), l.Primary())
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", app.NewStaticComponent("HOME")).
		Fill(toolbar, app.NewStaticComponent("HOME-TOOLBAR")), nil)
	a.RegisterScreen(app.NewScreen("/gone", routedGoneComp{}).
		Fill(toolbar, app.NewStaticComponent("GONE-TOOLBAR")), nil)
	ds := New(a)

	// Full GET: 410 before any body byte, body intact.
	req := httptest.NewRequest("GET", "/gone", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code != http.StatusGone {
		t.Fatalf("full GET status = %d, want 410", w.Code)
	}
	if !strings.Contains(w.Body.String(), "GONE") {
		t.Errorf("full GET body must still render: %s", w.Body.String())
	}

	// Partial GET (the runtime's headers): 410, partial-shaped, the
	// envelope and its fills still ship.
	preq := httptest.NewRequest("GET", "/gone", nil)
	preq.Header.Set("X-Gofastr-Navigate", "1")
	preq.Header.Set("X-Gofastr-From", "/")
	preq.Header.Set("X-Gofastr-Fills", "2")
	pw := httptest.NewRecorder()
	ds.ServeHTTP(pw, preq)
	if pw.Code != http.StatusGone {
		t.Fatalf("partial GET status = %d, want 410 (the status is decided before any body byte)", pw.Code)
	}
	if pw.Header().Get("X-Gofastr-Partial") != "true" {
		t.Errorf("partial GET must be partial-shaped, got headers %v", pw.Header())
	}
	if pw.Header().Get("X-Gofastr-Envelope") != "2" {
		t.Errorf("partial GET must ship the fills envelope even at 410")
	}
	if body := pw.Body.String(); !strings.Contains(body, `data-fui-fill="l:shell#toolbar"`) || !strings.Contains(body, "GONE-TOOLBAR") {
		t.Errorf("the fills must still ship at 410: %s", body)
	}
}

// paramEchoScreen renders its route param so the HTTP wire's decoding
// is observable in the page bytes.
type paramEchoScreen struct {
	name string
}

func (p *paramEchoScreen) SetParams(m map[string]string) { p.name = m["name"] }
func (p *paramEchoScreen) Render() render.HTML {
	return render.HTML(`<p id="param">` + render.Text(p.name) + `</p>`)
}

// TestRouteStateDecodedAndConstrained (the decoding half, at the HTTP
// level): net/http decodes the path exactly once (r.URL.Path) and the
// router never decodes again — `/…/caf%C3%A9` reaches the screen as
// the param `café`, and a double-encoded `%252F` arrives as the
// single-decoded `%2F`, never as a slash (a param is one segment; a
// re-decode would split it). The constraint arms are pinned in
// core-ui/app (route_constraint_test.go); TestResolveCatchAllRawNoDecode
// pins the router's raw half.
func TestRouteStateDecodedAndConstrained(t *testing.T) {
	a := app.NewApp("t")
	shell := app.NewLayout("shell", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return l.Primary()
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/items/{name}", &paramEchoScreen{}), nil)
	ds := New(a)

	get := func(path string) string {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("GET %s: status %d", path, w.Code)
		}
		body, _ := io.ReadAll(w.Body)
		i := strings.Index(string(body), `<p id="param">`)
		if i < 0 {
			t.Fatalf("GET %s: param marker missing from %s", path, body)
		}
		rest := string(body[i+len(`<p id="param">`):])
		return rest[:strings.Index(rest, "</p>")]
	}

	if got := get("/items/caf%C3%A9"); got != "café" {
		t.Errorf("GET /items/caf%%C3%%A9: param = %q, want café (decoded once by net/http)", got)
	}
	if got := get("/items/a%252Fb"); got != "a%2Fb" {
		t.Errorf("GET /items/a%%252Fb: param = %q, want a%%2Fb (never a slash)", got)
	}
}
