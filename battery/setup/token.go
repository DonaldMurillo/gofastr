package setup

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
)

const (
	// setupCookieName is the HttpOnly cookie that authenticates the
	// wizard after the one-time token exchange.
	setupCookieName = "gofastr_setup"
	// tokenBytes is the length of the raw random token (32 bytes → 64 hex).
	tokenBytes = 32
)

// generateToken returns a cryptographically random hex string.
func generateToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// tokenEqual reports whether the provided token matches the expected
// one using a constant-time comparison.
func tokenEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// setSetupCookie writes the HttpOnly SameSite=Strict cookie that
// authenticates subsequent wizard requests. The Secure flag is derived
// from the actual request transport so plain-http deployments (LAN IP,
// TLS-terminating proxy) aren't locked out: the cookie is Secure when
// the request arrived over TLS or via an X-Forwarded-Proto: https
// header (the standard convention behind TLS-terminating proxies).
func setSetupCookie(w http.ResponseWriter, r *http.Request, cookieValue string) {
	secure := r.TLS != nil ||
		strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     setupCookieName,
		Value:    cookieValue,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
	})
}

// hasSetupCookie reports whether the request carries a valid setup cookie.
func hasSetupCookie(r *http.Request, expected string) bool {
	c, err := r.Cookie(setupCookieName)
	if err != nil {
		return false
	}
	return tokenEqual(c.Value, expected)
}

// rejectCrossSiteForm refuses a cross-site POST to the wizard. Mirrors
// battery/auth's Sec-Fetch-Site convention: the authoritative signal is
// checked first; browsers send Origin:null on some legitimate flows so
// Origin-checking alone is wrong. Non-browser clients (curl, tests) send
// neither header and pass.
func rejectCrossSiteForm(w http.ResponseWriter, r *http.Request) bool {
	// Primary: Fetch Metadata. same-origin / same-site / none are safe.
	if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "" {
		if sfs == "cross-site" {
			http.Error(w, "forbidden: cross-site request", http.StatusForbidden)
			return true
		}
		return false
	}
	// Fallback for clients without Fetch Metadata: compare Origin host.
	// Absent or opaque ("null") Origin can't prove an attack — allow.
	if o := r.Header.Get("Origin"); o != "" && o != "null" {
		if h := parseOriginHost(o); h != "" && !strings.EqualFold(h, r.Host) {
			http.Error(w, "forbidden: cross-site request", http.StatusForbidden)
			return true
		}
	}
	return false
}

// parseOriginHost extracts the host portion from an Origin header value.
func parseOriginHost(origin string) string {
	u, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return u.Host
}
