package auth

import (
	"context"
	"html/template"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// CSRFCookieName is the cookie name battery/auth uses by default.
const CSRFCookieName = "auth_csrf"

// CSRFFormField is the hidden-input name CSRFInputHTML emits.
const CSRFFormField = "_csrf"

// CSRFHeaderName is the request header name JavaScript clients use.
const CSRFHeaderName = "X-CSRF-Token"

// CSRF returns double-submit-cookie CSRF middleware for unsafe methods, skipping bearer, embed and in-process requests.
func CSRF(opts ...CSRFOption) middleware.Middleware {
	cfg := middleware.CSRFConfig{
		CookieName: CSRFCookieName,
		HeaderName: CSRFHeaderName,
		FormField:  CSRFFormField,
		Skip:       middleware.SkipAny(middleware.SkipBearerAuth(), embed.CSRFExempt, framework.IsInProcessRequest),
	}
	for _, fn := range opts {
		fn(&cfg)
	}
	return middleware.CSRF(cfg)
}

// CSRFOption configures the auth CSRF middleware.
type CSRFOption func(*middleware.CSRFConfig)

// WithCSRFSecret sets the token HMAC key; without it a per-process random key is used.
func WithCSRFSecret(key []byte) CSRFOption {
	return func(c *middleware.CSRFConfig) { c.SecretKey = key }
}

// WithCSRFCookieSecure marks the CSRF cookie Secure.
func WithCSRFCookieSecure(secure bool) CSRFOption {
	return func(c *middleware.CSRFConfig) { c.CookieSecure = secure }
}

// WithCSRFCookieName overrides the cookie name. Defaults to "auth_csrf".
func WithCSRFCookieName(name string) CSRFOption {
	return func(c *middleware.CSRFConfig) { c.CookieName = name }
}

// WithDevCSRFKey loads or creates a stable dev HMAC key at path; use WithCSRFSecret in production.
func WithDevCSRFKey(path string) CSRFOption {
	key, err := middleware.DevCSRFKeyFromFile(path)
	if err != nil {
		panic("auth: WithDevCSRFKey: " + err.Error())
	}
	return WithCSRFSecret(key)
}

// WithCSRFSkipPaths exempts the path prefixes from CSRF, keeping the earlier skips.
func WithCSRFSkipPaths(prefixes ...string) CSRFOption {
	return func(c *middleware.CSRFConfig) {
		skipper := middleware.NewCSRFSkipper()
		skipper.Add(prefixes...)
		prior := c.Skip
		if prior == nil {
			c.Skip = skipper.Skip
			return
		}
		c.Skip = middleware.SkipAny(prior, skipper.Skip)
	}
}

// CSRFToken returns the request's CSRF token, freshly masked, from the context first and then the __Host- cookie before the bare one.
func CSRFToken(r *http.Request) string {
	if tok := middleware.TokenFromContext(r.Context()); tok != "" {
		return middleware.MaskCSRFToken(tok)
	}
	for _, name := range []string{"__Host-" + CSRFCookieName, CSRFCookieName} {
		if c, err := r.Cookie(name); err == nil && c.Value != "" {
			return middleware.MaskCSRFToken(c.Value)
		}
	}
	return ""
}

// CSRFInputHTML returns the hidden CSRF input for the request, or "" when it has no token.
func CSRFInputHTML(r *http.Request) template.HTML {
	return renderCSRFInput(CSRFToken(r))
}

// CSRFTokenFromCtx returns the masked CSRF token the middleware stashed on ctx, or "".
func CSRFTokenFromCtx(ctx context.Context) string {
	return middleware.MaskCSRFToken(middleware.TokenFromContext(ctx))
}

// CSRFInputFromCtx is CSRFInputHTML from a context.
func CSRFInputFromCtx(ctx context.Context) template.HTML {
	return renderCSRFInput(CSRFTokenFromCtx(ctx))
}

func renderCSRFInput(tok string) template.HTML {
	if tok == "" {
		return ""
	}
	return template.HTML(`<input type="hidden" name="` + CSRFFormField +
		`" value="` + template.HTMLEscapeString(tok) + `">`)
}

// crossSiteForm reports whether r is a forgeable browser form post from
// another site. The gate is handler.IsForgeableRequest, NOT the
// Content-Type alone: a form with enctype="text/plain" and a bodyless
// fetch() POST are CORS-simple, preflight-free CSRF vehicles exactly
// like a urlencoded form. handler.IsCrossSiteRequest is the repo's one
// cross-site predicate (Sec-Fetch-Site first; "same-site" falls through
// to the Origin-host compare — a sibling subdomain is same-site while
// its Origin names another host, and the SameSite session cookie still
// attaches). Non-browser clients (curl, tests, native apps) send neither
func crossSiteForm(r *http.Request) bool {
	return handler.IsForgeableRequest(r) && handler.IsCrossSiteRequest(r)
}

// RejectCrossSiteForm refuses a browser cross-site submission to a
// mutating route and reports whether it wrote a response. Batteries that
// answer with i18n.HTTPError share this copy; the CSRF middleware is an
// optional app-level add-on, so a battery that serves mutating posts
// without it must still refuse the forgeable cross-site shape itself.
func RejectCrossSiteForm(w http.ResponseWriter, r *http.Request) bool {
	if !crossSiteForm(r) {
		return false
	}
	i18n.HTTPError(w, r, http.StatusForbidden, handler.ReasonCrossSiteRefused)
	return true
}
