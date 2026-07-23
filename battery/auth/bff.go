package auth

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework"
)

// BFFPostureConfig configures the explicit browser-backend security preset.
// AllowedOrigins must contain exact http(s) origins such as
// "https://app.example.com"; wildcards and opaque origins are rejected.
type BFFPostureConfig struct {
	AllowedOrigins []string
	CSRFOptions    []CSRFOption
}

// WithBFFPosture configures cookie-only browser authentication in one option:
// JSON login responses omit JWTs, session identity is hydrated globally, CSRF
// protects cookie-authenticated mutations, and requests carrying an Origin to
// the API prefix must match the exact allowlist. API tokens with the gfsk_
// prefix remain available for non-browser automation; bearer JWTs are rejected
// on the BFF API surface. When no WithAPIPrefix is configured, entity CRUD
// mounts at the bare root, so the guard covers the entire app — list the
// app's own origin in AllowedOrigins in that case, or set an API prefix.
//
// This option lives in battery/auth (rather than framework) because it needs an
// AuthManager and importing the auth battery from framework would create a Go
// import cycle.
func WithBFFPosture(mgr *AuthManager, cfg BFFPostureConfig) framework.AppOption {
	if mgr == nil {
		panic("auth: WithBFFPosture requires AuthManager")
	}
	origins := validateBFFOrigins(cfg.AllowedOrigins)
	csrfOpts := append([]CSRFOption(nil), cfg.CSRFOptions...)
	// CorePlugin's logout handler performs its own same-origin request check.
	// Exempting exactly that route keeps the static ui.SignOut form usable
	// without accidentally exempting a future /logout-all sibling.
	csrfOpts = append(csrfOpts, withCSRFSkipExactPath(mgr.Config().BasePath+"/logout"))
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
			}, origins),
			CSRF(csrfOpts...),
			SessionMiddleware(mgr),
		)
	}
}

func withCSRFSkipExactPath(path string) CSRFOption {
	return func(cfg *middleware.CSRFConfig) {
		exact := func(r *http.Request) bool { return r.URL.Path == path }
		if cfg.Skip == nil {
			cfg.Skip = exact
			return
		}
		cfg.Skip = middleware.SkipAny(cfg.Skip, exact)
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
	// No APIPrefix means entity CRUD mounts at the bare root — there is
	// no delimited API surface, so the guard covers the WHOLE app.
	// Fail-closed: requests without an Origin (top-level navigations)
	// still pass, but any Origin-carrying or bearer-JWT request answers
	// to the BFF rules everywhere.
	return "/" + strings.Trim(prefix, "/")
}

func bffOriginGuard(prefix func() string, allowed map[string]struct{}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
					http.Error(w, "origin not allowed", http.StatusForbidden)
					return
				}
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Add("Vary", "Origin")
			}
			if token, ok := bffBearerCredential(r.Header.Get("Authorization")); ok &&
				!strings.HasPrefix(token, TokenPrefix) {
				http.Error(w, "bearer JWTs are disabled for the BFF API", http.StatusUnauthorized)
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
