package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// recoverComp is the branded recovery screen component.
type recoverComp struct{}

func (recoverComp) Render() render.HTML {
	return render.HTML("<section><h1>Session over</h1></section>")
}

// statusComp picks its status at render time via ScreenStatusCode.
type statusComp struct{ code int }

func (s statusComp) Render() render.HTML { return render.HTML("<p>status screen</p>") }
func (s statusComp) ScreenStatusCode() int {
	return s.code
}

func newRecoverApp() *app.App {
	a := app.NewApp("recoverapp")
	a.SetDefaultLayout(app.NewLayout("main"))
	return a
}

func TestRenderScreenStatusAndCache(t *testing.T) {
	ds := New(newRecoverApp())
	rec := httptest.NewRecorder()
	ds.RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/session/dead", nil),
		recoverComp{}, ScreenResponse{Status: http.StatusGone})

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Errorf("Cache-Control = %q, want private, no-store", cc)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Session over") {
		t.Errorf("recovery body missing: %s", body)
	}
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("full arm must render a document shell")
	}
	if !strings.Contains(body, "/__gofastr/runtime.js") {
		t.Error("full arm must inject runtime chrome")
	}
}

// The partial-navigation arm must carry the SAME status and cache
// policy as the full arm; only the body shape differs (bare content).
func TestRenderScreenPartialKeepsStatusAndCache(t *testing.T) {
	ds := New(newRecoverApp())
	req := httptest.NewRequest(http.MethodGet, "/session/dead", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	rec := httptest.NewRecorder()
	ds.RenderScreen(rec, req, recoverComp{}, ScreenResponse{Status: http.StatusGone})

	if rec.Code != http.StatusGone {
		t.Fatalf("partial status = %d, want 410", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Errorf("partial Cache-Control = %q, want private, no-store", cc)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Session over") {
		t.Errorf("partial body missing recovery content: %s", body)
	}
	for _, unwanted := range []string{"<!DOCTYPE", "runtime.js", "<html"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("partial body must not carry %q", unwanted)
		}
	}
}

// The zero ScreenResponse renders a normal 200 page (still private:
// a screen rendered by hand is per-caller by construction).
func TestRenderScreenZeroResponseIs200(t *testing.T) {
	ds := New(newRecoverApp())
	rec := httptest.NewRecorder()
	ds.RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/x", nil), recoverComp{}, ScreenResponse{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Errorf("Cache-Control = %q, want private, no-store", cc)
	}
}

// An explicit CacheControl replaces the default; the caller owns the
// reason.
func TestRenderScreenCacheControlOverride(t *testing.T) {
	ds := New(newRecoverApp())
	rec := httptest.NewRecorder()
	ds.RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/x", nil),
		recoverComp{}, ScreenResponse{Status: http.StatusNotFound, CacheControl: "no-cache"})

	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// ScreenStatusCode fills in the status when ScreenResponse.Status is
// zero, and an explicit Status wins over it.
func TestRenderScreenScreenStatusCodeFallback(t *testing.T) {
	ds := New(newRecoverApp())

	rec := httptest.NewRecorder()
	ds.RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/x", nil), statusComp{code: 404}, ScreenResponse{})
	if rec.Code != http.StatusNotFound {
		t.Errorf("fallback status = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	ds.RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/x", nil),
		statusComp{code: 404}, ScreenResponse{Status: http.StatusGone})
	if rec.Code != http.StatusGone {
		t.Errorf("explicit status = %d, want 410 (explicit must win)", rec.Code)
	}
}

// net/http panics on out-of-range codes; RenderScreen must clamp
// instead of taking the server down.
func TestRenderScreenInvalidStatusClamped(t *testing.T) {
	ds := New(newRecoverApp())
	rec := httptest.NewRecorder()
	ds.RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/x", nil),
		recoverComp{}, ScreenResponse{Status: 99})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// A nil component must not panic; the status and cache policy still
// apply to the plain-text body.
func TestRenderScreenNilComponent(t *testing.T) {
	ds := New(newRecoverApp())
	rec := httptest.NewRecorder()
	ds.RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/x", nil), nil,
		ScreenResponse{Status: http.StatusGone})

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Errorf("Cache-Control = %q, want private, no-store", cc)
	}
}

// The recovery arm mints nothing: no session cookie on either arm, so
// an auth-failure response can never hand out (or chain off) a grant.
func TestRenderScreenDoesNotMintSession(t *testing.T) {
	ds := New(newRecoverApp())

	rec := httptest.NewRecorder()
	ds.RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/session/dead", nil),
		recoverComp{}, ScreenResponse{Status: http.StatusGone})
	if got := rec.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("full arm set cookies: %v", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/session/dead", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	rec = httptest.NewRecorder()
	ds.RenderScreen(rec, req, recoverComp{}, ScreenResponse{Status: http.StatusGone})
	if got := rec.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("partial arm set cookies: %v", got)
	}
}

// A ScreenTitler component names the recovery page itself.
func TestRenderScreenTitleFromTitler(t *testing.T) {
	ds := New(newRecoverApp())
	rec := httptest.NewRecorder()
	ds.RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/x", nil),
		titleOnlyComp{}, ScreenResponse{Status: http.StatusGone})
	if body := rec.Body.String(); !strings.Contains(body, "<title>Session expired — recoverapp</title>") {
		t.Errorf("title not honored: %s", body[:min(200, len(body))])
	}
}

type titleOnlyComp struct{}

func (titleOnlyComp) Render() render.HTML { return render.HTML("<p>gone</p>") }
func (titleOnlyComp) ScreenTitle() string { return "Session expired" }
