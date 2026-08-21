package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func csrfHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestCSRF_GetSetsCookie(t *testing.T) {
	h := CSRF(CSRFConfig{})(csrfHandler())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "csrf_token=") {
		t.Fatalf("expected csrf_token cookie set, got %q", w.Header().Get("Set-Cookie"))
	}
}

func TestCSRF_PostBlockedWithoutToken(t *testing.T) {
	h := CSRF(CSRFConfig{})(csrfHandler())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestCSRF_PostAllowedWithMatchingHeader(t *testing.T) {
	// Tokens are signed (P1-8 hardening); generate a real one.
	secret := []byte("0123456789abcdef0123456789abcdef")
	h := CSRF(CSRFConfig{SecretKey: secret})(csrfHandler())
	tok, err := generateSignedCSRFToken(secret)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.AddCookie(&http.Cookie{Name: "csrf_token", Value: tok})
	r.Header.Set("X-CSRF-Token", tok)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with matching token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCSRF_PostBlockedOnMismatch(t *testing.T) {
	h := CSRF(CSRFConfig{})(csrfHandler())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.AddCookie(&http.Cookie{Name: "csrf_token", Value: "a"})
	r.Header.Set("X-CSRF-Token", "b")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on token mismatch, got %d", w.Code)
	}
}

func TestCSRF_SkipPredicateBypasses(t *testing.T) {
	h := CSRF(CSRFConfig{Skip: SkipBearerAuth()})(csrfHandler())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("Authorization", "Bearer xyz")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected bearer-authed POST to bypass CSRF, got %d", w.Code)
	}
}

// TestCSRFCookieHostPrefixedBehindProxy pins that the __Host- promotion
// is decided by the same signal the Secure flag is.
//
// Two notions of "secure" had drifted: the Secure FLAG is computed per
// request (r.TLS or X-Forwarded-Proto), while the __Host- NAME was
// resolved once at construction from CookieSecure. On the standard
// deployment, TLS terminated at a proxy, the host never calling
// WithCSRFCookieSecure(true), the cookie came back Secure but named
// plainly. The signed token is not session-bound, so the __Host- prefix
// was the whole defense against a sibling subdomain planting a valid
// token and driving a same-site POST (the session cookie is
// SameSite=Strict, so it rides).
func TestCSRFCookieHostPrefixedBehindProxy(t *testing.T) {
	cases := []struct {
		name       string
		header     string
		wantPrefix bool
	}{
		{"tls terminating proxy", "https", true},
		{"plain http", "", false},
		{"proxy reports http", "http", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// A host-supplied cookie name, which is what battery/auth
			// passes; the branch the per-request resolution skipped.
			h := CSRF(CSRFConfig{CookieName: "auth_csrf", HostPrefixWhenSecure: true})(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if c.header != "" {
				req.Header.Set("X-Forwarded-Proto", c.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			var got *http.Cookie
			for _, ck := range rec.Result().Cookies() {
				if strings.HasSuffix(ck.Name, "auth_csrf") {
					got = ck
				}
			}
			if got == nil {
				t.Fatalf("no CSRF cookie set: %v", rec.Result().Cookies())
			}
			hasPrefix := strings.HasPrefix(got.Name, "__Host-")
			if hasPrefix != c.wantPrefix {
				t.Errorf("SECURITY: [csrf] cookie name %q (__Host- = %v), want __Host- = %v — the name must follow the same per-request secure signal as the Secure flag (%v)",
					got.Name, hasPrefix, c.wantPrefix, got.Secure)
			}
			if got.Secure != c.wantPrefix {
				t.Errorf("Secure flag = %v, want %v — the two signals disagree again", got.Secure, c.wantPrefix)
			}
		})
	}
}
