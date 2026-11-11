package appgen

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// localeSegmentRE matches a first path segment shaped like a locale tag
// as a catalog file names it: a two-letter language, then optional
// subtags ("es", "pt-br", "zh-hant"), lowercase the way
// i18n.NewPathLocales requires. Three-letter languages are left out on
// purpose: they would flag ordinary routes such as /app and /faq.
var localeSegmentRE = regexp.MustCompile(`^[a-z]{2}(-[a-z0-9]{2,8})*$`)

// localeRouteFinding is a screen route whose first segment reads as a
// locale tag in an app with locale prefixes.
type localeRouteFinding struct {
	What    string // `screen "x" route` or the app key naming the path
	Route   string
	Segment string
}

func (f localeRouteFinding) Message() string {
	return fmt.Sprintf(
		"%s %q starts with %q, which reads as a locale tag: with app.locale_prefixes on, the app panics at Start once %q is a catalog locale (locales/%s.json, or the default locale), because the router strips that segment before matching and the route is unreachable. Rename the route",
		f.What, f.Route, f.Segment, f.Segment, f.Segment)
}

// LintLocaleRoutes flags a screen route whose first segment is shaped
// like a locale tag when app.locale_prefixes is on. framework's
// checkLocaleRoutes panics at Start on a route whose first segment is a
// catalog locale, but the catalog is locales/*.json, which an author
// adds after generation (the docs say to), so the spec cannot know the
// tag set: a warning from `gofastr validate` and `gofastr generate` on
// the shape, never an error. The auth and admin mounts are routes too,
// so their paths are read the same way.
func LintLocaleRoutes(bp appspec.AppSpec) []localeRouteFinding {
	if !bp.App.LocalePrefixes {
		return nil
	}
	var out []localeRouteFinding
	check := func(what, route string) {
		seg, _, _ := strings.Cut(strings.TrimPrefix(route, "/"), "/")
		if localeSegmentRE.MatchString(seg) {
			out = append(out, localeRouteFinding{What: what, Route: route, Segment: seg})
		}
	}
	for _, s := range appspec.AppSpecSynthesizeCRUDScreens(bp).Screens {
		check(fmt.Sprintf("screen %q route", s.Name), s.Route)
	}
	if bp.App.Auth.Enabled {
		check("app.auth.base_path", appSpecAuthBase(bp))
	}
	if bp.App.Admin.Enabled && bp.App.Admin.Path != "" {
		check("app.admin.path", bp.App.Admin.Path)
	}
	return out
}
