package framework

import "github.com/DonaldMurillo/gofastr/core/middleware"

// Re-exports of the observability middleware so callers using framework.X
// (and especially those on WithoutDefaultMiddleware who wire their own chain)
// can reach the metrics + tracing primitives without importing core/middleware
// directly. The ergonomic path is WithMetrics() / WithTracing().

type Metrics = middleware.Metrics

var (
	NewMetrics        = middleware.NewMetrics
	MetricsMiddleware = middleware.MetricsMiddleware
	MetricsHandler    = middleware.MetricsHandler
	Tracing           = middleware.Tracing
)
