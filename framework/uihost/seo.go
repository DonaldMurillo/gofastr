package uihost

import (
	"context"
	"fmt"
	stdhtml "html"
	"net/http"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// ─── Sitemap ───────────────────────────────────────────────────────
//
// WithSitemap registers a /sitemap.xml handler that lists every
// reachable route in the app. Dynamic routes are expanded via
// [app.StaticPathsProvider] when the screen implements it; otherwise
// they are skipped (the crawler can't reach them anyway).

// SitemapConfig configures the /sitemap.xml endpoint.
type SitemapConfig struct {
	// BaseURL is the canonical origin (scheme + host) for emitted
	// <loc> elements. Required — sitemap.xml entries must be absolute
	// URLs per the protocol spec.
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
// is fine — it ships the open default (allow everything).
func WithRobots(cfg RobotsConfig) Option {
	return func(ds *UIHost) {
		ds.robotsConfig = &cfg
	}
}

// ─── Internals ─────────────────────────────────────────────────────

func (ds *UIHost) handleSitemap(w http.ResponseWriter, _ *http.Request) {
	if ds.sitemapConfig == nil || ds.App == nil {
		http.Error(w, "sitemap not configured", http.StatusNotFound)
		return
	}
	cfg := ds.sitemapConfig
	base := strings.TrimRight(cfg.BaseURL, "/")
	lastmod := cfg.LastMod.UTC().Format("2006-01-02")

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")

	for _, route := range ds.App.Routes() {
		if shouldExcludePath(route.Path, cfg.ExcludePaths) {
			continue
		}
		paths := expandRouteForSitemap(ds.App, route.Path)
		for _, p := range paths {
			b.WriteString("  <url>\n")
			fmt.Fprintf(&b, "    <loc>%s%s</loc>\n", base, stdhtml.EscapeString(p))
			fmt.Fprintf(&b, "    <lastmod>%s</lastmod>\n", lastmod)
			b.WriteString("  </url>\n")
		}
	}

	b.WriteString(`</urlset>` + "\n")

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write([]byte(b.String()))
}

func (ds *UIHost) handleRobots(w http.ResponseWriter, _ *http.Request) {
	if ds.robotsConfig == nil {
		http.Error(w, "robots not configured", http.StatusNotFound)
		return
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
		// Open default — explicit so it's obvious the file isn't empty.
		b.WriteString("Allow: /\n")
	}
	for _, p := range cfg.Allow {
		fmt.Fprintf(&b, "Allow: %s\n", p)
	}
	for _, p := range cfg.Disallow {
		fmt.Fprintf(&b, "Disallow: %s\n", p)
	}
	if cfg.CrawlDelay > 0 {
		fmt.Fprintf(&b, "Crawl-delay: %d\n", cfg.CrawlDelay)
	}
	// AI bots explicitly DENIED: separate Disallow:/ groups. Deny is
	// stricter than any host rule, so separate groups don't shadow the
	// generic group's path-specific rules. (The ALLOW case is handled
	// above by listing the bots as consecutive User-agent lines in the
	// main group, so they inherit the host's Allow/Disallow — a
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
		sm = strings.TrimRight(ds.sitemapConfig.BaseURL, "/") + "/sitemap.xml"
	}
	if sm != "" {
		fmt.Fprintf(&b, "Sitemap: %s\n", sm)
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(b.String()))
}

// expandRouteForSitemap returns the concrete URLs to emit. Static
// routes return themselves; dynamic routes (with ":param") are
// expanded via StaticPathsProvider when the screen supports it,
// otherwise skipped.
func expandRouteForSitemap(a *app.App, pattern string) []string {
	if !strings.Contains(pattern, ":") {
		return []string{pattern}
	}
	screen, _, ok := a.Router.Resolve(pattern)
	if !ok {
		return nil
	}
	provider, ok := screen.Component.(app.StaticPathsProvider)
	if !ok {
		return nil
	}
	var out []string
	for _, params := range provider.StaticPaths(context.Background()) {
		out = append(out, applyParams(pattern, params))
	}
	return out
}

func applyParams(pattern string, params map[string]string) string {
	parts := strings.Split(pattern, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			key := strings.TrimPrefix(part, ":")
			if v, ok := params[key]; ok {
				parts[i] = v
			}
		}
	}
	return strings.Join(parts, "/")
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
