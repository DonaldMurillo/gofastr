package uihost

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ---------------------------------------------------------------------------
// Test components
// ---------------------------------------------------------------------------

type testHeaderComp struct{}

func (t *testHeaderComp) Render() render.HTML {
	return html.Header(html.HeaderConfig{},
		render.Text("Test Site"),
	)
}

type testFooterComp struct{}

func (t *testFooterComp) Render() render.HTML {
	return html.Footer(html.FooterConfig{},
		render.Text("© 2025"),
	)
}

type testHomeComp struct{}

func (t *testHomeComp) Render() render.HTML {
	return html.Div(html.DivConfig{},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Home Page")),
		html.Paragraph(html.TextConfig{}, render.Text("Welcome!")),
	)
}

type testClickButton struct {
	Label string
}

func (b *testClickButton) Render() render.HTML {
	return html.Button(html.ButtonConfig{Label: b.Label, ExtraAttrs: html.OnClick("do-click")})
}

func (b *testClickButton) Actions() {
	component.On("click", func(ctx *component.ComponentContext) {
		_ = ctx
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestUIHost() *UIHost {
	application := app.NewApp("Test App")
	layout := app.NewLayout("main").
		WithHeader(&testHeaderComp{}).
		WithFooter(&testFooterComp{})
	application.SetDefaultLayout(layout)
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home").WithDescription("Home page"), nil)

	return New(application)
}

func newTestUIHostWithCSS() *UIHost {
	ds := newTestUIHost()
	ds.customCSS = "body { background: red; }"
	return ds
}

func newTestUIHostWithMultipleRoutes() *UIHost {
	ds := newTestUIHost()
	ds.App.RegisterScreen(app.NewScreen("/about", &testHomeComp{}).WithTitle("About").WithDescription("About page"), nil)
	return ds
}

func assertContains(t *testing.T, html, substr string) {
	t.Helper()
	if !strings.Contains(html, substr) {
		t.Errorf("expected HTML to contain %q, got:\n%s", substr, truncate(html, 500))
	}
}

func assertNotContains(t *testing.T, html, substr string) {
	t.Helper()
	if strings.Contains(html, substr) {
		t.Errorf("expected HTML NOT to contain %q", substr)
	}
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// ---------------------------------------------------------------------------
// A. UIHost Basic Tests
// ---------------------------------------------------------------------------

func TestUIHostServesPages(t *testing.T) {
	ds := newTestUIHost()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	assertContains(t, body, "Home Page")
	assertContains(t, body, "Test Site")
	assertContains(t, body, "© 2025")
}

func TestUIHost404(t *testing.T) {
	ds := newTestUIHost()
	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// B. Runtime JS Injection
// ---------------------------------------------------------------------------

func TestUIHostInjectsRuntimeJS(t *testing.T) {
	ds := newTestUIHost()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	assertContains(t, body, `<script src="/__gofastr/runtime.js"></script>`)
}

func TestUIHostServesRuntimeJS(t *testing.T) {
	ds := newTestUIHost()
	req := httptest.NewRequest("GET", "/__gofastr/runtime.js", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	assertContains(t, body, "__gofastr")
	assertContains(t, body, "EventSource")
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Errorf("expected javascript content type, got %q", ct)
	}
}

// ---------------------------------------------------------------------------
// C. SSE Streaming
// ---------------------------------------------------------------------------

func TestUIHostInjectsSSEMetaTag(t *testing.T) {
	ds := newTestUIHost()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	assertContains(t, body, `name="gofastr-sse"`)
	assertContains(t, body, "/__gofastr/sse?session=")
}

func TestUIHostSSERequiresSession(t *testing.T) {
	ds := newTestUIHost()
	req := httptest.NewRequest("GET", "/__gofastr/sse", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUIHostSSEStream(t *testing.T) {
	ds := newTestUIHost()

	// Register an island for a session
	sess := ds.CreateSession()
	comp := &testHomeComp{}
	w := component.NewWidget("live-feed", comp)
	isl := island.NewIsland("live-feed-"+sess.ID, w)
	isl.SessionID = sess.ID
	ds.Islands.Register(isl)

	// Subscribe to session updates
	ch := ds.Islands.Subscribe(sess.ID)

	// Push an update in background
	go func() {
		time.Sleep(50 * time.Millisecond)
		ds.Islands.PushUpdate(island.IslandUpdate{
			IslandID: "live-feed-" + sess.ID,
			HTML:     "<p>Live update!</p>",
		}, sess.ID)
	}()

	// Read from channel
	select {
	case update := <-ch:
		if update.IslandID != "live-feed-"+sess.ID {
			t.Errorf("expected island ID live-feed-%s, got %q", sess.ID, update.IslandID)
		}
		if !strings.Contains(update.HTML, "Live update!") {
			t.Errorf("expected live update HTML, got %q", update.HTML)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE update")
	}
}

// ---------------------------------------------------------------------------
// D. Session Management
// ---------------------------------------------------------------------------

func TestUIHostCreatesSession(t *testing.T) {
	ds := newTestUIHost()
	sess := ds.CreateSession()

	if sess.ID == "" {
		t.Error("expected non-empty session ID")
	}
	if sess.Created.IsZero() {
		t.Error("expected non-zero creation time")
	}

	// Should be retrievable
	got, ok := ds.GetSession(sess.ID)
	if !ok {
		t.Error("expected to find session")
	}
	if got.ID != sess.ID {
		t.Errorf("expected session ID %q, got %q", sess.ID, got.ID)
	}
}

// TestUIHostSessionIDsUniqueAtScale asserts crypto/rand-derived session
// IDs don't collide even when thousands are minted back-to-back. The
// prior `sess-<UnixNano()>` form could repeat under load.
func TestUIHostSessionIDsUniqueAtScale(t *testing.T) {
	ds := newTestUIHost()
	seen := make(map[string]struct{}, 5000)
	for i := 0; i < 5000; i++ {
		s := ds.CreateSession()
		if _, dup := seen[s.ID]; dup {
			t.Fatalf("session ID collision at i=%d: %q", i, s.ID)
		}
		seen[s.ID] = struct{}{}
	}
}

func TestUIHostAutoSessionCookie(t *testing.T) {
	ds := newTestUIHost()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "gofastr-session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected gofastr-session cookie to be set")
	}
	if !strings.HasPrefix(sessionCookie.Value, "sess-") {
		t.Errorf("expected session ID starting with sess-, got %q", sessionCookie.Value)
	}
}

func TestUIHostReuseSession(t *testing.T) {
	ds := newTestUIHost()

	// First request creates session
	req1 := httptest.NewRequest("GET", "/", nil)
	w1 := httptest.NewRecorder()
	ds.ServeHTTP(w1, req1)
	cookie := w1.Result().Cookies()[0]

	// Second request reuses session
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	ds.ServeHTTP(w2, req2)

	body := w2.Body.String()
	// Should contain the same session ID in SSE meta
	assertContains(t, body, cookie.Value)
}

func TestUIHostSessionEndpoint(t *testing.T) {
	ds := newTestUIHost()
	req := httptest.NewRequest("GET", "/__gofastr/session", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if !strings.HasPrefix(result["sessionId"], "sess-") {
		t.Errorf("expected sessionId starting with sess-, got %q", result["sessionId"])
	}
}

// ---------------------------------------------------------------------------
// E. Custom CSS Injection
// ---------------------------------------------------------------------------

func TestUIHostExtraScriptsInjectedBeforeBodyEnd(t *testing.T) {
	application := app.NewApp("Test")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home").WithDescription("h"), nil)
	ds := New(application,
		WithExtraScripts("/__livereload.js", "/diag.js"),
	)

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	page := rec.Body.String()

	for _, src := range []string{`<script src="/__livereload.js"></script>`, `<script src="/diag.js"></script>`} {
		assertContains(t, page, src)
	}
	// Order matters: extras must appear after the framework runtime so
	// they don't race with bootstrap, and before </body>.
	runtimeAt := strings.Index(page, `src="/__gofastr/runtime.js"`)
	livereloadAt := strings.Index(page, `src="/__livereload.js"`)
	bodyCloseAt := strings.LastIndex(page, "</body>")
	if !(runtimeAt > 0 && runtimeAt < livereloadAt && livereloadAt < bodyCloseAt) {
		t.Fatalf("expected runtime.js < /__livereload.js < </body>; got runtime=%d livereload=%d body=%d",
			runtimeAt, livereloadAt, bodyCloseAt)
	}
}

func TestUIHostNoExtraScriptsByDefault(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	assertNotContains(t, rec.Body.String(), `/__livereload.js`)
}

// Even when the app sets no explicit theme, /__gofastr/app.css must
// still emit a canonical :root block so components' bare var(--*)
// references resolve. The migration dropped most `var(--x, #hex)`
// fallbacks; the :root floor is now load-bearing.
func TestUIHostAppCSSAlwaysEmitsRootVars(t *testing.T) {
	application := app.NewApp("NoThemeApp")
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(application) // no WithTheme

	req := httptest.NewRequest("GET", "/__gofastr/app.css", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	for _, want := range []string{
		":root",
		"--color-primary:",
		"--color-text:",
		"--spacing-md:",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("app.css missing %q — components emit bare var() refs that need this floor:\n%s",
				want, truncate(body, 600))
		}
	}
}

// TestUIHostAppCSSShipsFrameworkBuiltinCSS asserts that framework-
// built-in helpers (visually-hidden class) ride in /__gofastr/app.css
// so the SSR-emitted live region and skip-link work without the app
// opting in via WithCustomCSS. Removing the const would silently make
// the route-announce div visible on screen.
func TestUIHostAppCSSShipsFrameworkBuiltinCSS(t *testing.T) {
	application := app.NewApp("BuiltinApp")
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(application)

	req := httptest.NewRequest("GET", "/__gofastr/app.css", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, ".fui-visually-hidden") {
		t.Errorf("app.css must ship .fui-visually-hidden helper — without it the polite live region for SPA-nav and the skip link become visible on screen; got:\n%s",
			truncate(body, 600))
	}
	if !strings.Contains(body, ".skip-link") {
		t.Errorf("app.css must ship .skip-link default styles — without it the skip-to-content link is visible on every page; got:\n%s",
			truncate(body, 600))
	}
	if !strings.Contains(body, ".skip-link:focus") {
		t.Errorf("app.css must ship .skip-link:focus styles — without them the skip link cannot be revealed to keyboard users; got:\n%s",
			truncate(body, 600))
	}
}

func TestUIHostCustomCSS(t *testing.T) {
	ds := newTestUIHostWithCSS()

	// Page references the single merged app.css.
	pageReq := httptest.NewRequest("GET", "/", nil)
	pageRec := httptest.NewRecorder()
	ds.ServeHTTP(pageRec, pageReq)
	page := pageRec.Body.String()
	assertContains(t, page, `<link rel="stylesheet" href="/__gofastr/app.css">`)
	if strings.Contains(page, "body { background: red; }") {
		t.Errorf("custom CSS should not be inlined; expected external <link>. got:\n%s", page)
	}

	// app.css carries both the theme :root vars AND the customCSS body.
	cssReq := httptest.NewRequest("GET", "/__gofastr/app.css", nil)
	cssRec := httptest.NewRecorder()
	ds.ServeHTTP(cssRec, cssReq)
	if cssRec.Code != 200 {
		t.Fatalf("/__gofastr/app.css = %d, want 200", cssRec.Code)
	}
	if got := cssRec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", got)
	}
	assertContains(t, cssRec.Body.String(), "body { background: red; }")

	// Old endpoints are removed entirely — 404, not registered.
	for _, gone := range []string{"/__gofastr/theme.css", "/__gofastr/styles.css"} {
		req := httptest.NewRequest("GET", gone, nil)
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s should be 404 (removed entirely), got %d", gone, rec.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// F. Route Graph Injection
// ---------------------------------------------------------------------------

func TestUIHostRouteGraph(t *testing.T) {
	ds := newTestUIHostWithMultipleRoutes()

	// Page embeds the route graph as inline JSON (CSP-safe; no round-
	// trip; no executable inline script).
	pageReq := httptest.NewRequest("GET", "/", nil)
	pageRec := httptest.NewRecorder()
	ds.ServeHTTP(pageRec, pageReq)
	page := pageRec.Body.String()
	assertContains(t, page, `<script type="application/json" id="gofastr-routes">`)
	assertContains(t, page, `"path":"/"`)
	assertContains(t, page, `"title":"Home"`)
	assertContains(t, page, `"title":"About"`)
	// External script reference must not appear — routes ship inline.
	if strings.Contains(page, `src="/__gofastr/routes.js"`) {
		t.Errorf("page must NOT reference /__gofastr/routes.js — route graph ships inline:\n%s", page)
	}

	// Old endpoint removed entirely — 404, not registered.
	jsReq := httptest.NewRequest("GET", "/__gofastr/routes.js", nil)
	jsRec := httptest.NewRecorder()
	ds.ServeHTTP(jsRec, jsReq)
	if jsRec.Code != http.StatusNotFound {
		t.Errorf("/__gofastr/routes.js should be 404 (removed entirely), got %d", jsRec.Code)
	}
}

// ---------------------------------------------------------------------------
// G. Action Compilation & Injection
// ---------------------------------------------------------------------------

func TestUIHostCompilesActions(t *testing.T) {
	ds := newTestUIHost()
	btn := &testClickButton{Label: "Click me"}
	js := ds.CompileActions("btn-1", btn)

	if js == "" {
		t.Error("expected non-empty compiled JS for interactive component")
	}
	assertContains(t, js, "btn-1")
}

func TestUIHostInjectsActions(t *testing.T) {
	ds := newTestUIHost()
	btn := &testClickButton{Label: "Click me"}
	ds.CompileActions("btn-1", btn)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	page := w.Body.String()
	// The actions.js script reference is what we inject when there are
	// any compiled handlers. The body itself lives at /__gofastr/actions.js.
	assertContains(t, page, `<script src="/__gofastr/actions.js"></script>`)
	if strings.Contains(page, "btn-1") {
		t.Errorf("compiled action body should not be inlined; found in page:\n%s", page)
	}
}

func TestUIHostMountAutoCompilesScreenActions(t *testing.T) {
	application := app.NewApp("Actions App")
	application.RegisterScreen(app.NewScreen("/", &testClickButton{Label: "Click me"}), nil)
	ds := New(application)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	assertContains(t, w.Body.String(), `<script src="/__gofastr/actions.js"></script>`)

	req = httptest.NewRequest("GET", "/__gofastr/actions.js", nil)
	w = httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	assertContains(t, body, `// Component: home`)
	assertContains(t, body, `G.register(id, handlers)`)
}

func TestUIHostActionsEndpoint(t *testing.T) {
	ds := newTestUIHost()
	btn := &testClickButton{Label: "Click me"}
	ds.CompileActions("btn-1", btn)

	req := httptest.NewRequest("GET", "/__gofastr/actions.js", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	assertContains(t, body, "btn-1")
}

// ---------------------------------------------------------------------------
// H. Signal Update Endpoint
// ---------------------------------------------------------------------------

func TestUIHostSignalUpdateEndpoint(t *testing.T) {
	ds := newTestUIHost()
	sess := ds.CreateSession()

	// Register an island for this session
	comp := &testHomeComp{}
	w := component.NewWidget("counter", comp)
	isl := island.NewIsland("counter-"+sess.ID, w)
	isl.SessionID = sess.ID
	ds.Islands.Register(isl)

	// Subscribe to updates
	ch := ds.Islands.Subscribe(sess.ID)

	// Post signal update
	body := strings.NewReader(`{"value": 5}`)
	req := httptest.NewRequest("POST", "/__gofastr/signal/counter?session="+sess.ID, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Should receive island update via SSE channel
	select {
	case update := <-ch:
		if update.IslandID != "counter-"+sess.ID {
			t.Errorf("expected island counter-%s, got %q", sess.ID, update.IslandID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for island update after signal")
	}
}

func TestUIHostSignalUpdateRejectsGet(t *testing.T) {
	ds := newTestUIHost()
	req := httptest.NewRequest("GET", "/__gofastr/signal/x", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 405 {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// I. Widget + Island Integration via UIHost
// ---------------------------------------------------------------------------

func TestUIHostRegisterWidget(t *testing.T) {
	ds := newTestUIHost()
	sess := ds.CreateSession()

	btn := &testClickButton{Label: "Click"}
	w := component.NewWidget("cta-btn", btn)
	isl := ds.RegisterWidget(sess.ID, w)

	// Island should be registered
	islHTML := isl.Render()
	assertContains(t, string(islHTML), `data-island="cta-btn-`+sess.ID+`"`)
	assertContains(t, string(islHTML), `data-widget="cta-btn"`)
	assertContains(t, string(islHTML), "Click")
}

// --- F11: Static file path traversal prevention ---

func TestUIHost_PathTraversalBlocked(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "safe.txt"), []byte("ok"), 0644)

	a := app.NewApp("traversal-test")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a, WithStaticDir(dir))

	server := httptest.NewServer(ds)
	defer server.Close()

	// Try path traversal
	resp, err := http.Get(server.URL + "/../../../etc/passwd")
	if err != nil {
		t.Skipf("request error (client may normalize path): %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 200 && strings.Contains(string(body), "root:") {
		t.Error("path traversal should be blocked")
	}
}

// --- F12: Server action handler invocation ---

func TestUIHost_ServerActionInvokesHandler(t *testing.T) {
	a := app.NewApp("action-test")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)

	// Register a component with actions
	handlerCalled := false
	ic := &actionTestComp{
		actions: func() {
			component.On("test-action", func(ctx *component.ComponentContext) {
				handlerCalled = true
			})
		},
	}
	ds.CompileActions("test-comp", ic)

	// POST to the action endpoint
	body := strings.NewReader(`{"action":"test-action","params":{},"componentId":"test-comp"}`)
	req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !handlerCalled {
		t.Error("expected Go handler to be invoked")
	}

	var result map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %v", result["status"])
	}
}

func TestUIHost_ServerActionUnknownComponent(t *testing.T) {
	a := app.NewApp("action-test2")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)

	body := strings.NewReader(`{"action":"test","params":{},"componentId":"no-such-comp"}`)
	req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	var result map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&result)
	if result["status"] != "error" {
		t.Errorf("expected error for unknown component, got %v", result)
	}
}

// actionTestComp is a test component that implements InteractiveComponent
type actionTestComp struct {
	html    string
	actions func()
}

func (c *actionTestComp) Render() render.HTML { return render.Raw(c.html) }
func (c *actionTestComp) Actions()            { c.actions() }

// ─── Auto-meta from ScreenDescriber ────────────────────────────────

// describedHomeComp implements ScreenDescriber; the uihost should
// auto-emit <meta name="description"> matching ScreenDescription().
type describedHomeComp struct{}

func (d *describedHomeComp) Render() render.HTML {
	return html.Div(html.DivConfig{}, html.Heading(html.HeadingConfig{Level: 1}, render.Text("Home")))
}
func (d *describedHomeComp) ScreenTitle() string       { return "Home" }
func (d *describedHomeComp) ScreenDescription() string { return "Welcome to the test site" }

// silentHomeComp does NOT implement ScreenDescriber — no auto-meta.
type silentHomeComp struct{}

func (s *silentHomeComp) Render() render.HTML {
	return html.Div(html.DivConfig{}, html.Heading(html.HeadingConfig{Level: 1}, render.Text("Silent")))
}

func TestScreenDescriberMeta(t *testing.T) {
	application := app.NewApp("Test App")
	application.Register("/", &describedHomeComp{}, nil)
	ds := New(application)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, `<meta name="description" content="Welcome to the test site">`) {
		t.Errorf("expected auto-emitted meta description from ScreenDescriber, got:\n%s",
			body)
	}
}

func TestNoMetaWithoutDescriber(t *testing.T) {
	application := app.NewApp("Test App")
	application.Register("/", &silentHomeComp{}, nil)
	ds := New(application)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()
	if strings.Contains(body, `<meta name="description"`) {
		t.Errorf("did not expect meta description when screen has no ScreenDescriber, got:\n%s",
			body)
	}
}

func TestPerPageMetaWinsOverGlobal(t *testing.T) {
	// Per-page description must come AFTER WithDescription so search
	// engines (which use the last meta) pick the per-page text.
	application := app.NewApp("Test App")
	application.Register("/", &describedHomeComp{}, nil)
	ds := New(application, WithDescription("Global site description"))
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()
	gIdx := strings.Index(body, "Global site description")
	pIdx := strings.Index(body, "Welcome to the test site")
	if gIdx < 0 || pIdx < 0 {
		t.Fatalf("expected both meta tags present; global=%d per-page=%d", gIdx, pIdx)
	}
	if gIdx >= pIdx {
		t.Errorf("expected global meta BEFORE per-screen meta, got global=%d per-page=%d", gIdx, pIdx)
	}
}
