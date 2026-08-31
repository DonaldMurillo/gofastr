package uihost

import (
	"context"
	"errors"
	"fmt"
	stdhtml "html"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/axecov"
	"github.com/DonaldMurillo/gofastr/framework/dev"
)

// StrictLevel is the posture of one strict check. The zero value is
// StrictEnforce, so a zero-value [StrictConfig], and a bare
// WithStrict(), is the strictest configuration, and relaxing anything
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
// enforces everything: each field is only ever written to relax.
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
	// InternalLinks: every internal link in the site chrome (each
	// layout's header, sidebar, footer) points at a path the app
	// actually serves: a registered screen (dynamic routes included),
	// a static file, or any GET route on the framework router, once
	// the route table is complete at boot (App.Start → ValidateBoot).
	// A catch-all GET route (/{path...}) satisfies the check for every
	// path it claims, so links under one are accepted, not verified.
	// External URLs, anchors, query-only refs, and template
	// placeholders are out of scope. ExemptScreens entries also exempt
	// links whose target falls under them.
	InternalLinks StrictLevel
	// AxeCoverage: every page route has a recorded axe scan in the
	// .gofastr/axe-coverage.json manifest. Only evaluated under
	// `gofastr dev` regardless of level: the manifest is a local test
	// artifact that never ships, so production boots can't depend on it.
	AxeCoverage StrictLevel
	// AxeManifestMissing is the posture when the manifest doesn't exist
	// at all (fresh clone / fresh generate, the axe suite has never
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

// strict check identifiers: used to route findings through their
// configured level and to label warn logs.
const (
	strictCheckScreenTitles       = "screen-titles"
	strictCheckScreenDescriptions = "screen-descriptions"
	strictCheckSiteDescription    = "site-description"
	strictCheckSiteIcon           = "site-icon"
	strictCheckSitemap            = "sitemap"
	strictCheckRobots             = "robots"
	strictCheckInternalLinks      = "internal-links"
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
	case strictCheckInternalLinks:
		return c.InternalLinks
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

// WithStrict turns missing launch hygiene into boot failures. The host
// validates the app's declared surface and panics with every enforced
// finding at once (each with a fix hint) instead of serving. Most
// checks run at Mount; the internal-link check runs later, at boot
// (see [UIHost.ValidateBoot]), because it needs the complete route
// table, which batteries, plugins, and App.Start itself finish
// registering only after Mount. The checks:
//
//   - every page screen declares a title, and a description unless it
//     implements [ScreenSEO] (the documented zero-value opt-out: a
//     deliberate "this page is naked" beats a forgotten one);
//   - the site declares a description ([WithDescription]), an icon
//     ([WithFavicon] or [WithAppIcon]), a sitemap ([WithSitemap]), and
//     robots directives ([WithRobots]);
//   - every internal link in the site chrome (each layout's header,
//     sidebar, footer) points at a path the app serves: a registered
//     screen (dynamic routes included), a static file, or any GET
//     route on the framework router once the table is complete. A
//     catch-all GET route (/{path...}) satisfies the check for every
//     path it claims, so links under one are accepted, not verified;
//   - under `gofastr dev` only: every page route is covered by the
//     axe-coverage manifest (.gofastr/axe-coverage.json) that
//     framework/testkit/axetest scans record, i.e. every screen has an
//     accessibility test. A manifest that exists but misses a route is
//     drift; a manifest that doesn't exist yet (fresh clone or fresh
//     generate) warns by default so first boot is never walled off
//     behind a Chrome run. Production boots skip axe checks entirely.
//
// WithStrict() with no arguments enforces everything. Pass a
// [StrictConfig] to tune each check to enforce, warn, or off, exempt
// specific routes, or harden the missing-manifest posture. The zero
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

// enforceStrict runs the Mount-time strict checks — everything that
// judges only the host's own configuration and the screens registered
// before Mount — and panics with every enforced finding at once. Boot
// time, before any traffic, so a strict app can never serve a surface
// that fails its enforced checks. Panic (not error) is the framework's
// contract for configuration it cannot honor, same as route conflicts
// and fanout-without-secret.
//
// The internal-link check is NOT here: it resolves chrome hrefs against
// the route table, which is still partial at Mount (batteries and
// plugins register during App.Start, and Start itself registers more
// after them). It runs in ValidateBoot instead, on the complete table.
func (ds *UIHost) enforceStrict() {
	if !ds.strict {
		return
	}
	cfg := ds.strictConfig
	var findings []strictFinding
	findings = append(findings, ds.strictScreenFindings(cfg)...)
	findings = append(findings, ds.strictSiteFindings()...)
	if dev.Enabled() {
		findings = append(findings, ds.strictAxeCoverageFindings(cfg)...)
	}
	ds.enforceFindings(findings)
}

// ValidateBoot runs the strict checks that need the app's COMPLETE route
// table: the internal-link check, which resolves every chrome href
// against everything the app will serve. At Mount that table is partial
// — batteries and plugins register routes during App.Start's InitPlugins
// phase, App.Start registers more after them (OpenAPI, /mcp, health,
// well-knowns), and UIHost's own conditional endpoints register at the
// tail of Mount — so a link check there flags working links (a sidebar
// "Back office" → /admin panic-boots an app that serves it).
//
// App.Start calls this through framework.BootValidator after the last
// route registration and before the listener binds: the latest point at
// which a finding can still refuse to serve. A host mounted outside a
// framework.App never reaches this point and never gets the link check;
// nothing owns the "route table is complete" moment for such a host.
// Panic contract matches every other strict check.
func (ds *UIHost) ValidateBoot() {
	if !ds.strict || ds.strictConfig.level(strictCheckInternalLinks) == StrictOff {
		return
	}
	ds.enforceFindings(ds.strictChromeLinkFindings(ds.strictConfig))
}

// enforceFindings routes findings through their configured levels:
// off-level findings are dropped, warn-level findings are logged, and
// the enforced remainder panics together, each with its remedy. Shared
// by the Mount-time checks and ValidateBoot so both have identical
// warn/enforce semantics.
func (ds *UIHost) enforceFindings(findings []strictFinding) {
	cfg := ds.strictConfig
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
	b.WriteString("strict mode is opt-in (uihost.WithStrict): fix the findings above; they are ordered, independent, and each names its remedy. To relax a check deliberately, pass a StrictConfig (levels: enforce/warn/off, per-route ExemptScreens).")
	panic(b.String())
}

// strictScreenFindings checks per-screen SEO completeness for every
// page-type screen. Drawers, sheets, and dialogs are skipped. They
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
				"screen %q: no title: implement ScreenTitler on the component or register with Screen.WithTitle", screen.Path)})
		}
		if _, deliberate := screen.Component.(ScreenSEO); screen.Description == "" && !deliberate {
			out = append(out, strictFinding{strictCheckScreenDescriptions, fmt.Sprintf(
				"screen %q: no description: implement ScreenDescriber (or ScreenSEO; a zero-value ScreenSEO return deliberately opts the page out)", screen.Path)})
		}
	}
	return out
}

// strictSiteFindings checks the site-level surface a public app should
// never ship without.
func (ds *UIHost) strictSiteFindings() []strictFinding {
	var out []strictFinding
	if !ds.siteDescription {
		out = append(out, strictFinding{strictCheckSiteDescription, "site: no description: add uihost.WithDescription"})
	}
	if ds.faviconURL == "" && len(ds.appIcons) == 0 {
		out = append(out, strictFinding{strictCheckSiteIcon, "site: no icon: add uihost.WithAppIcon (one source image) or uihost.WithFavicon"})
	}
	switch {
	case ds.sitemapConfig == nil:
		out = append(out, strictFinding{strictCheckSitemap, "site: no sitemap: add uihost.WithSitemap so crawlers and the a11y audit can discover every route"})
	default:
		if reason := invalidSitemapBaseURL(ds.sitemapConfig.BaseURL); reason != "" {
			out = append(out, strictFinding{strictCheckSitemap, fmt.Sprintf(
				"site: sitemap BaseURL %q %s: <loc> entries must be absolute URLs (scheme + host), e.g. https://example.com", ds.sitemapConfig.BaseURL, reason)})
		}
	}
	if ds.robotsConfig == nil {
		out = append(out, strictFinding{strictCheckRobots, "site: no robots directives: add uihost.WithRobots"})
	}
	return out
}

// strictChromeLinkFindings renders the site chrome — every layout's
// header, sidebar, and footer — and reports internal links that point
// at a path nothing serves. The chrome is where a route reference is
// still declarative at boot (nav, footer, and sidebar config), and it
// needs no request: layouts render their components with a background
// is the same computation the first request performs.
//
// Scope, deliberately narrow on the input side so the output side can
// be strict:
//
//   - <a href> only. Form actions and data-fui-rpc attributes target
//     handlers, not pages; flagging them would misfire on every CRUD
//     form and API post in the chrome.
//   - A chrome component whose render fails is skipped with a warning,
//     not flagged: a context-aware header that needs a signed-in user
//     is legitimate, and strict must not demand a request at boot.
//     Coverage, not enforcement, is the honest limit there.
//   - Only layouts reachable through the exported router surface are
//     checked (the default layout plus each screen's own). The OUTER
//     shells of nested ScreenGroups with distinct layouts at two
//     levels are resolved through unexported state and are not
//     enumerated; the generator emits no such shape.
func (ds *UIHost) strictChromeLinkFindings(cfg StrictConfig) []strictFinding {
	probe := newGETProbe(ds.coreRouter)
	// href → "layout %q %s" origin of the first chrome that carried it.
	// One finding per href, however many layouts repeat it: the fix is
	// one registration (or one edit) either way.
	broken := map[string]string{}
	for _, l := range ds.strictLayouts() {
		for _, slot := range []struct {
			name string
			comp component.Component
		}{
			{"header", l.Header},
			{"sidebar", l.Sidebar},
			{"footer", l.Footer},
		} {
			if slot.comp == nil {
				continue
			}
			html, err := component.SafeRenderCtx(context.Background(), slot.comp)
			if err != nil {
				slog.Warn("uihost strict: chrome render failed; its links are unchecked",
					"layout", l.Name, "slot", slot.name, "err", err)
				continue
			}
			for _, href := range extractChromeHrefs(string(html)) {
				path, internal := internalRoutePath(href)
				if !internal || cfg.exempt(path) {
					continue
				}
				if ds.chromeLinkResolves(path, probe) {
					continue
				}
				if _, seen := broken[href]; !seen {
					broken[href] = fmt.Sprintf("layout %q %s", l.Name, slot.name)
				}
			}
		}
	}
	if len(broken) == 0 {
		return nil
	}
	out := make([]strictFinding, 0, len(broken))
	for _, href := range slices.Sorted(maps.Keys(broken)) {
		out = append(out, strictFinding{strictCheckInternalLinks, fmt.Sprintf(
			"internal link %q (%s) points at a path nothing serves: register a screen for it, fix the link, or add its path to ExemptScreens", href, broken[href])})
	}
	return out
}

// strictLayouts enumerates the layouts the exported router surface can
// reach: the router's default layout plus the layout of every page
// screen, deduplicated and ordered by name for deterministic findings.
// ScreenByPattern (not Resolve) because Paths() yields patterns and a
// constrained pattern's own text fails its constraint at resolve time.
func (ds *UIHost) strictLayouts() []*app.Layout {
	var layouts []*app.Layout
	seen := map[*app.Layout]bool{}
	add := func(l *app.Layout) {
		if l != nil && !seen[l] {
			seen[l] = true
			layouts = append(layouts, l)
		}
	}
	add(ds.App.Router.GetDefaultLayout())
	for _, path := range ds.App.Router.Paths() {
		screen, ok := ds.App.Router.ScreenByPattern(path)
		if !ok || screen.Type != app.ScreenPage {
			continue
		}
		add(screen.Layout)
	}
	sort.Slice(layouts, func(i, j int) bool { return layouts[i].Name < layouts[j].Name })
	return layouts
}

// chromeHrefRe finds href attribute values in chrome markup. The
// leading [\s<] excludes namespaced and data- attributes (xlink:href,
// data-href): only a plain href on an element counts as a navigation
// reference. Both quote styles are accepted; design-system output is
// double-quoted, hand-rolled chrome may not be.
var chromeHrefRe = regexp.MustCompile(`[\s<]href\s*=\s*(?:"([^"]*)"|'([^']*)')`)

// extractChromeHrefs returns every href value in a chrome fragment,
// HTML-unescaped and deduplicated, in first-appearance order.
func extractChromeHrefs(html string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range chromeHrefRe.FindAllStringSubmatch(html, -1) {
		v := m[1]
		if v == "" {
			v = m[2]
		}
		v = stdhtml.UnescapeString(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// internalRoutePath reduces a chrome href to the internal path it
// references, reporting false for everything the check must not judge:
//
//   - fragments and query-only references ("" or "?…" or "#…"): they
//     target the current page;
//   - anything not starting with "/" as written: absolute URLs
//     (https://…), scheme references (mailto:, tel:), and relative
//     references (about, ./x), which resolve against a page the check
//     has no knowledge of at boot. Percent-decoding does not make
//     "%2Fdocs" absolute — the browser resolves it as a relative
//     reference — so the "/" prefix is judged on the raw text;
//   - protocol-relative references (//host/…) and any backslash
//     (urlsafe rejects both; sanitized chrome never carries them, and
//     unsanitized chrome is a security finding, not a link-integrity
//     one);
//   - template placeholders: a segment starting with ":" (":id",
//     ":path*") or any "{" / "<" — patterns to be filled in, not
//     concrete references.
//
// Percent-escapes are decoded before matching: net/http routes on the
// decoded path, so a href to "/docs/caf%C3%A9" must resolve against the
// screen registered at "/docs/café" exactly as the request would. A
// malformed escape ("%zz", a trailing "%") is left raw: no route can
// serve it (the server answers 400 to the browser's verbatim request),
// so it fails resolution and surfaces as a finding instead of crashing
// the check.
func internalRoutePath(href string) (string, bool) {
	h := href
	// Fragment and query split on the RAW text: those are the bytes the
	// browser splits on, and "%3F" is path data, not a query separator.
	if i := strings.IndexByte(h, '#'); i >= 0 {
		h = h[:i]
	}
	if i := strings.IndexByte(h, '?'); i >= 0 {
		h = h[:i]
	}
	if h == "" || !strings.HasPrefix(h, "/") {
		return "", false
	}
	if d, err := url.PathUnescape(h); err == nil {
		h = d
	}
	if strings.HasPrefix(h, "//") || strings.Contains(h, `\`) {
		return "", false
	}
	for seg := range strings.SplitSeq(h, "/") {
		if strings.HasPrefix(seg, ":") || strings.Contains(seg, "{") || strings.Contains(seg, "<") {
			return "", false
		}
	}
	return h, true
}

// chromeLinkResolves reports whether anything answers for path: a
// registered screen (dynamic routes and trailing-slash canonicals
// included, via the host's own resolution predicate), a static file,
// the favicon shortcut, or any GET route on the framework router —
// including UIHost's own endpoints and the battery/plugin/Start-time
// routes, because ValidateBoot runs on the complete table. A link to a
// served JSON or infra endpoint is not this check's business.
//
// One honest gap: a catch-all GET route (/{path...}) answers for every
// path under it, so the check is vacuously satisfied there — a link it
// covers is accepted without being verified. A catch-all MAY serve the
// path, and probing the handler to find out would mean executing the
// app at boot; guessing is worse than the gap.
func (ds *UIHost) chromeLinkResolves(path string, probe *getProbe) bool {
	if ds.resolvesStaticOrScreen(&http.Request{Method: http.MethodGet, URL: &url.URL{Path: path}}) {
		return true
	}
	return probe.serves(path)
}

// getProbe answers "does any registered route serve GET path" for the
// framework router, the same way core/router's own 404/405 detection
// does: a throwaway ServeMux loaded with the registered patterns.
// Built from Routes() because the live mux dispatches handlers; asking
// it would mean executing the app at boot.
type getProbe struct{ mux *http.ServeMux }

func newGETProbe(r *router.Router) *getProbe {
	p := &getProbe{mux: http.NewServeMux()}
	if r == nil {
		return p
	}
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, rt := range r.Routes() {
		// A pattern that conflicts with an earlier one panics on Handle;
		// skip it rather than abort the checks, mirroring router.probe.
		func() {
			defer func() { _ = recover() }()
			p.mux.Handle(rt.Pattern, noop)
		}()
	}
	return p
}

func (p *getProbe) serves(path string) bool {
	_, pattern := p.mux.Handler(&http.Request{Method: http.MethodGet, URL: &url.URL{Path: path}})
	return pattern != ""
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
	case u.Path != "" && u.Path != "/":
		return "must be a bare origin: a deployment path prefix belongs in the static Builder's BasePath, not here (it would be prefixed twice)"
	}
	return ""
}

// isDynamicRoute reports whether a route pattern contains a parameter
// or wildcard segment (":id", "{path...}").
func isDynamicRoute(pattern string) bool {
	for seg := range strings.SplitSeq(pattern, "/") {
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
// gate, hence structurally uncoverable. Strict skips it and screams
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
			// Demand follows DISCOVERY, not the interface: a StaticPaths
			// that returns no instances leaves the route just as
			// invisible to the sitemap-driven gate as no StaticPaths at
			// all.
			provider, declares := screen.Component.(app.StaticPathsProvider)
			if !declares || len(provider.StaticPaths(context.Background())) == 0 {
				invisible = append(invisible, screen.Path)
				continue
			}
		}
		pageRoutes = append(pageRoutes, screen.Path)
	}
	if len(invisible) > 0 {
		sort.Strings(invisible)
		slog.Warn("uihost strict: dynamic screens whose StaticPaths returns no instances (or is not implemented) are invisible to the sitemap and the axe gate: return concrete instances to bring them under coverage",
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
				"axe coverage unverified: no manifest at %s; run the axe suite (go test) so every screen's scan is recorded (%v)", axecov.FileName, err)}}
		}
		// A manifest that exists but cannot be read is NOT absence:
		// treating corruption as absence would relax enforcement exactly
		// when the coverage record is untrustworthy. Routed through the
		// AxeCoverage level (enforced by default).
		return []strictFinding{{strictCheckAxeCoverage, fmt.Sprintf(
			"axe coverage manifest unreadable (%v): delete %s and re-run the axe suite", err, axecov.FileName)}}
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
				"axe coverage: screen %q has no recorded axe scan: add it to the axe gate's page list (derive the list from your screen catalog so this cannot recur)", route))
		}
	}
	sort.Strings(msgs)
	out := make([]strictFinding, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, strictFinding{strictCheckAxeCoverage, msg})
	}
	return out
}
