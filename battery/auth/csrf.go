package auth

import (
	"html/template"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// CSRFCookieName is the cookie name battery/auth uses by default.
// Override via WithCSRF(WithCSRFCookieName(...)).
const CSRFCookieName = "auth_csrf"

// CSRFFormField is the hidden-input name CSRFInputHTML emits.
const CSRFFormField = "_csrf"

// CSRFHeaderName is the request header name JavaScript clients use.
const CSRFHeaderName = "X-CSRF-Token"

// CSRF returns a Middleware that enforces double-submit-cookie CSRF
// protection on unsafe HTTP methods (POST/PUT/PATCH/DELETE).
//
// Behaviour:
//   - GET/HEAD/OPTIONS — sets the token cookie if missing; lets request through.
//   - POST/PUT/PATCH/DELETE — verifies the request header X-CSRF-Token
//     (JSON path) OR a hidden _csrf form field (HTML form path) matches
//     the cookie value AND has a valid HMAC signature.
//   - Bearer-token / X-API-Key requests are skipped (not cookie-based).
//
// Auth defaults: cookie name "auth_csrf", header X-CSRF-Token, form
// field "_csrf". Pair with CSRFInputHTML in your templates.
//
// Mount on the app router (or on a route group) BEFORE any form-POST
// route that needs protection:
//
//	app.Use(auth.CSRF())
//	// or, with a custom secret:
//	app.Use(auth.CSRF(auth.WithCSRFSecret(secret)))
func CSRF(opts ...CSRFOption) middleware.Middleware {
	cfg := middleware.CSRFConfig{
		CookieName: CSRFCookieName,
		HeaderName: CSRFHeaderName,
		FormField:  CSRFFormField,
		Skip:       middleware.SkipBearerAuth(),
	}
	for _, fn := range opts {
		fn(&cfg)
	}
	// Promote default cookie name to __Host-* when running secure.
	// The __Host- prefix gives the browser a hard guarantee against
	// sibling-subdomain cookie injection (Path=/, Secure, no Domain
	// required, which we satisfy). A host that overrode CookieName
	// via WithCSRFCookieName explicitly keeps their override.
	if cfg.CookieSecure && cfg.CookieName == CSRFCookieName {
		cfg.CookieName = "__Host-" + CSRFCookieName
	}
	return middleware.CSRF(cfg)
}

// CSRFOption configures the auth CSRF middleware.
type CSRFOption func(*middleware.CSRFConfig)

// WithCSRFSecret sets the HMAC key used to sign tokens. Recommended in
// production; without it a per-process random key is generated and
// rotates on every restart (invalidating all in-flight forms).
func WithCSRFSecret(key []byte) CSRFOption {
	return func(c *middleware.CSRFConfig) { c.SecretKey = key }
}

// WithCSRFCookieSecure marks the CSRF cookie Secure. Pair with HTTPS in
// production; leave false for local HTTP dev.
func WithCSRFCookieSecure(secure bool) CSRFOption {
	return func(c *middleware.CSRFConfig) { c.CookieSecure = secure }
}

// WithCSRFCookieName overrides the cookie name. Defaults to "auth_csrf".
func WithCSRFCookieName(name string) CSRFOption {
	return func(c *middleware.CSRFConfig) { c.CookieName = name }
}

// CSRFToken returns the current request's CSRF token. Prefers the
// value stashed on ctx by middleware.CSRF (which works on the SAME
// request that minted the cookie); falls back to reading the cookie
// (for follow-up requests where the cookie is already in r.Cookies()).
// Returns "" when no token is available — typically because the
// route isn't behind CSRF middleware.
func CSRFToken(r *http.Request) string {
	if tok := middleware.TokenFromContext(r.Context()); tok != "" {
		return tok
	}
	// Also try the __Host- prefixed variant when running secure.
	for _, name := range []string{CSRFCookieName, "__Host-" + CSRFCookieName} {
		if c, err := r.Cookie(name); err == nil && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// CSRFInputHTML returns the hidden <input> markup callers embed inside
// every HTML form they submit through the framework runtime. Safe for
// template/html — returns template.HTML so it isn't double-escaped.
//
//	<form method="POST" action="/save" data-fui-native>
//	  {{ auth.CSRFInputHTML .Request }}
//	  …
//	</form>
//
// When no token cookie is present yet, CSRFInputHTML returns "" — the
// next safe-method request to a CSRF-protected route will set the
// cookie, so the typical page flow lands the token before any form
// renders.
func CSRFInputHTML(r *http.Request) template.HTML {
	tok := CSRFToken(r)
	if tok == "" {
		return ""
	}
	// tok is base64url + "." + base64url(HMAC) — no HTML metachars need
	// escaping. EscapeString is still applied as defense in depth.
	return template.HTML(`<input type="hidden" name="` + CSRFFormField +
		`" value="` + template.HTMLEscapeString(tok) + `">`)
}
