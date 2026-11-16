package framework

import (
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"net/http"
)

// Root spellings of core/middleware's metrics and tracing.

type Metrics = middleware.Metrics

func NewMetrics() *middleware.Metrics { return middleware.NewMetrics() }

func MetricsMiddleware(m *middleware.Metrics) middleware.Middleware {
	return middleware.MetricsMiddleware(m)
}

func MetricsHandler(m *middleware.Metrics) http.Handler { return middleware.MetricsHandler(m) }

func Tracing() middleware.Middleware { return middleware.Tracing() }
