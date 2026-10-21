package framework

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

var prefixHello = i18n.Define("framework_test.prefix.hello", "hello")

// prefixApp serves /page: the locale, the routed path, the From header
// and a link, as HTML.
func prefixApp(t *testing.T, opts ...AppOption) *App {
	t.Helper()
	cat := i18n.NewMapCatalog()
	cat.Set("es", prefixHello.Key(), i18n.Message{Text: "hola"})
	cat.Set("fr", prefixHello.Key(), i18n.Message{Text: "bonjour"})
	app := NewApp(append([]AppOption{WithI18n(i18n.NewTranslator(cat, "en")), WithLocalePrefixes()}, opts...)...)
	app.Router().Get("/page", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, prefixHello.Text(r.Context())+" "+r.URL.Path+" from="+r.Header.Get("X-Gofastr-From")+
			` <a href="/other">o</a>`)
		// Past Compress's minimum size, so the gzip path runs.
		io.WriteString(w, "<!--"+strings.Repeat(" ", 2048)+"-->")
	}))
	app.Router().Get("/esmeralda", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "plain route")
	}))
	return app
}

func servePrefixed(app *App, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, r)
	return rec
}

func TestPrefixedPathSameScreen(t *testing.T) {
	rec := servePrefixed(prefixApp(t), httptest.NewRequest("GET", "/es/page", nil))
	if got := rec.Body.String(); !strings.HasPrefix(got, "hola /page ") {
		t.Fatalf("got %q", got)
	}
}

func TestPageLinksGainPrefix(t *testing.T) {
	rec := servePrefixed(prefixApp(t), httptest.NewRequest("GET", "/fr/page", nil))
	if !strings.Contains(rec.Body.String(), `href="/fr/other"`) {
		t.Fatalf("got %q", rec.Body.String())
	}
}

func TestCompressedPageRewritten(t *testing.T) {
	app := prefixApp(t, func(a *App) { a.Config.Compression = true })
	r := httptest.NewRequest("GET", "/es/page", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := servePrefixed(app, r)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Compress did not encode the page: %q", rec.Header().Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(zr)
	if !strings.Contains(string(b), `href="/es/other"`) {
		t.Fatalf("got %q", b)
	}
}

func TestUnprefixedPinsDefault(t *testing.T) {
	r := httptest.NewRequest("GET", "/page", nil)
	r.Header.Set("Accept-Language", "es")
	r.AddCookie(&http.Cookie{Name: "locale", Value: "fr"})
	rec := servePrefixed(prefixApp(t), r)
	if got := rec.Body.String(); !strings.HasPrefix(got, "hello /page ") || !strings.Contains(got, `href="/other"`) {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultPrefixRedirects308(t *testing.T) {
	rec := servePrefixed(prefixApp(t), httptest.NewRequest("GET", "/en/page?q=1&r=2", nil))
	if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != "/page?q=1&r=2" {
		t.Fatalf("got %d Location %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = servePrefixed(prefixApp(t), httptest.NewRequest("GET", "/en", nil))
	if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != "/" {
		t.Fatalf("got %d Location %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUnknownSegmentIsRoute(t *testing.T) {
	rec := servePrefixed(prefixApp(t), httptest.NewRequest("GET", "/esmeralda", nil))
	if rec.Body.String() != "plain route" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	rec = servePrefixed(prefixApp(t), httptest.NewRequest("GET", "/ES/page", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/ES/page: got %d, want a plain 404", rec.Code)
	}
}

func TestRawPathStrippedToo(t *testing.T) {
	app := prefixApp(t)
	app.Router().Get("/files/{name}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, r.PathValue("name"))
	}))
	rec := servePrefixed(app, httptest.NewRequest("GET", "/es/files/a%2Fb", nil))
	if rec.Body.String() != "a/b" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestSameLocaleFromStripped(t *testing.T) {
	r := httptest.NewRequest("GET", "/es/page", nil)
	r.Header.Set("X-Gofastr-From", "/es/list?page=2")
	if got := servePrefixed(prefixApp(t), r).Body.String(); !strings.Contains(got, "from=/list?page=2 ") {
		t.Fatalf("got %q", got)
	}
}

func TestCrossLocaleFromDropped(t *testing.T) {
	for _, c := range []struct{ path, from string }{
		{"/es/page", "/list"},
		{"/es/page", "/fr/list"},
		{"/page", "/es/list"},
	} {
		r := httptest.NewRequest("GET", c.path, nil)
		r.Header.Set("X-Gofastr-From", c.from)
		if got := servePrefixed(prefixApp(t), r).Body.String(); !strings.Contains(got, "from= ") {
			t.Errorf("%s from %s: got %q", c.path, c.from, got)
		}
	}
	r := httptest.NewRequest("GET", "/page", nil)
	r.Header.Set("X-Gofastr-From", "/list")
	if got := servePrefixed(prefixApp(t), r).Body.String(); !strings.Contains(got, "from=/list ") {
		t.Errorf("same default locale: got %q", got)
	}
}

func TestLocalePrefixesNeedI18n(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WithLocalePrefixes without WithI18n: want a panic")
		}
	}()
	NewApp(WithLocalePrefixes())
}

func TestLocalePrefixesRefuseResolver(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WithLocalePrefixes with WithLocaleResolver: want a panic")
		}
	}()
	prefixApp(t, WithLocaleResolver(i18n.CookieLocale("locale")))
}

func TestLocaleSegmentRoutePanics(t *testing.T) {
	app := prefixApp(t)
	app.checkLocaleRoutes()
	app.Router().Get("/es/promo", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer func() {
		if recover() == nil {
			t.Fatal("a route under a locale segment: want a boot panic")
		}
	}()
	app.checkLocaleRoutes()
}

// Start runs the check after every battery and plugin registered its
// routes, before the listener binds.
func TestStartRefusesLocaleRoute(t *testing.T) {
	app := prefixApp(t)
	app.Router().Get("/es/promo", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })
	refused := make(chan any, 1)
	go func() {
		defer func() { refused <- recover() }()
		_ = app.Start("127.0.0.1:0")
	}()
	select {
	case p := <-refused:
		if p == nil {
			t.Fatal("Start returned without refusing /es/promo")
		}
	case <-ready:
		_ = app.Shutdown(context.Background())
		t.Fatal("Start served a route under a locale segment")
	case <-time.After(10 * time.Second):
		t.Fatal("Start neither refused nor became ready")
	}
}

func TestServeLocaleReadsPrefix(t *testing.T) {
	app := prefixApp(t)
	var tag string
	h := app.serveLocale(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		tag = i18n.FromContext(r.Context()).Tag
	}))
	r := httptest.NewRequest("GET", "/fr/page", nil)
	r.Header.Set("Accept-Language", "es")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if tag != "fr" {
		t.Fatalf("server wrappers saw %q, want the path's fr", tag)
	}
}
