// Package tracing records OpenTelemetry client spans for the calls an app
// makes outward: SQL statements through a wrapped database/sql driver, and
// HTTP requests through a wrapped RoundTripper. Spans go to the global
// TracerProvider (otel.SetTracerProvider); with none set they are dropped.
//
// Spans carry no data values. A statement's span holds its SQL with every
// literal replaced by ?, and a request's span holds its URL without the
// query string. A failure is marked with the error's Go type, not its
// message, since driver and transport messages quote values.
package tracing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// instrumentation names the tracer every span here comes from.
const instrumentation = "github.com/DonaldMurillo/gofastr/core/tracing"

// tracer is looked up per span so a TracerProvider installed after the
// wrapper was built still receives its spans.
func tracer() trace.Tracer { return otel.Tracer(instrumentation) }

// markError sets the span's status to Error with the error's type as
// error.type. The message stays out: it routinely quotes row values,
// constraint keys and URLs.
func markError(span trace.Span, err error) {
	span.SetAttributes(attribute.String("error.type", errorType(err)))
	span.SetStatus(codes.Error, "")
}

// errorType is the Go type of the error under any fmt.Errorf wrapping, so
// fmt.Errorf("insert: %w", pqErr) still reads as *pq.Error. A wrapper
// with several %w verbs reads as the first error it wraps.
func errorType(err error) string {
	for {
		var inner error
		switch fmt.Sprintf("%T", err) {
		case "*fmt.wrapError":
			inner = errors.Unwrap(err)
		case "*fmt.wrapErrors":
			if multi, ok := err.(interface{ Unwrap() []error }); ok {
				if errs := multi.Unwrap(); len(errs) > 0 {
					inner = errs[0]
				}
			}
		}
		if inner == nil {
			return fmt.Sprintf("%T", err)
		}
		err = inner
	}
}

// startAt opens a client span that began at start. Calls are timed before
// the span opens because a driver.ErrSkip answer produces no span at all.
func startAt(ctx context.Context, name string, start time.Time, attrs ...attribute.KeyValue) trace.Span {
	_, span := tracer().Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithTimestamp(start),
		trace.WithAttributes(attrs...),
	)
	return span
}
