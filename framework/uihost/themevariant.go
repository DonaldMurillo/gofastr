package uihost

import (
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Theme variants let one process serve app.css under more than one palette
// without mutating App.Theme — which is process-global and shared across
// concurrent requests, so writing a per-request palette there would leak one
// visitor's theme into another's page.
//
// Two callers need this:
//
//   - an embedded surface rendering inside a customer's site with that
//     customer's brand color
//   - the theme configurator previewing an unsaved theme
//
// # Why registration rather than a request parameter
//
// A variant is resolved by CONTENT HASH against a registry the host populated.
// The request names a hash; it never supplies a theme. That is deliberate and
// load-bearing:
//
//   - Theme values reach CSS. A caller-supplied color like `red; --x:}` escapes
//     its declaration, and CSS alone can exfiltrate via attribute selectors and
//     background-image URLs. Resolving a pre-registered theme makes injection
//     unrepresentable rather than merely filtered.
//   - Component CSS is content-addressed per theme (registry.VersionFor). If a
//     request could mint an arbitrary theme it could mint unbounded distinct
//     hashes, each a guaranteed cache miss plus a fresh render — a cheap
//     amplification attack. A closed registry bounds the variant space to what
//     the host actually registered.
//
// An unknown hash therefore resolves to nothing and the caller falls back to
// the app's own theme; it never renders attacker-influenced CSS.
type themeVariants struct {
	mu sync.RWMutex
	m  map[string]style.Theme
}

// RegisterThemeVariant makes t servable as a theme variant and returns the key
// a themed surface puts in its app.css URL.
//
// # The key addresses the WHOLE stylesheet, not just the palette
//
// The served response is not the theme's :root block alone — it also carries
// framework built-ins, layout CSS, intercept CSS, global theme overrides,
// WithCustomCSS, and every style.Contribute fragment. Keying the URL on
// style.ThemeHash (the palette) while serving it `immutable` would be a false
// content address: a release that changed customCSS or a contributed fragment
// but kept the palette would reuse the URL, and browsers would hold the old
// bytes for a year against new HTML.
//
// So the key is a digest of the fully composed AppCSSFor(t) output. Registration
// is a wire-time operation and must happen after the host's CSS contributions
// are installed; registering earlier keys the URL to an incomplete stylesheet.
//
// Idempotent by content: two themes rendering identical CSS share a key and are
// stored once. A theme differing only in Name changes no pixel and is the same
// variant.
func (ds *UIHost) RegisterThemeVariant(t style.Theme) string {
	// Deep-copy the reference-typed fields BEFORE hashing or storing. Theme is
	// passed by value, but DarkColors/DarkCode are maps, so a plain copy shares
	// the caller's backing store: a later caller-side write would change what we
	// serve without changing the key, and could race a request reading the same
	// map (a fatal concurrent map read/write, not a recoverable panic).
	t.DarkColors = copyStringMap(t.DarkColors)
	t.DarkCode = copyStringMap(t.DarkCode)

	key := style.CSSFingerprint(ds.AppCSSFor(t))

	ds.variants.mu.Lock()
	defer ds.variants.mu.Unlock()
	if ds.variants.m == nil {
		ds.variants.m = make(map[string]style.Theme, 4)
	}
	if _, ok := ds.variants.m[key]; !ok {
		ds.variants.m[key] = t
	}
	return key
}

// copyStringMap returns an independently-owned copy, or nil for an empty input
// so an absent dark palette stays absent (Theme treats nil and empty alike, and
// darkSchemeCSS emits nothing for either).
func copyStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// themeVariant resolves a registered variant by hash. ok is false for an
// unknown hash — callers fall back to the app theme rather than erroring, so a
// stale URL degrades to the default palette instead of a broken page.
func (ds *UIHost) themeVariant(hash string) (style.Theme, bool) {
	if hash == "" {
		return style.Theme{}, false
	}
	ds.variants.mu.RLock()
	defer ds.variants.mu.RUnlock()
	t, ok := ds.variants.m[hash]
	return t, ok
}

// ThemeVariantCount reports how many distinct variants are registered. Exposed
// for tests and for a host that wants to alarm on unexpected growth: the count
// should track the number of themes the host deliberately registered, and a
// climbing count means something is minting themes per request.
func (ds *UIHost) ThemeVariantCount() int {
	ds.variants.mu.RLock()
	defer ds.variants.mu.RUnlock()
	return len(ds.variants.m)
}
