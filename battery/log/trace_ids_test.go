package log_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/DonaldMurillo/gofastr/battery/log"
	"github.com/DonaldMurillo/gofastr/framework"
)

// A line written inside a traced request carries that request's trace
// and span IDs at the top level, the access line included, and a line
// written with no span carries neither. This is what lets a reader go
// from a log line to its trace.
func TestLinesCarryTraceAndSpanIDs(t *testing.T) {
	prev := otel.GetTracerProvider()
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	sink := &memSink{}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}), framework.WithTracing())
	plugin := log.New(log.Config{Sinks: []log.Sink{sink}})
	app.RegisterPlugin(plugin)
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	logger := app.Logger()
	app.Router().Get("/ping", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Inside a group: the IDs must still land at the top level.
		logger.WithGroup("req").InfoContext(r.Context(), "in.handler", "k", "v")
		w.WriteHeader(204)
	}))
	logger.InfoContext(context.Background(), "no.span")

	app.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping", nil))

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want the request's one", len(spans))
	}
	traceID, spanID := spans[0].SpanContext().TraceID().String(), spans[0].SpanContext().SpanID().String()
	seen := map[string]bool{}
	for _, e := range sink.entries(t) {
		msg, _ := e["msg"].(string)
		seen[msg] = true
		switch msg {
		case "in.handler", "http.access":
			if e["trace_id"] != traceID || e["span_id"] != spanID {
				t.Errorf("%s: trace_id=%v span_id=%v, want %s %s", msg, e["trace_id"], e["span_id"], traceID, spanID)
			}
		case "no.span":
			if _, ok := e["trace_id"]; ok {
				t.Errorf("a line with no span carries trace_id %v", e["trace_id"])
			}
		}
	}
	for _, msg := range []string{"in.handler", "http.access", "no.span"} {
		if !seen[msg] {
			t.Errorf("no %s line was written", msg)
		}
	}
}
