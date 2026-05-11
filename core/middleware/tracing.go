package middleware

import (
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Tracing returns Middleware that opens an OpenTelemetry span around every
// request. Span name is "HTTP {method} {route}" where route is the matched
// pattern (r.Pattern from Go 1.22+ ServeMux). Attributes recorded:
//
//	http.method     — request method
//	http.route      — the route pattern (fallback "unmatched")
//	http.status_code — final response status
//	http.target     — request URL path
//
// Distributed-trace context is extracted from W3C traceparent / tracestate
// headers so spans from upstream services chain correctly. Outgoing
// responses get the corresponding headers injected if a propagator is
// configured.
//
// Without a configured TracerProvider (the otel default), this middleware
// is essentially a no-op — spans are created against a no-op tracer that
// drops everything. Callers wire up Jaeger / OTLP / etc. via the standard
// otel.SetTracerProvider.
func Tracing() Middleware {
	tracer := otel.Tracer("github.com/gofastr/gofastr/core/middleware")
	// Always use W3C TraceContext + Baggage as the propagator. Apps that
	// want a custom propagator should wire one before adding this middleware
	// and we'll honour the global; the composite below is the canonical
	// HTTP default and matches what otelhttp uses.
	prop := otel.GetTextMapPropagator()
	if prop == nil {
		prop = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	}
	// If the global propagator is the no-op default, swap to W3C so traces
	// propagate without requiring apps to call otel.SetTextMapPropagator.
	// The no-op's zero value extracts nothing.
	wcAware := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract incoming trace context with W3C-aware propagator so a
			// traceparent on the request joins us to the upstream trace.
			ctx := wcAware.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// r.Pattern isn't populated until after the mux matches, so we
			// start the span with a placeholder name + "unmatched" route and
			// update both after the handler returns.
			ctx, span := tracer.Start(ctx, fmt.Sprintf("HTTP %s", r.Method),
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.target", r.URL.Path),
				),
			)
			defer span.End()

			ww := &tracingResponseWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(ww, r.WithContext(ctx))

			// After the handler runs, r.Pattern is populated by the mux.
			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			span.SetName(fmt.Sprintf("HTTP %s %s", r.Method, route))
			span.SetAttributes(
				attribute.String("http.route", route),
				attribute.Int("http.status_code", ww.status),
			)
			if ww.status >= 500 {
				span.SetStatus(codes.Error, http.StatusText(ww.status))
			}

			// Inject our span context into outgoing response headers.
			prop.Inject(ctx, propagation.HeaderCarrier(ww.Header()))
		})
	}
}

// SpanFromRequest is a thin convenience that returns the active span on the
// request's context. Returns a no-op span if Tracing() isn't installed.
func SpanFromRequest(r *http.Request) trace.Span {
	return trace.SpanFromContext(r.Context())
}

// tracingResponseWriter mirrors the metrics wrapper: captures the status
// code and forwards Flush() so SSE / chunked handlers keep working.
type tracingResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *tracingResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *tracingResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *tracingResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
