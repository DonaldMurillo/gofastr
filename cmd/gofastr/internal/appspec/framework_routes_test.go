package appspec

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
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
