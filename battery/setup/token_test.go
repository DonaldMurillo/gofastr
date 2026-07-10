package setup

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// buildTestRunner creates a Runner with token enabled (default) for
// token-flow tests. completeVal controls the Complete predicate.
func buildTestRunner(t *testing.T, disableToken bool, completeVal *bool) *Runner {
	t.Helper()
	return New(Config{
		DisableToken: disableToken,
		Complete: func(_ context.Context) (bool, error) {
			return *completeVal, nil
		},
		Steps: []Step{
			{Name: "test", Fields: []Field{
				{Name: "FOO", Label: "Foo"},
			}, Run: func(_ context.Context, _ map[string]string) error { return nil }},
		},
	})
}

func doGet(handler http.Handler, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	handler.ServeHTTP(w, req)
	return w
}

func doPost(handler http.Handler, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	handler.ServeHTTP(w, req)
	return w
}

// TestToken_RequiredByDefault verifies that GET /setup without a cookie
// returns 403 when token is enabled.
func TestToken_RequiredByDefault(t *testing.T) {
	done := false
	r := buildTestRunner(t, false, &done)
	h := r.Handler(func() {}, nil, nil)

	w := doGet(h, "/setup")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without cookie, got %d", w.Code)
	}
}

// TestToken_ValidTokenSetsCookie verifies the token exchange flow:
// GET /setup?token=<t> sets the cookie and redirects to /setup.
func TestToken_ValidTokenSetsCookie(t *testing.T) {
	done := false
	r := buildTestRunner(t, false, &done)
	h := r.Handler(func() {}, nil, nil)

	tok := r.token // capture before exchange (token is single-use)
	w := doGet(h, "/setup?token="+tok)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	if w.Header().Get("Location") != "/setup" {
		t.Fatalf("expected redirect to /setup, got %q", w.Header().Get("Location"))
	}
	// Cookie must be set, with value matching cookieSecret (which
	// equals the original token; the cookie survives the token's
	// single-use invalidation).
	var hasCookie bool
	for _, c := range w.Result().Cookies() {
		if c.Name == setupCookieName && c.Value == tok {
			hasCookie = true
			if c.HttpOnly != true {
				t.Error("cookie must be HttpOnly")
			}
			if c.SameSite != http.SameSiteStrictMode {
				t.Error("cookie must be SameSite=Strict")
			}
		}
	}
	if !hasCookie {
		t.Fatal("expected setup cookie to be set")
	}
}

// TestToken_WrongTokenReturns403 verifies that a wrong token is rejected.
func TestToken_WrongTokenReturns403(t *testing.T) {
	done := false
	r := buildTestRunner(t, false, &done)
	h := r.Handler(func() {}, nil, nil)

	w := doGet(h, "/setup?token=deadbeef")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for wrong token, got %d", w.Code)
	}
}

// TestToken_CookieGrantsAccess verifies that after the token exchange,
// the cookie grants access to GET /setup.
func TestToken_CookieGrantsAccess(t *testing.T) {
	done := false
	r := buildTestRunner(t, false, &done)
	h := r.Handler(func() {}, nil, nil)

	// Use a request that carries the cookie.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.AddCookie(&http.Cookie{Name: setupCookieName, Value: r.token})
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with cookie, got %d", w.Code)
	}
}

// TestToken_ConstantTimeCompare verifies the comparison is via
// subtle.ConstantTimeCompare, not ==.
func TestToken_ConstantTimeCompare(t *testing.T) {
	tok := "abcdef1234567890"
	if !tokenEqual(tok, tok) {
		t.Fatal("identical tokens must match")
	}
	if tokenEqual(tok, "abcdef1234567891") {
		t.Fatal("different tokens must not match")
	}
	if tokenEqual(tok, "") {
		t.Fatal("empty must not match")
	}
}

// TestToken_DisableTokenSkipsAuth verifies that with DisableToken=true,
// GET /setup returns 200 without any cookie or token.
func TestToken_DisableTokenSkipsAuth(t *testing.T) {
	done := false
	r := buildTestRunner(t, true, &done)
	h := r.Handler(func() {}, nil, nil)

	w := doGet(h, "/setup")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 without token (DisableToken), got %d", w.Code)
	}
}

// TestToken_DisableTokenNoTokenInURL verifies SetupURL has no token when
// disabled.
func TestToken_DisableTokenNoTokenInURL(t *testing.T) {
	done := false
	r := buildTestRunner(t, true, &done)
	_ = r.Handler(func() {}, nil, nil)
	url := r.SetupURL("localhost:8080")
	if strings.Contains(url, "token=") {
		t.Fatalf("DisableToken must not include token in URL, got %s", url)
	}
}

// TestToken_URLIncludesToken verifies SetupURL includes the token.
func TestToken_URLIncludesToken(t *testing.T) {
	done := false
	r := buildTestRunner(t, false, &done)
	_ = r.Handler(func() {}, nil, nil)
	url := r.SetupURL("localhost:8080")
	if !strings.Contains(url, "token="+r.token) {
		t.Fatalf("expected token in URL, got %s", url)
	}
}

// TestCrossSite_RejectedOnPost verifies that a cross-site POST is
// rejected via Sec-Fetch-Site.
func TestCrossSite_RejectedOnPost(t *testing.T) {
	done := false
	r := buildTestRunner(t, true, &done)
	h := r.Handler(func() {}, nil, nil)

	w := doPost(h, "/setup", "FOO=bar", map[string]string{
		"Sec-Fetch-Site": "cross-site",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-site POST, got %d", w.Code)
	}
}

// TestCrossSite_SameOriginAllowed verifies that a same-site POST passes
// the Sec-Fetch-Site check.
func TestCrossSite_SameOriginAllowed(t *testing.T) {
	done := false
	r := buildTestRunner(t, true, &done)
	h := r.Handler(func() {}, nil, nil)

	w := doPost(h, "/setup", "FOO=bar", map[string]string{
		"Sec-Fetch-Site": "same-origin",
	})
	// Should NOT be 403 from CSRF (may be 303 redirect on success).
	if w.Code == http.StatusForbidden {
		t.Fatal("same-origin POST must not be rejected by CSRF check")
	}
}

// TestCrossSite_NoFetchMetadataAllowed verifies that a POST without
// Sec-Fetch-Site from a non-browser client passes (curl, tests).
func TestCrossSite_NoFetchMetadataAllowed(t *testing.T) {
	done := false
	r := buildTestRunner(t, true, &done)
	h := r.Handler(func() {}, nil, nil)

	w := doPost(h, "/setup", "FOO=bar", nil)
	if w.Code == http.StatusForbidden {
		t.Fatal("POST without Fetch Metadata must not be rejected")
	}
}

// TestCrossSite_DifferentOriginRejected verifies the Origin fallback.
func TestCrossSite_DifferentOriginRejected(t *testing.T) {
	done := false
	r := buildTestRunner(t, true, &done)
	h := r.Handler(func() {}, nil, nil)

	w := doPost(h, "/setup", "FOO=bar", map[string]string{
		"Origin": "http://evil.com",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for different-origin POST, got %d", w.Code)
	}
}

// extractTokenFromURL parses the setup URL and returns the token query param.
func extractTokenFromURL(t *testing.T, setupURL string) string {
	t.Helper()
	u, err := url.Parse(setupURL)
	if err != nil {
		t.Fatalf("parse setup URL: %v", err)
	}
	tok := u.Query().Get("token")
	if tok == "" {
		t.Fatal("setup URL has no token")
	}
	return tok
}

// ─── F2: Cookie Secure flag derived from request ───────────────────

// TestCookie_SecureFlagFromRequest verifies that the Secure flag on the
// setup cookie reflects the actual request transport, not a hardcoded
// true. Plain-http deployments (LAN IP, TLS-terminating proxy) must get
// a non-Secure cookie so the browser returns it.
func TestCookie_SecureFlagFromRequest(t *testing.T) {
	tests := []struct {
		name       string
		tls        bool
		proto      string
		wantSecure bool
	}{
		{"plain_http", false, "", false},
		{"direct_tls", true, "", true},
		{"forwarded_https", false, "https", true},
		{"forwarded_http", false, "http", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/setup", nil)
			if tt.tls {
				r.TLS = &tls.ConnectionState{}
			}
			if tt.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tt.proto)
			}
			setSetupCookie(w, r, "secret")
			cs := w.Result().Cookies()
			if len(cs) != 1 {
				t.Fatalf("expected 1 cookie, got %d", len(cs))
			}
			if cs[0].Secure != tt.wantSecure {
				t.Errorf("Secure=%v, want %v", cs[0].Secure, tt.wantSecure)
			}
		})
	}
}

// TestCookie_HTTPExchangeFlowWithJar verifies the full token-exchange →
// wizard flow works over plain HTTP using a real cookie jar. With the
// old hardcoded Secure:true, the jar would refuse to return the cookie
// over http://, causing a 403 loop.
func TestCookie_HTTPExchangeFlowWithJar(t *testing.T) {
	done := false
	r := buildTestRunner(t, false, &done)
	h := r.Handler(func() {}, nil, nil)
	tok := extractTokenFromURL(t, r.SetupURL("127.0.0.1:8080"))

	server := httptest.NewServer(h)
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // don't follow redirects
		},
	}

	// Exchange: GET /setup?token=... → 303 + Set-Cookie.
	resp, err := client.Get(server.URL + "/setup?token=" + tok)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("exchange expected 303, got %d", resp.StatusCode)
	}

	// Follow-up: GET /setup — cookie jar must return the cookie.
	resp, err = client.Get(server.URL + "/setup")
	if err != nil {
		t.Fatalf("get /setup: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /setup after exchange expected 200 (cookie not returned over http?), got %d", resp.StatusCode)
	}
}

// ─── F3: Single-use token exchange ─────────────────────────────────

// TestToken_SingleUse_SecondExchangeForbidden verifies that after the
// first successful exchange, a second attempt with the same token is 403.
func TestToken_SingleUse_SecondExchangeForbidden(t *testing.T) {
	done := false
	r := buildTestRunner(t, false, &done)
	h := r.Handler(func() {}, nil, nil)
	tok := extractTokenFromURL(t, r.SetupURL("127.0.0.1:8080"))

	// First exchange succeeds.
	w := doGet(h, "/setup?token="+tok)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("first exchange expected 303, got %d", w.Code)
	}

	// Second exchange is forbidden.
	w = doGet(h, "/setup?token="+tok)
	if w.Code != http.StatusForbidden {
		t.Fatalf("second exchange expected 403, got %d", w.Code)
	}
}

// TestToken_SingleUse_CookieSurvives verifies that after the token is
// invalidated, the already-issued cookie continues to grant access.
func TestToken_SingleUse_CookieSurvives(t *testing.T) {
	done := false
	r := buildTestRunner(t, false, &done)
	h := r.Handler(func() {}, nil, nil)
	tok := r.token

	// Exchange: get the cookie.
	w := doGet(h, "/setup?token="+tok)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("exchange expected 303, got %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	// Token is now invalidated.
	if r.token != "" {
		t.Fatal("token should be empty after exchange")
	}

	// Cookie still works for GET /setup.
	w = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/setup", nil)
	req.AddCookie(cookies[0])
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /setup with cookie after token invalidation expected 200, got %d", w.Code)
	}

	// Second token exchange is still forbidden.
	w = doGet(h, "/setup?token="+tok)
	if w.Code != http.StatusForbidden {
		t.Fatalf("second exchange expected 403, got %d", w.Code)
	}
}

// TestToken_SingleUse_RestartMintsFresh verifies that a new Runner (simulating
// app restart) mints a fresh token after the previous one was consumed.
func TestToken_SingleUse_RestartMintsFresh(t *testing.T) {
	done := false
	r := buildTestRunner(t, false, &done)
	h := r.Handler(func() {}, nil, nil)
	firstTok := r.token

	// Exchange it.
	w := doGet(h, "/setup?token="+firstTok)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("first exchange expected 303, got %d", w.Code)
	}

	// Simulate restart: new Runner, new Handler, new token.
	r2 := buildTestRunner(t, false, &done)
	h2 := r2.Handler(func() {}, nil, nil)
	if r2.token == "" {
		t.Fatal("new Runner should mint a fresh token")
	}
	if r2.token == firstTok {
		t.Fatal("new token should differ from the consumed one")
	}

	// The fresh token works.
	w = doGet(h2, "/setup?token="+r2.token)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("exchange with fresh token expected 303, got %d", w.Code)
	}
}
