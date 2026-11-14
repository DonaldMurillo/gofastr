package telemetry

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// dayOfLines writes a plain log file and a plain trace file for day,
// each large enough to span several blocks, and returns their bytes.
func dayOfLines(t *testing.T, dir string, day time.Time, n int) (logs, traces []byte) {
	t.Helper()
	pad := strings.Repeat("x", 180)
	for i := range n {
		at := day.Add(time.Duration(i) * time.Second)
		logs = append(logs, fmt.Sprintf(`{"time":%q,"level":"INFO","msg":"line-%05d","pad":%q}`+"\n", at.Format(time.RFC3339Nano), i, pad)...)
		traces = AppendTrace(traces, Trace{Time: at, TraceID: fmt.Sprintf("%016x%016x", i*7919+1, i), Route: "GET /r", Status: 200,
			Dur: time.Duration(i%900) * time.Millisecond, Spans: []Span{{SpanID: "01", Name: "root", Attrs: []Attr{{Key: "pad", Value: pad}}}}})
	}
	for kind, data := range map[string][]byte{KindLogs: logs, KindTraces: traces} {
		if err := os.WriteFile(filepath.Join(dir, FileName(kind, day)), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return logs, traces
}

func gunzipAll(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// A sealed day is the same lines in a file zcat reads, in more than
// one block, smaller than it was, with the plain file gone and nothing
// temporary left behind.
func TestSealedDayHoldsTheSameLinesAsOneGzipStream(t *testing.T) {
	dir := t.TempDir()
	logs, traces := dayOfLines(t, dir, day0, 12000)
	for _, kind := range []string{KindLogs, KindTraces} {
		if err := SealDay(dir, kind, day0); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"logs-20261110.idx", "logs-20261110.jsonl.gz", "traces-20261110.heads.gz", "traces-20261110.idx", "traces-20261110.jsonl.gz"}
	if got := names(t, dir); !slices.Equal(got, want) {
		t.Fatalf("files after sealing: %v, want %v", got, want)
	}
	for name, orig := range map[string][]byte{"logs-20261110.jsonl.gz": logs, "traces-20261110.jsonl.gz": traces} {
		if got := gunzipAll(t, filepath.Join(dir, name)); !bytes.Equal(got, orig) {
			t.Fatalf("%s unpacks to %d bytes, not the %d that were sealed", name, len(got), len(orig))
		}
		info, _ := os.Stat(filepath.Join(dir, name))
		if info.Size() > int64(len(orig))/2 {
			t.Errorf("%s is %d bytes for %d of lines: barely packed", name, info.Size(), len(orig))
		}
		if info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
			t.Errorf("%s mode %v: want owner-only", name, info.Mode())
		}
	}
	root, _ := os.OpenRoot(dir)
	defer root.Close()
	for _, kind := range []string{KindLogs, KindTraces} {
		f, _ := root.Open(dayBase(kind, day0) + extIndex)
		idx, err := readIndex(f)
		f.Close()
		if err != nil || len(idx.data) < 2 || !checkIndex(root, dayBase(kind, day0)+extIndex) {
			t.Fatalf("%s index: %+v, %v", kind, idx, err)
		}
		if kind == KindTraces && (idx.ids != 12000 || len(idx.heads) == 0) {
			t.Fatalf("trace index holds %d IDs and %d summary blocks", idx.ids, len(idx.heads))
		}
	}
	// The summaries are each trace up to its spans: a list reads them.
	heads := gunzipAll(t, filepath.Join(dir, "traces-20261110.heads.gz"))
	if n := bytes.Count(heads, []byte{'\n'}); n != 12000 || bytes.Contains(heads, []byte(`"spans"`)) {
		t.Fatalf("summaries: %d lines, spans included: %v", n, bytes.Contains(heads, []byte(`"spans"`)))
	}
	// Sealing a day that is sealed, or has no file, changes nothing.
	for _, day := range []time.Time{day0, day0.Add(24 * time.Hour)} {
		if err := SealDay(dir, KindLogs, day); err != nil {
			t.Fatal(err)
		}
	}
	if got := names(t, dir); !slices.Equal(got, want) {
		t.Fatalf("files after sealing again: %v", got)
	}
}

// A pass can stop anywhere. Whatever it leaves, every line is read
// exactly once, and the next pass finishes the job.
func TestInterruptedSealLeavesTheDayReadableOnce(t *testing.T) {
	stopAfter := func(n int) func(time.Duration) error {
		return func(time.Duration) error {
			if n--; n < 0 {
				return errSealStopped
			}
			return nil
		}
	}
	count := func(t *testing.T, dir string) (logs, traces int) {
		t.Helper()
		s, err := Open(Config{Dir: dir})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		l, err := s.Logs(LogQuery{}, 1<<30)
		if err != nil {
			t.Fatal(err)
		}
		tr, err := s.Traces(TraceQuery{}, 1<<30)
		if err != nil {
			t.Fatal(err)
		}
		return len(l), len(tr)
	}
	cases := map[string]func(t *testing.T, dir string, root *os.Root){
		"stopped after its first block": func(t *testing.T, dir string, root *os.Root) {
			for _, kind := range []string{KindLogs, KindTraces} {
				if err := sealDay(root, kind, day0, stopAfter(0)); !errors.Is(err, errSealStopped) {
					t.Fatalf("seal %s: %v", kind, err)
				}
			}
			for _, name := range names(t, dir) {
				if !strings.HasSuffix(name, extPlain) {
					t.Fatalf("a stopped pass left %s behind", name)
				}
			}
		},
		"blocks renamed, index not yet": func(t *testing.T, dir string, root *os.Root) {
			copyDir := t.TempDir()
			dayOfLines(t, copyDir, day0, 6000)
			if err := SealDay(copyDir, KindLogs, day0); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(filepath.Join(copyDir, "logs-20261110.jsonl.gz"))
			if err := os.WriteFile(filepath.Join(dir, "logs-20261110.jsonl.gz"), b, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "logs-20261110.idx.tmp"), []byte("half an index"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"index written, plain file not yet removed": func(t *testing.T, dir string, root *os.Root) {
			plain, _ := os.ReadFile(filepath.Join(dir, "logs-20261110.jsonl"))
			if err := sealDay(root, KindLogs, day0, nil); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "logs-20261110.jsonl"), plain, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			dayOfLines(t, dir, day0, 6000)
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			breakIt(t, dir, root)
			if l, tr := count(t, dir); l != 6000 || tr != 6000 {
				t.Fatalf("after the interruption: %d log lines and %d traces, want 6000 of each", l, tr)
			}
			for _, kind := range []string{KindLogs, KindTraces} {
				if err := sealDay(root, kind, day0, nil); err != nil {
					t.Fatalf("the next pass, %s: %v", kind, err)
				}
			}
			if l, tr := count(t, dir); l != 6000 || tr != 6000 {
				t.Fatalf("after the next pass: %d log lines and %d traces", l, tr)
			}
			for _, name := range names(t, dir) {
				if strings.HasSuffix(name, extPlain) || strings.HasSuffix(name, extTmp) {
					t.Errorf("the next pass left %s", name)
				}
			}
		})
	}
}

// A trace is found through the ID table in whichever block holds it,
// two traces that share the table's 64 bits are told apart, and an ID
// the day does not hold is not found.
func TestSealedTraceLookup(t *testing.T) {
	dir := t.TempDir()
	dayOfLines(t, dir, day0, 12000)
	// Two traces with the same first 16 digits, far apart in the file.
	twins := []string{"00000000000000aa" + strings.Repeat("1", 16), "00000000000000aa" + strings.Repeat("2", 16)}
	path := filepath.Join(dir, FileName(KindTraces, day0))
	orig, _ := os.ReadFile(path)
	var withTwins []byte
	withTwins = AppendTrace(withTwins, Trace{Time: day0, TraceID: twins[0], Route: "GET /first"})
	withTwins = append(withTwins, orig...)
	withTwins = AppendTrace(withTwins, Trace{Time: day0.Add(23 * time.Hour), TraceID: twins[1], Route: "GET /second"})
	if err := os.WriteFile(path, withTwins, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SealDay(dir, KindTraces, day0); err != nil {
		t.Fatal(err)
	}
	s, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i, want := range map[int]string{0: "line 0", 5999: "middle", 11999: "last"} {
		id := fmt.Sprintf("%016x%016x", i*7919+1, i)
		line, ok, err := s.TraceByID(id)
		if err != nil || !ok || !bytes.Contains(line, []byte(id)) {
			t.Fatalf("%s trace %s: ok=%v err=%v", want, id, ok, err)
		}
	}
	for i, id := range twins {
		line, ok, err := s.TraceByID(id)
		if err != nil || !ok || !bytes.Contains(line, []byte([]string{"GET /first", "GET /second"}[i])) {
			t.Fatalf("twin %d: %s, %v, %v", i, line, ok, err)
		}
	}
	if _, ok, err := s.TraceByID("00000000000000aa" + strings.Repeat("3", 16)); ok || err != nil {
		t.Fatalf("a third ID sharing the prefix: ok=%v err=%v", ok, err)
	}
	if _, ok, err := s.TraceByID(strings.Repeat("f", 32)); ok || err != nil {
		t.Fatalf("an ID the day does not hold: ok=%v err=%v", ok, err)
	}

	// The table names the one block to unpack, or the two for twins:
	// a lookup that unpacked the rest of the day would still be right,
	// and slow.
	root, _ := os.OpenRoot(dir)
	defer root.Close()
	idxF, _ := root.Open(dayBase(KindTraces, day0) + extIndex)
	defer idxF.Close()
	idx, err := readIndex(idxF)
	if err != nil || len(idx.data) < 4 {
		t.Fatalf("index: %d blocks, %v", len(idx.data), err)
	}
	for prefix, want := range map[uint64]int{uint64(5999*7919 + 1): 1, 0xaa: 2, 0xdeadbeef: 0} {
		blocks, err := idx.blocksFor(idxF, prefix)
		if err != nil || len(blocks) != want {
			t.Errorf("prefix %x: blocks %v, %v; want %d", prefix, blocks, err, want)
		}
	}

	// A list reads the summaries and never opens the traces themselves.
	if err := os.Remove(filepath.Join(dir, dayBase(KindTraces, day0)+extData)); err != nil {
		t.Fatal(err)
	}
	if sums, err := s.Traces(TraceQuery{MinDur: 899 * time.Millisecond}, 1<<30); err != nil || len(sums) == 0 {
		t.Fatalf("a list of a sealed day without its trace blocks: %d, %v", len(sums), err)
	}
	if slow, err := s.SlowestTraces("GET /r", time.Time{}, time.Time{}, 5); err != nil || len(slow) != 5 || slow[0].Dur != 899*time.Millisecond {
		t.Fatalf("slowest from summaries: %+v, %v", slow, err)
	}
}

// A damaged index is an error a query returns, never a panic or a
// wrong answer, and repair rebuilds it from the blocks: the day reads
// as it did before.
func TestDamagedIndexIsRefusedAndRebuilt(t *testing.T) {
	dir := t.TempDir()
	dayOfLines(t, dir, day0, 6000)
	for _, kind := range []string{KindLogs, KindTraces} {
		if err := SealDay(dir, kind, day0); err != nil {
			t.Fatal(err)
		}
	}
	read := func() (int, int, string, error) {
		s, err := Open(Config{Dir: dir})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		logs, err1 := s.Logs(LogQuery{}, 1<<30)
		traces, err2 := s.Traces(TraceQuery{MinDur: 800 * time.Millisecond}, 1<<30)
		line, _, err3 := s.TraceByID(fmt.Sprintf("%016x%016x", 4321*7919+1, 4321))
		return len(logs), len(traces), string(line), errors.Join(err1, err2, err3)
	}
	wantLogs, wantTraces, wantLine, err := read()
	if err != nil || wantLogs != 6000 || wantTraces == 0 || wantLine == "" {
		t.Fatalf("before any damage: %d, %d, %q, %v", wantLogs, wantTraces, wantLine, err)
	}
	good := map[string][]byte{}
	for _, kind := range []string{KindLogs, KindTraces} {
		good[kind], _ = os.ReadFile(filepath.Join(dir, dayBase(kind, day0)+extIndex))
	}
	huge := append(bytes.Clone(indexMagic[:]), 0xff, 0xff, 0xff, 0xff)
	damage := map[string]func(b []byte) []byte{
		"truncated":      func(b []byte) []byte { return b[:len(b)/2] },
		"not an index":   func([]byte) []byte { return []byte("{}") },
		"empty":          func([]byte) []byte { return nil },
		"a huge count":   func([]byte) []byte { return huge },
		"bytes appended": func(b []byte) []byte { return append(bytes.Clone(b), 1, 2, 3) },
		"removed":        nil,
	}
	for name, hurt := range damage {
		for _, kind := range []string{KindLogs, KindTraces} {
			path := filepath.Join(dir, dayBase(kind, day0)+extIndex)
			if hurt == nil {
				os.Remove(path)
			} else if err := os.WriteFile(path, hurt(good[kind]), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if hurt != nil {
			if _, _, _, err := read(); err == nil {
				t.Errorf("%s: a query read a day with a damaged index and reported nothing", name)
			}
		}
		for _, kind := range []string{KindLogs, KindTraces} {
			if err := RepairDay(dir, kind, day0); err != nil {
				t.Fatalf("%s: repair %s: %v", name, kind, err)
			}
		}
		l, tr, line, err := read()
		if err != nil || l != wantLogs || tr != wantTraces || line != wantLine {
			t.Errorf("%s, after repair: %d logs, %d slow traces, trace found %v, %v", name, l, tr, line == wantLine, err)
		}
	}
	// Repairing a day that is whole leaves its index alone.
	idxPath := filepath.Join(dir, dayBase(KindLogs, day0)+extIndex)
	before, _ := os.Stat(idxPath)
	time.Sleep(20 * time.Millisecond)
	if err := RepairDay(dir, KindLogs, day0); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(idxPath); !after.ModTime().Equal(before.ModTime()) {
		t.Error("repair rewrote the index of a day that was whole")
	}

	// An index that claims billions of blocks, or a block of gigabytes,
	// is refused before anything that size is asked of the allocator.
	lying := bytes.Clone(good[KindLogs])
	binary.LittleEndian.PutUint32(lying[8+4+12:], 0xffffffff) // the first block's unpacked length
	binary.LittleEndian.PutUint32(lying[len(lying)-4:], crc32.ChecksumIEEE(lying[:len(lying)-4]))
	for name, content := range map[string][]byte{"a huge count": huge, "a huge block": lying} {
		os.WriteFile(idxPath, content, 0o600)
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		_, _, _, err := read()
		runtime.ReadMemStats(&m1)
		if err == nil {
			t.Errorf("%s: the query reported nothing", name)
		}
		if grew := m1.TotalAlloc - m0.TotalAlloc; grew > 256<<20 {
			t.Errorf("%s: the query allocated %d MiB on the index's word", name, grew>>20)
		}
	}
	os.WriteFile(idxPath, good[KindLogs], 0o600)

	// A flipped bit inside a table passes the shape check a query makes
	// and is caught by the checksum the repair pass reads.
	root, _ := os.OpenRoot(dir)
	defer root.Close()
	name := dayBase(KindTraces, day0) + extIndex
	b, _ := os.ReadFile(filepath.Join(dir, name))
	b[len(b)/2] ^= 0x40
	os.WriteFile(filepath.Join(dir, name), b, 0o600)
	if checkIndex(root, name) {
		t.Fatal("a flipped bit passed the checksum")
	}
	if err := RepairDay(dir, KindTraces, day0); err != nil || !checkIndex(root, name) {
		t.Fatalf("repair after a flipped bit: %v", err)
	}
}

// The writer seals a day once it is over, in the background, and its
// disk budget is then counted in sealed files: yesterday's lines stay
// readable, and a retention pass removes a sealed day whole.
func TestWriterSealsFinishedDays(t *testing.T) {
	dir := t.TempDir()
	ck := &clock{t: day0.Add(12 * time.Hour)}
	w, err := newWriter(Config{Dir: dir, Retention: 36 * time.Hour}, ck.now, true)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pad := strings.Repeat("y", 400)
	for i := range 3000 {
		at := ck.now().Add(time.Duration(i) * time.Millisecond)
		if err := w.WriteLog([]byte(fmt.Sprintf(`{"time":%q,"level":"INFO","msg":"day one %04d","pad":%q}`, at.Format(time.RFC3339Nano), i, pad))); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.WriteTrace(Trace{Time: ck.now(), TraceID: strings.Repeat("ab", 16), Route: "GET /one"}); err != nil {
		t.Fatal(err)
	}
	plainSize := func() int64 {
		info, err := os.Stat(filepath.Join(dir, "logs-20261110.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		return info.Size()
	}()

	ck.set(day0.Add(24*time.Hour + time.Minute))
	if err := w.WriteLog(logLine(ck.now(), "day two")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteTrace(Trace{Time: ck.now(), TraceID: strings.Repeat("cd", 16), Route: "GET /two"}); err != nil {
		t.Fatal(err)
	}
	w.bg.Wait()

	want := []string{"logs-20261110.idx", "logs-20261110.jsonl.gz", "logs-20261111.jsonl",
		"traces-20261110.heads.gz", "traces-20261110.idx", "traces-20261110.jsonl.gz", "traces-20261111.jsonl"}
	if got := names(t, dir); !slices.Equal(got, want) {
		t.Fatalf("files after the day turned: %v, want %v", got, want)
	}
	if st := w.Stats(); st.DaysSealed != 2 {
		t.Fatalf("DaysSealed = %d, want 2 (logs and traces)", st.DaysSealed)
	}
	if info, _ := os.Stat(filepath.Join(dir, "logs-20261110.jsonl.gz")); info.Size() > plainSize/3 {
		t.Errorf("sealed log is %d bytes, the plain file was %d", info.Size(), plainSize)
	}
	s, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tail, err := s.TailLogs(2, slog.LevelDebug)
	if err != nil || len(tail) != 2 || !strings.Contains(string(tail[0].Line), "day one 2999") || !strings.Contains(string(tail[1].Line), "day two") {
		t.Fatalf("tail across a sealed day and today: %v, %v", tail, err)
	}
	for _, id := range []string{strings.Repeat("ab", 16), strings.Repeat("cd", 16)} {
		if _, ok, err := s.TraceByID(id); !ok || err != nil {
			t.Fatalf("trace %s after sealing: ok=%v err=%v", id, ok, err)
		}
	}

	// Two days on, the first day is past retention: every one of its
	// files goes, the sealed second day stays.
	ck.set(day0.Add(3*24*time.Hour + time.Hour))
	if err := w.WriteLog(logLine(ck.now(), "day four")); err != nil {
		t.Fatal(err)
	}
	w.bg.Wait()
	for _, name := range names(t, dir) {
		if strings.Contains(name, "20261110") {
			t.Errorf("%s is past retention and still here", name)
		}
	}
	if got := names(t, dir); !slices.Contains(got, "logs-20261111.jsonl.gz") || !slices.Contains(got, "logs-20261113.jsonl") {
		t.Fatalf("files after retention: %v", got)
	}
}

// Closing a writer stops a sealing pass at its next block instead of
// waiting out the day, and what it was sealing stays plain and whole.
func TestCloseStopsASealingPass(t *testing.T) {
	dir := t.TempDir()
	dayOfLines(t, dir, day0, 60000)
	ck := &clock{t: day0.Add(30 * time.Hour)}
	w, err := newWriter(Config{Dir: dir}, ck.now, true)
	if err != nil {
		t.Fatal(err)
	}
	// Wait until the pass is in the middle of a day.
	for deadline := time.Now().Add(10 * time.Second); ; {
		if slices.ContainsFunc(names(t, dir), func(n string) bool { return strings.HasSuffix(n, extTmp) }) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the sealing pass never started")
		}
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("Close waited %s for the sealing pass", took)
	}
	// The pass is over when Close returns: nothing is still writing a
	// moment later, and the day it had started on is still plain.
	time.Sleep(400 * time.Millisecond)
	got := names(t, dir)
	for _, name := range got {
		if strings.HasSuffix(name, extTmp) {
			t.Errorf("a stopped pass left %s", name)
		}
	}
	if slices.ContainsFunc(got, func(n string) bool { return strings.HasSuffix(n, extIndex) }) ||
		!slices.Contains(got, "logs-20261110.jsonl") || !slices.Contains(got, "traces-20261110.jsonl") {
		t.Errorf("the pass sealed a day after Close was called: %v", got)
	}
	s, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if logs, err := s.Logs(LogQuery{}, 1<<30); err != nil || len(logs) != 60000 {
		t.Fatalf("after a stopped pass: %d log lines, %v", len(logs), err)
	}
}
