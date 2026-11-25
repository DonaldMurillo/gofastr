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
	if j := strings.Index(page, "<body"); j >= 0 {
		if k := strings.IndexByte(page[j:], '>'); k >= 0 {
			at := j + k + 1
			page = page[:at] + notice + page[at:]
		}
	}
	return page
}

func stampStatic(page string) string {
	if i := strings.Index(page, "<html"); i >= 0 {
		return page[:i+len("<html")] + " data-cui-static" + page[i+len("<html"):]
	}
	return page
}

var baseAttrURL = regexp.MustCompile(`(?i)([\s])(src|href|action|data-cui-push-state|data-cui-match-prefix)="/([^/])`)

var pageLinkAttr = regexp.MustCompile(`(?i)([\s](?:href|action|data-cui-push-state)=")(/[^"?#]*)([?#][^"]*)?"`)

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
			rel = "/"
		} else if strings.HasSuffix(rel, "/") {
			return m
		}
		if key := coreapp.RoutePath(rel); !pages[key] && !pages[key+"/"] {
			return m
		}
		return sub[1] + slashed(p) + sub[3] + `"`
	})
}

func slashed(p string) string {
	if strings.HasSuffix(p, "/") {
		return p
	}
	return p + "/"
}

func pageSet(paths []string) map[string]bool {
	pages := make(map[string]bool, len(paths))
	for _, p := range paths {
		pages[p] = true
	}
	return pages
}

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

func (b *Builder) rewriteBaseURLs(page string) string {
	if b.BasePath == "" {
		return page
	}
	replacementBase := strings.ReplaceAll(b.BasePath, "$", "$$")
	page = baseAttrURL.ReplaceAllString(page, "${1}${2}=\""+replacementBase+"/${3}")
	page = baseSrcsetAttr.ReplaceAllStringFunc(page, func(m string) string {
		sub := baseSrcsetAttr.FindStringSubmatch(m)
		return sub[1] + prefixSrcset(sub[2], b.BasePath) + `"`
	})
	page = strings.ReplaceAll(page, `"/__gofastr/`, `"`+b.BasePath+`/__gofastr/`)
	page = baseRouteGraph.ReplaceAllStringFunc(page, func(block string) string {
		m := baseRouteGraph.FindStringSubmatch(block)
		return m[1] + strings.ReplaceAll(m[2], `"path":"/`, `"path":"`+b.BasePath+`/`) + m[3]
	})
	base, _ := json.Marshal(b.BasePath)
	page = baseLocales.ReplaceAllStringFunc(page, func(m string) string {
		return baseLocales.FindStringSubmatch(m)[1] + `"base":` + string(base)
	})
	return page
}

var baseSrcsetAttr = regexp.MustCompile(`(?i)([\s](?:srcset|imagesrcset)=")([^"]*)"`)

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
			continue
		}
		for j < len(v) && v[j] != ',' {
			j++
		}
		sb.WriteString(v[i:j])
		i = j
	}
	return sb.String()
}

var baseLocales = regexp.MustCompile(`(<script type="application/json" id="gofastr-locales">[^<]*?)"base":""`)

var baseRouteGraph = regexp.MustCompile(`(?s)(<script type="application/json" id="gofastr-routes">)(.*?)(</script>)`)

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
	rp := coreapp.RoutePath(p)
	if _, _, ok := b.Host.App.Router.Resolve(rp); ok {
		return false
	}
	if _, ok := b.Host.App.ResolveRedirect(rp); ok {
		return false
	}
	return true
}

func (b *Builder) rewriteJSAsset(body []byte) []byte {
	if b.BasePath == "" {
		return body
	}
	return []byte(strings.ReplaceAll(string(body), "/__gofastr/", b.BasePath+"/__gofastr/"))
}
