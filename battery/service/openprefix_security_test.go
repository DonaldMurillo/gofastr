package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/internal/localuser"
)

// newOpenGate is newTestGate with Config.OpenPrefixes = /api and a
// local user, so a test can tell a signed-in request from an anonymous
// one by what reaches the app.
func newOpenGate(t *testing.T) *gate {
	t.Helper()
	g := newTestGate(t, false)
	g.open = []string{"/api"}
	g.user = localuser.ForID("local-user")
	return g
}

// whoApp answers with the signed-in user's id, or "anonymous".
var whoApp = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if u, ok := handler.GetUser(r.Context()); ok {
		if lu, ok := u.(*localuser.User); ok {
			_, _ = w.Write([]byte(lu.GetID()))
			return
		}
	}
	_, _ = w.Write([]byte("anonymous"))
})

func serveWho(g *gate, r *http.Request) (int, string) {
	rec := httptest.NewRecorder()
	g.wrap(whoApp).ServeHTTP(rec, r)
	return rec.Code, rec.Body.String()
}

// A client with no browser reaches an open path with no session, and
// reaches it anonymous: the app's own auth decides, not the gate.
func TestOpenPrefixPassesWithoutSessionAsAnonymous(t *testing.T) {
	g := newOpenGate(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		code, body := serveWho(g, request(method, gateHost, "/api/things", ""))
		if code != http.StatusOK || body != "anonymous" {
			t.Errorf("%s /api/things with no session = %d %q; want 200 anonymous", method, code, body)
		}
	}
}

// A path off the prefix stays gated, including every spelling that a
// router could resolve to a gated route.
func TestOpenPrefixLeavesEverythingElseGated(t *testing.T) {
	g := newOpenGate(t)
	for _, p := range []string{"/", "/admin", "/apix", "/api/../admin", "/api//x", "/api%2F..%2Fadmin", "/__gofastr/sse"} {
		code, _ := serveWho(g, request(http.MethodGet, gateHost, p, ""))
		if code != http.StatusUnauthorized {
			t.Errorf("GET %s with no session = %d; want 401", p, code)
		}
	}
}

// Host pinning stays: an open path is no way around DNS rebinding.
func TestOpenPrefixKeepsHostPin(t *testing.T) {
	g := newOpenGate(t)
	code, _ := serveWho(g, request(http.MethodGet, "attacker.example:47811", "/api/things", ""))
	if code != http.StatusForbidden {
		t.Fatalf("open path with a rebinding Host = %d; want 403", code)
	}
	code, _ = serveWho(g, request(http.MethodGet, "proxy.internal:8443", "/api/things", ""))
	if code != http.StatusOK {
		t.Fatalf("open path with an AllowHosts Host = %d; want 200", code)
	}
}

// The cross-site refusal for writes stays: a page on another site
// cannot POST to an open path, with or without a session.
func TestOpenPrefixKeepsCrossSiteRefusal(t *testing.T) {
	g := newOpenGate(t)
	r := request(http.MethodPost, gateHost, "/api/things", "x=1")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if code, _ := serveWho(g, r); code != http.StatusForbidden {
		t.Fatalf("cross-site POST to an open path = %d; want 403", code)
	}
}

// A browser client on another origin sends JSON, which the browser
// preflights; CORS and the app's own auth decide it, so an open path lets
// it through. The same call to a gated path is still refused.
func TestOpenPrefixLetsPreflightedCrossOriginWritesThrough(t *testing.T) {
	g := newOpenGate(t)
	r := request(http.MethodPost, gateHost, "/api/things", "")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if code, _ := serveWho(g, r); code != http.StatusOK {
		t.Fatalf("cross-site JSON POST to an open path = %d; want 200", code)
	}
	// The browser sends the preflight first: OPTIONS, no body, no
	// content type. It must reach the app's CORS, or the POST never goes.
	pre := request(http.MethodOptions, gateHost, "/api/things", "")
	pre.Header.Set("Sec-Fetch-Site", "cross-site")
	pre.Header.Set("Origin", "https://spa.example")
	pre.Header.Set("Access-Control-Request-Method", "POST")
	if code, _ := serveWho(g, pre); code != http.StatusOK {
		t.Fatalf("cross-site CORS preflight to an open path = %d; want it to reach the app", code)
	}
	c := signIn(t, g)
	r = request(http.MethodPost, gateHost, "/admin", "")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.AddCookie(c)
	if code, _ := serveWho(g, r); code != http.StatusForbidden {
		t.Fatalf("cross-site JSON POST to a gated path = %d; want 403", code)
	}
}

// An open path's answer to a caller with no session is not cacheable,
// like every other answer the gate lets through.
func TestOpenPrefixAnswerIsNoStore(t *testing.T) {
	g := newOpenGate(t)
	rec := httptest.NewRecorder()
	g.wrap(whoApp).ServeHTTP(rec, request(http.MethodGet, gateHost, "/api/things", ""))
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q; want no-store", got)
	}
}

// A signed-in browser calling the open path is still the local user,
// so the app's own UI keeps working through /api.
func TestOpenPrefixSignedInStaysSignedIn(t *testing.T) {
	g := newOpenGate(t)
	c := signIn(t, g)
	r := request(http.MethodGet, gateHost, "/api/things", "")
	r.AddCookie(c)
	if code, body := serveWho(g, r); code != http.StatusOK || body != "local-user" {
		t.Fatalf("signed-in GET on an open path = %d %q; want 200 local-user", code, body)
	}
}

// localhost:<port> is redirected to 127.0.0.1 for the cookie's sake;
// an API client on an open path has no cookie and gets the answer.
func TestOpenPrefixSkipsLoopbackRedirect(t *testing.T) {
	g := newOpenGate(t)
	if code, _ := serveWho(g, request(http.MethodGet, "localhost:47811", "/api/things", "")); code != http.StatusOK {
		t.Fatalf("GET localhost:<port>/api/things = %d; want 200, not the 127.0.0.1 redirect", code)
	}
	if code, _ := serveWho(g, request(http.MethodGet, "localhost:47811", "/admin", "")); code != http.StatusTemporaryRedirect {
		t.Fatalf("GET localhost:<port>/admin = %d; want the 307 to 127.0.0.1", code)
	}
}

func TestBindAddressHosts(t *testing.T) {
	b := &Battery{cfg: Config{BindAddress: "192.168.1.20", AllowHosts: []string{"host.docker.internal:47811"}}}
	got := b.extraHosts(47811)
	want := map[string]bool{"host.docker.internal:47811": true, "192.168.1.20:47811": true}
	if len(got) != len(want) {
		t.Fatalf("extraHosts = %v; want %v", got, want)
	}
	for _, h := range got {
		if !want[h] {
			t.Fatalf("extraHosts = %v; want %v", got, want)
		}
	}
	v6 := &Battery{cfg: Config{BindAddress: "::1"}}
	if got := v6.extraHosts(47811); len(got) != 1 || got[0] != "[::1]:47811" {
		t.Fatalf("a ::1 bind pins %v; want [::1]:47811, the address open hands the browser", got)
	}
	if !b.boundHostOK("192.168.1.20") || b.boundHostOK("127.0.0.1") {
		t.Fatal("boundHostOK must accept exactly the configured address")
	}
	wild := &Battery{cfg: Config{BindAddress: "0.0.0.0"}}
	if len(wild.extraHosts(1)) != 0 {
		t.Fatal("a wildcard bind must add no Host value: the caller's name goes in AllowHosts")
	}
	if !wild.boundHostOK("0.0.0.0") || !wild.boundHostOK("::") {
		t.Fatal("a wildcard bind must accept the listener's wildcard address")
	}
	if (&Battery{}).boundHostOK("0.0.0.0") {
		t.Fatal("the default bind must refuse a listener that came up on the wildcard address")
	}
	if got := browserAddr("0.0.0.0:47811"); got != "127.0.0.1:47811" {
		t.Fatalf("browserAddr(0.0.0.0) = %q; want 127.0.0.1:47811", got)
	}
	if got := browserAddr("[::]:47811"); got != "[::1]:47811" {
		t.Fatalf("browserAddr(::) = %q; want [::1]:47811 (an IPv6-only host has no 127.0.0.1)", got)
	}
	v6wild := &Battery{cfg: Config{BindAddress: "::"}}
	if got := v6wild.extraHosts(47811); len(got) != 1 || got[0] != "[::1]:47811" {
		t.Fatalf("a :: bind pins %v; want [::1]:47811, where browserAddr sends the browser", got)
	}
	if got := browserAddr("192.168.1.20:47811"); got != "192.168.1.20:47811" {
		t.Fatalf("browserAddr(specific) = %q; want it unchanged", got)
	}
}

// New keeps its own copy of the validated slices: a caller editing its
// slice afterwards cannot open a prefix New never checked.
func TestNewCopiesOpenPrefixes(t *testing.T) {
	prefixes := []string{"/api"}
	hosts := []string{"proxy.internal:8443"}
	b := New(Config{ID: "dev.gofastr.hello", Port: 47811, OpenPrefixes: prefixes, AllowHosts: hosts})
	if b.err != nil {
		t.Fatal(b.err)
	}
	prefixes[0], hosts[0] = "/", "attacker.example:80"
	if b.cfg.OpenPrefixes[0] != "/api" || b.cfg.AllowHosts[0] != "proxy.internal:8443" {
		t.Fatalf("New aliased the caller's slices: %v %v", b.cfg.OpenPrefixes, b.cfg.AllowHosts)
	}
}
