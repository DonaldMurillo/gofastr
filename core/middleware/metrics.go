package middleware

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// Metrics holds per-(method, route, status) request counters, per-route latency histograms and subsystem collectors, served by MetricsHandler.
type Metrics struct {
	mu         sync.Mutex
	counters   map[metricKey]*atomic.Uint64
	durations  map[string]*latencyHistogram
	collectors map[string]CollectorFunc
}

type metricKey struct {
	Method string
	Route  string
	Status int
}

type latencyHistogram struct {
	mu      sync.Mutex
	count   uint64
	sumMS   float64
	buckets [9]uint64
}

var histogramBoundsMS = [...]float64{1, 5, 10, 50, 100, 250, 500, 1000}

// NewMetrics returns an empty Metrics store.
func NewMetrics() *Metrics {
	return &Metrics{
		counters:   map[metricKey]*atomic.Uint64{},
		durations:  map[string]*latencyHistogram{},
		collectors: map[string]CollectorFunc{},
	}
}

// CollectorFunc writes Prometheus text lines for a subsystem into w once per scrape; it must be cheap and safe for concurrent calls.
type CollectorFunc func(w io.Writer)

// RegisterCollector adds or replaces a named collector on the /metrics output; a nil fn removes it.
func (m *Metrics) RegisterCollector(name string, fn CollectorFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fn == nil {
		delete(m.collectors, name)
		return
	}
	m.collectors[name] = fn
}

// MetricsMiddleware returns middleware that counts every request and records its latency, labelled by r.Pattern or "unmatched".
func MetricsMiddleware(m *Metrics) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := newStatusWriter(w)
			record := func(status int) {
				route := r.Pattern
				if route == "" {
					route = "unmatched"
				}
				m.record(boundMethod(r.Method), route, status, time.Since(start))
			}
			defer func() {
				if v := recover(); v != nil {
					record(panickedStatus(w, ww.wroteHeader, ww.status))
					panic(v)
				}
			}()
			next.ServeHTTP(ww, r)
			record(ww.status)
		})
	}
}

func panickedStatus(w http.ResponseWriter, committed bool, status int) int {
	for cur := w; cur != nil; {
		if rw, ok := cur.(*recoveryWriter); ok {
			switch {
			case !rw.started:
				return http.StatusInternalServerError
			case rw.status != 0:
				return rw.status
			}
			break
		}
		u, ok := cur.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		cur = u.Unwrap()
	}
	if committed {
		return status
	}
	return http.StatusInternalServerError
}

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

// MetricsHandler serves m in Prometheus text exposition format.
func MetricsHandler(m *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		var sb strings.Builder

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
		type namedCollector struct {
			name string
			fn   CollectorFunc
		}
		snapshot := make([]namedCollector, 0, len(m.collectors))
		for name, fn := range m.collectors {
			snapshot = append(snapshot, namedCollector{name, fn})
		}
		m.mu.Unlock()
		sort.Slice(snapshot, func(i, j int) bool { return snapshot[i].name < snapshot[j].name })
		for _, c := range snapshot {
			if out := runCollectorSafely(c.name, c.fn); out != nil {
				sb.Write(out)
			}
		}

		w.Write([]byte(sb.String()))
	})
}

func runCollectorSafely(name string, fn CollectorFunc) (out []byte) {
	defer func() {
		if r := recover(); r != nil {
			out = nil
			slog.Error("metrics collector panic isolated",
				"collector", name,
				"error", textsafe.Recovered(r),
			)
		}
	}()
	var buf bytes.Buffer
	fn(&buf)
	return buf.Bytes()
}
