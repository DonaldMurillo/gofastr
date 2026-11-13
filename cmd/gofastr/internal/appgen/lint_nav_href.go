package appgen

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	stdpath "path"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	corestatic "github.com/DonaldMurillo/gofastr/core/static"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

type navHrefFinding struct{ Path, Href string }

func (f navHrefFinding) Message() string {
	return fmt.Sprintf("%s %q may point at a path nothing serves: the generated app's strict boot refuses an unserved chrome link; register a route or fix the link", f.Path, f.Href)
}

// LintNavHrefs estimates served paths, skipping anonymous role-gated chrome.
// Unlike uihost.strictChromeLinkFindings, validate only warns: owned routes and static inputs are not fully known.
// projectDir is the project root for static files, or empty when unknown.
func LintNavHrefs(bp appspec.AppSpec, projectDir string) []navHrefFinding {
	if len(bp.Nav) == 0 {
		return nil
	}
	if bp.IsFragment() { // fragments cannot enumerate the project's screens
		return nil
	}
	mux := http.NewServeMux()
	serve := func(pattern string) {
		// Other checks report malformed or conflicting declarations.
		defer func() { _ = recover() }()
		if _, path, ok := strings.Cut(pattern, " "); ok {
			pattern = path
		}
		mux.Handle(pattern, http.NotFoundHandler())
	}
	screens := app.NewRouter()
	for _, screen := range appspec.AppSpecSynthesizeCRUDScreens(bp).Screens {
		func() {
			defer func() { _ = recover() }() // route validation owns malformed patterns
			screens.Screen(&app.Screen{Path: screen.Route, Component: navRouteProbe{}}, nil)
		}()
	}
	for _, decl := range bp.Entities {
		if !appspec.EntityDeclarationCRUDEnabled(decl) {
			continue
		}
		cfg, err := decl.Config()
		if err != nil {
			continue
		}
		mount := navEntityAPIMount(bp, decl)
		for _, pattern := range crud.CrudRoutePatterns(mount, crud.CrudRouteOptions{States: cfg.States, Key: cfg.PrimaryKey}) {
			serve(pattern)
		}
		for _, suffix := range []string{"/_bulk", "/_export.csv", "/_pick"} {
			serve(mount + suffix)
		}
	}
	// Endpoint paths are relative to the same entity API mount.
	for _, ep := range bp.Endpoints {
		for _, decl := range bp.Entities {
			if ep.Entity != decl.Name {
				continue
			}
			mount := navEntityAPIMount(bp, decl)
			serve(strings.ToUpper(ep.Method) + " " + mount + "/" + ep.Path)
		}
	}
	routeApp := bp.RouteApp()
	if routeApp.Auth.Enabled {
		base := strings.TrimRight(routeApp.Auth.BasePath, "/")
		if base == "" {
			base = "/auth"
		}
		for _, suffix := range []string{"/login", "/logout", "/me", "/register", "/password"} {
			serve(base + suffix)
		}
	}
	for _, pattern := range appspec.AppSpecFrameworkRoutePatterns(bp) {
		serve(pattern)
	}
	if routeApp.Admin.Enabled {
		base := strings.TrimRight(routeApp.Admin.Path, "/")
		if base == "" {
			base = "/admin"
		}
		// Generated apps hand admin a DB, which mounts the audit log.
		for _, suffix := range []string{"", "/search", "/shortcuts", "/account", "/audit"} {
			serve(base + suffix)
		}
		for _, decl := range bp.Entities {
			mount := base + "/entities/" + decl.Name
			for _, suffix := range []string{"", "/create", "/{id}"} {
				serve(mount + suffix)
			}
		}
	}
	if routeApp.PWA.Enabled {
		for _, path := range []string{"/icons/icon-192.png", "/icons/icon-512.png", "/icons/icon-maskable.png", "/__gofastr/pwa/register.js", "/__gofastr/pwa/offline"} {
			serve(path)
		}
	}
	var staticRoot *os.Root
	if projectDir != "" {
		if dir := appSpecEffectiveStaticDir(bp); dir != "" {
			staticRoot, _ = os.OpenRoot(filepath.Join(projectDir, dir))
			if staticRoot != nil {
				defer staticRoot.Close()
			}
		}
	}
	screenServes := func(path string) bool {
		_, _, ok := screens.Resolve(app.RoutePath(path))
		if !ok && path != "/" && !strings.HasSuffix(path, "/") {
			_, _, ok = screens.Resolve(app.RoutePath(path + "/"))
		}
		return ok
	}
	var findings []navHrefFinding
	var walk func([]appspec.AppSpecNavItem, string)
	walk = func(items []appspec.AppSpecNavItem, prefix string) {
		for i, item := range items {
			if routeApp.Auth.Enabled && item.Role != "" {
				continue
			}
			at := fmt.Sprintf("%s[%d]", prefix, i)
			if path, internal := navInternalPath(item.Href); internal {
				if routeApp.LocalePrefixes {
					seg, rest, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
					locale, _ := url.PathUnescape(seg)
					if localeShaped(locale) {
						path = "/" + rest
					}
				}
				_, pattern := mux.Handler(&http.Request{Method: http.MethodGet, URL: navPathURL(path)})
				served := pattern != "" || screenServes(path)
				if !served && routeApp.LLMMD && strings.HasSuffix(path, "/llm.md") {
					base := strings.TrimSuffix(path, "/llm.md")
					if base == "" {
						base = "/"
					}
					served = screenServes(base)
				}
				if !served && staticRoot != nil && path != "/" {
					served = navStaticServes(staticRoot, navPathURL(path).Path)
				}
				if !served {
					findings = append(findings, navHrefFinding{Path: at + ".href", Href: item.Href})
				}
			}
			walk(item.Items, at+".items")
		}
	}
	walk(bp.Nav, "nav")
	return findings
}

// Keep the reference classification in step with uihost's internalRoutePath:
// query/fragment split before unescaping; only concrete root-relative paths.
func navInternalPath(href string) (string, bool) {
	h := strings.SplitN(strings.SplitN(href, "#", 2)[0], "?", 2)[0]
	if h == "" || !strings.HasPrefix(h, "/") {
		return "", false
	}
	decoded, err := url.PathUnescape(h)
	if err != nil {
		decoded = h
	}
	if strings.HasPrefix(decoded, "//") || strings.Contains(decoded, `\`) {
		return "", false
	}
	for seg := range strings.SplitSeq(decoded, "/") {
		if strings.HasPrefix(seg, ":") || strings.ContainsAny(seg, "{<") {
			return "", false
		}
	}
	return h, true
}

// navRouteProbe supplies the screen router's parameter contract without
// rendering or loading an app component during validation.
type navRouteProbe struct{}

func (navRouteProbe) Render() render.HTML         { return "" }
func (navRouteProbe) SetParams(map[string]string) {}

func navStaticServes(root *os.Root, path string) bool {
	rel := strings.TrimPrefix(stdpath.Clean("/"+path), "/")
	fsys, check := root.FS(), rel
	if sub, ok := strings.CutPrefix(rel, ".well-known/"); ok {
		public, err := fs.Sub(fsys, ".well-known")
		if err != nil {
			return false
		}
		fsys, check = public, sub
	}
	if corestatic.RefusesIn(fsys, check) {
		return false
	}
	info, err := root.Stat(rel)
	return err == nil && !info.IsDir()
}

// navPathURL retains escaped segment boundaries for ServeMux matching.
func navPathURL(path string) *url.URL {
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return &url.URL{Path: path}
	}
	return &url.URL{Path: decoded, RawPath: path}
}
func navEntityAPIMount(bp appspec.AppSpec, decl entity.EntityDeclaration) string {
	base := ""
	if prefix := strings.Trim(bp.RouteApp().APIPrefix, "/"); prefix != "" {
		base = "/" + prefix
	}
	return appSpecEntityAPIPath(base, decl)
}
