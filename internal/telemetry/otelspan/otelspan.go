// Package otelspan feeds finished OpenTelemetry spans to a
// telemetry.Collector. Install it on the SDK's TracerProvider:
//
//	sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(otelspan.New(collector)))
//
// It is the one package here that imports the OpenTelemetry SDK, so an
// app that sends its spans elsewhere does not link the file store's
// half of it.
package otelspan

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/DonaldMurillo/gofastr/internal/telemetry"
)

// Processor is an sdktrace.SpanProcessor that keeps nothing itself:
// every finished span goes straight to the collector.
type Processor struct{ c *telemetry.Collector }

// New returns a processor writing to c.
func New(c *telemetry.Collector) *Processor { return &Processor{c: c} }

// OnStart does nothing: a span is recorded when it ends.
func (*Processor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}

// OnEnd hands the span to the collector.
func (p *Processor) OnEnd(s sdktrace.ReadOnlySpan) {
	sc, parent := s.SpanContext(), s.Parent()
	e := telemetry.SpanEnd{
		TraceID: sc.TraceID().String(),
		Start:   s.StartTime(),
		Root:    !parent.IsValid() || parent.IsRemote(),
		Error:   s.Status().Code == codes.Error,
		Route:   s.Name(),
		Span: telemetry.Span{
			SpanID: sc.SpanID().String(),
			Name:   s.Name(),
			Dur:    s.EndTime().Sub(s.StartTime()),
		},
	}
	if parent.IsValid() {
		e.Span.ParentID = parent.SpanID().String()
	}
	attrs := s.Attributes()
	e.Span.Attrs = make([]telemetry.Attr, 0, len(attrs))
	var method, route string
	for _, kv := range attrs {
		switch kv.Key {
		case "http.status_code", "http.response.status_code":
			e.Status = int(kv.Value.AsInt64())
		case "http.method", "http.request.method":
			method = kv.Value.AsString()
		case "http.route":
			route = kv.Value.AsString()
		}
		e.Span.Attrs = append(e.Span.Attrs, telemetry.Attr{Key: string(kv.Key), Value: kv.Value.Emit()})
	}
	// A request is listed by method and route. The mux pattern is the
	// route and may already start with the method ("GET /orders/{id}");
	// the span name repeats it, so the name is only for other spans.
	if route != "" {
		e.Route = route
		if method != "" && !strings.Contains(route, " ") {
			e.Route = method + " " + route
		}
	}
	p.c.SpanEnded(e)
}

// Shutdown has nothing to flush: the collector holds no finished trace.
func (*Processor) Shutdown(context.Context) error { return nil }

// ForceFlush has nothing to flush.
func (*Processor) ForceFlush(context.Context) error { return nil }
