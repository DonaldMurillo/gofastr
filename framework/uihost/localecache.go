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
	// failed: the build panicked. sync.Once counts a panicking call as
	// done, so without this every later get returned the zero value: a
	// refused behaviour graph answered 500 once and then served an empty
	// manifest.js, every page linked ?v= with no hash, and nothing was
	// logged again.
	failed bool
}

// get returns the memoized value for key, building it once. A build that
// panics is not memoized: its entry is dropped and the panic goes on
// unrecovered, so the recovery middleware logs the stack where it
// happened, the next get builds again (a transient failure heals, a
// persistent one panics on every request with its own stack), and a
// caller that waited on the failed build retries it.
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
	e.once.Do(func() {
		ok := false
		defer func() {
			if ok {
				return
			}
			e.failed = true
			p.mu.Lock()
			if p.m[key] == e {
				delete(p.m, key)
			}
			p.mu.Unlock()
		}()
		e.v = build()
		ok = true
	})
	if e.failed {
		return p.get(key, build)
	}
	return e.v
}
