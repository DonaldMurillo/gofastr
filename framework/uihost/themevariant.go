package uihost

import (
	"net/http"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

type themeVariants struct {
	mu   sync.RWMutex
	m    map[string]style.Theme
	refs map[string]int
}

// RegisterThemeVariant makes a copy of t servable as an app.css variant and
// returns its key, the fingerprint of the stylesheet served for it. Call it
// after the host's CSS contributions are installed.
func (ds *UIHost) RegisterThemeVariant(t style.Theme) string {
	t = t.Clone()

	key := style.CSSFingerprint(minify.ServedCSS(ds.AppCSSFor(t)))

	ds.variants.mu.Lock()
	defer ds.variants.mu.Unlock()
	if ds.variants.m == nil {
		ds.variants.m = make(map[string]style.Theme, 4)
	}
	if _, ok := ds.variants.m[key]; !ok {
		ds.variants.m[key] = t
	}
	if ds.variants.refs == nil {
		ds.variants.refs = make(map[string]int, 4)
	}
	ds.variants.refs[key]++
	return key
}

// ReleaseThemeVariant drops one registration of key; the last release
// removes the variant and its cached component CSS.
func (ds *UIHost) ReleaseThemeVariant(key string) {
	if key == "" {
		return
	}
	var evictHash string
	ds.variants.mu.Lock()
	n := ds.variants.refs[key]
	if n <= 1 {
		if t, ok := ds.variants.m[key]; ok {
			if h := style.ThemeHash(t); h != style.ThemeHash(ds.activeTheme()) {
				evictHash = h
			}
		}
		delete(ds.variants.refs, key)
		delete(ds.variants.m, key)
	} else {
		ds.variants.refs[key] = n - 1
	}
	ds.variants.mu.Unlock()

	if evictHash != "" {
		registry.EvictTheme(evictHash)
	}
}

func (ds *UIHost) requestTheme(r *http.Request) style.Theme {
	if r == nil {
		return ds.activeTheme()
	}
	if key := r.URL.Query().Get("t"); key != "" {
		if t, ok := ds.themeVariant(key); ok {
			return t
		}
	}
	return ds.activeTheme()
}

func (ds *UIHost) themeVariant(hash string) (style.Theme, bool) {
	if hash == "" {
		return style.Theme{}, false
	}
	ds.variants.mu.RLock()
	defer ds.variants.mu.RUnlock()
	t, ok := ds.variants.m[hash]
	return t, ok
}

// ThemeVariantCount reports how many distinct variants are registered.
func (ds *UIHost) ThemeVariantCount() int {
	ds.variants.mu.RLock()
	defer ds.variants.mu.RUnlock()
	return len(ds.variants.m)
}
