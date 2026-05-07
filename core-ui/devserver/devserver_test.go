package devserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core-ui/island"
	"github.com/gofastr/gofastr/core/render"
)

// ---------------------------------------------------------------------------
// Test components
// ---------------------------------------------------------------------------

type testHeaderComp struct{}

func (t *testHeaderComp) Render() render.HTML {
	return elements.Header(elements.Aria("label", "Site header"),
		render.Text("Test Site"),
	)
}

type testFooterComp struct{}

func (t *testFooterComp) Render() render.HTML {
	return elements.Footer(elements.Aria("label", "Site footer"),
		render.Text("© 2025"),
	)
}

type testHomeComp struct{}

func (t *testHomeComp) Render() render.HTML {
	return elements.Div(nil,
		elements.Heading(1, nil, render.Text("Home Page")),
		elements.Paragraph(nil, render.Text("Welcome!")),
	)
}

type testClickButton struct {
	Label string
}

func (b *testClickButton) Render() render.HTML {
	return elements.Button(b.Label, elements.OnClick("do-click"))
}

func (b *testClickButton) Actions() {
	component.On("click", func(ctx *component.ComponentContext) {
		_ = ctx
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestDevServer() *DevServer {
	application := app.NewApp("Test App")
	layout := app.NewLayout("main").
		WithHeader(&testHeaderComp{}).
		WithFooter(&testFooterComp{})
	application.SetDefaultLayout(layout)
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home").WithDescription("Home page"), nil)

	return NewDevServer(application)
}

func newTestDevServerWithCSS() *DevServer {
	ds := newTestDevServer()
	ds.customCSS = "body { background: red; }"
	return ds
}

func newTestDevServerWithRouteGraph() *DevServer {
	ds := newTestDevServer()
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
// A. DevServer Basic Tests
// ---------------------------------------------------------------------------

func TestDevServerServesPages(t *testing.T) {
	ds := newTestDevServer()
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

func TestDevServer404(t *testing.T) {
	ds := newTestDevServer()
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

func TestDevServerInjectsRuntimeJS(t *testing.T) {
	ds := newTestDevServer()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	assertContains(t, body, `<script src="/__gofastr/runtime.js"></script>`)
}

func TestDevServerServesRuntimeJS(t *testing.T) {
	ds := newTestDevServer()
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

func TestDevServerInjectsSSEMetaTag(t *testing.T) {
	ds := newTestDevServer()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	assertContains(t, body, `name="gofastr-sse"`)
	assertContains(t, body, "/__gofastr/sse?session=")
}

func TestDevServerSSERequiresSession(t *testing.T) {
	ds := newTestDevServer()
	req := httptest.NewRequest("GET", "/__gofastr/sse", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDevServerSSEStream(t *testing.T) {
	ds := newTestDevServer()

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

func TestDevServerCreatesSession(t *testing.T) {
	ds := newTestDevServer()
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

func TestDevServerAutoSessionCookie(t *testing.T) {
	ds := newTestDevServer()
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

func TestDevServerReuseSession(t *testing.T) {
	ds := newTestDevServer()

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

func TestDevServerSessionEndpoint(t *testing.T) {
	ds := newTestDevServer()
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

func TestDevServerCustomCSS(t *testing.T) {
	ds := newTestDevServerWithCSS()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	assertContains(t, body, "body { background: red; }")
}

// ---------------------------------------------------------------------------
// F. Route Graph Injection
// ---------------------------------------------------------------------------

func TestDevServerRouteGraph(t *testing.T) {
	ds := newTestDevServerWithRouteGraph()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	assertContains(t, body, "window.__gofastr_routes")
	assertContains(t, body, `"path":"/"`)
	assertContains(t, body, `"title":"Home"`)
	assertContains(t, body, `"title":"About"`)
}

// ---------------------------------------------------------------------------
// G. Action Compilation & Injection
// ---------------------------------------------------------------------------

func TestDevServerCompilesActions(t *testing.T) {
	ds := newTestDevServer()
	btn := &testClickButton{Label: "Click me"}
	js := ds.CompileActions("btn-1", btn)

	if js == "" {
		t.Error("expected non-empty compiled JS for interactive component")
	}
	assertContains(t, js, "btn-1")
}

func TestDevServerInjectsActions(t *testing.T) {
	ds := newTestDevServer()
	btn := &testClickButton{Label: "Click me"}
	ds.CompileActions("btn-1", btn)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	assertContains(t, body, "btn-1")
	assertContains(t, body, "__gofastr")
}

func TestDevServerActionsEndpoint(t *testing.T) {
	ds := newTestDevServer()
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

func TestDevServerSignalUpdateEndpoint(t *testing.T) {
	ds := newTestDevServer()
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

func TestDevServerSignalUpdateRejectsGet(t *testing.T) {
	ds := newTestDevServer()
	req := httptest.NewRequest("GET", "/__gofastr/signal/x", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 405 {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// I. Widget + Island Integration via DevServer
// ---------------------------------------------------------------------------

func TestDevServerRegisterWidget(t *testing.T) {
	ds := newTestDevServer()
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

// ---------------------------------------------------------------------------
// J. RenderPage (testing helper)
// ---------------------------------------------------------------------------

func TestDevServerRenderPage(t *testing.T) {
	ds := newTestDevServerWithCSS()
	page, err := ds.RenderPage("/", "sess-test123")
	if err != nil {
		t.Fatal(err)
	}

	assertContains(t, page, "Home Page")
	assertContains(t, page, `/__gofastr/sse?session=sess-test123`)
	assertContains(t, page, "/__gofastr/runtime.js")
	assertContains(t, page, "body { background: red; }")
}

func TestDevServerRenderPageNotFound(t *testing.T) {
	ds := newTestDevServer()
	_, err := ds.RenderPage("/nope", "sess-test")
	if err == nil {
		t.Error("expected error for unknown path")
	}
}
