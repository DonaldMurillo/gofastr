package auth

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework"
)

// BFFPostureConfig configures the explicit browser-backend security preset.
// AllowedOrigins must contain exact http(s) origins such as
// "https://app.example.com"; wildcards and opaque origins are rejected.
type BFFPostureConfig struct {
	AllowedOrigins []string
	CSRFOptions    []CSRFOption
	TokenPrefix    string
}

// WithBFFPosture configures cookie-only browser authentication in one option:
// JSON login responses omit JWTs, session identity is hydrated globally, CSRF
// protects cookie-authenticated mutations, and requests carrying an Origin to
// the API prefix must match the exact allowlist. API tokens with TokenPrefix
// (default gfsk_) remain available for automation; bearer JWTs are rejected
// on the BFF API surface. When no WithAPIPrefix is configured, entity CRUD
// mounts at the bare root, so the guard covers the entire app, list the
// app's own origin in AllowedOrigins in that case, or set an API prefix.
//
// This option lives in battery/auth (rather than framework) because it needs an
// AuthManager and importing the auth battery from framework would create a Go
// import cycle.
func WithBFFPosture(mgr *AuthManager, cfg BFFPostureConfig) framework.AppOption {
	if mgr == nil {
		panic("auth: WithBFFPosture requires AuthManager")
	}
	if cfg.TokenPrefix == "" {
		cfg.TokenPrefix = TokenPrefix
	}
	if !tokenPrefixPattern.MatchString(cfg.TokenPrefix) {
		panic("auth: WithBFFPosture: invalid TokenPrefix " + cfg.TokenPrefix)
	}
	origins := validateBFFOrigins(cfg.AllowedOrigins)
	csrfOpts := append([]CSRFOption(nil), cfg.CSRFOptions...)
	csrfOpts = append(csrfOpts, WithCSRFAuthRoutes(mgr))
	csrfOpts = append(csrfOpts, WithCSRFCookieSecure(true))

	return func(app *framework.App) {
		mgr.mu.Lock()
		if mgr.started {
			mgr.mu.Unlock()
			panic("auth: WithBFFPosture must be applied before AuthManager starts")
		}
		mgr.config.CookieOnly = true
		mgr.config.SessionSecure = true
		if mgr.config.SessionCookie == "session_id" {
			mgr.config.SessionCookie = "__Host-session"
		}
		mgr.mu.Unlock()

		app.Use(
			bffOriginGuard(func() string {
				// Resolve lazily so WithAPIPrefix and WithBFFPosture compose
				// in either option order. There is one API prefix, owned by
				// framework.AppConfig, rather than a duplicated security knob.
				return normalizeBFFPrefix(app.Config.APIPrefix)
			}, origins, cfg.TokenPrefix, mgr.Config().BasePath),
			CSRF(csrfOpts...),
			SessionMiddleware(mgr),
		)
	}
}

// WithCSRFAuthRoutes exempts the two auth routes that carry their own
// cross-site check from a hand-mounted auth.CSRF, as WithBFFPosture
// does: POST {BasePath}/logout, and the form-POST OAuth callback
// POST {BasePath}/oauth/{provider}/callback that Sign in with Apple
// answers on. Nothing else under BasePath is exempt.
//
//	app.Use(auth.CSRF(auth.WithCSRFAuthRoutes(mgr)))
func WithCSRFAuthRoutes(mgr *AuthManager) CSRFOption {
	if mgr == nil {
		panic("auth: WithCSRFAuthRoutes requires AuthManager")
	}
	return withCSRFSkip(func(r *http.Request) bool {
		base := mgr.Config().BasePath
		if r.URL.Path == base+"/logout" {
			return true
		}
		return r.Method == http.MethodPost && isFormPostCallback(base, r.URL.Path)
	})
}

func withCSRFSkip(skip func(*http.Request) bool) CSRFOption {
	return func(cfg *middleware.CSRFConfig) {
		if cfg.Skip == nil {
			cfg.Skip = skip
			return
		}
		cfg.Skip = middleware.SkipAny(cfg.Skip, skip)
	}
}

func validateBFFOrigins(values []string) map[string]struct{} {
	if len(values) == 0 {
		panic("auth: WithBFFPosture requires at least one exact AllowedOrigin")
	}
	out := make(map[string]struct{}, len(values))
	for _, raw := range values {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			panic("auth: WithBFFPosture AllowedOrigins must be exact http(s) origins: " + raw)
		}
		out[u.Scheme+"://"+u.Host] = struct{}{}
	}
	return out
}

func normalizeBFFPrefix(prefix string) string {
	// No APIPrefix means entity CRUD mounts at the bare root, there is
	// no delimited API surface, so the guard covers the WHOLE app.
	// Fail-closed: requests without an Origin (top-level navigations)
	// still pass, but any Origin-carrying or bearer-JWT request answers
	// to the BFF rules everywhere.
	return "/" + strings.Trim(prefix, "/")
}

func bffOriginGuard(prefix func() string, allowed map[string]struct{}, tokenPrefix, authBase string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && isFormPostCallback(authBase, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			apiPrefix := prefix()
			// "/" (no APIPrefix configured) guards every path.
			if apiPrefix != "/" &&
				r.URL.Path != apiPrefix && !strings.HasPrefix(r.URL.Path, apiPrefix+"/") {
				next.ServeHTTP(w, r)
				return
			}
			origin := r.Header.Get("Origin")
			if origin != "" {
				if _, ok := allowed[origin]; !ok {
					i18n.HTTPError(w, r, http.StatusForbidden, i18n.ReasonOriginRefused)
					return
				}
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Add("Vary", "Origin")
			}
			if token, ok := bffBearerCredential(r.Header.Get("Authorization")); ok &&
				!strings.HasPrefix(token, tokenPrefix) {
				i18n.HTTPError(w, r, http.StatusUnauthorized, reasonBearerDisabled)
				return
			}
			// Only consume real CORS preflights. Application-owned OPTIONS
			// requests without Origin + Access-Control-Request-Method continue.
			if r.Method == http.MethodOptions && origin != "" &&
				r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, Authorization")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bffBearerCredential(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", false
	}
	return parts[1], true
}
