package appgen

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"golang.org/x/text/language"
)

var localeSegmentRE = regexp.MustCompile(`^[a-z]{2}(-[a-z]{4})?(-([a-z]{2}|[0-9]{3}))?$`)

func localeShaped(seg string) bool {
	if !localeSegmentRE.MatchString(seg) {
		return false
	}
	_, err := language.Parse(seg)
	return err == nil
}

type localeRouteFinding struct {
	What    string
	Route   string
	Segment string
}

func (f localeRouteFinding) Message() string {
	return fmt.Sprintf(
		"%s %q starts with %q, which reads as a locale tag: with app.locale_prefixes on, the app panics at Start once %q is a catalog locale (locales/%s.json, or the default locale), because the router strips that segment before matching and the route is unreachable. Rename the route",
		f.What, f.Route, f.Segment, f.Segment, f.Segment)
}

// LintLocaleRoutes flags each screen, entity API, auth or admin route whose first segment is shaped like a locale tag when app.locale_prefixes is on.
func LintLocaleRoutes(bp appspec.AppSpec) []localeRouteFinding {
	app := bp.RouteApp()
	if !app.LocalePrefixes {
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
	if base := appSpecAPIBase(app.APIPrefix); base == "" {
		for _, decl := range bp.Entities {
			if appspec.EntityDeclarationCRUDEnabled(decl) {
				check(fmt.Sprintf("entity %q API route", decl.Name), appSpecEntityAPIPath("", decl))
			}
		}
	} else if bp.App.LocalePrefixes {
		check("app.api_prefix", base)
	}
	if !bp.App.LocalePrefixes {
		return out
	}
	if bp.App.Auth.Enabled {
		check("app.auth.base_path", appSpecAuthBase(bp))
	}
	if bp.App.Admin.Enabled && bp.App.Admin.Path != "" {
		check("app.admin.path", bp.App.Admin.Path)
	}
	return out
}
