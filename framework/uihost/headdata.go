package uihost

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
)

func catalogJSONScript(ds *UIHost) string {
	ds.catalogCacheOnce.Do(func() {
		ds.catalogCache = catalogJSONScriptFor(ds, ds.activeTheme(), "")
	})
	return ds.catalogCache
}

func catalogJSONScriptFor(ds *UIHost, theme style.Theme, variantKey string) string {
	buf := catalogJSON(ds, theme, variantKey)
	if buf == nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-catalog">` +
		escapeJSONForScript(buf) +
		`</script>`
}

func catalogJSON(ds *UIHost, theme style.Theme, variantKey string) []byte {
	registry.Freeze()
	all := registry.All()
	if len(all) == 0 {
		return nil
	}
	suffix := ""
	if variantKey != "" {
		suffix = "?t=" + url.QueryEscape(variantKey)
	}
	cat := make(map[string]map[string]any, len(all))
	for _, e := range all {
		cat[e.Name] = map[string]any{
			"stylePath": "/__gofastr/comp/" + e.Name + ".css" + suffix,
			"version":   e.VersionFor(theme),
			"loadMode":  loadModeString(e.Load),
		}
	}
	ds.catalogMarshalCount.Add(1)
	buf, err := json.Marshal(cat)
	if err != nil {
		return nil
	}
	return buf
}

func signalsJSONScript(seed map[string]any) string {
	if len(seed) == 0 {
		return ""
	}
	buf, err := json.Marshal(seed)
	if err != nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-signals">` +
		escapeJSONForScript(buf) +
		`</script>`
}

func signalPageNamesScript(seed map[string]any) string {
	var names []string
	for _, k := range slices.Sorted(maps.Keys(seed)) {
		if store.ScopeOf(k) != store.ScopeGlobal {
			names = append(names, k)
		}
	}
	if len(names) == 0 {
		return ""
	}
	buf, err := json.Marshal(names)
	if err != nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-signals-page">` + escapeJSONForScript(buf) + `</script>`
}

func runtimeStringsScript(ctx context.Context) string {
	strs := runtime.StringsJSON(ctx)
	if strs == nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-strings">` + escapeJSONForScript(strs) + `</script>`
}

func injectHeadData(ctx context.Context, page string) string {
	seed := store.SeedFor(ctx, page)
	block := signalsJSONScript(seed)
	if names := signalPageNamesScript(seed); names != "" {
		block += "\n" + names
	}
	if strs := runtimeStringsScript(ctx); strs != "" {
		if block != "" {
			block += "\n"
		}
		block += strs
	}
	if block == "" {
		return page
	}
	if i := strings.LastIndex(page, "</head>"); i >= 0 {
		return page[:i] + block + "\n" + page[i:]
	}
	return page
}

func partialSeedIsland(ctx context.Context, html string) string {
	return partialSeedIslandDelta(ctx, html, nil)
}

func partialSeedIslandDelta(ctx context.Context, html string, sent map[string]string) string {
	page, global := store.SeedSplit(ctx, html)
	if sent != nil {
		for scope, m := range map[string]map[string]any{"p": page, "g": global} {
			for k, v := range m {
				j, err := json.Marshal(v)
				if err != nil {
					continue
				}
				key := scope + ":" + k
				if prev, ok := sent[key]; ok && prev == string(j) {
					delete(m, k)
					continue
				}
				sent[key] = string(j)
			}
		}
	}
	if len(page) == 0 && len(global) == 0 {
		return ""
	}
	buf, err := json.Marshal(map[string]map[string]any{"p": page, "g": global})
	if err != nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-signals-partial">` +
		escapeJSONForScript(buf) +
		`</script>`
}

func runtimeModuleManifestScript() string {
	return widget.RuntimeModuleManifestScript()
}

func runtimeModulePreloadLinks(pageHTML string, skip ...string) string {
	mods := runtime.NeededModules(pageHTML)
	if len(mods) == 0 {
		return ""
	}
	var b strings.Builder
	for _, name := range mods {
		if slices.Contains(skip, name) {
			continue
		}
		hash := widget.RuntimeModuleHash(name)
		href := "/__gofastr/runtime/" + name + ".js"
		if hash != "" {
			href += "?v=" + hash
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(`<link rel="preload" as="script" fetchpriority="low" href="` + href + `">`)
	}
	return b.String()
}

func escapeJSONForScript(buf []byte) string {
	return strings.ReplaceAll(string(buf), "</", `<\/`)
}

var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9_:.-]+$`)

var scriptTagRe = regexp.MustCompile(`(?is)<scrip` + `t\b[^>]*?(/>|>.*?</scrip` + `t\s*>)`)

var dangerousHeadTagsRe = regexp.MustCompile(
	`(?is)<(iframe|object|style|svg|math|audio|video|form|button|picture|marquee|portal|template|noscript|foreignObject|details|summary)\b[^>]*?(/>|>(.*?</\s*\w+\s*>)?)`,
)

var voidHeadTagsRe = regexp.MustCompile(
	`(?is)<(base|embed|img|source)\b[^>]*?/?>`,
)

var eventHandlerAttrRe = regexp.MustCompile(`(?is)[\s/]on[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s/>]+)`)

var autofocusAttrRe = regexp.MustCompile(`(?is)[\s/]autofocus(\s*=\s*("[^"]*"|'[^']*'|[^\s/>]+))?`)

var metaRefreshRe = regexp.MustCompile(`(?is)<meta\b[^>]*http-equiv\s*=\s*["']?\s*refresh\s*["']?[^>]*>`)

var linkTagRe = regexp.MustCompile(`(?is)<link\b[^>]*?/?>`)

func stripInlineScripts(s string) string {
	if s == "" {
		return s
	}
	for range headScrubMaxPasses {
		next := stripInlineScriptsOnce(s)
		if next == s {
			return s
		}
		s = next
	}
	headScrubCapWarnOnce.Do(func() {
		slog.Default().Warn("uihost: caller-supplied head HTML still changed after the scrub's pass cap; dropping it",
			"passes", headScrubMaxPasses,
			"fix", "remove the nested script, handler or refresh markup from WithHeadHTML / SEOScreen.HeadHTML")
	})
	return ""
}

const headScrubMaxPasses = 8

var headScrubCapWarnOnce sync.Once

func stripInlineScriptsOnce(s string) string {
	out := scriptTagRe.ReplaceAllString(s, "")
	out = dangerousHeadTagsRe.ReplaceAllString(out, "")
	out = voidHeadTagsRe.ReplaceAllString(out, "")
	out = metaRefreshRe.ReplaceAllString(out, "")
	out = eventHandlerAttrRe.ReplaceAllString(out, "")
	out = autofocusAttrRe.ReplaceAllString(out, "")
	out = linkTagRe.ReplaceAllStringFunc(out, func(tag string) string {
		if isSafeLinkTag(tag) {
			return tag
		}
		return ""
	})
	return out
}

func isSafeLinkTag(tag string) bool {
	rel := extractAttrValue(tag, "rel")
	for _, tok := range strings.Fields(strings.ToLower(rel)) {
		switch tok {
		case "preload", "modulepreload", "prefetch":
			return false
		}
	}
	href := extractAttrValue(tag, "href")
	if href == "" {
		return true
	}
	return isSafeHeadURL(href)
}

var attrValueRegexps sync.Map

func attrValueRegexp(attr string) *regexp.Regexp {
	if re, ok := attrValueRegexps.Load(attr); ok {
		return re.(*regexp.Regexp)
	}
	re := regexp.MustCompile(`(?is)["'/\s]` + regexp.QuoteMeta(attr) + `\s*=\s*("([^"]*)"|'([^']*)'|[^\s/>]*)`)
	attrValueRegexps.Store(attr, re)
	return re
}

func extractAttrValue(tag, attr string) string {
	m := attrValueRegexp(attr).FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	switch {
	case strings.HasPrefix(m[1], `"`):
		return m[2]
	case strings.HasPrefix(m[1], `'`):
		return m[3]
	default:
		return m[1]
	}
}

func isSafeHeadURL(u string) bool {
	return urlsafe.OK(u, urlsafe.Resource)
}
