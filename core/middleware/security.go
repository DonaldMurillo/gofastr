package middleware

import "net/http"

// SecurityHeadersConfig controls defensive HTTP response headers.
type SecurityHeadersConfig struct {
	ContentSecurityPolicy string
	ReferrerPolicy        string
	FrameOptions          string
	PermissionsPolicy     string
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

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", referrer)
			h.Set("X-Frame-Options", frameOptions)
			h.Set("Permissions-Policy", permissions)
			next.ServeHTTP(w, r)
		})
	}
}
