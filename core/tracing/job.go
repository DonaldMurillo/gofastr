package tracing

import (
	"context"
	"runtime/debug"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/panicstack"
)

const (
	maxJobErrLen   = 512
	maxJobStackLen = 8 << 10
)

// Job runs fn inside a span of its own trace, named name: a piece of
// background work nobody asked for over HTTP, such as a scheduled run.
// The span fails when fn returns an error, with the error's text as its
// status, or when fn panics, with the panic's value as its status and
// the stack from the function that panicked as exception.stacktrace.
// A panic goes on to the caller, which owns the recovery.
//
// With no TracerProvider installed the span is a no-op and Job is a
// plain call. The error's text is the span's status as written: a
// caller whose errors quote data that must not be stored sets its own
// span.
func Job(ctx context.Context, name string, fn func(context.Context) error) (err error) {
	ctx, span := otel.Tracer("github.com/DonaldMurillo/gofastr/core/tracing").Start(ctx, textsafe.ScrubControlBytes(name),
		trace.WithSpanKind(trace.SpanKindInternal), trace.WithNewRoot())
	defer func() {
		if r := recover(); r != nil {
			span.SetStatus(codes.Error, textsafe.Recovered(r))
			span.SetAttributes(attribute.String("exception.stacktrace", textsafe.Truncate(panicstack.Origin(string(debug.Stack())), maxJobStackLen)))
			span.End()
			panic(r)
		}
		if err != nil {
			//gofastr:allow(errleak) a span's status goes to the operator's trace store, not to a client; saying what failed is what it is for
			span.SetStatus(codes.Error, textsafe.Truncate(textsafe.ScrubControlBytes(err.Error()), maxJobErrLen))
		}
		span.End()
	}()
	return fn(ctx)
}
