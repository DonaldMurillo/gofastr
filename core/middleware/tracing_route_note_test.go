package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// A router behind a catch-all leaves r.Pattern empty or at the
// catch-all, so its handler names the route it matched and the span
// takes that name. A mux pattern that names one route wins over a note,
// the first note wins over a later one, and with no note the span keeps
// what the mux said, or "unmatched".
func TestNoteRouteNamesTheSpan(t *testing.T) {
	prev := otel.GetTracerProvider()
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	noting := func(routes ...string) http.Handler {
		return http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			for _, route := range routes {
				NoteRoute(r.Context(), route)
			}
		})
	}
	// As the app's router runs it: inside the mux, after the match.
	h := http.NewServeMux()
	h.Handle("GET /orders/{id}", Tracing()(noting("/ignored")))
	h.Handle("/", Tracing()(noting("/pages/{slug}", "/later")))
	h.Handle("/files/{path...}", Tracing()(noting("/files/by-name")))
	h.Handle("/static/", Tracing()(noting()))

	for _, c := range []struct{ path, name, route string }{
		{"/orders/42", "HTTP GET GET /orders/{id}", "GET /orders/{id}"},
		{"/pages/about", "HTTP GET /pages/{slug}", "/pages/{slug}"},
		{"/files/a/b.txt", "HTTP GET /files/by-name", "/files/by-name"},
		{"/static/app.css", "HTTP GET /static/", "/static/"},
	} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, c.path, nil))
		spans := rec.Ended()
		got := spans[len(spans)-1]
		var route string
		for _, kv := range got.Attributes() {
			if kv.Key == "http.route" {
				route = kv.Value.AsString()
			}
		}
		if got.Name() != c.name || route != c.route {
			t.Errorf("%s: span %q with http.route %q, want %q and %q", c.path, got.Name(), route, c.name, c.route)
		}
	}

	Tracing()(noting()).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	spans := rec.Ended()
	if got := spans[len(spans)-1].Name(); got != "HTTP GET unmatched" {
		t.Errorf("with no note: span %q", got)
	}
	// Outside the middleware a note goes nowhere, and does not panic.
	NoteRoute(httptest.NewRequest(http.MethodGet, "/x", nil).Context(), "/x")
}
