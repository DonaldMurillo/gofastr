package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

// withRecordingTracer installs an in-memory SpanRecorder + TracerProvider
// for the duration of the test and restores the previous globals on
// cleanup. Returns the recorder so assertions can inspect the spans the
// middleware emitted.
func withRecordingTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	prev := otel.GetTracerProvider()
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return recorder
}

// ============================================================================
// Tracing wraps each request in a span with the expected attributes
// ============================================================================

func TestTracing_RecordsSpan(t *testing.T) {
	rec := withRecordingTracer(t)

	// Use the framework's Router so Tracing wraps the handler chain
	// (middleware runs INSIDE the mux dispatch where r.Pattern is set).
	rt := router.New()
	rt.Use(router.Middleware(Tracing()))
	rt.Get("/posts/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/posts/p1", nil)
	w := httptest.NewRecorder()
	rt.ServeHTTP(w, req)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	s := spans[0]
	if want := "HTTP GET GET /posts/{id}"; s.Name() != want {
		t.Errorf("span name: got %q, want %q", s.Name(), want)
	}
	attrs := attrsAsMap(s.Attributes())
	if attrs["http.method"] != "GET" {
		t.Errorf("http.method: %v", attrs["http.method"])
	}
	if attrs["http.route"] != "GET /posts/{id}" {
		t.Errorf("http.route: %v", attrs["http.route"])
	}
	if attrs["http.status_code"] != int64(200) {
		t.Errorf("http.status_code: %v", attrs["http.status_code"])
	}
	if attrs["http.target"] != "/posts/p1" {
		t.Errorf("http.target: %v", attrs["http.target"])
	}
}

// ============================================================================
// 5xx responses mark the span as Error
// ============================================================================

func TestTracing_500MarksSpanError(t *testing.T) {
	rec := withRecordingTracer(t)
	rt := router.New()
	rt.Use(router.Middleware(Tracing()))
	rt.Get("/boom", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "kaboom", http.StatusInternalServerError)
	}))
	w := httptest.NewRecorder()
	rt.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status().Code.String() != "Error" {
		t.Fatalf("expected Error status, got %v", spans[0].Status())
	}
}

// ============================================================================
// Incoming traceparent header is honoured (distributed propagation)
// ============================================================================

func TestTracing_PropagatesIncomingTrace(t *testing.T) {
	rec := withRecordingTracer(t)
	rt := router.New()
	rt.Use(router.Middleware(Tracing()))
	rt.Get("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))

	// W3C traceparent format: version-trace_id-span_id-flags
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	w := httptest.NewRecorder()
	rt.ServeHTTP(w, req)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	// Server span should be a CHILD of the incoming trace id.
	got := spans[0].SpanContext().TraceID().String()
	if got != "0af7651916cd43dd8448eb211c80319c" {
		t.Fatalf("expected propagated trace id, got %q", got)
	}
}

// ============================================================================
// Without a TracerProvider configured we don't blow up — spans go to noop
// ============================================================================

func TestTracing_NoProviderIsSafe(t *testing.T) {
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(noop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	rt := router.New()
	rt.Use(router.Middleware(Tracing()))
	rt.Get("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = SpanFromRequest(r)
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	rt.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
}

// ============================================================================
// SpanFromRequest exposes the active span to handlers
// ============================================================================

func TestTracing_SpanFromRequest(t *testing.T) {
	_ = withRecordingTracer(t)
	var seenContext context.Context
	rt := router.New()
	rt.Use(router.Middleware(Tracing()))
	rt.Get("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenContext = r.Context()
		s := SpanFromRequest(r)
		if !s.SpanContext().IsValid() {
			t.Error("expected valid span context inside handler")
		}
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	rt.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if seenContext == nil {
		t.Fatal("handler did not run")
	}
}

func attrsAsMap(attrs []attribute.KeyValue) map[string]any {
	out := map[string]any{}
	for _, kv := range attrs {
		out[string(kv.Key)] = kv.Value.AsInterface()
	}
	return out
}
