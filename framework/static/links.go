package static

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// applyStaticMode post-processes a rendered page for the serverless
// static export. It does two things:
//
//  1. Stamps <html> with data-cui-static: the runtime's static-mode
//     switch. When present, the runtime skips the widget catalog fetch,
//     no-ops data-cui-rpc dispatch, and short-circuits data-cui-open,
//     so a click on a dead demo does not fire a request that 404s
//     against the host. Live pages never carry the marker.
//
//  2. Injects a dismissible "run locally" notice via the single shared
//     styling surface (framework/ui.Banner) so server-backed demos read
//     as intentionally inactive rather than broken. When BasePath is
//     set, every root-absolute asset/nav URL is rewritten to resolve
//     under the mount path.
func (b *Builder) applyStaticMode(ctx context.Context, page string) string {
	page = stampStatic(page)
	page = applyStaticCSP(page, b.policy())
	page = b.rewriteBaseURLs(page)
	notice := string(ui.Banner(ui.BannerConfig{
		Ctx:         ctx,
		Title:       pageStaticTitle.Text(ctx),
		Body:        pageStaticBody.Text(ctx),
		Variant:     ui.BannerInfo,
		Dismissible: true,
		DismissID:   "gofastr-static-preview",
	}))
	// Inject immediately after the opening <body ...> tag.
	if j := strings.Index(page, "<body"); j >= 0 {
		if k := strings.IndexByte(page[j:], '>'); k >= 0 {
			at := j + k + 1
			page = page[:at] + notice + page[at:]
		}
	}
	return page
}

// stampStatic marks <html> with data-cui-static: the runtime's
// static-mode switch. The first "<html" in the document is always the
// real root element (it precedes any body content); the marker is
// value-agnostic, so a bare boolean attribute suffices.
func stampStatic(page string) string {
	if i := strings.Index(page, "<html"); i >= 0 {
		return page[:i+len("<html")] + " data-cui-static" + page[i+len("<html"):]
	}
	return page
}

// baseAttrURL matches a whitespace-delimited src="…", href="…", action="…",
// data-cui-push-state="…" or data-cui-match-prefix="…" whose value is
// root-absolute (leading "/") but NOT protocol-relative ("//").
// data-cui-push-state is included so combobox/palette selection targets get
// base-prefixed on subpath deploys (otherwise selecting a command navigates
// to the apex path and 404s), action so a GET form (the search page's)
// submits under the base, and data-cui-match-prefix because the runtime
// compares the section it names with the page path, which carries the base.
// The leading ([\s]) anchor ensures only real attributes match, never "data-src" /
// "srcset", and code samples are safe because core/markdown escapes quotes
// inside <code> to &quot; (so the ="… pattern never appears in rendered code
// text). Group 3 is the first path byte, re-emitted so the prefix is inserted
// after the leading slash.
var baseAttrURL = regexp.MustCompile(`([\s])(src|href|action|data-cui-push-state|data-cui-match-prefix)="/([^/])`)

// pageLinkAttr matches a root-absolute href, action or
// data-cui-push-state value (the attributes baseAttrURL prefixes), split
// into the path and its optional query or fragment.
var pageLinkAttr = regexp.MustCompile(`([\s](?:href|action|data-cui-push-state)=")(/[^"?#]*)([?#][^"]*)?"`)

// slashPageLinks writes every link to an exported page in the form its
// host serves without a redirect. A page is written as <path>/index.html,
// and GitHub Pages, S3 website hosting, Netlify, Cloudflare Pages and the
// common file servers serve a directory index at <path>/ and answer
// <path> with a 301, so a link spelled /docs cost every click, client
// navigation included, an extra round trip. pages holds the exported
// route paths ("/docs"); a link to anything else (an asset, a contributor
// file, a route the export skipped) is left as written.
func (b *Builder) slashPageLinks(page string, pages map[string]bool) string {
	return pageLinkAttr.ReplaceAllStringFunc(page, func(m string) string {
		sub := pageLinkAttr.FindStringSubmatch(m)
		p := sub[2]
		rel := p
		if b.BasePath != "" {
			var ok bool
			if rel, ok = strings.CutPrefix(p, b.BasePath); !ok || (rel != "" && rel[0] != '/') {
				return m
			}
		}
		if rel == "" {
			rel = "/" // the base path itself, the exported root
		} else if strings.HasSuffix(rel, "/") {
			return m
		}
		// A route registered with its slash (/components/) is linked
		// without one too. The page set holds routing form and an href
		// is escaped: /items/caf%C3%A9 names the page /items/café.
		if key := coreapp.RoutePath(rel); !pages[key] && !pages[key+"/"] {
			return m
		}
		return sub[1] + slashed(p) + sub[3] + `"`
	})
}

// slashed is an exported page's URL path in the form its host serves
// without a redirect: "/" stays, "/about" becomes "/about/".
func slashed(p string) string {
	if strings.HasSuffix(p, "/") {
		return p
	}
	return p + "/"
}

// pageSet is the exported route paths, as slashPageLinks looks them up.
func pageSet(paths []string) map[string]bool {
	pages := make(map[string]bool, len(paths))
	for _, p := range paths {
		pages[p] = true
	}
	return pages
}

// slashLinks applies slashPageLinks to every written page and to
// 404.html. dumpWidgetAssets applies it to each widget's chrome.
func (b *Builder) slashLinks(res Result) error {
	pages := pageSet(res.Pages)
	files := make([]string, 0, len(res.Pages)+1)
	for _, p := range res.Pages {
		files = append(files, filepath.Join(b.OutDir, pathToFile(p)))
	}
	if slices.Contains(res.Assets, "/404.html") {
		files = append(files, filepath.Join(b.OutDir, "404.html"))
	}
	for _, f := range files {
		page, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("static: slash page links: %w", err)
		}
		if out := b.slashPageLinks(string(page), pages); out != string(page) {
			if err := writeUnder(b.OutDir, f, []byte(out)); err != nil {
				return err
			}
		}
	}
	return nil
}

// rewriteBaseURLs prefixes every root-absolute asset and navigation URL in
// the page with b.BasePath. No-op when BasePath is empty (apex deploy /
// live server). Covers /__gofastr/… assets (runtime.js, modules, CSS,
// module preload links) and internal nav links (/about → /<base>/about) in a
// single pass. External (https://), protocol-relative (//host), fragment
// (#…), and relative URLs are untouched.
func (b *Builder) rewriteBaseURLs(page string) string {
	if b.BasePath == "" {
		return page
	}
	// 1. Attributes: src="…"/href="…"/action="…" (assets, nav links, forms).
	page = baseAttrURL.ReplaceAllString(page, "${1}${2}=\""+b.BasePath+"/${3}")
	// 1b. srcset: a comma-separated list of URLs, so step 1's one URL per
	// attribute does not fit it. It runs before step 2, which would
	// otherwise prefix only a first candidate under /__gofastr/.
	page = baseSrcsetAttr.ReplaceAllStringFunc(page, func(m string) string {
		sub := baseSrcsetAttr.FindStringSubmatch(m)
		return sub[1] + prefixSrcset(sub[2], b.BasePath) + `"`
	})
	// 2. Inline JSON values: the component catalog seeds
	// "stylePath":"/__gofastr/comp/<name>.css" entries the runtime
	// lazy-loads at runtime. Those aren't attributes, so the regex
	// above misses them. Prefix every quoted root-absolute /__gofastr/
	// path. Safe vs code samples: core/markdown escapes quotes in
	// <code> to &quot;, so the literal "/__gofastr/ (real double-quote
	// + path) only occurs in real JSON, never in rendered code text.
	page = strings.ReplaceAll(page, `"/__gofastr/`, `"`+b.BasePath+`/__gofastr/`)
	// 3. The route graph. The runtime takes the client-side path only for
	// a link it finds in the graph, looking the link's pathname up by
	// route path. Step 1 rewrote every href for the base while the graph
	// kept root-relative paths, so on a project page no link was ever
	// recognised and every click was a full page load.
	page = baseRouteGraph.ReplaceAllStringFunc(page, func(block string) string {
		m := baseRouteGraph.FindStringSubmatch(block)
		return m[1] + strings.ReplaceAll(m[2], `"path":"/`, `"path":"`+b.BasePath+`/`) + m[3]
	})
	// 4. The locale set: the runtime finds a link's locale prefix behind
	// the base, so it has to know the base.
	// json.Marshal escapes < > &, so the value cannot close the script.
	base, _ := json.Marshal(b.BasePath)
	page = baseLocales.ReplaceAllStringFunc(page, func(m string) string {
		return baseLocales.FindStringSubmatch(m)[1] + `"base":` + string(base)
	})
	return page
}

// baseSrcsetAttr matches a whitespace-delimited srcset="…" or
// imagesrcset="…" (a preload link's spelling of the same list).
var baseSrcsetAttr = regexp.MustCompile(`([\s](?:srcset|imagesrcset)=")([^"]*)"`)

// prefixSrcset puts base in front of every root-absolute URL in a srcset
// value. A <picture>'s <source srcset> is what the browser loads, so with
// only <img src> prefixed, ui.Image and ui.PipelineImage drew nothing on
// a subpath deploy. The list is read the way the HTML parser reads it: a
// URL runs to the next whitespace, so the comma inside a data: URL is
// part of it, and its descriptors run to the next comma.
func prefixSrcset(v, base string) string {
	space := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }
	var sb strings.Builder
	for i := 0; i < len(v); {
		j := i
		for j < len(v) && (space(v[j]) || v[j] == ',') {
			j++
		}
		sb.WriteString(v[i:j])
		i = j
		for j < len(v) && !space(v[j]) {
			j++
		}
		url := v[i:j]
		if strings.HasPrefix(url, "/") && !strings.HasPrefix(url, "//") {
			sb.WriteString(base)
		}
		sb.WriteString(url)
		i = j
		if strings.HasSuffix(url, ",") {
			continue // no descriptors: the comma ended the candidate
		}
		for j < len(v) && v[j] != ',' {
			j++
		}
		sb.WriteString(v[i:j])
		i = j
	}
	return sb.String()
}

// baseLocales matches the base field of the locale set a page of an app
// that routes by locale prefix embeds.
var baseLocales = regexp.MustCompile(`(<script type="application/json" id="gofastr-locales">[^<]*?)"base":""`)

// baseRouteGraph matches the route graph the host embeds in every page,
// whose "path" values are the keys the runtime's navigation looks links
// up by.
var baseRouteGraph = regexp.MustCompile(`(?s)(<script type="application/json" id="gofastr-routes">)(.*?)(</script>)`)

// keepUnprefixed is the locale rule's Keep: a link a locale tree must
// leave unprefixed. The live router serves every path under each locale
// prefix, but the export copies the static source and ExtraDirs once,
// to its root, and writes pages only for routes, so a link to a copied
// file, or to any path no route or redirect takes, keeps its root path.
// A copied file wins over a route at the same path, as it does live.
func (b *Builder) keepUnprefixed(p string) bool {
	dec, err := url.PathUnescape(p)
	if err != nil {
		dec = p
	}
	if rel := strings.TrimPrefix(dec, "/"); fs.ValidPath(rel) && rel != "." {
		if b.staticSourceFile(rel) {
			return true
		}
		for mount, fsys := range b.extraDirs {
			sub, ok := strings.CutPrefix(rel, strings.Trim(mount, "/")+"/")
			if strings.Trim(mount, "/") == "" {
				sub, ok = rel, true
			}
			if ok && fsys != nil && isFile(fsys, sub) {
				return true
			}
		}
	}
	// The router and redirect table key on routing form: decoded, a
	// "/" or "%" inside one segment still %2F or %25. An href is
	// escaped, so /caf%C3%A9 must be read as /café.
	rp := coreapp.RoutePath(p)
	if _, _, ok := b.Host.App.Router.Resolve(rp); ok {
		return false
	}
	if _, ok := b.Host.App.ResolveRedirect(rp); ok {
		return false
	}
	return true
}

// rewriteJSAsset bakes b.BasePath into a JS asset's hardcoded /__gofastr/…
// URL literals (runtime.js constructs split-module and endpoint URLs at
// runtime; those aren't in the HTML to rewrite). No-op when BasePath is
// empty. The live server serves the original, unmodified asset.
func (b *Builder) rewriteJSAsset(body []byte) []byte {
	if b.BasePath == "" {
		return body
	}
	return []byte(strings.ReplaceAll(string(body), "/__gofastr/", b.BasePath+"/__gofastr/"))
}
