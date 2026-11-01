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
	if got := browserAddr("192.168.1.20:47811"); got != "192.168.1.20:47811" {
		t.Fatalf("browserAddr(specific) = %q; want it unchanged", got)
	}
}
