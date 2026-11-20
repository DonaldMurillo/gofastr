// Package apiversions mounts each API version as a route group at a URL
// prefix (/v1, /v2) and marks old versions deprecated.
package apiversions

import (
	"fmt"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

var validVersionRe = regexp.MustCompile(`^v[0-9]+(\.[0-9]+)?$`)

// VersionPrefix is a version's URL prefix, such as "v1".
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

// Version mounts a route group at "/"+prefix, with that prefix as its MCP
// namespace and OpenAPI tag. "1", "/v1" and "v1" all name v1; any other
// shape than v<major> or v<major>.<minor> panics.
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

// WithDeprecation marks the version deprecated, with its sunset date and successor URL.
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

// DeprecationMiddleware returns middleware that sets Deprecation: true,
// Sunset (an HTTP-date) and a successor-version Link on every response
// from this version. An unsafe replacement URL writes no Link header.
func (v *APIVersion) DeprecationMiddleware() router.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Deprecation", "true")
			if v.sunset != nil {
				w.Header().Set("Sunset", httpDate(*v.sunset))
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

// DeprecationHeaders writes the headers of [APIVersion.DeprecationMiddleware]
// onto one response, for a single deprecated endpoint.
func DeprecationHeaders(w http.ResponseWriter, sunset time.Time, replacement string) {
	w.Header().Set("Deprecation", "true")
	if !sunset.IsZero() {
		w.Header().Set("Sunset", httpDate(sunset))
	}
	if replacement != "" {
		if safe, ok := safeReplacementURL(replacement); ok {
			w.Header().Set("Link", "<"+safe+">; rel=\"successor-version\"")
		}
	}
}

func safeReplacementURL(u string) (string, bool) {
	if !urlsafe.OK(u, urlsafe.Resource) {
		return "", false
	}
	return u, true
}

func httpDate(t time.Time) string {
	return t.UTC().Format(http.TimeFormat)
}
