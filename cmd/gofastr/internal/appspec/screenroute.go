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
