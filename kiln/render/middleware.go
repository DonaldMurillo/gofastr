package render

import (
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// middlewareBuilder produces a router.Middleware from declarative config.
type middlewareBuilder func(cfg map[string]any) (router.Middleware, error)

// middlewareCatalog is the closed set of middlewares the world IR can
// reference. Keep small and additive; the agent surfaces unknown names
// as an error rather than silently dropping them.
var middlewareCatalog = map[string]middlewareBuilder{
	"recover": buildRecover,
}

// buildRecover is core/middleware's Recovery: a panic before the
// response started answers 500, one after it cuts the connection rather
// than splicing error text onto the partial body, and the panic is
// logged. The catalog's own recover wrote http.Error unconditionally.
func buildRecover(_ map[string]any) (router.Middleware, error) {
	return middleware.Recovery(), nil
}
