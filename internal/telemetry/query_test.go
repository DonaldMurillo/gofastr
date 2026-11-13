package telemetry_test

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/telemetry"
	"github.com/DonaldMurillo/gofastr/internal/telemetry/telemetrygen"
)

// The reference answers every query the slow, obvious way: decode every
// line of every file. The store must agree with it.
type refLog struct {
	Time    time.Time  `json:"time"`
	Level   slog.Level `json:"level"`
	TraceID string     `json:"trace_id"`
	raw     string
}

type refTrace struct {
	Time    time.Time `json:"time"`
	TraceID string    `json:"trace_id"`
	Route   string    `json:"route"`
	Status  int       `json:"status"`
	DurUS   int64     `json:"dur_us"`
	Error   bool      `json:"error"`
	raw     string
}

type fixture struct {
	store  *telemetry.Store
	stats  telemetrygen.Stats
	logs   []refLog // oldest first
	traces []refTrace
}

func readLines(t testing.TB, dir, kind string, each func(line []byte)) {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, kind+"-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(names)
	for _, name := range names {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(nil, 4<<20)
		for sc.Scan() {
			each(sc.Bytes())
		}
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	cfg := telemetry.Config{Dir: t.TempDir()}
	stats, err := telemetrygen.Generate(cfg, telemetrygen.Load{
		RPS: 0.1, Days: 3, End: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC), Seed: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := telemetry.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	fx := &fixture{store: store, stats: stats}
	readLines(t, cfg.Dir, telemetry.KindLogs, func(line []byte) {
		var l refLog
		if err := json.Unmarshal(line, &l); err != nil {
			t.Fatalf("log line: %v", err)
		}
		l.raw = string(line)
		fx.logs = append(fx.logs, l)
	})
	readLines(t, cfg.Dir, telemetry.KindTraces, func(line []byte) {
		var tr refTrace
		if err := json.Unmarshal(line, &tr); err != nil {
			t.Fatalf("trace line: %v", err)
		}
		tr.raw = string(line)
		fx.traces = append(fx.traces, tr)
	})
	if int64(len(fx.logs)) != stats.LogLines || int64(len(fx.traces)) != stats.Traces || len(fx.traces) == 0 {
		t.Fatalf("files hold %d logs, %d traces; stats say %d, %d", len(fx.logs), len(fx.traces), stats.LogLines, stats.Traces)
	}
	return fx
}

func raws(es []telemetry.LogEntry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = string(e.Line)
	}
	return out
}

func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(want) == 0 {
		t.Fatalf("%s: the reference found nothing, so the check proves nothing", what)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%s: got %d lines, want %d\nfirst got:  %.200s\nfirst want: %.200s",
			what, len(got), len(want), strings.Join(got[:min(1, len(got))], ""), want[0])
	}
}

func TestQueriesMatchFullRead(t *testing.T) {
	fx := newFixture(t)
	// A window that starts and ends mid-file and crosses midnight.
	from := fx.stats.First.Add(19*time.Hour + 7*time.Minute)
	to := fx.stats.First.Add(31*time.Hour + 3*time.Second)

	t.Run("tail", func(t *testing.T) {
		got, err := fx.store.TailLogs(40, slog.LevelWarn)
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, l := range slices.Backward(fx.logs) {
			if l.Level >= slog.LevelWarn && len(want) < 40 {
				want = append(want, l.raw)
			}
		}
		slices.Reverse(want)
		equal(t, "tail", raws(got), want)
		if got[0].Level < slog.LevelWarn || got[0].Time.IsZero() {
			t.Fatalf("entry fields not filled: %+v", got[0])
		}
	})

	t.Run("search", func(t *testing.T) {
		for _, c := range []struct {
			text  string
			limit int
		}{{"cache miss", 1 << 30}, {"cache miss", 5}, {"/customers", 1 << 30}} {
			got, err := fx.store.SearchLogs(c.text, from, to, c.limit)
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, l := range slices.Backward(fx.logs) {
				if strings.Contains(l.raw, c.text) && !l.Time.Before(from) && !l.Time.After(to) && len(want) < c.limit {
					want = append(want, l.raw)
				}
			}
			equal(t, "search "+c.text, raws(got), want)
		}
	})

	t.Run("logs for trace", func(t *testing.T) {
		for _, s := range fx.stats.Samples {
			got, err := fx.store.LogsForTrace(s.TraceID, s.Start, s.End)
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, l := range fx.logs {
				if l.TraceID == s.TraceID {
					want = append(want, l.raw)
				}
			}
			equal(t, "logs for trace", raws(got), want)
		}
		for _, bad := range []string{`x","level":"`, strings.Repeat("a", 31), strings.Repeat("a", 33), strings.Repeat("A", 32)} {
			if _, err := fx.store.LogsForTrace(bad, from, to); err != telemetry.ErrBadTraceID {
				t.Fatalf("trace ID %q: err = %v", bad, err)
			}
		}
	})

	t.Run("trace by id", func(t *testing.T) {
		for _, want := range []refTrace{fx.traces[0], fx.traces[len(fx.traces)/2], fx.traces[len(fx.traces)-1]} {
			got, ok, err := fx.store.TraceByID(want.TraceID)
			if err != nil || !ok || string(got) != want.raw {
				t.Fatalf("TraceByID(%s) = %.80s, %v, %v", want.TraceID, got, ok, err)
			}
		}
		if _, ok, err := fx.store.TraceByID(strings.Repeat("0", 32)); ok || err != nil {
			t.Fatalf("unknown ID: ok=%v err=%v", ok, err)
		}
	})

	t.Run("slowest", func(t *testing.T) {
		for _, route := range []string{"GET /", ""} {
			got, err := fx.store.SlowestTraces(route, from, to, 10)
			if err != nil {
				t.Fatal(err)
			}
			var ref []refTrace
			for _, tr := range fx.traces {
				if (route == "" || tr.Route == route) && !tr.Time.Before(from) && !tr.Time.After(to) {
					ref = append(ref, tr)
				}
			}
			slices.SortFunc(ref, func(a, b refTrace) int {
				return cmp.Or(cmp.Compare(b.DurUS, a.DurUS), b.Time.Compare(a.Time))
			})
			ref = ref[:min(len(ref), 10)]
			if len(ref) < 10 || len(got) != len(ref) {
				t.Fatalf("route %q: got %d, reference has %d", route, len(got), len(ref))
			}
			for i, w := range ref {
				g := got[i]
				if g.TraceID != w.TraceID || g.Route != w.Route || g.Status != w.Status || g.Error != w.Error ||
					g.Dur != time.Duration(w.DurUS)*time.Microsecond || !g.Time.Equal(w.Time) {
					t.Fatalf("route %q #%d: got %+v, want %+v", route, i, g, w)
				}
			}
		}
	})
}

// A reader must survive what a live file can hold: a line still being
// written, a line too long to keep, and bytes that are not JSON.
func TestReadersSkipTornAndOversizedLines(t *testing.T) {
	dir := t.TempDir()
	day := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	tid := strings.Repeat("ab", 16)
	line := func(sec int, msg string) string {
		return `{"time":"` + day.Add(time.Duration(sec)*time.Second).Format(time.RFC3339Nano) + `","trace_id":"` + tid + `","level":"INFO","msg":"` + msg + `"}` + "\n"
	}
	var b bytes.Buffer
	b.WriteString(line(1, "first"))
	b.WriteString("not json at all\n")
	b.WriteString(line(2, strings.Repeat("x", 3<<20))) // over the line cap
	b.WriteString(line(3, "third"))
	b.WriteString(strings.TrimSuffix(line(4, "torn"), "}\n")) // no newline yet
	if err := os.WriteFile(filepath.Join(dir, telemetry.FileName(telemetry.KindLogs, day)), b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := telemetry.Open(telemetry.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	want := []string{strings.TrimSpace(line(1, "first")), strings.TrimSpace(line(3, "third"))}
	tail, err := store.TailLogs(10, slog.LevelDebug)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "tail", raws(tail), want)

	found, err := store.SearchLogs("msg", day, day.Add(time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(want)
	equal(t, "search", raws(found), want)

	// The forward reader meets the same three lines, each carrying the ID.
	slices.Reverse(want)
	traced, err := store.LogsForTrace(tid, day, day.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "logs for trace", raws(traced), want)
}

func TestConfigResolve(t *testing.T) {
	got, err := telemetry.Config{Dir: "x"}.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	want := telemetry.Config{Dir: "x", Retention: telemetry.DefaultRetention, MaxDiskBytes: telemetry.DefaultMaxDiskBytes,
		SampleRate: telemetry.DefaultSampleRate, SlowThreshold: telemetry.DefaultSlowThreshold, MaxMemoryBytes: telemetry.DefaultMaxMemoryBytes}
	if got != want {
		t.Fatalf("defaults: got %+v", got)
	}
	set := telemetry.Config{Dir: "x", Retention: time.Hour, MaxDiskBytes: 1, SampleRate: 1, SlowThreshold: time.Second, MaxMemoryBytes: 2}
	if got, err := set.Resolve(); err != nil || got != set {
		t.Fatalf("explicit settings changed: %+v, %v", got, err)
	}
	if got, _ := (telemetry.Config{Dir: "x", SampleRate: 0.5, KeepNoOrdinary: true}).Resolve(); got.SampleRate != 0 {
		t.Fatalf("KeepNoOrdinary left SampleRate at %v", got.SampleRate)
	}
	nan := 0.0
	nan /= nan
	for name, bad := range map[string]telemetry.Config{
		"no dir":    {},
		"retention": {Dir: "x", Retention: -1},
		"disk":      {Dir: "x", MaxDiskBytes: -1},
		"rate low":  {Dir: "x", SampleRate: -0.1},
		"rate high": {Dir: "x", SampleRate: 1.1},
		"rate nan":  {Dir: "x", SampleRate: nan},
		"slow":      {Dir: "x", SlowThreshold: -1},
		"memory":    {Dir: "x", MaxMemoryBytes: -1},
	} {
		if _, err := bad.Resolve(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
