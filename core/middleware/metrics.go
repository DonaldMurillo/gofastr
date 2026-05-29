package middleware

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics tracks per-(method, route, status) request counters and per-route
// latency histograms. Exposes a Prometheus-compatible text endpoint at
// /metrics (or wherever the caller mounts MetricsHandler).
//
// Single instance per process; pass the *Metrics into MetricsMiddleware and
// MetricsHandler so both share the same store.
type Metrics struct {
	mu        sync.Mutex
	counters  map[metricKey]*atomic.Uint64
	durations map[string]*latencyHistogram
}

type metricKey struct {
	Method string
	Route  string
	Status int
}

// latencyHistogram is a fixed-bucket histogram per route. The bucket
// boundaries are millisecond breakpoints chosen for HTTP server traffic.
type latencyHistogram struct {
	mu      sync.Mutex
	count   uint64
	sumMS   float64
	buckets [9]uint64 // <=1ms, <=5, <=10, <=50, <=100, <=250, <=500, <=1000, >1000
}

var histogramBoundsMS = [...]float64{1, 5, 10, 50, 100, 250, 500, 1000}

// NewMetrics returns an empty Metrics store.
func NewMetrics() *Metrics {
	return &Metrics{
		counters:  map[metricKey]*atomic.Uint64{},
		durations: map[string]*latencyHistogram{},
	}
}

// MetricsMiddleware returns Middleware that records counters + latency for
// every served request. Route label uses r.Pattern when available (Go 1.22+
// ServeMux fills it in), falling back to the URL path so unmatched paths
// don't explode cardinality with raw user input.
func MetricsMiddleware(m *Metrics) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := &metricsResponseWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(ww, r)
			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			m.record(boundMethod(r.Method), route, ww.status, time.Since(start))
		})
	}
}

// knownMethods is the set of HTTP methods we keep as distinct metric label
// values. net/http accepts any RFC 7230 token as a request method, so the
// method dimension is attacker-controlled; collapsing everything outside this
// allow-list to a single "other" sentinel bounds metric cardinality and stops
// an unauthenticated client from growing the in-memory store without limit.
var knownMethods = map[string]struct{}{
	http.MethodGet:     {},
	http.MethodHead:    {},
	http.MethodPost:    {},
	http.MethodPut:     {},
	http.MethodPatch:   {},
	http.MethodDelete:  {},
	http.MethodConnect: {},
	http.MethodOptions: {},
	http.MethodTrace:   {},
}

// boundMethod maps any method outside the known-method allow-list to a single
// "other" sentinel so the metrics cardinality stays bounded.
func boundMethod(method string) string {
	if _, ok := knownMethods[method]; ok {
		return method
	}
	return "other"
}

func (m *Metrics) record(method, route string, status int, dur time.Duration) {
	key := metricKey{Method: method, Route: route, Status: status}
	m.mu.Lock()
	c, ok := m.counters[key]
	if !ok {
		c = new(atomic.Uint64)
		m.counters[key] = c
	}
	h, ok := m.durations[route]
	if !ok {
		h = &latencyHistogram{}
		m.durations[route] = h
	}
	m.mu.Unlock()
	c.Add(1)
	h.observe(float64(dur.Microseconds()) / 1000.0)
}

func (h *latencyHistogram) observe(ms float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
	h.sumMS += ms
	for i, b := range histogramBoundsMS {
		if ms <= b {
			h.buckets[i]++
			return
		}
	}
	h.buckets[len(h.buckets)-1]++
}

// MetricsHandler returns an http.Handler that serves the metrics in
// Prometheus text exposition format. Mount at /metrics or anywhere the
// scrape config points.
func MetricsHandler(m *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		var sb strings.Builder

		// http_requests_total — counter per (method, route, status).
		sb.WriteString("# HELP http_requests_total Number of HTTP requests served.\n")
		sb.WriteString("# TYPE http_requests_total counter\n")

		m.mu.Lock()
		keys := make([]metricKey, 0, len(m.counters))
		for k := range m.counters {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].Method != keys[j].Method {
				return keys[i].Method < keys[j].Method
			}
			if keys[i].Route != keys[j].Route {
				return keys[i].Route < keys[j].Route
			}
			return keys[i].Status < keys[j].Status
		})
		for _, k := range keys {
			n := m.counters[k].Load()
			fmt.Fprintf(&sb, "http_requests_total{method=%q,route=%q,status=\"%d\"} %d\n",
				k.Method, k.Route, k.Status, n)
		}

		// http_request_duration_ms — histogram per route.
		sb.WriteString("# HELP http_request_duration_ms Request latency in ms.\n")
		sb.WriteString("# TYPE http_request_duration_ms histogram\n")
		routes := make([]string, 0, len(m.durations))
		for r := range m.durations {
			routes = append(routes, r)
		}
		sort.Strings(routes)
		for _, r := range routes {
			h := m.durations[r]
			h.mu.Lock()
			cumulative := uint64(0)
			for i, b := range histogramBoundsMS {
				cumulative += h.buckets[i]
				fmt.Fprintf(&sb, "http_request_duration_ms_bucket{route=%q,le=\"%g\"} %d\n", r, b, cumulative)
			}
			cumulative += h.buckets[len(h.buckets)-1]
			fmt.Fprintf(&sb, "http_request_duration_ms_bucket{route=%q,le=\"+Inf\"} %d\n", r, cumulative)
			fmt.Fprintf(&sb, "http_request_duration_ms_sum{route=%q} %g\n", r, h.sumMS)
			fmt.Fprintf(&sb, "http_request_duration_ms_count{route=%q} %d\n", r, h.count)
			h.mu.Unlock()
		}
		m.mu.Unlock()

		w.Write([]byte(sb.String()))
	})
}

// metricsResponseWriter captures the response status code so the recording
// middleware can label by it. Wraps the inner ResponseWriter transparently.
type metricsResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *metricsResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *metricsResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Flush forwards to the underlying ResponseWriter's Flusher if it has one.
// Without this, wrapping breaks any handler that streams (SSE, chunked JSON,
// long-poll), because the SSE constructor type-asserts http.Flusher and
// panics otherwise. Found the hard way by the full-stack E2E test that
// exercised /posts/_events through the metrics middleware.
func (w *metricsResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards to the underlying ResponseWriter's Hijacker if it has one.
// Without this, wrapping breaks any handler that performs a WebSocket upgrade
// or otherwise type-asserts http.Hijacker (e.g. core/stream/websocket.go),
// because the assertion would see the wrapper instead of the real writer.
func (w *metricsResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Push forwards to the underlying ResponseWriter's Pusher if it has one.
func (w *metricsResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pu, ok := w.ResponseWriter.(http.Pusher); ok {
		return pu.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Unwrap exposes the wrapped writer to net/http's ResponseController so it
// can reach optional interfaces this wrapper doesn't re-expose directly.
func (w *metricsResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
