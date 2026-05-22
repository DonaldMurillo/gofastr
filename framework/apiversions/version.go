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
	"net/http"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

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
func (v *APIVersion) DeprecationMiddleware() router.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Deprecation", "true")
			if v.sunset != nil {
				w.Header().Set("Sunset", v.sunset.Format(time.RFC1123))
			}
			if v.replace != nil {
				w.Header().Set("Link", "<"+*v.replace+">; rel=\"successor-version\"")
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
func normalizeVersion(prefix string) string {
	prefix = strings.TrimPrefix(prefix, "/")
	if !strings.HasPrefix(prefix, "v") {
		prefix = "v" + prefix
	}
	return prefix
}

// DeprecationHeaders is a helper that writes deprecation headers onto a
// response. Useful when you need to mark individual endpoints as deprecated
// rather than a whole version.
func DeprecationHeaders(w http.ResponseWriter, sunset time.Time, replacement string) {
	w.Header().Set("Deprecation", "true")
	if !sunset.IsZero() {
		w.Header().Set("Sunset", sunset.Format(time.RFC1123))
	}
	if replacement != "" {
		w.Header().Set("Link", "<"+replacement+">; rel=\"successor-version\"")
	}
}
