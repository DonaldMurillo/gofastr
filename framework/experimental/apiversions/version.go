// Package apiversions provides first-class API versioning built on top of
// route groups.
//
// Multiple versions of the same entity coexist; deprecations are explicit
// and machine-readable. The default versioning scheme is URL prefix
// ("/v1", "/v2"), implemented via route groups.
//
// Usage:
//
//	v1 := apiversions.Version(app, "v1")
//	v1.Entity("orders", ordersConfigV1)
//
//	v2 := apiversions.Version(app, "v2")
//	v2.Entity("orders", ordersConfigV2)
//	apiversions.Deprecate(v2.Router(), "/v1/orders", "2026-06-01", "/v2/orders")
package apiversions

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

// validVersionRe accepts canonicalised version prefixes: "v" followed by
// a numeric major (and optional .minor). Anything outside this shape —
// path separators, query glue, empty string — is rejected at Version()
// time so misuse can't smuggle a /v1/admin prefix into the router.
var validVersionRe = regexp.MustCompile(`^v[0-9]+(\.[0-9]+)?$`)

// VersionPrefix is the default URL prefix format. "/v1", "/v2", etc.
type VersionPrefix string

// APIVersion represents an API version mounted at a URL prefix.
type APIVersion struct {
	prefix  string
	group   *routegroup.RouteGroup
	sunset  *time.Time
	replace *string
}

// VersionOption configures a Version.
type VersionOption func(*APIVersion)

// Version creates a new API version bound to the given prefix (e.g. "v1").
// It creates a route group at "/v1" with MCP namespacing set to "v1".
func Version(appRouter *router.Router, prefix string, opts ...VersionOption) *APIVersion {
	prefix = normalizeVersion(prefix)
	group := routegroup.New(appRouter, "/"+prefix,
		routegroup.WithMCPNamespace(prefix),
		routegroup.WithOpenAPITag(prefix),
	)
	v := &APIVersion{
		prefix: prefix,
		group:  group,
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// WithDeprecation marks this version as deprecated with a sunset date
// and a link to the replacement version.
func WithDeprecation(sunsetDate time.Time, replacementURL string) VersionOption {
	return func(v *APIVersion) {
		v.sunset = &sunsetDate
		v.replace = &replacementURL
	}
}

// Router returns the version's route group router.
func (v *APIVersion) Router() *router.Router {
	return v.group.Router()
}

// Group returns the underlying route group.
func (v *APIVersion) Group() *routegroup.RouteGroup {
	return v.group
}

// Prefix returns the version prefix (e.g. "v1").
func (v *APIVersion) Prefix() string {
	return v.prefix
}

// FullPrefix returns the full URL path (e.g. "/v1").
func (v *APIVersion) FullPrefix() string {
	return "/" + v.prefix
}

// IsDeprecated returns true if this version has been marked deprecated.
func (v *APIVersion) IsDeprecated() bool {
	return v.sunset != nil
}

// DeprecationMiddleware returns middleware that adds deprecation headers
// to every response from this version. Headers:
//
//	Deprecation: true
//	Sunset: <RFC 1123 date>
//	Link: <replacementURL>; rel="successor-version"
//
// Unsafe replacement URLs (non-http(s) schemes, embedded CR/LF) are
// dropped silently — the Link header is a clickable hint to API clients
// and a `javascript:` / `mailto:` / data: there is a phishing primitive.
func (v *APIVersion) DeprecationMiddleware() router.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Deprecation", "true")
			if v.sunset != nil {
				w.Header().Set("Sunset", v.sunset.Format(time.RFC1123))
			}
			if v.replace != nil {
				if safe, ok := safeReplacementURL(*v.replace); ok {
					w.Header().Set("Link", "<"+safe+">; rel=\"successor-version\"")
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Use adds middleware to the version's route group.
func (v *APIVersion) Use(mw ...router.Middleware) {
	v.group.Use(mw...)
}

// normalizeVersion strips any leading "/" or "v" from the prefix
// and returns the clean form (e.g. "/v1" → "v1", "1" → "v1").
// Panics with a clear message if the result is not a valid version
// identifier — apps must catch this at startup, not in the request path.
func normalizeVersion(prefix string) string {
	prefix = strings.TrimPrefix(prefix, "/")
	if !strings.HasPrefix(prefix, "v") {
		prefix = "v" + prefix
	}
	if !validVersionRe.MatchString(prefix) {
		panic(fmt.Sprintf("apiversions: invalid version prefix %q (want v<major> or v<major>.<minor>)", prefix))
	}
	return prefix
}

// DeprecationHeaders is a helper that writes deprecation headers onto a
// response. Useful when you need to mark individual endpoints as deprecated
// rather than a whole version. Unsafe replacement URLs are dropped — see
// [APIVersion.DeprecationMiddleware] for the policy.
func DeprecationHeaders(w http.ResponseWriter, sunset time.Time, replacement string) {
	w.Header().Set("Deprecation", "true")
	if !sunset.IsZero() {
		w.Header().Set("Sunset", sunset.Format(time.RFC1123))
	}
	if replacement != "" {
		if safe, ok := safeReplacementURL(replacement); ok {
			w.Header().Set("Link", "<"+safe+">; rel=\"successor-version\"")
		}
	}
}

// safeReplacementURL returns the cleaned replacement URL and true when
// it is safe to expose in a Link header. Allowed shapes: relative paths
// ("/v2", "./v2") and absolute http(s) URLs. CR/LF/NUL anywhere in the
// string disqualifies it (header smuggling). Other schemes like
// `javascript:`, `data:`, `file:`, `mailto:`, `view-source:` are dropped
// — they would render as clickable in API clients and turn the
// deprecation hint into a phishing vector. Protocol-relative URLs
// ("//other.example") are dropped as ambiguous about origin trust.
func safeReplacementURL(u string) (string, bool) {
	if u == "" {
		return "", false
	}
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c < 0x20 || c == 0x7f {
			return "", false
		}
	}
	if strings.HasPrefix(u, "//") {
		return "", false
	}
	// Percent-encoded CR/LF still smuggles a line when consumers decode.
	low := strings.ToLower(u)
	if strings.Contains(low, "%0d") || strings.Contains(low, "%0a") {
		return "", false
	}
	if strings.HasPrefix(u, "/") || strings.HasPrefix(u, "./") || strings.HasPrefix(u, "../") {
		return u, true
	}
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c == ':' {
			scheme := strings.ToLower(u[:i])
			switch scheme {
			case "http", "https":
				return u, true
			default:
				return "", false
			}
		}
		if c == '/' || c == '?' || c == '#' {
			// No scheme delimiter before path-ish char — relative path.
			return u, true
		}
	}
	return u, true
}
