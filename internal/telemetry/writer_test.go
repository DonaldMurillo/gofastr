package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

func logLine(t time.Time, msg string) []byte {
	return []byte(`{"time":"` + t.UTC().Format(time.RFC3339Nano) + `","level":"INFO","msg":"` + msg + `"}`)
}

func dayFiles(t *testing.T, dir string) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range names {
		names[i] = filepath.Base(names[i])
	}
	slices.Sort(names)
	return names
}

var day0 = time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)

// What the writer writes, the store reads back, split by UTC day, and a
// line is on disk the moment WriteLog returns: nothing is closed or
// flushed before the reads.
func TestWriterWritesDayFilesTheStoreReads(t *testing.T) {
	dir := t.TempDir()
	ck := &clock{t: day0.Add(23*time.Hour + 59*time.Minute)}
	w, err := newWriter(Config{Dir: dir}, ck.now, false)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	id := strings.Repeat("ab", 16)
	if err := w.WriteLog(logLine(ck.now(), "before midnight")); err != nil {
		t.Fatal(err)
	}
	ck.set(day0.Add(24*time.Hour + time.Second))
	if err := w.WriteLog(logLine(ck.now(), "after midnight")); err != nil {
		t.Fatal(err)
	}
	tr := Trace{Time: ck.now(), TraceID: id, Route: "GET /x", Status: 200, Dur: time.Millisecond,
		Spans: []Span{{SpanID: "0000000000000001", Name: "HTTP GET /x", Dur: time.Millisecond}}}
	if err := w.WriteTrace(tr); err != nil {
		t.Fatal(err)
	}

	if got, want := dayFiles(t, dir), []string{"logs-20261110.jsonl", "logs-20261111.jsonl", "traces-20261111.jsonl"}; !slices.Equal(got, want) {
		t.Fatalf("files %v, want %v", got, want)
	}
	// Windows has no mode bits to check.
	if info, err := os.Stat(filepath.Join(dir, "logs-20261110.jsonl")); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		t.Fatalf("day file mode %v, err %v: want owner-only", info.Mode(), err)
	}
	s, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tail, err := s.TailLogs(10, slog.LevelDebug)
	if err != nil || len(tail) != 2 || !strings.Contains(string(tail[0].Line), "before midnight") || !strings.Contains(string(tail[1].Line), "after midnight") {
		t.Fatalf("tail = %v, %v", tail, err)
	}
	line, ok, err := s.TraceByID(id)
	if err != nil || !ok || !json.Valid(line) {
		t.Fatalf("TraceByID: %s, %v, %v", line, ok, err)
	}
	if st := w.Stats(); st.LogLines != 2 || st.Traces != 1 || st.Dropped != 0 || st.LogBytes == 0 || st.TraceBytes != int64(len(line))+1 {
		t.Fatalf("stats %+v", st)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteLog(logLine(ck.now(), "late")); !errors.Is(err, ErrClosed) {
		t.Fatalf("write after Close: %v", err)
	}
}

func seed(t *testing.T, dir string, day time.Time, size int) {
	t.Helper()
	for _, kind := range []string{KindLogs, KindTraces} {
		if err := os.WriteFile(filepath.Join(dir, FileName(kind, day)), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWriterRemovesFilesPastRetention(t *testing.T) {
	dir := t.TempDir()
	for d := range 5 {
		seed(t, dir, day0.Add(time.Duration(d)*24*time.Hour), 10)
	}
	other := filepath.Join(dir, "notes.txt") // not ours: never touched
	if err := os.WriteFile(other, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Noon on the fifth day, keeping 2 days: the files that ended more
	// than 48 hours ago are days 0 and 1.
	ck := &clock{t: day0.Add(4*24*time.Hour + 12*time.Hour)}
	w, err := newWriter(Config{Dir: dir, Retention: 48 * time.Hour}, ck.now, false)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	want := []string{"logs-20261112.jsonl", "logs-20261113.jsonl", "logs-20261114.jsonl",
		"traces-20261112.jsonl", "traces-20261113.jsonl", "traces-20261114.jsonl"}
	if got := dayFiles(t, dir); !slices.Equal(got, want) {
		t.Fatalf("files %v, want %v", got, want)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("a file that is not a day file was removed: %v", err)
	}
	if st := w.Stats(); st.FilesRemoved != 4 {
		t.Fatalf("FilesRemoved = %d, want 4", st.FilesRemoved)
	}
}

func TestWriterHoldsTheDiskCap(t *testing.T) {
	dir := t.TempDir()
	for d := range 3 {
		seed(t, dir, day0.Add(time.Duration(d)*24*time.Hour), 1000) // 2000 bytes a day
	}
	ck := &clock{t: day0.Add(3*24*time.Hour + time.Hour)}
	// Room for two past days and a little of today.
	w, err := newWriter(Config{Dir: dir, MaxDiskBytes: 4500}, ck.now, false)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if got := dayFiles(t, dir); len(got) != 4 || got[0] != "logs-20261111.jsonl" {
		t.Fatalf("after open: %v, want the oldest day gone", got)
	}

	// Today grows past the cap on its own: the older days go first,
	// then writes are refused, and the directory stays within one
	// check interval of the cap.
	big := logLine(ck.now(), strings.Repeat("x", 1<<20))
	var refused int
	for range 40 {
		if err := w.WriteLog(slices.Clone(big)); errors.Is(err, ErrDiskFull) {
			refused++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if got := dayFiles(t, dir); !slices.Equal(got, []string{"logs-20261113.jsonl"}) {
		t.Fatalf("files %v, want only today's", got)
	}
	info, err := os.Stat(filepath.Join(dir, "logs-20261113.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if refused == 0 || info.Size() > 4500+pruneEvery+int64(len(big)) {
		t.Fatalf("refused %d writes, today's file is %d bytes", refused, info.Size())
	}
	if st := w.Stats(); st.Dropped != int64(refused) {
		t.Fatalf("Dropped = %d, refused %d", st.Dropped, refused)
	}

	// The day turns: yesterday's file can go, and writing resumes.
	ck.set(day0.Add(4*24*time.Hour + time.Minute))
	for i := range 2 {
		if err := w.WriteLog(logLine(ck.now(), fmt.Sprint("new day ", i))); err != nil {
			t.Fatalf("write %d on the new day: %v", i, err)
		}
	}
	if got := dayFiles(t, dir); !slices.Equal(got, []string{"logs-20261114.jsonl"}) {
		t.Fatalf("files %v, want only the new day's", got)
	}
}

func TestCollectorKeepsErrorsSlowAndTheSampledShare(t *testing.T) {
	start := day0.Add(time.Hour)
	child := func(id string) SpanEnd {
		return SpanEnd{TraceID: id, Start: start.Add(2 * time.Millisecond),
			Span: Span{SpanID: "000000000000000c", ParentID: "000000000000000a", Name: "db.query", Dur: time.Millisecond}}
	}
	root := func(id string, dur time.Duration) SpanEnd {
		return SpanEnd{TraceID: id, Start: start, Root: true, Route: "GET /x", Status: 200,
			Span: Span{SpanID: "000000000000000a", Name: "HTTP GET /x", Dur: dur}}
	}
	low, high := "00"+strings.Repeat("1", 30), "ff"+strings.Repeat("1", 30) // first and last 1/256 of the ID space
	cases := []struct {
		name  string
		cfg   Config
		id    string
		dur   time.Duration
		fails bool
		keep  bool
		// rootFails fails the whole, where fails fails one step.
		rootFails bool
		// stray makes the root a lone outgoing call.
		stray bool
		// quiet makes it a request for one of the framework's assets.
		quiet bool
	}{
		{name: "ordinary, rate 1", cfg: Config{SampleRate: 1}, id: high, dur: time.Millisecond, keep: true},
		{name: "ordinary, keep none", cfg: Config{KeepNoOrdinary: true}, id: low, dur: time.Millisecond},
		{name: "ordinary, in the sampled share", cfg: Config{SampleRate: 0.01}, id: low, dur: time.Millisecond, keep: true},
		{name: "ordinary, outside it", cfg: Config{SampleRate: 0.01}, id: high, dur: time.Millisecond},
		{name: "slow", cfg: Config{KeepNoOrdinary: true, SlowThreshold: time.Second}, id: high, dur: time.Second, keep: true},
		{name: "just under slow", cfg: Config{KeepNoOrdinary: true, SlowThreshold: time.Second}, id: high, dur: time.Second - 1},
		{name: "a step failed, the whole did not", cfg: Config{KeepNoOrdinary: true}, id: high, dur: time.Millisecond, fails: true},
		{name: "a step failed, sampled in", cfg: Config{SampleRate: 1}, id: high, dur: time.Millisecond, fails: true, keep: true},
		{name: "a stray call, sampled in for anything else", cfg: Config{SampleRate: 1}, id: low, dur: time.Millisecond, stray: true},
		{name: "a stray call that failed", cfg: Config{SampleRate: 1}, id: low, dur: time.Millisecond, rootFails: true, stray: true},
		{name: "a stray call that was slow", cfg: Config{KeepNoOrdinary: true, SlowThreshold: time.Second}, id: high, dur: time.Second, keep: true, stray: true},
		{name: "a quiet request, sampled in for anything else", cfg: Config{SampleRate: 1}, id: low, dur: time.Millisecond, quiet: true},
		{name: "a quiet request that failed", cfg: Config{KeepNoOrdinary: true}, id: high, dur: time.Millisecond, keep: true, rootFails: true, quiet: true},
		{name: "the whole failed", cfg: Config{KeepNoOrdinary: true}, id: high, dur: time.Millisecond, keep: true, rootFails: true},
	}
	for _, tc := range cases {
		var got []Trace
		tc.cfg.Dir = "unused"
		c, err := NewCollector(tc.cfg, func(tr Trace) error { got = append(got, tr); return nil })
		if err != nil {
			t.Fatal(err)
		}
		ch := child(tc.id)
		ch.Error = tc.fails
		c.SpanEnded(ch)
		rt := root(tc.id, tc.dur)
		rt.Error = tc.rootFails
		rt.Stray = tc.stray
		rt.Quiet = tc.quiet
		c.SpanEnded(rt)
		st := c.Stats()
		if (len(got) == 1) != tc.keep || st.Kept+st.SampledOut != 1 || (st.Kept == 1) != tc.keep || st.OpenTraces != 0 || st.BufferedBytes != 0 {
			t.Errorf("%s: wrote %d traces, stats %+v", tc.name, len(got), st)
			continue
		}
		if !tc.keep {
			continue
		}
		tr := got[0]
		if tr.TraceID != tc.id || tr.Route != "GET /x" || tr.Status != 200 || tr.Dur != tc.dur || tr.Error != tc.rootFails || tr.FailedSteps != map[bool]int{true: 1}[tc.fails] ||
			!tr.Time.Equal(start.Add(tc.dur)) || len(tr.Spans) != 2 ||
			tr.Spans[0].Name != "HTTP GET /x" || tr.Spans[0].Offset != 0 || tr.Spans[1].Offset != 2*time.Millisecond ||
			tr.Spans[0].Error != tc.rootFails || tr.Spans[1].Error != tc.fails {
			t.Errorf("%s: trace %+v", tc.name, tr)
		}
	}
}

// Spans whose root never ends must not pile up: the buffer stays under
// the cap by giving up the oldest trace, and a trace left waiting too
// long is given up whatever the buffer holds.
func TestCollectorBufferIsBounded(t *testing.T) {
	ck := &clock{t: day0}
	var written int
	c, err := NewCollector(Config{Dir: "unused", MaxMemoryBytes: 64 << 10, SampleRate: 1}, func(Trace) error { written++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	c.now = ck.now
	span := func(i int, root bool) SpanEnd {
		return SpanEnd{TraceID: fmt.Sprintf("%032x", i), Start: day0, Root: root,
			Span: Span{SpanID: "000000000000000a", Name: "work", Attrs: []Attr{{"k", strings.Repeat("v", 5000)}}}}
	}
	for i := range 2000 {
		c.SpanEnded(span(i, false))
		if st := c.Stats(); st.BufferedBytes > 64<<10 {
			t.Fatalf("after %d spans the buffer holds %d bytes, over the 64 KiB cap", i+1, st.BufferedBytes)
		}
	}
	st := c.Stats()
	if st.Dropped == 0 || st.OpenTraces == 0 || int64(st.OpenTraces)+st.Dropped != 2000 {
		t.Fatalf("stats %+v: want every trace either waiting or dropped", st)
	}
	// An attribute is cut to its limit, which is why 5000 bytes of
	// value cost about 1 KiB each above.
	if per := st.BufferedBytes / int64(st.OpenTraces); per > maxAttrValue+512 {
		t.Fatalf("a span with one long attribute costs %d bytes", per)
	}

	// The newest trace is still waiting; its root completes it.
	c.SpanEnded(span(1999, true))
	if written != 1 {
		t.Fatalf("the newest waiting trace was not written when its root ended")
	}
	// Ten minutes on, one more span arrives: everything still waiting
	// has waited too long.
	ck.set(day0.Add(maxOpenAge))
	c.SpanEnded(span(5000, true))
	if st := c.Stats(); st.OpenTraces != 0 || st.BufferedBytes != 0 {
		t.Fatalf("stale traces still buffered: %+v", st)
	}
}

func TestCollectorCapsSpansPerTrace(t *testing.T) {
	var got Trace
	c, err := NewCollector(Config{Dir: "unused", SampleRate: 1}, func(tr Trace) error { got = tr; return nil })
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("1", 32)
	for range maxSpansPerTrace + 10 {
		c.SpanEnded(SpanEnd{TraceID: id, Start: day0, Span: Span{Name: "child"}})
	}
	c.SpanEnded(SpanEnd{TraceID: id, Start: day0, Root: true, Span: Span{Name: "root"}})
	if len(got.Spans) != maxSpansPerTrace+1 || got.Spans[0].Name != "root" || c.Stats().SpansDropped != 10 {
		t.Fatalf("%d spans, first %q, %d dropped", len(got.Spans), got.Spans[0].Name, c.Stats().SpansDropped)
	}
	if line := AppendTrace(nil, got); len(line) > maxLine {
		t.Fatalf("a full trace is %d bytes, past the readers' line cap", len(line))
	}
}
