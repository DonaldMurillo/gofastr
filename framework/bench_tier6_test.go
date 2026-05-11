package framework

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// Tier 6 — latency percentiles + concurrency
//
// Raw ns/op is a mean and hides tail behaviour. These benchmarks record per-
// operation latencies, sort them, and emit p50/p90/p99/p99.9 via
// b.ReportMetric so we can see what the slowest 1% look like under load.
//
// Concurrency is exercised via b.RunParallel with explicit
// b.SetParallelism. The reported throughput is whatever Go computes per
// b.N; the percentiles come from each goroutine's own measurements
// aggregated at the end.
// ============================================================================

// latencyRecorder collects per-op durations from one or more goroutines and
// reports the resulting distribution as p50/p90/p99/p99.9 metrics.
//
// Concurrency-safe via atomic append into a pre-sized slice. The slice is
// sized to b.N to avoid reallocations distorting timing.
type latencyRecorder struct {
	samples []time.Duration
	next    atomic.Int64
}

func newLatencyRecorder(n int) *latencyRecorder {
	return &latencyRecorder{samples: make([]time.Duration, n)}
}

// record appends a single sample. Drops samples beyond cap (shouldn't
// happen if cap == b.N, but harmless if it does).
func (l *latencyRecorder) record(d time.Duration) {
	idx := l.next.Add(1) - 1
	if int(idx) < len(l.samples) {
		l.samples[idx] = d
	}
}

// report computes the percentiles from collected samples and emits them as
// custom metrics. Called after b.StopTimer in the parent benchmark.
func (l *latencyRecorder) report(b *testing.B) {
	used := int(l.next.Load())
	if used > len(l.samples) {
		used = len(l.samples)
	}
	if used == 0 {
		return
	}
	samples := l.samples[:used]
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })

	pct := func(p float64) float64 {
		idx := int(float64(used-1) * p)
		return float64(samples[idx].Nanoseconds())
	}
	b.ReportMetric(pct(0.50), "p50_ns")
	b.ReportMetric(pct(0.90), "p90_ns")
	b.ReportMetric(pct(0.99), "p99_ns")
	b.ReportMetric(pct(0.999), "p999_ns")
	b.ReportMetric(float64(samples[used-1].Nanoseconds()), "max_ns")
}

// ----------------------------------------------------------------------------
// 6.1 — Read-heavy: GET /posts at varying concurrency
// ----------------------------------------------------------------------------

// BenchmarkT6_ListConcurrency drives a GET /posts list endpoint through
// b.RunParallel at parallelism multipliers 1, 8, 64. Reports percentiles
// per concurrency level so tail latency under load is visible.
func BenchmarkT6_ListConcurrency(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		app := setupBlogDomain(b, db, 500, 0)
		req := httptest.NewRequest(http.MethodGet, "/posts?limit=50", nil)

		for _, par := range []int{1, 8, 64} {
			par := par
			b.Run(fmt.Sprintf("parallelism=%d", par), func(b *testing.B) {
				rec := newLatencyRecorder(b.N + par*8)
				b.SetParallelism(par)
				b.ReportAllocs()
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						start := time.Now()
						w := httptest.NewRecorder()
						app.Router.ServeHTTP(w, req)
						rec.record(time.Since(start))
						if w.Code != http.StatusOK {
							b.Fatalf("status %d", w.Code)
						}
					}
				})
				b.StopTimer()
				rec.report(b)
			})
		}
	})
}

// ----------------------------------------------------------------------------
// 6.2 — Write-heavy: POST /posts at varying concurrency
// ----------------------------------------------------------------------------

// BenchmarkT6_CreateConcurrency drives POST /posts at parallelism 1, 8, 64.
// Writes hit the same DB so this also surfaces contention behaviour.
// SQLite serialises writers; Postgres won't, so the slope vs concurrency
// will differ markedly between dialects.
func BenchmarkT6_CreateConcurrency(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		app := setupBlogDomain(b, db, 0, 0)

		buildBody := func(i int64) []byte {
			body, _ := json.Marshal(map[string]any{
				"title": fmt.Sprintf("Bench post %d", i),
				"body":  "lorem ipsum",
			})
			return body
		}

		for _, par := range []int{1, 8, 64} {
			par := par
			b.Run(fmt.Sprintf("parallelism=%d", par), func(b *testing.B) {
				rec := newLatencyRecorder(b.N + par*8)
				var counter atomic.Int64
				b.SetParallelism(par)
				b.ReportAllocs()
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						id := counter.Add(1)
						body := buildBody(id)
						req := httptest.NewRequest(http.MethodPost, "/posts", bytesReader(body))
						req.Header.Set("Content-Type", "application/json")
						start := time.Now()
						w := httptest.NewRecorder()
						app.Router.ServeHTTP(w, req)
						rec.record(time.Since(start))
						if w.Code >= 400 {
							b.Fatalf("status %d: %s", w.Code, w.Body.String())
						}
					}
				})
				b.StopTimer()
				rec.report(b)
			})
		}
	})
}

// ----------------------------------------------------------------------------
// 6.3 — Mixed read/write at concurrency — closer to production traffic
// ----------------------------------------------------------------------------

// BenchmarkT6_MixedRW runs a 9:1 read:write mix at parallelism 8 and 64.
// Models realistic web traffic (most requests are reads). Reports
// percentiles + per-op throughput broken down by op type.
func BenchmarkT6_MixedRW(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		app := setupBlogDomain(b, db, 200, 0)
		listReq := httptest.NewRequest(http.MethodGet, "/posts?limit=20", nil)

		buildBody := func(i int64) []byte {
			body, _ := json.Marshal(map[string]any{
				"title": fmt.Sprintf("Mixed post %d", i),
			})
			return body
		}

		for _, par := range []int{8, 64} {
			par := par
			b.Run(fmt.Sprintf("parallelism=%d", par), func(b *testing.B) {
				rec := newLatencyRecorder(b.N + par*8)
				var counter atomic.Int64
				var reads, writes atomic.Int64

				b.SetParallelism(par)
				b.ReportAllocs()
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					i := 0
					for pb.Next() {
						i++
						start := time.Now()
						w := httptest.NewRecorder()
						if i%10 == 0 {
							// Write
							id := counter.Add(1)
							body := buildBody(id)
							req := httptest.NewRequest(http.MethodPost, "/posts", bytesReader(body))
							req.Header.Set("Content-Type", "application/json")
							app.Router.ServeHTTP(w, req)
							writes.Add(1)
							if w.Code >= 400 {
								b.Fatalf("write status %d", w.Code)
							}
						} else {
							// Read
							app.Router.ServeHTTP(w, listReq)
							reads.Add(1)
							if w.Code != http.StatusOK {
								b.Fatalf("read status %d", w.Code)
							}
						}
						rec.record(time.Since(start))
					}
				})
				b.StopTimer()
				rec.report(b)
				b.ReportMetric(float64(reads.Load()), "reads")
				b.ReportMetric(float64(writes.Load()), "writes")
			})
		}
	})
}
