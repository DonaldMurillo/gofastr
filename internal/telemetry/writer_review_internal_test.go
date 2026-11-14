package telemetry

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A trace longer than a reader returns is written without its
// attributes, and without its later steps when that is not enough, so
// that what was counted as kept can be found. Whole, it was written,
// read by nothing, and dropped when its day was packed.
func TestOversizedTraceIsTrimmedToWhatAReaderReturns(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	w, err := newWriter(Config{Dir: dir}, func() time.Time { return now }, false)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	big := Trace{Time: now, TraceID: strings.Repeat("a", 32), Route: "GET /report", Status: 500, Error: true, Dur: time.Second}
	for i := range 400 {
		big.Spans = append(big.Spans, Span{SpanID: "s" + strings.Repeat("0", 8), Name: "SELECT", Dur: time.Millisecond, Offset: time.Duration(i) * time.Millisecond,
			Attrs: []Attr{{"db.statement", strings.Repeat("x", 1000)}, {"a", strings.Repeat("y", 1000)}, {"b", strings.Repeat("z", 1000)}}})
	}
	// One whose steps alone are too long: ids nothing bounds.
	wide := Trace{Time: now, TraceID: strings.Repeat("b", 32), Route: "GET /wide", Status: 200, Dur: time.Second}
	for range 500 {
		wide.Spans = append(wide.Spans, Span{SpanID: strings.Repeat("9", 4000), Name: "step", Dur: time.Millisecond})
	}
	small := Trace{Time: now, TraceID: strings.Repeat("c", 32), Route: "GET /", Status: 200, Dur: time.Millisecond,
		Spans: []Span{{SpanID: "s1", Name: "x", Dur: time.Millisecond, Attrs: []Attr{{"k", "v"}}}}}
	for _, tr := range []Trace{big, wide, small} {
		if err := w.WriteTrace(tr); err != nil {
			t.Fatal(err)
		}
	}
	// The caller's trace is its own: trimming works on a copy.
	if len(big.Spans) != 400 || len(big.Spans[0].Attrs) != 3 || len(wide.Spans) != 500 {
		t.Error("writing a long trace cut the caller's own spans")
	}
	if got := w.Stats().Trimmed; got != 2 {
		t.Errorf("Trimmed = %d, want the two long traces", got)
	}
	s, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for id, want := range map[string]struct {
		spans int
		attrs bool
	}{big.TraceID: {400, false}, wide.TraceID: {250, false}, small.TraceID: {1, true}} {
		line, ok, err := s.TraceByID(id)
		if err != nil || !ok {
			t.Errorf("%s: written and not found (ok %v, err %v)", id[:4], ok, err)
			continue
		}
		if len(line) > maxLine {
			t.Errorf("%s: line of %d bytes", id[:4], len(line))
		}
		tr, err := ParseTrace(line)
		if err != nil {
			t.Fatalf("%s: %v", id[:4], err)
		}
		if len(tr.Spans) != want.spans || (len(tr.Spans[0].Attrs) > 0) != want.attrs {
			t.Errorf("%s: %d steps, attributes %v; want %d, %v", id[:4], len(tr.Spans), len(tr.Spans[0].Attrs) > 0, want.spans, want.attrs)
		}
	}
	listed, err := s.Traces(TraceQuery{Failed: true}, 10)
	if err != nil || len(listed) != 1 || listed[0].TraceID != big.TraceID {
		t.Errorf("the failed list holds %v (err %v), want the long failed trace", listed, err)
	}
}

// A write whose clock reading is from before midnight, taken after the
// day has turned, goes in the open file: it must not open the finished
// day's file again, which nothing reads once that day is packed.
func TestLateWriteDoesNotReopenAFinishedDay(t *testing.T) {
	dir := t.TempDir()
	ck := &clock{t: time.Date(2026, 11, 13, 23, 59, 0, 0, time.UTC)}
	w, err := newWriter(Config{Dir: dir}, ck.now, false)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	line := func(msg string) []byte {
		return []byte(`{"time":"` + ck.now().UTC().Format(time.RFC3339Nano) + `","level":"INFO","msg":"` + msg + `"}`)
	}
	if err := w.WriteLog(line("before")); err != nil {
		t.Fatal(err)
	}
	ck.set(time.Date(2026, 11, 14, 0, 0, 1, 0, time.UTC))
	if err := w.WriteLog(line("after")); err != nil {
		t.Fatal(err)
	}
	ck.set(time.Date(2026, 11, 13, 23, 59, 59, 999e6, time.UTC))
	if err := w.WriteLog(line("straggler")); err != nil {
		t.Fatal(err)
	}
	read := func(day time.Time) string {
		b, _ := os.ReadFile(filepath.Join(dir, FileName(KindLogs, day)))
		return string(b)
	}
	if y := read(time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)); strings.Contains(y, "straggler") || !strings.Contains(y, "before") {
		t.Errorf("the finished day's file was written again:\n%s", y)
	}
	if today := read(time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)); !strings.Contains(today, "straggler") || !strings.Contains(today, "after") {
		t.Errorf("the late line is not in the open file:\n%s", today)
	}
}

// Close against writes in flight: every write either lands before the
// file closes or is refused, no file is opened behind a closed writer,
// and no packing pass starts once Close is waiting. Run with -race.
func TestCloseAgainstWritesInFlight(t *testing.T) {
	for range 300 {
		dir := t.TempDir()
		ck := &clock{t: time.Date(2026, 11, 13, 23, 59, 59, 0, time.UTC)}
		w, err := newWriter(Config{Dir: dir}, ck.now, true)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for g := range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for i := range 50 {
					if i == 10 && g == 0 {
						ck.set(time.Date(2026, 11, 14, 0, 0, 1, 0, time.UTC)) // the day turns
					}
					if err := w.WriteLog([]byte(`{"time":"2026-11-13T23:59:59Z","msg":"x"}`)); err != nil && !errors.Is(err, ErrClosed) {
						t.Errorf("write: %v", err)
					}
				}
			}()
		}
		close(start)
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		for _, d := range []*dayFileWriter{&w.logs, &w.traces, &w.stats, &w.host} {
			d.mu.Lock()
			if d.f != nil {
				t.Fatalf("%s: a day file is open after Close", d.kind)
			}
			d.mu.Unlock()
		}
		wg.Wait()
		for _, d := range []*dayFileWriter{&w.logs, &w.traces, &w.stats, &w.host} {
			if d.f != nil {
				t.Fatalf("%s: a write after Close opened the day file again", d.kind)
			}
		}
		if err := w.WriteLog([]byte(`{"time":"2026-11-14T00:00:02Z"}`)); !errors.Is(err, ErrClosed) {
			t.Fatalf("a write after Close = %v, want ErrClosed", err)
		}
	}
}

// Request counts a writer would refuse stay in the rollup, to be
// written when it can: they are exact, and a refused write lost them.
// A directory that stays full lets them go after an hour.
func TestRefusedStatsAreKept(t *testing.T) {
	dir := t.TempDir()
	ck := &clock{t: time.Date(2026, 11, 13, 10, 0, 30, 0, time.UTC)}
	// Yesterday's file alone is over the cap: the directory is full.
	if err := os.WriteFile(filepath.Join(dir, FileName(KindLogs, ck.now())), bytes.Repeat([]byte("x"), 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := newWriter(Config{Dir: dir, MaxDiskBytes: 1024}, ck.now, false)
	if err != nil {
		t.Fatal(err)
	}
	if !w.full.Load() {
		t.Fatal("setup: the directory is not over its cap")
	}
	r := NewRollup()
	r.Observe(ck.now().Add(-2*time.Minute), "GET /a", time.Millisecond, false, false)
	if err := w.FlushStats(r, false); !errors.Is(err, ErrDiskFull) {
		t.Fatalf("FlushStats into a full directory = %v, want ErrDiskFull", err)
	}
	if len(r.cells) != 1 {
		t.Fatal("the refused minute was taken out of the rollup and lost")
	}
	// Room is made; within a minute the writer has not measured again.
	if err := os.Remove(filepath.Join(dir, FileName(KindLogs, ck.now()))); err != nil {
		t.Fatal(err)
	}
	ck.set(ck.now().Add(61 * time.Second))
	if err := w.FlushStats(r, false); err != nil {
		t.Fatalf("FlushStats after room was made: %v", err)
	}
	stats, _ := os.ReadFile(filepath.Join(dir, FileName(KindStats, ck.now())))
	if len(r.cells) != 0 || !bytes.Contains(stats, []byte(`"route":"GET /a"`)) {
		t.Errorf("the kept minute was not written once there was room (cells %d):\n%s", len(r.cells), stats)
	}

	// Full for good: an hour on, the minute is let go and counted.
	w.full.Store(true)
	w.lastPrune.Store(ck.now().Unix()) // measured just now: still full
	r.Observe(ck.now().Add(-90*time.Minute), "GET /old", time.Millisecond, false, false)
	r.Observe(ck.now().Add(-5*time.Minute), "GET /recent", time.Millisecond, false, false)
	before := w.Stats().Dropped
	if err := w.FlushStats(r, false); !errors.Is(err, ErrDiskFull) {
		t.Fatalf("FlushStats = %v, want ErrDiskFull", err)
	}
	if len(r.cells) != 1 || w.Stats().Dropped != before+1 {
		t.Errorf("after an hour full: %d minutes kept, %d dropped; want the recent one kept and the old one counted", len(r.cells), w.Stats().Dropped-before)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.FlushStats(r, true); !errors.Is(err, ErrClosed) || len(r.cells) != 1 {
		t.Errorf("FlushStats on a closed writer = %v with %d minutes left; want ErrClosed and the minute kept", err, len(r.cells))
	}
}

// A day being packed is on disk twice for as long as the pass runs: the
// plain file and the blocks being written. The second copy does not
// count toward the cap, which would otherwise remove a day more or
// refuse new lines.
func TestFilesBeingPackedDoNotCountTowardTheCap(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	yesterday := now.Add(-24 * time.Hour)
	plain := filepath.Join(dir, FileName(KindLogs, yesterday))
	if err := os.WriteFile(plain, bytes.Repeat([]byte("x"), 3000), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := strings.TrimSuffix(plain, extPlain) + extData + extTmp
	if err := os.WriteFile(tmp, bytes.Repeat([]byte("y"), 3000), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := newWriter(Config{Dir: dir, MaxDiskBytes: 4000}, func() time.Time { return now }, false)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := os.Stat(plain); err != nil || w.full.Load() {
		t.Errorf("a day being packed was removed, or the directory called full (stat %v, full %v)", err, w.full.Load())
	}
}

// Close waits for a packing pass that is starting: a pass holds bgMu
// from its look at "closed" to its registration, and Close must come
// through the same lock before it waits, or the pass registers after
// the wait is over and runs against a closed directory. The window is
// a few instructions wide, so the test holds the lock open itself.
func TestCloseWaitsForAPassThatIsStarting(t *testing.T) {
	w, err := newWriter(Config{Dir: t.TempDir()}, time.Now, true)
	if err != nil {
		t.Fatal(err)
	}
	w.bg.Wait() // the pass newWriter started
	w.bgMu.Lock()
	closed := make(chan error, 1)
	go func() { closed <- w.Close() }()
	select {
	case <-closed:
		t.Fatal("Close returned while a pass was between its check and its registration")
	case <-time.After(100 * time.Millisecond):
	}
	w.bgMu.Unlock()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close never returned")
	}
}

// A window that ends "now" lists the newest traces even when they are
// long. The search for the window's end stops within a granule of it
// only when lines are short: after 230 KB lines it stopped several
// lines early, and the fixed margin added to it cut the newest off.
func TestWindowEndKeepsTheNewestLongLines(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC)
	now := base
	w, err := newWriter(Config{Dir: dir}, func() time.Time { return now }, false)
	if err != nil {
		t.Fatal(err)
	}
	id := func(i int) string {
		return strings.Repeat("0", 28) + string("0123456789abcdef"[i>>12&15]) + string("0123456789abcdef"[i>>8&15]) + string("0123456789abcdef"[i>>4&15]) + string("0123456789abcdef"[i&15])
	}
	for i := range 3000 {
		now = base.Add(time.Duration(i) * time.Second)
		if err := w.WriteTrace(Trace{Time: now, TraceID: id(i), Route: "GET /small", Status: 200, Dur: time.Millisecond,
			Spans: []Span{{SpanID: "s", Name: "x", Dur: time.Millisecond}}}); err != nil {
			t.Fatal(err)
		}
	}
	var want []string
	for i := 3000; i < 3006; i++ {
		now = base.Add(time.Duration(i) * time.Second)
		tr := Trace{Time: now, TraceID: id(i), Route: "GET /long", Status: 200, Dur: time.Second}
		for range 500 {
			tr.Spans = append(tr.Spans, Span{SpanID: "s", Name: strings.Repeat("n", 200), Dur: time.Millisecond, Attrs: []Attr{{"q", strings.Repeat("v", 200)}}})
		}
		if err := w.WriteTrace(tr); err != nil {
			t.Fatal(err)
		}
		want = append([]string{tr.TraceID}, want...)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ids := func(q TraceQuery) []string {
		got, err := s.Traces(q, 6)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, g := range got {
			out = append(out, g.TraceID)
		}
		return out
	}
	// "Up to now", as every page asks, and the same rows oldest first.
	if got := ids(TraceQuery{To: now.Add(10 * time.Second)}); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("a window ending after the newest trace lists\n %v\nwant\n %v", got, want)
	}
	// Read oldest first (the page a "Newer" link asks for), the rows
	// come back in the same newest-first order.
	if got := ids(TraceQuery{From: base.Add(3000 * time.Second), To: now.Add(10 * time.Second), OldestFirst: true}); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the same window read oldest first lists %v", got)
	}
	// A window that ends before the long traces holds none of them.
	if got := ids(TraceQuery{To: base.Add(2990 * time.Second)}); len(got) != 6 || got[0] != id(2990) {
		t.Errorf("a window ending at trace 2990 starts with %v", got)
	}
}

func writeLogDay(t *testing.T, dir string, day time.Time, n int) {
	t.Helper()
	var b []byte
	for i := range n {
		b = append(b, logLine(day.Add(time.Duration(i)*time.Second), "line")...)
		b = append(b, '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, FileName(KindLogs, day)), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A query lists the days and then opens them. A day packed in between
// is read from its blocks, and a day removed in between is skipped:
// neither loses the answer or fails the query.
func TestQuerySurvivesADayPackedOrRemovedUnderIt(t *testing.T) {
	dir := t.TempDir()
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	writeLogDay(t, dir, day, 5)
	s, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	count := func(d dayRef, sealed bool) (int, error) {
		n := 0
		each := func([]byte) bool { n++; return true }
		var err error
		if sealed {
			err = s.scanSealed(d, false, time.Time{}, time.Time{}, true, each)
		} else {
			err = s.scanPlain(d, time.Time{}, time.Time{}, true, each)
		}
		return n, err
	}
	// The listing said "plain"; the day is packed before it is opened.
	if err := SealDay(dir, KindLogs, day); err != nil {
		t.Fatal(err)
	}
	stale := dayRef{base: KindLogs + "-" + day.Format(dayLayout), day: day}
	if _, err := count(stale, false); !errors.Is(err, errDayMoved) {
		t.Fatalf("a plain day that is gone = %v, want errDayMoved", err)
	}
	// Through scan, which is what a query calls, with the stale listing
	// stood in for by removing the index after the days are listed is
	// not possible from here; what scan does with the two answers is.
	got, err := s.TailLogs(10, -100)
	if err != nil || len(got) != 5 {
		t.Fatalf("the packed day reads %d lines (err %v), want 5", len(got), err)
	}
	// The listing said "sealed"; the day is removed before it is opened.
	for _, ext := range []string{extData, extIndex} {
		if err := os.Remove(filepath.Join(dir, stale.base+ext)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := count(stale, true); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a sealed day that is gone = %v, want a not-exist error for scan to pass over", err)
	}
}

// scan itself, against a listing that goes stale between the two steps:
// days() is asked through the store, then the files change.
func TestScanPassesOverWhatItsListingLost(t *testing.T) {
	dir := t.TempDir()
	day1 := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	day2 := day1.Add(24 * time.Hour)
	writeLogDay(t, dir, day1, 3)
	writeLogDay(t, dir, day2, 4)
	s, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n := 0
	first := true
	err = s.scan(KindLogs, time.Time{}, time.Time{}, true, false, func([]byte) bool {
		if first {
			first = false
			// While the newer day is being read, the older one is packed.
			if err := SealDay(dir, KindLogs, day1); err != nil {
				t.Error(err)
			}
		}
		n++
		return true
	})
	if err != nil || n != 7 {
		t.Errorf("scan read %d lines (err %v), want all 7: the day packed under it is read from its blocks", n, err)
	}
	// And one removed under it is passed over.
	n, first = 0, true
	err = s.scan(KindLogs, time.Time{}, time.Time{}, true, false, func([]byte) bool {
		if first {
			first = false
			base := KindLogs + "-" + day1.Format(dayLayout)
			_ = os.Remove(filepath.Join(dir, base+extData))
			_ = os.Remove(filepath.Join(dir, base+extIndex))
		}
		n++
		return true
	})
	if err != nil || n != 4 {
		t.Errorf("scan read %d lines (err %v), want the 4 of the day that is left and no error", n, err)
	}
}

// A block is checked against its checksum: one whose trailer no longer
// matches is an error, not data.
func TestDamagedBlockIsNotServed(t *testing.T) {
	dir := t.TempDir()
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	writeLogDay(t, dir, day, 50)
	if err := SealDay(dir, KindLogs, day); err != nil {
		t.Fatal(err)
	}
	s, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.TailLogs(100, -100); err != nil || len(got) != 50 {
		t.Fatalf("setup: %d lines, %v", len(got), err)
	}
	// The last eight bytes of a gzip member are its checksum and its
	// length: the data before them still unpacks.
	name := filepath.Join(dir, KindLogs+"-"+day.Format(dayLayout)+extData)
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-8] ^= 0xff
	if err := os.WriteFile(name, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.TailLogs(100, -100); err == nil {
		t.Errorf("a block with a wrong checksum was served as %d lines", len(got))
	}
}

// A percentile read from counts a file holds stays inside its bucket
// whatever the counts are.
func TestQuantileSurvivesAnyCount(t *testing.T) {
	for _, n := range []int64{1, 9e8, 4e12, 1 << 50} {
		var h [statBuckets]int64
		h[len(statBounds)-1] = n // the 5 to 10 second bucket
		for _, pct := range []int64{50, 95} {
			got := quantile(&h, pct)
			if got < statBounds[len(statBounds)-2] || got > statBounds[len(statBounds)-1] {
				t.Errorf("p%d of %d requests in the last bucket = %v, outside the bucket", pct, n, got)
			}
		}
	}
	var huge [statBuckets]int64
	huge[0], huge[1] = 1<<61, 1<<50 // a sum that fits, times a percentage that does not
	if got := quantile(&huge, 95); got != 0 {
		t.Errorf("counts that overflow their sum read as %v, want 0 (not known)", got)
	}
	// Ordinary counts read as they did: one request is the middle of
	// its bucket.
	var one [statBuckets]int64
	one[3] = 1
	if got, want := quantile(&one, 50), (statBounds[2]+statBounds[3])/2; got != want {
		t.Errorf("one request in a bucket reads %v, want its middle %v", got, want)
	}
}

// A disk rule resolves under its line, however low the line is.
func TestDiskRuleResolvesAtAnyLine(t *testing.T) {
	for atLeast, resolveAt := range map[int]int{90: 85, 6: 1, 5: 4, 3: 2, 1: 0} {
		a, err := NewAlerts([]AlertRule{{Name: "disk", Kind: AlertDisk, AtLeast: atLeast}})
		if err != nil {
			t.Fatal(err)
		}
		now := time.Unix(1700000000, 0)
		a.SetDisk(atLeast)
		if ev := a.Check(now); len(ev) != 1 || !ev[0].Firing {
			t.Fatalf("a rule at %d%% did not fire at %d%%: %v", atLeast, atLeast, ev)
		}
		// One point above where it resolves: still firing, and says
		// nothing (for a low line that point is the line itself, and a
		// rule that resolved there would fire and resolve in turns).
		a.SetDisk(resolveAt + 1)
		if ev := a.Check(now); len(ev) != 0 {
			t.Errorf("a rule at %d%% reported %v at %d%%, above its resolve line %d%%", atLeast, ev, resolveAt+1, resolveAt)
		}
		a.SetDisk(resolveAt)
		if ev := a.Check(now); len(ev) != 1 || ev[0].Firing {
			t.Errorf("a rule at %d%% did not resolve at %d%%: %v", atLeast, resolveAt, ev)
		}
	}
}

// Paging by time lists every row once, even where two rows were written
// the other way round from their times: before, the one written out of
// turn across a page's edge was on neither page.
func TestPagingListsOutOfOrderRowsOnce(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	const total = 60
	id := func(i int) string {
		return strings.Repeat("0", 30) + string("0123456789abcdef"[i>>4&15]) + string("0123456789abcdef"[i&15])
	}
	// Row i ended i seconds in, so the allowance for lines out of order
	// (timeSlop, two seconds) is two rows wide, not the whole file.
	// Written in order, except that every sixth row is written two
	// places early or two places late, turn about: wherever a page of
	// 6 or of 5 ends, some row near that edge is out of turn by as much
	// as the allowance covers, on either side.
	order := make([]int, total)
	for i := range order {
		order[i] = i
	}
	for n, i := 0, 4; i+2 < total; n, i = n+1, i+6 {
		if n%2 == 0 {
			order[i], order[i+1], order[i+2] = order[i+2], order[i], order[i+1] // i+2 written early
		} else {
			order[i], order[i+1], order[i+2] = order[i+1], order[i+2], order[i] // i written late
		}
	}
	var traces, logs []byte
	for _, i := range order {
		at := base.Add(time.Duration(i) * time.Second)
		traces = AppendTrace(traces, Trace{Time: at, TraceID: id(i), Route: "GET /a", Status: 200, Dur: time.Millisecond,
			Spans: []Span{{SpanID: "s", Name: "x", Dur: time.Millisecond}}})
		logs = append(append(logs, logLine(at, id(i))...), '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, FileName(KindTraces, base)), traces, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName(KindLogs, base)), logs, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	check := func(what string, seen []string) {
		t.Helper()
		if len(seen) != total {
			t.Errorf("%s: %d rows listed, want %d", what, len(seen), total)
		}
		count := map[string]int{}
		for _, v := range seen {
			count[v]++
		}
		for i := range total {
			if count[id(i)] != 1 {
				t.Errorf("%s: row %d listed %d times", what, i, count[id(i)])
			}
		}
	}

	for _, size := range []int{6, 5, 1} {
		// Older, as the page's link asks: everything before the oldest shown.
		var seen []string
		to := time.Time{}
		for range total {
			page, err := s.Traces(TraceQuery{To: to}, size)
			if err != nil {
				t.Fatal(err)
			}
			if len(page) == 0 {
				break
			}
			for i, tr := range page {
				if i > 0 && tr.Time.After(page[i-1].Time) {
					t.Fatalf("a page is not newest first: %v after %v", tr.Time, page[i-1].Time)
				}
				seen = append(seen, tr.TraceID)
			}
			to = page[len(page)-1].Time.Add(-time.Nanosecond)
		}
		check("traces, walking older", seen)

		// Newer: everything after the newest shown, read oldest first.
		seen = nil
		from := base.Add(-time.Second)
		for range total {
			page, err := s.Traces(TraceQuery{From: from, OldestFirst: true}, size)
			if err != nil {
				t.Fatal(err)
			}
			if len(page) == 0 {
				break
			}
			for _, tr := range page {
				seen = append(seen, tr.TraceID)
			}
			from = page[0].Time.Add(time.Nanosecond) // returned newest first
		}
		check("traces, walking newer", seen)

		seen = nil
		to = time.Time{}
		for range total {
			page, err := s.Logs(LogQuery{To: to, MinLevel: -100}, size)
			if err != nil {
				t.Fatal(err)
			}
			if len(page) == 0 {
				break
			}
			for _, e := range page {
				msg, _ := stringField(e.Line, `"msg":`)
				seen = append(seen, msg)
			}
			to = page[len(page)-1].Time.Add(-time.Nanosecond)
		}
		check("logs, walking older", seen)
	}
}
