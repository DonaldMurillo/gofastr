package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// A screen is a GET resource. A POST, PUT, PATCH or DELETE that no
// registered route takes used to reach handlePage, which ran the
// screen's policy and Load, rendered the page with a 200 and minted a
// session for a request that never asked to read it. The host now
// answers 405 with Allow: GET, HEAD (OPTIONS answers 204 with the same
// Allow) before anything of the screen's runs.

// countingScreen counts its Loads through a pointer the per-request
// copy shares.
type countingScreen struct {
	loads  *atomic.Int32
	params map[string]string
}

func (c *countingScreen) Load(context.Context) error {
	c.loads.Add(1)
	return nil
}

func (c *countingScreen) SetParams(p map[string]string) { c.params = p }

func (c *countingScreen) Render() render.HTML { return render.Text("COUNTED PAGE") }

type methodFixture struct {
	ds       *UIHost
	loads    atomic.Int32
	policies atomic.Int32
}

// newMethodFixture builds a host with a root layout, an exact screen at
// /counted, a dynamic screen at /users/{id}, a trailing-slash screen at
// /docs/, and a declared redirect /old → /counted. Both counted screens
// share one Load counter and one policy counter.
func newMethodFixture(t *testing.T) *methodFixture {
	t.Helper()
	f := &methodFixture{}
	a := app.NewApp("method-test")
	a.SetDefaultLayout(chromeTestLayout("main", &testHeaderComp{}, nil, &testFooterComp{}))
	policy := app.PolicyFunc(func(context.Context) app.Decision {
		f.policies.Add(1)
		return decide.Allow()
	})
	a.RegisterScreen(app.NewScreen("/counted", &countingScreen{loads: &f.loads}).WithTitle("Counted").WithPolicy(policy), nil)
	a.RegisterScreen(app.NewScreen("/users/{id}", &countingScreen{loads: &f.loads}).WithTitle("User").WithPolicy(policy), nil)
	a.RegisterScreen(app.NewScreen("/docs/", &testHomeComp{}).WithTitle("Docs"), nil)
	a.Redirect("/old", "/counted")
	// An exact redirect may shadow one concrete URL of a dynamic screen:
	// the router resolves /users/legacy to the screen, but the redirect
	// answers it first.
	a.Redirect("/users/legacy", "/counted")
	f.ds = New(a)
	return f
}

func (f *methodFixture) do(t *testing.T, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.ds.ServeHTTP(w, req)
	return w
}

// TestScreenRefusesUnsafeMethods: every method but GET and HEAD to a
// screen path, exact or dynamic, answers 405 with Allow: GET, HEAD and
// the kit's 405 document (no-store), and runs no policy, no Load and
// mints no session.
func TestScreenRefusesUnsafeMethods(t *testing.T) {
	for _, path := range []string{"/counted", "/users/42", "/docs/"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			f := newMethodFixture(t)
			w := f.do(t, method, path, nil)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: status %d, want 405; body=%q", method, path, w.Code, truncate(w.Body.String(), 200))
				continue
			}
			if got := w.Header().Get("Allow"); got != "GET, HEAD" {
				t.Errorf("%s %s: Allow = %q, want \"GET, HEAD\"", method, path, got)
			}
			if got := w.Header().Values("Set-Cookie"); len(got) != 0 {
				t.Errorf("%s %s: minted a session: %v", method, path, got)
			}
			if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("%s %s: Cache-Control = %q, want no-store", method, path, cc)
			}
			if n := f.loads.Load(); n != 0 {
				t.Errorf("%s %s: Load ran %d times", method, path, n)
			}
			if n := f.policies.Load(); n != 0 {
				t.Errorf("%s %s: policy ran %d times", method, path, n)
			}
			body := w.Body.String()
			if !strings.Contains(body, "<html") || !strings.Contains(body, "405") {
				t.Errorf("%s %s: want the kit's 405 document, got %q", method, path, truncate(body, 300))
			}
			if strings.Contains(body, "COUNTED PAGE") || strings.Contains(body, "Home Page") {
				t.Errorf("%s %s: the screen rendered into the 405", method, path)
			}
			if sid := chromeSessionID(body); sid != "" {
				t.Errorf("%s %s: chrome carries session %q with no cookie presented", method, path, sid)
			}
		}
	}
}

// TestScreenUnsafeMethodHeaderShapes: the part, navigation and
// markdown headers do not route a POST around the refusal.
func TestScreenUnsafeMethodHeaderShapes(t *testing.T) {
	for name, hdr := range map[string]map[string]string{
		"navigate": {"X-Gofastr-Navigate": "1", "X-Gofastr-From": "/"},
		"part":     {"X-Gofastr-Part": "main"},
	} {
		f := newMethodFixture(t)
		w := f.do(t, http.MethodPost, "/counted", hdr)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status %d, want 405", name, w.Code)
		}
		if f.loads.Load() != 0 || f.policies.Load() != 0 {
			t.Errorf("%s: screen code ran (loads=%d policies=%d)", name, f.loads.Load(), f.policies.Load())
		}
	}
}

// TestScreenUnsafeMethodKeepsLiveSession: a live cookie is read, never
// re-minted, so the 405 document's chrome stays on the caller's stream.
func TestScreenUnsafeMethodKeepsLiveSession(t *testing.T) {
	f := newMethodFixture(t)
	sess := f.ds.CreateSession()
	req := httptest.NewRequest(http.MethodPost, "/counted", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: sess.Token})
	req.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
	w := httptest.NewRecorder()
	f.ds.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", w.Code)
	}
	if got := w.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("Set-Cookie on a live session: %v", got)
	}
	if id := chromeSessionID(w.Body.String()); id != sess.ID {
		t.Errorf("chrome session = %q, want the live %q", id, sess.ID)
	}
}

// TestScreenUnsafeMethodProblemJSON: a machine client gets the RFC 9457
// shape the 404 and 500 give it.
func TestScreenUnsafeMethodProblemJSON(t *testing.T) {
	f := newMethodFixture(t)
	w := f.do(t, http.MethodPost, "/counted", map[string]string{"Accept": "application/problem+json"})
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.Contains(w.Body.String(), `"status":405`) {
		t.Errorf("body = %q", w.Body.String())
	}
	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q", got)
	}
}

// TestScreenOptionsAnswers204: OPTIONS to a screen path is 204 with the
// same Allow, no body, and nothing of the screen's runs.
func TestScreenOptionsAnswers204(t *testing.T) {
	for _, path := range []string{"/counted", "/users/7"} {
		f := newMethodFixture(t)
		w := f.do(t, http.MethodOptions, path, nil)
		if w.Code != http.StatusNoContent {
			t.Errorf("OPTIONS %s: status %d, want 204", path, w.Code)
		}
		if got := w.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("OPTIONS %s: Allow = %q", path, got)
		}
		if w.Body.Len() != 0 {
			t.Errorf("OPTIONS %s: body %q", path, w.Body.String())
		}
		if got := w.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Errorf("OPTIONS %s: minted a session", path)
		}
		if f.loads.Load() != 0 || f.policies.Load() != 0 {
			t.Errorf("OPTIONS %s: screen code ran", path)
		}
	}
}

// TestScreenSafeMethodsUnchanged: GET and HEAD still render (Load and
// policy run, a session is minted), and a navigation fetch still gets
// the fragment, not a document.
func TestScreenSafeMethodsUnchanged(t *testing.T) {
	f := newMethodFixture(t)
	w := f.do(t, http.MethodGet, "/counted", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "COUNTED PAGE") {
		t.Fatalf("GET: status %d body=%q", w.Code, truncate(w.Body.String(), 200))
	}
	if len(w.Header().Values("Set-Cookie")) == 0 {
		t.Error("GET: no session minted")
	}
	if f.loads.Load() != 1 || f.policies.Load() == 0 {
		t.Errorf("GET: loads=%d policies=%d", f.loads.Load(), f.policies.Load())
	}

	w = f.do(t, http.MethodHead, "/users/9", nil)
	if w.Code != http.StatusOK {
		t.Errorf("HEAD: status %d, want 200", w.Code)
	}
	if f.loads.Load() != 2 {
		t.Errorf("HEAD: Load did not run (loads=%d)", f.loads.Load())
	}

	w = f.do(t, http.MethodGet, "/counted", map[string]string{"X-Gofastr-Navigate": "1"})
	if w.Code != http.StatusOK {
		t.Fatalf("navigate: status %d", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "COUNTED PAGE") || strings.Contains(body, "<!DOCTYPE") {
		t.Errorf("navigate: want the fragment, got %q", truncate(body, 300))
	}
}

// TestUnsafeMethodOffScreenKeeps404: a path no screen serves still
// answers 404 to a POST: the 405 needs the path to BE a screen.
func TestUnsafeMethodOffScreenKeeps404(t *testing.T) {
	f := newMethodFixture(t)
	for _, method := range []string{http.MethodPost, http.MethodOptions, http.MethodDelete} {
		w := f.do(t, method, "/nope", nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s /nope: status %d, want 404", method, w.Code)
		}
		if w.Header().Get("Allow") != "" {
			t.Errorf("%s /nope: Allow = %q on a 404", method, w.Header().Get("Allow"))
		}
	}
}

// TestUnsafeMethodRedirectsUnchanged: a declared redirect is not a
// screen; its 308 keeps the method, and the trailing-slash 301 for a
// slash-registered screen still answers before the screen check.
func TestUnsafeMethodRedirectsUnchanged(t *testing.T) {
	f := newMethodFixture(t)
	for _, p := range []string{"/old", "/users/legacy"} {
		w := f.do(t, http.MethodPost, p, nil)
		if w.Code != http.StatusPermanentRedirect || w.Header().Get("Location") != "/counted" {
			t.Errorf("POST %s: status %d Location %q, want 308 /counted", p, w.Code, w.Header().Get("Location"))
		}
	}
	var w *httptest.ResponseRecorder
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		w = f.do(t, method, "/docs", nil)
		if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/docs/" {
			t.Errorf("%s /docs: status %d Location %q, want 301 /docs/", method, w.Code, w.Header().Get("Location"))
		}
	}
}

// TestRegisteredRouteOnScreenPathWins: a route the host registers at a
// screen's path keeps its method. POST reaches the handler; a method
// neither serves is 405 with both sets in Allow; GET still renders.
// An explicit PageHandler registration for POST is a registered handler
// too, and renders the page as the host asked.
func TestRegisteredRouteOnScreenPathWins(t *testing.T) {
	f := newMethodFixture(t)
	r := router.New()
	r.Post("/counted", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	r.Post("/users/{id}", f.ds.PageHandler("/users/{id}"))
	f.ds.Mount(r)

	serve := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	if w := serve(http.MethodPost, "/counted"); w.Code != http.StatusCreated {
		t.Errorf("POST /counted: status %d, want the route's 201", w.Code)
	}
	if w := serve(http.MethodGet, "/counted"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "COUNTED PAGE") {
		t.Errorf("GET /counted: status %d, want the screen", w.Code)
	}
	w := serve(http.MethodPut, "/counted")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT /counted: status %d, want 405", w.Code)
	}
	allow := w.Header().Get("Allow")
	for _, m := range []string{"GET", "HEAD", "POST"} {
		if !strings.Contains(allow, m) {
			t.Errorf("PUT /counted: Allow = %q, missing %s", allow, m)
		}
	}
	if len(w.Header().Values("Set-Cookie")) != 0 {
		t.Error("PUT /counted: minted a session")
	}
	if w := serve(http.MethodOptions, "/counted"); w.Code != http.StatusNoContent || !strings.Contains(w.Header().Get("Allow"), "POST") {
		t.Errorf("OPTIONS /counted: status %d Allow %q, want 204 with POST", w.Code, w.Header().Get("Allow"))
	}
	loadsBefore := f.loads.Load()
	if w := serve(http.MethodPost, "/users/5"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "COUNTED PAGE") {
		t.Errorf("POST /users/5 via PageHandler: status %d, want the page", w.Code)
	}
	if f.loads.Load() != loadsBefore+1 {
		t.Error("POST /users/5 via PageHandler: Load did not run")
	}
}

// TestInfrastructureRoutesBypassScreenGate: the host's own endpoints and
// static files never reach the screen check, whatever the method. A
// static file wins its path the way it does for GET, and /__gofastr/*
// routes answer through their own handlers.
func TestInfrastructureRoutesBypassScreenGate(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "app.css"), []byte("body{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newMethodFixture(t)
	f.ds.staticDir = staticDir

	if w := f.do(t, http.MethodGet, "/app.css", nil); w.Code != http.StatusOK || w.Body.String() != "body{}" {
		t.Errorf("GET /app.css: status %d body %q", w.Code, w.Body.String())
	}
	if w := f.do(t, http.MethodPost, "/app.css", nil); w.Header().Get("Allow") == "GET, HEAD" && w.Code == http.StatusMethodNotAllowed {
		t.Error("POST /app.css: a static file took the screen 405")
	}
	// The SSE bus is GET-only: a POST is the router's method mismatch
	// on a non-screen path, never the screen answer.
	w := f.do(t, http.MethodPost, "/__gofastr/sse", nil)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /__gofastr/sse: status %d, want 405", w.Code)
	}
	if allow := w.Header().Get("Allow"); strings.Contains(allow, "HEAD") && !strings.Contains(allow, "GET") {
		t.Errorf("POST /__gofastr/sse: Allow = %q", allow)
	}
	// The server-action and session endpoints take their POST: whatever
	// they answer to a bare request (a missing session, a bad body), it
	// is their own handler's answer, never a 405.
	for _, p := range []string{"/__gofastr/action", "/__gofastr/session"} {
		w := f.do(t, http.MethodPost, p, nil)
		if w.Code == http.StatusMethodNotAllowed {
			t.Errorf("POST %s: status 405, want the endpoint's own answer", p)
		}
	}
	if f.loads.Load() != 0 || f.policies.Load() != 0 {
		t.Errorf("infrastructure requests ran screen code (loads=%d policies=%d)", f.loads.Load(), f.policies.Load())
	}
}
