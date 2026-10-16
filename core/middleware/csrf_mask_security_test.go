package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A CSRF token rendered into a compressed page is a BREACH target: an
// attacker who can reflect input next to it reads the secret byte by
// byte from response sizes. MaskCSRFToken gives every render a fresh
// one-time pad, so the bytes in the page never repeat, and the
// middleware accepts the masked form wherever it accepts the raw one.

func csrfCookie(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	cs := w.Result().Cookies()
	if len(cs) == 0 {
		t.Fatal("no CSRF cookie issued")
	}
	return cs[0]
}

func postWithToken(h http.Handler, c *http.Cookie, token string) int {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.AddCookie(c)
	r.Header.Set("X-CSRF-Token", token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

func TestMaskedCSRFTokenIsAccepted(t *testing.T) {
	h := CSRF(CSRFConfig{SecretKey: []byte("0123456789abcdef0123456789abcdef")})(newOK())
	c := csrfCookie(t, h)
	if code := postWithToken(h, c, MaskCSRFToken(c.Value)); code != http.StatusNoContent {
		t.Fatalf("masked token: status %d, want 204", code)
	}
	if code := postWithToken(h, c, c.Value); code != http.StatusNoContent {
		t.Fatalf("raw token: status %d, want 204", code)
	}
}

func TestMaskedCSRFTokenNeverRepeats(t *testing.T) {
	const tok = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG.signature"
	a, b := MaskCSRFToken(tok), MaskCSRFToken(tok)
	if a == b {
		t.Fatal("two masks of one token are identical")
	}
	if strings.Contains(a, tok) || strings.Contains(a, tok[:16]) {
		t.Fatal("masked token carries the raw token's bytes")
	}
	if got := unmaskCSRFToken(a); got != tok {
		t.Fatalf("unmask = %q, want the raw token", got)
	}
}

func TestTamperedMaskedTokenRejected(t *testing.T) {
	h := CSRF(CSRFConfig{SecretKey: []byte("0123456789abcdef0123456789abcdef")})(newOK())
	c := csrfCookie(t, h)
	m := []byte(MaskCSRFToken(c.Value))
	m[len(m)-1] ^= 0x01
	if m[len(m)-1] == '.' || m[len(m)-1] == '=' {
		m[len(m)-1] = 'A'
	}
	if code := postWithToken(h, c, string(m)); code != http.StatusForbidden {
		t.Fatalf("tampered mask: status %d, want 403", code)
	}
	for _, bad := range []string{maskedPrefix, maskedPrefix + "!!", maskedPrefix + "QUJD"} {
		if code := postWithToken(h, c, bad); code != http.StatusForbidden {
			t.Fatalf("malformed mask %q: status %d, want 403", bad, code)
		}
	}
}

func TestMaskOfEmptyTokenIsEmpty(t *testing.T) {
	if MaskCSRFToken("") != "" {
		t.Fatal("masking no token must render nothing")
	}
}
