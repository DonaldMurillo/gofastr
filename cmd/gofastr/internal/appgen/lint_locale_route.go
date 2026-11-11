package appgen

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"golang.org/x/text/language"
)

// localeSegmentRE matches a first path segment shaped like a locale tag
// as a catalog file names it, lowercase the way i18n.NewPathLocales
// requires: a two-letter language, then an optional four-letter script
// and an optional two-letter or three-digit region, in BCP 47 order
// ("es", "pt-br", "zh-hant", "es-419", "zh-hant-tw"). Three-letter
// languages are left out on purpose: they would flag ordinary routes
// such as /app and /faq.
var localeSegmentRE = regexp.MustCompile(`^[a-z]{2}(-[a-z]{4})?(-([a-z]{2}|[0-9]{3}))?$`)

// localeShaped reports a segment that has a tag's shape and names a
// language x/text knows. The shape alone flagged /go-live (a "go"
// language with a "live" script), and x/text alone flagged /my-app
// ("app" parses as an extended language); together they keep the real
// tags and drop the words.
func localeShaped(seg string) bool {
	if !localeSegmentRE.MatchString(seg) {
		return false
	}
	_, err := language.Parse(seg)
	return err == nil
}

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
// so their paths are read the same way, and so is the entity API's
// mount (app.api_prefix, or each table when the prefix is empty).
func LintLocaleRoutes(bp appspec.AppSpec) []localeRouteFinding {
	if !bp.App.LocalePrefixes {
		return nil
	}
	var out []localeRouteFinding
	check := func(what, route string) {
		seg, _, _ := strings.Cut(strings.TrimPrefix(route, "/"), "/")
		if localeShaped(seg) {
			out = append(out, localeRouteFinding{What: what, Route: route, Segment: seg})
		}
	}
	for _, s := range appspec.AppSpecSynthesizeCRUDScreens(bp).Screens {
		check(fmt.Sprintf("screen %q route", s.Name), s.Route)
	}
	// The entity API mounts under app.api_prefix, or with none at each
	// CRUD entity's table.
	if base := appSpecAPIBase(bp.App.APIPrefix); base != "" {
		check("app.api_prefix", base)
	} else {
		for _, decl := range bp.Entities {
			if appspec.EntityDeclarationCRUDEnabled(decl) {
				check(fmt.Sprintf("entity %q API route", decl.Name), appSpecEntityAPIPath("", decl))
			}
		}
	}
	if bp.App.Auth.Enabled {
		check("app.auth.base_path", appSpecAuthBase(bp))
	}
	if bp.App.Admin.Enabled && bp.App.Admin.Path != "" {
		check("app.admin.path", bp.App.Admin.Path)
	}
	return out
}
