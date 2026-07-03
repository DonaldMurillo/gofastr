package middleware

import (
	"net/http"
	"strconv"
	"strings"
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

	// HSTSMaxAge sets the Strict-Transport-Security max-age. Zero
	// (default) emits ONE YEAR (31536000) — HSTS-by-default matches the
	// rest of the header set; forgetting it used to be the #1 production
	// gap. Set -1 to disable the header entirely. The header is only
	// emitted when the request is actually HTTPS: direct TLS, an
	// X-Forwarded-Proto: https from a TLS-terminating proxy, or
	// Secure: true — plain-HTTP local dev never sees it.
	HSTSMaxAge     int
	HSTSIncludeSub bool
	HSTSPreload    bool

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
	hstsMaxAge := cfg.HSTSMaxAge
	if hstsMaxAge == 0 {
		hstsMaxAge = 31536000 // 1 year; -1 opts out
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

			// HSTS — only emit over HTTPS: direct TLS, a TLS-terminating
			// proxy's X-Forwarded-Proto, or an explicit Secure: true.
			// (A client spoofing X-Forwarded-Proto only affects its own
			// response — HSTS binds per received response.)
			secure := cfg.Secure
			if !secure && r.TLS != nil {
				secure = true // auto-detect TLS
			}
			if !secure && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
				secure = true // TLS terminated upstream
			}
			if secure && hstsMaxAge > 0 {
				val := "max-age=" + strconv.Itoa(hstsMaxAge)
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
