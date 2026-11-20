// Package tracing records OpenTelemetry client spans for an app's SQL
// statements and outbound HTTP requests, and a root span per background job,
// on the global TracerProvider.
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

const instrumentation = "github.com/DonaldMurillo/gofastr/core/tracing"

func tracer() trace.Tracer { return otel.Tracer(instrumentation) }

func markError(span trace.Span, err error) {
	span.SetAttributes(attribute.String("error.type", ErrorType(err)))
	span.SetStatus(codes.Error, "")
}

// ErrorTypePanic is the error.type value of a span whose work panicked.
const ErrorTypePanic = "panic"

// ErrorType returns err's Go type, unwrapping only fmt.Errorf %w wrappers.
// For multiple %w operands it follows the first error. For nil it returns "<nil>".
// errors.Join reports *errors.joinError; a wrapped errors.New sentinel reports
// *errors.errorString.
func ErrorType(err error) string {
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

func startAt(ctx context.Context, name string, start time.Time, attrs ...attribute.KeyValue) trace.Span {
	_, span := tracer().Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithTimestamp(start),
		trace.WithAttributes(attrs...),
	)
	return span
}
