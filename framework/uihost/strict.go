package uihost

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/axecov"
	"github.com/DonaldMurillo/gofastr/framework/dev"
)

// WithStrict turns missing launch hygiene into boot failures. At Mount
// time the host validates the app's declared surface and panics with
// every finding at once (each with a fix hint) instead of serving:
//
//   - every page screen declares a title, and a description unless it
//     implements [ScreenSEO] (the documented zero-value opt-out — a
//     deliberate "this page is naked" beats a forgotten one);
//   - the site declares a description ([WithDescription]), an icon
//     ([WithFavicon] or [WithAppIcon]), a sitemap ([WithSitemap]), and
//     robots directives ([WithRobots]);
//   - under `gofastr dev` only: every page route is covered by the
//     axe-coverage manifest (.gofastr/axe-coverage.json) that
//     framework/testkit/axetest scans record — i.e. every screen has an
//     accessibility test. A manifest that exists but misses a route is
//     drift and fails boot; a manifest that doesn't exist yet (fresh
//     clone or fresh generate — the axe suite simply hasn't run) warns
//     loudly and serves, so first boot is never walled off behind a
//     Chrome run. Production boots skip this check entirely because the
//     manifest is a local test artifact that never ships.
//
// Strict mode is opt-in and all-or-nothing; per-screen relaxation goes
// through the same interfaces normal SEO uses (implement ScreenSEO and
// return a zero value), never through a config toggle.
func WithStrict() Option {
	return func(ds *UIHost) {
		ds.strict = true
	}
}

// enforceStrict runs the strict checks and panics with the aggregated
// findings. Called from Mount — boot time, before any traffic — so a
// strict app can never serve a surface that fails the checks. Panic (not
// error) is the framework's contract for configuration it cannot honor,
// same as route conflicts and fanout-without-secret.
func (ds *UIHost) enforceStrict() {
	if !ds.strict {
		return
	}
	violations := ds.strictScreenViolations()
	violations = append(violations, ds.strictSiteViolations()...)
	if dev.Enabled() {
		violations = append(violations, ds.strictAxeCoverageViolations()...)
	}
	if len(violations) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "uihost: strict mode: %d finding(s):\n", len(violations))
	for i, v := range violations {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, v)
	}
	b.WriteString("strict mode is opt-in (uihost.WithStrict) — fix the findings above; they are ordered, independent, and each names its remedy.")
	panic(b.String())
}

// strictScreenViolations checks per-screen SEO completeness for every
// page-type screen. Drawers, sheets, and dialogs are skipped — they
// render inside a page and have no head of their own.
func (ds *UIHost) strictScreenViolations() []string {
	var out []string
	for _, path := range ds.App.Router.Paths() {
		screen, _, ok := ds.App.Router.Resolve(path)
		if !ok || screen.Type != app.ScreenPage {
			continue
		}
		if screen.Title == "" {
			out = append(out, fmt.Sprintf(
				"screen %q: no title — implement ScreenTitler on the component or register with Screen.WithTitle", screen.Path))
		}
		if _, deliberate := screen.Component.(ScreenSEO); screen.Description == "" && !deliberate {
			out = append(out, fmt.Sprintf(
				"screen %q: no description — implement ScreenDescriber (or ScreenSEO; a zero-value ScreenSEO return deliberately opts the page out)", screen.Path))
		}
	}
	return out
}

// strictSiteViolations checks the site-level surface a public app should
// never ship without.
func (ds *UIHost) strictSiteViolations() []string {
	var out []string
	if !ds.siteDescription {
		out = append(out, "site: no description — add uihost.WithDescription")
	}
	if ds.faviconURL == "" && len(ds.appIcons) == 0 {
		out = append(out, "site: no icon — add uihost.WithAppIcon (one source image) or uihost.WithFavicon")
	}
	if ds.sitemapConfig == nil {
		out = append(out, "site: no sitemap — add uihost.WithSitemap so crawlers and the a11y audit can discover every route")
	}
	if ds.robotsConfig == nil {
		out = append(out, "site: no robots directives — add uihost.WithRobots")
	}
	return out
}

// strictAxeCoverageViolations diffs the app's page routes against the
// axe-coverage manifest the test suite recorded. A manifest entry covers
// a route when the concrete scanned path resolves to it, so one scanned
// "/docs/install" covers the "/docs/:slug" pattern.
func (ds *UIHost) strictAxeCoverageViolations() []string {
	var pageRoutes []string
	for _, path := range ds.App.Router.Paths() {
		if screen, _, ok := ds.App.Router.Resolve(path); ok && screen.Type == app.ScreenPage {
			pageRoutes = append(pageRoutes, screen.Path)
		}
	}
	// No page screens → nothing an axe test could scan; requiring a
	// manifest would fail every screen-less (API-only) app for a file
	// it has no way to produce.
	if len(pageRoutes) == 0 {
		return nil
	}
	m, err := axecov.Read(".")
	if err != nil {
		// A missing manifest means the axe suite has not run in this
		// checkout (fresh clone, fresh generate) — that is a state every
		// project passes through, so scream but serve. A manifest that
		// EXISTS but misses a route is real drift and fails below.
		slog.Warn("uihost strict: axe coverage unverified — no manifest; run the axe suite (go test) so every screen's scan is recorded",
			"manifest", axecov.FileName, "err", err)
		return nil
	}
	covered := map[string]bool{}
	for scanned := range m.Pages {
		if screen, _, ok := ds.App.Router.Resolve(scanned); ok {
			covered[screen.Path] = true
		}
	}
	var out []string
	for _, route := range pageRoutes {
		if !covered[route] {
			out = append(out, fmt.Sprintf(
				"axe coverage: screen %q has no recorded axe scan — add it to the axe gate's page list (derive the list from your screen catalog so this cannot recur)", route))
		}
	}
	sort.Strings(out)
	return out
}
