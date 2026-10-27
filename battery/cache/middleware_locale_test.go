package cache_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/cache"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework"
)

var cacheHello = i18n.Define("cachetest.hello", "hello")

// localeCacheApp serves one localized page behind CacheMiddleware, the
// app.Use placement the cache docs give.
func localeCacheApp(t *testing.T, opts ...framework.AppOption) *framework.App {
	t.Helper()
	cat := i18n.NewMapCatalog()
	cat.Set("es", cacheHello.Key(), i18n.Message{Text: "hola"})
	app := framework.NewApp(append([]framework.AppOption{framework.WithI18n(i18n.NewTranslator(cat, "en"))}, opts...)...)
	c := cache.NewMemoryCache()
	t.Cleanup(func() { c.Close() })
	app.Use(cache.CacheMiddleware(c, time.Minute))
	app.Router().Get("/pricing", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, cacheHello.Text(r.Context()))
	}))
	return app
}

func serveBody(app *framework.App, req *http.Request) string {
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	return rec.Body.String()
}

// The router strips /es before middleware runs, so /es/pricing and
// /pricing reach the cache with the same URL; the locale keeps them apart.
func TestCacheKeysPathLocale(t *testing.T) {
	app := localeCacheApp(t, framework.WithLocalePrefixes())
	if got := serveBody(app, httptest.NewRequest(http.MethodGet, "/es/pricing", nil)); got != "hola" {
		t.Fatalf("/es/pricing = %q, want hola", got)
	}
	if got := serveBody(app, httptest.NewRequest(http.MethodGet, "/pricing", nil)); got != "hello" {
		t.Fatalf("/pricing = %q after /es/pricing, want hello", got)
	}
}

// A page negotiated from Accept-Language is stored per language.
func TestCacheKeysAcceptLanguage(t *testing.T) {
	app := localeCacheApp(t)
	get := func(lang string) string {
		req := httptest.NewRequest(http.MethodGet, "/pricing", nil)
		req.Header.Set("Accept-Language", lang)
		return serveBody(app, req)
	}
	if got := get("es"); got != "hola" {
		t.Fatalf("es reader = %q, want hola", got)
	}
	if got := get("en"); got != "hello" {
		t.Fatalf("en reader = %q after an es reader, want hello", got)
	}
}
