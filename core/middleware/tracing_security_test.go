package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestTracingPreservesHijacker asserts the tracing writer keeps the
// underlying Hijacker (and Flusher) so a WS-upgrade handler behind the
// Tracing() middleware keeps its upgrade path.
func TestTracingPreservesHijacker(t *testing.T) {
	var sawHijacker, sawFlusher bool
	h := Tracing()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := w.(http.Hijacker); ok {
			sawHijacker = true
		}
		if _, ok := w.(http.Flusher); ok {
			sawFlusher = true
		}
	}))

	rec := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if !sawHijacker {
		t.Fatal("Hijacker not preserved through tracing wrapper")
	}
	if !sawFlusher {
		t.Fatal("Flusher not preserved through tracing wrapper")
	}
}

// spanRecordingTracer installs an in-memory span recorder as the global
// TracerProvider BEFORE the middleware is constructed (Tracing() resolves
// otel.Tracer at construction time) and restores the previous provider on
// cleanup. Package-internal twin of withRecordingTracer in the external
// test package; the two packages' symbols do not collide.
func spanRecordingTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	prev := otel.GetTracerProvider()
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return rec
}

// Property: a malformed W3C traceparent never joins the server span onto
// an attacker-chosen trace — every invalid shape must be dropped at
// extraction so the span runs on a fresh, valid trace ID. Surfaces: the
// distinct W3C-invalid shapes (all-zero trace id, all-zero span id,
// short trace id, non-hex version, non-hex trace id).
func TestTracing_BadTraceparentGetsFreshTrace(t *testing.T) {
	const attackerTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
	shapes := []string{
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01", // all-zero trace id
		"00-" + attackerTrace + "-0000000000000000-01",            // all-zero span id
		"00-" + attackerTrace[:31] + "-00f067aa0ba902b7-01",       // 31-hex trace id
		"zz-" + attackerTrace + "-00f067aa0ba902b7-01",            // non-hex version
		"00-4bf9zz3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", // non-hex trace id
	}
	for _, tp := range shapes {
		t.Run(tp, func(t *testing.T) {
			rec := spanRecordingTracer(t)
			h := Tracing()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("traceparent", tp)
			h.ServeHTTP(httptest.NewRecorder(), req)

			spans := rec.Ended()
			if len(spans) != 1 {
				t.Fatalf("expected exactly 1 span, got %d", len(spans))
			}
			tid := spans[0].SpanContext().TraceID()
			if !tid.IsValid() {
				t.Fatalf("span trace id not valid: %s", tid)
			}
			if tid.String() == attackerTrace {
				t.Fatalf("malformed traceparent joined attacker-chosen trace %s; extraction must drop it", attackerTrace)
			}
		})
	}
}

// Property: request-borne W3C propagation headers (traceparent,
// tracestate, baggage) never put control bytes into the response headers
// the middleware injects through the configured propagator. Surfaces: the
// three extraction headers x control-byte / DEL / junk payloads, plus a
// valid triple as the happy path that must still inject.
func TestTracing_InjectedHeadersNoCtrlBytes(t *testing.T) {
	const validTP = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	prevProp := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	t.Cleanup(func() { otel.SetTextMapPropagator(prevProp) })

	cases := []struct{ name, tp, ts, bag string }{
		{"ctrl byte in tracestate", validTP, "a=b\x01c", ""},
		{"DEL in tracestate", validTP, "a=b\x7fc", ""},
		{"ctrl byte in baggage", validTP, "", "k=\x01v"},
		{"DEL in baggage", validTP, "", "k=\x7fv"},
		{"junk traceparent", "not-a-traceparent", "", ""},
		{"valid triple injects", validTP, "congo=congos", "k=v"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = spanRecordingTracer(t)
			h := Tracing()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.tp != "" {
				req.Header.Set("traceparent", tc.tp)
			}
			if tc.ts != "" {
				req.Header.Set("tracestate", tc.ts)
			}
			if tc.bag != "" {
				req.Header.Set("baggage", tc.bag)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			for k, vs := range rec.Header() {
				for _, v := range vs {
					if strings.ContainsAny(v, c0AndDelSet) {
						t.Fatalf("response header %q carries raw control bytes: %q", k, v)
					}
				}
			}
			if tc.name == "valid triple injects" && rec.Header().Get("traceparent") == "" {
				t.Fatal("valid trace context was not injected into the response headers")
			}
		})
	}
}

// Property (same family as TestLogSinksScrubAndBound): request-derived
// values a middleware records into an observability sink are
// control-byte scrubbed. The tracing sink writes the span NAME from
// r.Method and the http.target ATTRIBUTE from the percent-decoded URL
// path — both RAW today. A forged method or a %0d%0a path reaches the
// tracing backend verbatim, where collector UIs and exporters render it:
// the log-forging twin. Pinning the same scrub the slog sinks already
// carry.
func TestTracing_SpanAttrsScrubControlBytes(t *testing.T) {
	rec := spanRecordingTracer(t)
	h := Tracing()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/ok%0d%0afake-entry", nil)
	req.Method = "GET\x01"
	h.ServeHTTP(httptest.NewRecorder(), req)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected exactly 1 span, got %d", len(spans))
	}
	if strings.ContainsAny(spans[0].Name(), c0AndDelSet) {
		t.Errorf("span name carries raw control bytes: %q", spans[0].Name())
	}
	for _, kv := range spans[0].Attributes() {
		if s := kv.Value.Emit(); strings.ContainsAny(s, c0AndDelSet) {
			t.Errorf("span attribute %q carries raw control bytes: %q", string(kv.Key), s)
		}
	}
}
