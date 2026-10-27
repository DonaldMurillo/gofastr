package uihost

import (
	"context"
	"fmt"
	stdhtml "html"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// ─── Sitemap ───────────────────────────────────────────────────────
//
// WithSitemap registers a /sitemap.xml handler that lists every
// reachable route in the app. Dynamic routes are expanded via
// [app.StaticPathsProvider] when the screen implements it; otherwise
// they are skipped (the crawler can't reach them anyway). A screen whose
// policy redirects or blocks a signed-out visitor is skipped too.

// SitemapConfig configures the /sitemap.xml endpoint.
type SitemapConfig struct {
	// BaseURL is the canonical origin (scheme + host) for emitted
	// <loc> elements. Required: sitemap.xml entries must be absolute
	// URLs per the protocol spec. It may carry the subpath a static
	// export is mounted under ("https://x.github.io/docs" beside
	// --export-base /docs); the export then adds the subpath once.
	BaseURL string

	// LastMod sets the <lastmod> timestamp emitted for every page.
	// Zero defaults to the time the server started, which is a
	// reasonable signal that anything older was content the build
	// already covered.
	LastMod time.Time

	// ExcludePaths lists route prefixes to omit from the sitemap.
	// Useful for admin routes, drafts, etc. Prefix match.
	ExcludePaths []string
}

// WithSitemap registers a /sitemap.xml handler.
func WithSitemap(cfg SitemapConfig) Option {
	return func(ds *UIHost) {
		ds.sitemapConfig = &cfg
		if ds.sitemapConfig.LastMod.IsZero() {
			ds.sitemapConfig.LastMod = time.Now().UTC()
		}
	}
}

// ─── Robots.txt ────────────────────────────────────────────────────

// RobotsConfig configures the /robots.txt endpoint.
type RobotsConfig struct {
	// UserAgent is the target crawler. Empty defaults to "*" (all
	// crawlers).
	UserAgent string

	// Allow lists the path prefixes the crawler may visit. Empty +
	// empty Disallow is the open default ("Allow: /").
	Allow []string

	// Disallow lists path prefixes the crawler must not visit.
	Disallow []string

	// SitemapURL is the absolute URL of the sitemap. When empty and
	// WithSitemap was also configured, the handler derives it from
	// SitemapConfig.BaseURL + "/sitemap.xml".
	SitemapURL string

	// CrawlDelay seconds between requests. Zero omits the directive.
	CrawlDelay int
}

// WithRobots registers a /robots.txt handler. A nil-zero RobotsConfig
// is fine: it ships the open default (allow everything).
func WithRobots(cfg RobotsConfig) Option {
	return func(ds *UIHost) {
		ds.robotsConfig = &cfg
	}
}

// ─── Internals ─────────────────────────────────────────────────────

func (ds *UIHost) handleSitemap(w http.ResponseWriter, r *http.Request) {
	doc, ok := ds.SitemapXML("")
	serveSeoDoc(w, r, "application/xml; charset=utf-8", reasonSitemapNotConfigured, doc, ok)
}

// serveSeoDoc is the shared handler body behind handleSitemap and
// handleRobots, which were verbatim copies of each other differing only
// in content type, not-configured message, and the document builder they
// call: 404 with that message when the doc is unconfigured, otherwise the
// doc bytes under contentType.
func serveSeoDoc(w http.ResponseWriter, r *http.Request, contentType string, notConfigured i18n.Phrase, doc string, ok bool) {
	if !ok {
		i18n.HTTPError(w, r, http.StatusNotFound, notConfigured)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Write([]byte(doc))
}

// SitemapXML builds the sitemap document WithSitemap configured. It is
// the single source for both the live /sitemap.xml handler and the
// static exporter (framework/static), which writes the same bytes to
// disk. basePath is the URL subpath a static deploy is mounted under
// (e.g. "/gofastr" for a GitHub Pages project site) and is inserted
// between the configured BaseURL and each route path; pass "" for the
// live server or an apex deploy. ok is false when WithSitemap was not
// configured.
func (ds *UIHost) SitemapXML(basePath string) (string, bool) {
	if ds.sitemapConfig == nil || ds.App == nil {
		return "", false
	}
	cfg := ds.sitemapConfig
	base := joinBaseURL(cfg.BaseURL, basePath)
	lastmod := cfg.LastMod.UTC().Format("2006-01-02")

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	if ds.pathLocales != nil {
		b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n")
	} else {
		b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	}

	for _, route := range ds.App.Routes() {
		if route.RedirectTo != "" {
			continue // redirects have no page to list
		}
		if shouldExcludePath(route.Path, cfg.ExcludePaths) {
			continue
		}
		if ds.pathLocales != nil {
			// Each locale lists the pages its own StaticPaths name: a
			// localized catalogue gives /es/ its own slugs.
			set := *ds.pathLocales
			listed := map[string]map[string]bool{}
			var paths []string
			for _, tag := range set.All() {
				listed[tag] = map[string]bool{}
				for _, p := range expandRouteForSitemap(sitemapLocaleContext(set, tag), ds.App, route.Path) {
					if !slices.Contains(paths, p) {
						paths = append(paths, p)
					}
					listed[tag][p] = true
				}
			}
			for _, p := range paths {
				// One entry per locale the page is listed and readable
				// in, each naming every such alternate, the way the
				// protocol's xhtml:link extension asks.
				links := ds.readableLocaleLinks(base, p, listed)
				for _, l := range links {
					if l.Lang == "x-default" {
						continue
					}
					b.WriteString("  <url>\n")
					fmt.Fprintf(&b, "    <loc>%s</loc>\n", stdhtml.EscapeString(l.URL))
					fmt.Fprintf(&b, "    <lastmod>%s</lastmod>\n", lastmod)
					for _, alt := range links {
						fmt.Fprintf(&b, "    <xhtml:link rel=\"alternate\" hreflang=\"%s\" href=\"%s\"/>\n",
							stdhtml.EscapeString(alt.Lang), stdhtml.EscapeString(urlsafe.CleanAnchor(alt.URL)))
					}
					b.WriteString("  </url>\n")
				}
			}
			continue
		}
		for _, p := range expandRouteForSitemap(context.Background(), ds.App, route.Path) {
			if !ds.sitemapPublic(context.Background(), p) {
				continue
			}
			b.WriteString("  <url>\n")
			fmt.Fprintf(&b, "    <loc>%s%s</loc>\n", base, stdhtml.EscapeString(app.EscapeRoutePath(p)))
			fmt.Fprintf(&b, "    <lastmod>%s</lastmod>\n", lastmod)
			b.WriteString("  </url>\n")
		}
	}

	b.WriteString(`</urlset>` + "\n")
	return b.String(), true
}

// joinBaseURL inserts a static deploy's basePath after baseURL, unless
// baseURL already ends in it: a site whose canonical URL names its
// mount ("https://x.github.io/gofastr") exported with
// --export-base /gofastr is served from /gofastr once, not
// /gofastr/gofastr.
func joinBaseURL(baseURL, basePath string) string {
	base, sub := strings.TrimRight(baseURL, "/"), strings.TrimRight(basePath, "/")
	if sub != "" && strings.HasSuffix(base, sub) {
		return base
	}
	return base + sub
}

// pageRequest is the GET a page at the routing path p arrives as when
// no live request exists (an export, the sitemap): p's segments
// escaped the way its live URL is (app.EscapeRoutePath), so a "?" or
// "#" a param holds stays in the request path instead of starting a
// query or a fragment.
func pageRequest(ctx context.Context, p string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, app.EscapeRoutePath(p), nil)
}

// readableLocaleLinks is localeLinks for the page at p, cut to the
// locales whose expansion lists p (listed, by tag) and an anonymous
// visitor can read it in. Each locale is judged in the context its
// prefixed request carries, so a policy that reads i18n.PathLocale
// decides per locale; x-default is the unprefixed page, the default
// locale's, and stays only when that one does.
func (ds *UIHost) readableLocaleLinks(base, p string, listed map[string]map[string]bool) []HreflangLink {
	set := *ds.pathLocales
	readable := map[string]bool{}
	for _, tag := range set.All() {
		readable[tag] = listed[tag][p] && ds.sitemapPublic(sitemapLocaleContext(set, tag), p)
	}
	readable["x-default"] = readable[set.Default]
	var out []HreflangLink
	for _, l := range localeLinks(set, base, p) {
		if readable[l.Lang] {
			out = append(out, l)
		}
	}
	return out
}

// sitemapPublic reports whether an anonymous visitor gets the page at the
// concrete path p: the screen's policy phase, evaluated in ctx for a bare
// GET with no session, ends in Allow or RenderAlt. A Redirect or Block is
// the same outcome that makes the static exporter skip the page
// (PolicyBlockedError), so the sitemap never lists a URL the export left
// out or a crawler cannot read. The evaluation ignores the sitemap
// request's own credentials: the document is the same for every caller.
// ctx carries only what the page request would: the locale its prefix
// names, or nothing.
func (ds *UIHost) sitemapPublic(ctx context.Context, p string) bool {
	if req, err := pageRequest(ctx, p); err == nil {
		ctx = app.WithRequest(ctx, req)
	}
	if m, ok := ds.App.Router.MatchFor(p); ok {
		ctx = app.WithMatch(ctx, m)
	}
	// The render's policy phase, resolver state included: a policy that
	// reads a resolver got ErrNoResolver from a bare ResolvePolicy, and
	// a public page vanished. A failed resolver read fails the page.
	d, ok, err := ds.App.PagePolicy(ctx, p)
	if !ok {
		return true // nothing to gate; the route list already vouched for it
	}
	if err != nil {
		return false
	}
	switch d.Kind {
	case app.DecisionRedirect, app.DecisionBlock:
		return false
	}
	return true
}

func (ds *UIHost) handleRobots(w http.ResponseWriter, r *http.Request) {
	doc, ok := ds.RobotsTXT("")
	serveSeoDoc(w, r, "text/plain; charset=utf-8", reasonRobotsNotConfigured, doc, ok)
}

// robotsPaths returns the Allow or Disallow prefixes as robots.txt
// lists them. With path locales each locale serves the app's paths
// under its prefix, so a rule for /app holds for /es/app too; "/"
// already covers every prefix.
func (ds *UIHost) robotsPaths(paths []string) []string {
	if ds.pathLocales == nil {
		return paths
	}
	var out []string
	for _, p := range paths {
		out = append(out, p)
		if p == "/" || !strings.HasPrefix(p, "/") {
			continue
		}
		for _, tag := range ds.pathLocales.Prefixed {
			out = append(out, ds.pathLocales.Path(tag, p))
		}
	}
	return out
}

// RobotsTXT builds the robots.txt document WithRobots configured. Like
// [UIHost.SitemapXML] it serves both the live handler and the static
// exporter. basePath only affects the derived Sitemap: URL (Allow/
// Disallow prefixes are emitted as authored: they are the app's own
// route prefixes, and a subpath deploy that needs them prefixed should
// author them that way). With path locales each prefix is repeated
// under every non-default locale. ok is false when WithRobots was not
// configured.
func (ds *UIHost) RobotsTXT(basePath string) (string, bool) {
	if ds.robotsConfig == nil {
		return "", false
	}
	cfg := ds.robotsConfig
	ua := cfg.UserAgent
	if ua == "" {
		ua = "*"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "User-agent: %s\n", ua)
	// When AI bots are explicitly ALLOWED, list them as consecutive
	// User-agent lines in THIS group (RFC 9309: consecutive UA lines are
	// members of one group), so they inherit the host's Allow/Disallow
	// rules below. A standalone Allow:/ group would shadow path-specific
	// Disallow rules for those bots, since a crawler applies only its
	// most-specific matching group.
	if ds.agentReady != nil && ds.agentReady.allowAIBots != nil && *ds.agentReady.allowAIBots {
		for _, bot := range aiBotUserAgents() {
			fmt.Fprintf(&b, "User-agent: %s\n", bot)
		}
	}
	if len(cfg.Allow) == 0 && len(cfg.Disallow) == 0 {
		// Open default, explicit so it's obvious the file isn't empty.
		b.WriteString("Allow: /\n")
	}
	for _, p := range ds.robotsPaths(cfg.Allow) {
		fmt.Fprintf(&b, "Allow: %s\n", p)
	}
	for _, p := range ds.robotsPaths(cfg.Disallow) {
		fmt.Fprintf(&b, "Disallow: %s\n", p)
	}
	if cfg.CrawlDelay > 0 {
		fmt.Fprintf(&b, "Crawl-delay: %d\n", cfg.CrawlDelay)
	}
	// AI bots explicitly DENIED: separate Disallow:/ groups. Deny is
	// stricter than any host rule, so separate groups don't shadow the
	// generic group's path-specific rules. (The ALLOW case is handled
	// above by listing the bots as consecutive User-agent lines in the
	// main group, so they inherit the host's Allow/Disallow. A
	// standalone Allow:/ group would shadow those rules for the bots,
	// since RFC 9309 applies only a crawler's most-specific group.)
	if ds.agentReady != nil && ds.agentReady.allowAIBots != nil && !*ds.agentReady.allowAIBots {
		for _, bot := range aiBotUserAgents() {
			b.WriteString("\n")
			fmt.Fprintf(&b, "User-agent: %s\nDisallow: /\n", bot)
		}
	}
	sm := cfg.SitemapURL
	if sm == "" && ds.sitemapConfig != nil {
		sm = joinBaseURL(ds.sitemapConfig.BaseURL, basePath) + "/sitemap.xml"
	}
	if sm != "" {
		fmt.Fprintf(&b, "Sitemap: %s\n", sm)
	}
	// Content-Signal directive (contentsignals.org): AI usage preferences.
	// A standalone directive line in robots.txt, like Sitemap.
	if ds.agentReady != nil && ds.agentReady.contentSignals != "" {
		fmt.Fprintf(&b, "Content-Signal: %s\n", ds.agentReady.contentSignals)
	}
	return b.String(), true
}

// sitemapLocaleContext is the context a request under tag's prefix
// carries, as the router and the static exporter build it.
func sitemapLocaleContext(set i18n.PathLocales, tag string) context.Context {
	return i18n.WithPathLocale(i18n.WithContext(context.Background(), i18n.Locale{Tag: tag}), set, tag)
}

// expandRouteForSitemap returns the concrete URLs to emit. Static
// routes return themselves; dynamic routes (with ":param") are
// expanded via StaticPathsProvider, under ctx (the locale a page
// request carries), when the screen supports it, otherwise skipped.
func expandRouteForSitemap(ctx context.Context, a *app.App, pattern string) []string {
	if !strings.Contains(pattern, ":") {
		return []string{pattern}
	}
	screen, ok := a.Router.ScreenByPattern(pattern)
	if !ok {
		return nil
	}
	provider, ok := screen.Component.(app.StaticPathsProvider)
	if !ok {
		return nil
	}
	var out []string
	for _, params := range provider.StaticPaths(ctx) {
		out = append(out, app.PatternPath(pattern, params))
	}
	return out
}

func shouldExcludePath(p string, excludes []string) bool {
	for _, e := range excludes {
		if e != "" && strings.HasPrefix(p, e) {
			return true
		}
	}
	return false
}

// aiBotUserAgents returns the canonical AI crawler user-agents that
// isitagentready-class scanners look for explicit rules on. Keeping it
// one place lets the list grow as new crawlers appear.
func aiBotUserAgents() []string {
	return []string{
		"GPTBot",             // OpenAI
		"ChatGPT-User",       // OpenAI user-initiated
		"OAI-SearchBot",      // OpenAI search
		"ClaudeBot",          // Anthropic
		"Claude-Web",         // Anthropic (legacy)
		"anthropic-ai",       // Anthropic
		"Google-Extended",    // Google (Gemini training/inference)
		"PerplexityBot",      // Perplexity
		"PerplexityUser",     // Perplexity user-initiated
		"CCBot",              // Common Crawl
		"Bytespider",         // ByteDance
		"Applebot-Extended",  // Apple
		"cohere-ai",          // Cohere
		"Meta-ExternalAgent", // Meta
		"Amazonbot",          // Amazon
	}
}
