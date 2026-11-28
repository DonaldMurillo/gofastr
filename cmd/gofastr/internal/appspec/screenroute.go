package appspec

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
)

func screenRouteProblem(route string) string {
	if !strings.HasPrefix(route, "/") {
		return "must start with /"
	}
	if i := strings.IndexFunc(route, func(r rune) bool { return r <= ' ' || r == 0x7f }); i >= 0 {
		return "holds a space or control character"
	}
	for _, c := range []struct{ char, why string }{
		{"?", "holds ?, which starts a URL's query, so no path reaches the screen"},
		{"#", "holds #, which starts a URL's fragment, so no path reaches the screen"},
		{`\`, "holds a backslash, which a browser reads as a slash"},
	} {
		if strings.Contains(route, c.char) {
			return c.why
		}
	}
	if route == "/" {
		return ""
	}
	seen := map[string]bool{}
	for i, seg := range strings.Split(strings.TrimSuffix(route, "/"), "/")[1:] {
		switch seg {
		case "":
			if i == 0 {
				return "has an empty segment: a link to //host leaves the site"
			}
			return "has an empty segment"
		case ".", "..":
			return `has a "." or ".." segment, which a browser folds away before asking`
		}
		if name, ok := RouteSegParam(seg); ok {
			if seen[name] {
				return fmt.Sprintf("names {%s} twice, so one value is lost", name)
			}
			seen[name] = true
		}
	}
	return ""
}

func screenRouteOnEntityAPI(bp AppSpec, route string) string {
	if bp.appless && bp.projectApp == nil {
		return ""
	}
	base := ""
	if p := strings.Trim(bp.RouteApp().APIPrefix, "/"); p != "" {
		base = "/" + p
	}
	for _, decl := range bp.Entities {
		if !EntityDeclarationCRUDEnabled(decl) || !project.IsGoIdentifier(ToCamelCase(decl.Name)) {
			continue
		}
		mount := base + "/" + entityTable(decl)
		if r := strings.TrimRight(route, "/"); r == mount || strings.HasPrefix(r, mount+"/") {
			return mount
		}
	}
	return ""
}

var frameworkRoutes = []struct {
	path   string
	match  int // routeExact, routeAtOrUnder or routeUnder: screen reservations
	owner  string
	when   func(AppSpec) bool // nil: every generated app
	served []string           // actual path patterns for a broad reservation; exact routes use path
}{
	{"/healthz", routeExact, "framework health check", nil, nil},
	{"/readyz", routeExact, "framework readiness check", nil, nil},
	{"/api/llm.md", routeExact, "the API entity index", func(bp AppSpec) bool {
		return appSpecHasEntities(bp) && (bp.RouteApp().EntityLLMMD == nil || *bp.RouteApp().EntityLLMMD)
	}, nil},
	{"/robots.txt", routeExact, "uihost.WithRobots", nil, nil},
	{"/sitemap.xml", routeExact, "uihost.WithSitemap", nil, nil},
	{"/sitemap", routeUnder, "uihost.WithSitemap's sitemap parts", nil, []string{"/sitemap/{part}"}},
	{"/favicon.ico", routeExact, "uihost.WithAppIcon", nil, nil},
	{"/mcp", routeAtOrUnder, "the MCP endpoint, framework.WithMCP", nil, []string{"/mcp", "/mcp/server-card"}},
	{"/.well-known", routeAtOrUnder, "the well-known discovery documents", nil, []string{
		"/.well-known/mcp/catalog.json", "/.well-known/mcp/server-card.json", "/.well-known/mcp.json", "/.well-known/agent-skills/index.json",
	}},
	{"/.well-known/api-catalog", routeExact, "the API discovery catalog", appSpecHasEntities, nil},
	{"/__gofastr", routeAtOrUnder, "the UI runtime's own assets and RPC", nil, []string{
		"/__gofastr/runtime.js", "/__gofastr/color-scheme.js", "/__gofastr/actions.js", "/__gofastr/manifest.js", "/__gofastr/app.css", "/__gofastr/sse",
		"/__gofastr/session", "/__gofastr/action", "/__gofastr/widget/{id}", "/__gofastr/comp/{path...}", "/__gofastr/comp-bundle.css", "/__gofastr/widgets",
		"/__gofastr/runtime/{name}", "/__gofastr/compute/{name}", "/__gofastr/icons/icon-32.png", "/__gofastr/icons/icon-180.png", "/__gofastr/icons/icon-192.png", "/__gofastr/icons/icon-512.png",
	}},
	{"/openapi.json", routeExact, "the OpenAPI schema", appSpecHasEntities, nil},
	{"/api/docs", routeAtOrUnder, "the API docs", appSpecHasEntities, []string{"/api/docs/"}},
	{"/manifest.webmanifest", routeExact, "the PWA manifest", func(bp AppSpec) bool { return bp.RouteApp().PWA.Enabled }, nil},
	{"/service-worker.js", routeExact, "the PWA service worker", func(bp AppSpec) bool { return bp.RouteApp().PWA.Enabled }, nil},
	{"/llm-pages.md", routeExact, "the llm.md page index, app.llm_md", func(bp AppSpec) bool { return bp.RouteApp().LLMMD }, nil},
}

const (
	routeExact = iota
	routeAtOrUnder
	routeUnder
)

func appSpecHasEntities(bp AppSpec) bool { return len(bp.Entities) > 0 }

func screenRouteOnFrameworkRoute(bp AppSpec, route string) string {
	r := strings.TrimRight(route, "/")
	if r == "" {
		return ""
	}
	under := func(base string) bool { return strings.HasPrefix(r, base+"/") }
	for _, fr := range frameworkRoutes {
		if fr.when != nil && !fr.when(bp) {
			continue
		}
		if (fr.match != routeUnder && r == fr.path) || (fr.match != routeExact && under(fr.path)) {
			return fmt.Sprintf("%s (%s)", fr.path, fr.owner)
		}
	}
	app := bp.RouteApp()
	if app.LLMMD && r[strings.LastIndexByte(r, '/')+1:] == "llm.md" {
		return "a page's llm.md document (app.llm_md)"
	}
	if app.Admin.Enabled {
		admin := strings.TrimRight(app.Admin.Path, "/")
		if admin == "" {
			admin = "/admin"
		}
		if r == admin || under(admin) {
			return fmt.Sprintf("%s (the admin back-office, which serves every entity at %s/e/<table>)", admin, admin)
		}
	}
	return ""
}

// AppSpecFrameworkRoutePatterns returns the path patterns a generated app's framework mounts for bp.
func AppSpecFrameworkRoutePatterns(bp AppSpec) []string {
	var paths []string
	for _, fr := range frameworkRoutes {
		if fr.when != nil && !fr.when(bp) {
			continue
		}
		if fr.match == routeExact {
			paths = append(paths, fr.path)
		} else {
			paths = append(paths, fr.served...)
		}
	}
	return paths
}
