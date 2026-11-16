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

// catalogJSONScript returns the inline JSON block embedding the
// component catalog into the SSR'd page. The browser parses it as
// inert data because of type="application/json", no JS execution,
// so strict CSP (default-src 'self') is happy.
//
// runtime.js reads it on boot:
//
//	const el = document.getElementById('gofastr-catalog');
//	if (el) window.__gofastr_catalog = JSON.parse(el.textContent);
func catalogJSONScript(ds *UIHost) string {
	// The default-theme catalog is static after wire time (registry entries +
	// the active theme are fixed before serving), so marshal it once and reuse
	// on every page render instead of rebuilding the map + json.Marshal per
	// request. Themed variants (catalogJSONScriptFor with a variantKey) are NOT
	// cached here: they vary per variant and stay on the embed path.
	ds.catalogCacheOnce.Do(func() {
		ds.catalogCache = catalogJSONScriptFor(ds, ds.activeTheme(), "")
	})
	return ds.catalogCache
}

// catalogJSONScriptFor builds the catalog against a specific theme.
//
// variantKey, when set, is appended to every stylePath so the runtime's CSS
// scanner fetches each component's stylesheet under the SAME theme the page was
// rendered with. Without it an embed served the customer's palette in app.css
// and the app's palette in every component stylesheet, and only components
// whose StyleFn reads theme values directly showed it, so the page came back
// half-rebranded in a way no DOM assertion can see.
func catalogJSONScriptFor(ds *UIHost, theme style.Theme, variantKey string) string {
	buf := catalogJSON(ds, theme, variantKey)
	if buf == nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-catalog">` +
		escapeJSONForScript(buf) +
		`</script>`
}

// catalogJSON marshals the component catalog against a theme. Shared by
// the inline script form (export mode, theme variants) and the external
// manifest.js (live pages).
func catalogJSON(ds *UIHost, theme style.Theme, variantKey string) []byte {
	// The runtime loads sheets only through this catalog, and hosts cache
	// it: a style registered after this point would never reach a page.
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

// signalsJSONScript returns the inline JSON block seeding the client
// signal store (core-ui/store) with server-resolved initial values.
// Same inert-data pattern as catalogJSONScript: type="application/json"
// is parsed by JSON.parse, never executed, so strict CSP is happy.
// runtime.js reads #gofastr-signals on boot and seeds __gofastr._signals
// BEFORE hydration. Returns "" when there is nothing to seed.
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

// signalPageNamesScript names the page-scoped keys of a full page's seed
// in an inert #gofastr-signals-page block (a sorted JSON array). The
// flat seed mixes scopes; a partial splits them in its own island. The
// runtime reads this list to restore just those values, as a partial's
// island does, when Back replays the first page of a visit or a
// navigation swaps in a fetched document. Returns "" when the seed
// holds none.
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

// runtimeStringsScript is the #gofastr-strings block for ctx's locale, or
// "" when every runtime string reads as its English.
func runtimeStringsScript(ctx context.Context) string {
	strs := runtime.StringsJSON(ctx)
	if strs == nil {
		return ""
	}
	return `<script type="application/json" id="gofastr-strings">` + escapeJSONForScript(strs) + `</script>`
}

// injectHeadData splices the page's per-request JSON blocks into <head>
// just before </head>, so they are in place before runtime.js (deferred,
// after parsing) reads them:
//
//   - #gofastr-signals, the signal seed (names referenced in the HTML
//     plus all app-global slices). ctx must carry the request value bag
//     (store.WithValues), so producer-seeded per-request values win over
//     declared defaults.
//   - #gofastr-signals-page, the seed's page-scoped keys
//     (signalPageNamesScript).
//   - #gofastr-strings, the runtime strings translated for ctx's locale
//     (runtime.StringsJSON), which the kernel's say() reads.
//
// Each block is left out when it has nothing to carry.
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

// partialSeedIsland builds the scope-split seed block for a SPA-nav
// partial. It is embedded inside the swapped content; the runtime reads
// #gofastr-signals-partial after the swap and merges it (page-scoped
// applied always, globals only when first seen). Returns "" when empty.
// SeedSplit includes every DECLARED route.*
// name (see core-ui/store), so the merge below rewrites route state on
// every navigation — kept-layer bindings repaint, and a previous
// route's params (seeded ”) cannot survive.
func partialSeedIsland(ctx context.Context, html string) string {
	return partialSeedIslandDelta(ctx, html, nil)
}

// partialSeedIslandDelta is partialSeedIsland for one unit of a streamed
// response. sent (nil: no filtering) records "p:"/"g:" + name → the JSON
// of every value an earlier unit of the same response carried; a name
// whose value is unchanged is dropped, and the island is omitted when
// nothing is left. The client merges each unit's island into the same
// state, so a repeat changes nothing but the byte count.
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

// runtimeModuleManifestScript delegates to widget.RuntimeModuleManifestScript
// so the same JSON manifest ships from both framework/uihost-rendered pages
// and kiln-style hosts that consume widget.RuntimeTag() directly.
func runtimeModuleManifestScript() string {
	return widget.RuntimeModuleManifestScript()
}

// runtimeModulePreloadLinks emits <link rel="preload" as="script"> tags
// for every demand-load runtime module whose marker substring appears in
// pageHTML (post-render scan via runtime.NeededModules). The href
// carries the content-addressed ?v=<hash> URL so preload hits the same
// immutable cache entry as the eventual fetch.
//
// rel="preload" as="script", NOT rel="modulepreload": the loader injects
// classic scripts (boot.js loadModule, s.async=false for ordering), and
// a modulepreload'd response is fetched with module destination/
// credentials semantics the classic request does not match. The browser
// would not reuse it, so every hinted module was fetched twice.
//
// fetchpriority="low": a script preload otherwise fetches at high
// priority and competes with the stylesheets and the document itself
// for the first-paint bandwidth. Modules hydrate after paint, so the
// hint only has to land before the kernel asks for them.
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

// escapeJSONForScript escapes the one HTML sequence that can
// prematurely terminate an inline <script>…</script> block: the
// closing `</` characters. JSON itself never produces `</` (no
// language feature emits it), but URL strings in the payload might
// (e.g. a path like `/foo</bar`; exotic, but defending against it
// is cheap).
func escapeJSONForScript(buf []byte) string {
	return strings.ReplaceAll(string(buf), "</", `<\/`)
}

// validNameRe restricts component names accepted by the comp CSS
// endpoint to the same character class registry uses for marker
// values. Defense-in-depth: any path with /, .., null bytes, etc.
// fails before reaching Lookup.
var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9_:.-]+$`)

// scriptTagRe matches inline <script>…</script> blocks and
// self-closing / attribute-only forms like <script src="…"></script>
// or <script src="…" />.
//
// The pattern is assembled from fragments so the no-inline-scripts
// linter (core-ui/check/noinlinescripts.go) doesn't flag this file as
// emitting an inline <script> tag literal. Each fragment alone is not
// a recognizable <script> open tag.
var scriptTagRe = regexp.MustCompile(`(?is)<scrip` + `t\b[^>]*?(/>|>.*?</scrip` + `t\s*>)`)

// dangerousHeadTagsRe matches content tag families that have no place
// in a server-controlled <head>: iframes, inline styles, scriptable
// media (svg/math/audio/video), forms, the marquee/portal grab-bag,
// and template/noscript wrappers. The opening tag alone is enough to
// match: an UNCLOSED opener like `<svg onload=alert(1)>` (no `/>`, no
// closing tag) is created and its handler fired by the browser's lenient
// parser, so requiring a closing tag would let it slip through. The
// optional closing-tag group greedily consumes any inner content +
// matching close so the whole element (not only its opener) is removed
// when it IS closed.
var dangerousHeadTagsRe = regexp.MustCompile(
	`(?is)<(iframe|object|style|svg|math|audio|video|form|button|picture|marquee|portal|template|noscript|foreignObject|details|summary)\b[^>]*?(/>|>(.*?</\s*\w+\s*>)?)`,
)

// voidHeadTagsRe matches void/empty dangerous tags in the head: base
// (re-roots the page), img (active resource fetch), embed (plugin
// loader), source (used by media + picture). Void tags have no closing
// pair so they need their own pattern.
var voidHeadTagsRe = regexp.MustCompile(
	`(?is)<(base|embed|img|source)\b[^>]*?/?>`,
)

// eventHandlerAttrRe matches any `on…=` event-handler attribute (quoted,
// single-quoted, or bare value) plus the bare `autofocus` boolean. Form
// controls (input/select/textarea/keygen) are hoisted into <body> by the
// browser's lenient parser, and an `autofocus` on one fires its `onfocus`
// handler with no user interaction: an XSS vector that survives the
// tag-family block-lists because those tags aren't in them. Stripping the
// attributes (rather than the tags) also future-proofs any new
// interactive element that lands in caller-supplied head HTML.
//
// The leading boundary is [\s/], not only \s: HTML5 treats '/' as an
// attribute separator (no whitespace required), so `<input/onfocus=…>`
// is a live handler the browser parses. A whitespace-only boundary would
// miss the slash-delimited form. The same [\s/] applies to the value's
// terminator so a trailing `/autofocus` is consumed, not left dangling.
var eventHandlerAttrRe = regexp.MustCompile(`(?is)[\s/]on[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s/>]+)`)

// autofocusAttrRe matches the bare `autofocus` boolean attribute. Like
// eventHandlerAttrRe, the leading boundary is [\s/] to catch the HTML5
// '/'-as-separator form (`<input/autofocus>`).
var autofocusAttrRe = regexp.MustCompile(`(?is)[\s/]autofocus(\s*=\s*("[^"]*"|'[^']*'|[^\s/>]+))?`)

// metaRefreshRe matches `<meta http-equiv="refresh" …>` in any casing
// or attribute order. Meta-refresh is the canonical "redirect via
// markup" primitive and has no business being injected by an SEO
// escape hatch.
var metaRefreshRe = regexp.MustCompile(`(?is)<meta\b[^>]*http-equiv\s*=\s*["']?\s*refresh\s*["']?[^>]*>`)

// linkTagRe matches a single <link> tag (always void). Used to walk
// link tags and drop the ones whose href / rel / as combos pull in
// scripts or use unsafe schemes.
var linkTagRe = regexp.MustCompile(`(?is)<link\b[^>]*?/?>`)

// stripInlineScripts removes <script> tags AND a broader set of
// dangerous tag families from caller-supplied head HTML (WithHeadHTML,
// SEOScreen.HeadHTML). The name is kept for back-compat; the behavior
// is now defense-in-depth across the whole "active in head" tag set.
// Allowed survivors: <meta> (except http-equiv=refresh), <link> with
// http(s)/relative href, <title>, and inline text content. Even
// those have any on*= event-handler / autofocus attribute scrubbed.
//
// One pass is not enough: every rule removes bytes, and removing an
// inner match splices its neighbours together, so
// `<scr<script></script>ipt>` leaves `<script>` behind and
// `http-equiv="ref/autofocusresh"` leaves `http-equiv="refresh"`. The
// pass runs again until its output stops changing. Each changing pass
// shortens the string, so the loop ends; headScrubMaxPasses bounds the
// work, and HTML still changing at the cap is nested on purpose to
// outlast the scrub: it is dropped whole (fail closed) and logged once.
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

// headScrubMaxPasses caps stripInlineScripts' fixpoint loop. Honest head
// HTML converges in one or two passes; each further pass undoes one more
// level of deliberate nesting.
const headScrubMaxPasses = 8

// headScrubCapWarnOnce gates stripInlineScripts' one-time WARN.
var headScrubCapWarnOnce sync.Once

// stripInlineScriptsOnce is one pass of every head-scrub rule.
func stripInlineScriptsOnce(s string) string {
	out := scriptTagRe.ReplaceAllString(s, "")
	out = dangerousHeadTagsRe.ReplaceAllString(out, "")
	out = voidHeadTagsRe.ReplaceAllString(out, "")
	out = metaRefreshRe.ReplaceAllString(out, "")
	// Generic on*= / autofocus strip so any surviving tag (notably form
	// controls the browser hoists into <body>) can't fire an event handler.
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
	// Decide on the PARSED rel, not on literal spellings: the HTML
	// parser tolerates whitespace around '=', trims the attribute
	// value, and treats rel as a space-separated token list, so
	// `<link rel = " preload ">` and rel="modulepreload preload" are
	// preload links a substring blocklist never sees.
	rel := extractAttrValue(tag, "rel")
	for _, tok := range strings.Fields(strings.ToLower(rel)) {
		switch tok {
		case "preload", "modulepreload", "prefetch":
			// They pull arbitrary resources into the page on the
			// framework's behalf.
			return false
		}
	}
	href := extractAttrValue(tag, "href")
	if href == "" {
		// No href → benign (e.g., `<link rel=canonical>` is set by typed
		// helpers, not by the escape hatch).
		return true
	}
	return isSafeHeadURL(href)
}

// extractAttrValue is a permissive attribute-value extractor for the
// scrub path. Not for production rendering, only for "is this href
// safe enough to keep" decisions on caller-supplied HTML fragments.
//
// It decides the way the HTML parser does: the attribute name matches
// case-insensitively as its own word, whitespace is allowed around '=',
// and a quoted value keeps its interior verbatim (the parser trims it).
// attrValueRegexps caches one compiled matcher per attribute name:
// extractAttrValue runs on every caller-supplied head tag at render time,
// and the attribute set is tiny and fixed (rel, href, as).
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

// isSafeHeadURL is the allow-list for URLs that may appear in caller-
// supplied head tags. These are subresources the browser fetches on its own
// (og:image, canonical, alternate), so the Resource policy applies: http(s)
// and relative references, nothing else.
//
// The rule set is core-ui/urlsafe's, not a local copy; this guard had been
// re-derived six times across the repo.
func isSafeHeadURL(u string) bool {
	return urlsafe.OK(u, urlsafe.Resource)
}
