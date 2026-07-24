package app

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ============================================================================
// ScreenLLMMD — basic page
// ============================================================================

func TestScreenLLMMD_BasicPage(t *testing.T) {
	screen := NewScreen("/", &basicComp{})
	screen.Title = "Home"
	screen.Description = "The homepage"

	md := ScreenLLMMD(screen)

	if !strings.Contains(md, "# Home") {
		t.Error("expected '# Home' header")
	}
	if !strings.Contains(md, "`/`") {
		t.Error("expected path '/'")
	}
	if !strings.Contains(md, "page") {
		t.Error("expected 'page' type")
	}
	if !strings.Contains(md, "The homepage") {
		t.Error("expected description")
	}
}

// ============================================================================
// ScreenLLMMD — dynamic route with params
// ============================================================================

func TestScreenLLMMD_DynamicRoute(t *testing.T) {
	screen := NewScreen("/products/:slug", &basicComp{})
	screen.Title = "Product Detail"

	md := ScreenLLMMD(screen)

	if !strings.Contains(md, "`slug`") {
		t.Error("expected 'slug' parameter")
	}
	if !strings.Contains(md, "**Params:**") {
		t.Error("expected Params line")
	}
}

// ============================================================================
// ScreenLLMMD — drawer type
// ============================================================================

func TestScreenLLMMD_DrawerType(t *testing.T) {
	screen := NewDrawer("/cart-drawer", &basicComp{})
	screen.Title = "Shopping Cart"

	md := ScreenLLMMD(screen)

	if !strings.Contains(md, "drawer") {
		t.Error("expected 'drawer' type")
	}
}

// ============================================================================
// ScreenLLMMD — dialog type
// ============================================================================

func TestScreenLLMMD_DialogType(t *testing.T) {
	screen := NewDialog("/confirm-delete", &basicComp{})
	screen.Title = "Confirm Delete"

	md := ScreenLLMMD(screen)

	if !strings.Contains(md, "dialog") {
		t.Error("expected 'dialog' type")
	}
}

// ============================================================================
// ScreenLLMMD — ScreenLoader capability detection
// ============================================================================

func TestScreenLLMMD_ScreenLoader(t *testing.T) {
	screen := NewScreen("/dashboard", &loaderComp{})
	screen.Title = "Dashboard"

	md := ScreenLLMMD(screen)

	if !strings.Contains(md, "ScreenLoader") {
		t.Error("expected ScreenLoader mention")
	}
	if !strings.Contains(md, "**Capabilities:**") {
		t.Error("expected Capabilities line")
	}
}

// ============================================================================
// ScreenLLMMD — StaticPathsProvider capability detection
// ============================================================================

func TestScreenLLMMD_StaticPathsProvider(t *testing.T) {
	screen := NewScreen("/docs/:slug", &staticPathsComp{})
	screen.Title = "Documentation"

	md := ScreenLLMMD(screen)

	if !strings.Contains(md, "StaticPathsProvider") {
		t.Error("expected StaticPathsProvider mention")
	}
}

// ============================================================================
// ScreenLLMMD — ScreenActions capability detection
// ============================================================================

func TestScreenLLMMD_ScreenActions(t *testing.T) {
	screen := NewScreen("/editor", &actionsComp{})
	screen.Title = "Editor"

	md := ScreenLLMMD(screen)

	if !strings.Contains(md, "ScreenActions") {
		t.Error("expected ScreenActions capability")
	}
}

// ============================================================================
// AppLLMMD — index with multiple screens
// ============================================================================

func TestAppLLMMD_Index(t *testing.T) {
	a := NewApp("Test App")
	layout := NewLayout("default")
	a.Register("/", &basicComp{}, layout)
	a.Register("/about", &basicComp{}, layout)
	a.Register("/products/:slug", &loaderComp{}, layout)
	a.RegisterScreen(NewDialog("/confirm", &basicComp{}), layout)

	md := AppLLMMD(a)

	if !strings.Contains(md, "# Test App") {
		t.Error("expected '# Test App' header")
	}
	if !strings.Contains(md, "## Pages") {
		t.Error("expected Pages section")
	}
	if !strings.Contains(md, "## Dynamic Routes") {
		t.Error("expected Dynamic Routes section")
	}
	if !strings.Contains(md, "/products/:slug") {
		t.Error("expected dynamic route pattern")
	}
	if !strings.Contains(md, "## Architecture") {
		t.Error("expected Architecture section")
	}
	// Root "/" should link to /llm.md (not skipped)
	if !strings.Contains(md, "/llm.md") {
		t.Error("expected root page to link to /llm.md")
	}
}

// ============================================================================
// AppLLMMD — empty app
// ============================================================================

func TestAppLLMMD_Empty(t *testing.T) {
	a := NewApp("Empty")
	md := AppLLMMD(a)

	if !strings.Contains(md, "No pages registered") {
		t.Error("expected empty message")
	}
	// Should have cross-link to API docs
	if !strings.Contains(md, "/api/llm.md") {
		t.Error("expected cross-link to /api/llm.md API docs")
	}
}

// ============================================================================
// ScreenLLMMDHandler — HTTP serving
// ============================================================================

func TestScreenLLMMDHandler_HTTP(t *testing.T) {
	screen := NewScreen("/about", &basicComp{})
	screen.Title = "About Us"

	handler := ScreenLLMMDHandler(screen)
	req := httptest.NewRequest("GET", "/about/llm.md", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/markdown") {
		t.Errorf("expected text/markdown, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "# About Us") {
		t.Error("expected '# About Us' in body")
	}
}

// ============================================================================
// AppLLMMDHandler — HTTP serving
// ============================================================================

func TestAppLLMMDHandler_HTTP(t *testing.T) {
	a := NewApp("Test")
	a.Register("/", &basicComp{}, nil)

	handler := AppLLMMDHandler(a)
	req := httptest.NewRequest("GET", "/llm-pages.md", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/markdown") {
		t.Errorf("expected text/markdown, got %q", ct)
	}
}

// ============================================================================
// extractParamNames
// ============================================================================

func TestExtractParamNames(t *testing.T) {
	tests := []struct {
		path string
		want []string
	}{
		{"/", nil},
		{"/about", nil},
		{"/products/:slug", []string{"slug"}},
		{"/users/:userId/posts/:postId", []string{"userId", "postId"}},
	}
	for _, tt := range tests {
		got := extractParamNames(tt.path)
		if len(got) != len(tt.want) {
			t.Errorf("extractParamNames(%q) = %v, want %v", tt.path, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("extractParamNames(%q)[%d] = %q, want %q", tt.path, i, got[i], tt.want[i])
			}
		}
	}
}

// ============================================================================
// ScreenLLMMD — content rendering
// ============================================================================

func TestScreenLLMMD_ContentRendered(t *testing.T) {
	comp := renderComp{html: "<h2>Section One</h2><p>Hello world</p><ul><li>Item A</li><li>Item B</li></ul>"}
	screen := NewScreen("/test", &comp)
	screen.Title = "Test Page"

	md := ScreenLLMMD(screen)

	if !strings.Contains(md, "## Page Content") {
		t.Error("expected Page Content section")
	}
	if !strings.Contains(md, "## Section One") {
		t.Error("expected h2 converted to markdown heading")
	}
	if !strings.Contains(md, "Hello world") {
		t.Error("expected paragraph text")
	}
	if !strings.Contains(md, "- Item A") {
		t.Error("expected list items")
	}
}

type renderComp struct{ html string }

func (c *renderComp) Render() render.HTML { return render.HTML(c.html) }

// ============================================================================
// Test components
// ============================================================================

type basicComp struct{}

func (c *basicComp) Render() render.HTML         { return render.HTML("<div>basic</div>") }
func (c *basicComp) SetParams(map[string]string) {}

// Ensure basicComp satisfies component.Component
var _ component.Component = (*basicComp)(nil)

type loaderComp struct{ basicComp }

func (c *loaderComp) Load(ctx context.Context) error { return nil }

// Ensure loaderComp satisfies ScreenLoader
var _ ScreenLoader = (*loaderComp)(nil)

type staticPathsComp struct{ basicComp }

func (c *staticPathsComp) StaticPaths(ctx context.Context) []map[string]string {
	return []map[string]string{
		{"slug": "getting-started"},
		{"slug": "api-reference"},
	}
}

type actionsComp struct{ basicComp }

func (c *actionsComp) Actions() {}

// ============================================================================
// Opt-out: per-screen and global NoLLMMD
// ============================================================================

func TestAppLLMMD_NoLLMMD_PerScreen(t *testing.T) {
	a := NewApp("Test")
	layout := NewLayout("default")
	a.Register("/about", &basicComp{}, layout)
	a.Register("/home", &basicComp{}, layout)

	// Opt out just /about
	screen, _, _ := a.Router.Resolve("/about")
	screen.NoLLMMD = true

	md := AppLLMMD(a)
	if strings.Contains(md, "/about") {
		t.Error("opted-out screen should not appear in index")
	}
	if !strings.Contains(md, "/home") {
		t.Error("non-opted-out screen should appear in index")
	}
}

func TestAppLLMMD_NoLLMMD_Global(t *testing.T) {
	a := NewApp("Test")
	a.NoLLMMD = true
	layout := NewLayout("default")
	a.Register("/about", &basicComp{}, layout)

	md := AppLLMMD(a)
	// Global NoLLMMD on App doesn't affect the index generation itself,
	// but mountPageLLMMD in uihost checks it to skip route registration.
	// The index still lists pages unless per-screen NoLLMMD is set.
	if !strings.Contains(md, "/about") {
		t.Error("global NoLLMMD should not affect index content, only route registration")
	}
}

// ============================================================================
// ScreenLLMMD — panic guard on Render()
// ============================================================================

type panicComp struct{}

func (c *panicComp) Render() render.HTML { panic("boom") }

func TestScreenLLMMD_PanicGuard(t *testing.T) {
	screen := NewScreen("/crash", &panicComp{})
	screen.Title = "Crash"

	// Should NOT panic — should produce a generic fallback message
	md := ScreenLLMMD(screen)
	if !strings.Contains(md, "# Crash") {
		t.Error("expected title to still appear")
	}
	if strings.Contains(md, "boom") {
		t.Error("panic value should NOT appear in output, got:", md)
	}
	if !strings.Contains(md, "error rendering") {
		t.Error("expected generic error fallback message, got:", md)
	}
}

// ============================================================================
// ScreenLLMMD — ScreenLoader gets dynamic content note
// ============================================================================

type emptyRenderLoader struct{ basicComp }

func (c *emptyRenderLoader) Load(ctx context.Context) error { return nil }

func TestScreenLLMMD_ScreenLoaderFallback(t *testing.T) {
	screen := NewScreen("/dashboard", &emptyRenderLoader{})
	screen.Title = "Dashboard"

	md := ScreenLLMMD(screen)
	if !strings.Contains(md, "dynamically") {
		t.Error("expected dynamic content note for ScreenLoader, got:", md)
	}
}

// ============================================================================
// ScreenLLMMDWithMeta — metadata prefix
// ============================================================================

func TestScreenLLMMDWithMeta_EmptyPrefixMatchesBase(t *testing.T) {
	screen := NewScreen("/", &basicComp{})
	screen.Title = "Home"
	screen.Description = "The homepage"

	got := ScreenLLMMDWithMeta(screen, "")
	want := ScreenLLMMD(screen)
	if got != want {
		t.Errorf("empty prefix must equal ScreenLLMMD output\ngot:  %q\nwant: %q", got, want)
	}
}

func TestScreenLLMMDWithMeta_InsertsPrefixBeforeTitle(t *testing.T) {
	screen := NewScreen("/", &basicComp{})
	screen.Title = "Home"
	screen.Description = "The homepage"

	meta := "---\ntitle: \"Home\"\ndescription: \"Front-matter desc\"\n---"
	md := ScreenLLMMDWithMeta(screen, meta)

	// Front-matter must appear at the very top of the document.
	if !strings.HasPrefix(md, meta+"\n") {
		t.Errorf("expected metaPrefix at the top, got:\n%s", md)
	}
	// The "# Home" heading must appear AFTER the front-matter.
	titleIdx := strings.Index(md, "# Home")
	fmEnd := strings.Index(md, meta) + len(meta)
	if titleIdx < fmEnd {
		t.Errorf("title must come after metaPrefix; titleIdx=%d fmEnd=%d\n%s",
			titleIdx, fmEnd, md)
	}
	// Existing body must still be present.
	if !strings.Contains(md, "## Route") {
		t.Errorf("expected '## Route' section to remain; got:\n%s", md)
	}
}

// --- ScreenLLMMDForPath: per-instance docs for concrete URLs ---

type loadedDocComp struct {
	slug  string
	title string
}

func (c *loadedDocComp) SetParams(m map[string]string) { c.slug = m["path"] }
func (c *loadedDocComp) Load(ctx context.Context) error {
	c.title = "Doc " + c.slug
	return nil
}
func (c *loadedDocComp) ScreenTitle() string { return c.title }
func (c *loadedDocComp) Render() render.HTML {
	return render.HTML("<h1>Doc " + c.slug + "</h1><p>Body of " + c.slug + ".</p>")
}

func TestScreenLLMMDForPathLoadsInstance(t *testing.T) {
	a := NewApp("t")
	a.Register("/docs/{path...}", &loadedDocComp{}, nil)

	doc, ok := ScreenLLMMDForPath(context.Background(), a, "/docs/getting-started")
	if !ok {
		t.Fatal("concrete path should resolve")
	}
	md, title := doc.MD, doc.Title
	if title != "Doc getting-started" {
		t.Errorf("title = %q, want the loaded title", title)
	}
	if !doc.Allowed || doc.Component == nil {
		t.Error("allowed render must report Allowed + the loaded Component")
	}
	if !strings.Contains(md, "Body of getting-started.") {
		t.Errorf("md must carry loaded content, not the ScreenLoader placeholder:\n%s", md)
	}
	if strings.Contains(md, "not available in static context") {
		t.Errorf("loaded doc must not contain the placeholder:\n%s", md)
	}
	// Two URLs of the same route produce distinct docs.
	doc2, _ := ScreenLLMMDForPath(context.Background(), a, "/docs/other")
	if md == doc2.MD {
		t.Error("distinct URLs must produce distinct docs")
	}
}

func TestScreenLLMMDForPathUnknown(t *testing.T) {
	a := NewApp("t")
	a.Register("/x", &basicComp{}, nil)
	if _, ok := ScreenLLMMDForPath(context.Background(), a, "/nope"); ok {
		t.Error("unresolvable path must return ok=false")
	}
}

func TestScreenLLMMDForPathHonorsPolicy(t *testing.T) {
	a := NewApp("t")
	deny := PolicyFunc(func(ctx context.Context) Decision {
		return Decision{Kind: DecisionRedirect, URL: "/login"}
	})
	a.RegisterScreen(NewScreen("/secret/{path}", &loadedDocComp{}).WithPolicy(deny), nil)

	doc, ok := ScreenLLMMDForPath(context.Background(), a, "/secret/42")
	if !ok {
		t.Fatal("gated path still resolves — it degrades, not vanishes")
	}
	md := doc.MD
	if doc.Allowed || doc.Component != nil {
		t.Error("gated render must not report Allowed or carry an instance")
	}
	if strings.Contains(md, "Body of 42.") {
		t.Errorf("policy-gated screen leaked loaded content via llm.md:\n%s", md)
	}
}

func TestLLMMDPolicyWithholdsNonLoaderContent(t *testing.T) {
	// A NON-ScreenLoader gated screen: the pattern-doc fallback renders
	// registration-instance content, so the withheld doc must be served
	// instead.
	a := NewApp("t")
	deny := PolicyFunc(func(ctx context.Context) Decision {
		return Decision{Kind: DecisionBlock, Status: 403}
	})
	a.RegisterScreen(NewScreen("/secret/{id}", &secretNonLoaderComp{body: "internal instructions"}).WithPolicy(deny), nil)

	doc, ok := ScreenLLMMDForPath(context.Background(), a, "/secret/42")
	if !ok {
		t.Fatal("gated path should degrade, not vanish")
	}
	md := doc.MD
	if strings.Contains(md, "internal instructions") {
		t.Errorf("non-loader gated screen leaked registration content:\n%s", md)
	}
	if !strings.Contains(md, "withheld") {
		t.Errorf("expected withheld marker:\n%s", md)
	}
}

type secretNonLoaderComp struct{ body string }

func (c *secretNonLoaderComp) SetParams(map[string]string) {}
func (c *secretNonLoaderComp) Render() render.HTML {
	return render.HTML("<p>" + c.body + "</p>")
}

// Sol round-2 finding 1: the withheld doc must be METADATA-free — the
// screen's title, description, and SEO bundle are component-supplied and
// protected by the same policy as the content.
func TestWithheldDocLeaksNoMetadata(t *testing.T) {
	a := NewApp("t")
	deny := PolicyFunc(func(ctx context.Context) Decision {
		return Decision{Kind: DecisionBlock, Status: 403}
	})
	scr := NewScreen("/secret", &secretNonLoaderComp{body: "internal instructions"}).WithPolicy(deny)
	scr.Title = "Project NIGHTFALL"
	scr.Description = "Acquisition target: Example Corp"
	a.RegisterScreen(scr, nil)

	doc, ok := ScreenLLMMDForPath(context.Background(), a, "/secret")
	if !ok {
		t.Fatal("gated path should degrade, not vanish")
	}
	for _, leak := range []string{"NIGHTFALL", "Acquisition", "internal instructions"} {
		if strings.Contains(doc.MD, leak) {
			t.Errorf("withheld doc leaked %q:\n%s", leak, doc.MD)
		}
	}
	if !strings.Contains(doc.MD, "/secret") {
		t.Errorf("withheld doc should still document the route path:\n%s", doc.MD)
	}
}

// The page index lists policy-gated screens metadata-free.
func TestAppLLMMDCtxGatesMetadata(t *testing.T) {
	a := NewApp("t")
	a.Register("/", &basicComp{}, nil)
	deny := PolicyFunc(func(ctx context.Context) Decision {
		return Decision{Kind: DecisionBlock, Status: 403}
	})
	scr := NewScreen("/secret", &basicComp{}).WithPolicy(deny)
	scr.Title = "Project NIGHTFALL"
	scr.Description = "Acquisition target: Example Corp"
	a.RegisterScreen(scr, nil)

	md := AppLLMMDCtx(context.Background(), a)
	if strings.Contains(md, "NIGHTFALL") || strings.Contains(md, "Acquisition") {
		t.Errorf("index leaked gated metadata:\n%s", md)
	}
	if !strings.Contains(md, "/secret") {
		t.Errorf("index should still list the gated route's path:\n%s", md)
	}
}
