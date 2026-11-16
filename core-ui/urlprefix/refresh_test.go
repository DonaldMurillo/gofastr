package urlprefix

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// refreshCases are Refresh values in each form a browser reads (the
// HTML standard's shared declarative refresh steps), same-origin first.
var refreshCases = []struct{ in, want string }{
	{"0;url=/orders/7", "0;url=/es/orders/7"},
	{"0; url=/orders/7", "0; url=/es/orders/7"},
	{"0; URL=/orders/7", "0; URL=/es/orders/7"},
	{"0;Url = /orders/7", "0;Url = /es/orders/7"},
	{"0,url=/orders/7", "0,url=/es/orders/7"},
	{"0 url=/orders/7", "0 url=/es/orders/7"},
	{"3.5;url=/orders/7", "3.5;url=/es/orders/7"},
	{" 0;url=/orders/7", " 0;url=/es/orders/7"},
	{"0;/orders/7", "0;/es/orders/7"},
	{"0; /orders/7", "0; /es/orders/7"},
	{"0 /orders/7", "0 /es/orders/7"},
	{"0;url='/orders/7'", "0;url='/es/orders/7'"},
	{`0; url="/orders/7"`, `0; url="/es/orders/7"`},
	{"0;url='/a b' ignored", "0;url='/es/a b' ignored"},
	{"0;url=/?next=%2Fx", "0;url=/es?next=%2Fx"},
	{"0;url=/", "0;url=/es"},
	{"0;url=/a?x=1&y=2#f", "0;url=/es/a?x=1&y=2#f"},
	{"0;url=http://example.com/orders", "0;url=http://example.com/es/orders"},
	// Already under the prefix, or under another locale.
	{"0;url=/es/orders/7", "0;url=/es/orders/7"},
	{"0; url='/es/orders/7'", "0; url='/es/orders/7'"},
	{"0;url=/%65s/orders", "0;url=/%65s/orders"},
	{"0;url=/en/orders", "0;url=/en/orders"},
	{"0;url=http://example.com/es/orders", "0;url=http://example.com/es/orders"},
	// Not this app, or no root-relative URL at all.
	{"0;url=https://idp.example/login", "0;url=https://idp.example/login"},
	{"0;url=//evil.example/x", "0;url=//evil.example/x"},
	{"0;url=orders", "0;url=orders"},
	{"0;url=?page=2", "0;url=?page=2"},
	{"0;url=/__gofastr/sse", "0;url=/__gofastr/sse"},
	{"0;url=", "0;url="},
	{"0;url", "0;url"},
	{"5", "5"},
	{"5;", "5;"},
	{"", ""},
	// "url" with no "=" is itself the URL, a relative one.
	{"0;url /orders", "0;url /orders"},
	// No time, so a browser ignores the whole value.
	{"url=/orders", "url=/orders"},
	{"/orders", "/orders"},
	{"0x;url=/orders", "0x;url=/orders"},
}

func TestRefreshRule(t *testing.T) {
	r := es
	r.Origins = []string{"http://example.com"}
	for _, c := range refreshCases {
		if got := Refresh(c.in, r); got != c.want {
			t.Errorf("Refresh(%q) = %q, want %q", c.in, got, c.want)
		}
		if got := Refresh(c.in, Rule{}); got != c.in {
			t.Errorf("empty rule changed %q to %q", c.in, got)
		}
	}
}

// Rewriting twice must change nothing more: a host that already put the
// prefix on, or a second pass, gets no /es/es.
func TestRefreshRuleIdempotent(t *testing.T) {
	for _, c := range refreshCases {
		once := Refresh(c.in, es)
		if twice := Refresh(once, es); twice != once {
			t.Errorf("Refresh(Refresh(%q)) = %q, want %q", c.in, twice, once)
		}
	}
}

// A Refresh header is a navigation like Location: whoever writes one,
// the reader stays in their locale.
func TestRefreshHeaderGainsPrefix(t *testing.T) {
	for _, c := range refreshCases {
		h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Refresh", c.in)
			w.WriteHeader(http.StatusOK)
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, esRequest("GET", "/x"))
		if got := rec.Header().Get("Refresh"); got != c.want {
			t.Errorf("Refresh %q -> %q, want %q", c.in, got, c.want)
		}
	}
}

func TestContentLocationGainsPrefix(t *testing.T) {
	for _, c := range [][2]string{
		{"/orders/7", "/es/orders/7"},
		{"/es/orders/7", "/es/orders/7"},
		{"http://example.com/orders/7", "http://example.com/es/orders/7"},
		{"https://cdn.example/orders/7", "https://cdn.example/orders/7"},
		{"//cdn.example/orders/7", "//cdn.example/orders/7"},
	} {
		h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Location", c[0])
			w.WriteHeader(http.StatusCreated)
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, esRequest("POST", "/x"))
		if got := rec.Header().Get("Content-Location"); got != c[1] {
			t.Errorf("Content-Location %q -> %q, want %q", c[0], got, c[1])
		}
	}
}

// A Link header names what a <link> element does, and the page rewrite
// leaves those alone: assets are served unprefixed, and a canonical or
// alternate URL is written with its locale by whoever builds it.
func TestLinkHeaderLeftAlone(t *testing.T) {
	const in = `</app.css>; rel=preload; as=style, </orders?page=2>; rel="next"`
	h := Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", in)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, esRequest("GET", "/x"))
	if got := rec.Header().Get("Link"); got != in {
		t.Fatalf("Link = %q, want it as written", got)
	}
}

// A meta refresh is the Refresh header written in the page.
func TestMetaRefreshGainsPrefix(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`<meta http-equiv="refresh" content="0;url=/orders?a=1&amp;b=2">`, `content="0;url=/es/orders?a=1&amp;b=2"`},
		{`<META HTTP-EQUIV="Refresh" CONTENT="0; URL='/orders'">`, `content="0; URL=&#39;/es/orders&#39;"`},
		{`<meta content="2;/orders" http-equiv=refresh>`, `content="2;/es/orders"`},
	} {
		if got := string(HTML([]byte(c.in), es)); !strings.Contains(got, c.want) {
			t.Errorf("HTML(%s) = %s, want %s", c.in, got, c.want)
		}
	}
	for _, in := range []string{
		`<meta http-equiv="refresh" content="0;url=/es/orders">`,
		`<meta http-equiv="refresh" content="0; url='/es/orders'">`,
		`<meta http-equiv="refresh" content="0;url=https://idp.example/x">`,
		`<meta http-equiv="refresh" content="0;url=//evil.example/x">`,
		`<meta http-equiv="refresh" content="30">`,
		`<meta name="description" content="0;url=/orders">`,
		`<meta http-equiv="content-security-policy" content="default-src /orders">`,
		`<meta property="og:url" content="/orders">`,
	} {
		if got := string(HTML([]byte(in), es)); got != in {
			t.Errorf("HTML(%s) = %s, want it as written", in, got)
		}
	}
}
