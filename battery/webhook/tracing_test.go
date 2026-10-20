package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// deliveryTransport is the default delivery client's *http.Transport
// under the tracing wrapper.
func deliveryTransport(mgr *Manager) (*http.Transport, bool) {
	rt := mgr.opts.HTTPClient.Transport
	if u, ok := rt.(interface{ Unwrap() http.RoundTripper }); ok {
		rt = u.Unwrap()
	}
	tr, ok := rt.(*http.Transport)
	return tr, ok
}

func TestDeliveryRecordsClientSpan(t *testing.T) {
	prev := otel.GetTracerProvider()
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	var traceparent atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent.Store(r.Header.Get("traceparent"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	mgr := newTestManager(t)
	defer mgr.Stop(context.Background())
	ctx := context.Background()
	if _, err := mgr.Subscribe(ctx, Subscriber{URL: srv.URL, Secret: "shh", Events: []string{"orders.created"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Publish(ctx, "orders.created", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		for _, s := range rec.Ended() {
			if s.Name() == "HTTP POST" {
				return true
			}
		}
		return false
	}, 2*time.Second, "no HTTP POST span for the delivery")
	if got, _ := traceparent.Load().(string); got != "" {
		t.Errorf("delivery sent traceparent %q to the subscriber", got)
	}
}
