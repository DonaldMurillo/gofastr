package uihost

import (
	"context"
	"fmt"
	stdhtml "html"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/dev"
)

// SitemapConfig configures the /sitemap.xml endpoint.
type SitemapConfig struct {
	// BaseURL is the required absolute origin of every <loc>; it may end in a static export's mount path.
	BaseURL string
	// LastMod is every page's <lastmod>; zero means the time the option ran.
	LastMod time.Time
	// ExcludePaths are route prefixes left out of the sitemap.
	ExcludePaths []string
}

// WithSitemap registers /sitemap.xml, listing every route an anonymous
// visitor can read; a dynamic route is listed only through StaticPaths.
func WithSitemap(cfg SitemapConfig) Option {
	return func(ds *UIHost) {
		ds.sitemapConfig = &cfg
		if ds.sitemapConfig.LastMod.IsZero() {
			ds.sitemapConfig.LastMod = time.Now().UTC()
		}
	}
}

// RobotsConfig configures the /robots.txt endpoint.
type RobotsConfig struct {
	// UserAgent is the group's crawler; empty means "*".
	UserAgent string
	// Allow lists permitted path prefixes; with Disallow also empty the file says "Allow: /".
	Allow []string
	// Disallow lists forbidden path prefixes.
	Disallow []string
	// SitemapURL is the sitemap's absolute URL; empty derives it from WithSitemap's BaseURL.
	SitemapURL string
	// CrawlDelay is seconds between requests; zero omits the directive.
	CrawlDelay int
}

// WithRobots registers /robots.txt; the zero RobotsConfig allows everything.
func WithRobots(cfg RobotsConfig) Option {
	return func(ds *UIHost) {
		ds.robotsConfig = &cfg
	}
}

func (ds *UIHost) handleSitemap(w http.ResponseWriter, r *http.Request) {
	files, ok := ds.liveSitemap()
	doc := ""
	if ok {
		doc = files[0].Body
	}
	serveSeoDoc(w, r, "application/xml; charset=utf-8", reasonSitemapNotConfigured, doc, ok)
}

func (ds *UIHost) handleSitemapPart(w http.ResponseWriter, r *http.Request) {
	files, _ := ds.liveSitemap()
	want := "/sitemap/" + r.PathValue("part")
	for _, f := range files {
		if f.Path == want {
			serveSeoDoc(w, r, "application/xml; charset=utf-8", reasonSitemapNotConfigured, f.Body, true)
			return
		}
	}
	ds.serveOrRender(w, r)
}

const sitemapTTL = time.Minute

type sitemapMemo struct {
	group singleflight.Group
	mu    sync.Mutex
	at    time.Time
	files []SitemapFile
}

func (ds *UIHost) liveSitemap() ([]SitemapFile, bool) {
	if dev.Enabled() {
		return ds.SitemapFiles("", nil)
	}
	m := &ds.sitemapMemo
	m.mu.Lock()
	files, at := m.files, m.at
	m.mu.Unlock()
	if time.Since(at) < sitemapTTL {
		return files, files != nil
	}
	v, _, _ := m.group.Do("", func() (any, error) {
		files, _ := ds.SitemapFiles("", nil)
		m.mu.Lock()
		m.files, m.at = files, time.Now()
		m.mu.Unlock()
		return files, nil
	})
	files = v.([]SitemapFile)
	return files, files != nil
}

func serveSeoDoc(w http.ResponseWriter, r *http.Request, contentType string, notConfigured i18n.Phrase, doc string, ok bool) {
	if !ok {
		i18n.HTTPError(w, r, http.StatusNotFound, notConfigured)
		return
	}
	w.Header().Set("Content-Type", contentType)
	//gofastr:allow(nostore) sitemap and robots bodies are built from context.Background with no request or principal, so every caller gets the same bytes; the principal handleSitemapPart can reach is serveOrRender's, on the miss path, which writes its own response.
	w.Write([]byte(doc))
}

const (
	sitemapMaxURLs  = 50000
	sitemapMaxBytes = 50 << 20
)

const (
	sitemapXMLHead     = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"
	sitemapURLSetClose = "</urlset>\n"
)

// SitemapFile is one sitemap file: "/sitemap.xml" first, then any
// "/sitemap/<n>.xml" parts it indexes.
type SitemapFile struct {
	Path string
	Body string
}

// SitemapXML returns /sitemap.xml: the URL set, or the index of its parts.
// basePath is a static deploy's mount path, "" for the live server; ok is
// false without WithSitemap.
func (ds *UIHost) SitemapXML(basePath string) (string, bool) {
	files, ok := ds.SitemapFiles(basePath, nil)
	if !ok {
		return "", false
	}
	return files[0].Body, true
}

// SitemapFiles returns every sitemap file, for the live handlers and the
// static exporter alike. basePath is as for SitemapXML. A non-nil keep is
// asked about each page by its prefixed routing path (/es/about); a page
// it refuses is neither an entry nor an alternate.
func (ds *UIHost) SitemapFiles(basePath string, keep func(page string) bool) ([]SitemapFile, bool) {
	if ds.sitemapConfig == nil || ds.App == nil {
		return nil, false
	}
	cfg := ds.sitemapConfig
	base := joinBaseURL(cfg.BaseURL, basePath)
	lastmod := cfg.LastMod.UTC().Format("2006-01-02")
	open := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n"
	if ds.pathLocales != nil {
		open = `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n"
	}
	entries := ds.sitemapEntries(base, lastmod, keep)
	return splitSitemap(base, lastmod, open, entries, sitemapMaxURLs, sitemapMaxBytes), true
}

func splitSitemap(base, lastmod, open string, entries []string, maxURLs, maxBytes int) []SitemapFile {
	frame := len(sitemapXMLHead) + len(open) + len(sitemapURLSetClose)
	var parts [][]string
	var cur []string
	size := frame
	for _, e := range entries {
		if len(cur) > 0 && (len(cur) == maxURLs || size+len(e) > maxBytes) {
			parts = append(parts, cur)
			cur, size = nil, frame
		}
		cur = append(cur, e)
		size += len(e)
	}
	urlset := func(es []string) string {
		return sitemapXMLHead + open + strings.Join(es, "") + sitemapURLSetClose
	}
	if len(parts) == 0 {
		return []SitemapFile{{Path: "/sitemap.xml", Body: urlset(cur)}}
	}
	parts = append(parts, cur)
	files := make([]SitemapFile, 1, len(parts)+1)
	var idx strings.Builder
	idx.WriteString(sitemapXMLHead)
	idx.WriteString(`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for i, part := range parts {
		p := "/sitemap/" + strconv.Itoa(i+1) + ".xml"
		fmt.Fprintf(&idx, "  <sitemap>\n    <loc>%s</loc>\n    <lastmod>%s</lastmod>\n  </sitemap>\n", stdhtml.EscapeString(base+p), lastmod)
		files = append(files, SitemapFile{Path: p, Body: urlset(part)})
	}
	idx.WriteString("</sitemapindex>\n")
	files[0] = SitemapFile{Path: "/sitemap.xml", Body: idx.String()}
	return files
}

func (ds *UIHost) sitemapEntries(base, lastmod string, keep func(page string) bool) []string {
	cfg := ds.sitemapConfig
	var out []string
	for _, route := range ds.App.Routes() {
		if route.RedirectTo != "" {
			continue
		}
		if shouldExcludePath(route.Path, cfg.ExcludePaths) {
			continue
		}
		if ds.pathLocales != nil {
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
				links := ds.readableLocaleLinks(base, p, listed)
				if keep != nil {
					links = slices.DeleteFunc(links, func(l HreflangLink) bool {
						return !keep(set.Path(l.Lang, p))
					})
				}
				for _, l := range links {
					if l.Lang == "x-default" {
						continue
					}
					var b strings.Builder
					b.WriteString("  <url>\n")
					fmt.Fprintf(&b, "    <loc>%s</loc>\n", stdhtml.EscapeString(l.URL))
					fmt.Fprintf(&b, "    <lastmod>%s</lastmod>\n", lastmod)
					for _, alt := range links {
						fmt.Fprintf(&b, "    <xhtml:link rel=\"alternate\" hreflang=\"%s\" href=\"%s\"/>\n",
							stdhtml.EscapeString(alt.Lang), stdhtml.EscapeString(urlsafe.CleanAnchor(alt.URL)))
					}
					b.WriteString("  </url>\n")
					out = append(out, b.String())
				}
			}
			continue
		}
		for _, p := range expandRouteForSitemap(context.Background(), ds.App, route.Path) {
			if keep != nil && !keep(p) {
				continue
			}
			if !ds.sitemapPublic(context.Background(), p) {
				continue
			}
			out = append(out, fmt.Sprintf("  <url>\n    <loc>%s%s</loc>\n    <lastmod>%s</lastmod>\n  </url>\n",
				base, stdhtml.EscapeString(app.EscapeRoutePath(p)), lastmod))
		}
	}
	return out
}

func joinBaseURL(baseURL, basePath string) string {
	base, sub := strings.TrimRight(baseURL, "/"), strings.TrimRight(basePath, "/")
	if sub != "" && strings.HasSuffix(base, sub) {
		return base
	}
	return base + sub
}

func pageRequest(ctx context.Context, p string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, app.EscapeRoutePath(p), nil)
}

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

func (ds *UIHost) sitemapPublic(ctx context.Context, p string) bool {
	if req, err := pageRequest(ctx, p); err == nil {
		ctx = app.WithRequest(ctx, req)
	}
	if m, ok := ds.App.Router.MatchFor(p); ok {
		ctx = app.WithMatch(ctx, m)
	}
	d, ok, err := ds.App.PagePolicy(ctx, p)
	if !ok {
		return true
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

// RobotsTXT returns /robots.txt, for the live handler and the static
// exporter. basePath changes only the derived Sitemap URL; with path
// locales each Allow and Disallow prefix repeats under every prefixed
// locale. ok is false without WithRobots.
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
	if ds.agentReady != nil && ds.agentReady.allowAIBots != nil && *ds.agentReady.allowAIBots {
		for _, bot := range aiBotUserAgents() {
			fmt.Fprintf(&b, "User-agent: %s\n", bot)
		}
	}
	if len(cfg.Allow) == 0 && len(cfg.Disallow) == 0 {
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
	if ds.agentReady != nil && ds.agentReady.contentSignals != "" {
		fmt.Fprintf(&b, "Content-Signal: %s\n", ds.agentReady.contentSignals)
	}
	return b.String(), true
}

func sitemapLocaleContext(set i18n.PathLocales, tag string) context.Context {
	return i18n.WithPathLocale(i18n.WithContext(context.Background(), i18n.Locale{Tag: tag}), set, tag)
}

func expandRouteForSitemap(ctx context.Context, a *app.App, pattern string) []string {
	if !isDynamicScreenPath(pattern) {
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

func aiBotUserAgents() []string {
	return []string{
		"GPTBot", "ChatGPT-User", "OAI-SearchBot",
		"ClaudeBot", "Claude-Web", "anthropic-ai",
		"Google-Extended",
		"PerplexityBot", "PerplexityUser",
		"CCBot", "Bytespider", "Applebot-Extended",
		"cohere-ai", "Meta-ExternalAgent", "Amazonbot",
	}
}
