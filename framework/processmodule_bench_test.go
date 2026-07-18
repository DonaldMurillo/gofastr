package framework

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// This file is the §10.7 falsifiable transport gate (design #37 §10 item 7).
// It measures the steady-state per-call cost of proxying a small JSON
// module.http request through the REAL demo child over moduleproto/stdio,
// against an equivalent in-process handler, and reports the p95 DELTA — the
// transport tax the process boundary adds. The design's ≤ 2 ms p95 target is
// what we REPORT against; the assertion is deliberately generous (machine-
// dependent numbers must not flake CI): the run completes and the delta stays
// under a wide ceiling. A saturated variant runs the child at the 32-lease
// concurrency ceiling and asserts no collapse.
//
//	go test -run=NONE -bench ProxyLatency -benchtime=10000x ./framework/
//
// prints the steady-state ns/op for both paths.

// benchStoreCounter makes each bench-store DSN unique so concurrent bench
// runs (or the bench + a test) do not collide on the shared-cache DB name.
var benchStoreCounter atomic.Uint64

// benchStore is the testing.TB analog of newTestStore: a SQLite shared-cache
// :memory: store with SetMaxOpenConns(1) (the pattern the supervisor tests
// use). newTestStore takes *testing.T, so the benchmark (which has a
// *testing.B / testing.TB) rebuilds the same shape here.
func benchStore(tb testing.TB) *SQLProcessModuleStore {
	tb.Helper()
	dsn := fmt.Sprintf("file:pmstorebench%d?mode=memory&cache=shared", benchStoreCounter.Add(1))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		tb.Skipf("sqlite3 driver not available: %v", err)
	}
	db.SetMaxOpenConns(1)
	store, err := NewSQLProcessModuleStore(db)
	if err != nil {
		tb.Fatalf("new store: %v", err)
	}
	if err := store.EnsureSchema(context.Background()); err != nil {
		tb.Fatalf("ensure schema: %v", err)
	}
	tb.Cleanup(func() { _ = db.Close() })
	return store
}

// benchSupervisor brings up a demo child over /tree and returns a ready
// supervisor + the module name. Shared by the benchmark + the two _tests.
func benchSupervisor(tb testing.TB, env map[string]string) (*ProcessModuleSupervisor, string) {
	tb.Helper()
	sup := newGateSupervisor(tb, benchStore(tb), NopBroker{}, &demoRunner{env: env}, "bench")
	d := demoDescriptor(tb, []access.Permission{"articles:read"}, "", TrustTrusted)
	registerEnableReady(tb, sup, d, ApprovedGrants{"articles:read"})
	return sup, d.Name
}

// smallJSONBody is the bytes the demo's /tree route returns — a small valid
// ui.node.v1 tree (~0.5 KiB). The in-process baseline handler writes the same
// bytes so the proxy-vs-in-process comparison isolates the transport tax.
var smallJSONBody = mustDemoScreenTree()

func mustDemoScreenTree() []byte {
	// Mirrors examples/processmodule-demo/main.go screenTree() clean tree.
	return []byte(`{` +
		`"component":"card",` +
		`"props":{"title":"Demo module"},` +
		`"children":[` +
		`{"component":"heading","props":{"level":1,"text":"Hello from the demo module"}},` +
		`{"component":"paragraph","props":{"text":"This screen came from a validated ui.node.v1 tree returned by an out-of-process child over moduleproto."}},` +
		`{"component":"button","props":{"label":"Refresh","variant":"primary"},"action_ref":"refresh"}` +
		`]}`)
}

// inProcessBaseline is the equivalent in-process handler: it writes the same
// small JSON the proxied route produces, with no process boundary. The
// transport gate compares proxying against THIS, not against a no-op.
func inProcessBaseline(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Module", "demo")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(smallJSONBody)
}

// BenchmarkProxyLatency measures steady-state small-JSON module.http proxying
// through the real child. b.Loop() runs the body for the -benchtime budget
// (the verification step uses -benchtime=10000x for the ≥10k-call sample).
func BenchmarkProxyLatency(b *testing.B) {
	sup, name := benchSupervisor(b, nil)
	req := httptest.NewRequest(http.MethodGet, "/tree", nil)
	b.ResetTimer()
	for b.Loop() {
		rec := httptest.NewRecorder()
		sup.serveProxy(name, "tree", rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status = %d, want 200", rec.Code)
		}
	}
}

// BenchmarkProxyLatencyInProcess is the baseline: the equivalent work done by
// an in-process handler (no process boundary). Subtract this from
// [BenchmarkProxyLatency] to get the transport tax.
func BenchmarkProxyLatencyInProcess(b *testing.B) {
	h := http.HandlerFunc(inProcessBaseline)
	req := httptest.NewRequest(http.MethodGet, "/tree", nil)
	b.ResetTimer()
	for b.Loop() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status = %d", rec.Code)
		}
	}
}

// percentile returns the p-th percentile (0..100) of a sorted-ascending copy
// of ds. p95 is the design's reported figure.
func percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := make([]time.Duration, len(ds))
	copy(s, ds)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := int(float64(len(s)-1) * p / 100)
	return s[idx]
}

// TestGate_TransportLatency is the short, asserted version of item 7: it runs
// a bounded sample of proxied + in-process calls, computes the p95 DELTA (the
// transport tax), LOGS it (so a human sees the number against the ≤ 2 ms
// design target), and asserts only that the run completes and the delta stays
// under a generous, non-flaky ceiling. It is the "report, don't hard-fail CI"
// gate the design calls for.
func TestGate_TransportLatency(t *testing.T) {
	sup, name := benchSupervisor(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/tree", nil)

	const n = 3000
	proxyLat := make([]time.Duration, 0, n)
	inProcLat := make([]time.Duration, 0, n)
	h := http.HandlerFunc(inProcessBaseline)

	// Warm up (first call pays handshake-already-done + JIT; steady state is
	// what the gate measures).
	for range 50 {
		rec := httptest.NewRecorder()
		sup.serveProxy(name, "tree", rec, req)
	}

	for i := range n {
		start := time.Now()
		rec := httptest.NewRecorder()
		sup.serveProxy(name, "tree", rec, req)
		proxyLat = append(proxyLat, time.Since(start))
		if rec.Code != http.StatusOK {
			t.Fatalf("proxy iter %d: status = %d", i, rec.Code)
		}
	}
	for range n {
		start := time.Now()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		inProcLat = append(inProcLat, time.Since(start))
	}

	proxyP95 := percentile(proxyLat, 95)
	inProcP95 := percentile(inProcLat, 95)
	delta := proxyP95 - inProcP95
	t.Logf("transport-latency (n=%d): proxy p95=%s  in-process p95=%s  delta=%s  (design target: delta ≤ 2ms)",
		n, proxyP95, inProcP95, delta)

	// Generous ceiling: the process tax must be bounded, but the absolute
	// number is machine-dependent (CI runners, -race, container scheduling).
	// 50 ms is far above the design's ≤ 2 ms target yet still catches a
	// collapsed transport (a full-buffer + stdio + JSON round-trip that
	// regressed by ~25x). This is the "completes + stays bounded" assertion.
	const ceiling = 50 * time.Millisecond
	if delta > ceiling {
		t.Errorf("transport p95 delta = %s, want ≤ %s (transport regressed)", delta, ceiling)
	}
	if proxyP95 <= inProcP95 {
		t.Errorf("proxy p95 (%s) not greater than in-process p95 (%s) — baseline mismatch", proxyP95, inProcP95)
	}
}

// TestGate_TransportLatencySaturated runs the child at the 32-lease
// concurrency ceiling and asserts the transport does not collapse: every call
// completes with 200, and the p95 stays bounded under contention (no head-of-
// line blocking from the full-buffer-before-commit + stdio round-trips).
func TestGate_TransportLatencySaturated(t *testing.T) {
	sup, name := benchSupervisor(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/tree", nil)

	const concurrency = 32 // the v1 per-child lease ceiling
	const callsPerWorker = 400
	var wg sync.WaitGroup
	start := make(chan struct{})
	var (
		mu       sync.Mutex
		lat      []time.Duration
		failures int
	)
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]time.Duration, 0, callsPerWorker)
			<-start
			for range callsPerWorker {
				ts := time.Now()
				rec := httptest.NewRecorder()
				sup.serveProxy(name, "tree", rec, req)
				d := time.Since(ts)
				if rec.Code != http.StatusOK {
					mu.Lock()
					failures++
					mu.Unlock()
					continue
				}
				local = append(local, d)
			}
			mu.Lock()
			lat = append(lat, local...)
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	if failures != 0 {
		t.Errorf("saturated run: %d/%d calls failed (want 0 — no collapse at the 32-lease ceiling)", failures, concurrency*callsPerWorker)
	}
	if len(lat) == 0 {
		t.Fatal("saturated run: no successful calls recorded")
	}
	p95 := percentile(lat, 95)
	t.Logf("transport-latency saturated (concurrency=%d, calls=%d): p95=%s  (no-collapse bound: ≤ %s)",
		concurrency, len(lat), p95, 100*time.Millisecond)
	// 100 ms p95 under 32-way contention is a generous no-collapse bound; a
	// transport that serialized (head-of-line blocking) would blow past it.
	const ceiling = 100 * time.Millisecond
	if p95 > ceiling {
		t.Errorf("saturated p95 = %s, want ≤ %s (transport collapsed under contention)", p95, ceiling)
	}
}

// keep imports used even when -tags short skips the saturated test.
var _ = access.Permission("articles:read")
