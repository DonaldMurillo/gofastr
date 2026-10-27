package uihost

import (
	"context"
	"maps"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

var cacheLoadingWord = i18n.Define("uihost_test.loading", "Waiting")

// noDefaultTranslator clears the process-wide translator for one test,
// so the host sees only the translator the request carries.
func noDefaultTranslator(t *testing.T) {
	prev := i18n.Default()
	i18n.SetDefault(nil)
	t.Cleanup(func() { i18n.SetDefault(prev) })
}

func loadingHost(show func(ctx context.Context) render.HTML) *UIHost {
	a := app.NewApp("loading-cache")
	a.RegisterScreen(app.NewScreen("/r", &staticComp{html: "<p>r</p>"}).
		WithLoading(&app.Loading{Show: app.NewContextComponent(show)}), nil)
	return New(a)
}

func saysWaiting(ctx context.Context) render.HTML {
	return "<i>" + render.Text(cacheLoadingWord.Text(ctx)) + "</i>"
}

func spanishTranslator() *i18n.Translator {
	cat := i18n.NewMapCatalog()
	cat.Set("es", cacheLoadingWord.Key(), i18n.Message{Text: "Esperando"})
	return i18n.NewTranslator(cat, "en")
}

func readerCtx(tr *i18n.Translator, tag string) context.Context {
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: tag})
	if tr != nil {
		ctx = i18n.WithTranslator(ctx, tr)
	}
	return ctx
}

// X-Locale reaches ctx sanitized but unmatched, so a request can carry
// any tag; the caches key on the catalog locale it reads in and never
// hold more entries than the catalog has locales.
func TestLocaleCachesBoundedByCatalog(t *testing.T) {
	noDefaultTranslator(t)
	tr := spanishTranslator()
	ds := loadingHost(saysWaiting)
	_, esHash := ds.manifestJS(readerCtx(tr, "es"))
	for _, tag := range []string{"es-mx", "zz-a", "zz-b", "x-attacker-1", "", "en"} {
		_, h := ds.manifestJS(readerCtx(tr, tag))
		if tag == "es-mx" && h != esHash {
			t.Errorf("es-mx reads the es catalog but got its own manifest")
		}
		ds.buildRouteScript(readerCtx(tr, tag))
	}
	for name, keys := range map[string][]string{
		"manifests":    slices.Sorted(maps.Keys(ds.manifests.m)),
		"routeScripts": slices.Sorted(maps.Keys(ds.routeScripts.m)),
	} {
		if strings.Join(keys, ",") != "en,es" {
			t.Errorf("%s keyed on %q, want en,es", name, keys)
		}
	}
	if es := ds.buildRouteScript(readerCtx(tr, "es")); !strings.Contains(es, "Esperando") {
		t.Errorf("the es route script is not Spanish:\n%s", es)
	}
}

// Without a translator there is one manifest, its loading content
// rendered under a bare context exactly as before localization.
func TestNoTranslatorOneManifest(t *testing.T) {
	noDefaultTranslator(t)
	ds := loadingHost(func(ctx context.Context) render.HTML {
		if i18n.TranslatorFrom(ctx) != nil || i18n.FromContext(ctx).Tag != "" {
			return "LEAKED"
		}
		return "PLAIN"
	})
	_, first := ds.manifestJS(readerCtx(nil, "es"))
	for _, tag := range []string{"fr", "zz", ""} {
		if _, h := ds.manifestJS(readerCtx(nil, tag)); h != first {
			t.Errorf("tag %q got a second manifest", tag)
		}
	}
	if keys := slices.Collect(maps.Keys(ds.manifests.m)); len(keys) != 1 || keys[0] != "" {
		t.Errorf("manifest keys = %q, want the one key \"\"", keys)
	}
	if body, _ := ds.manifestJS(context.Background()); !strings.Contains(body, "PLAIN") {
		t.Errorf("loading content rendered under a request context:\n%s", body)
	}
}

// The script request carries no locale prefix, so the hash decides:
// a ?v= naming the es body serves it even on a replica that has only
// rendered English pages, and a ?v= naming nothing serves the request's
// locale uncached.
func TestManifestServesBodyByHash(t *testing.T) {
	noDefaultTranslator(t)
	tr := spanishTranslator()
	_, esHash := loadingHost(saysWaiting).manifestJS(readerCtx(tr, "es"))
	ds := loadingHost(saysWaiting) // another replica
	get := func(v string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/__gofastr/manifest.js?v="+v, nil)
		w := httptest.NewRecorder()
		ds.handleManifestJS(w, r.WithContext(readerCtx(tr, "en")))
		return w
	}
	w := get(esHash)
	if !strings.Contains(w.Body.String(), "Esperando") || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("?v=<es hash> served %q:\n%s", w.Header().Get("Cache-Control"), w.Body.String())
	}
	w = get("0123456789ab")
	if !strings.Contains(w.Body.String(), "Waiting") || w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("an unknown ?v= served %q:\n%s", w.Header().Get("Cache-Control"), w.Body.String())
	}
	if n := len(ds.manifests.m); n != 2 {
		t.Errorf("%d manifests built, want one per catalog locale (2)", n)
	}
}

// The service worker precaches each catalog locale's manifest under the
// translator the request carries. Built under the process default, it
// listed no translated manifest, or another app's.
func TestWorkerPrecachesRequestLocales(t *testing.T) {
	noDefaultTranslator(t)
	tr := spanishTranslator()
	a := app.NewApp("pwa-locales")
	a.RegisterScreen(app.NewScreen("/r", &staticComp{html: "<p>r</p>"}).
		WithLoading(&app.Loading{Show: app.NewContextComponent(saysWaiting)}), nil)
	ds := New(a, WithPWA(PWAConfig{}))
	_, esHash := ds.manifestJS(readerCtx(tr, "es"))
	w := httptest.NewRecorder()
	ds.handlePWAServiceWorker(w, httptest.NewRequest("GET", "/sw.js", nil).WithContext(readerCtx(tr, "en")))
	if !strings.Contains(w.Body.String(), "/__gofastr/manifest.js?v="+esHash) {
		t.Errorf("worker precache lacks the es manifest %s:\n%s", esHash, w.Body.String())
	}
}

// staticComp renders fixed markup.
type staticComp struct{ html render.HTML }

func (s *staticComp) Render() render.HTML { return s.html }

var _ component.Component = (*staticComp)(nil)
