package framework

import (
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"net/http"
)

// Re-exports of the observability middleware so callers using framework.X
// (and especially those on WithoutDefaultMiddleware who wire their own chain)
// can reach the metrics + tracing primitives without importing core/middleware
// directly. The ergonomic path is WithMetrics() / WithTracing().

type Metrics = middleware.Metrics

// NewMetrics wraps middleware.NewMetrics.
func NewMetrics() *middleware.Metrics { return middleware.NewMetrics() }

// MetricsMiddleware wraps middleware.MetricsMiddleware.
func MetricsMiddleware(m *middleware.Metrics) middleware.Middleware {
	return middleware.MetricsMiddleware(m)
}

// MetricsHandler wraps middleware.MetricsHandler.
func MetricsHandler(m *middleware.Metrics) http.Handler { return middleware.MetricsHandler(m) }

// Tracing wraps middleware.Tracing.
func Tracing() middleware.Middleware { return middleware.Tracing() }
