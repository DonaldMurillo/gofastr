package static

import (
	"context"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/urlprefix"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// jsonString is s as the widget catalog encodes it: a quoted JSON string
// with HTML escaping off (ServeWidgetList's encoder setting).
func jsonString(s string) string {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return strings.TrimSuffix(sb.String(), "\n")
}

// dumpWidgetAssets writes the widget catalog JSON and each widget's chrome
// HTML + CSS as query-free static files. Hidden click-to-open widgets
// (command palette, section-menu drawers, modals) are not SSR-inlined, so
// the runtime fetches their chrome from cfg.chromePath on open, which 404s
// on a serverless host. Dumping the same bytes as files lets openWidget
// resolve against the static tree, so every data-cui-open overlay works.
func (b *Builder) dumpWidgetAssets(ctx context.Context, res *Result) error {
	defs := widget.AllForSSR()
	if len(defs) == 0 {
		return nil
	}
	// Catalog JSON: canonical shape from ServeWidgetList (no page filter =
	// every widget). Its style, chrome and state URLs are root-absolute
	// values the runtime fetches at open time; prefix each one for a
	// subpath deploy. They are matched whole and quoted, so a custom
	// StylePath outside /core-ui/widget/ is prefixed too.
	rec := httptest.NewRecorder()
	widget.ServeWidgetList(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/widgets", nil))
	cat := rec.Body.String()
	if b.BasePath != "" {
		for _, d := range defs {
			for _, u := range []string{d.StyleURL(), d.ChromeURL(), d.StateURL()} {
				cat = strings.ReplaceAll(cat, jsonString(u), jsonString(b.BasePath+u))
			}
		}
	}
	if err := b.writeRawAsset("/__gofastr/widgets.json", []byte(cat)); err != nil {
		return err
	}
	res.Assets = append(res.Assets, "/__gofastr/widgets.json")
	b.log("wrote %s", "/__gofastr/widgets.json")
	pages := pageSet(res.Pages)
	locales, byPrefix := b.Host.PathLocales()
	// Every file in the export is public. A widget an anonymous caller is
	// not offered (OfferedTo: its live /chrome endpoint refuses them via
	// RequireSession or RequireAuthenticated, or its ListedFor hides it,
	// as widgets.json above does) gets its style sheet, which the live
	// host serves ungated, and no chrome.
	anon := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, d := range defs {
		if err := b.writeRawAsset(d.StyleURL(), []byte(widget.RenderCSS(d))); err != nil {
			return err
		}
		res.Assets = append(res.Assets, d.StyleURL())
		if !d.OfferedTo(anon) {
			b.log("wrote widget %s (css only: its chrome is not offered to anonymous callers)", d.Name)
			continue
		}
		// Chrome HTML may carry root-absolute nav links (/docs/…); rewrite
		// them the same way page HTML is rewritten for a subpath deploy,
		// and slash its page links the way slashLinks does a page's (a
		// section menu's links live here). Its slots render in the
		// locale of the tree it lands in, like the pages beside it.
		dctx := ctx
		if byPrefix {
			dctx = localeContext(ctx, locales, locales.Default)
		}
		chrome := b.slashPageLinks(b.rewriteBaseURLs(widget.RenderChromeCtx(dctx, d)), pages)
		if err := b.writeRawAsset(d.ChromeURL(), []byte(chrome)); err != nil {
			return err
		}
		res.Assets = append(res.Assets, d.ChromeURL())
		// On a /<tag>/ page the runtime asks for the chrome under the
		// same prefix, the way the live router serves it.
		if byPrefix {
			for _, tag := range locales.Prefixed {
				rule := urlprefix.Rule{Prefix: "/" + tag, Locales: locales.All(), Keep: b.keepUnprefixed}
				raw := widget.RenderChromeCtx(localeContext(ctx, locales, tag), d)
				local := b.slashPageLinks(b.rewriteBaseURLs(string(urlprefix.HTML([]byte(raw), rule))), pages)
				at := "/" + tag + d.ChromeURL()
				if err := b.writeRawAsset(at, []byte(local)); err != nil {
					return err
				}
				res.Assets = append(res.Assets, at)
			}
		}
		b.log("wrote widget %s (chrome + css)", d.Name)
	}
	return nil
}

// dumpSEOAssets writes sitemap.xml (with the parts it indexes, past one
// file's limits) and robots.txt when the host configured
// WithSitemap/WithRobots. Skips any file the app's static source already
// provides (the user's copy is copied verbatim later and must win); a
// user-supplied sitemap.xml replaces the generated parts too. No-op when
// neither option is configured.
func (b *Builder) dumpSEOAssets(res *Result) error {
	var files []uihost.SitemapFile
	if sitemaps, ok := b.exportedSitemap(pageSet(res.Pages)); ok {
		if b.staticSourceHas("sitemap.xml") {
			b.log("kept user-supplied /sitemap.xml")
		} else {
			files = append(files, sitemaps...)
		}
	}
	if robots, ok := b.Host.RobotsTXT(b.BasePath); ok {
		if b.staticSourceHas("robots.txt") {
			b.log("kept user-supplied /robots.txt")
		} else {
			files = append(files, uihost.SitemapFile{Path: "/robots.txt", Body: robots})
		}
	}
	for _, f := range files {
		if err := b.writeRawAsset(f.Path, []byte(f.Body)); err != nil {
			return err
		}
		res.Assets = append(res.Assets, f.Path)
		b.log("wrote %s", f.Path)
	}
	return nil
}

// sitemapLoc matches one sitemap URL.
var sitemapLoc = regexp.MustCompile(`<loc>([^<]*)</loc>`)

// exportedSitemap is the host's sitemap files with only the pages this
// export wrote: a page it skipped (an excluded route, a 404-outlet
// route, one that answers a status other than 200, one a redirect
// takes) has no file on the static host, so a crawler sent there finds
// 404.html. The host asks about each page by the routing path the
// export wrote it under, so a StaticPaths that answers differently
// per call can only drop an entry, never list an unwritten page. Page
// URLs then take the slash their links carry (slashSitemapLocs); a
// sitemap index names part files, not pages, and is left alone.
func (b *Builder) exportedSitemap(pages map[string]bool) ([]uihost.SitemapFile, bool) {
	files, ok := b.Host.SitemapFiles(b.BasePath, func(p string) bool { return pages[p] })
	if !ok {
		return nil, false
	}
	escaped := escapedPageSet(pages)
	for i := range files {
		if len(files) == 1 || i > 0 {
			files[i].Body = slashSitemapLocs(files[i].Body, escaped)
		}
	}
	return files, true
}

// escapedPageSet is pages keyed by the escaped path a sitemap <loc>
// spells (app.EscapeRoutePath), the one spelling every ID has: a
// routing-form key keeps "%" as %25 but Unicode raw, so an ID holding
// both ("café%") matched neither the loc nor its plain unescape.
func escapedPageSet(pages map[string]bool) map[string]bool {
	out := make(map[string]bool, len(pages))
	for p := range pages {
		out[coreapp.EscapeRoutePath(p)] = true
	}
	return out
}

// slashSitemapLocs gives every sitemap URL that names an exported page
// the trailing slash its links carry (see slashPageLinks), so a crawler
// is not sent through the host's 301 for every page. A <loc> is the
// site URL followed by the page path, so the page is found as a
// segment-aligned suffix of the URL's escaped path; escaped holds the
// pages in that spelling (escapedPageSet).
func slashSitemapLocs(doc string, escaped map[string]bool) string {
	return sitemapLoc.ReplaceAllStringFunc(doc, func(m string) string {
		loc := sitemapLoc.FindStringSubmatch(m)[1]
		u, err := url.Parse(stdhtml.UnescapeString(loc))
		if err != nil || u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(u.Path, "/") {
			return m
		}
		ep := u.EscapedPath()
		for i := 0; i < len(ep); i++ {
			if ep[i] == '/' && (escaped[ep[i:]] || escaped[ep[i:]+"/"]) {
				return "<loc>" + loc + "/</loc>"
			}
		}
		return m
	})
}

// dumpAppIcons writes the WithAppIcon-generated PNGs (and the
// /favicon.ico alias) to the export tree. No-op when the host has no
// generated icons.
func (b *Builder) dumpAppIcons(res *Result) error {
	icons := b.Host.AppIconAssets()
	if len(icons) == 0 {
		return nil
	}
	paths := make([]string, 0, len(icons))
	for p := range icons {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		rel := filepath.FromSlash(strings.TrimPrefix(p, "/"))
		if b.staticSourceHas(rel) {
			b.log("kept user-supplied %s", p)
			continue
		}
		if err := b.writeRawAsset(p, icons[p]); err != nil {
			return err
		}
		res.Assets = append(res.Assets, p)
		b.log("wrote %s", p)
	}
	return nil
}

// dumpRailScripts fetches each extra-script rail entry through b.Handler
// and writes it into the export. A file this build already wrote (a
// framework asset, the static dir, ExtraDirs) wins; a leftover from an
// earlier build in a reused OutDir does not. The PWA registration script
// is skipped because dumpPWAAssets writes its base-path-aware copy, and
// a src that is not a same-origin path (a CDN URL, a relative src) is
// not the app's to serve and is left to the browser.
func (b *Builder) dumpRailScripts(ctx context.Context, res *Result, routes []string, rendered *fileSet) error {
	if b.Handler == nil {
		return nil
	}
	// Keyed folded: a script in another case is the same file on a
	// case-insensitive filesystem.
	written := newFileSet()
	for _, p := range res.Assets {
		written.add(p)
	}
	for _, src := range b.Host.ExtraScriptSrcs(routes) {
		u, err := url.Parse(src)
		if err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(src, "/") || strings.HasPrefix(src, "//") || strings.HasPrefix(src, "/\\") {
			continue
		}
		// The file goes under the decoded path, the name a static host
		// resolves the request to.
		urlPath := path.Clean(u.Path)
		// rendered holds every page and redirect page, compared in any
		// case.
		if rendered.has(strings.TrimPrefix(urlPath, "/")) {
			return fmt.Errorf("static: extra script %s would overwrite the exported page at the same path", urlPath)
		}
		// The same spelling is the file the build wrote, which wins.
		// Another spelling would be skipped while every page still asks
		// for it: a 404 on a case-sensitive host.
		if own, ok := written.spelling(urlPath); ok {
			if own != urlPath {
				return caseTwinError(own, urlPath)
			}
			continue
		}
		if urlPath == pwaRegisterJS && b.Host.PWAEnabled() {
			continue
		}
		target := u.EscapedPath()
		if u.RawQuery != "" {
			target += "?" + u.RawQuery
		}
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		b.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			return fmt.Errorf("static: extra script %s answered %d; every exported page loads it", urlPath, rec.Code)
		}
		if err := b.writeRawAsset(urlPath, b.rewriteJSAsset(rec.Body.Bytes())); err != nil {
			return err
		}
		res.Assets = append(res.Assets, urlPath)
		written.add(urlPath)
		b.log("script %s", urlPath)
	}
	return nil
}
