package middleware

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestMetricsPreservesHijacker asserts the metrics writer keeps the
// underlying Hijacker (and Flusher) so a WS-upgrade handler behind the
// global metrics middleware keeps its upgrade path.
func TestMetricsPreservesHijacker(t *testing.T) {
	var sawHijacker, sawFlusher bool
	h := MetricsMiddleware(NewMetrics())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
		t.Fatal("Hijacker not preserved through metrics wrapper")
	}
	if !sawFlusher {
		t.Fatal("Flusher not preserved through metrics wrapper")
	}
}

// TestMetricsBoundsMethodCardinality asserts arbitrary RFC-7230 method
// tokens collapse to a single "other" label so an unauthenticated client
// can't grow the in-memory metrics store without bound.
func TestMetricsBoundsMethodCardinality(t *testing.T) {
	m := NewMetrics()
	h := MetricsMiddleware(m)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))

	// A known method survives as itself.
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	// 1000 distinct attacker-chosen method tokens must NOT each spawn a key.
	for _, tok := range []string{"FOOBAR", "X1", "X2", "ZZZZZ", "AAAA", "BBBB", "CCCC", "DDDD"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Method = tok
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	for i := range 1000 {
		req := httptest.NewRequest("GET", "/", nil)
		req.Method = "M" + strings.Repeat("z", i%7) + string(rune('A'+i%26)) + string(rune('0'+i%10))
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	m.mu.Lock()
	keys := len(m.counters)
	methods := map[string]struct{}{}
	for k := range m.counters {
		methods[k.Method] = struct{}{}
	}
	m.mu.Unlock()

	// Should be GET + "other" only, two distinct method labels.
	if _, ok := methods["other"]; !ok {
		t.Fatalf("expected unknown methods collapsed to \"other\"; got methods %v", methods)
	}
	if len(methods) > 2 {
		t.Fatalf("method cardinality unbounded: %d distinct method labels %v", len(methods), methods)
	}
	if keys > 4 {
		t.Fatalf("counter cardinality unbounded: %d keys after 1008 distinct methods", keys)
	}
}

// TestMetricsCollectorPanicIsolated pins panic isolation at the collector
// extension point. A third-party CollectorFunc runs with no recover, and the
// buffer is flushed only AFTER the loop, so one panicking collector dropped
// the whole /metrics scrape. The property (mirroring hook.runHookSafely):
// a bad collector is isolated, logged once, and the remaining families still
// land on the scrape.
func TestMetricsCollectorPanicIsolated(t *testing.T) {
	m := NewMetrics()
	m.RegisterCollector("good", func(w io.Writer) {
		fmt.Fprintln(w, "good_metric 42")
	})
	m.RegisterCollector("bad", func(w io.Writer) {
		panic("collector explosion")
	})
	m.RegisterCollector("also_good", func(w io.Writer) {
		fmt.Fprintln(w, "also_good_metric 7")
	})

	rec := httptest.NewRecorder()
	MetricsHandler(m).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200 (a panicking collector must not fail the scrape)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "good_metric 42") {
		t.Errorf("SECURITY: [availability] a good collector was lost when a sibling panicked:\n%s", body)
	}
	if !strings.Contains(body, "also_good_metric 7") {
		t.Errorf("SECURITY: [availability] collector AFTER the panicking one was lost:\n%s", body)
	}
}

// TestMetricsCollectorPanicDiscardsPartialOutput pins the second half of
// panic isolation: a collector that has already written a PARTIAL metric
// line into the shared exposition buffer before it panics must contribute
// NOTHING to the scrape. Without a per-collector buffer, the half-written
// line ("partial_metric{...} " with no value or newline) survived the
// recover and corrupted the output, a truncated line a Prometheus parser
// rejects, dropping families after it too. The good families still appear.
//
// Collectors run in SORTED name order, so the names are chosen to put
// "partial" in the MIDDLE (aaa_good < partial < zzz_also_good): a collector
// runs both before AND after the panicking one. With the old names
// (good/partial/also_good) "partial" sorted last, so the test proved nothing
// about a collector running after the panic.
func TestMetricsCollectorPanicDiscardsPartialOutput(t *testing.T) {
	m := NewMetrics()
	m.RegisterCollector("aaa_good", func(w io.Writer) {
		fmt.Fprintln(w, "good_metric 42")
	})
	m.RegisterCollector("partial", func(w io.Writer) {
		// Write a half-line (no value, no newline) then blow up.
		fmt.Fprint(w, "partial_metric{label=\"x\"} ")
		panic("mid-write explosion")
	})
	m.RegisterCollector("zzz_also_good", func(w io.Writer) {
		fmt.Fprintln(w, "also_good_metric 7")
	})

	rec := httptest.NewRecorder()
	MetricsHandler(m).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "good_metric 42") {
		t.Errorf("good collector lost:\n%s", body)
	}
	if !strings.Contains(body, "also_good_metric 7") {
		t.Errorf("collector after the panicking one lost:\n%s", body)
	}
	if strings.Contains(body, "partial_metric") {
		t.Errorf("SECURITY: partial output from a panicking collector leaked into the scrape:\n%s", body)
	}
}

// Property: every metrics label derived from a request is either
// allow-list bounded (method) or collapsed to the "unmatched" sentinel
// (route when no mux pattern matched) — no raw request path reaches the
// exposition, so neither label injection nor cardinality growth is
// reachable from the URL. Surfaces: percent-decoded CRLF paths,
// exposition-syntax paths, unicode paths, an 8 KB path, and forged
// methods; asserted on the scrape output as a whole (no C0/DEL).
func TestMetrics_UnmatchedPathsOneRouteLabel(t *testing.T) {
	m := NewMetrics()
	h := MetricsMiddleware(m)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	newRequest := func(method, target string) *http.Request {
		u, err := url.Parse(target)
		if err != nil {
			t.Fatalf("parse target %q: %v", target, err)
		}
		return &http.Request{
			Method:     method,
			URL:        u,
			Header:     http.Header{},
			Body:       http.NoBody,
			Host:       u.Host,
			RequestURI: target,
		}
	}
	deepPath := strings.Repeat("/seg", 2000)
	paths := []string{
		"/a%0d%0aX-Forged:%201",
		`/x"},evil="1`,
		`/unmatched/metric",status="200`,
		"/%C3%A9%E4%B8%AD",
		deepPath,
	}
	for _, p := range paths {
		h.ServeHTTP(httptest.NewRecorder(), newRequest(http.MethodGet, p))
	}
	for _, meth := range []string{"G\x01T", "FAKEMETHOD"} {
		h.ServeHTTP(httptest.NewRecorder(), newRequest(meth, "/m"))
	}

	scrape := httptest.NewRecorder()
	MetricsHandler(m).ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := scrape.Body.String()

	routes := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		const marker = `route="`
		i := strings.Index(line, marker)
		if i < 0 {
			continue
		}
		rest := line[i+len(marker):]
		routes[rest[:strings.IndexByte(rest[0:], '"')]] = true
	}
	if len(routes) != 1 || !routes["unmatched"] {
		t.Fatalf("expected the single sentinel route label %q, got %v", "unmatched", routes)
	}
	if strings.Contains(body, "/seg") {
		t.Error("raw request path leaked into the metrics exposition")
	}
	if i := strings.IndexFunc(body, func(r rune) bool {
		return (r < 0x20 && r != '\n') || r == 0x7f
	}); i >= 0 {
		t.Fatalf("control byte in exposition at offset %d: %q", i, body[i:])
	}
}
