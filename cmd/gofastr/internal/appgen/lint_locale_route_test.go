package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// With locale_prefixes on, the app panics at Start on a route whose
// first segment names a catalog locale (the router strips that segment
// first, so the route is unreachable). The catalog can grow after
// generation, so validate and generate warn on a tag-shaped first
// segment instead of waiting for the boot.
func TestLocaleShapedRouteWarns(t *testing.T) {
	spec := func(prefixes bool, routes ...string) appspec.AppSpec {
		bp := appspec.AppSpec{}
		bp.App.LocalePrefixes = prefixes
		for i, r := range routes {
			bp.Screens = append(bp.Screens, appspec.AppSpecScreen{Name: "s" + string(rune('a'+i)), Route: r})
		}
		return bp
	}
	for _, r := range []string{"/es", "/es/pricing", "/en", "/pt-br/home", "/zh-hant"} {
		got := LintLocaleRoutes(spec(true, r))
		if len(got) != 1 || !strings.Contains(got[0].Message(), r) || !strings.Contains(got[0].Message(), "locale_prefixes") {
			t.Errorf("route %q: findings %v", r, got)
		}
	}
	for _, r := range []string{"/", "/pricing", "/app", "/ES", "/e", "/{id}", "/docs/es"} {
		if got := LintLocaleRoutes(spec(true, r)); len(got) != 0 {
			t.Errorf("route %q warned: %v", r, got)
		}
	}
	bp := spec(true)
	bp.App.Auth.Enabled, bp.App.Auth.BasePath = true, "/de"
	bp.App.Admin.Enabled, bp.App.Admin.Path = true, "/fr/admin"
	if got := LintLocaleRoutes(bp); len(got) != 2 || !strings.Contains(got[0].Message(), "app.auth.base_path") || !strings.Contains(got[1].Message(), "app.admin.path") {
		t.Errorf("auth and admin mounts: findings %v", got)
	}
	if got := LintLocaleRoutes(spec(false, "/es")); len(got) != 0 {
		t.Errorf("locale_prefixes off warned: %v", got)
	}
}
