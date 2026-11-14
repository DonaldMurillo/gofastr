package tracing

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	prev := otel.GetTracerProvider()
	sr := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return sr
}

//go:noinline
func jobThatPanics(context.Context) error { panic("nightly export: nil ledger") }

// A piece of background work is a trace of its own: not a step of
// whatever trace the caller happened to be in, with the work's own
// steps under it, failed when the work returns an error, and failed with
// the stack when it panics. The panic still reaches the caller.
func TestJobIsATraceOfItsOwn(t *testing.T) {
	sr := recordSpans(t)
	outer, parent := otel.Tracer("test").Start(context.Background(), "a request")

	var inside trace.SpanContext
	if err := Job(outer, "cron nightly\x1b[2J", func(ctx context.Context) error {
		inside = trace.SpanContextFromContext(ctx)
		_, step := otel.Tracer("test").Start(ctx, "SELECT")
		step.End()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	parent.End()
	spans := sr.Ended() // the step, the job, the request
	if len(spans) != 3 {
		t.Fatalf("%d spans", len(spans))
	}
	step, job := spans[0], spans[1]
	if strings.Contains(job.Name(), "\x1b") || !strings.HasPrefix(job.Name(), "cron nightly") {
		t.Fatalf("the name kept a control byte: %q", job.Name())
	}
	if job.Parent().IsValid() || job.SpanContext().TraceID() == parent.SpanContext().TraceID() {
		t.Fatal("the job is a step of the trace it was started from")
	}
	if step.Parent().SpanID() != job.SpanContext().SpanID() || inside.SpanID() != job.SpanContext().SpanID() {
		t.Fatal("the work's own step is not under the job")
	}
	if job.SpanKind() != trace.SpanKindInternal || job.Status().Code == codes.Error {
		t.Fatalf("kind %v, status %v", job.SpanKind(), job.Status())
	}

	sr = recordSpans(t)
	long := strings.Repeat("x", 2000)
	want := errors.New("ledger closed\x07" + long)
	if err := Job(context.Background(), "cron close", func(context.Context) error { return want }); err != want {
		t.Fatalf("returned %v", err)
	}
	st := sr.Ended()[0].Status()
	if st.Code != codes.Error || !strings.HasPrefix(st.Description, "ledger closed") || strings.Contains(st.Description, "\x07") || len(st.Description) > maxJobErrLen+64 {
		t.Fatalf("a failed job's status: %v, %d bytes", st.Code, len(st.Description))
	}

	sr = recordSpans(t)
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = Job(context.Background(), "cron export", jobThatPanics)
	}()
	if recovered != "nightly export: nil ledger" {
		t.Fatalf("the caller recovered %v", recovered)
	}
	if n := len(sr.Ended()); n != 1 {
		t.Fatalf("%d spans ended for a job that panicked", n)
	}
	sp := sr.Ended()[0]
	var stack string
	for _, kv := range sp.Attributes() {
		if kv.Key == "exception.stacktrace" {
			stack = kv.Value.AsString()
		}
	}
	if sp.Status().Code != codes.Error || sp.Status().Description != "nightly export: nil ledger" ||
		!strings.HasPrefix(stack, "github.com/DonaldMurillo/gofastr/core/tracing.jobThatPanics(") {
		t.Fatalf("a panicked job: status %+v, stack:\n%s", sp.Status(), stack)
	}
}

// With no provider installed a job is a plain call.
func TestJobWithNoTracerIsACall(t *testing.T) {
	ran := false
	if err := Job(context.Background(), "cron x", func(context.Context) error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("ran=%v, %v", ran, err)
	}
}
