package uihost

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// First-paint inlining. A live page cannot paint until it holds the
// document and every render-blocking resource its <head> names: the
// color-scheme bootstrap, the component bundle and app.css. As links
// those cost a second round trip after the document, which on a slow
// link is most of the time to first paint. A page whose head resources
// are small carries them in the document instead, so the first response
// is enough to paint.
//
// The budget is in gzip-compressed bytes, the way they cross the wire
// once the app (AppConfig.Compression) or a proxy in front of it
// compresses.
// Inlining trades a first visit's round trip for a return visit's
// download: linked sheets come from the cache on the next full load,
// inlined ones arrive again inside every document. 16 KiB takes about one
// round trip on a slow mobile link (400 kbps, 300 ms), so the bytes a
// return visit pays for stay under the round trip a first visit saves. A
// heavier head keeps its links.
//
// Every inlined block is allowed by its hash in the response's own
// Content-Security-Policy (cspAllowInline), never by 'unsafe-inline'. A
// page whose policy cannot take a hash keeps its links. Static exports
// always link: their files are served by hosts whose policy the framework
// does not write.
const firstPaintBudget = 16 << 10

// maxCachedBundles bounds the per-host cache of inlined component
// bundles. The key set is the distinct component sets the app's pages
// render, a few dozen in practice; past the bound a bundle is built per
// request instead of cached.
const maxCachedBundles = 256

// WithNoInlineFirstPaint keeps every head resource a link: the component
// sheets, app.css and the color-scheme bootstrap. Set it when the
// Content-Security-Policy is written somewhere the page's hashes cannot
// reach (a proxy, a CDN rule, a <meta> tag in a custom layout), where an
// inlined block would be refused and the page would paint unstyled.
func WithNoInlineFirstPaint() Option {
	return func(ds *UIHost) { ds.noInlineFirstPaint = true }
}

// inlineBlock is a <style> or <script> body that can ship in the
// document, with its compressed size and its CSP hash source.
type inlineBlock struct {
	body string
	gz   int
	hash string // 'sha256-<base64>'
}

// newInlineBlock refuses a body that could end its element early or open
// a comment the parser would carry past the closing tag: such a body
// ships as a link instead. HTML matches end tags ASCII case-insensitively.
//
// The browser hashes the element's text after HTML preprocessing, which
// turns CR LF and a lone CR into LF, so the body is normalized the same
// way first; CSS and JS read every one of them as the same newline. A NUL
// the parser would replace refuses the block.
func newInlineBlock(body string) (inlineBlock, bool) {
	if strings.IndexByte(body, 0) >= 0 {
		return inlineBlock{}, false
	}
	if strings.IndexByte(body, '\r') >= 0 {
		body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	}
	low := asciiLower(body)
	if strings.Contains(low, "</style") || strings.Contains(low, "</script") || strings.Contains(body, "<!--") {
		return inlineBlock{}, false
	}
	sum := sha256.Sum256([]byte(body))
	return inlineBlock{
		body: body,
		gz:   gzipLen(body),
		hash: "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'",
	}, true
}

// newStyleBlock is newInlineBlock for a stylesheet. A relative URL
// resolves against the sheet's address when the sheet is linked and
// against the page's when it is inlined, so a sheet holding one keeps
// its link.
func newStyleBlock(css string) (inlineBlock, bool) {
	if cssHasRelativeRef(css) {
		return inlineBlock{}, false
	}
	return newInlineBlock(css)
}

// cssHasRelativeRef reports whether css names a resource by a relative
// URL: in a url(), in an @import string, or anywhere it uses
// image-set(), whose bare strings are URLs too. It reads text, not
// tokens, so a match inside a comment or a string counts as well; that
// only keeps a link.
func cssHasRelativeRef(css string) bool {
	low := asciiLower(css)
	if strings.Contains(low, "image-set(") {
		return true
	}
	for rest := low; ; {
		k := strings.Index(rest, "url(")
		if k < 0 {
			break
		}
		rest = rest[k+len("url("):]
		if relativeTarget(rest) {
			return true
		}
	}
	for rest := low; ; {
		k := strings.Index(rest, "@import")
		if k < 0 {
			break
		}
		rest = rest[k+len("@import"):]
		if q := strings.TrimLeft(rest, " \t\n\r\f"); q != "" && (q[0] == '"' || q[0] == '\'') && relativeTarget(q) {
			return true
		}
	}
	return false
}

// relativeTarget reports whether the URL starting s (after any space
// and an opening quote) is relative: not rooted at /, not a fragment,
// and without a scheme. An empty URL is relative.
func relativeTarget(s string) bool {
	s = strings.TrimLeft(s, " \t\n\r\f")
	if s != "" && (s[0] == '"' || s[0] == '\'') {
		s = s[1:]
	}
	if s == "" || s[0] == '/' || s[0] == '#' {
		return s == ""
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == ':':
			return i == 0
		case 'a' <= c && c <= 'z':
		case i > 0 && ('0' <= c && c <= '9' || c == '+' || c == '-' || c == '.'):
		default:
			return true
		}
	}
	return true
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

type countWriter int

func (c *countWriter) Write(p []byte) (int, error) {
	*c += countWriter(len(p))
	return len(p), nil
}

func gzipLen(s string) int {
	var n countWriter
	zw := gzip.NewWriter(&n)
	_, _ = io.WriteString(zw, s)
	_ = zw.Close()
	return int(n)
}

// firstPaintCache holds the blocks a host inlines, each built once per
// content: app.css and the bootstrap are fixed per process, a bundle is
// keyed by its component set and their versions.
type firstPaintCache struct {
	appOnce sync.Once
	app     inlineBlock
	appOK   bool
	bundles sync.Map // bundleKey → bundleBlock
	n       atomic.Int32
	// subsets holds the first-paint subsets of bundles (firstpaint_prune.go).
	subsets        sync.Map // subsetKey → bundleBlock
	nSubsets       atomic.Int32
	behaviorOnce   sync.Once
	behaviorTokens map[string]struct{}
}

type bundleKey struct{ names, version string }

type bundleBlock struct {
	inlineBlock
	names []string // the components the block carries, for data-cui-bundle
	key   bundleKey
	// droppable lists the fui- classes the bundle's selectors require,
	// the ones a subset may drop rules for.
	droppable []string
	// fullURL is the URL a linked head names for the same sheets.
	fullURL string
	// href is set on a subset: fullURL, which the runtime links at boot
	// (data-cui-bundle-href).
	href string
	ok   bool
}

var (
	schemeBlockOnce sync.Once
	schemeBlock     inlineBlock
	schemeBlockOK   bool
)

func colorSchemeBlock() (inlineBlock, bool) {
	schemeBlockOnce.Do(func() {
		if js, err := runtime.ColorSchemeJS(); err == nil {
			schemeBlock, schemeBlockOK = newInlineBlock(js)
		}
	})
	return schemeBlock, schemeBlockOK
}

func (ds *UIHost) appCSSBlock() (inlineBlock, bool) {
	c := &ds.firstPaint
	c.appOnce.Do(func() {
		body, _ := ds.appCSSCached()
		c.app, c.appOK = newStyleBlock(body)
	})
	return c.app, c.appOK
}

// bundleBlockFor builds the inline twin of the comp-bundle.css response
// for names under the host's theme: the same sheets, in the same order,
// each followed by the newline the endpoint writes.
func (ds *UIHost) bundleBlockFor(names []string) bundleBlock {
	theme := ds.activeTheme()
	th := style.ThemeHash(theme)
	kept := make([]string, 0, len(names))
	entries := make([]*registry.Entry, 0, len(names))
	versions := make([]string, 0, len(names))
	for _, n := range names {
		e, ok := registry.Lookup(n)
		if !ok || !safeComponentName(n) {
			continue
		}
		kept = append(kept, n)
		entries = append(entries, e)
		versions = append(versions, e.VersionForHash(theme, th))
	}
	key := bundleKey{names: strings.Join(kept, ","), version: hashStrings(versions...)}
	c := &ds.firstPaint
	if v, ok := c.bundles.Load(key); ok {
		return v.(bundleBlock)
	}
	b := borrowBuilder()
	defer returnBuilder(b)
	for _, e := range entries {
		b.WriteString(e.ServedCSSForHash(theme, th))
		b.WriteByte('\n')
	}
	blk, ok := newStyleBlock(b.String())
	out := bundleBlock{
		inlineBlock: blk, names: kept, key: key, ok: ok,
		droppable: droppableClasses(blk.body),
		fullURL:   liveSheetURL(names, theme, th),
	}
	if c.n.Load() < maxCachedBundles {
		if _, loaded := c.bundles.LoadOrStore(key, out); !loaded {
			c.n.Add(1)
		}
	}
	return out
}

// firstPaintPlan is what one page inlines.
type firstPaintPlan struct {
	comp   *bundleBlock // nil when the page renders no component sheet
	app    inlineBlock
	scheme inlineBlock
}

func (p *firstPaintPlan) styleHashes() []string {
	if p.comp != nil {
		return []string{p.comp.hash, p.app.hash}
	}
	return []string{p.app.hash}
}

func (p *firstPaintPlan) scriptHashes() []string { return []string{p.scheme.hash} }

// planFirstPaint returns the blocks a page with these component sheets
// inlines, or false when any block cannot be inlined or together they
// exceed the budget. A page whose whole bundle does not fit inlines the
// subset its markup can match, when that fits.
func (ds *UIHost) planFirstPaint(names []string, markup ...string) (*firstPaintPlan, bool) {
	if ds.App == nil {
		return nil, false
	}
	scheme, ok := colorSchemeBlock()
	if !ok {
		return nil, false
	}
	app, ok := ds.appCSSBlock()
	if !ok {
		return nil, false
	}
	p := &firstPaintPlan{app: app, scheme: scheme}
	total := app.gz + scheme.gz
	if len(names) > 0 {
		b := ds.bundleBlockFor(names)
		if !b.ok {
			return nil, false
		}
		if len(b.names) > 0 {
			if total+b.gz > firstPaintBudget {
				if sub, ok := ds.subsetBlockFor(b, markup...); ok {
					b = sub
				}
			}
			p.comp = &b
			total += b.gz
		}
	}
	if total > firstPaintBudget {
		return nil, false
	}
	return p, true
}

// allowInlineHashes adds the hashes to every policy on the response and
// reports whether the enforced ones can all take them. When one cannot,
// nothing is changed and the page links instead. Report-only policies are
// patched where they can be, so trying a stricter policy does not report
// the framework's own blocks.
func allowInlineHashes(h http.Header, styles, scripts []string) bool {
	const enforce, report = "Content-Security-Policy", "Content-Security-Policy-Report-Only"
	enforced := append([]string(nil), h.Values(enforce)...)
	for i, p := range enforced {
		np, ok := cspAllowInline(p, styles, scripts)
		if !ok {
			return false
		}
		enforced[i] = np
	}
	if len(enforced) > 0 {
		h.Del(enforce)
		for _, p := range enforced {
			h.Add(enforce, p)
		}
	}
	reported := append([]string(nil), h.Values(report)...)
	for i, p := range reported {
		if np, ok := cspAllowInline(p, styles, scripts); ok {
			reported[i] = np
		}
	}
	if len(reported) > 0 {
		h.Del(report)
		for _, p := range reported {
			h.Add(report, p)
		}
	}
	return true
}

type cspDirective struct {
	name string
	srcs []string
}

// cspAllowInline returns policy with the style hashes allowed for <style>
// elements and the script hashes for <script> elements. A header value
// may hold several comma-separated policies; each is patched. ok is false
// when a policy refuses the kind outright ('none'), which no hash can
// lift without loosening it.
//
// Each kind is governed by its -src-elem directive, then its -src, then
// default-src. A policy with none of them already allows inline content
// of that kind. One whose list allows 'unsafe-inline' with no nonce or
// hash is left alone: adding a hash would switch 'unsafe-inline' off.
// When default-src governs, a new -src directive carries default-src's
// sources plus the hashes, so everything default-src allowed still loads.
func cspAllowInline(policy string, styles, scripts []string) (string, bool) {
	if strings.TrimSpace(policy) == "" {
		return policy, true
	}
	parts := strings.Split(policy, ",")
	for i, part := range parts {
		np, ok := patchPolicy(part, styles, scripts)
		if !ok {
			return "", false
		}
		parts[i] = np
	}
	return strings.Join(parts, ", "), true
}

func patchPolicy(policy string, styles, scripts []string) (string, bool) {
	var ds []cspDirective
	for _, raw := range strings.Split(policy, ";") {
		f := strings.Fields(raw)
		if len(f) == 0 {
			continue
		}
		ds = append(ds, cspDirective{name: f[0], srcs: f[1:]})
	}
	find := func(name string) int {
		for i, d := range ds {
			if strings.EqualFold(d.name, name) {
				return i
			}
		}
		return -1
	}
	for _, k := range []struct {
		kind   string
		hashes []string
	}{{"style", styles}, {"script", scripts}} {
		if len(k.hashes) == 0 {
			continue
		}
		idx, fromDefault := find(k.kind+"-src-elem"), false
		if idx < 0 {
			idx = find(k.kind + "-src")
		}
		if idx < 0 {
			idx, fromDefault = find("default-src"), true
		}
		if idx < 0 {
			continue // unrestricted
		}
		srcs := ds[idx].srcs
		if hasSource(srcs, func(s string) bool { return s == "'none'" }) {
			return "", false
		}
		inlineOK := hasSource(srcs, func(s string) bool { return s == "'unsafe-inline'" })
		keyed := hasSource(srcs, func(s string) bool {
			return strings.HasPrefix(s, "'nonce-") || strings.HasPrefix(s, "'sha")
		})
		if inlineOK && !keyed {
			continue
		}
		merged := append(append([]string(nil), srcs...), k.hashes...)
		if fromDefault {
			ds = append(ds, cspDirective{name: k.kind + "-src", srcs: merged})
		} else {
			ds[idx].srcs = merged
		}
	}
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = strings.Join(append([]string{d.name}, d.srcs...), " ")
	}
	return strings.Join(out, "; "), true
}

func hasSource(srcs []string, match func(string) bool) bool {
	for _, s := range srcs {
		if match(asciiLower(s)) {
			return true
		}
	}
	return false
}
