package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// testB3Propagator is a B3-style single-header propagator that knows no
// W3C header: "x-test-trace: <trace id>-<span id>", plus an optional
// "x-test-bag: <key>=<value>" carrying one baggage member in its own
// format.
type testB3Propagator struct{}

func (testB3Propagator) Inject(ctx context.Context, c propagation.TextMapCarrier) {
	if sc := oteltrace.SpanContextFromContext(ctx); sc.IsValid() {
		c.Set("x-test-trace", sc.TraceID().String()+"-"+sc.SpanID().String())
	}
}

func (testB3Propagator) Extract(ctx context.Context, c propagation.TextMapCarrier) context.Context {
	if raw := c.Get("x-test-bag"); raw != "" {
		k, v, _ := strings.Cut(raw, "=")
		if m, err := baggage.NewMember(k, v); err == nil {
			if b, err := baggage.New(m); err == nil {
				ctx = baggage.ContextWithBaggage(ctx, b)
			}
		}
	}
	tid, sid, ok := strings.Cut(c.Get("x-test-trace"), "-")
	if !ok {
		return ctx
	}
	traceID, err := oteltrace.TraceIDFromHex(tid)
	if err != nil {
		return ctx
	}
	spanID, err := oteltrace.SpanIDFromHex(sid)
	if err != nil {
		return ctx
	}
	return oteltrace.ContextWithRemoteSpanContext(ctx, oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: oteltrace.FlagsSampled, Remote: true,
	}))
}

func (testB3Propagator) Fields() []string { return []string{"x-test-trace", "x-test-bag"} }

const (
	upstreamTrace = "0af7651916cd43dd8448eb211c80319c"
	upstreamSpan  = "b7ad6b7169203331"
	w3cTrace      = "4bf92f3577b34da6a3ce929d0e0e4736"
	w3cSpan       = "00f067aa0ba902b7"
)

// withGlobalPropagator sets the global otel propagator for one test and
// puts the previous one back on cleanup.
func withGlobalPropagator(t *testing.T, p propagation.TextMapPropagator) {
	t.Helper()
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(p)
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })
}

// serveTraced runs one request through Tracing and returns the server
// span's trace id, its parent span id, and the baggage the handler saw.
func serveTraced(t *testing.T, hdr map[string]string) (trace, parent string, bag baggage.Baggage) {
	t.Helper()
	rec := withRecordingTracer(t)
	rt := router.New()
	rt.Use(middleware.Tracing())
	rt.Get("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bag = baggage.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rt.ServeHTTP(httptest.NewRecorder(), req)
	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	return spans[0].SpanContext().TraceID().String(), spans[0].Parent().SpanID().String(), bag
}

// Tracing extracted with W3C only, whatever the global was, so an app
// that set a B3 propagator injected B3 and never joined a B3 upstream.
func TestTracing_GlobalPropagatorJoinsUpstream(t *testing.T) {
	withGlobalPropagator(t, testB3Propagator{})
	trace, parent, _ := serveTraced(t, map[string]string{"x-test-trace": upstreamTrace + "-" + upstreamSpan})
	if trace != upstreamTrace || parent != upstreamSpan {
		t.Fatalf("span did not join the upstream trace from the global's header: trace %s parent %s", trace, parent)
	}
}

// A global that lacks W3C still joins a caller that sent only traceparent,
// and W3C baggage rides along when the global brought none.
func TestTracing_GlobalWithoutW3CFallsBackToTraceparent(t *testing.T) {
	withGlobalPropagator(t, testB3Propagator{})
	trace, parent, bag := serveTraced(t, map[string]string{
		"traceparent": "00-" + w3cTrace + "-" + w3cSpan + "-01",
		"baggage":     "tenant=acme",
	})
	if trace != w3cTrace || parent != w3cSpan {
		t.Fatalf("traceparent was not honoured behind a non-W3C global: trace %s parent %s", trace, parent)
	}
	if got := bag.Member("tenant").Value(); got != "acme" {
		t.Fatalf("W3C baggage fallback: got tenant=%q", got)
	}
}

// When the global finds a span context, a traceparent on the same request
// does not override it.
func TestTracing_GlobalSpanContextWinsOverTraceparent(t *testing.T) {
	withGlobalPropagator(t, testB3Propagator{})
	trace, parent, _ := serveTraced(t, map[string]string{
		"x-test-trace": upstreamTrace + "-" + upstreamSpan,
		"traceparent":  "00-" + w3cTrace + "-" + w3cSpan + "-01",
	})
	if trace != upstreamTrace || parent != upstreamSpan {
		t.Fatalf("the global's span context lost to traceparent: trace %s parent %s", trace, parent)
	}
}

// The W3C fallback keeps baggage the global extracted in its own format.
func TestTracing_FallbackKeepsGlobalBaggage(t *testing.T) {
	withGlobalPropagator(t, testB3Propagator{})
	_, _, bag := serveTraced(t, map[string]string{
		"x-test-bag":  "tenant=from-global",
		"traceparent": "00-" + w3cTrace + "-" + w3cSpan + "-01",
		"baggage":     "other=from-w3c",
	})
	if got := bag.Member("tenant").Value(); got != "from-global" {
		t.Fatalf("the global's baggage was overwritten by the W3C fallback: %v", bag)
	}
}

// No global configured (the otel no-op: no fields): W3C TraceContext and
// Baggage, as before, and a header only a custom propagator reads is not.
func TestTracing_NoGlobalPropagatorUnchanged(t *testing.T) {
	withGlobalPropagator(t, propagation.NewCompositeTextMapPropagator())
	trace, parent, bag := serveTraced(t, map[string]string{
		"traceparent": "00-" + w3cTrace + "-" + w3cSpan + "-01",
		"baggage":     "tenant=acme",
	})
	if trace != w3cTrace || parent != w3cSpan {
		t.Fatalf("traceparent not joined without a global: trace %s parent %s", trace, parent)
	}
	if got := bag.Member("tenant").Value(); got != "acme" {
		t.Fatalf("W3C baggage without a global: got tenant=%q", got)
	}
	trace, _, _ = serveTraced(t, map[string]string{"x-test-trace": upstreamTrace + "-" + upstreamSpan})
	if trace == upstreamTrace {
		t.Fatal("with no global configured, a non-W3C header joined the trace")
	}
}
