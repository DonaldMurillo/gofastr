package urlprefix

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

var es = Rule{Prefix: "/es", Locales: []string{"en", "es", "fr-ca"}}

func TestPathRule(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/pricing", "/es/pricing"},
		{"/", "/es"},
		{"/?q=1", "/es?q=1"},
		{"/#top", "/es#top"},
		{"/a?q=1#f", "/es/a?q=1#f"},
		{"/es/pricing", "/es/pricing"},
		{"/es", "/es"},
		{"/es?q=1", "/es?q=1"},
		{"/en/pricing", "/en/pricing"},
		{"/fr-ca", "/fr-ca"},
		{"/espresso", "/es/espresso"},
		{"/__gofastr/action", "/__gofastr/action"},
		{"/__gofastr", "/__gofastr"},
		{"//cdn.example/x", "//cdn.example/x"},
		{"https://other.example/x", "https://other.example/x"},
		{"#top", "#top"},
		{"?page=2", "?page=2"},
		{"relative/x", "relative/x"},
		{"", ""},
		{"mailto:a@b.c", "mailto:a@b.c"},
	}
	for _, c := range cases {
		if got := Path(c.in, es); got != c.want {
			t.Errorf("Path(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := Path("/x", Rule{}); got != "/x" {
		t.Errorf("empty rule changed %q", got)
	}
	// A prefix that names no locale (a mount base) still holds its own
	// URLs: the Locales list must not be what spares them.
	if got := Path("/base/x", Rule{Prefix: "/base"}); got != "/base/x" {
		t.Errorf("Path(/base/x) under /base = %q", got)
	}
}

func TestPathOnOrigin(t *testing.T) {
	r := es
	r.Origins = []string{"https://shop.example"}
	for in, want := range map[string]string{
		"https://shop.example/x":   "https://shop.example/es/x",
		"https://shop.example":     "https://shop.example/es",
		"https://shop.example?q=1": "https://shop.example/es?q=1",
		"https://shop.example#top": "https://shop.example/es#top",
		"https://shop.example/es/": "https://shop.example/es/",
		"https://shop.examplex/x":  "https://shop.examplex/x",
		"https://other.example/x":  "https://other.example/x",
	} {
		if got := Path(in, r); got != want {
			t.Errorf("Path(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPageLinksKeepPrefix(t *testing.T) {
	in := `<nav><a href="/pricing" class="x">P</a><a href='/about?x=1&amp;y=2'>A</a></nav>` +
		`<form action="/search"><button formaction="/save">S</button></form>` +
		`<div data-cui-rpc="/cart/add" data-cui-poll-src="/stats" data-cui-push-state="/list?page=2" data-cui-rpc-navigate="/done" data-cui-intercept-panel="/orders/7"></div>` +
		`<a href="/docs/start" data-cui-match-prefix="/docs">D</a><a href="/blog" data-cui-match-prefix="">B</a>`
	got := string(HTML([]byte(in), es))
	for _, want := range []string{
		`href="/es/pricing"`, `href="/es/about?x=1&amp;y=2"`, `action="/es/search"`, `formaction="/es/save"`,
		`data-cui-rpc="/es/cart/add"`, `data-cui-poll-src="/es/stats"`, `data-cui-push-state="/es/list?page=2"`,
		`data-cui-rpc-navigate="/es/done"`, `data-cui-intercept-panel="/es/orders/7"`,
		// The runtime compares the section prefix with the browser's
		// path, which carries the locale.
		`data-cui-match-prefix="/es/docs"`, `data-cui-match-prefix=""`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
}

func TestRewriteLeavesAssetsAndSwitchers(t *testing.T) {
	in := `<link rel="stylesheet" href="/__gofastr/app.css"><link rel="canonical" href="/x">` +
		`<script src="/__gofastr/runtime.js"></script><img src="/logo.png" srcset="/logo2.png 2x">` +
		`<a href="/" hreflang="en">English</a><a href="/report.pdf" download>R</a>` +
		`<a href="https://other.example/">o</a><a href="#main">skip</a>` +
		`<script type="application/json" id="gofastr-routes">[{"path":"/pricing"}]</script>` +
		`<style>a[href="/x"]{color:red}</style>`
	if got := string(HTML([]byte(in), es)); got != in {
		t.Fatalf("rewrote what it must leave:\n%s", got)
	}
}

func TestPrefixedURLNotDoubled(t *testing.T) {
	in := `<a href="/es/pricing">x</a>`
	if got := string(HTML([]byte(in), es)); got != in {
		t.Fatalf("got %s", got)
	}
}

func TestTemplateFillsKeepPrefix(t *testing.T) {
	in := `<template data-cui-fill="main"><a href="/next">n</a></template>`
	got := string(HTML([]byte(in), es))
	if !strings.Contains(got, `href="/es/next"`) {
		t.Fatalf("template content not rewritten: %s", got)
	}
}

// esRequest is a request whose URL named the es locale.
func esRequest(method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	set := i18n.PathLocales{Default: "en", Prefixed: []string{"es"}}
	return r.WithContext(i18n.WithPathLocale(context.Background(), set, "es"))
}

func TestMiddlewareRewritesHTML(t *testing.T) {
	h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", "26")
		io.WriteString(w, `<a href="/pricing">p</a>`)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, esRequest("GET", "/x"))
	if got := rec.Body.String(); got != `<a href="/es/pricing">p</a>` {
		t.Fatalf("got %q", got)
	}
	if cl := rec.Header().Get("Content-Length"); cl != "" {
		t.Fatalf("stale Content-Length %q kept", cl)
	}
}

func TestMiddlewareSniffsUntypedHTML(t *testing.T) {
	h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `<!doctype html><a href="/pricing">p</a>`)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, esRequest("GET", "/x"))
	if !strings.Contains(rec.Body.String(), `/es/pricing`) {
		t.Fatalf("got %q", rec.Body.String())
	}
}

func TestMiddlewarePassesOtherTypes(t *testing.T) {
	body := `{"href":"/pricing"}`
	h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, esRequest("GET", "/x"))
	if rec.Body.String() != body {
		t.Fatalf("got %q", rec.Body.String())
	}
}

func TestMiddlewareSkipsEncodedBodies(t *testing.T) {
	h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		io.WriteString(gz, `<a href="/pricing">p</a>`)
		gz.Close()
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, esRequest("GET", "/x"))
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("body is not gzip any more: %v", err)
	}
	b, _ := io.ReadAll(zr)
	if string(b) != `<a href="/pricing">p</a>` {
		t.Fatalf("got %q", b)
	}
}

func TestLocationGainsPrefix(t *testing.T) {
	h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gofastr-Location", "/orders/7")
		http.Redirect(w, r, "/login?next=%2Fx", http.StatusSeeOther)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, esRequest("POST", "/x"))
	if got := rec.Header().Get("Location"); got != "/es/login?next=%2Fx" {
		t.Fatalf("Location = %q", got)
	}
	if got := rec.Header().Get("X-Gofastr-Location"); got != "/es/orders/7" {
		t.Fatalf("X-Gofastr-Location = %q", got)
	}
}

// The runtime applies X-Gofastr-Push-State to the URL bar as given, so
// an unprefixed one moves a Spanish page to the English URL.
func TestPushStateGainsPrefix(t *testing.T) {
	h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Gofastr-Push-State", "/orders?page=2")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, esRequest("POST", "/x"))
	if got := rec.Header().Get("X-Gofastr-Push-State"); got != "/es/orders?page=2" {
		t.Fatalf("X-Gofastr-Push-State = %q", got)
	}
}

func TestLocationWithoutBody(t *testing.T) {
	h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Gofastr-Location", "/orders/7")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, esRequest("POST", "/x"))
	if got := rec.Header().Get("X-Gofastr-Location"); got != "/es/orders/7" {
		t.Fatalf("X-Gofastr-Location = %q", got)
	}
}

// X-Gofastr-Document carries an absolute URL; one on this app keeps the
// locale a document load's rewritten Location would, another site's is
// left alone.
func TestDocumentHeaderGainsPrefix(t *testing.T) {
	for _, c := range [][2]string{
		{"http://example.com/login", "http://example.com/es/login"},
		{"https://idp.example/login", "https://idp.example/login"},
	} {
		in, want := c[0], c[1]
		h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Gofastr-Document", in)
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, esRequest("GET", "/x"))
		if got := rec.Header().Get("X-Gofastr-Document"); got != want {
			t.Errorf("X-Gofastr-Document %q -> %q, want %q", in, got, want)
		}
	}
}

func TestMiddlewareIdleWithoutPrefix(t *testing.T) {
	in := `<a href="/pricing">p</a>`
	h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, in)
	}))
	for _, r := range []*http.Request{
		httptest.NewRequest("GET", "/x", nil),
		httptest.NewRequest("GET", "/x", nil).WithContext(i18n.WithPathLocale(context.Background(),
			i18n.PathLocales{Default: "en", Prefixed: []string{"es"}}, "en")),
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Body.String() != in {
			t.Fatalf("got %q", rec.Body.String())
		}
	}
}

func TestMiddlewareFlushKeepsBuffer(t *testing.T) {
	h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<a href="/p`)
		http.NewResponseController(w).Flush()
		io.WriteString(w, `ricing">p</a>`)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, esRequest("GET", "/x"))
	if got := rec.Body.String(); got != `<a href="/es/pricing">p</a>` {
		t.Fatalf("got %q", got)
	}
}
