package uihost

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/axecov"
	"github.com/DonaldMurillo/gofastr/framework/dev"
)

// StrictLevel is the posture of one strict check. The zero value is
// StrictEnforce, so a zero-value [StrictConfig] — and a bare
// WithStrict() — is the strictest configuration, and relaxing anything
// is always an explicit, visible-in-review choice.
type StrictLevel int

const (
	// StrictEnforce fails boot on findings (the default everywhere).
	StrictEnforce StrictLevel = iota
	// StrictWarn logs each finding via slog.Warn and serves.
	StrictWarn
	// StrictOff skips the check entirely.
	StrictOff
)

// StrictAbsencePosture is the posture of the missing-axe-manifest
// reminder, a check whose DEFAULT is warn (unlike everything else):
// absence is a state every fresh checkout passes through, drift is not.
type StrictAbsencePosture int

const (
	// StrictAbsenceWarn (the zero value) logs and serves.
	StrictAbsenceWarn StrictAbsencePosture = iota
	// StrictAbsenceEnforce fails boot until the axe suite has run.
	StrictAbsenceEnforce
	// StrictAbsenceOff silences the reminder entirely.
	StrictAbsenceOff
)

// StrictConfig tunes every strict check individually. The zero value
// enforces everything — each field is only ever written to relax.
type StrictConfig struct {
	// ScreenTitles: every page screen declares a title.
	ScreenTitles StrictLevel
	// ScreenDescriptions: every page screen declares a description or
	// implements [ScreenSEO] (a zero-value return is the documented
	// per-page opt-out).
	ScreenDescriptions StrictLevel
	// SiteDescription: [WithDescription] configured with a non-empty value.
	SiteDescription StrictLevel
	// SiteIcon: [WithAppIcon] or [WithFavicon] configured.
	SiteIcon StrictLevel
	// Sitemap: [WithSitemap] configured.
	Sitemap StrictLevel
	// Robots: [WithRobots] configured.
	Robots StrictLevel
	// AxeCoverage: every page route has a recorded axe scan in the
	// .gofastr/axe-coverage.json manifest. Only evaluated under
	// `gofastr dev` regardless of level — the manifest is a local test
	// artifact that never ships, so production boots can't depend on it.
	AxeCoverage StrictLevel
	// AxeManifestMissing is the posture when the manifest doesn't exist
	// at all (fresh clone / fresh generate — the axe suite has never
	// run). Its zero value is StrictAbsenceWarn, NOT enforce: first
	// boot should never be walled off behind a Chrome run. Set
	// StrictAbsenceEnforce for environments where an unproven checkout
	// must not serve, or StrictAbsenceOff to silence the reminder.
	AxeManifestMissing StrictAbsencePosture

	// ExemptScreens lists route patterns the per-screen checks
	// (ScreenTitles, ScreenDescriptions, AxeCoverage) skip. An entry is
	// an exact route pattern ("/machine/feed") or a prefix wildcard
	// ("/internal/*"). Use for routes that are deliberately outside the
	// SEO/a11y bar; site-level checks are unaffected.
	ExemptScreens []string
}

// strict check identifiers — used to route findings through their
// configured level and to label warn logs.
const (
	strictCheckScreenTitles       = "screen-titles"
	strictCheckScreenDescriptions = "screen-descriptions"
	strictCheckSiteDescription    = "site-description"
	strictCheckSiteIcon           = "site-icon"
	strictCheckSitemap            = "sitemap"
	strictCheckRobots             = "robots"
	strictCheckAxeCoverage        = "axe-coverage"
	strictCheckAxeManifest        = "axe-manifest"
)

// level resolves the configured posture for one check id.
func (c StrictConfig) level(check string) StrictLevel {
	switch check {
	case strictCheckScreenTitles:
		return c.ScreenTitles
	case strictCheckScreenDescriptions:
		return c.ScreenDescriptions
	case strictCheckSiteDescription:
		return c.SiteDescription
	case strictCheckSiteIcon:
		return c.SiteIcon
	case strictCheckSitemap:
		return c.Sitemap
	case strictCheckRobots:
		return c.Robots
	case strictCheckAxeCoverage:
		return c.AxeCoverage
	case strictCheckAxeManifest:
		switch c.AxeManifestMissing {
		case StrictAbsenceEnforce:
			return StrictEnforce
		case StrictAbsenceOff:
			return StrictOff
		default:
			return StrictWarn
		}
	}
	return StrictEnforce
}

// exempt reports whether a route pattern is excluded from per-screen
// checks. Entries match exactly, or by prefix when they end in "/*".
func (c StrictConfig) exempt(route string) bool {
	for _, e := range c.ExemptScreens {
		if prefix, ok := strings.CutSuffix(e, "/*"); ok {
			if route == prefix || strings.HasPrefix(route, prefix+"/") {
				return true
			}
			continue
		}
		if route == e {
			return true
		}
	}
	return false
}

// WithStrict turns missing launch hygiene into boot failures. At Mount
// time the host validates the app's declared surface and panics with
// every enforced finding at once (each with a fix hint) instead of
// serving. The checks:
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
//     drift; a manifest that doesn't exist yet (fresh clone or fresh
//     generate) warns by default so first boot is never walled off
//     behind a Chrome run. Production boots skip axe checks entirely.
//
// WithStrict() with no arguments enforces everything. Pass a
// [StrictConfig] to tune each check to enforce, warn, or off, exempt
// specific routes, or harden the missing-manifest posture — the zero
// value of every field is the strictest setting, so configuration only
// ever relaxes, visibly.
func WithStrict(cfg ...StrictConfig) Option {
	if len(cfg) > 1 {
		panic("uihost.WithStrict: pass at most one StrictConfig")
	}
	conf := StrictConfig{}
	if len(cfg) == 1 {
		conf = cfg[0]
	}
	return func(ds *UIHost) {
		ds.strict = true
		// Always assign, so a later bare WithStrict() restores the
		// documented all-enforced posture (last option wins) instead of
		// silently keeping an earlier relaxed config.
		ds.strictConfig = conf
	}
}

// strictFinding is one violation, tagged with the check that produced
// it so enforceStrict can route it through the configured level.
type strictFinding struct {
	check string
	msg   string
}

// enforceStrict runs the strict checks, warns the warn-level findings,
// and panics with the enforced ones. Called from Mount — boot time,
// before any traffic — so a strict app can never serve a surface that
// fails its enforced checks. Panic (not error) is the framework's
// contract for configuration it cannot honor, same as route conflicts
// and fanout-without-secret.
func (ds *UIHost) enforceStrict() {
	if !ds.strict {
		return
	}
	cfg := ds.strictConfig
	findings := ds.strictScreenFindings(cfg)
	findings = append(findings, ds.strictSiteFindings()...)
	if dev.Enabled() {
		findings = append(findings, ds.strictAxeCoverageFindings(cfg)...)
	}

	var enforced []string
	for _, f := range findings {
		switch cfg.level(f.check) {
		case StrictOff:
			// skipped entirely
		case StrictWarn:
			slog.Warn("uihost strict: "+f.msg, "check", f.check)
		default:
			enforced = append(enforced, f.msg)
		}
	}
	if len(enforced) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "uihost: strict mode: %d finding(s):\n", len(enforced))
	for i, v := range enforced {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, v)
	}
	b.WriteString("strict mode is opt-in (uihost.WithStrict) — fix the findings above; they are ordered, independent, and each names its remedy. To relax a check deliberately, pass a StrictConfig (levels: enforce/warn/off, per-route ExemptScreens).")
	panic(b.String())
}

// strictScreenFindings checks per-screen SEO completeness for every
// page-type screen. Drawers, sheets, and dialogs are skipped — they
// render inside a page and have no head of their own.
func (ds *UIHost) strictScreenFindings(cfg StrictConfig) []strictFinding {
	var out []strictFinding
	for _, path := range ds.App.Router.Paths() {
		screen, _, ok := ds.App.Router.Resolve(path)
		if !ok || screen.Type != app.ScreenPage || cfg.exempt(screen.Path) {
			continue
		}
		if screen.Title == "" {
			out = append(out, strictFinding{strictCheckScreenTitles, fmt.Sprintf(
				"screen %q: no title — implement ScreenTitler on the component or register with Screen.WithTitle", screen.Path)})
		}
		if _, deliberate := screen.Component.(ScreenSEO); screen.Description == "" && !deliberate {
			out = append(out, strictFinding{strictCheckScreenDescriptions, fmt.Sprintf(
				"screen %q: no description — implement ScreenDescriber (or ScreenSEO; a zero-value ScreenSEO return deliberately opts the page out)", screen.Path)})
		}
	}
	return out
}

// strictSiteFindings checks the site-level surface a public app should
// never ship without.
func (ds *UIHost) strictSiteFindings() []strictFinding {
	var out []strictFinding
	if !ds.siteDescription {
		out = append(out, strictFinding{strictCheckSiteDescription, "site: no description — add uihost.WithDescription"})
	}
	if ds.faviconURL == "" && len(ds.appIcons) == 0 {
		out = append(out, strictFinding{strictCheckSiteIcon, "site: no icon — add uihost.WithAppIcon (one source image) or uihost.WithFavicon"})
	}
	switch {
	case ds.sitemapConfig == nil:
		out = append(out, strictFinding{strictCheckSitemap, "site: no sitemap — add uihost.WithSitemap so crawlers and the a11y audit can discover every route"})
	default:
		if reason := invalidSitemapBaseURL(ds.sitemapConfig.BaseURL); reason != "" {
			out = append(out, strictFinding{strictCheckSitemap, fmt.Sprintf(
				"site: sitemap BaseURL %q %s — <loc> entries must be absolute URLs (scheme + host), e.g. https://example.com", ds.sitemapConfig.BaseURL, reason)})
		}
	}
	if ds.robotsConfig == nil {
		out = append(out, strictFinding{strictCheckRobots, "site: no robots directives — add uihost.WithRobots"})
	}
	return out
}

// invalidSitemapBaseURL reports why base cannot serve as the sitemap's
// canonical origin ("" when it can). The sitemap protocol requires
// absolute <loc> URLs, so strict mode refuses a configured-but-broken
// sitemap the same way it refuses a missing one. A path prefix is
// allowed (subpath deploys); userinfo, query, and fragment are not.
func invalidSitemapBaseURL(base string) string {
	u, err := url.Parse(base)
	switch {
	case base == "":
		return "is empty"
	case err != nil:
		return fmt.Sprintf("does not parse (%v)", err)
	case u.Scheme != "http" && u.Scheme != "https":
		return "needs an http or https scheme"
	case u.Host == "":
		return "has no host"
	case u.User != nil:
		return "must not carry userinfo"
	case u.RawQuery != "" || u.Fragment != "":
		return "must not carry a query or fragment"
	}
	return ""
}

// isDynamicRoute reports whether a route pattern contains a parameter
// or wildcard segment (":id", "{path...}").
func isDynamicRoute(pattern string) bool {
	for _, seg := range strings.Split(pattern, "/") {
		if strings.HasPrefix(seg, ":") || strings.HasPrefix(seg, "{") {
			return true
		}
	}
	return false
}

// strictAxeCoverageFindings diffs the app's page routes against the
// axe-coverage manifest the test suite recorded. A manifest entry covers
// a route when the concrete scanned path resolves to it, so one scanned
// "/docs/install" covers the "/docs/:slug" pattern.
//
// The demand surface mirrors the sitemap's discovery surface: a dynamic
// route whose screen does not implement [app.StaticPathsProvider] is
// invisible to the sitemap, hence unreachable by a sitemap-driven axe
// gate, hence structurally uncoverable — strict skips it and screams
// instead of demanding the impossible.
func (ds *UIHost) strictAxeCoverageFindings(cfg StrictConfig) []strictFinding {
	if cfg.level(strictCheckAxeCoverage) == StrictOff {
		return nil
	}
	var pageRoutes, invisible []string
	for _, path := range ds.App.Router.Paths() {
		screen, _, ok := ds.App.Router.Resolve(path)
		if !ok || screen.Type != app.ScreenPage || cfg.exempt(screen.Path) {
			continue
		}
		if isDynamicRoute(screen.Path) {
			if _, declares := screen.Component.(app.StaticPathsProvider); !declares {
				invisible = append(invisible, screen.Path)
				continue
			}
		}
		pageRoutes = append(pageRoutes, screen.Path)
	}
	if len(invisible) > 0 {
		sort.Strings(invisible)
		slog.Warn("uihost strict: dynamic screens without StaticPaths are invisible to the sitemap and the axe gate — implement StaticPaths(ctx) on them to bring them under coverage",
			"routes", strings.Join(invisible, ", "))
	}
	// No page screens → nothing an axe test could scan; requiring a
	// manifest would fail every screen-less (API-only) app for a file
	// it has no way to produce.
	if len(pageRoutes) == 0 {
		return nil
	}
	m, err := axecov.Read(axecov.DefaultDir())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []strictFinding{{strictCheckAxeManifest, fmt.Sprintf(
				"axe coverage unverified — no manifest at %s; run the axe suite (go test) so every screen's scan is recorded (%v)", axecov.FileName, err)}}
		}
		// A manifest that exists but cannot be read is NOT absence:
		// treating corruption as absence would relax enforcement exactly
		// when the coverage record is untrustworthy. Routed through the
		// AxeCoverage level (enforced by default).
		return []strictFinding{{strictCheckAxeCoverage, fmt.Sprintf(
			"axe coverage manifest unreadable (%v) — delete %s and re-run the axe suite", err, axecov.FileName)}}
	}
	covered := map[string]bool{}
	for scanned := range m.Pages {
		if screen, _, ok := ds.App.Router.Resolve(scanned); ok {
			covered[screen.Path] = true
		}
	}
	var msgs []string
	for _, route := range pageRoutes {
		if !covered[route] {
			msgs = append(msgs, fmt.Sprintf(
				"axe coverage: screen %q has no recorded axe scan — add it to the axe gate's page list (derive the list from your screen catalog so this cannot recur)", route))
		}
	}
	sort.Strings(msgs)
	out := make([]strictFinding, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, strictFinding{strictCheckAxeCoverage, msg})
	}
	return out
}
