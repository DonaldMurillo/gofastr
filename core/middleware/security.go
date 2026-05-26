package middleware

import (
	"net/http"
	"strconv"
)

// SecurityHeadersConfig controls defensive HTTP response headers.
type SecurityHeadersConfig struct {
	ContentSecurityPolicy string
	ReferrerPolicy        string
	FrameOptions          string
	PermissionsPolicy     string

	// CrossOriginResourcePolicy controls the Cross-Origin-Resource-Policy
	// header. Defaults to "same-origin" — Spectre-style cross-origin
	// reads of this resource are blocked. Set to "cross-origin" to opt
	// out for CDN-style assets.
	CrossOriginResourcePolicy string

	// CrossOriginOpenerPolicy controls the Cross-Origin-Opener-Policy
	// header. Defaults to "same-origin" — the document gets a fresh
	// browsing context group, blocking cross-origin window references
	// and the broader XS-Leaks class. Set to "unsafe-none" to opt out
	// when you intentionally interact with cross-origin windows.
	CrossOriginOpenerPolicy string

	// HSTS enables the Strict-Transport-Security header.
	// When set (non-zero), browsers will only use HTTPS for this duration.
	// Requires HTTPS to be active; the header is silently skipped on plain HTTP.
	// Recommended: 31536000 seconds (1 year) for production.
	// Only takes effect when Secure is true.
	HSTSMaxAge    int
	HSTSIncludeSub bool
	HSTSPreload   bool

	// Secure indicates whether the connection is over HTTPS.
	// When true AND HSTSMaxAge > 0, the Strict-Transport-Security header is added.
	// Defaults to true.
	Secure bool
}

// SecurityHeaders adds conservative browser security headers.
//
// The default Content-Security-Policy is strict (default-src 'self', no
// 'unsafe-inline'). The framework's UI host renders pages with all CSS
// and scripts as external resources under /__gofastr/* so they comply
// out of the box. img-src additionally allows data: so embedded
// data-URI images (icons, base64 placeholders) work.
func SecurityHeaders(cfg SecurityHeadersConfig) Middleware {
	csp := cfg.ContentSecurityPolicy
	if csp == "" {
		csp = "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'"
	}
	referrer := cfg.ReferrerPolicy
	if referrer == "" {
		referrer = "no-referrer"
	}
	frameOptions := cfg.FrameOptions
	if frameOptions == "" {
		frameOptions = "DENY"
	}
	permissions := cfg.PermissionsPolicy
	if permissions == "" {
		permissions = "geolocation=(), microphone=(), camera=()"
	}
	corp := cfg.CrossOriginResourcePolicy
	if corp == "" {
		corp = "same-origin"
	}
	coop := cfg.CrossOriginOpenerPolicy
	if coop == "" {
		coop = "same-origin"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", referrer)
			h.Set("X-Frame-Options", frameOptions)
			h.Set("Permissions-Policy", permissions)
			h.Set("Cross-Origin-Resource-Policy", corp)
			h.Set("Cross-Origin-Opener-Policy", coop)
			h.Set("X-XSS-Protection", "0") // disabled per modern guidance (CSP supersedes it)

			// HSTS — only emit over HTTPS. When HSTSMaxAge is set and
			// Secure is true (default), add the Strict-Transport-Security header.
			secure := cfg.Secure
			if !secure && r.TLS != nil {
				secure = true // auto-detect TLS
			}
			if secure && cfg.HSTSMaxAge > 0 {
				val := "max-age=" + strconv.Itoa(cfg.HSTSMaxAge)
				if cfg.HSTSIncludeSub {
					val += "; includeSubDomains"
				}
				if cfg.HSTSPreload {
					val += "; preload"
				}
				h.Set("Strict-Transport-Security", val)
			}

			next.ServeHTTP(w, r)
		})
	}
}
