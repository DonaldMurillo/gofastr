package uihost

import (
	"context"
	"slices"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// The route graph and manifest.js carry each route's loading content,
// and loading content says words ("Loading…") in the reader's language,
// so both are built once per catalog locale rather than once per
// process. The key is i18n.CatalogLocale: a catalog locale or the
// translator's fallback whatever tag the request carried (X-Locale is
// only sanitized, never matched), so an attacker-chosen header cannot
// grow the caches past the catalog's locale set. An app without a
// translator has the one key "" and renders under context.Background,
// the bytes it served before loading content was localized.

// localeRender is one cache key and the context its output renders
// under: the key's locale and the translator, nothing from the request.
type localeRender struct {
	key string
	ctx context.Context
}

// renderLocale returns the cache key a request's locale maps to.
func (ds *UIHost) renderLocale(ctx context.Context) localeRender {
	tr := i18n.TranslatorFrom(ctx)
	if tr == nil {
		tr = i18n.Default()
	}
	if tr == nil {
		return localeRender{ctx: context.Background()}
	}
	return ds.localeRenderFor(tr, i18n.CatalogLocale(ctx))
}

// localeRenderFor builds the render context for key under tr. An app
// that routes by locale prefix also gets the path locale the router
// would set, so loading content that builds a link can read its prefix.
func (ds *UIHost) localeRenderFor(tr *i18n.Translator, key string) localeRender {
	ctx := i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: key}), tr)
	if ds.pathLocales != nil && slices.Contains(ds.pathLocales.All(), key) {
		ctx = i18n.WithPathLocale(ctx, *ds.pathLocales, key)
	}
	return localeRender{key: key, ctx: ctx}
}

// renderLocales lists every key a request can map to: one per catalog
// locale plus the fallback, or the one key "" without a translator.
// The PWA precache lists each locale's manifest, and a manifest request
// whose ?v= names no body built yet looks through them.
func (ds *UIHost) renderLocales(ctx context.Context) []localeRender {
	tr := i18n.TranslatorFrom(ctx)
	if tr == nil {
		tr = i18n.Default()
	}
	if tr == nil {
		return []localeRender{{ctx: context.Background()}}
	}
	keys := tr.Locales()
	if !slices.Contains(keys, tr.Fallback()) {
		keys = append(keys, tr.Fallback())
	}
	slices.Sort(keys)
	out := make([]localeRender, len(keys))
	for i, k := range keys {
		out[i] = ds.localeRenderFor(tr, k)
	}
	return out
}

// perLocale memoizes one value per locale key. The build runs outside
// the lock, once per key.
type perLocale[T any] struct {
	mu sync.Mutex
	m  map[string]*perLocaleEntry[T]
}

type perLocaleEntry[T any] struct {
	once sync.Once
	v    T
}

func (p *perLocale[T]) get(key string, build func() T) T {
	p.mu.Lock()
	if p.m == nil {
		p.m = map[string]*perLocaleEntry[T]{}
	}
	e := p.m[key]
	if e == nil {
		e = &perLocaleEntry[T]{}
		p.m[key] = e
	}
	p.mu.Unlock()
	e.once.Do(func() { e.v = build() })
	return e.v
}
