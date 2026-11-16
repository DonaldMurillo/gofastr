package uihost

import (
	"context"
	"slices"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

type localeRender struct {
	key string
	ctx context.Context
}

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

func (ds *UIHost) localeRenderFor(tr *i18n.Translator, key string) localeRender {
	ctx := i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: key}), tr)
	if ds.pathLocales != nil && slices.Contains(ds.pathLocales.All(), key) {
		ctx = i18n.WithPathLocale(ctx, *ds.pathLocales, key)
	}
	return localeRender{key: key, ctx: ctx}
}

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

type perLocale[T any] struct {
	mu sync.Mutex
	m  map[string]*perLocaleEntry[T]
}

type perLocaleEntry[T any] struct {
	once   sync.Once
	v      T
	failed bool
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
