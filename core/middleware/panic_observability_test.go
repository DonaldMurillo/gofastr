package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// A panicking request has to leave a trace an operator can follow: the
// stack of the code that panicked, a metrics sample, an error span.

//go:noinline
func panicSiteForStackTest() { panic("deep handler bug") }

// TestTimeoutPanicKeepsHandlerStack: Timeout runs the handler on its own
// goroutine and re-raised the panic VALUE on the request goroutine, so
// the stack Recovery logged was Timeout's re-raise site and never named
// the code that panicked. Timeout now captures the stack where the panic
// happened and Recovery logs that one.
func TestTimeoutPanicKeepsHandlerStack(t *testing.T) {
	logs := &captureHandler{}
	h := RecoveryFn(func() *slog.Logger { return slog.New(logs) })(Timeout(time.Minute)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panicSiteForStackTest() })))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	if got := logs.get("error"); got != "deep handler bug" {
		t.Fatalf("logged error %q, want the handler's panic value", got)
	}
	if stack := logs.get("stack"); !strings.Contains(stack, "panicSiteForStackTest") {
		t.Fatalf("logged stack does not name the panicking function:\n%s", stack)
	}
}

// TestTimeoutLatePanicLogsStack: a handler that panics after its 504 is
// logged from a watcher goroutine; that line carries the stack too.
func TestTimeoutLatePanicLogsStack(t *testing.T) {
	logs := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(logs))
	t.Cleanup(func() { slog.SetDefault(prev) })
	h := Timeout(30 * time.Millisecond)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		panicSiteForStackTest()
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && logs.get("stack") == "" {
		time.Sleep(5 * time.Millisecond)
	}
	if stack := logs.get("stack"); !strings.Contains(stack, "panicSiteForStackTest") {
		t.Fatalf("late-panic log stack does not name the panicking function:\n%s", stack)
	}
}

// serveRecovered serves h, recovering a re-raised panic the way an
// outer Recovery or net/http would, and returns the value it saw.
func serveRecovered(h http.Handler) (rec *httptest.ResponseRecorder, v any) {
	rec = httptest.NewRecorder()
	func() {
		defer func() { v = recover() }()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	}()
	return rec, v
}

// TestMetricsCountsPanickedRequest: MetricsMiddleware recorded after the
// handler returned, inline, so a panicking request (a 500 to the client)
// was missing from http_requests_total. It now records the 500, or the
// status already sent when the response had started, and re-raises the
// same panic value.
func TestMetricsCountsPanickedRequest(t *testing.T) {
	for name, tc := range map[string]struct {
		h    http.HandlerFunc
		want string
	}{
		"before the response": {func(http.ResponseWriter, *http.Request) { panic("boom") }, `status="500"`},
		"after a 201": {func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			panic("boom")
		}, `status="201"`},
	} {
		t.Run(name, func(t *testing.T) {
			m := NewMetrics()
			if _, v := serveRecovered(MetricsMiddleware(m)(tc.h)); v != "boom" {
				t.Fatalf("recovered %v, want the handler's panic value re-raised", v)
			}
			scrape := httptest.NewRecorder()
			MetricsHandler(m).ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			if !strings.Contains(scrape.Body.String(), tc.want) {
				t.Fatalf("panicked request not counted under %s:\n%s", tc.want, scrape.Body.String())
			}
		})
	}
}

// TestTracingMarksPanickedSpan: the span of a panicking request ended
// with no status code and no error status, though the client got a 500
// (or a cut connection). It now carries the status sent, Error, and one
// exception event (the SDK's End records the in-flight panic).
func TestTracingMarksPanickedSpan(t *testing.T) {
	for name, tc := range map[string]struct {
		h    http.HandlerFunc
		want int64
	}{
		"before the response": {func(http.ResponseWriter, *http.Request) { panic("boom") }, http.StatusInternalServerError},
		"after a 200": {func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			panic("boom")
		}, http.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			prev := otel.GetTracerProvider()
			sr := tracetest.NewSpanRecorder()
			otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr)))
			t.Cleanup(func() { otel.SetTracerProvider(prev) })

			if _, v := serveRecovered(Tracing()(tc.h)); v != "boom" {
				t.Fatalf("recovered %v, want the handler's panic value re-raised", v)
			}
			spans := sr.Ended()
			if len(spans) != 1 {
				t.Fatalf("spans = %d, want 1", len(spans))
			}
			span := spans[0]
			if span.Status().Code != codes.Error {
				t.Fatalf("span status %v, want Error", span.Status().Code)
			}
			var status int64
			for _, kv := range span.Attributes() {
				if kv.Key == "http.status_code" {
					status = kv.Value.AsInt64()
				}
			}
			if status != tc.want {
				t.Fatalf("span http.status_code = %d, want %d", status, tc.want)
			}
			exceptions := 0
			for _, ev := range span.Events() {
				if ev.Name == "exception" {
					exceptions++
				}
			}
			if exceptions != 1 {
				t.Fatalf("span has %d exception events for the panic, want 1", exceptions)
			}
		})
	}
}
