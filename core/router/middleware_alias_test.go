package router_test

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// TestMiddlewareTypeAlias pins that router.Middleware is a type ALIAS
// (not a distinct named type) for middleware.Middleware. The aliasing
// is what lets battery/auth (which returns middleware.Middleware) flow
// into router.Use directly without a wrapper cast.
func TestMiddlewareTypeAlias(t *testing.T) {
	// A value of middleware.Middleware must satisfy router.Middleware
	// without a conversion.
	var mw middleware.Middleware = func(next http.Handler) http.Handler { return next }
	var asRouter router.Middleware = mw // compile-time check
	_ = asRouter

	// And vice versa.
	var rmw router.Middleware = func(next http.Handler) http.Handler { return next }
	var asCore middleware.Middleware = rmw // compile-time check
	_ = asCore

	// Router.Use should accept a middleware.Middleware verbatim.
	r := router.New()
	r.Use(mw)
}
