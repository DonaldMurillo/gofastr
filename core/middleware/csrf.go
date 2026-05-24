package middleware

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// logSlogWarnDefault routes to slog.Default() so callers passing nil to
// WarnIfCSRFUnconfigured still get the warning on whatever the host's
// configured default handler is.
func logSlogWarnDefault(msg string) {
	slog.Default().Warn(msg)
}

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
	// the middleware autogenerates a per-process key on first use —
	// fine for single-instance dev, NOT acceptable for production
	// (each restart and each fleet replica gets a different key, so
	// in-flight tokens silently 403). Call WarnIfCSRFUnconfigured at
	// startup to surface this to operators.
	SecretKey []byte

	// AdditionalKeys lets a deploy rotate SecretKey without invalidating
	// every in-flight form. The middleware signs new tokens with
	// SecretKey but accepts tokens verified by SecretKey OR any key
	// listed here. Drain the previous key from this list once all old
	// tokens have expired.
	AdditionalKeys [][]byte

	// Skip allows the middleware to be bypassed for specific requests.
	Skip func(*http.Request) bool

	// FormField is the form-body field name read as a fallback when the
	// request is form-encoded and the header is missing. Defaults to
	// "_csrf". HTML form flows put the token in a hidden input with
	// this name so the header doesn't need to be set client-side.
	FormField string

	// MaxFormBytes caps how much of a form-encoded request body the
	// middleware will buffer when probing for FormField. Defaults to
	// 1 MiB. Bodies above the cap return 413 before any allocation —
	// without this, an unauthenticated attacker could force the
	// process to buffer up to 10 MB (form-urlencoded) or 32 MB
	// (multipart) per request just to land in the signature-mismatch
	// branch. Set to a smaller value for endpoints that never carry
	// large forms; do NOT set to 0 (interpreted as "use default").
	MaxFormBytes int64
}

// defaultCSRFMaxFormBytes is the form-body cap when CSRFConfig.MaxFormBytes
// is unset. Matches the json-body cap in battery/auth so all body-limit
// surfaces in the stack agree at 1 MiB.
const defaultCSRFMaxFormBytes int64 = 1 << 20

// csrfTokenCtxKey types the request-context key the middleware uses to
// surface a freshly-minted token on the SAME request that received it.
// Template helpers (e.g. auth.CSRFInputHTML) read this so the hidden
// input renders correctly on the first GET, before the browser has
// echoed the cookie back.
type csrfTokenCtxKey struct{}

// TokenFromContext returns the CSRF token stashed on ctx by the
// middleware, or "" when no token is available. Template helpers
// should prefer this over r.Cookie because the cookie isn't in the
// request on the GET that mints it.
func TokenFromContext(ctx context.Context) string {
	v, _ := ctx.Value(csrfTokenCtxKey{}).(string)
	return v
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
	if cfg.FormField == "" {
		cfg.FormField = "_csrf"
	}
	if cfg.MaxFormBytes <= 0 {
		cfg.MaxFormBytes = defaultCSRFMaxFormBytes
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
				// Set a signed token cookie if the client doesn't have one
				// yet. EITHER way, stash the token value on ctx so the
				// downstream template helper (CSRFInputHTML) can render
				// the hidden input on the same request — without that,
				// the first GET's response has the cookie but the body
				// has no token (the cookie isn't in r.Cookies() yet).
				//
				// Skip the cookie/ctx work entirely when an outer CSRF
				// middleware already stashed a token on ctx — nested
				// instances would otherwise emit duplicate Set-Cookie
				// headers (browser keeps the last, but it's noisy and
				// can race with operator-rotated keys).
				if TokenFromContext(r.Context()) != "" {
					next.ServeHTTP(w, r)
					return
				}
				var token string
				if existing, err := r.Cookie(cookieName); err == nil {
					token = existing.Value
				} else {
					tok, err := generateSignedCSRFToken(cfg.SecretKey)
					if err != nil {
						http.Error(w, "csrf: token generation failed", http.StatusInternalServerError)
						return
					}
					token = tok
					http.SetCookie(w, &http.Cookie{
						Name:     cookieName,
						Value:    tok,
						Path:     cfg.CookiePath,
						HttpOnly: false, // client JS must read it to set the header
						Secure:   secure,
						SameSite: http.SameSiteLaxMode,
					})
				}
				ctx := context.WithValue(r.Context(), csrfTokenCtxKey{}, token)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Unsafe method — verify header (or form-body fallback) matches
			// cookie AND signature is valid.
			cookie, err := r.Cookie(cookieName)
			if err != nil {
				http.Error(w, "csrf: missing cookie", http.StatusForbidden)
				return
			}
			submitted := r.Header.Get(cfg.HeaderName)
			if submitted == "" && isFormContentType(r) {
				// Buffer the body up to MaxFormBytes, parse for the CSRF
				// token, then restore r.Body so downstream handlers (which
				// might want raw r.Body access, e.g. upload streams) still
				// see the bytes. Over-cap requests return 413 immediately.
				buf, err := readAndBufferCapped(r, cfg.MaxFormBytes)
				if err != nil {
					http.Error(w, "csrf: form body too large", http.StatusRequestEntityTooLarge)
					return
				}
				submitted = parseFormField(buf, r.Header.Get("Content-Type"), cfg.FormField)
				r.Body = io.NopCloser(bytes.NewReader(buf))
			}
			if submitted == "" || subtle.ConstantTimeCompare([]byte(submitted), []byte(cookie.Value)) != 1 {
				http.Error(w, "csrf: token mismatch", http.StatusForbidden)
				return
			}
			if !verifySignedTokenAny(cookie.Value, cfg.SecretKey, cfg.AdditionalKeys) {
				// Header matched cookie but the signature is bogus — i.e.
				// an attacker planted both via a subdomain and didn't
				// have the signing key (and the additional rotation
				// keys, if any, also rejected). Reject.
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

// readAndBufferCapped reads at most cap+1 bytes from r.Body. Returns the
// buffered bytes when within cap, or an "over-capped" error otherwise.
// The +1 sentinel lets us distinguish "exactly cap bytes" from "more
// than cap bytes" without re-reading.
func readAndBufferCapped(r *http.Request, cap int64) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	limited := io.LimitReader(r.Body, cap+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > cap {
		return nil, errors.New("body exceeds cap")
	}
	return buf, nil
}

// parseFormField extracts the named field from a buffered request body,
// handling either application/x-www-form-urlencoded or multipart/form-data.
// Returns "" when the field is missing or the body is malformed —
// the caller treats either as "no token submitted" and rejects.
func parseFormField(body []byte, contentType, name string) string {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	switch mediaType {
	case "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return ""
		}
		return values.Get(name)
	case "multipart/form-data":
		boundary := params["boundary"]
		if boundary == "" {
			return ""
		}
		mr := multipart.NewReader(bytes.NewReader(body), boundary)
		// Cap multipart parsing at the same body length already held;
		// ReadForm spills above maxMemory but we've already capped at
		// MaxFormBytes upstream.
		form, err := mr.ReadForm(int64(len(body)))
		if err != nil {
			return ""
		}
		defer form.RemoveAll()
		if vs, ok := form.Value[name]; ok && len(vs) > 0 {
			return vs[0]
		}
		return ""
	default:
		return ""
	}
}

// isFormContentType reports whether the request body is form-encoded.
// Used by the CSRF check to decide whether to look for the token in
// the form-body fallback field.
func isFormContentType(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(strings.ToLower(ct))
	return ct == "application/x-www-form-urlencoded" || ct == "multipart/form-data"
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

// verifySignedTokenAny returns true if any of (primary, ...additional)
// validates value's HMAC. Lets a rolling deploy keep verifying tokens
// signed by the previous SecretKey while minting new ones under the
// rolled key.
//
// Runs ALL key checks even after a match — early-return on match would
// leak (via timing) which key signed the token, useful to an attacker
// who wants to identify sessions signed by the old, soon-to-be-dropped
// rotation key.
func verifySignedTokenAny(value string, primary []byte, additional [][]byte) bool {
	ok := verifySignedCSRFToken(value, primary)
	for _, k := range additional {
		if len(k) == 0 {
			continue
		}
		// Don't short-circuit — the cost of one extra HMAC per request
		// is negligible vs. the cost of a rotation-key timing oracle.
		if verifySignedCSRFToken(value, k) {
			ok = true
		}
	}
	return ok
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

// WarnIfCSRFUnconfigured emits a WARN log when the given CSRFConfig
// relies on the per-process auto-generated SecretKey. Hosts should
// call this once at startup. Multi-instance deploys silently break
// without a shared SecretKey because every replica signs tokens with
// a different per-process key — verification fails when a request
// lands on a different replica than the one that minted the cookie.
//
// Passing nil for logger uses slog.Default. Safe to call with any
// CSRFConfig; emits nothing when SecretKey is set.
func WarnIfCSRFUnconfigured(cfg CSRFConfig, logger interface {
	Warn(msg string, args ...any)
}) {
	if len(cfg.SecretKey) > 0 {
		return
	}
	if logger == nil {
		// stdlib slog default — keeps this package log-implementation-agnostic.
		// Importing log/slog here is fine; it's stdlib since Go 1.21.
		logSlogWarnDefault("CSRF SecretKey not configured — auto-generated per-process key will not survive restart and will reject form submits across multi-instance deploys. Set CSRFConfig.SecretKey for production.")
		return
	}
	logger.Warn("CSRF SecretKey not configured — auto-generated per-process key will not survive restart and will reject form submits across multi-instance deploys",
		"recommendation", "set CSRFConfig.SecretKey for production")
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
