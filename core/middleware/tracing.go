package middleware

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Tracing returns Middleware that opens an OpenTelemetry span around every
// request. Span name is "HTTP {method} {route}" where route is the matched
// pattern (r.Pattern from Go 1.22+ ServeMux). Attributes recorded:
//
//	http.method:      request method
//	http.route:       the route pattern (fallback "unmatched")
//	http.status_code: final response status
//	http.target:      request URL path
//
// Distributed-trace context is extracted with the global otel propagator
// (otel.SetTextMapPropagator) when one is configured, so a B3 or Jaeger
// header from an upstream service chains correctly. When that propagator
// finds no remote span context, or none is configured, the W3C
// traceparent / tracestate and baggage headers are read instead. Outgoing
// responses get the global propagator's headers injected.
//
// Without a configured TracerProvider (the otel default), this middleware
// is essentially a no-op; spans are created against a no-op tracer that
// drops everything. Callers wire up Jaeger / OTLP / etc. via the standard
// otel.SetTracerProvider.
func Tracing() Middleware {
	tracer := otel.Tracer("github.com/DonaldMurillo/gofastr/core/middleware")
	// The global propagator injects, and extracts when one is configured.
	// Before otel.SetTextMapPropagator runs, the global is a delegate that
	// forwards to whatever is set later, so the check for "configured"
	// (any header fields) happens per request, not here.
	prop := otel.GetTextMapPropagator()
	if prop == nil {
		prop = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	}
	// W3C TraceContext + Baggage, the canonical HTTP default (what otelhttp
	// uses): the extractor when no global is configured, and the fallback
	// when the global finds no remote span context, so a traceparent still
	// joins the upstream trace behind a B3-only global.
	w3c := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := extractTraceContext(r.Context(), prop, w3c, propagation.HeaderCarrier(r.Header))

			// r.Pattern isn't populated until after the mux matches, so we
			// start the span with a placeholder name + "unmatched" route and
			// update both after the handler returns.
			// r.Method and r.URL.Path are request-borne (a forged method,
			// a %0d%0a path). Scrub both at the sink — the same
			// control-byte rule the slog sinks carry — so a forged value
			// cannot render as a forged line in collector UIs/exporters.
			ctx, span := tracer.Start(ctx, fmt.Sprintf("HTTP %s", textsafe.ScrubControlBytes(r.Method)),
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.method", textsafe.ScrubControlBytes(r.Method)),
					attribute.String("http.target", textsafe.ScrubControlBytes(r.URL.Path)),
				),
			)
			defer span.End()

			// Put our span context on the response headers before the
			// handler runs: once it writes, the headers are sent and a
			// later change never reaches the client.
			prop.Inject(ctx, propagation.HeaderCarrier(w.Header()))
			ww := &tracingResponseWriter{ResponseWriter: w, status: 200}
			finish := func(status int) {
				// After the handler runs, r.Pattern is populated by the mux.
				route := r.Pattern
				if route == "" {
					route = "unmatched"
				}
				span.SetName(fmt.Sprintf("HTTP %s %s", textsafe.ScrubControlBytes(r.Method), textsafe.ScrubControlBytes(route)))
				span.SetAttributes(
					attribute.String("http.route", textsafe.ScrubControlBytes(route)),
					attribute.Int("http.status_code", status),
				)
				if status >= 500 {
					span.SetStatus(codes.Error, http.StatusText(status))
				}
			}
			// A panicking handler: the span gets the status the client
			// gets (Recovery's 500, or the one already sent) and an Error
			// status naming the panic, even when a 2xx had already gone
			// out. Runs before the deferred span.End above; the panic goes
			// on, and the SDK's End records it as the span's exception
			// event, so no RecordError here (it would add a second one).
			defer func() {
				if v := recover(); v != nil {
					finish(panickedStatus(w, ww.wroteHeader, ww.status))
					span.SetStatus(codes.Error, textsafe.Recovered(v))
					panic(v)
				}
			}()
			next.ServeHTTP(ww, r.WithContext(ctx))
			finish(ww.status)
		})
	}
}

// extractTraceContext reads the incoming trace context. A global
// propagator with no fields is the otel no-op default: W3C alone, as
// before. Otherwise the global goes first; if it yields no valid remote
// span context, W3C TraceContext is tried on top, and W3C Baggage only
// when the global left no baggage, so a global's own baggage format is
// not overwritten.
func extractTraceContext(parent context.Context, global, w3c propagation.TextMapPropagator, carrier propagation.TextMapCarrier) context.Context {
	if len(global.Fields()) == 0 {
		return w3c.Extract(parent, carrier)
	}
	ctx := global.Extract(parent, carrier)
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() && sc.IsRemote() {
		return ctx
	}
	ctx = propagation.TraceContext{}.Extract(ctx, carrier)
	if baggage.FromContext(ctx).Len() == 0 {
		ctx = propagation.Baggage{}.Extract(ctx, carrier)
	}
	return ctx
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
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(code) // informational: forward, keep waiting
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
	// A flush sends the headers: the status is the one set, or 200.
	// Latch it, so a WriteHeader after the flush (which net/http drops)
	// does not record a status the client never got.
	w.wroteHeader = true
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards to the underlying ResponseWriter's Hijacker if it has one,
// so a WebSocket-upgrade handler behind Tracing() keeps its upgrade path.
func (w *tracingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Push forwards to the underlying ResponseWriter's Pusher if it has one.
func (w *tracingResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pu, ok := w.ResponseWriter.(http.Pusher); ok {
		return pu.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Unwrap exposes the wrapped writer to net/http's ResponseController.
func (w *tracingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
