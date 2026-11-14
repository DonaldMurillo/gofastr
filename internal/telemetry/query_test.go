package telemetry_test

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
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
	Err     string    `json:"err"`
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

func newFixture(t *testing.T) *fixture { return newFixtureSealed(t, 0) }

// newFixtureSealed generates three days and seals the oldest n of them
// after the reference has read the plain files, so one reference checks
// plain days, sealed days and a mix.
func newFixtureSealed(t *testing.T, seal int) *fixture {
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
	for d := range seal {
		day := stats.First.Add(time.Duration(d) * 24 * time.Hour)
		for _, kind := range []string{telemetry.KindLogs, telemetry.KindTraces} {
			if err := telemetry.SealDay(cfg.Dir, kind, day); err != nil {
				t.Fatalf("seal %s %s: %v", kind, day.Format("2006-01-02"), err)
			}
			if _, err := os.Stat(filepath.Join(cfg.Dir, telemetry.FileName(kind, day))); !os.IsNotExist(err) {
				t.Fatalf("the plain %s file for a sealed day is still there: %v", kind, err)
			}
		}
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

// Every query gives the same answer whether a day is a plain file or
// sealed into blocks: with none sealed, with the two finished days
// sealed, and with all three.
func TestQueriesMatchFullRead(t *testing.T) {
	for sealed, name := range map[int]string{0: "plain", 2: "older days sealed", 3: "all sealed"} {
		t.Run(name, func(t *testing.T) { queriesMatchFullRead(t, newFixtureSealed(t, sealed)) })
	}
}

func queriesMatchFullRead(t *testing.T, fx *fixture) {
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
		}{{`"cache miss"`, 1 << 30}, {`"cache miss"`, 5}, {"/customers", 1 << 30}} {
			got, err := fx.store.SearchLogs(c.text, from, to, c.limit)
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, l := range slices.Backward(fx.logs) {
				if strings.Contains(l.raw, strings.Trim(c.text, `"`)) && !l.Time.Before(from) && !l.Time.After(to) && len(want) < c.limit {
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

	t.Run("logs", func(t *testing.T) {
		for _, q := range []telemetry.LogQuery{
			{},
			{MinLevel: slog.LevelError},
			{Text: `"cache miss"`, MinLevel: slog.LevelInfo, From: from, To: to},
			{From: from, To: to},
		} {
			for _, limit := range []int{7, 1 << 30} {
				if limit > 7 && q.From.IsZero() {
					continue // the whole data set, already covered by the windowed cases
				}
				got, err := fx.store.Logs(q, limit)
				if err != nil {
					t.Fatal(err)
				}
				var want []string
				for _, l := range slices.Backward(fx.logs) {
					if l.Level >= q.MinLevel && strings.Contains(l.raw, strings.Trim(q.Text, `"`)) &&
						(q.From.IsZero() || !l.Time.Before(q.From)) && (q.To.IsZero() || !l.Time.After(q.To)) && len(want) < limit {
						want = append(want, l.raw)
					}
				}
				equal(t, fmt.Sprintf("logs %+v limit %d", q, limit), raws(got), want)
			}
		}
		// A search out of time says where it stopped, returns what it
		// had found, and the same search bounded there carries on: the
		// two halves are the whole answer, with nothing twice.
		whole, _ := fx.store.Logs(telemetry.LogQuery{Text: "export", MinLevel: slog.LevelDebug}, 1<<30)
		first, stopped, err := fx.store.LogsWithin(telemetry.LogQuery{Text: "export", MinLevel: slog.LevelDebug}, 1<<30, time.Nanosecond)
		if err != nil || stopped.IsZero() || len(first) >= len(whole) {
			t.Fatalf("a search with no time: %d of %d lines, stopped %v, %v", len(first), len(whole), stopped, err)
		}
		rest, again, err := fx.store.LogsWithin(telemetry.LogQuery{Text: "export", MinLevel: slog.LevelDebug, To: stopped}, 1<<30, 0)
		if err != nil || !again.IsZero() {
			t.Fatalf("the rest of the search: stopped %v, %v", again, err)
		}
		joined := raws(first)
		for _, r := range raws(rest) {
			if !slices.Contains(joined, r) { // the line it stopped on is in both
				joined = append(joined, r)
			}
		}
		equal(t, "a search finished in two goes", joined, raws(whole))
		if _, stopped, _ := fx.store.LogsWithin(telemetry.LogQuery{Text: "export"}, 1<<30, time.Hour); !stopped.IsZero() {
			t.Fatal("a search with time to spare says it stopped")
		}

		// Skipped path prefixes drop their access lines and nothing else.
		skipped, err := fx.store.Logs(telemetry.LogQuery{MinLevel: slog.LevelDebug, From: from, To: to, SkipPaths: []string{"/orders", "/customers/"}}, 1<<30)
		if err != nil {
			t.Fatal(err)
		}
		var kept []string
		for _, l := range slices.Backward(fx.logs) {
			if !l.Time.Before(from) && !l.Time.After(to) && !strings.Contains(l.raw, `"path":"/orders"`) && !strings.Contains(l.raw, `"path":"/orders/`) && !strings.Contains(l.raw, `"path":"/customers/`) {
				kept = append(kept, l.raw)
			}
		}
		if len(kept) == len(fx.logs) {
			t.Fatal("the reference skipped nothing, so the check proves nothing")
		}
		equal(t, "logs with skipped paths", raws(skipped), kept)

		// Oldest first takes the limit from the old end and still
		// answers newest first: the page of lines after a point.
		for _, q := range []telemetry.LogQuery{{OldestFirst: true, From: from, To: to}, {OldestFirst: true, MinLevel: slog.LevelWarn, From: from}} {
			got, err := fx.store.Logs(q, 9)
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, l := range fx.logs {
				if l.Level >= q.MinLevel && !l.Time.Before(q.From) && (q.To.IsZero() || !l.Time.After(q.To)) && len(want) < 9 {
					want = append(want, l.raw)
				}
			}
			slices.Reverse(want)
			equal(t, fmt.Sprintf("oldest-first logs %+v", q), raws(got), want)
		}
		// Paging older then newer lands back on the same lines.
		top, _ := fx.store.Logs(telemetry.LogQuery{}, 5)
		older, _ := fx.store.Logs(telemetry.LogQuery{To: top[4].Time.Add(-time.Nanosecond)}, 5)
		back, err := fx.store.Logs(telemetry.LogQuery{From: older[0].Time.Add(time.Nanosecond), OldestFirst: true}, 5)
		if err != nil || !slices.Equal(raws(back), raws(top)) {
			t.Fatalf("older then newer did not return to the first page: %v", err)
		}
		// The next page: the same query, ending just before the last line shown.
		page1, _ := fx.store.Logs(telemetry.LogQuery{MinLevel: slog.LevelWarn}, 5)
		second, err := fx.store.Logs(telemetry.LogQuery{MinLevel: slog.LevelWarn, To: page1[4].Time.Add(-time.Nanosecond)}, 5)
		if err != nil || len(second) != 5 || !second[0].Time.Before(page1[4].Time) {
			t.Fatalf("second page: %d lines, %v", len(second), err)
		}
	})

	t.Run("traces", func(t *testing.T) {
		for _, q := range []telemetry.TraceQuery{
			{},
			{Failed: true},
			{MinDur: 100 * time.Millisecond},
			{Route: "GET /orders", From: from, To: to},
			{Route: "GET /", Failed: true, From: from, To: to},
		} {
			got, err := fx.store.Traces(q, 25)
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, tr := range slices.Backward(fx.traces) {
				if (q.Route == "" || tr.Route == q.Route) && (!q.Failed || tr.Error) && tr.DurUS >= q.MinDur.Microseconds() &&
					(q.From.IsZero() || !tr.Time.Before(q.From)) && (q.To.IsZero() || !tr.Time.After(q.To)) && len(want) < 25 {
					want = append(want, tr.TraceID)
				}
			}
			ids := make([]string, len(got))
			for i, g := range got {
				ids[i] = g.TraceID
			}
			equal(t, fmt.Sprintf("traces %+v", q), ids, want)
		}
	})

	t.Run("traces oldest first", func(t *testing.T) {
		for _, q := range []telemetry.TraceQuery{{OldestFirst: true}, {OldestFirst: true, Failed: true, From: from, To: to}} {
			got, err := fx.store.Traces(q, 7)
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, tr := range fx.traces {
				if (!q.Failed || tr.Error) && (q.From.IsZero() || !tr.Time.Before(q.From)) && (q.To.IsZero() || !tr.Time.After(q.To)) && len(want) < 7 {
					want = append(want, tr.TraceID)
				}
			}
			slices.Reverse(want)
			ids := make([]string, len(got))
			for i, g := range got {
				ids[i] = g.TraceID
			}
			equal(t, fmt.Sprintf("oldest-first traces %+v", q), ids, want)
		}
	})

	t.Run("issues", func(t *testing.T) {
		// The reference knows the generator's three kinds of failure
		// by how each begins, and groups by route, status and kind.
		kind := func(err string) string {
			for _, k := range []string{"upstream timed out", "dial tcp", "pq: deadlock"} {
				if strings.HasPrefix(err, k) {
					return k
				}
			}
			t.Fatalf("a failure the reference does not know: %q", err)
			return ""
		}
		type group struct {
			count       int
			first, last time.Time
			lastID      string
		}
		want := map[string]*group{}
		for _, tr := range fx.traces {
			if !tr.Error || tr.Time.Before(from) || tr.Time.After(to) {
				continue
			}
			k := fmt.Sprintf("%s|%d|%s", tr.Route, tr.Status, kind(tr.Err))
			g := want[k]
			if g == nil {
				g = &group{first: tr.Time}
				want[k] = g
			}
			g.count++
			g.last, g.lastID = tr.Time, tr.TraceID
		}
		got, leftOut, err := fx.store.Issues(from, to, 1<<30)
		if err != nil || leftOut != 0 || len(got) != len(want) || len(got) < 10 {
			t.Fatalf("%d issues (%d left out, %v), the reference has %d", len(got), leftOut, err, len(want))
		}
		for i, is := range got {
			w := want[fmt.Sprintf("%s|%d|%s", is.Route, is.Status, kind(is.Example))]
			if w == nil || is.Count != w.count || !is.First.Equal(w.first) || !is.Last.Equal(w.last) || is.LastTraceID != w.lastID {
				t.Fatalf("issue %+v, reference %+v", is, w)
			}
			if i > 0 && is.Last.After(got[i-1].Last) {
				t.Fatalf("issues are not latest first at %d", i)
			}
			if strings.ContainsAny(is.Err, "0123456789") && !strings.Contains(is.Err, "tcp") {
				t.Errorf("the grouped error still holds a number: %q", is.Err)
			}
		}
		if few, _, _ := fx.store.Issues(from, to, 3); len(few) != 3 || few[0].ID != got[0].ID {
			t.Fatalf("limit 3: %d issues", len(few))
		}
		// An issue's own requests: exactly the failures it counted.
		pick := got[len(got)/2]
		of, err := fx.store.Traces(telemetry.TraceQuery{Issue: pick.ID, From: from, To: to}, 1<<30)
		if err != nil || len(of) != pick.Count || of[0].TraceID != pick.LastTraceID {
			t.Fatalf("issue %s: %d requests listed for a count of %d, %v", pick.ID, len(of), pick.Count, err)
		}
		for _, sum := range of {
			if !sum.Error || sum.Route != pick.Route || telemetry.IssueID(sum) != pick.ID {
				t.Fatalf("a request of another issue was listed: %+v", sum)
			}
		}
		if none, _ := fx.store.Traces(telemetry.TraceQuery{Issue: strings.Repeat("0", 16)}, 10); len(none) != 0 {
			t.Fatalf("an unknown issue listed %d requests", len(none))
		}
	})

	t.Run("parse trace", func(t *testing.T) {
		want := telemetry.Trace{Time: from, TraceID: strings.Repeat("ab", 16), Route: `GET /a "b"`, Path: "/a/é", Status: 503, Dur: 1500 * time.Microsecond, Error: true, FailedSteps: 1, Err: `dial tcp 10.0.0.7:5432: "refused"`,
			Spans: []telemetry.Span{
				{SpanID: "01", Name: "root", Dur: 1500 * time.Microsecond, Attrs: []telemetry.Attr{{Key: "a", Value: "1"}, {Key: "b", Value: "x\ny"}}},
				{SpanID: "02", ParentID: "01", Name: "child", Offset: 200 * time.Microsecond, Dur: 300 * time.Microsecond, Error: true},
			}}
		got, err := telemetry.ParseTrace(telemetry.AppendTrace(nil, want))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip:\n got  %+v\n want %+v\n err %v", got, want, err)
		}
		// A summary reads the trace's own fields, never a span's: this
		// trace is fine and took 9 ms, whatever its failed 3 ms step says.
		fine := telemetry.Trace{Time: from, TraceID: strings.Repeat("cd", 16), Route: "GET /x", Status: 200, Dur: 9 * time.Millisecond, FailedSteps: 1,
			Spans: []telemetry.Span{{SpanID: "01", Name: "root", Dur: 9 * time.Millisecond}, {SpanID: "02", ParentID: "01", Name: "q", Dur: 3 * time.Millisecond, Error: true}}}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, telemetry.FileName(telemetry.KindTraces, from)), telemetry.AppendTrace(nil, fine), 0o600); err != nil {
			t.Fatal(err)
		}
		st, err := telemetry.Open(telemetry.Config{Dir: dir})
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		sums, err := st.Traces(telemetry.TraceQuery{}, 5)
		if err != nil || len(sums) != 1 || sums[0].Error || sums[0].FailedSteps != 1 || sums[0].Dur != 9*time.Millisecond || sums[0].Status != 200 {
			t.Fatalf("summary of a fine trace with a failed step: %+v, %v", sums, err)
		}
		// A count no duration holds stops at the largest one; it does not
		// wrap into a small number.
		huge, err := telemetry.ParseTrace([]byte(`{"time":"2026-11-13T00:00:00Z","trace_id":"x","dur_us":9223372036854775807,"spans":[{"start_us":-5,"dur_us":9223372036854775807}]}`))
		if err != nil || huge.Dur < 290*365*24*time.Hour || huge.Spans[0].Dur != huge.Dur || huge.Spans[0].Offset != 0 {
			t.Fatalf("out-of-range microseconds: %+v, %v", huge, err)
		}
		if _, err := telemetry.ParseTrace([]byte("{not json")); err == nil {
			t.Fatal("a broken line parsed")
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
