package uihost

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/urlprefix"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// SetPathLocales tells the host that the app names the reader's locale
// in the URL; framework.WithLocalePrefixes calls it at Mount. The host
// then gives each page that declares no hreflang one alternate per
// locale plus x-default (absolute URLs on the WithSitemap BaseURL, so
// the alternates need one), puts the request's prefix on a declared
// canonical and og:url, lists every page once per locale in the
// sitemap, and publishes the set to the runtime, which keeps client
// navigation in the reader's locale. Call it before the first request.
func (ds *UIHost) SetPathLocales(set i18n.PathLocales) {
	ds.pathLocales = &set
}

// PathLocales returns the set SetPathLocales installed; ok is false for
// an app that does not route by locale prefix. The static exporter
// writes one page tree per prefixed locale from it.
func (ds *UIHost) PathLocales() (set i18n.PathLocales, ok bool) {
	if ds.pathLocales == nil {
		return i18n.PathLocales{}, false
	}
	return *ds.pathLocales, true
}

// siteBase returns the sitemap's BaseURL without a trailing slash, or
// "" when none is configured.
func (ds *UIHost) siteBase() string {
	if ds.sitemapConfig == nil {
		return ""
	}
	return strings.TrimRight(ds.sitemapConfig.BaseURL, "/")
}

// localeAlternates returns the page's hreflang alternates: one per
// locale and x-default, the unprefixed page. Nil without path locales
// or a BaseURL, since hreflang URLs must be absolute.
func (ds *UIHost) localeAlternates(pagePath string) []HreflangLink {
	base := ds.siteBase()
	if ds.pathLocales == nil || base == "" {
		return nil
	}
	return localeLinks(*ds.pathLocales, base, pagePath)
}

// localeLinks takes the page path in routing form (app.RoutePath, a
// sitemap path) and escapes it: a "?" in it is part of the path, and a
// key value's "/" stays %2F.
func localeLinks(set i18n.PathLocales, base, pagePath string) []HreflangLink {
	p := app.EscapeRoutePath(pagePath)
	var out []HreflangLink
	for _, tag := range set.All() {
		out = append(out, HreflangLink{Lang: tag, URL: base + set.Path(tag, p)})
	}
	return append(out, HreflangLink{Lang: "x-default", URL: base + p})
}

// localizeSEO is a page's resolved SEO as the request's locale serves
// it: generated hreflang alternates when the page declares none, and
// the prefix on its canonical and og:url. The HTML head and the page's
// markdown both read it; the markdown kept the English canonical.
func (ds *UIHost) localizeSEO(ctx context.Context, pagePath string, s SEO) SEO {
	if len(s.Hreflangs) == 0 {
		s.Hreflangs = ds.localeAlternates(pagePath)
	}
	s.Canonical = ds.localizeURL(ctx, s.Canonical)
	if s.OG != nil && s.OG.URL != "" {
		og := *s.OG
		og.URL = ds.localizeURL(ctx, og.URL)
		s.OG = &og
	}
	return s
}

// localizeURL puts the request's locale prefix on a URL a page declares
// for itself (canonical, og:url), root-relative or absolute on the
// BaseURL; the response rewriter leaves <link> and <meta> alone.
func (ds *UIHost) localizeURL(ctx context.Context, u string) string {
	prefix := i18n.PathPrefix(ctx)
	if prefix == "" || u == "" {
		return u
	}
	set, _, _ := i18n.PathLocale(ctx)
	rule := urlprefix.Rule{Prefix: prefix, Locales: set.All()}
	if base := ds.siteBase(); base != "" {
		rule.Origins = []string{base}
	}
	return urlprefix.Path(u, rule)
}

// localesJSON is the locale set the runtime reads as
// window.__gofastr_locales: {"default":"en","prefixes":["es"],"base":""}.
// base is the subpath a static export is mounted under. Nil without
// path locales.
func (ds *UIHost) localesJSON(basePath string) []byte {
	if ds.pathLocales == nil {
		return nil
	}
	b, _ := json.Marshal(struct {
		Default  string   `json:"default"`
		Prefixes []string `json:"prefixes"`
		Base     string   `json:"base"`
	}{ds.pathLocales.Default, append([]string{}, ds.pathLocales.Prefixed...), basePath})
	return b
}

// localesJSONScript embeds the locale set as inert JSON for a page that
// is its own file (a static export); live pages get it from
// manifest.js.
func (ds *UIHost) localesJSONScript() string {
	b := ds.localesJSON("")
	if b == nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-locales">` + escapeJSONForScript(b) + `</script>`
}
