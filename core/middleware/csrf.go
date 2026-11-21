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
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func logSlogWarnDefault(msg string) {
	slog.Default().Warn(msg)
}

// CSRFConfig configures the signed double-submit-cookie CSRF middleware; on a secure request the cookie name gets the __Host- prefix unless NoHostPrefix is set.
type CSRFConfig struct {
	CookieName string
	HeaderName string
	CookiePath string

	// CookieSecure marks the cookie Secure on every request, not only TLS or X-Forwarded-Proto: https ones.
	CookieSecure bool

	// NoHostPrefix keeps CookieName without the __Host- prefix on secure requests, for a CookiePath other than "/".
	NoHostPrefix bool

	// SecretKey signs tokens; empty uses a per-process random key, which does not survive a restart or span replicas.
	SecretKey []byte

	// AdditionalKeys are also accepted when verifying, so SecretKey can rotate; new tokens are signed with SecretKey.
	AdditionalKeys [][]byte

	// Skip allows the middleware to be bypassed for specific requests.
	Skip func(*http.Request) bool

	// FormField is the form field read when the header is missing on a form-encoded request; default "_csrf".
	FormField string

	// MaxFormBytes caps the form body read for FormField, answering 413 above it; zero or less means 1 MiB.
	MaxFormBytes int64
}

const defaultCSRFMaxFormBytes int64 = 1 << 20

type csrfTokenCtxKey struct{}

// TokenFromContext returns the CSRF token the middleware put on ctx, or "" when there is none.
func TokenFromContext(ctx context.Context) string {
	v, _ := ctx.Value(csrfTokenCtxKey{}).(string)
	return v
}

type csrfSubmissionKey struct{}

type csrfSubmission struct{ header, token string }

// VerifiedCSRFSubmission returns the header name and raw token an unsafe request passed CSRF with, or ok false when no check passed it; never render the token.
func VerifiedCSRFSubmission(ctx context.Context) (header, token string, ok bool) {
	s, ok := ctx.Value(csrfSubmissionKey{}).(csrfSubmission)
	return s.header, s.token, ok
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
			panic("csrf: autogenerating SecretKey: " + err.Error())
		}
		csrfAutoKey = k
	})
	cfg.SecretKey = csrfAutoKey
}

// CSRF returns middleware that sets a signed token cookie on safe methods and answers 403 to an unsafe method whose header or form token does not match a validly signed cookie.
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
	if cfg.CookiePath != "/" && (!cfg.NoHostPrefix || strings.HasPrefix(cfg.CookieName, "__Host-")) {
		panic(fmt.Sprintf("csrf: CookiePath %q cannot carry the __Host- prefix, which needs Path=/; set NoHostPrefix to accept a cookie a sibling subdomain can plant", cfg.CookiePath))
	}
	ensureCSRFKey(&cfg)
	resolveCookieName := func(secure bool) string {
		if cfg.CookieName != "" {
			if secure && !cfg.NoHostPrefix && !strings.HasPrefix(cfg.CookieName, "__Host-") {
				return "__Host-" + cfg.CookieName
			}
			return cfg.CookieName
		}
		if secure && !cfg.NoHostPrefix {
			return "__Host-csrf"
		}
		return "csrf_token"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if (cfg.Skip != nil && cfg.Skip(r)) || isCSPReportPost(r) {
				next.ServeHTTP(w, r)
				return
			}
			secure := cfg.CookieSecure || r.TLS != nil ||
				strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
			cookieName := resolveCookieName(secure)

			if isSafeMethod(r.Method) {
				if TokenFromContext(r.Context()) != "" {
					next.ServeHTTP(w, r)
					return
				}
				var token string
				if existing, err := r.Cookie(cookieName); err == nil &&
					verifySignedTokenAny(existing.Value, cfg.SecretKey, cfg.AdditionalKeys) {
					token = existing.Value
				} else {
					tok, err := generateSignedCSRFToken(cfg.SecretKey)
					if err != nil {
						i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
						return
					}
					token = tok
					//gofastr:allow(GOFASTR1404) the double-submit pattern requires a JS-readable cookie: the client reads this value to set the matching header, and an attacker on another origin can do neither. Secure and SameSite are set below.
					http.SetCookie(w, &http.Cookie{
						Name:     cookieName,
						Value:    tok,
						Path:     cfg.CookiePath,
						HttpOnly: false,
						Secure:   secure,
						SameSite: http.SameSiteLaxMode,
					})
				}
				ctx := context.WithValue(r.Context(), csrfTokenCtxKey{}, token)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			cookie, err := r.Cookie(cookieName)
			if err != nil {
				i18n.HTTPError(w, r, http.StatusForbidden, reasonCSRFMissingCookie)
				return
			}
			submitted := r.Header.Get(cfg.HeaderName)
			if submitted == "" && isFormContentType(r) {
				buf, err := readAndBufferCapped(r, cfg.MaxFormBytes)
				if err != nil {
					i18n.HTTPError(w, r, http.StatusRequestEntityTooLarge, reasonCSRFFormTooLarge)
					return
				}
				submitted = parseFormField(buf, r.Header.Get("Content-Type"), cfg.FormField)
				r.Body = io.NopCloser(bytes.NewReader(buf))
			}
			submitted = unmaskCSRFToken(submitted)
			if submitted == "" || subtle.ConstantTimeCompare([]byte(submitted), []byte(cookie.Value)) != 1 {
				i18n.HTTPError(w, r, http.StatusForbidden, reasonCSRFTokenMismatch)
				return
			}
			if !verifySignedTokenAny(cookie.Value, cfg.SecretKey, cfg.AdditionalKeys) {
				i18n.HTTPError(w, r, http.StatusForbidden, reasonCSRFBadSignature)
				return
			}
			ctx := context.WithValue(r.Context(), csrfSubmissionKey{}, csrfSubmission{header: cfg.HeaderName, token: submitted})
			ctx = context.WithValue(ctx, csrfTokenCtxKey{}, cookie.Value)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isCSPReportPost is a browser's violation report: it carries no token,
// and the sink changes no state.
func isCSPReportPost(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == CSPReportPath
}

func isSafeMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

func readAndBufferCapped(r *http.Request, cap int64) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	limited := io.LimitReader(r.Body, sentinelLimit(cap))
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > cap {
		return nil, errors.New("body exceeds cap")
	}
	return buf, nil
}

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

func isFormContentType(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(strings.ToLower(ct))
	return ct == "application/x-www-form-urlencoded" || ct == "multipart/form-data"
}

func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

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

func verifySignedTokenAny(value string, primary []byte, additional [][]byte) bool {
	ok := verifySignedCSRFToken(value, primary)
	for _, k := range additional {
		if len(k) == 0 {
			continue
		}
		if verifySignedCSRFToken(value, k) {
			ok = true
		}
	}
	return ok
}

func verifySignedCSRFToken(value string, secret []byte) bool {
	random, sig, ok := strings.CutLast(value, ".")
	if !ok || random == "" || sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(random))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) == 1
}

// WarnIfCSRFUnconfigured logs a warning, to slog.Default when logger is nil, if cfg has no SecretKey.
func WarnIfCSRFUnconfigured(cfg CSRFConfig, logger interface {
	Warn(msg string, args ...any)
}) {
	if len(cfg.SecretKey) > 0 {
		return
	}
	if l, ok := logger.(*slog.Logger); ok && l == nil {
		logger = nil
	}
	if logger == nil {
		logSlogWarnDefault("CSRF SecretKey not configured — auto-generated per-process key will not survive restart and will reject form submits across multi-instance deploys. Set CSRFConfig.SecretKey for production.")
		return
	}
	logger.Warn("CSRF SecretKey not configured — auto-generated per-process key will not survive restart and will reject form submits across multi-instance deploys",
		"recommendation", "set CSRFConfig.SecretKey for production")
}

// SkipBearerAuth returns a CSRFConfig.Skip predicate that skips requests carrying Authorization: Bearer or X-API-Key, never another Authorization scheme.
func SkipBearerAuth() func(*http.Request) bool {
	return func(r *http.Request) bool {
		if scheme, _, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " "); ok && strings.EqualFold(scheme, "bearer") {
			return true
		}
		if r.Header.Get("X-API-Key") != "" {
			return true
		}
		return false
	}
}
