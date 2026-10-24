package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func recordJobSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	prev := otel.GetTracerProvider()
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return rec
}

func endedSpan(rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	for _, s := range rec.Ended() {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

func spanAttr(s sdktrace.ReadOnlySpan, key string) string {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.Emit()
		}
	}
	return ""
}

func TestMemoryJobSpans(t *testing.T) {
	rec := recordJobSpans(t)
	q := NewMemoryQueue(1)
	var inHandler trace.SpanContext
	q.RegisterHandler("ok", func(ctx context.Context, _ Job) error {
		inHandler = trace.SpanContextFromContext(ctx)
		return nil
	})
	q.RegisterHandler("fail", func(context.Context, Job) error { return errors.New("card 4242 declined") })
	q.RegisterHandler("boom", func(context.Context, Job) error { panic("boom") })
	q.Start()
	ctx := context.Background()
	for _, typ := range []string{"ok", "fail", "boom"} {
		if err := q.Enqueue(ctx, Job{ID: "job-" + typ, Type: typ, Lane: "mail", MaxAttempts: 1}); err != nil {
			t.Fatal(err)
		}
	}
	q.Close()

	ok := endedSpan(rec, "process ok")
	if ok == nil {
		t.Fatal("no span for the ok job")
	}
	if ok.SpanKind() != trace.SpanKindConsumer || ok.Parent().IsValid() {
		t.Errorf("kind = %v, parent valid = %v; want a consumer root span", ok.SpanKind(), ok.Parent().IsValid())
	}
	if inHandler.SpanID() != ok.SpanContext().SpanID() {
		t.Error("the handler's context does not carry the job span")
	}
	if got := spanAttr(ok, "messaging.message.id"); got != "job-ok" {
		t.Errorf("messaging.message.id = %q", got)
	}
	if got := spanAttr(ok, "gofastr.queue.lane"); got != "mail" {
		t.Errorf("gofastr.queue.lane = %q", got)
	}
	if ok.Status().Code == codes.Error {
		t.Error("a successful job is marked an error")
	}

	fail := endedSpan(rec, "process fail")
	if fail == nil || fail.Status().Code != codes.Error || spanAttr(fail, "error.type") != "*errors.errorString" {
		t.Fatalf("failed job span = %+v", fail)
	}
	if fail.Status().Description != "" || len(fail.Events()) != 0 {
		t.Error("the handler's error message reached the span")
	}
	boom := endedSpan(rec, "process boom")
	if boom == nil || boom.Status().Code != codes.Error || spanAttr(boom, "error.type") != "panic" {
		t.Fatalf("panicked job span = %+v", boom)
	}
}

func TestDBJobSpans(t *testing.T) {
	rec := recordJobSpans(t)
	_, q := openDBQueue(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Workers started under a span (an app's boot trace) must not file
	// every job into that one trace.
	ctx, boot := otel.Tracer("test").Start(ctx, "boot")
	defer boot.End()
	done := make(chan struct{})
	q.RegisterHandler("send", func(context.Context, Job) error { close(done); return nil })
	q.Start(ctx)
	if err := q.Enqueue(ctx, Job{Type: "send"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("the job never ran")
	}
	q.Close()
	s := endedSpan(rec, "process send")
	if s == nil {
		t.Fatal("no span for the DB queue job")
	}
	if s.Parent().IsValid() {
		t.Error("the job span is a child of the worker's boot span, want a root")
	}
}
