package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestSkipBearerAuthKeepsCSRFForAmbientSchemes: SkipBearerAuth skipped
// CSRF for ANY Authorization header. Basic, Digest, Negotiate and NTLM
// are ambient: the browser caches the credentials and attaches them to
// a cross-site form POST on its own, so an app behind a Basic-auth gate
// or an intranet Negotiate SSO lost CSRF protection on every cookie
// write (auth.CSRF ships SkipBearerAuth as its default Skip). Only
// Bearer skips now: Token and AWS4-HMAC-SHA256 owe a token too.
func TestSkipBearerAuthKeepsCSRFForAmbientSchemes(t *testing.T) {
	var reached atomic.Int32
	h := CSRF(CSRFConfig{SecretKey: []byte("ambient-auth-csrf-key-32-bytes!!"), Skip: SkipBearerAuth()})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			reached.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
	post := func(authz string) (int, int32) {
		reached.Store(0)
		r := httptest.NewRequest(http.MethodPost, "/transfer", strings.NewReader("amount=1000"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Authorization", authz)
		r.AddCookie(&http.Cookie{Name: "session_id", Value: "victim-session"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, reached.Load()
	}
	for _, authz := range []string{
		"Basic YWxpY2U6c2VjcmV0",
		"basic YWxpY2U6c2VjcmV0",
		"Negotiate YIIGhgYGKwYBBQUCoIIGejCCBnag",
		"NTLM TlRMTVNTUAABAAAAB4IIog==",
		`Digest username="alice", realm="x", nonce="n", uri="/", response="r"`,
		"DIGEST\tusername=\"alice\"",
		"Token abc",
		"AWS4-HMAC-SHA256 Credential=x",
	} {
		if code, n := post(authz); code != http.StatusForbidden || n != 0 {
			t.Errorf("SECURITY: Authorization %q: tokenless form POST reached the handler (status %d); only Bearer skips CSRF",
				strings.Fields(authz)[0], code)
		}
	}
	for _, authz := range []string{"Bearer eyJhbGciOi", "bearer abc"} {
		if code, n := post(authz); code != http.StatusOK || n != 1 {
			t.Errorf("Authorization %q: status %d, want the skip (a Bearer credential)", strings.Fields(authz)[0], code)
		}
	}
}
