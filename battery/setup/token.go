package setup

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
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
	// No secret has been minted yet, so no cookie can be valid. Without
	// this, an empty expected would authenticate an empty cookie value.
	if expected == "" {
		return false
	}
	c, err := r.Cookie(setupCookieName)
	if err != nil {
		return false
	}
	return tokenEqual(c.Value, expected)
}

// rejectCrossSiteForm refuses a cross-site POST to the wizard. The gate
// is handler.IsForgeableRequest (the wizard's forms are urlencoded,
// CORS-simple) and handler.IsCrossSiteRequest, the repo's one cross-site
// predicate: Sec-Fetch-Site first, where "same-site" is NOT proof of
// same-origin — a page on a sibling subdomain (evil.example.com →
// app.example.com) is same-site while its Origin names another host, and
// the SameSite=Strict setup cookie still attaches — so it falls through
// to the Origin-host comparison, as does any unknown value. Non-browser
// clients (curl, tests) send neither header and pass.
func rejectCrossSiteForm(w http.ResponseWriter, r *http.Request) bool {
	if !handler.IsForgeableRequest(r) {
		return false
	}
	if handler.IsCrossSiteRequest(r) {
		http.Error(w, "forbidden: cross-site request", http.StatusForbidden)
		return true
	}
	return false
}
