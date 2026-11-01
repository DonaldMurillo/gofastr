package openapi

import (
	"context"
	"fmt"
	"html"
	"maps"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// The docs page's words. It is a page outside the app shell, so it
// says them through phrases and declares its language with i18n.DocLang.
var (
	phraseDocsTitle     = i18n.Define("openapi.docs.title", "API reference")
	phraseDocsVersion   = i18n.Define("openapi.docs.version", "Version {{version}}")
	phraseDocsSpec      = i18n.Define("openapi.docs.spec", "OpenAPI document")
	phraseDocsMethod    = i18n.Define("openapi.docs.method", "Method")
	phraseDocsPath      = i18n.Define("openapi.docs.path", "Path")
	phraseDocsSummary   = i18n.Define("openapi.docs.summary", "Summary")
	phraseDocsUntagged  = i18n.Define("openapi.docs.untagged", "Other operations")
	phraseDocsNoneShown = i18n.Define("openapi.docs.none", "There are no operations to show.")
)

// DocsConfig configures DocsHandlerWith.
type DocsConfig struct {
	// BasePath is where the handler is mounted ("/api/docs",
	// "/swagger"); the handler strips it before matching SpecPath and
	// PagePath. A trailing slash is dropped. Mount the handler on
	// BasePath and everything below it; any other path below it is 404.
	BasePath string
	// SpecPath is where the OpenAPI JSON is served, under BasePath.
	// Empty means "/openapi.json". "/v1/swagger.json" under BasePath
	// "/swagger" serves /swagger/v1/swagger.json.
	SpecPath string
	// PagePath is a second path, under BasePath, that serves the page
	// besides BasePath + "/" ("/index.html"). Empty means none.
	PagePath string
	// Public serves the page and the spec without the auth check. The
	// gated page lists only the operations the caller's spec view
	// (Spec.RequestView) holds.
	Public bool
}

// reDocsSubpath is a SpecPath / PagePath: "/" then plain URL segments.
var reDocsSubpath = regexp.MustCompile(`^(?:/[A-Za-z0-9._~-]+)+$`)

func validDocsSubpath(p string) bool {
	return reDocsSubpath.MatchString(p) && path.Clean(p) == p
}

// docsCSP is the page's policy. The page carries no script and no
// style, so nothing inline needs allowing, and it renders under a
// stricter app-wide policy too.
const docsCSP = "default-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// DocsHandlerWith serves a server-rendered API reference page and the
// spec it describes. The page lists every operation the caller can see
// (method, path, summary), grouped by tag, and links the spec for
// viewers such as Swagger UI, Insomnia or Postman. It is plain
// semantic HTML: no script, no stylesheet and no inline style, so it
// works under the default Content-Security-Policy and reads in the
// browser's own styles. It panics on a SpecPath or PagePath that is not
// an absolute clean path of plain segments, and on the two colliding.
func DocsHandlerWith(spec *Spec, cfg DocsConfig) http.Handler {
	// "/docs/" and "/docs" name the same mount; with the slash kept,
	// StripPrefix would leave "" for "/docs/" and miss "/docs/openapi.json".
	// "/" becomes "", a mount at the site root.
	if strings.HasPrefix(cfg.BasePath, "/") {
		cfg.BasePath = strings.TrimRight(cfg.BasePath, "/")
	}
	if cfg.SpecPath == "" {
		cfg.SpecPath = "/openapi.json"
	}
	if !validDocsSubpath(cfg.SpecPath) {
		panic(fmt.Sprintf("openapi: DocsConfig.SpecPath %q must be an absolute clean path such as /openapi.json", cfg.SpecPath))
	}
	if cfg.PagePath != "" && (!validDocsSubpath(cfg.PagePath) || cfg.PagePath == cfg.SpecPath) {
		panic(fmt.Sprintf("openapi: DocsConfig.PagePath %q must be an absolute clean path such as /index.html, other than SpecPath", cfg.PagePath))
	}
	specHandler := Handler(spec)
	if cfg.Public {
		specHandler = PublicHandler(spec)
	}
	// BasePath is developer config but reaches an href and visible
	// text. Escaping keeps it inside the attribute and does nothing about
	// its scheme, so the href also passes the shared anchor allow-list: a
	// "javascript:" base links to the bare SpecPath instead.
	specText := html.EscapeString(cfg.BasePath + cfg.SpecPath)
	specHref := html.EscapeString(urlsafe.Clean(cfg.BasePath+cfg.SpecPath, urlsafe.Anchor))
	if specHref == "" {
		specHref = html.EscapeString(cfg.SpecPath)
	}

	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security headers first, so the 401 carries them too.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", docsCSP)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		view := spec
		if !cfg.Public {
			if _, ok := handler.GetUser(r.Context()); !ok {
				i18n.HTTPError(w, r, http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized))
				return
			}
			if spec.RequestView != nil {
				if v := spec.RequestView(r); v != nil {
					view = v.withInheritedInfo(spec)
				}
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(renderDocsPage(r.Context(), view, specHref, specText)))
	})

	// Routing is by exact path, so an unknown path or method answers
	// through i18n like every other framework refusal, not with the
	// standard library's English 404/405.
	route := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var h http.Handler
		switch p := r.URL.Path; {
		case p == "" || p == "/" || (cfg.PagePath != "" && p == cfg.PagePath):
			h = page
		case p == cfg.SpecPath:
			h = specHandler
		default:
			i18n.HTTPError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
			return
		}
		h.ServeHTTP(w, r)
	})
	return http.StripPrefix(cfg.BasePath, route)
}

// docsRow is one operation on the page.
type docsRow struct{ method, path, summary string }

// methodOrder is the order operations of one path are listed in.
var methodOrder = []string{"get", "put", "post", "patch", "delete", "options", "head", "trace"}

// renderDocsPage writes the page for view. Every value from the spec
// is escaped; the only markup is the page's own.
func renderDocsPage(ctx context.Context, view *Spec, specHref, specText string) string {
	doc := view.Build()
	info, _ := doc["info"].(map[string]any)
	title, _ := info["title"].(string)
	version, _ := info["version"].(string)
	description, _ := info["description"].(string)

	groups := map[string][]docsRow{}
	paths, _ := doc["paths"].(map[string]map[string]any)
	for _, p := range slices.Sorted(maps.Keys(paths)) {
		ops := paths[p]
		for _, m := range methodOrder {
			raw, present := ops[m]
			if !present {
				continue
			}
			// AddPath stores Operation.ToMap, always this type; a value
			// of another type has no row to show.
			op, isOp := raw.(map[string]any)
			if !isOp {
				continue
			}
			tag := ""
			if tags, ok := op["tags"].([]string); ok && len(tags) > 0 {
				tag = tags[0]
			}
			summary, _ := op["summary"].(string)
			groups[tag] = append(groups[tag], docsRow{method: strings.ToUpper(m), path: p, summary: summary})
		}
	}

	lang := i18n.DocLang(ctx)
	var b strings.Builder
	esc := html.EscapeString
	pageTitle := phraseDocsTitle.Text(ctx)
	if title != "" {
		pageTitle = title + " — " + pageTitle
	}
	fmt.Fprintf(&b, "<!DOCTYPE html>\n<html lang=\"%s\" dir=\"%s\">\n<head>\n<meta charset=\"utf-8\">\n", esc(lang), esc(i18n.Dir(lang)))
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	// Lets the browser draw its own dark defaults; no stylesheet needed.
	b.WriteString("<meta name=\"color-scheme\" content=\"light dark\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n</head>\n<body>\n<main>\n<h1>%s</h1>\n", esc(pageTitle), esc(pageTitle))
	if version != "" {
		fmt.Fprintf(&b, "<p>%s</p>\n", esc(phraseDocsVersion.Text(ctx, map[string]any{"version": version})))
	}
	for _, para := range strings.Split(strings.ReplaceAll(description, "\r\n", "\n"), "\n\n") {
		if para = strings.TrimSpace(para); para != "" {
			fmt.Fprintf(&b, "<p>%s</p>\n", esc(para))
		}
	}
	fmt.Fprintf(&b, "<p><a href=\"%s\">%s</a> <code>%s</code></p>\n", specHref, esc(phraseDocsSpec.Text(ctx)), specText)

	if len(groups) == 0 {
		fmt.Fprintf(&b, "<p>%s</p>\n", esc(phraseDocsNoneShown.Text(ctx)))
	}
	tags := slices.Sorted(maps.Keys(groups))
	// Untagged operations come last.
	if len(tags) > 0 && tags[0] == "" {
		tags = append(tags[1:], "")
	}
	for i, tag := range tags {
		heading := tag
		if heading == "" {
			heading = phraseDocsUntagged.Text(ctx)
		}
		fmt.Fprintf(&b, "<section aria-labelledby=\"ops-%d\">\n<h2 id=\"ops-%d\">%s</h2>\n<table>\n", i, i, esc(heading))
		fmt.Fprintf(&b, "<thead><tr><th scope=\"col\">%s</th><th scope=\"col\">%s</th><th scope=\"col\">%s</th></tr></thead>\n<tbody>\n",
			esc(phraseDocsMethod.Text(ctx)), esc(phraseDocsPath.Text(ctx)), esc(phraseDocsSummary.Text(ctx)))
		for _, row := range groups[tag] {
			fmt.Fprintf(&b, "<tr><td><code>%s</code></td><td><code>%s</code></td><td>%s</td></tr>\n",
				esc(row.method), esc(row.path), esc(row.summary))
		}
		b.WriteString("</tbody>\n</table>\n</section>\n")
	}
	b.WriteString("</main>\n</body>\n</html>\n")
	return b.String()
}
