package queue

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

func invokeRecovering(ctx context.Context, h Handler, job Job) (err error) {
	jobType := textsafe.ScrubControlBytes(job.Type)
	ctx, span := otel.Tracer("github.com/DonaldMurillo/gofastr/battery/queue").Start(ctx, "process "+jobType,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithNewRoot(),
		trace.WithAttributes(
			attribute.String("messaging.system", "gofastr"),
			attribute.String("messaging.operation.type", "process"),
			attribute.String("messaging.destination.name", jobType),
			attribute.String("messaging.message.id", textsafe.ScrubControlBytes(job.ID)),
			attribute.String("gofastr.queue.lane", textsafe.ScrubControlBytes(job.Lane)),
			attribute.Int("gofastr.queue.attempts", job.Attempts),
		),
	)
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("queue: handler for %q panicked: %v", job.Type, r)
			span.SetAttributes(attribute.String("error.type", "panic"))
			span.SetStatus(codes.Error, "")
		} else if err != nil {
			span.SetAttributes(attribute.String("error.type", fmt.Sprintf("%T", err)))
			span.SetStatus(codes.Error, "")
		}
		span.End()
	}()
	return h(ctx, job)
}
