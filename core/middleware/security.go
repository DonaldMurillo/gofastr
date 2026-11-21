package middleware

import (
	"net/http"
	"slices"
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

	// CSPReportURI, when set, is where browsers send policy violation reports: it is named
	// CSPReportEndpoint in Reporting-Endpoints (beside any endpoints already set), each policy
	// that names neither report-to nor report-uri gains both, and the default policy gains
	// 'report-sample'.
	CSPReportURI string

	// TrustedTypesPolicies names more Trusted Types policies the default policy lets page
	// scripts create, beside TrustedTypesPolicy; "default" names the browser's catch-all one.
	TrustedTypesPolicies []string

	// NoTrustedTypes drops require-trusted-types-for and trusted-types from the default policy.
	NoTrustedTypes bool
}

// TrustedTypesPolicy is the Trusted Types policy the runtime creates; the default policy
// requires one for every HTML and script sink and allows only this name and TrustedTypesPolicies.
const TrustedTypesPolicy = "gofastr"

// SecurityHeaders sets CSP, nosniff, referrer, frame, permissions, CORP, COOP and HTTPS-only HSTS headers, each with a strict default.
func SecurityHeaders(cfg SecurityHeadersConfig) Middleware {
	csp := cfg.ContentSecurityPolicy
	reportURI := strings.TrimSpace(cfg.CSPReportURI)
	if reportURI != "" && !validCSPReportURI(reportURI) {
		panic("middleware: SecurityHeadersConfig.CSPReportURI " + strconv.Quote(reportURI) +
			" is not a bare URL: it may not hold a space, quote, comma, semicolon, backslash or control byte")
	}
	if csp == "" {
		src := "'self'"
		if reportURI != "" {
			src += " 'report-sample'"
		}
		csp = "default-src " + src + "; img-src 'self' data:; object-src 'none'; worker-src 'self'; " +
			"form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
		if !cfg.NoTrustedTypes {
			csp += "; " + TrustedTypesDirectives(cfg.TrustedTypesPolicies...)
		}
	}
	reportingEndpoints := ""
	if reportURI != "" {
		csp = withCSPReporting(csp, reportURI)
		reportingEndpoints = CSPReportEndpoint + `="` + reportURI + `"`
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
			if reportingEndpoints != "" {
				h.Set("Reporting-Endpoints", mergeReportingEndpoints(h.Get("Reporting-Endpoints"), reportingEndpoints))
			}
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

func validCSPReportURI(u string) bool {
	for i := 0; i < len(u); i++ {
		switch c := u[i]; {
		case c <= ' ' || c == 0x7f, c == '"', c == '\'', c == ',', c == ';', c == '\\':
			return false
		}
	}
	return true
}

func withCSPReporting(csp, uri string) string {
	parts := strings.Split(csp, ",")
	for i, part := range parts {
		hasTo, hasURI := false, false
		for _, d := range strings.Split(part, ";") {
			name, _, _ := strings.Cut(strings.TrimSpace(d), " ")
			switch strings.ToLower(name) {
			case "report-to":
				hasTo = true
			case "report-uri":
				hasURI = true
			}
		}
		if hasTo || hasURI {
			parts[i] = strings.TrimSpace(part)
			continue
		}
		parts[i] = strings.TrimRight(strings.TrimSpace(part), "; ") + "; report-to " + CSPReportEndpoint + "; report-uri " + uri
	}
	return strings.Join(parts, ", ")
}

// mergeReportingEndpoints adds ours to an endpoint list an outer handler set,
// unless that list already defines CSPReportEndpoint.
func mergeReportingEndpoints(existing, ours string) string {
	existing = strings.TrimSpace(existing)
	if existing == "" {
		return ours
	}
	for _, member := range strings.Split(existing, ",") {
		name, _, _ := strings.Cut(strings.TrimSpace(member), "=")
		if strings.TrimSpace(name) == CSPReportEndpoint {
			return existing
		}
	}
	return existing + ", " + ours
}

// TrustedTypesDirectives returns the default policy's Trusted Types directives,
// require-trusted-types-for 'script' and trusted-types naming TrustedTypesPolicy and
// extra; it panics on a name the trusted-types grammar refuses or on 'none'.
func TrustedTypesDirectives(extra ...string) string {
	names := []string{TrustedTypesPolicy}
	for _, n := range extra {
		if !validTrustedTypesName(n) {
			panic("middleware: Trusted Types policy name " + strconv.Quote(n) +
				" is not a policy name: use letters, digits and - # = _ / @ . %")
		}
		if !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	return "require-trusted-types-for 'script'; trusted-types " + strings.Join(names, " ")
}

func validTrustedTypesName(n string) bool {
	if n == "" {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("-#=_/@.%", c) >= 0) {
			return false
		}
	}
	return true
}
