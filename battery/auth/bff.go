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
type BFFPostureConfig struct {
	AllowedOrigins []string
	CSRFOptions    []CSRFOption
	TokenPrefix    string
}

// WithBFFPosture configures cookie-only browser authentication in one option:
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
				return normalizeBFFPrefix(app.Config.APIPrefix)
			}, origins, cfg.TokenPrefix, mgr.Config().BasePath),
			CSRF(csrfOpts...),
			SessionMiddleware(mgr),
		)
	}
}

// WithCSRFAuthRoutes exempts POST {BasePath}/logout and the form-post OAuth callback from a hand-mounted CSRF, as WithBFFPosture does.
func WithCSRFAuthRoutes(mgr *AuthManager) CSRFOption {
	if mgr == nil {
		panic("auth: WithCSRFAuthRoutes requires AuthManager")
	}
	return withCSRFSkip(func(r *http.Request) bool {
		if r.Method != http.MethodPost {
			return false
		}
		base := mgr.Config().BasePath
		return r.URL.Path == base+"/logout" || isFormPostCallback(base, r.URL.Path)
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
