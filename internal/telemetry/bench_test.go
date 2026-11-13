package telemetry_test

import (
	"log/slog"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/telemetry"
	"github.com/DonaldMurillo/gofastr/internal/telemetry/telemetrygen"
)

// The benchmarks run every query against one generated data set. By
// default it is small (1 request a second for a day, about 90 MB) so a
// stray -bench stays cheap. The design load is
//
//	TELEMETRY_BENCH_RPS=20 TELEMETRY_BENCH_DAYS=14 TELEMETRY_BENCH_DIR=/var/tmp/telemetry-bench \
//	    GOMAXPROCS=1 GOMEMLIMIT=64MiB go test ./internal/telemetry -run '^$' -bench . -benchmem
//
// which writes about 1.6 GB a day. With TELEMETRY_BENCH_DIR set, the
// data is kept and reused by the next run with the same load.
var bench struct {
	once  sync.Once
	store *telemetry.Store
	stats telemetrygen.Stats
	err   error
}

func envFloat(name string, def float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(name), 64); err == nil && v > 0 && v <= 1e5 {
		return v
	}
	return def
}

func benchStore(b *testing.B) (*telemetry.Store, telemetrygen.Stats) {
	b.Helper()
	bench.once.Do(func() {
		dir := os.Getenv("TELEMETRY_BENCH_DIR")
		if dir == "" {
			// Not b.TempDir: the data outlives the first benchmark.
			if dir, bench.err = os.MkdirTemp("", "telemetry-bench-"); bench.err != nil {
				return
			}
		}
		cfg := telemetry.Config{Dir: dir}
		load := telemetrygen.Load{
			RPS:  envFloat("TELEMETRY_BENCH_RPS", 1),
			Days: int(envFloat("TELEMETRY_BENCH_DAYS", 1)),
			End:  time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC),
			Seed: 1,
		}
		var ok bool
		if bench.stats, ok = telemetrygen.Existing(dir, load); !ok {
			start := time.Now()
			if bench.stats, bench.err = telemetrygen.Generate(cfg, load); bench.err != nil {
				return
			}
			b.Logf("generated in %s", time.Since(start).Round(time.Second))
		}
		bench.store, bench.err = telemetry.Open(cfg)
		st := bench.stats
		b.Logf("data: %g req/s for %d days in %s: %d log lines (%d MB), %d traces (%d MB)",
			st.RPS, st.Days, dir, st.LogLines, st.LogBytes>>20, st.Traces, st.TraceBytes>>20)
	})
	if bench.err != nil {
		b.Fatal(bench.err)
	}
	return bench.store, bench.stats
}

func TestMain(m *testing.M) {
	code := m.Run()
	if bench.store != nil && os.Getenv("TELEMETRY_BENCH_DIR") == "" {
		_ = os.RemoveAll(bench.store.Dir())
	}
	os.Exit(code)
}

func run(b *testing.B, wantAtLeast int, query func() (int, error)) {
	b.Helper()
	b.ReportAllocs()
	for b.Loop() {
		n, err := query()
		if err != nil {
			b.Fatal(err)
		}
		if n < wantAtLeast {
			b.Fatalf("query returned %d results, want at least %d", n, wantAtLeast)
		}
	}
}

// Target < 50 ms.
func BenchmarkTail100Warn(b *testing.B) {
	s, _ := benchStore(b)
	run(b, 100, func() (int, error) {
		es, err := s.TailLogs(100, slog.LevelWarn)
		return len(es), err
	})
}

// Target < 100 ms: the trace page asking for its own log lines.
func BenchmarkLogsForTrace(b *testing.B) {
	s, st := benchStore(b)
	sm := st.Samples[len(st.Samples)/2]
	run(b, 3, func() (int, error) {
		es, err := s.LogsForTrace(sm.TraceID, sm.Start, sm.End)
		return len(es), err
	})
}

// Target < 300 ms, with no time hint. The oldest day is the worst case:
// every newer file is read first.
func BenchmarkTraceByIDOldestDay(b *testing.B) {
	s, st := benchStore(b)
	run(b, 1, func() (int, error) {
		_, ok, err := s.TraceByID(st.Samples[0].TraceID)
		if !ok {
			return 0, err
		}
		return 1, err
	})
}

func BenchmarkTraceByIDNewestDay(b *testing.B) {
	s, st := benchStore(b)
	run(b, 1, func() (int, error) {
		_, ok, err := s.TraceByID(st.Samples[len(st.Samples)-1].TraceID)
		if !ok {
			return 0, err
		}
		return 1, err
	})
}

// Target < 300 ms.
func BenchmarkSlowest50Route24h(b *testing.B) {
	s, st := benchStore(b)
	run(b, 50, func() (int, error) {
		ts, err := s.SlowestTraces("GET /orders", st.Last.Add(-24*time.Hour), st.Last, 50)
		return len(ts), err
	})
}

// Target < 2 s. The needle is rare, so the whole day is read.
func BenchmarkSearchRare24h(b *testing.B) {
	s, st := benchStore(b)
	run(b, 1, func() (int, error) {
		es, err := s.SearchLogs(telemetrygen.Needle, st.Last.Add(-24*time.Hour), st.Last, 100000)
		return len(es), err
	})
}

// Target < 15 s over the full retention.
func BenchmarkSearchRareAllDays(b *testing.B) {
	s, st := benchStore(b)
	run(b, 1, func() (int, error) {
		es, err := s.SearchLogs(telemetrygen.Needle, st.First, st.Last, 100000)
		return len(es), err
	})
}

// Target < 1 s: how long a search takes to show its first page.
func BenchmarkSearchFirstPage(b *testing.B) {
	s, st := benchStore(b)
	run(b, 50, func() (int, error) {
		es, err := s.SearchLogs("cache miss", st.First, st.Last, 50)
		return len(es), err
	})
}
