package service

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const gatePort = 47811

var gateHost = "127.0.0.1:47811"

func newTestGate(t *testing.T, allowGET bool) *gate {
	t.Helper()
	g, err := newGate("hello service open", "0123456789abcdef0123456789abcdef-secret", time.Hour, allowGET)
	if err != nil {
		t.Fatal(err)
	}
	g.pin(gatePort, []string{"proxy.internal:8443"})
	return g
}

var okApp = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("app"))
})

func serveGate(g *gate, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	g.wrap(okApp).ServeHTTP(rec, r)
	return rec
}

func request(method, host, path string, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, "http://"+host+path, nil)
	}
	r.Host = host
	return r
}

// signIn redeems a fresh token and returns the session cookie.
func signIn(t *testing.T, g *gate) *http.Cookie {
	t.Helper()
	tok, err := g.mintToken()
	if err != nil {
		t.Fatal(err)
	}
	rec := serveGate(g, request(http.MethodPost, gateHost, loginPath, "t="+url.QueryEscape(tok)))
	if rec.Code != http.StatusOK {
		t.Fatalf("login answered %d: %s", rec.Code, rec.Body)
	}
	cs := rec.Result().Cookies()
	if len(cs) != 1 {
		t.Fatalf("login set %d cookies; want 1", len(cs))
	}
	return cs[0]
}

func TestGateRefusesEverythingBeforePin(t *testing.T) {
	g, err := newGate("hello service open", "0123456789abcdef0123456789abcdef-secret", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{gateHost, "localhost:47811", ""} {
		if rec := serveGate(g, request(http.MethodGet, host, "/", "")); rec.Code != http.StatusForbidden {
			t.Errorf("Host %q before pin answered %d; want 403", host, rec.Code)
		}
	}
}

// DNS rebinding: a page on attacker.example resolves its name to
// 127.0.0.1, so the request arrives on the loopback port with the
// attacker's Host. Pinning Host is the guard; a valid session does not
// override it.
func TestGateRefusesRebindingHost(t *testing.T) {
	g := newTestGate(t, false)
	c := signIn(t, g)
	for _, host := range []string{"attacker.example:47811", "127.0.0.1:47812", "127.0.0.1", "0.0.0.0:47811", "[::1]:47811"} {
		r := request(http.MethodGet, host, "/", "")
		r.AddCookie(c)
		if rec := serveGate(g, r); rec.Code != http.StatusForbidden {
			t.Errorf("Host %q answered %d; want 403", host, rec.Code)
		}
	}
}

func TestGateAcceptsLoopbackAndAllowHosts(t *testing.T) {
	g := newTestGate(t, false)
	c := signIn(t, g)
	for _, host := range []string{gateHost, "localhost:47811", "LOCALHOST:47811", "proxy.internal:8443"} {
		r := request(http.MethodGet, host, "/", "")
		r.AddCookie(c)
		if rec := serveGate(g, r); rec.Code != http.StatusOK {
			t.Errorf("Host %q answered %d; want 200", host, rec.Code)
		}
	}
}

func TestGateNoSessionIs401NoStore(t *testing.T) {
	g := newTestGate(t, false)
	rec := serveGate(g, request(http.MethodGet, gateHost, "/__gofastr/sse", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session answered %d; want 401", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("401 Cache-Control = %q; want no-store", rec.Header().Get("Cache-Control"))
	}
	if !strings.Contains(rec.Body.String(), "hello service open") {
		t.Errorf("401 body %q does not say how to sign in", rec.Body)
	}
}

func TestGateRefusesCrossSiteUnsafeMethod(t *testing.T) {
	g := newTestGate(t, false)
	c := signIn(t, g)
	r := request(http.MethodPost, gateHost, "/api/things", "x=1")
	r.AddCookie(c)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if rec := serveGate(g, r); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST with a session answered %d; want 403", rec.Code)
	}
	r = request(http.MethodPost, gateHost, "/api/things", "x=1")
	r.AddCookie(c)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if rec := serveGate(g, r); rec.Code != http.StatusOK {
		t.Fatalf("same-origin POST with a session answered %d; want 200", rec.Code)
	}
}

func TestGateSessionCookieAttributes(t *testing.T) {
	g := newTestGate(t, false)
	c := signIn(t, g)
	if c.Name != "gofastr_service_47811" {
		t.Errorf("cookie name %q; want the port-scoped name", c.Name)
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Errorf("cookie HttpOnly=%v SameSite=%v Path=%q; want true, Strict, /", c.HttpOnly, c.SameSite, c.Path)
	}
	if c.Expires.IsZero() {
		t.Error("cookie has no Expires")
	}
}

func TestGateTokenIsSingleUse(t *testing.T) {
	g := newTestGate(t, false)
	tok, err := g.mintToken()
	if err != nil {
		t.Fatal(err)
	}
	body := "t=" + url.QueryEscape(tok)
	if rec := serveGate(g, request(http.MethodPost, gateHost, loginPath, body)); rec.Code != http.StatusOK {
		t.Fatalf("first redeem answered %d; want 200", rec.Code)
	}
	if rec := serveGate(g, request(http.MethodPost, gateHost, loginPath, body)); rec.Code != http.StatusForbidden {
		t.Fatalf("second redeem answered %d; want 403", rec.Code)
	}
}

func TestGateWrongTokenBurnsNothing(t *testing.T) {
	g := newTestGate(t, false)
	tok, err := g.mintToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "nope", tok + "x", strings.ToUpper(tok)} {
		if rec := serveGate(g, request(http.MethodPost, gateHost, loginPath, "t="+url.QueryEscape(bad)+"&z=1")); rec.Code != http.StatusForbidden {
			t.Errorf("token %q answered %d; want 403", bad, rec.Code)
		}
	}
	if !g.redeem(tok) {
		t.Fatal("a wrong guess burned the live token")
	}
}

func TestGateTokenExpires(t *testing.T) {
	g := newTestGate(t, false)
	now := time.Now()
	g.now = func() time.Time { return now }
	tok, err := g.mintToken()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(loginTokenTTL)
	if g.redeem(tok) {
		t.Fatal("a token redeemed at its expiry instant")
	}
}

func TestGateCapsPendingTokens(t *testing.T) {
	g := newTestGate(t, false)
	now := time.Now()
	g.now = func() time.Time { return now }
	for i := range maxPendingTokens {
		if _, err := g.mintToken(); err != nil {
			t.Fatalf("token %d: %v", i, err)
		}
	}
	if _, err := g.mintToken(); err == nil {
		t.Fatal("minted past the pending cap")
	}
	now = now.Add(loginTokenTTL)
	if _, err := g.mintToken(); err != nil {
		t.Fatalf("expired tokens were not pruned: %v", err)
	}
}

func TestGateLoginGETNeedsHatch(t *testing.T) {
	g := newTestGate(t, false)
	tok, err := g.mintToken()
	if err != nil {
		t.Fatal(err)
	}
	if rec := serveGate(g, request(http.MethodGet, gateHost, loginPath+"?t="+url.QueryEscape(tok), "")); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET login without InsecureTokenInURL answered %d; want 405", rec.Code)
	}
	if !g.redeem(tok) {
		t.Fatal("the refused GET burned the token")
	}

	g = newTestGate(t, true)
	tok, err = g.mintToken()
	if err != nil {
		t.Fatal(err)
	}
	if rec := serveGate(g, request(http.MethodGet, gateHost, loginPath+"?t="+url.QueryEscape(tok), "")); rec.Code != http.StatusOK {
		t.Fatalf("GET login with InsecureTokenInURL answered %d; want 200", rec.Code)
	}
}

func TestGateLoginBodyIsBounded(t *testing.T) {
	g := newTestGate(t, false)
	body := "t=" + strings.Repeat("a", maxLoginBody+1)
	if rec := serveGate(g, request(http.MethodPost, gateHost, loginPath, body)); rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized login body answered %d; want 400", rec.Code)
	}
}

func TestGateLoginStillChecksHost(t *testing.T) {
	g := newTestGate(t, false)
	tok, err := g.mintToken()
	if err != nil {
		t.Fatal(err)
	}
	if rec := serveGate(g, request(http.MethodPost, "attacker.example:47811", loginPath, "t="+url.QueryEscape(tok))); rec.Code != http.StatusForbidden {
		t.Fatalf("login on a foreign Host answered %d; want 403", rec.Code)
	}
}

func TestGateRefusesForgedSessions(t *testing.T) {
	g := newTestGate(t, false)
	c := signIn(t, g)
	other, err := newGate("hello service open", "another-secret-another-secret-another", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	other.pin(gatePort, nil)
	foreign, _, err := other.mintSession()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(c.Value, ".")
	longer := parts[0] + ".9999999999." + parts[2] + "." + parts[3]
	cases := map[string]string{
		"other secret":     foreign,
		"extended expiry":  longer,
		"no signature":     strings.Join(parts[:3], "."),
		"v2 prefix":        "v2" + c.Value[2:],
		"flipped sig byte": c.Value[:len(c.Value)-1] + string(rune(c.Value[len(c.Value)-1]^1)),
		"empty":            "",
	}
	for name, v := range cases {
		r := request(http.MethodGet, gateHost, "/", "")
		r.AddCookie(&http.Cookie{Name: c.Name, Value: v})
		if rec := serveGate(g, r); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: answered %d; want 401", name, rec.Code)
		}
	}
}

func TestGateSessionExpires(t *testing.T) {
	g := newTestGate(t, false)
	now := time.Now()
	g.now = func() time.Time { return now }
	c := signIn(t, g)
	now = now.Add(time.Hour + time.Second)
	r := request(http.MethodGet, gateHost, "/", "")
	r.AddCookie(c)
	if rec := serveGate(g, r); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired session answered %d; want 401", rec.Code)
	}
}

// The cookie name carries the port, and a session from the dev run's
// port is not a session here.
func TestGateCookieIsPortScoped(t *testing.T) {
	g := newTestGate(t, false)
	c := signIn(t, g)
	r := request(http.MethodGet, gateHost, "/", "")
	r.AddCookie(&http.Cookie{Name: "gofastr_service_47812", Value: c.Value})
	if rec := serveGate(g, r); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a session under another port's cookie name answered %d; want 401", rec.Code)
	}
}

func TestGateStaleDuplicateCookieDoesNotLogOut(t *testing.T) {
	g := newTestGate(t, false)
	c := signIn(t, g)
	r := request(http.MethodGet, gateHost, "/", "")
	r.AddCookie(&http.Cookie{Name: c.Name, Value: "v1.1.stale.sig"})
	r.AddCookie(c)
	if rec := serveGate(g, r); rec.Code != http.StatusOK {
		t.Fatalf("live session behind a stale duplicate answered %d; want 200", rec.Code)
	}
}
