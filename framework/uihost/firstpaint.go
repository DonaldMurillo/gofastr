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
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

const firstPaintBudget = 16 << 10

const maxCachedBundles = 256

// WithNoInlineFirstPaint keeps every head resource a link: the component
// sheets, app.css and the color-scheme bootstrap. Set it when the
// Content-Security-Policy is written where the page's hashes cannot reach
// (a proxy, a CDN rule, a <meta> tag in a custom layout).
func WithNoInlineFirstPaint() Option {
	return func(ds *UIHost) { ds.noInlineFirstPaint = true }
}

type inlineBlock struct {
	body string
	gz   int
	hash string // 'sha256-<base64>'
}

func newInlineBlock(body string) (inlineBlock, bool) {
	if strings.IndexByte(body, 0) >= 0 {
		return inlineBlock{}, false
	}
	if strings.IndexByte(body, '\r') >= 0 {
		body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	}
	low := asciicase.Lower(body)
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

func newStyleBlock(css string) (inlineBlock, bool) {
	if cssHasRelativeRef(css) {
		return inlineBlock{}, false
	}
	return newInlineBlock(css)
}

func cssHasRelativeRef(css string) bool {
	low := asciicase.Lower(css)
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

type firstPaintCache struct {
	appOnce        sync.Once
	app            inlineBlock
	appOK          bool
	bundles        sync.Map // bundleKey → bundleBlock
	n              atomic.Int32
	subsets        sync.Map // subsetKey → bundleBlock
	nSubsets       atomic.Int32
	behaviorOnce   sync.Once
	behaviorTokens map[string]struct{}
}

type bundleKey struct{ names, version string }

type bundleBlock struct {
	inlineBlock
	names     []string // the components the block carries, for data-cui-bundle
	key       bundleKey
	droppable []string
	fullURL   string
	href      string
	ok        bool
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

type firstPaintPlan struct {
	comp    *bundleBlock // nil when the page renders no component sheet
	app     inlineBlock
	scheme  inlineBlock
	linkApp bool // keep app.css cached separately when only the subset fits
}

func (p *firstPaintPlan) styleHashes() []string {
	var hashes []string
	if p.comp != nil {
		hashes = append(hashes, p.comp.hash)
	}
	if !p.linkApp {
		hashes = append(hashes, p.app.hash)
	}
	return hashes
}

func (p *firstPaintPlan) scriptHashes() []string { return []string{p.scheme.hash} }

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
	if total > firstPaintBudget && p.comp != nil && p.comp.href != "" {
		p.linkApp = true
		total -= app.gz
	}
	if total > firstPaintBudget {
		return nil, false
	}
	return p, true
}

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
		if match(asciicase.Lower(s)) {
			return true
		}
	}
	return false
}
