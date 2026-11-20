package setup

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

const (
	setupCookieName = "gofastr_setup"
	tokenBytes      = 32
)

func generateToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func tokenEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

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

func hasSetupCookie(r *http.Request, expected string) bool {
	if expected == "" {
		return false
	}
	c, err := r.Cookie(setupCookieName)
	if err != nil {
		return false
	}
	return tokenEqual(c.Value, expected)
}

func rejectCrossSiteForm(w http.ResponseWriter, r *http.Request) bool {
	if !handler.IsForgeableRequest(r) {
		return false
	}
	if handler.IsCrossSiteRequest(r) {
		i18n.HTTPError(w, r, http.StatusForbidden, handler.ReasonCrossSiteRefused)
		return true
	}
	return false
}
