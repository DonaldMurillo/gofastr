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

	// CrossOriginResourcePolicy defaults to "same-origin"; "cross-origin" opts out.
	CrossOriginResourcePolicy string

	// CrossOriginOpenerPolicy defaults to "same-origin"; "unsafe-none" opts out.
	CrossOriginOpenerPolicy string

	// HSTSMaxAge is the Strict-Transport-Security max-age: zero means one year, -1 disables the header.
	HSTSMaxAge     int
	HSTSIncludeSub bool
	HSTSPreload    bool

	// Secure sends HSTS on plain HTTP too; false keeps auto-detection (TLS or X-Forwarded-Proto: https), and HSTSMaxAge: -1 is the off switch.
	Secure bool
}

// SecurityHeaders sets CSP, nosniff, referrer, frame, permissions, CORP, COOP and HTTPS-only HSTS headers, each with a strict default.
func SecurityHeaders(cfg SecurityHeadersConfig) Middleware {
	csp := cfg.ContentSecurityPolicy
	if csp == "" {
		csp = "default-src 'self'; img-src 'self' data:; object-src 'none'; " +
			"form-action 'self'; frame-ancestors 'none'; base-uri 'self'"
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
		hstsMaxAge = 31536000
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
			h.Set("X-XSS-Protection", "0")

			secure := cfg.Secure
			if !secure && r.TLS != nil {
				secure = true
			}
			if !secure && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
				secure = true
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
