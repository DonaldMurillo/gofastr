package appspec

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// A screen at a route the generated app's framework serves itself
// passed validate and then never rendered: /robots.txt answered the real
// robots file, /mcp the MCP stream, and /openapi.json, with
// public_openapi false, served the screen's HTML to anonymous callers in
// place of the gated schema. A screen at <admin path>/e/<table> took the
// admin back-office page for that entity.
func TestScreenAtFrameworkRouteRefused(t *testing.T) {
	posts := framework.EntityDeclaration{Name: "posts", Exposure: &framework.ExposureDeclaration{CRUD: new(true)}, Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}
	spec := func(route string) AppSpec {
		bp := AppSpec{
			App:      AppSpecApp{Name: "Routes"},
			Entities: []framework.EntityDeclaration{posts},
			Screens:  []AppSpecScreen{{Name: "page", Route: route, Body: []AppSpecBlock{{Type: "heading", Text: "Page"}}}},
		}
		bp.App.Admin = AppSpecAdmin{Enabled: true, Path: "/manage"}
		bp.App.LLMMD = true
		bp.App.PWA.Enabled = true
		return bp
	}
	for _, route := range []string{
		"/robots.txt", "/sitemap.xml", "/sitemap/2", "/mcp", "/mcp/", "/mcp/server-card",
		"/.well-known/mcp.json", "/.well-known/anything", "/openapi.json", "/api/docs",
		"/api/docs/x", "/llm-pages.md", "/llm.md", "/posts-page/llm.md", "/{x}/llm.md",
		"/favicon.ico", "/manifest.webmanifest", "/service-worker.js", "/__gofastr/x",
		"/manage", "/manage/", "/manage/e/posts", "/manage/e/{table}",
	} {
		err := ValidateAppSpec(spec(route))
		if err == nil || !strings.Contains(err.Error(), `screen "page"`) || !strings.Contains(err.Error(), "framework serves") {
			t.Errorf("route %q: got %v, want the framework-route refusal", route, err)
		}
	}
	for _, route := range []string{"/", "/about", "/mcpx", "/sitemap", "/robots", "/docs/llm", "/management", "/api/docsx", "/well-known", "/metrics", "/auth.md", "/llms.txt"} {
		if err := ValidateAppSpec(spec(route)); err != nil && strings.Contains(err.Error(), "framework serves") {
			t.Errorf("route %q refused: %v", route, err)
		}
	}
	// Routes an option mounts are free without it: no PWA, no llm_md,
	// no entities (so no OpenAPI schema).
	for _, route := range []string{"/manifest.webmanifest", "/service-worker.js", "/llm-pages.md", "/docs/llm.md", "/openapi.json"} {
		bp := spec(route)
		bp.App.LLMMD, bp.App.PWA.Enabled, bp.Entities = false, false, nil
		if err := ValidateAppSpec(bp); err != nil && strings.Contains(err.Error(), "framework serves") {
			t.Errorf("route %q refused without its option: %v", route, err)
		}
	}
	// The admin path is the default /admin when the admin is enabled with
	// no path, and free when the admin is off.
	bp := spec("/admin/e/posts")
	bp.App.Admin.Path = ""
	if err := ValidateAppSpec(bp); err == nil || !strings.Contains(err.Error(), "framework serves") {
		t.Errorf("default admin path: got %v", err)
	}
	bp.App.Admin = AppSpecAdmin{}
	if err := ValidateAppSpec(bp); err != nil && strings.Contains(err.Error(), "framework serves") {
		t.Errorf("admin off: /admin/e/posts refused: %v", err)
	}
}

// Use the real framework lifecycle and route registrations, with no generated
// app, listener, browser, or network request. Exact pattern membership prevents
// UIHost's catch-all from making a stale table entry appear served.
//
//gofastr:test-reads /framework/*.go /framework/uihost/*.go /core-ui/widget/*.go
func TestFrameworkRoutePatternsMatchRegistrations(t *testing.T) {
	for _, entities := range []bool{false, true} {
		for _, optional := range []bool{false, true} {
			t.Run(fmt.Sprintf("entities=%t/optional=%t", entities, optional), func(t *testing.T) {
				bp := AppSpec{App: AppSpecApp{Name: "Routes"}}
				bp.App.PWA.Enabled = optional
				bp.App.LLMMD = optional
				fw := framework.NewApp(framework.WithMCP(), framework.WithoutDefaultMiddleware())
				if entities {
					decl := framework.EntityDeclaration{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}
					bp.Entities = []framework.EntityDeclaration{decl}
					cfg, err := decl.Config()
					if err != nil {
						t.Fatal(err)
					}
					fw.Entity(decl.Name, cfg)
				}
				var icon bytes.Buffer
				if err := png.Encode(&icon, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
					t.Fatal(err)
				}
				opts := []uihost.Option{uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://example.com"}), uihost.WithRobots(uihost.RobotsConfig{}), uihost.WithAppIcon(icon.Bytes())}
				if optional {
					opts = append(opts, uihost.WithPWA(uihost.PWAConfig{Name: "Routes"}), uihost.WithPublicLLMMD())
				}
				fw.Mount(uihost.New(app.NewApp("Routes"), opts...))
				var registered []router.RegisteredRoute
				host := routeTableHost{inspect: func() { registered = fw.Router().Routes() }}
				if err := fw.Run(context.Background(), host); err != nil {
					t.Fatal(err)
				}
				got := map[string]bool{}
				for _, route := range registered {
					if route.Method == "GET" || route.Method == "" {
						got[route.Pattern] = true
					}
				}
				for _, pattern := range AppSpecFrameworkRoutePatterns(bp) {
					if !got[pattern] {
						t.Errorf("framework table claims GET %s, but framework did not register it", pattern)
					}
				}
			})
		}
	}
}

type routeTableHost struct{ inspect func() }

func (h routeTableHost) Run(context.Context, *framework.Session) error { h.inspect(); return nil }
func (routeTableHost) Stop(context.Context) error                      { return nil }
