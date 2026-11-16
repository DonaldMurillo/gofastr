package uihost

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/urlprefix"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// SetPathLocales tells the host the app names the reader's locale in the
// URL prefix: pages get generated hreflang alternates (absolute on the
// WithSitemap BaseURL), prefixed canonical and og:url, one sitemap entry
// per locale, and the runtime gets the set. Call it before the first request.
func (ds *UIHost) SetPathLocales(set i18n.PathLocales) {
	ds.pathLocales = &set
}

// PathLocales returns the set SetPathLocales installed; ok is false without one.
func (ds *UIHost) PathLocales() (set i18n.PathLocales, ok bool) {
	if ds.pathLocales == nil {
		return i18n.PathLocales{}, false
	}
	return *ds.pathLocales, true
}

func (ds *UIHost) siteBase() string {
	if ds.sitemapConfig == nil {
		return ""
	}
	return strings.TrimRight(ds.sitemapConfig.BaseURL, "/")
}

func (ds *UIHost) localeAlternates(ctx context.Context, pagePath string) []HreflangLink {
	base := ds.siteBase()
	if ds.pathLocales == nil || base == "" {
		return nil
	}
	basePath, _ := ctx.Value(staticBasePathKey{}).(string)
	base = joinBaseURL(base, basePath)
	return localeLinks(*ds.pathLocales, base, pagePath)
}

type staticBasePathKey struct{}

// WithStaticBasePath sets a static export's mount path, which generated
// hreflang URLs carry, for RenderStaticPage and RenderStaticPageResult.
func WithStaticBasePath(ctx context.Context, basePath string) context.Context {
	return context.WithValue(ctx, staticBasePathKey{}, basePath)
}

func localeLinks(set i18n.PathLocales, base, pagePath string) []HreflangLink {
	p := app.EscapeRoutePath(pagePath)
	var out []HreflangLink
	for _, tag := range set.All() {
		out = append(out, HreflangLink{Lang: tag, URL: base + set.Path(tag, p)})
	}
	return append(out, HreflangLink{Lang: "x-default", URL: base + p})
}

func (ds *UIHost) localizeSEO(ctx context.Context, pagePath string, s SEO) SEO {
	if len(s.Hreflangs) == 0 {
		s.Hreflangs = ds.localeAlternates(ctx, pagePath)
	}
	s.Canonical = ds.localizeURL(ctx, s.Canonical)
	if s.OG != nil && s.OG.URL != "" {
		og := *s.OG
		og.URL = ds.localizeURL(ctx, og.URL)
		s.OG = &og
	}
	return s
}

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

func (ds *UIHost) localesJSONScript() string {
	b := ds.localesJSON("")
	if b == nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-locales">` + escapeJSONForScript(b) + `</script>`
}
