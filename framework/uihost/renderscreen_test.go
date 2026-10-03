package uihost

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
	a.SetDefaultLayout(bareLayout("main"))
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
	// The full arm finishes through the shared page tail: no-store +
	// Vary: Cookie (the doc carries session chrome), not the caller's
	// private default.
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if rec.Header().Get("Vary") != "Cookie" {
		t.Errorf("Vary = %q, want Cookie", rec.Header().Get("Vary"))
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

// The zero ScreenResponse renders a normal 200 page through the shared
// page tail (no-store + Vary: Cookie).
func TestRenderScreenZeroResponseIs200(t *testing.T) {
	ds := New(newRecoverApp())
	rec := httptest.NewRecorder()
	ds.RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/x", nil), recoverComp{}, ScreenResponse{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if rec.Header().Get("Vary") != "Cookie" {
		t.Errorf("Vary = %q, want Cookie", rec.Header().Get("Vary"))
	}
}

// An explicit CacheControl still governs the BARE arms (partial body,
// nil component); the full document goes through the shared page tail,
// whose no-store + Vary: Cookie is not overridable — a document
// carrying session chrome and a Set-Cookie token must never enter a
// shared cache.
func TestRenderScreenCacheControlOverride(t *testing.T) {
	ds := New(newRecoverApp())

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	rec := httptest.NewRecorder()
	ds.RenderScreen(rec, req, recoverComp{}, ScreenResponse{Status: http.StatusNotFound, CacheControl: "no-cache"})
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("partial Cache-Control = %q, want no-cache (the bare arm keeps the caller's policy)", cc)
	}

	rec2 := httptest.NewRecorder()
	ds.RenderScreen(rec2, httptest.NewRequest(http.MethodGet, "/x", nil),
		recoverComp{}, ScreenResponse{Status: http.StatusNotFound, CacheControl: "no-cache"})
	if rec2.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec2.Code)
	}
	if cc := rec2.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("full-arm Cache-Control = %q, want no-store (the page tail owns the document policy)", cc)
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

// The full arm finishes like every page: it verify-or-mints the
// session (a direct load of a recovery screen is a real page — its
// widget catalog fetch and islands need the live id), while the
// PARTIAL arm still mints nothing: a bare auth-failure body must never
// hand out (or chain off) a grant.
func TestRenderScreenFullArmMintsPartialDoesNot(t *testing.T) {
	ds := New(newRecoverApp())

	rec := httptest.NewRecorder()
	ds.RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/session/dead", nil),
		recoverComp{}, ScreenResponse{Status: http.StatusGone})
	if got := rec.Header().Values("Set-Cookie"); len(got) == 0 {
		t.Error("full arm must mint a session on a cookie-less request (finishPageDocument)")
	}
	if !strings.Contains(rec.Body.String(), "session=") {
		t.Error("full arm must embed the session id in the SSE chrome")
	}

	// A live cookie is reused: no re-mint churn.
	sess := ds.CreateSession()
	req := httptest.NewRequest(http.MethodGet, "/session/dead", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: sess.Token})
	req.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
	rec2 := httptest.NewRecorder()
	ds.RenderScreen(rec2, req, recoverComp{}, ScreenResponse{Status: http.StatusGone})
	if got := rec2.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("full arm re-minted a live session: %v", got)
	}
	if !strings.Contains(rec2.Body.String(), "session="+url.QueryEscape(sess.ID)) {
		t.Errorf("full arm must embed the LIVE session id %q in the chrome", sess.ID)
	}

	preq := httptest.NewRequest(http.MethodGet, "/session/dead", nil)
	preq.Header.Set("X-Gofastr-Navigate", "1")
	rec3 := httptest.NewRecorder()
	ds.RenderScreen(rec3, preq, recoverComp{}, ScreenResponse{Status: http.StatusGone})
	if got := rec3.Header().Values("Set-Cookie"); len(got) != 0 {
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
