package tracing

import (
	"context"
	"runtime/debug"

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

// Job runs fn inside a root span named name, failed with fn's error type and
// text or with a panic's value and stack; the panic goes on to the caller.
func Job(ctx context.Context, name string, fn func(context.Context) error) (err error) {
	ctx, span := tracer().Start(ctx, textsafe.ScrubControlBytes(name),
		trace.WithSpanKind(trace.SpanKindInternal), trace.WithNewRoot())
	defer func() {
		if r := recover(); r != nil {
			span.SetStatus(codes.Error, textsafe.Recovered(r))
			span.SetAttributes(
				attribute.String("error.type", "panic"),
				attribute.String("exception.stacktrace", textsafe.Truncate(panicstack.Origin(string(debug.Stack())), maxJobStackLen)),
			)
			span.End()
			panic(r)
		}
		if err != nil {
			span.SetAttributes(attribute.String("error.type", ErrorType(err)))
			//gofastr:allow(errleak) a span's status goes to the operator's trace store, not to a client; saying what failed is what it is for
			span.SetStatus(codes.Error, textsafe.Truncate(textsafe.ScrubControlBytes(err.Error()), maxJobErrLen))
		}
		span.End()
	}()
	return fn(ctx)
}
