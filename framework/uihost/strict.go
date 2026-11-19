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
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/axecov"
	"github.com/DonaldMurillo/gofastr/framework/dev"
)

// StrictLevel is the posture of one strict check. The zero value is
// StrictEnforce.
type StrictLevel int

const (
	// StrictEnforce fails boot on findings.
	StrictEnforce StrictLevel = iota
	// StrictWarn logs each finding via slog.Warn and serves.
	StrictWarn
	// StrictOff skips the check.
	StrictOff
)

// StrictAbsencePosture is the posture of the missing-axe-manifest
// reminder. Its zero value warns.
type StrictAbsencePosture int

const (
	// StrictAbsenceWarn logs and serves.
	StrictAbsenceWarn StrictAbsencePosture = iota
	// StrictAbsenceEnforce fails boot until the axe suite has run.
	StrictAbsenceEnforce
	// StrictAbsenceOff silences the reminder.
	StrictAbsenceOff
)

// StrictConfig sets the level of each strict check. The zero value
// enforces everything.
type StrictConfig struct {
	// ScreenTitles: every page screen declares a title.
	ScreenTitles StrictLevel
	// ScreenDescriptions: every page screen declares a description or
	// implements [ScreenSEO].
	ScreenDescriptions StrictLevel
	// SiteDescription: [WithDescription] or a description tag in [WithHeadHTML].
	SiteDescription StrictLevel
	// SiteIcon: [WithAppIcon] or [WithFavicon] configured.
	SiteIcon StrictLevel
	// Sitemap: [WithSitemap] configured with a valid BaseURL.
	Sitemap StrictLevel
	// Robots: [WithRobots] configured.
	Robots StrictLevel
	// InternalLinks: every internal link in the layouts' chrome points at a
	// path the app serves. Checked by [UIHost.ValidateBoot].
	InternalLinks StrictLevel
	// AxeCoverage: every page route has a recorded axe scan in
	// .gofastr/axe-coverage.json. Evaluated only under `gofastr dev`.
	AxeCoverage StrictLevel
	// AxeManifestMissing is the posture when that manifest does not exist.
	AxeManifestMissing StrictAbsencePosture

	// ExemptScreens lists route patterns, exact or "/prefix/*", that the
	// per-screen checks and the link check skip.
	ExemptScreens []string
}

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

// WithStrict makes the host refuse to boot while the app misses launch
// hygiene: screen titles and descriptions, a site description, an icon, a
// sitemap, robots directives, working chrome links and, under `gofastr dev`,
// axe coverage. Every enforced finding panics together, each with its fix.
// Pass at most one [StrictConfig] to relax checks; a later WithStrict wins.
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
		ds.strictConfig = conf
	}
}

type strictFinding struct {
	check string
	msg   string
}

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

// ValidateBoot runs the boot checks that need the complete route and screen
// tables: it compiles late screens' actions, refuses server actions on
// embeddable screens, builds the behaviour registry and, under [WithStrict],
// checks chrome links. framework.App.Start calls it after the last route
// registration; it panics on a finding.
func (ds *UIHost) ValidateBoot() {
	ds.ensureToastStack()
	ds.AutoCompileActions()
	ds.enforceNoServerActionsOnEmbeds()
	runtime.BehaviorsJSON()
	if !ds.strict || ds.strictConfig.level(strictCheckInternalLinks) == StrictOff {
		return
	}
	ds.enforceFindings(ds.strictChromeLinkFindings(ds.strictConfig))
}

func (ds *UIHost) enforceFindings(findings []strictFinding) {
	cfg := ds.strictConfig
	var enforced []string
	for _, f := range findings {
		switch cfg.level(f.check) {
		case StrictOff:
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

func (ds *UIHost) strictScreenFindings(cfg StrictConfig) []strictFinding {
	var out []strictFinding
	for _, path := range ds.App.Router.Paths() {
		screen, ok := ds.App.Router.ScreenByPattern(path)
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

func (ds *UIHost) strictSiteFindings() []strictFinding {
	var out []strictFinding
	if ds.siteDescriptionText() == "" {
		out = append(out, strictFinding{strictCheckSiteDescription, "site: no description: add uihost.WithDescription"})
	}
	if ds.faviconURL == "" && len(ds.appIcons) == 0 {
		out = append(out, strictFinding{strictCheckSiteIcon, "site: no icon: add uihost.WithAppIcon (one source image) or uihost.WithFavicon"})
	}
	switch {
	case ds.sitemapConfig == nil:
		out = append(out, strictFinding{strictCheckSitemap, "site: no sitemap: add uihost.WithSitemap so crawlers and the a11y audit can discover every route"})
	default:
		if reason := InvalidSitemapBaseURL(ds.sitemapConfig.BaseURL); reason != "" {
			out = append(out, strictFinding{strictCheckSitemap, fmt.Sprintf(
				"site: sitemap BaseURL %q %s: <loc> entries must be absolute URLs (scheme + host), e.g. https://example.com", ds.sitemapConfig.BaseURL, reason)})
		}
	}
	if ds.robotsConfig == nil {
		out = append(out, strictFinding{strictCheckRobots, "site: no robots directives: add uihost.WithRobots"})
	}
	return out
}

func (ds *UIHost) strictChromeLinkFindings(cfg StrictConfig) []strictFinding {
	probe := newGETProbe(ds.coreRouter)
	broken := map[string]string{}
	for _, l := range ds.strictLayouts() {
		html := string(l.WrapCtx(context.Background(), ""))
		for _, href := range extractChromeHrefs(html) {
			path, internal := internalRoutePath(href)
			if !internal || cfg.exempt(path) {
				continue
			}
			if ds.chromeLinkResolves(path, probe) {
				continue
			}
			if _, seen := broken[href]; !seen {
				broken[href] = fmt.Sprintf("layout %q chrome", l.Name)
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

var chromeHrefRe = regexp.MustCompile(`[\s<]href\s*=\s*(?:"([^"]*)"|'([^']*)')`)

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

func internalRoutePath(href string) (string, bool) {
	h := href
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

func (ds *UIHost) chromeLinkResolves(path string, probe *getProbe) bool {
	if ds.pathLocales != nil {
		if tag, rest := ds.pathLocales.Split(path); tag != "" {
			path = rest
		}
	}
	if ds.resolvesStaticOrScreen(&http.Request{Method: http.MethodGet, URL: &url.URL{Path: path}}) {
		return true
	}
	return probe.serves(path)
}

type getProbe struct{ mux *http.ServeMux }

func newGETProbe(r *router.Router) *getProbe {
	p := &getProbe{mux: http.NewServeMux()}
	if r == nil {
		return p
	}
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, rt := range r.Routes() {
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

// InvalidSitemapBaseURL reports why base cannot serve as the sitemap's
// origin, or "" when it can. A valid base is a bare http or https origin:
// no userinfo, query, fragment or path.
func InvalidSitemapBaseURL(base string) string {
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
	case u.RawQuery != "" || u.Fragment != "" || u.ForceQuery:
		return "must not carry a query or fragment"
	case u.Path != "" && u.Path != "/":
		return "must be a bare origin: a deployment path prefix belongs in the static Builder's BasePath, not here (it would be prefixed twice)"
	}
	return ""
}

func (ds *UIHost) strictAxeCoverageFindings(cfg StrictConfig) []strictFinding {
	if cfg.level(strictCheckAxeCoverage) == StrictOff {
		return nil
	}
	var pageRoutes, invisible []string
	for _, path := range ds.App.Router.Paths() {
		screen, ok := ds.App.Router.ScreenByPattern(path)
		if !ok || screen.Type != app.ScreenPage || cfg.exempt(screen.Path) {
			continue
		}
		if isDynamicScreenPath(screen.Path) {
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
	if len(pageRoutes) == 0 {
		return nil
	}
	m, err := axecov.Read(axecov.DefaultDir())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []strictFinding{{strictCheckAxeManifest, fmt.Sprintf(
				"axe coverage unverified: no manifest at %s; run the axe suite (go test) so every screen's scan is recorded (%v)", axecov.FileName, err)}}
		}
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
