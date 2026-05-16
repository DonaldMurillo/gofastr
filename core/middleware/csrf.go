package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
)

// CSRFConfig configures the double-submit-cookie CSRF middleware.
//
// CookieName / HeaderName must match what the client sends back; defaults
// are sensible.
//
// Skip is consulted on every request — return true to bypass the check
// entirely (e.g., for endpoints authenticated by Bearer tokens or API
// keys, which aren't subject to CSRF since they don't ride on cookies).
//
// SecretKey, when non-empty, switches the middleware to a signed-double-
// submit pattern: the cookie value is "<random>.<HMAC>" and the server
// rejects any request whose cookie/header lacks a valid signature. Without
// this, naive double-submit is vulnerable to cookie injection from sibling
// subdomains. Strongly recommended; defaults to a per-process random key
// if left empty (rotates on restart).
//
// When SecretKey is set AND CookieSecure (or r.TLS) is true, the cookie
// name automatically gets the __Host- prefix, which forbids subdomain
// cookie injection at the browser level.
type CSRFConfig struct {
	CookieName string
	HeaderName string
	CookiePath string

	// CookieSecure marks the CSRF cookie Secure (HTTPS-only). Leave false
	// for local dev; set true in production.
	CookieSecure bool

	// SecretKey is the HMAC key used to sign the CSRF token. Empty means
	// the middleware autogenerates a per-process key on first use.
	SecretKey []byte

	// Skip allows the middleware to be bypassed for specific requests.
	Skip func(*http.Request) bool
}

var (
	csrfAutoKeyOnce sync.Once
	csrfAutoKey     []byte
)

func ensureCSRFKey(cfg *CSRFConfig) {
	if len(cfg.SecretKey) > 0 {
		return
	}
	csrfAutoKeyOnce.Do(func() {
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			// Crypto-rand never fails in practice; bail loudly.
			panic("csrf: autogenerating SecretKey: " + err.Error())
		}
		csrfAutoKey = k
	})
	cfg.SecretKey = csrfAutoKey
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
	if cfg.HeaderName == "" {
		cfg.HeaderName = "X-CSRF-Token"
	}
	if cfg.CookiePath == "" {
		cfg.CookiePath = "/"
	}
	ensureCSRFKey(&cfg)
	// Resolve the cookie name. __Host- prefix requires Path=/, Secure,
	// and no Domain — all of which we satisfy. The browser refuses to
	// accept a __Host- cookie set from a sibling subdomain.
	resolveCookieName := func(secure bool) string {
		if cfg.CookieName != "" {
			return cfg.CookieName
		}
		if secure {
			return "__Host-csrf"
		}
		return "csrf_token"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Skip != nil && cfg.Skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			secure := cfg.CookieSecure || r.TLS != nil
			cookieName := resolveCookieName(secure)

			if isSafeMethod(r.Method) {
				// Set a signed token cookie if the client doesn't have one yet.
				if _, err := r.Cookie(cookieName); err != nil {
					tok, err := generateSignedCSRFToken(cfg.SecretKey)
					if err != nil {
						http.Error(w, "csrf: token generation failed", http.StatusInternalServerError)
						return
					}
					http.SetCookie(w, &http.Cookie{
						Name:     cookieName,
						Value:    tok,
						Path:     cfg.CookiePath,
						HttpOnly: false, // client JS must read it to set the header
						Secure:   secure,
						SameSite: http.SameSiteLaxMode,
					})
				}
				next.ServeHTTP(w, r)
				return
			}

			// Unsafe method — verify header matches cookie AND signature is valid.
			cookie, err := r.Cookie(cookieName)
			if err != nil {
				http.Error(w, "csrf: missing cookie", http.StatusForbidden)
				return
			}
			header := r.Header.Get(cfg.HeaderName)
			if header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 {
				http.Error(w, "csrf: token mismatch", http.StatusForbidden)
				return
			}
			if !verifySignedCSRFToken(cookie.Value, cfg.SecretKey) {
				// Header matched cookie but the signature is bogus — i.e.
				// an attacker planted both via a subdomain and didn't
				// have the signing key. Reject.
				http.Error(w, "csrf: invalid token signature", http.StatusForbidden)
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

// generateSignedCSRFToken returns "<random>.<HMAC>". The HMAC binds the
// token to the server's secret so an attacker who can plant a cookie
// (e.g. via subdomain XSS) but doesn't know the secret cannot forge a
// value the server accepts.
func generateSignedCSRFToken(secret []byte) (string, error) {
	random, err := generateCSRFToken()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(random))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return random + "." + sig, nil
}

// verifySignedCSRFToken returns true if `value` is a well-formed signed
// CSRF token whose HMAC matches `secret`. Constant-time comparison.
func verifySignedCSRFToken(value string, secret []byte) bool {
	idx := strings.LastIndexByte(value, '.')
	if idx <= 0 || idx == len(value)-1 {
		return false
	}
	random, sig := value[:idx], value[idx+1:]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(random))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) == 1
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
