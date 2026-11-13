package otelspan_test

import (
	"context"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/DonaldMurillo/gofastr/internal/telemetry"
	"github.com/DonaldMurillo/gofastr/internal/telemetry/otelspan"
)

type traceLine struct {
	TraceID string `json:"trace_id"`
	Route   string `json:"route"`
	Status  int    `json:"status"`
	Error   bool   `json:"error"`
	Spans   []struct {
		SpanID   string            `json:"span_id"`
		ParentID string            `json:"parent_id"`
		Name     string            `json:"name"`
		Attrs    map[string]string `json:"attrs"`
	} `json:"spans"`
}

// Real SDK spans come out as one trace line when the request's span
// ends: the root first, parents linked, attributes as strings, status
// and failure read from the spans. A request continued from another
// process is complete when its local root ends.
func TestSpansBecomeOneTraceLine(t *testing.T) {
	for _, remote := range []bool{false, true} {
		var lines [][]byte
		c, err := telemetry.NewCollector(telemetry.Config{Dir: "unused", SampleRate: 1}, func(tr telemetry.Trace) error {
			lines = append(lines, telemetry.AppendTrace(nil, tr))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(otelspan.New(c)))
		tracer := tp.Tracer("test")

		ctx := context.Background()
		if remote {
			ctx = trace.ContextWithRemoteSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
				TraceID: trace.TraceID{1, 2, 3}, SpanID: trace.SpanID{9}, TraceFlags: trace.FlagsSampled, Remote: true,
			}))
		}
		ctx, root := tracer.Start(ctx, "HTTP GET GET /orders/{id}")
		_, child := tracer.Start(ctx, "SELECT", trace.WithAttributes(attribute.Int("db.rows", 3)))
		child.SetStatus(codes.Error, "boom")
		child.End()
		if len(lines) != 0 {
			t.Fatalf("remote=%v: a trace was written before its root ended", remote)
		}
		// What the tracing middleware sets: a pattern with its method
		// for one request, a bare path for the other.
		route := map[bool]string{false: "GET /orders/{id}", true: "/orders/{id}"}[remote]
		root.SetAttributes(attribute.Int("http.status_code", 503), attribute.String("http.method", "GET"), attribute.String("http.route", route))
		root.End()

		if len(lines) != 1 {
			t.Fatalf("remote=%v: %d trace lines, want 1", remote, len(lines))
		}
		var got traceLine
		if err := json.Unmarshal(lines[0], &got); err != nil {
			t.Fatalf("remote=%v: %v\n%s", remote, err, lines[0])
		}
		rootID, childID := root.SpanContext().SpanID().String(), child.SpanContext().SpanID().String()
		if got.TraceID != root.SpanContext().TraceID().String() || got.Route != "GET /orders/{id}" || got.Status != 503 || !got.Error ||
			len(got.Spans) != 2 || got.Spans[0].SpanID != rootID || got.Spans[0].Name != "HTTP GET GET /orders/{id}" ||
			got.Spans[1].SpanID != childID || got.Spans[1].ParentID != rootID || got.Spans[1].Attrs["db.rows"] != "3" {
			t.Fatalf("remote=%v: %s", remote, lines[0])
		}
		if wantParent := map[bool]string{false: "", true: "0900000000000000"}[remote]; got.Spans[0].ParentID != wantParent {
			t.Fatalf("remote=%v: root parent_id %q, want %q", remote, got.Spans[0].ParentID, wantParent)
		}
	}
}
