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

func (c *basicComp) Render() render.HTML { return render.HTML("<div>basic</div>") }

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
