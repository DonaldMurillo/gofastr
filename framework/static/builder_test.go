package static

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// homeScreen renders a fixed HTML body.
type homeScreen struct{}

func (homeScreen) ScreenTitle() string                { return "Home" }
func (homeScreen) ScreenDescription() string          { return "" }
func (homeScreen) ScreenType() coreapp.ScreenType     { return coreapp.ScreenPage }
func (homeScreen) Render() render.HTML                { return html.Heading(html.HeadingConfig{Level: 1}, render.Text("Home")) }

// loadingScreen sets a body string from Load(ctx) — ensures Load runs at SSG time.
type loadingScreen struct {
	Body string
}

func (l *loadingScreen) Load(ctx context.Context) error { l.Body = "loaded:" + ctx.Value(loadKey{}).(string); return nil }
func (l *loadingScreen) ScreenTitle() string             { return "Loaded" }
func (l *loadingScreen) ScreenDescription() string       { return "" }
func (l *loadingScreen) ScreenType() coreapp.ScreenType  { return coreapp.ScreenPage }
func (l *loadingScreen) Render() render.HTML             { return render.HTML("<p>" + l.Body + "</p>") }

type loadKey struct{}

// productScreen has a dynamic :slug param and supplies StaticPaths for SSG.
type productScreen struct {
	slug string
}

func (p *productScreen) SetParams(params map[string]string) { p.slug = params["slug"] }
func (p *productScreen) ScreenTitle() string                 { return "Product " + p.slug }
func (p *productScreen) ScreenDescription() string           { return "" }
func (p *productScreen) ScreenType() coreapp.ScreenType      { return coreapp.ScreenPage }
func (p *productScreen) Render() render.HTML                 { return render.HTML("<p>product-" + p.slug + "</p>") }
func (p *productScreen) StaticPaths(ctx context.Context) []map[string]string {
	return []map[string]string{
		{"slug": "alpha"},
		{"slug": "beta"},
	}
}

func TestBuildWritesIndexHTMLPerRoute(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/about", &homeScreen{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	res, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(res.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %v", res.Pages)
	}

	// Root → out/index.html ; /about → out/about/index.html.
	for _, rel := range []string{"index.html", "about/index.html"} {
		full := filepath.Join(out, rel)
		data, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
		if !strings.Contains(string(data), "<h1") {
			t.Errorf("%s does not contain rendered body", rel)
		}
		if strings.Contains(string(data), "gofastr-sse") {
			t.Errorf("%s should not include SSE meta tag — SSG pages are session-less", rel)
		}
		if !strings.Contains(string(data), `<script src="/__gofastr/runtime.js">`) {
			t.Errorf("%s missing runtime.js script tag", rel)
		}
	}
}

func TestBuildEmitsRuntimeJS(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "__gofastr", "runtime.js"))
	if err != nil {
		t.Fatalf("runtime.js missing: %v", err)
	}
	if len(data) == 0 {
		t.Error("runtime.js is empty")
	}
}

func TestBuildPropagatesLoaderContext(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/p", &loadingScreen{}, nil)
	host := uihost.New(a)

	ctx := context.WithValue(context.Background(), loadKey{}, "marker")
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(ctx); err != nil {
		t.Fatalf("Build: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "p", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "loaded:marker") {
		t.Errorf("Load did not run with the caller's context: %s", data)
	}
}

func TestBuildExpandsDynamicRoutesViaStaticPaths(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/products/:slug", &productScreen{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	res, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(res.Pages) != 2 {
		t.Fatalf("expected 2 expanded pages, got %v", res.Pages)
	}
	for _, slug := range []string{"alpha", "beta"} {
		full := filepath.Join(out, "products", slug, "index.html")
		data, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("missing %s: %v", full, err)
		}
		if !strings.Contains(string(data), "product-"+slug) {
			t.Errorf("%s missing slug body: %s", full, data)
		}
	}
}

func TestBuildSkipsDynamicRoutesWithoutStaticPaths(t *testing.T) {
	// No StaticPaths method on this screen — should be skipped silently.
	type plainProduct struct{ productScreenWithoutPaths }
	a := coreapp.NewApp("SSGTest")
	a.Register("/items/:id", &plainProduct{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	res, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(res.Pages) != 0 {
		t.Errorf("expected 0 pages, got %v", res.Pages)
	}
}

type productScreenWithoutPaths struct{}

func (productScreenWithoutPaths) Render() render.HTML            { return render.HTML("ignored") }
func (productScreenWithoutPaths) ScreenTitle() string             { return "" }
func (productScreenWithoutPaths) ScreenDescription() string       { return "" }
func (productScreenWithoutPaths) ScreenType() coreapp.ScreenType  { return coreapp.ScreenPage }

// ============================================================================
// SSG respects NoLLMMD opt-out
// ============================================================================

type noopScreen struct{}

func (noopScreen) Render() render.HTML  { return render.HTML("<p>noop</p>") }
func (noopScreen) ScreenTitle() string  { return "Noop" }
func (noopScreen) ScreenDescription() string { return "" }
func (noopScreen) ScreenType() coreapp.ScreenType { return coreapp.ScreenPage }

func TestBuild_NoLLMMD_PerScreen(t *testing.T) {
	a := coreapp.NewApp("NoLLMMDTest")
	a.Register("/public", &noopScreen{}, nil)
	a.Register("/secret", &noopScreen{}, nil)

	// Opt out /secret
	screen, _, _ := a.Router.Resolve("/secret")
	screen.NoLLMMD = true

	host := uihost.New(a)
	out := t.TempDir()
	_, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// /public should have llm.md
	if _, err := os.ReadFile(filepath.Join(out, "public", "llm.md")); err != nil {
		t.Error("expected public/llm.md to exist")
	}
	// /secret should NOT have llm.md
	if _, err := os.ReadFile(filepath.Join(out, "secret", "llm.md")); err == nil {
		t.Error("expected secret/llm.md to NOT exist")
	}
}

// TestBuildExpandsDynamicRoutesForLLMMD asserts that per-page llm.md files
// for dynamic routes are written under the concrete expanded paths (matching
// the HTML SSG structure), not under literal ":param" directories. A literal
// ":slug" directory is invalid on Windows and never URL-reachable elsewhere.
func TestBuildExpandsDynamicRoutesForLLMMD(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/products/:slug", &productScreen{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// One llm.md per concrete slug, mirroring the index.html layout.
	for _, slug := range []string{"alpha", "beta"} {
		full := filepath.Join(out, "products", slug, "llm.md")
		if _, err := os.ReadFile(full); err != nil {
			t.Errorf("missing %s: %v", full, err)
		}
	}

	// The literal ":slug" path must not be written.
	if _, err := os.Stat(filepath.Join(out, "products", ":slug")); err == nil {
		t.Errorf("SSG wrote a literal :slug directory — dynamic routes must be expanded")
	}
}

func TestBuild_NoLLMMD_GlobalApp(t *testing.T) {
	a := coreapp.NewApp("NoLLMMDGlobalTest")
	a.NoLLMMD = true
	a.Register("/home", &noopScreen{}, nil)

	host := uihost.New(a)
	out := t.TempDir()
	_, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// No llm.md should exist for any page
	if _, err := os.ReadFile(filepath.Join(out, "home", "llm.md")); err == nil {
		t.Error("expected home/llm.md to NOT exist with global NoLLMMD")
	}
	// No llm-pages.md index
	if _, err := os.ReadFile(filepath.Join(out, "llm-pages.md")); err == nil {
		t.Error("expected llm-pages.md to NOT exist with global NoLLMMD")
	}
}
