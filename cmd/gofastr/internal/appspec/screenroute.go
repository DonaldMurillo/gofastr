package appspec

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
)

// screenRouteProblem says why a browser cannot reach a screen mounted at
// route, or returns "". A route is a path: it starts with "/", has no
// empty, "." or ".." segment (a browser folds those away before asking,
// and "//host" in a link is protocol-relative), and carries none of the
// characters a URL spends on something else (? starts the query, # the
// fragment, a backslash is a slash to a browser) or that a sitemap and
// llm.md cannot list raw (spaces and control bytes). A param named twice
// keeps only one of its values.
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

// screenRouteOnEntityAPI returns the CRUD mount path of the entity whose
// routes a screen at route would take, or "": the mount itself, where
// App.Mount panics at boot (entityScreenCollisionMessage), or any path
// under it, where the CRUD routes shadow the screen. Trailing slashes are
// trimmed, as the framework compares.
func screenRouteOnEntityAPI(bp AppSpec, route string) string {
	if bp.appless {
		// A fragment with no app: section (generate app --add, generate
		// screen) does not say the project's api_prefix, so it cannot
		// say where the CRUD routes mount.
		return ""
	}
	base := ""
	if p := strings.Trim(bp.App.APIPrefix, "/"); p != "" {
		base = "/" + p
	}
	for _, decl := range bp.Entities {
		if !EntityDeclarationCRUDEnabled(decl) || !project.IsGoIdentifier(ToCamelCase(decl.Name)) {
			continue
		}
		mount := base + "/" + entityTable(decl)
		// At the mount App.Mount panics; under it, CRUD's GET patterns
		// ({id}, a composite key's segments, llm.md, _events) are
		// registered routes and win over a screen UIHost serves from
		// its catch-all, so the page is never shown.
		if r := strings.TrimRight(route, "/"); r == mount || strings.HasPrefix(r, mount+"/") {
			return mount
		}
	}
	return ""
}

// frameworkRoutes are the GET routes a generated app's framework serves
// itself, each with what serves it and the app spec setting that makes
// the generated main mount it. A screen at one never renders (the
// registered route answers) or, where the framework's route answers only
// some callers, takes it over: a screen at /openapi.json served HTML to
// anonymous callers in place of the gated schema. The list follows the
// options the generated main passes (appgen's main.go emission:
// uihost.WithSitemap, WithRobots, WithAppIcon, WithPWA, WithPublicLLMMD,
// framework.WithMCP) and the routes those mount (framework/app.go
// finalizeRoutes, framework/uihost/uihost.go). Opt-in routes the
// generated main never enables (/metrics, /auth.md, /llms.txt) stay free.
var frameworkRoutes = []struct {
	path  string
	match int // routeExact, routeAtOrUnder or routeUnder
	owner string
	when  func(AppSpec) bool // nil: every generated app
}{
	{"/robots.txt", routeExact, "uihost.WithRobots", nil},
	{"/sitemap.xml", routeExact, "uihost.WithSitemap", nil},
	{"/sitemap", routeUnder, "uihost.WithSitemap's sitemap parts", nil},
	{"/favicon.ico", routeExact, "uihost.WithAppIcon", nil},
	{"/mcp", routeAtOrUnder, "the MCP endpoint, framework.WithMCP", nil},
	{"/.well-known", routeAtOrUnder, "the well-known discovery documents", nil},
	{"/__gofastr", routeAtOrUnder, "the UI runtime's own assets and RPC", nil},
	{"/openapi.json", routeExact, "the OpenAPI schema", appSpecHasEntities},
	{"/api/docs", routeAtOrUnder, "the API docs", appSpecHasEntities},
	{"/manifest.webmanifest", routeExact, "the PWA manifest", func(bp AppSpec) bool { return bp.App.PWA.Enabled }},
	{"/service-worker.js", routeExact, "the PWA service worker", func(bp AppSpec) bool { return bp.App.PWA.Enabled }},
	{"/llm-pages.md", routeExact, "the llm.md page index, app.llm_md", func(bp AppSpec) bool { return bp.App.LLMMD }},
}

const (
	routeExact = iota
	routeAtOrUnder
	routeUnder
)

func appSpecHasEntities(bp AppSpec) bool { return len(bp.Entities) > 0 }

// screenRouteOnFrameworkRoute says which framework-served route a screen
// at route would collide with, or returns "". Besides frameworkRoutes,
// with app.llm_md a last segment of llm.md is a page's markdown doc, and
// with app.admin the back-office owns its whole path, the
// <path>/e/<table> entity pages included.
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
	if bp.App.LLMMD && r[strings.LastIndexByte(r, '/')+1:] == "llm.md" {
		return "a page's llm.md document (app.llm_md)"
	}
	if bp.App.Admin.Enabled {
		admin := strings.TrimRight(bp.App.Admin.Path, "/")
		if admin == "" {
			admin = "/admin"
		}
		if r == admin || under(admin) {
			return fmt.Sprintf("%s (the admin back-office, which serves every entity at %s/e/<table>)", admin, admin)
		}
	}
	return ""
}
