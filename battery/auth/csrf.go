package auth

import (
	"context"
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

// WithDevCSRFKey loads (or creates+writes) a stable HMAC key from path
// and uses it as SecretKey. Intended for dev only — survives process
// restarts so browsers don't 403 on the next form submit after every
// dev-server reload (V3 #5). In production, use WithCSRFSecret with a
// value sourced from your secret manager.
//
// Returns an option that PANICS on key-load failure: in a dev startup
// path, failing loudly is the right behavior (silently falling back
// to the auto-key reintroduces exactly the UX problem this option
// exists to solve).
func WithDevCSRFKey(path string) CSRFOption {
	key, err := middleware.DevCSRFKeyFromFile(path)
	if err != nil {
		panic("auth: WithDevCSRFKey: " + err.Error())
	}
	return WithCSRFSecret(key)
}

// WithCSRFSkipPaths exempts the given path prefixes from CSRF enforcement
// alongside the default SkipBearerAuth predicate. Use for webhook
// endpoints authenticated by signature, health checks, and similar
// non-cookie surfaces. Without this option, hosts have to write a Skip
// closure that inspects r.URL.Path manually (V3 #9 friction):
//
//	app.Use(auth.CSRF(auth.WithCSRFSkipPaths("/webhooks/", "/health")))
//
// Path matching is literal string-prefix — see middleware.CSRFSkipper.
func WithCSRFSkipPaths(prefixes ...string) CSRFOption {
	return func(c *middleware.CSRFConfig) {
		skipper := middleware.NewCSRFSkipper()
		skipper.Add(prefixes...)
		// Preserve the prior Skip (the default is SkipBearerAuth, but the
		// caller might have layered something via a previous option).
		prior := c.Skip
		if prior == nil {
			c.Skip = skipper.Skip
			return
		}
		c.Skip = middleware.SkipAny(prior, skipper.Skip)
	}
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
//	<form method="POST" action="/save">
//	  {{ auth.CSRFInputHTML .Request }}
//	  …
//	</form>
//
// When no token cookie is present yet, CSRFInputHTML returns "" — the
// next safe-method request to a CSRF-protected route will set the
// cookie, so the typical page flow lands the token before any form
// renders.
//
// Most callers should prefer CSRFInputFromCtx for code paths that only
// have a context.Context (e.g. core-ui screens). They share the same
// renderer underneath.
func CSRFInputHTML(r *http.Request) template.HTML {
	return renderCSRFInput(CSRFToken(r))
}

// CSRFTokenFromCtx returns the request's CSRF token from a context. It
// reads the value the CSRF middleware stashes on ctx via
// middleware.TokenFromContext — works on the SAME request that minted
// the cookie, where the cookie isn't in r.Cookies() yet. Returns "" if
// the route isn't behind CSRF middleware or no token is available.
//
// Use this from any code path that holds only a context.Context (e.g.
// core-ui Screen.Load, Screen.Render) rather than threading a
// *http.Request through.
func CSRFTokenFromCtx(ctx context.Context) string {
	return middleware.TokenFromContext(ctx)
}

// CSRFInputFromCtx is the ctx-based counterpart to CSRFInputHTML — same
// hidden-input markup, derived from middleware.TokenFromContext. Use
// from core-ui Screen.Render where only ctx is in scope:
//
//	func (s *EditScreen) Render() render.HTML {
//	    return render.Join(
//	        render.Raw(string(auth.CSRFInputFromCtx(s.ctx))),
//	        ...,
//	    )
//	}
//
// Returns "" when no token is available (route not gated by CSRF).
func CSRFInputFromCtx(ctx context.Context) template.HTML {
	return renderCSRFInput(CSRFTokenFromCtx(ctx))
}

// renderCSRFInput builds the hidden-input markup for the given token.
// Returns "" when tok is empty so callers can pass an unknown-token
// path without conditionals.
func renderCSRFInput(tok string) template.HTML {
	if tok == "" {
		return ""
	}
	// tok is base64url + "." + base64url(HMAC) — no HTML metachars need
	// escaping. EscapeString is still applied as defense in depth.
	return template.HTML(`<input type="hidden" name="` + CSRFFormField +
		`" value="` + template.HTMLEscapeString(tok) + `">`)
}
