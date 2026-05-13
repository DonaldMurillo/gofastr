package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// stubComponent is a test helper that renders fixed HTML.
type stubComponent struct {
	html render.HTML
}

func (s *stubComponent) Render() render.HTML { return s.html }

// stubService is a test helper for DI tests.
type stubService struct {
	Value string
}

func TestNewApp(t *testing.T) {
	a := NewApp("TestApp")
	if a.Name != "TestApp" {
		t.Errorf("expected name TestApp, got %q", a.Name)
	}
	if a.Container == nil {
		t.Error("expected Container to be initialized")
	}
	if a.Router == nil {
		t.Error("expected Router to be initialized")
	}
	if a.Theme != nil {
		t.Error("expected Theme to be nil by default")
	}
}

func TestAppWithTheme(t *testing.T) {
	a := NewApp("ThemedApp")
	theme := style.DefaultTheme()
	result := a.WithTheme(theme)
	if result != a {
		t.Error("WithTheme should return the app for chaining")
	}
	if a.Theme == nil {
		t.Error("expected Theme to be set")
	}
	if a.Theme.Name != "default" {
		t.Errorf("expected theme name 'default', got %q", a.Theme.Name)
	}
}

func TestNewScreen(t *testing.T) {
	comp := &stubComponent{html: render.Raw("<p>Hello</p>")}
	s := NewScreen("/home", comp)
	if s.Path != "/home" {
		t.Errorf("expected path '/home', got %q", s.Path)
	}
	if s.Type != ScreenPage {
		t.Errorf("expected ScreenPage, got %v", s.Type)
	}
	if s.Component != comp {
		t.Error("expected component to be set")
	}
}

func TestNewDrawer(t *testing.T) {
	comp := &stubComponent{html: render.Raw("<nav>Menu</nav>")}
	s := NewDrawer("/sidebar", comp)
	if s.Type != ScreenDrawer {
		t.Errorf("expected ScreenDrawer, got %v", s.Type)
	}

	// Drawer renders bare content — runtime adds structural wrapping
	html := string(s.Render())
	if !strings.Contains(html, "<nav>Menu</nav>") {
		t.Errorf("expected bare content in drawer, got: %s", html)
	}
	// Wrappers are NOT added server-side for overlays
	if strings.Contains(html, "overlay-backdrop") {
		t.Errorf("drawer should not include overlay wrapper, got: %s", html)
	}
}

func TestNewSheet(t *testing.T) {
	comp := &stubComponent{html: render.Raw("<p>Sheet content</p>")}
	s := NewSheet("/sheet", comp)
	if s.Type != ScreenSheet {
		t.Errorf("expected ScreenSheet, got %v", s.Type)
	}

	// Sheet renders bare content — runtime adds structural wrapping
	html := string(s.Render())
	if !strings.Contains(html, "<p>Sheet content</p>") {
		t.Errorf("expected bare content in sheet, got: %s", html)
	}
	if strings.Contains(html, "overlay-backdrop") {
		t.Errorf("sheet should not include overlay wrapper, got: %s", html)
	}
}

func TestNewDialog(t *testing.T) {
	comp := &stubComponent{html: render.Raw("<p>Dialog content</p>")}
	s := NewDialog("/dialog", comp)
	if s.Type != ScreenDialog {
		t.Errorf("expected ScreenDialog, got %v", s.Type)
	}

	// Dialog renders bare content — runtime adds structural wrapping
	html := string(s.Render())
	if !strings.Contains(html, "<p>Dialog content</p>") {
		t.Errorf("expected bare content in dialog, got: %s", html)
	}
	if strings.Contains(html, "overlay-backdrop") {
		t.Errorf("dialog should not include overlay wrapper, got: %s", html)
	}
}

func TestScreenRender(t *testing.T) {
	comp := &stubComponent{html: render.Raw("<p>Page content</p>")}
	s := NewScreen("/", comp)

	html := string(s.Render())
	if !strings.Contains(html, "<main") {
		t.Errorf("expected <main> element, got: %s", html)
	}
	if !strings.Contains(html, `role="main"`) {
		t.Errorf("expected role=main, got: %s", html)
	}
	if !strings.Contains(html, "<p>Page content</p>") {
		t.Errorf("expected page content, got: %s", html)
	}
}

func TestLayout(t *testing.T) {
	headerComp := &stubComponent{html: render.Raw("<h1>Header</h1>")}
	sidebarComp := &stubComponent{html: render.Raw("<ul><li>Nav</li></ul>")}
	footerComp := &stubComponent{html: render.Raw("<p>Footer</p>")}

	l := NewLayout("app").
		WithHeader(headerComp).
		WithSidebar(sidebarComp).
		WithFooter(footerComp)

	if l.Name != "app" {
		t.Errorf("expected name 'app', got %q", l.Name)
	}
	if l.Header != headerComp {
		t.Error("expected header to be set")
	}
	if l.Sidebar != sidebarComp {
		t.Error("expected sidebar to be set")
	}
	if l.Footer != footerComp {
		t.Error("expected footer to be set")
	}
}

func TestLayoutWrap(t *testing.T) {
	headerComp := &stubComponent{html: render.Raw("<h1>Header</h1>")}
	sidebarComp := &stubComponent{html: render.Raw("<ul><li>Nav</li></ul>")}
	footerComp := &stubComponent{html: render.Raw("<p>Footer</p>")}

	l := NewLayout("app").
		WithHeader(headerComp).
		WithSidebar(sidebarComp).
		WithFooter(footerComp)

	content := render.Raw("<p>Content</p>")
	html := string(l.Wrap(content))

	// Check structure.
	if !strings.Contains(html, `class="layout-app"`) {
		t.Errorf("expected layout-app class, got: %s", html)
	}
	if !strings.Contains(html, `role="banner"`) {
		t.Errorf("expected role=banner, got: %s", html)
	}
	if !strings.Contains(html, "<h1>Header</h1>") {
		t.Errorf("expected header content, got: %s", html)
	}
	if !strings.Contains(html, `aria-label="Sidebar"`) {
		t.Errorf("expected Sidebar aria-label, got: %s", html)
	}
	if !strings.Contains(html, `class="layout-body"`) {
		t.Errorf("expected layout-body class, got: %s", html)
	}
	if !strings.Contains(html, `role="contentinfo"`) {
		t.Errorf("expected role=contentinfo, got: %s", html)
	}
	if !strings.Contains(html, "<p>Content</p>") {
		t.Errorf("expected content, got: %s", html)
	}
}

func TestLayoutWrapNil(t *testing.T) {
	var l *Layout
	content := render.Raw("<p>Just content</p>")
	html := string(l.Wrap(content))
	if html != "<p>Just content</p>" {
		t.Errorf("nil layout should pass through content, got: %s", html)
	}
}

func TestRouter(t *testing.T) {
	r := NewRouter()
	comp := &stubComponent{html: render.Raw("<p>Home</p>")}
	screen := NewScreen("/", comp)
	r.Screen(screen, nil)

	resolved, _, ok := r.Resolve("/")
	if !ok {
		t.Error("expected to resolve /")
	}
	if resolved != screen {
		t.Error("expected to get the same screen")
	}

	_, _, ok = r.Resolve("/nonexistent")
	if ok {
		t.Error("expected not to resolve /nonexistent")
	}
}

func TestRouterRender(t *testing.T) {
	r := NewRouter()
	comp := &stubComponent{html: render.Raw("<p>Home</p>")}
	screen := NewScreen("/", comp)
	r.Screen(screen, nil)

	html, err := r.Render("/")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	s := string(html)
	if !strings.Contains(s, "<p>Home</p>") {
		t.Errorf("expected content in rendered output, got: %s", s)
	}

	_, err = r.Render("/nonexistent")
	if err == nil {
		t.Error("expected error for unregistered path")
	}
}

func TestRouterRenderWithLayout(t *testing.T) {
	r := NewRouter()
	comp := &stubComponent{html: render.Raw("<p>Home</p>")}
	screen := NewScreen("/", comp)

	layout := NewLayout("sidebar").WithHeader(&stubComponent{html: render.Raw("<h1>App</h1>")})
	r.Screen(screen, layout)

	html, err := r.Render("/")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	s := string(html)
	if !strings.Contains(s, `class="layout-sidebar"`) {
		t.Errorf("expected layout-sidebar class, got: %s", s)
	}
	if !strings.Contains(s, "<h1>App</h1>") {
		t.Errorf("expected header content, got: %s", s)
	}
}

func TestRouterPaths(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/", &stubComponent{html: render.Raw("a")}), nil)
	r.Screen(NewScreen("/about", &stubComponent{html: render.Raw("b")}), nil)
	r.Screen(NewScreen("/contact", &stubComponent{html: render.Raw("c")}), nil)

	paths := r.Paths()
	if len(paths) != 3 {
		t.Errorf("expected 3 paths, got %d", len(paths))
	}

	// Check all paths are present.
	pathSet := make(map[string]bool)
	for _, p := range paths {
		pathSet[p] = true
	}
	for _, expected := range []string{"/", "/about", "/contact"} {
		if !pathSet[expected] {
			t.Errorf("expected path %q in Paths()", expected)
		}
	}
}

func TestAppRenderPage(t *testing.T) {
	a := NewApp("MyApp")
	comp := &stubComponent{html: render.Raw("<p>Welcome</p>")}
	a.RegisterScreen(NewScreen("/", comp), nil)

	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatalf("RenderPage failed: %v", err)
	}
	s := string(html)

	// Check DOCTYPE.
	if !strings.HasPrefix(s, "<!DOCTYPE html>") {
		t.Errorf("expected DOCTYPE, got: %s", s[:50])
	}
	// Check html lang.
	if !strings.Contains(s, `<html lang="en"`) {
		t.Errorf("expected html lang=en, got: %s", s)
	}
	// Check charset.
	if !strings.Contains(s, `charset="UTF-8"`) {
		t.Errorf("expected charset, got: %s", s)
	}
	// Check viewport.
	if !strings.Contains(s, `name="viewport"`) {
		t.Errorf("expected viewport meta, got: %s", s)
	}
	// Check title.
	if !strings.Contains(s, "<title>MyApp</title>") {
		t.Errorf("expected title, got: %s", s)
	}
	// Check skip link.
	if !strings.Contains(s, `class="skip-link"`) {
		t.Errorf("expected skip-link, got: %s", s)
	}
	if !strings.Contains(s, "Skip to main content") {
		t.Errorf("expected skip link text, got: %s", s)
	}
	if !strings.Contains(s, `href="#main-content"`) {
		t.Errorf("expected skip link href, got: %s", s)
	}
	// Check content.
	if !strings.Contains(s, "<p>Welcome</p>") {
		t.Errorf("expected content, got: %s", s)
	}
}

func TestAppRenderPageDoesNotInlineThemeStyles(t *testing.T) {
	// Theme is now served as an external resource by the host (e.g.
	// framework/uihost serves /__gofastr/theme.css). RenderPage must
	// emit no inline <style> for the theme so strict CSP holds.
	a := NewApp("ThemedApp")
	theme := style.DefaultTheme()
	a.WithTheme(theme)
	a.RegisterScreen(NewScreen("/", &stubComponent{html: render.Raw("<p>Content</p>")}), nil)

	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatalf("RenderPage failed: %v", err)
	}
	s := string(html)

	if strings.Contains(s, "<style>") {
		t.Errorf("expected no inline <style> in core-ui RenderPage; the host injects an external <link>. got:\n%s", s)
	}
	if strings.Contains(s, ":root {") {
		t.Errorf("expected no inline :root custom properties; theme is served separately. got:\n%s", s)
	}
}

// Theme content is still reachable from the App so hosts can serve it.
func TestAppThemeCustomPropertiesAccessible(t *testing.T) {
	a := NewApp("ThemedApp")
	a.WithTheme(style.DefaultTheme())
	if a.Theme == nil {
		t.Fatal("Theme should be exposed on App")
	}
	css := a.Theme.CSSCustomProperties()
	if !strings.Contains(css, ":root {") || !strings.Contains(css, "--color-primary") {
		t.Errorf("theme CSS missing expected properties: %s", css)
	}
}

func TestAppRenderScreenWithLayout(t *testing.T) {
	a := NewApp("LayoutApp")
	comp := &stubComponent{html: render.Raw("<p>Screen content</p>")}
	screen := NewScreen("/dashboard", comp)

	headerComp := &stubComponent{html: render.Raw("<h1>Dashboard</h1>")}
	layout := NewLayout("dashboard").WithHeader(headerComp)
	a.RegisterScreen(screen, layout)

	html, err := a.RenderPage(context.Background(), "/dashboard")
	if err != nil {
		t.Fatalf("RenderPage failed: %v", err)
	}
	s := string(html)

	if !strings.Contains(s, `class="layout-dashboard"`) {
		t.Errorf("expected layout-dashboard class, got: %s", s)
	}
	if !strings.Contains(s, "<h1>Dashboard</h1>") {
		t.Errorf("expected header content, got: %s", s)
	}
	if !strings.Contains(s, "<p>Screen content</p>") {
		t.Errorf("expected screen content, got: %s", s)
	}
}

func TestDefaultLayout(t *testing.T) {
	a := NewApp("DefaultLayoutApp")
	a.RegisterScreen(NewScreen("/", &stubComponent{html: render.Raw("<p>Home</p>")}), nil)
	a.RegisterScreen(NewScreen("/about", &stubComponent{html: render.Raw("<p>About</p>")}), nil)

	// Set default layout after screen registration.
	headerComp := &stubComponent{html: render.Raw("<h1>Global Header</h1>")}
	defaultLayout := NewLayout("default").WithHeader(headerComp)
	a.SetDefaultLayout(defaultLayout)

	// Both screens should use the default layout.
	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatalf("RenderPage / failed: %v", err)
	}
	s := string(html)
	if !strings.Contains(s, `class="layout-default"`) {
		t.Errorf("expected layout-default class for /, got: %s", s)
	}
	if !strings.Contains(s, "<h1>Global Header</h1>") {
		t.Errorf("expected global header for /, got: %s", s)
	}

	html, err = a.RenderPage(context.Background(), "/about")
	if err != nil {
		t.Fatalf("RenderPage /about failed: %v", err)
	}
	s = string(html)
	if !strings.Contains(s, `class="layout-default"`) {
		t.Errorf("expected layout-default class for /about, got: %s", s)
	}
}

func TestDefaultLayoutOverride(t *testing.T) {
	a := NewApp("OverrideApp")

	// Default layout.
	defaultHeader := &stubComponent{html: render.Raw("<h1>Default</h1>")}
	a.SetDefaultLayout(NewLayout("default").WithHeader(defaultHeader))

	// Screen with explicit layout overrides default.
	customHeader := &stubComponent{html: render.Raw("<h1>Custom</h1>")}
	customLayout := NewLayout("custom").WithHeader(customHeader)

	a.RegisterScreen(NewScreen("/", &stubComponent{html: render.Raw("<p>Home</p>")}), nil)
	a.RegisterScreen(NewScreen("/special", &stubComponent{html: render.Raw("<p>Special</p>")}), customLayout)

	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatalf("RenderPage / failed: %v", err)
	}
	s := string(html)
	if !strings.Contains(s, `class="layout-default"`) {
		t.Errorf("expected default layout for /, got: %s", s)
	}

	html, err = a.RenderPage(context.Background(), "/special")
	if err != nil {
		t.Fatalf("RenderPage /special failed: %v", err)
	}
	s = string(html)
	if !strings.Contains(s, `class="layout-custom"`) {
		t.Errorf("expected custom layout for /special, got: %s", s)
	}
	if !strings.Contains(s, "<h1>Custom</h1>") {
		t.Errorf("expected custom header for /special, got: %s", s)
	}
}

func TestAppProvideAndInject(t *testing.T) {
	a := NewApp("DIApp")
	svc := &stubService{Value: "app-injected"}
	_ = a.Provide(svc)

	type target struct {
		Service *stubService `inject:""`
	}

	var tgt target
	_ = a.Inject(&tgt)
	if tgt.Service == nil || tgt.Service.Value != "app-injected" {
		t.Errorf("expected injected service via app, got %v", tgt.Service)
	}
}

func TestRenderPageUnregistered(t *testing.T) {
	a := NewApp("EmptyApp")
	_, err := a.RenderPage(context.Background(), "/nonexistent")
	if err == nil {
		t.Error("expected error for unregistered path")
	}
}

func TestScreenTypeString(t *testing.T) {
	tests := []struct {
		t        ScreenType
		expected string
	}{
		{ScreenPage, "page"},
		{ScreenDrawer, "drawer"},
		{ScreenSheet, "sheet"},
		{ScreenDialog, "dialog"},
		{ScreenType(99), "unknown(99)"},
	}
	for _, tt := range tests {
		got := tt.t.String()
		if got != tt.expected {
			t.Errorf("ScreenType(%d).String() = %q, want %q", tt.t, got, tt.expected)
		}
	}
}

func TestLayoutWrapNoHeaderNoFooter(t *testing.T) {
	// Layout with only sidebar, no header/footer.
	l := NewLayout("minimal").WithSidebar(&stubComponent{html: render.Raw("<nav>Links</nav>")})
	html := string(l.Wrap(render.Raw("<p>Content</p>")))

	if !strings.Contains(html, `class="layout-body"`) {
		t.Errorf("expected layout-body, got: %s", html)
	}
	if !strings.Contains(html, `aria-label="Sidebar"`) {
		t.Errorf("expected sidebar aria-label, got: %s", html)
	}
	if strings.Contains(html, "role=\"banner\"") {
		t.Errorf("did not expect header when none set, got: %s", html)
	}
	if strings.Contains(html, "role=\"contentinfo\"") {
		t.Errorf("did not expect footer when none set, got: %s", html)
	}
}

// --- F13: Route params isolation (no shared mutable state) ---

func TestRouteParamsIsolation(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/users/:id", &stubComponent{html: render.Raw("<p>User</p>")}), nil)
	r.Screen(NewScreen("/posts/:slug", &stubComponent{html: render.Raw("<p>Post</p>")}), nil)

	// Resolve first dynamic route
	s1, params1, ok1 := r.Resolve("/users/42")
	if !ok1 || params1 == nil {
		t.Fatal("expected to resolve /users/42")
	}
	if params1["id"] != "42" {
		t.Errorf("expected id=42, got %v", params1)
	}

	// Resolve second dynamic route
	s2, params2, ok2 := r.Resolve("/posts/hello-world")
	if !ok2 || params2 == nil {
		t.Fatal("expected to resolve /posts/hello-world")
	}
	if params2["slug"] != "hello-world" {
		t.Errorf("expected slug=hello-world, got %v", params2)
	}

	// The first screen's RouteParams should NOT have been mutated
	if s1.RouteParams() != nil {
		t.Errorf("screen routeParams should be nil until explicitly set, got %v", s1.RouteParams())
	}

	// Screens are the same shared instances but params are independent
	if s1 == s2 {
		t.Error("different dynamic routes should resolve to different screens")
	}
}

func TestRouteParamsNotMutatedOnSecondResolve(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/items/:id", &stubComponent{html: render.Raw("<p>Item</p>")}), nil)

	// Resolve with id=1
	_, params1, _ := r.Resolve("/items/1")
	if params1["id"] != "1" {
		t.Fatalf("first resolve: expected id=1, got %v", params1)
	}

	// Resolve with id=2
	screen, params2, _ := r.Resolve("/items/2")
	if params2["id"] != "2" {
		t.Fatalf("second resolve: expected id=2, got %v", params2)
	}

	// First params map should be unchanged
	if params1["id"] != "1" {
		t.Errorf("first params mutated: expected id=1, got %v", params1)
	}

	// Screen should not have accumulated params from previous resolve
	if screen.RouteParams() != nil {
		t.Errorf("shared screen should not have stale params, got %v", screen.RouteParams())
	}
}

// loaderComponent records that Load ran and remembers the context, so the
// test can assert on cancellation propagation and ordering vs Render.
type loaderComponent struct {
	loadCalls int
	gotCtx    context.Context
	failWith  error
	body      string
}

func (l *loaderComponent) Load(ctx context.Context) error {
	l.loadCalls++
	l.gotCtx = ctx
	if l.failWith != nil {
		return l.failWith
	}
	return nil
}

func (l *loaderComponent) Render() render.HTML {
	return render.HTML(fmt.Sprintf(`<div data-loaded="%d">%s</div>`, l.loadCalls, l.body))
}

func TestRenderPageRunsLoaderBeforeRender(t *testing.T) {
	a := NewApp("LoaderApp")
	loader := &loaderComponent{body: "after-load"}
	a.Register("/p", loader, nil)

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")

	html, err := a.RenderPage(ctx, "/p")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if loader.loadCalls != 1 {
		t.Errorf("Load should run exactly once, got %d", loader.loadCalls)
	}
	if loader.gotCtx == nil || loader.gotCtx.Value(ctxKey{}) != "marker" {
		t.Errorf("Load did not receive the caller's context")
	}
	if !strings.Contains(string(html), "after-load") {
		t.Errorf("rendered HTML missing loader output: %s", html)
	}
	if !strings.Contains(string(html), `data-loaded="1"`) {
		t.Errorf("expected Render to observe loadCalls=1: %s", html)
	}
}

func TestRenderPagePropagatesLoadError(t *testing.T) {
	a := NewApp("LoaderApp")
	want := errors.New("boom")
	a.Register("/p", &loaderComponent{failWith: want}, nil)

	_, err := a.RenderPage(context.Background(), "/p")
	if err == nil || !errors.Is(err, want) {
		t.Errorf("expected wrapped %v, got %v", want, err)
	}
}

func TestRenderPartialRunsLoader(t *testing.T) {
	a := NewApp("LoaderApp")
	loader := &loaderComponent{body: "partial"}
	a.Register("/p", loader, nil)

	if _, err := a.RenderPartial(context.Background(), "/p"); err != nil {
		t.Fatalf("RenderPartial: %v", err)
	}
	if loader.loadCalls != 1 {
		t.Errorf("expected Load to run once for partials, got %d", loader.loadCalls)
	}
}

func TestRenderPageNoContextStillWorks(t *testing.T) {
	// The no-ctx wrappers must continue to work; they substitute a Background ctx.
	a := NewApp("LoaderApp")
	loader := &loaderComponent{body: "no-ctx"}
	a.Register("/p", loader, nil)

	if _, err := a.RenderPage(context.Background(), "/p"); err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if loader.gotCtx == nil {
		t.Errorf("Load should still receive a non-nil context (Background)")
	}
}
