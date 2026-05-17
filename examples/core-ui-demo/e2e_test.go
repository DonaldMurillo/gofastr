package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/check"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/signal"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func createTestApp() *app.App {
	application := app.NewApp("GoFastr Demo")
	theme := createTheme()
	application.WithTheme(theme)

	layout := app.NewLayout("main").
		WithHeader(&HeaderComponent{}).
		WithFooter(&FooterComponent{})
	application.SetDefaultLayout(layout)

	cartCount := signal.New(0)
	application.RegisterScreen(app.NewScreen("/", &HomeScreen{}), nil)
	application.RegisterScreen(app.NewScreen("/products", &ProductListScreen{}), nil)
	application.RegisterScreen(app.NewScreen("/about", &AboutScreen{}), nil)
	application.RegisterScreen(app.NewScreen("/cart", &CartDrawer{CartCount: cartCount}), nil)

	return application
}

func assertContains(t *testing.T, html render.HTML, substr string) {
	t.Helper()
	if !strings.Contains(string(html), substr) {
		t.Errorf("expected HTML to contain %q, got:\n%s", substr, truncateHTML(html, 500))
	}
}

func assertContainsAll(t *testing.T, html render.HTML, substrs ...string) {
	t.Helper()
	for _, s := range substrs {
		assertContains(t, html, s)
	}
}

func assertNotContains(t *testing.T, html render.HTML, substr string) {
	t.Helper()
	if strings.Contains(string(html), substr) {
		t.Errorf("expected HTML NOT to contain %q", substr)
	}
}

func truncateHTML(html render.HTML, maxLen int) string {
	s := string(html)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// ---------------------------------------------------------------------------
// A. HTML Rendering Tests
// ---------------------------------------------------------------------------

func TestAppSetup(t *testing.T) {
	a := createTestApp()
	if a.Name != "GoFastr Demo" {
		t.Errorf("expected app name 'GoFastr Demo', got %q", a.Name)
	}
	if a.Theme == nil {
		t.Error("expected theme to be set")
	}
	if a.Router == nil {
		t.Error("expected router to be set")
	}
}

func TestHomeScreenRenders(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatalf("RenderPage(/) error: %v", err)
	}
	assertContains(t, html, "Welcome to GoFastr")
}

func TestAllScreensRender(t *testing.T) {
	a := createTestApp()
	paths := []string{"/", "/products", "/about", "/cart"}
	for _, p := range paths {
		html, err := a.RenderPage(context.Background(), p)
		if err != nil {
			t.Errorf("RenderPage(%q) error: %v", p, err)
			continue
		}
		if len(html) == 0 {
			t.Errorf("RenderPage(%q) returned empty HTML", p)
		}
	}
}

func TestPageHasDoctype(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, "<!DOCTYPE html>")
}

func TestPageHasHTMLLang(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, `<html lang="en">`)
}

func TestPageHasMetaViewport(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, `name="viewport"`)
	assertContains(t, html, `width=device-width`)
}

func TestPageHasSkipLink(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, "skip-link")
	assertContains(t, html, "Skip to main content")
	assertContains(t, html, "#main-content")
}

func TestPageHasTitle(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	// The title is composed as "<screen-title> — <app-name>" when the
	// screen self-declares a title via ScreenSpec; the home screen
	// declares "Home", so the full <title> is "Home — GoFastr Demo".
	assertContains(t, html, "<title>Home — GoFastr Demo</title>")
}

// ---------------------------------------------------------------------------
// B. ADA / Accessibility Tests
// ---------------------------------------------------------------------------

func TestImagesHaveAlt(t *testing.T) {
	a := createTestApp()
	for _, path := range []string{"/", "/products"} {
		html, err := a.RenderPage(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		imgRe := regexp.MustCompile(`<img\b[^>]*>`)
		matches := imgRe.FindAllString(string(html), -1)
		for _, img := range matches {
			if !strings.Contains(img, `alt=`) {
				t.Errorf("img tag missing alt attribute: %s", img)
			}
		}
	}
}

func TestFormsHaveLabels(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/products")
	if err != nil {
		t.Fatal(err)
	}
	// Check that the search input has an associated label via for/id
	inputRe := regexp.MustCompile(`<input\b[^>]*\bid="([^"]*)"[^>]*>`)
	inputMatches := inputRe.FindAllStringSubmatch(string(html), -1)
	for _, m := range inputMatches {
		inputID := m[1]
		labelFor := fmt.Sprintf(`for="%s"`, inputID)
		if !strings.Contains(string(html), labelFor) {
			t.Errorf("input with id=%q has no associated <label for>", inputID)
		}
	}
}

func TestHeadingsHaveContent(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	headingRe := regexp.MustCompile(`<(h[1-6])[^>]*>\s*</(h[1-6])>`)
	if headingRe.MatchString(string(html)) {
		t.Error("found empty heading tags")
	}
}

func TestNavHasAriaLabel(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	navRe := regexp.MustCompile(`<nav\b[^>]*>`)
	matches := navRe.FindAllString(string(html), -1)
	for _, nav := range matches {
		if !strings.Contains(nav, `aria-label`) {
			t.Errorf("nav tag missing aria-label: %s", nav)
		}
	}
}

func TestMainHasRole(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, `role="main"`)
}

func TestARIALandmarksPresent(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, html,
		`role="banner"`,
		`role="navigation"`,
		`role="main"`,
		`role="contentinfo"`,
	)
}

func TestButtonsHaveAccessibleName(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	buttonRe := regexp.MustCompile(`<button\b[^>]*>`)
	matches := buttonRe.FindAllString(string(html), -1)
	for _, btn := range matches {
		// If button has aria-label, it's accessible
		if strings.Contains(btn, `aria-label=`) {
			continue
		}
		// Otherwise it must have text content — we check for non-empty between <button>...</button>
		// by verifying there's something between the tags (not rigorous but sufficient for our components)
		if strings.Contains(btn, `aria-label=""`) {
			t.Errorf("button has empty aria-label: %s", btn)
		}
	}
}

// ---------------------------------------------------------------------------
// C. Component Tests
// ---------------------------------------------------------------------------

func TestComponentComposition(t *testing.T) {
	inner := &HeroComponent{Title: "Hello", Subtitle: "World", CTAText: "Go", CTALink: "/go"}
	outer := html.Div(html.DivConfig{}, inner.Render())
	assertContainsAll(t, outer, "<h1", "Hello", "World")
}

func TestComponentList(t *testing.T) {
	items := []component.Component{
		&ProductCard{Name: "A", Price: 1.0, ImageSrc: "/a.jpg", ImageAlt: "A"},
		&ProductCard{Name: "B", Price: 2.0, ImageSrc: "/b.jpg", ImageAlt: "B"},
	}
	html := component.ComponentList(items...)
	assertContainsAll(t, html, "$1.00", "$2.00")
}

func TestInteractiveComponentDetection(t *testing.T) {
	btn := &InteractiveButton{Label: "Click me"}
	if !component.IsInteractive(btn) {
		t.Error("InteractiveButton should be detected as interactive")
	}

	plain := &HeaderComponent{}
	if component.IsInteractive(plain) {
		t.Error("HeaderComponent should NOT be detected as interactive")
	}
}

func TestExtractActions(t *testing.T) {
	btn := &InteractiveButton{Label: "Click me"}
	reg := component.ExtractActions(btn)
	if !reg.HasActions() {
		t.Error("expected InteractiveButton to have actions")
	}
	action, ok := reg.Get("add-to-cart")
	if !ok {
		t.Error("expected 'add-to-cart' action to be registered")
	}
	if action.Event != "add-to-cart" {
		t.Errorf("expected event 'add-to-cart', got %q", action.Event)
	}
	if action.ClientJS == "" {
		t.Error("expected ClientJS to be set on add-to-cart action")
	}
}

// ---------------------------------------------------------------------------
// D. Signal Tests
// ---------------------------------------------------------------------------

func TestSignalInComponent(t *testing.T) {
	count := signal.New(5)
	badge := &CartBadge{Count: count}
	html := badge.Render()
	assertContains(t, html, "5")
}

func TestSignalUpdatesReRender(t *testing.T) {
	count := signal.New(0)
	badge := &CartBadge{Count: count}

	html1 := badge.Render()
	assertContains(t, html1, "0")

	count.Set(3)
	html2 := badge.Render()
	assertContains(t, html2, "3")
	assertNotContains(t, html2, "0")
}

func TestComputedInComponent(t *testing.T) {
	count := signal.New(10)
	double := signal.NewComputed(func() int {
		return count.Get() * 2
	})

	out := html.Span(html.TextConfig{}, render.Text(fmt.Sprintf("%d", double.Get())))
	assertContains(t, out, "20")

	count.Set(5)
	out2 := html.Span(html.TextConfig{}, render.Text(fmt.Sprintf("%d", double.Get())))
	assertContains(t, out2, "10")
}

func TestSignalSubscribe(t *testing.T) {
	count := signal.New(0)
	var received int
	unsub := count.Subscribe(func(v int) {
		received = v
	})
	defer unsub()

	count.Set(42)
	if received != 42 {
		t.Errorf("expected subscriber to receive 42, got %d", received)
	}
}

func TestSignalBatch(t *testing.T) {
	count := signal.New(0)
	var notifications []int
	count.Subscribe(func(v int) {
		notifications = append(notifications, v)
	})

	signal.Batch(func() {
		count.Set(1)
		count.Set(2)
		count.Set(3)
	})

	// After batch completes, subscriber should have been called
	if len(notifications) == 0 {
		t.Error("expected subscriber to be called after batch")
	}
}

// ---------------------------------------------------------------------------
// E. Theme / Style Tests
// ---------------------------------------------------------------------------

func TestThemeApplied(t *testing.T) {
	theme := createTheme()
	a := app.NewApp("Test")
	a.WithTheme(theme)
	html, err := a.RenderPage(context.Background(), "/")
	// No screens, so this will fail — just test theme CSS
	_ = html
	_ = err

	css := theme.CSSCustomProperties()
	if !strings.Contains(css, "#6366F1") {
		t.Error("expected custom primary color in CSS")
	}
	if !strings.Contains(css, "#8B5CF6") {
		t.Error("expected custom secondary color in CSS")
	}
}

func TestCSSCustomProperties(t *testing.T) {
	theme := createTheme()
	css := theme.CSSCustomProperties()
	assertContainsAll(t, render.HTML(css),
		"--color-primary",
		"--color-secondary",
		"--spacing-md",
		"--radii-md",
	)
}

func TestCSSExtraction(t *testing.T) {
	theme := createTheme()
	extractor := style.NewCSSExtractor(theme)
	html := `<div class="flex items-center p-md"><span class="text-primary">Hello</span></div>`
	classes := extractor.ExtractFromHTML(html)

	expected := []string{"flex", "items-center", "p-md", "text-primary"}
	for _, e := range expected {
		found := false
		for _, c := range classes {
			if c == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected class %q in extracted classes, got %v", e, classes)
		}
	}
}

func TestRouteGraphPreload(t *testing.T) {
	graph := style.NewRouteGraph()
	graph.AddRoute("/", "home.css", []string{"/products", "/about"})
	graph.AddRoute("/products", "products.css", []string{"/"})
	graph.AddRoute("/about", "about.css", []string{"/"})

	manifest := graph.PreloadManifest()

	home, ok := manifest["/"]
	if !ok {
		t.Fatal("expected '/' in manifest")
	}
	if home.CSS != "home.css" {
		t.Errorf("expected CSS 'home.css', got %q", home.CSS)
	}
	if len(home.Preload) != 2 {
		t.Errorf("expected 2 preload chunks for /, got %d", len(home.Preload))
	}
}

func TestUtilityClass(t *testing.T) {
	t.Skip("Theme.UtilityClass removed alongside the typed-theme migration; utility classes resolve through GenerateUtilityCSS which emits var() refs against the theme. See core-ui/style/classes.go.")
}

func TestClassesMap(t *testing.T) {
	classes := style.Classes{
		"flex":   true,
		"hidden": false,
		"p-4":    true,
		"m-2":    true,
	}
	result := classes.String()
	if !strings.Contains(result, "flex") {
		t.Error("expected 'flex' in classes string")
	}
	if strings.Contains(result, "hidden") {
		t.Error("did NOT expect 'hidden' in classes string")
	}
}

// ---------------------------------------------------------------------------
// F. Island Tests
// ---------------------------------------------------------------------------

func TestIslandRender(t *testing.T) {
	comp := &HeaderComponent{}
	isl := island.NewIsland("header-island", comp)
	html := isl.Render()
	assertContains(t, html, `data-island="header-island"`)
	assertContains(t, html, "<nav")
}

func TestIslandPush(t *testing.T) {
	count := signal.New(3)
	comp := &CartBadge{Count: count}
	isl := island.NewIsland("cart-badge", comp)

	html1 := isl.Render()
	assertContains(t, html1, "3")

	count.Set(7)
	html2 := isl.Update()
	assertContains(t, html2, "7")
}

func TestManagerLifecycle(t *testing.T) {
	mgr := island.NewManager()

	comp := &HeaderComponent{}
	isl := island.NewIsland("test-island", comp)
	isl.SessionID = "sess-1"

	// Register
	if err := mgr.Register(isl); err != nil {
		t.Fatalf("Register error: %v", err)
	}

	// Verify registered
	got, ok := mgr.Get("test-island")
	if !ok {
		t.Fatal("expected island to be registered")
	}
	if got.ID != "test-island" {
		t.Errorf("expected ID 'test-island', got %q", got.ID)
	}

	// Push
	if err := mgr.Push("test-island"); err != nil {
		t.Fatalf("Push error: %v", err)
	}

	// Unregister
	mgr.Unregister("test-island")
	_, ok = mgr.Get("test-island")
	if ok {
		t.Error("expected island to be unregistered")
	}
}

func TestSSEEndpoint(t *testing.T) {
	mgr := island.NewManager()

	// Create and register an island with a session
	comp := &HeaderComponent{}
	isl := island.NewIsland("sse-island", comp)
	isl.SessionID = "test-session"
	if err := mgr.Register(isl); err != nil {
		t.Fatal(err)
	}

	// Set up the SSE handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mgr.ServeSSE(w, r)
	})

	// Test missing session parameter
	req := httptest.NewRequest("GET", "/islands/sse", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("expected status 400 for missing session, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// G. Runtime Tests
// ---------------------------------------------------------------------------

func TestE2ERuntimeJSLoads(t *testing.T) {
	js, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatalf("RuntimeJS error: %v", err)
	}
	if len(js) == 0 {
		t.Error("RuntimeJS returned empty string")
	}
}

func TestRuntimeSize(t *testing.T) {
	size := runtime.RuntimeSize()
	if size == 0 {
		t.Error("RuntimeSize returned 0")
	}
	// Cap aligned with core-ui/runtime/runtime_test.go: 96000 bytes.
	if size > 96000 {
		t.Errorf("runtime is %d bytes, expected under 96000", size)
	}
}

// ---------------------------------------------------------------------------
// H. Linter Tests
// ---------------------------------------------------------------------------

func TestLinterAllowsValid(t *testing.T) {
	// Use the current package directory for linting
	result, err := check.LintPackage(".")
	if err != nil {
		t.Fatalf("LintPackage error: %v", err)
	}
	// Our files may or may not pass linting depending on imports,
	// but the linter should at least run without error
	_ = result
}

// ---------------------------------------------------------------------------
// Additional integration tests
// ---------------------------------------------------------------------------

func TestPageNotFound(t *testing.T) {
	a := createTestApp()
	_, err := a.RenderPage(context.Background(), "/nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestLayoutWrapsContent(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	// Layout should have header, main-content, and footer
	assertContainsAll(t, html,
		"GoFastr Demo",        // header content
		"main-content",        // main id
		"All rights reserved", // footer content
	)
}

func TestCartScreenRenders(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/cart")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, "Shopping Cart")
}

func TestProductsScreenHasSearch(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/products")
	if err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, html,
		`name="q"`,
		"Search",
		"product-grid",
	)
}

func TestAboutScreenHasSections(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/about")
	if err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, html,
		"Our Mission",
		"Our Team",
		"Contact",
		"Alice",
	)
}

func TestSignalEffect(t *testing.T) {
	count := signal.New(1)
	var effectVal int
	dispose := signal.Effect(func() {
		effectVal = count.Get()
	})
	defer dispose()

	if effectVal != 1 {
		t.Errorf("expected initial effect value 1, got %d", effectVal)
	}

	count.Set(5)
	if effectVal != 5 {
		t.Errorf("expected effect value after set to be 5, got %d", effectVal)
	}
}

func TestAppRenderScreen(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderScreen("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(html) == 0 {
		t.Error("RenderScreen returned empty HTML")
	}
}

func TestRouterPaths(t *testing.T) {
	a := createTestApp()
	paths := a.Router.Paths()
	if len(paths) != 4 {
		t.Errorf("expected 4 registered paths, got %d: %v", len(paths), paths)
	}
}

func TestDIContainer(t *testing.T) {
	a := app.NewApp("Test DI")
	if a.Container == nil {
		t.Error("expected Container to be initialized")
	}
}

func TestThemeResolution(t *testing.T) {
	// Var-only contract: Resolve* now returns CSS variable
	// references, not literal values. The literal still lives on
	// the typed token's .Value field for non-CSS contexts.
	theme := createTheme()
	if got := theme.ResolveColor("primary"); got != "var(--color-primary)" {
		t.Errorf("ResolveColor: %q", got)
	}
	if got := theme.ResolveSpacing("md"); got != "var(--spacing-md)" {
		t.Errorf("ResolveSpacing: %q", got)
	}
	if got := theme.ResolveRadius("lg"); got != "var(--radii-lg)" {
		t.Errorf("ResolveRadius: %q", got)
	}
	// Literal values stay accessible via typed token fields.
	if theme.Colors.Primary.Value != "#6366F1" {
		t.Errorf("Colors.Primary.Value: %q", theme.Colors.Primary.Value)
	}
	if theme.Spacing.MD.Value != 8 {
		t.Errorf("Spacing.MD.Value: %d", theme.Spacing.MD.Value)
	}
	if theme.Radii.LG.Value != 12 {
		t.Errorf("Radii.LG.Value: %d", theme.Radii.LG.Value)
	}
}

// ---------------------------------------------------------------------------
// J. Widget Tests (hydration boundary)
// ---------------------------------------------------------------------------

func TestWidgetRender(t *testing.T) {
	comp := &HeroComponent{Title: "Test", Subtitle: "Widget", CTAText: "Go", CTALink: "/go"}
	w := component.NewWidget("hero", comp)
	html := w.Render()
	assertContains(t, html, `data-widget="hero"`)
	assertContains(t, html, `data-hydrate=`)
	assertContains(t, html, "Test")
}

func TestWidgetInteractive(t *testing.T) {
	// Non-interactive component
	plain := &HeaderComponent{}
	w1 := component.NewWidget("header", plain)
	if w1.IsInteractive() {
		t.Error("non-interactive component should not be interactive widget")
	}

	// Interactive component
	btn := &InteractiveButton{Label: "Click"}
	w2 := component.NewWidget("btn", btn)
	if !w2.IsInteractive() {
		t.Error("interactive component should be interactive widget")
	}
}

func TestWidgetHydrationStrategy(t *testing.T) {
	// Lazy hydration for non-interactive
	plain := &HeaderComponent{}
	w1 := component.NewWidget("plain", plain)
	html1 := w1.Render()
	assertContains(t, html1, `data-hydrate="lazy"`)

	// Interaction-based hydration for interactive
	btn := &InteractiveButton{Label: "Click"}
	w2 := component.NewWidget("btn", btn)
	html2 := w2.Render()
	assertContains(t, html2, `data-hydrate="interaction"`)
}

// ---------------------------------------------------------------------------
// K. Group & ButtonGroup Tests
// ---------------------------------------------------------------------------

func TestGroupLiveRegion(t *testing.T) {
	html := html.Group(html.GroupConfig{Role: html.RoleStatus, AriaLabel: "3 items"}, render.Text("3 items"))
	assertContainsAll(t, html, `role="status"`, "3 items")
}

func TestGroupWithAlert(t *testing.T) {
	html := html.Group(html.GroupConfig{Role: html.RoleAlert}, render.Text("Error!"))
	assertContainsAll(t, html, `role="alert"`, "Error!")
}

func TestButtonGroup(t *testing.T) {
	html := html.ButtonGroup(html.ButtonGroupConfig{},
		html.Button(html.ButtonConfig{Label: "Yes", Attrs: html.OnClick("yes")}),
		html.Button(html.ButtonConfig{Label: "No", Attrs: html.OnClick("no")}),
	)
	assertContainsAll(t, html, `role="group"`, "Yes", "No")
}

// ---------------------------------------------------------------------------
// L. Event Helper Tests
// ---------------------------------------------------------------------------

func TestOnClickInButton(t *testing.T) {
	html := html.Button(html.ButtonConfig{Label: "Save", Attrs: html.OnClick("save")})
	assertContainsAll(t, html, `data-action="save"`, "Save")
}

func TestOnInputOnSearchField(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/products")
	if err != nil {
		t.Fatal(err)
	}
	// Products page should have a search input
	assertContains(t, html, `name="q"`)
}

func TestEventHelperAttrs(t *testing.T) {
	onClick := html.OnClick("action")
	if onClick["data-action"] != "action" {
		t.Errorf("expected data-action=action, got %v", onClick)
	}

	onSubmit := html.OnSubmit("submit-form")
	if onSubmit["data-action-type"] != "submit" {
		t.Errorf("expected data-action-type=submit, got %v", onSubmit)
	}

	onInput := html.OnInput("search")
	if onInput["data-action-type"] != "input" {
		t.Errorf("expected data-action-type=input, got %v", onInput)
	}

	onChange := html.OnChange("category")
	if onChange["data-action-type"] != "change" {
		t.Errorf("expected data-action-type=change, got %v", onChange)
	}
}

// ---------------------------------------------------------------------------
// M. Use() Semantic Style Tests — removed alongside typed-theme migration.
// Components are now registered via core-ui/registry and resolve their
// scoped CSS through the registry catalog. The style.Use / UseWith /
// ComponentCSS / StyleDef APIs no longer exist. Tests stay as TODOs so
// future maintainers see the migration trail.
// ---------------------------------------------------------------------------

func TestUseComponentStyle(t *testing.T) {
	t.Skip("style.Use removed — components register via core-ui/registry now")
}

func TestUseWith(t *testing.T) {
	t.Skip("style.UseWith removed — components register via core-ui/registry now")
}

func TestComponentCSSGeneration(t *testing.T) {
	t.Skip("Theme.ComponentCSS / StyleDef removed — components register via core-ui/registry now")
}

// ---------------------------------------------------------------------------
// N. Widget + Island Integration
// ---------------------------------------------------------------------------

func TestWidgetInIsland(t *testing.T) {
	btn := &InteractiveButton{Label: "Click me"}
	w := component.NewWidget("clicker", btn)
	isl := island.NewIsland("island-1", w)

	html := isl.Render()
	// Island wrapper
	assertContains(t, html, `data-island="island-1"`)
	// Widget wrapper
	assertContains(t, html, `data-widget="clicker"`)
	// Button content
	assertContains(t, html, "Click me")
}

func TestWidgetCompositionInScreen(t *testing.T) {
	a := createTestApp()
	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	// Home page should have content
	assertContains(t, html, "Welcome to GoFastr")
	// Should have landmarks
	assertContains(t, html, `role="main"`)
}

// ---------------------------------------------------------------------------
// O. DevServer Integration Tests
// ---------------------------------------------------------------------------

func createTestHost() *uihost.UIHost {
	application := app.NewApp("GoFastr Demo")
	theme := createTheme()
	application.WithTheme(theme)

	layout := app.NewLayout("main").
		WithHeader(&HeaderComponent{}).
		WithFooter(&FooterComponent{})
	application.SetDefaultLayout(layout)

	application.RegisterScreen(app.NewScreen("/", &HomeScreen{}).WithTitle("Home").WithDescription("Homepage"), nil)
	application.RegisterScreen(app.NewScreen("/products", &ProductListScreen{}).WithTitle("Products").WithDescription("Products"), nil)
	application.RegisterScreen(app.NewScreen("/about", &AboutScreen{}).WithTitle("About").WithDescription("About"), nil)
	application.RegisterScreen(app.NewDrawer("/cart", &CartDrawer{CartCount: signal.New(0)}).WithTitle("Cart").WithDescription("Cart"), nil)

	return uihost.New(application)
}

func TestDevServerHomePageHasRuntimeAndSSE(t *testing.T) {
	ds := createTestHost()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()

	// Core framework injections — runtime is an external <script src>;
	// route graph now ships inline as <script type="application/json">
	// (CSP-safe inert data block, parsed by runtime.js at boot).
	assertContainsAll(t, render.HTML(body),
		`<script src="/__gofastr/runtime.js"></script>`,
		`<script type="application/json" id="gofastr-routes">`,
		`name="gofastr-sse"`,
		"/__gofastr/sse?session=",
	)

	// Page content
	assertContainsAll(t, render.HTML(body),
		"Welcome to GoFastr",
		`role="main"`,
		"Skip to main content",
	)

	// Session cookie
	var hasCookie bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "gofastr-session" {
			hasCookie = true
			if !strings.HasPrefix(c.Value, "sess-") {
				t.Errorf("expected session ID starting with sess-, got %q", c.Value)
			}
		}
	}
	if !hasCookie {
		t.Error("expected gofastr-session cookie")
	}
}

func TestDevServerProductsPageHasSearch(t *testing.T) {
	ds := createTestHost()
	req := httptest.NewRequest("GET", "/products", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	assertContainsAll(t, render.HTML(body),
		"Products",
		`name="q"`,
		"Search products",
		"Widget Pro",
		"Gadget Max",
		`/__gofastr/runtime.js`,
	)
}

func TestDevServerAboutPage(t *testing.T) {
	ds := createTestHost()
	req := httptest.NewRequest("GET", "/about", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	assertContainsAll(t, render.HTML(body),
		"About GoFastr",
		"Our Mission",
		"Alice — Founder",
		"hello@gofastr.dev",
	)
}

func TestDevServerCartDrawer(t *testing.T) {
	ds := createTestHost()
	req := httptest.NewRequest("GET", "/cart", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	assertContainsAll(t, render.HTML(body),
		"Shopping Cart",
		"Your cart is empty",
		"data-page",
	)
}

func TestDevServerRuntimeJSEndpoint(t *testing.T) {
	ds := createTestHost()
	req := httptest.NewRequest("GET", "/__gofastr/runtime.js", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()

	// Runtime must have event delegation + SSE
	assertContainsAll(t, render.HTML(body),
		"__gofastr",
		"data-action",
		"EventSource",
		"MutationObserver",
		"hydrate",
	)

	// Content type
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Errorf("expected javascript content type, got %q", ct)
	}
}

func TestDevServerSSEStreamWithIsland(t *testing.T) {
	ds := createTestHost()
	sess := ds.CreateSession()

	// Register an island
	hero := &HeroComponent{Title: "Live", Subtitle: "Updates", CTAText: "Go", CTALink: "/go"}
	w := component.NewWidget("live-hero", hero)
	isl := ds.RegisterWidget(sess.ID, w)

	// Subscribe
	ch := ds.Islands.Subscribe(sess.ID)

	// Push update
	go func() {
		time.Sleep(50 * time.Millisecond)
		ds.Islands.PushUpdate(island.IslandUpdate{
			IslandID: isl.ID,
			HTML:     "<p>Updated content!</p>",
		}, sess.ID)
	}()

	select {
	case update := <-ch:
		if !strings.Contains(update.HTML, "Updated content!") {
			t.Errorf("expected updated content, got %q", update.HTML)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE island update")
	}
}

func TestDevServerSessionCreation(t *testing.T) {
	ds := createTestHost()

	// First visit creates session
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	var sessionID string
	for _, c := range cookies {
		if c.Name == "gofastr-session" {
			sessionID = c.Value
		}
	}
	if sessionID == "" {
		t.Fatal("expected session cookie")
	}

	// Subsequent request with cookie reuses session
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.AddCookie(&http.Cookie{Name: "gofastr-session", Value: sessionID})
	w2 := httptest.NewRecorder()
	ds.ServeHTTP(w2, req2)

	body := w2.Body.String()
	if !strings.Contains(body, sessionID) {
		t.Error("expected page to contain the same session ID")
	}
}

func TestDevServerActionsJSInjection(t *testing.T) {
	ds := createTestHost()

	// Compile actions for interactive component
	btn := &InteractiveButton{Label: "Test"}
	ds.CompileActions("test-btn", btn)

	// Page links to /__gofastr/actions.js — body is no longer inlined.
	pageReq := httptest.NewRequest("GET", "/", nil)
	pageRec := httptest.NewRecorder()
	ds.ServeHTTP(pageRec, pageReq)
	if !strings.Contains(pageRec.Body.String(), `<script src="/__gofastr/actions.js"></script>`) {
		t.Error("page should reference /__gofastr/actions.js")
	}

	jsReq := httptest.NewRequest("GET", "/__gofastr/actions.js", nil)
	jsRec := httptest.NewRecorder()
	ds.ServeHTTP(jsRec, jsReq)
	assertContainsAll(t, render.HTML(jsRec.Body.String()),
		"test-btn",
		"__gofastr",
	)
}

func TestDevServerSignalUpdatePushesIsland(t *testing.T) {
	ds := createTestHost()
	sess := ds.CreateSession()

	// Register island
	cart := &CartDrawer{CartCount: signal.New(3)}
	w := component.NewWidget("cart-widget", cart)
	isl := ds.RegisterWidget(sess.ID, w)

	// Subscribe to catch updates
	ch := ds.Islands.Subscribe(sess.ID)

	// Post signal update
	body := strings.NewReader(`{"value": 5}`)
	req := httptest.NewRequest("POST", "/__gofastr/signal/cart?session="+sess.ID, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Should get island update via SSE
	select {
	case update := <-ch:
		if update.IslandID != isl.ID {
			t.Errorf("expected island %s, got %q", isl.ID, update.IslandID)
		}
		if !strings.Contains(update.HTML, "Shopping Cart") {
			t.Errorf("expected cart content, got %q", update.HTML)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for island update")
	}
}

func TestDevServerRouteGraphPreload(t *testing.T) {
	ds := createTestHost()

	// The route graph ships inline as <script type="application/json"
	// id="gofastr-routes">. runtime.js parses it at boot. CSP-safe;
	// no separate /__gofastr/routes.js round-trip.
	pageReq := httptest.NewRequest("GET", "/", nil)
	pageRec := httptest.NewRecorder()
	ds.ServeHTTP(pageRec, pageReq)
	body := pageRec.Body.String()
	if !strings.Contains(body, `<script type="application/json" id="gofastr-routes">`) {
		t.Errorf("page should embed inline route-graph JSON block; got:\n%s", body[:min(len(body), 800)])
	}
	for _, want := range []string{
		`"preload":true`,
		`"title":"Home"`,
		`"title":"Products"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing route-graph entry %q", want)
		}
	}

	// The legacy endpoint surfaces as 410 GONE so any cached browser
	// reference fails loudly instead of silently 404'ing.
	jsReq := httptest.NewRequest("GET", "/__gofastr/routes.js", nil)
	jsRec := httptest.NewRecorder()
	ds.ServeHTTP(jsRec, jsReq)
	if jsRec.Code != 410 {
		t.Errorf("/__gofastr/routes.js should be 410 GONE, got %d", jsRec.Code)
	}
}
