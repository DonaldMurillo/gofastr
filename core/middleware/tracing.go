package middleware

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/panicstack"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Tracing returns middleware that opens an OpenTelemetry server span per request, named "HTTP {method} {route}", extracting context with the global propagator and then W3C, and injecting the global propagator's headers into the response.
func Tracing() Middleware {
	tracer := otel.Tracer("github.com/DonaldMurillo/gofastr/core/middleware")
	prop := otel.GetTextMapPropagator()
	if prop == nil {
		prop = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	}
	w3c := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := extractTraceContext(r.Context(), prop, w3c, propagation.HeaderCarrier(r.Header))

			note := &routeNote{}
			ctx = context.WithValue(ctx, routeNoteKey{}, note)
			ctx, span := tracer.Start(ctx, fmt.Sprintf("HTTP %s", textsafe.ScrubControlBytes(r.Method)),
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.method", textsafe.ScrubControlBytes(r.Method)),
					attribute.String("http.target", textsafe.ScrubControlBytes(r.URL.Path)),
				),
			)
			defer span.End()

			prop.Inject(ctx, propagation.HeaderCarrier(w.Header()))
			ww := newStatusWriter(w)
			finish := func(status int) {
				route := r.Pattern
				if route == "" || strings.HasSuffix(route, "/") || strings.HasSuffix(route, "...}") {
					if noted := note.load(); noted != "" {
						route = noted
					}
				}
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
					span.SetAttributes(attribute.String("error.type", strconv.Itoa(status)))
				}
			}
			defer func() {
				if v := recover(); v != nil {
					finish(panickedStatus(w, ww.wroteHeader, ww.status))
					span.SetStatus(codes.Error, textsafe.Recovered(v))
					_, stack, _ := unwrapPanic(v)
					if stack == nil {
						stack = debug.Stack()
					}
					span.SetAttributes(
						attribute.String("error.type", "panic"),
						attribute.String("exception.stacktrace", panicOrigin(stack)),
					)
					panic(v)
				}
			}()
			next.ServeHTTP(ww, r.WithContext(ctx))
			finish(ww.status)
		})
	}
}

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

type routeNoteKey struct{}

type routeNote struct {
	mu    sync.Mutex
	route string
}

func (n *routeNote) load() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.route
}

// NoteRoute names the matched route for a router behind a catch-all, so Tracing names the span for it; a specific mux pattern and an earlier note win, and outside Tracing it does nothing.
func NoteRoute(ctx context.Context, route string) {
	n, _ := ctx.Value(routeNoteKey{}).(*routeNote)
	if n == nil || route == "" {
		return
	}
	n.mu.Lock()
	if n.route == "" {
		n.route = route
	}
	n.mu.Unlock()
}

// SpanFromRequest returns the span on r's context, a no-op span without Tracing.
func SpanFromRequest(r *http.Request) trace.Span {
	return trace.SpanFromContext(r.Context())
}

const maxSpanStackLen = 8 << 10

func panicOrigin(stack []byte) string {
	return textsafe.Truncate(panicstack.Origin(string(stack)), maxSpanStackLen)
}
