package otelspan_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

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
	Path    string `json:"path"`
	Status  int    `json:"status"`
	Error   bool   `json:"error"`
	Failed  int    `json:"failed_steps"`
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
		root.SetAttributes(attribute.Int("http.status_code", 503), attribute.String("http.method", "GET"), attribute.String("http.route", route), attribute.String("http.target", "/orders/42"))
		// The tracing middleware fails the request's span on a 5xx.
		root.SetStatus(codes.Error, "Service Unavailable")
		root.End()

		if len(lines) != 1 {
			t.Fatalf("remote=%v: %d trace lines, want 1", remote, len(lines))
		}
		var got traceLine
		if err := json.Unmarshal(lines[0], &got); err != nil {
			t.Fatalf("remote=%v: %v\n%s", remote, err, lines[0])
		}
		rootID, childID := root.SpanContext().SpanID().String(), child.SpanContext().SpanID().String()
		if got.TraceID != root.SpanContext().TraceID().String() || got.Route != "GET /orders/{id}" || got.Path != "/orders/42" || got.Status != 503 || !got.Error || got.Failed != 1 ||
			len(got.Spans) != 2 || got.Spans[0].SpanID != rootID || got.Spans[0].Name != "HTTP GET GET /orders/{id}" ||
			got.Spans[1].SpanID != childID || got.Spans[1].ParentID != rootID || got.Spans[1].Attrs["db.rows"] != "3" {
			t.Fatalf("remote=%v: %s", remote, lines[0])
		}
		if wantParent := map[bool]string{false: "", true: "0900000000000000"}[remote]; got.Spans[0].ParentID != wantParent {
			t.Fatalf("remote=%v: root parent_id %q, want %q", remote, got.Spans[0].ParentID, wantParent)
		}
	}
}

// A lone outgoing call, a query some background loop ran outside any
// request, is not a request: it is not kept as part of the sample. The
// same call inside a request is one of that request's steps.
func TestLoneClientSpanIsNotSampledIn(t *testing.T) {
	var kept []telemetry.Trace
	c, err := telemetry.NewCollector(telemetry.Config{Dir: "unused", SampleRate: 1}, func(tr telemetry.Trace) error {
		kept = append(kept, tr)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(otelspan.New(c, "/assets/"))).Tracer("test")

	// A request under a quiet prefix is not sampled in either; its
	// neighbour outside the prefix is.
	for path, want := range map[string]int{"/assets/app.js": 0, "/assets": 1} {
		kept = nil
		_, req := tracer.Start(context.Background(), "HTTP GET", trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("http.target", path)))
		req.End()
		if len(kept) != want {
			t.Fatalf("request for %s: kept %d traces, want %d", path, len(kept), want)
		}
	}
	kept = nil

	_, lone := tracer.Start(context.Background(), "SELECT", trace.WithSpanKind(trace.SpanKindClient))
	lone.End()
	if len(kept) != 0 {
		t.Fatalf("a lone client span was kept as a request: %+v", kept)
	}

	ctx, root := tracer.Start(context.Background(), "job", trace.WithSpanKind(trace.SpanKindConsumer))
	_, step := tracer.Start(ctx, "SELECT", trace.WithSpanKind(trace.SpanKindClient))
	step.End()
	root.End()
	if len(kept) != 1 || len(kept[0].Spans) != 2 {
		t.Fatalf("a job and its query: kept %+v", kept)
	}
}

// Where a span failed reaches the trace by either of the two ways an
// OpenTelemetry span can say it: the exception.stacktrace attribute the
// tracing middleware sets for a panic, or the exception event of an
// error recorded with its stack. The attribute is the trace's own
// field, not one more span attribute cut to a line.
func TestStackReachesTheTrace(t *testing.T) {
	var kept []telemetry.Trace
	c, err := telemetry.NewCollector(telemetry.Config{Dir: "unused", SampleRate: 1}, func(tr telemetry.Trace) error {
		kept = append(kept, tr)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(otelspan.New(c))).Tracer("test")

	long := "app.handler(...)\n\t/app/h.go:12 +0x2\n" + strings.Repeat("y", 3000)
	_, span := tracer.Start(context.Background(), "HTTP GET", trace.WithSpanKind(trace.SpanKindServer))
	span.SetStatus(codes.Error, "index out of range")
	span.SetAttributes(attribute.String("exception.stacktrace", long), attribute.String("http.route", "/a"))
	span.End()
	if len(kept) != 1 || kept[0].Stack != long || kept[0].Err != "index out of range" {
		t.Fatalf("from the attribute: %d traces, stack of %d bytes, err %q", len(kept), len(kept[0].Stack), kept[0].Err)
	}
	for _, a := range kept[0].Spans[0].Attrs {
		if a.Key == "exception.stacktrace" {
			t.Fatalf("the stack is also a span attribute, cut to %d bytes", len(a.Value))
		}
	}

	kept = nil
	_, span = tracer.Start(context.Background(), "job", trace.WithSpanKind(trace.SpanKindConsumer))
	span.RecordError(errors.New("disk full"), trace.WithStackTrace(true))
	span.SetStatus(codes.Error, "")
	span.End()
	if len(kept) != 1 || !strings.Contains(kept[0].Stack, "TestStackReachesTheTrace") || kept[0].Err != "disk full" {
		t.Fatalf("from a recorded error: %+v", kept)
	}

	// A span that did not fail brings no stack, whatever it carries.
	kept = nil
	_, span = tracer.Start(context.Background(), "HTTP GET", trace.WithSpanKind(trace.SpanKindServer))
	span.SetAttributes(attribute.String("exception.stacktrace", "stale"))
	span.End()
	if len(kept) != 1 || kept[0].Stack != "" {
		t.Fatalf("a span that did not fail: %+v", kept)
	}
}

// What a trace's first span says it is decides how it is counted: a
// server's span is a request, a consumer's or an internal one is
// background work, and a lone client call is neither.
func TestRootSpanKindSaysRequestOrJob(t *testing.T) {
	dir := t.TempDir()
	w, err := telemetry.NewWriter(telemetry.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	c, err := telemetry.NewCollector(telemetry.Config{Dir: dir, SampleRate: 1}, func(telemetry.Trace) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	counts := telemetry.NewRollup()
	c.WatchStats(counts)
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(otelspan.New(c))).Tracer("test")
	for name, kind := range map[string]trace.SpanKind{
		"HTTP GET": trace.SpanKindServer, "process welcome-email": trace.SpanKindConsumer,
		"cron nightly": trace.SpanKindInternal, "SELECT": trace.SpanKindClient,
	} {
		ctx, span := tracer.Start(context.Background(), name, trace.WithSpanKind(kind))
		// A step under it is nothing of its own.
		_, step := tracer.Start(ctx, "step", trace.WithSpanKind(trace.SpanKindInternal))
		step.End()
		span.End()
	}
	if err := w.FlushStats(counts, true); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := telemetry.Open(telemetry.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	got, err := s.Stats(telemetry.StatsQuery{From: now.Add(-time.Hour), To: now.Add(time.Hour), Step: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	var routes, jobs []string
	for _, r := range got.Routes {
		routes = append(routes, r.Route)
	}
	for _, j := range got.Jobs {
		jobs = append(jobs, j.Route)
	}
	slices.Sort(jobs)
	if len(routes) != 1 || routes[0] != "HTTP GET" || len(jobs) != 2 || jobs[0] != "cron nightly" || jobs[1] != "process welcome-email" {
		t.Fatalf("requests %v, jobs %v", routes, jobs)
	}
}
