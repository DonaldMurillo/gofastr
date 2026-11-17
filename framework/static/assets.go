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

func jsonString(s string) string {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return strings.TrimSuffix(sb.String(), "\n")
}

func (b *Builder) dumpWidgetAssets(ctx context.Context, res *Result) error {
	defs := widget.AllForSSR()
	if len(defs) == 0 {
		return nil
	}
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
		dctx := ctx
		if byPrefix {
			dctx = localeContext(ctx, locales, locales.Default)
		}
		chrome := b.slashPageLinks(b.rewriteBaseURLs(widget.RenderChromeCtx(dctx, d)), pages)
		if err := b.writeRawAsset(d.ChromeURL(), []byte(chrome)); err != nil {
			return err
		}
		res.Assets = append(res.Assets, d.ChromeURL())
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

var sitemapLoc = regexp.MustCompile(`<loc>([^<]*)</loc>`)

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

func escapedPageSet(pages map[string]bool) map[string]bool {
	out := make(map[string]bool, len(pages))
	for p := range pages {
		out[coreapp.EscapeRoutePath(p)] = true
	}
	return out
}

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

func (b *Builder) dumpRailScripts(ctx context.Context, res *Result, routes []string, rendered *fileSet) error {
	if b.Handler == nil {
		return nil
	}
	written := newFileSet()
	for _, p := range res.Assets {
		written.add(p)
	}
	for _, src := range b.Host.ExtraScriptSrcs(routes) {
		u, err := url.Parse(src)
		if err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(src, "/") || strings.HasPrefix(src, "//") || strings.HasPrefix(src, "/\\") {
			continue
		}
		urlPath := path.Clean(u.Path)
		if rendered.has(strings.TrimPrefix(urlPath, "/")) {
			return fmt.Errorf("static: extra script %s would overwrite the exported page at the same path", urlPath)
		}
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
