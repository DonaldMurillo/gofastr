package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

// CSRFConfig configures the double-submit-cookie CSRF middleware.
//
// CookieName / HeaderName must match what the client sends back; defaults
// are sensible.
//
// Skip is consulted on every request — return true to bypass the check
// entirely (e.g., for endpoints authenticated by Bearer tokens or API
// keys, which aren't subject to CSRF since they don't ride on cookies).
type CSRFConfig struct {
	CookieName string
	HeaderName string
	CookiePath string

	// CookieSecure marks the CSRF cookie Secure (HTTPS-only). Leave false
	// for local dev; set true in production.
	CookieSecure bool

	// Skip allows the middleware to be bypassed for specific requests.
	Skip func(*http.Request) bool
}

// CSRF returns a Middleware that enforces the double-submit cookie pattern:
//
//  1. On safe methods (GET, HEAD, OPTIONS) the middleware sets a cookie
//     containing a freshly-rotated token if none is present.
//  2. On unsafe methods (POST, PUT, PATCH, DELETE) the middleware verifies
//     that the header value matches the cookie value. Mismatch → 403.
//
// This protects against cross-site form submissions because attacker-
// controlled pages can't read the cookie value to populate the header.
func CSRF(cfg CSRFConfig) Middleware {
	if cfg.CookieName == "" {
		cfg.CookieName = "csrf_token"
	}
	if cfg.HeaderName == "" {
		cfg.HeaderName = "X-CSRF-Token"
	}
	if cfg.CookiePath == "" {
		cfg.CookiePath = "/"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Skip != nil && cfg.Skip(r) {
				next.ServeHTTP(w, r)
				return
			}

			if isSafeMethod(r.Method) {
				// Set a token cookie if the client doesn't have one yet.
				if _, err := r.Cookie(cfg.CookieName); err != nil {
					tok, err := generateCSRFToken()
					if err != nil {
						http.Error(w, "csrf: token generation failed", http.StatusInternalServerError)
						return
					}
					http.SetCookie(w, &http.Cookie{
						Name:     cfg.CookieName,
						Value:    tok,
						Path:     cfg.CookiePath,
						HttpOnly: false, // client JS must read it to set the header
						Secure:   cfg.CookieSecure,
						SameSite: http.SameSiteLaxMode,
					})
				}
				next.ServeHTTP(w, r)
				return
			}

			// Unsafe method — verify header matches cookie.
			cookie, err := r.Cookie(cfg.CookieName)
			if err != nil {
				http.Error(w, "csrf: missing cookie", http.StatusForbidden)
				return
			}
			header := r.Header.Get(cfg.HeaderName)
			if header == "" || header != cookie.Value {
				http.Error(w, "csrf: token mismatch", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isSafeMethod reports whether the HTTP method is considered safe (does not
// mutate state) under RFC 7231 §4.2.1.
func isSafeMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// generateCSRFToken returns 32 bytes of cryptographic randomness, base64-
// encoded for header / cookie transport.
func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// SkipBearerAuth returns a Skip predicate suitable for CSRFConfig.Skip that
// bypasses requests using Authorization: Bearer or Api-Key headers — those
// don't ride on cookies and so aren't subject to CSRF.
func SkipBearerAuth() func(*http.Request) bool {
	return func(r *http.Request) bool {
		if r.Header.Get("Authorization") != "" {
			return true
		}
		if r.Header.Get("X-API-Key") != "" {
			return true
		}
		return false
	}
}
