package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Found by the 2026-11-01 battery/auth bug bash. auth.CSRF skipped any
// request carrying an Authorization header. Basic, Negotiate and NTLM are
// credentials the browser attaches by itself, to a cross-site form too, so
// behind an HTTP-auth staging gate every cookie-authenticated form POST
// passed CSRF with no token.
func TestCSRFChecksBrowserAttachedAuthorization(t *testing.T) {
	for _, header := range []string{"Basic dXNlcjpwYXNz", "Negotiate YIIGhgYJKo"} {
		called := false
		h := CSRF()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
		req := httptest.NewRequest(http.MethodPost, "/account/delete", strings.NewReader("x=1"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", header)
		req.AddCookie(&http.Cookie{Name: "session_id", Value: "victim-session"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if called || rec.Code != http.StatusForbidden {
			t.Errorf("SECURITY: Authorization %q: form POST with no CSRF token passed (status %d)", header, rec.Code)
		}
	}
}

// CSRFToken prefers the __Host- cookie: on HTTPS it is the one the
// middleware set, and a sibling subdomain can plant only the bare name.
func TestCSRFTokenPrefersHostPrefixedCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "planted"})
	req.AddCookie(&http.Cookie{Name: "__Host-" + CSRFCookieName, Value: "real"})
	got := CSRFToken(req)
	if got == "" {
		t.Fatal("no token")
	}
	if !csrfMaskedEquals(got, "real") {
		t.Errorf("CSRFToken read the bare cookie over the __Host- one")
	}
}

// csrfMaskedEquals unmasks a MaskCSRFToken value ("m1." + base64url(pad ||
// pad XOR token)) and compares it with want.
func csrfMaskedEquals(masked, want string) bool {
	rest, ok := strings.CutPrefix(masked, "m1.")
	if !ok {
		return masked == want
	}
	buf, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil || len(buf)%2 != 0 {
		return false
	}
	n := len(buf) / 2
	out := make([]byte, n)
	for i := range n {
		out[i] = buf[i] ^ buf[n+i]
	}
	return string(out) == want
}
