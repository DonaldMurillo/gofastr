package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// CSRF must use the __Host- prefix when secure, and the cookie value
// must be HMAC-signed so a subdomain attacker who plants both a cookie
// AND the matching header value can't forge a request — without the
// signing key they can't construct a value the server will accept.

func newOK() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func TestCSRF_SecretRequiresHostPrefix(t *testing.T) {
	cfg := CSRFConfig{
		SecretKey:    []byte("0123456789abcdef0123456789abcdef"),
		CookieSecure: true,
	}
	h := CSRF(cfg)(newOK())

	req := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	req.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var name string
	for _, c := range w.Result().Cookies() {
		name = c.Name
	}
	if !strings.HasPrefix(name, "__Host-") {
		t.Fatalf("CSRF cookie should use __Host- prefix in secure mode; got %q", name)
	}
}

func TestCSRF_UnsignedHeaderRejected(t *testing.T) {
	cfg := CSRFConfig{
		SecretKey:    []byte("0123456789abcdef0123456789abcdef"),
		CookieSecure: false, // simulate dev so __Host- isn't required
	}
	h := CSRF(cfg)(newOK())

	// Get a cookie via a safe request first.
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	cookies := w1.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("no CSRF cookie issued on safe request")
	}
	cookieName := cookies[0].Name
	cookieVal := cookies[0].Value

	// Cookie value must contain the signature separator.
	if !strings.Contains(cookieVal, ".") {
		t.Fatalf("signed cookie should be of form <random>.<sig>, got %q", cookieVal)
	}

	// An attacker who plants a fake cookie and matching header (both being
	// just the random part — no signature) must be rejected.
	random := strings.SplitN(cookieVal, ".", 2)[0]
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	req2.AddCookie(&http.Cookie{Name: cookieName, Value: random})
	req2.Header.Set("X-CSRF-Token", random)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unsigned token (subdomain attacker scenario), got %d", w2.Code)
	}
}

func TestCSRF_ForgedSignatureRejected(t *testing.T) {
	cfg := CSRFConfig{
		SecretKey: []byte("0123456789abcdef0123456789abcdef"),
	}
	h := CSRF(cfg)(newOK())

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	cookies := w1.Result().Cookies()
	cookieName := cookies[0].Name
	cookieVal := cookies[0].Value

	// Replace the signature with garbage — server must reject.
	parts := strings.SplitN(cookieVal, ".", 2)
	forged := parts[0] + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	req2.AddCookie(&http.Cookie{Name: cookieName, Value: forged})
	req2.Header.Set("X-CSRF-Token", forged)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for forged signature, got %d", w2.Code)
	}
}

func TestCSRF_GenuineSignedTokenAccepted(t *testing.T) {
	cfg := CSRFConfig{
		SecretKey: []byte("0123456789abcdef0123456789abcdef"),
	}
	h := CSRF(cfg)(newOK())

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	cookies := w1.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("no cookie issued")
	}
	cookieName := cookies[0].Name
	cookieVal := cookies[0].Value

	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	req2.AddCookie(&http.Cookie{Name: cookieName, Value: cookieVal})
	req2.Header.Set("X-CSRF-Token", cookieVal)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("genuine signed token must pass; got %d", w2.Code)
	}
}
