package tracing

import (
	"net/http"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// TransportOption configures Transport.
type TransportOption func(*transport)

// PropagateTraceContext sends a W3C traceparent header with each request.
func PropagateTraceContext() TransportOption {
	return func(t *transport) { t.propagate = true }
}

// Transport wraps base (http.DefaultTransport when nil) so each request gets
// a client span named "HTTP <method>" that ends when the response headers
// arrive, failed on a transport error or a 4xx or 5xx status.
func Transport(base http.RoundTripper, opts ...TransportOption) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	t := &transport{base: base}
	for _, o := range opts {
		o(t)
	}
	return t
}

type transport struct {
	base      http.RoundTripper
	propagate bool
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	method := textsafe.ScrubControlBytes(req.Method)
	if method == "" {
		method = http.MethodGet
	}
	ctx, span := tracer().Start(req.Context(), "HTTP "+method, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	span.SetAttributes(
		attribute.String("http.request.method", method),
		attribute.String("url.full", textsafe.ScrubControlBytes(bareURL(req))),
		attribute.String("server.address", textsafe.ScrubControlBytes(req.URL.Hostname())),
	)
	out := req.WithContext(ctx)
	if t.propagate {
		out = req.Clone(ctx)
		propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(out.Header))
	}
	resp, err := t.base.RoundTrip(out)
	if err != nil {
		markError(span, err)
		return resp, err
	}
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
	if resp.StatusCode >= 400 {
		span.SetAttributes(attribute.String("error.type", strconv.Itoa(resp.StatusCode)))
		span.SetStatus(codes.Error, "")
	}
	return resp, nil
}

func (t *transport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

func (t *transport) Unwrap() http.RoundTripper { return t.base }

func bareURL(req *http.Request) string {
	u := *req.URL
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}
