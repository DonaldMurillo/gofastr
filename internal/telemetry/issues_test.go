package telemetry

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Two failures of one kind differ in a number or an ID and group as
// one; two kinds of failure do not, and a word spelled in hex letters
// is still a word.
func TestStableErrSetsAsideWhatChanges(t *testing.T) {
	same := [][2]string{
		{"upstream timed out after 3012ms", "upstream timed out after 3899ms"},
		{"dial tcp 10.0.0.7:5432: connect: connection refused", "dial tcp 10.0.0.201:5432: connect: connection refused"},
		{"order 4f9a2c1e-77b0-4d1e-9c3a-0a1b2c3d4e5f not found", "order 00000000-0000-4000-8000-000000000001 not found"},
		{"session 9f86d081884c7d65 expired", "session 2c26b46b68ffc68f expired"},
		{"row 7 of 12 failed", "row 9431 of 2 failed"},
	}
	for _, p := range same {
		if a, b := stableErr(p[0]), stableErr(p[1]); a != b {
			t.Errorf("%q and %q group apart: %q, %q", p[0], p[1], a, b)
		}
	}
	apart := [][2]string{
		{"pq: deadlock detected", "pq: duplicate key"},
		{"decode failed", "facade failed"},
		{"bad feed", "bad deed"},
		{"timeout", "timeout after 5s"},
	}
	for _, p := range apart {
		if a, b := stableErr(p[0]), stableErr(p[1]); a == b {
			t.Errorf("%q and %q group together as %q", p[0], p[1], a)
		}
	}
	for in, want := range map[string]string{
		"":               "",
		"plain words":    "plain words",
		"deadbeef":       "deadbeef", // hex letters with no digit: a word
		"a-b-c":          "a-b-c",
		"id=deadbeef1 x": "id=ID x",
		"-12":            "-N",
		"émoji 5 ok":     "émoji N ok",
	} {
		if got := stableErr(in); got != want {
			t.Errorf("stableErr(%q) = %q, want %q", in, got, want)
		}
	}
}

// Route, status and error each tell issues apart, and none of them can
// be confused for another by where one ends and the next begins.
func TestIssueIDTellsIssuesApart(t *testing.T) {
	base := TraceSummary{Route: "GET /a", Status: 500, Err: "boom"}
	seen := map[string]string{IssueID(base): "base"}
	for name, sum := range map[string]TraceSummary{
		"another route":  {Route: "GET /b", Status: 500, Err: "boom"},
		"another status": {Route: "GET /a", Status: 502, Err: "boom"},
		"another error":  {Route: "GET /a", Status: 500, Err: "bang"},
		"shifted parts":  {Route: "GET /aboom", Status: 500, Err: ""},
		"shifted back":   {Route: "GET /", Status: 500, Err: "aboom"},
	} {
		id := IssueID(sum)
		if other, dup := seen[id]; dup {
			t.Errorf("%s has the ID of %s", name, other)
		}
		seen[id] = name
	}
	if IssueID(TraceSummary{Route: "GET /a", Status: 500, Err: "boom 12"}) != IssueID(TraceSummary{Route: "GET /a", Status: 500, Err: "boom 99"}) {
		t.Error("two failures that differ in a number have different IDs")
	}
}

// Past two thousand distinct issues a listing stops making new ones and
// says how many failures it left out; the ones it has keep counting.
func TestIssuesAreBounded(t *testing.T) {
	dir := t.TempDir()
	var traces []byte
	for i := range maxIssues + 50 {
		at := day0.Add(time.Duration(i) * time.Second)
		// A different route each time: a different issue each time.
		traces = AppendTrace(traces, Trace{Time: at, TraceID: fmt.Sprintf("%032x", i+1), Route: fmt.Sprintf("GET /r/%c%c%c", 'a'+i%26, 'a'+i/26%26, 'a'+i/676%26), Status: 500, Error: true, Err: "boom"})
	}
	// And three more of the newest issue, which is being counted.
	for j := range 3 {
		traces = AppendTrace(traces, Trace{Time: day0.Add(23*time.Hour + time.Duration(j)*time.Second), TraceID: fmt.Sprintf("%032x", 90000+j), Route: "GET /newest", Status: 500, Error: true, Err: "boom"})
	}
	if err := os.WriteFile(filepath.Join(dir, FileName(KindTraces, day0)), traces, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	issues, leftOut, err := s.Issues(time.Time{}, time.Time{}, 1<<30)
	if err != nil || len(issues) != maxIssues || leftOut != 51 {
		t.Fatalf("%d issues, %d left out, %v; want %d and 51", len(issues), leftOut, err, maxIssues)
	}
	if issues[0].Route != "GET /newest" || issues[0].Count != 3 {
		t.Fatalf("the newest issue: %+v", issues[0])
	}
}

// What went wrong is the first failed step's error, else the first
// error the app logged for the request, else the whole's own status
// text. And an error of any length is cut before it is written: the
// summary a list reads has a fixed size.
func TestTraceErrComesFromWhoeverSaysItBest(t *testing.T) {
	run := func(rootErr, logged string, steps ...string) Trace {
		var got Trace
		c, err := NewCollector(Config{Dir: "unused", SampleRate: 1}, func(tr Trace) error { got = tr; return nil })
		if err != nil {
			t.Fatal(err)
		}
		id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		// The usual order: the handler logs before any span has ended.
		if logged != "" {
			c.NoteError(id, logged)
			c.NoteError(id, "a second logged error") // the first one stands
		}
		for _, e := range steps {
			c.SpanEnded(SpanEnd{TraceID: id, Start: day0, Error: e != "", Err: e, Span: Span{Name: "step"}})
		}
		c.NoteError("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "another request's error")
		c.SpanEnded(SpanEnd{TraceID: id, Start: day0, Root: true, Error: rootErr != "", Err: rootErr, Span: Span{Name: "root"}})
		return got
	}
	for name, c := range map[string]struct {
		got  Trace
		want string
	}{
		"only steps failed":                              {run("", "", "", "first step broke", "second step broke"), "first step broke"},
		"a step and the whole failed":                    {run("Internal Server Error", "logged", "first step broke"), "first step broke"},
		"the whole failed and the app logged":            {run("Internal Server Error", "card declined", ""), "card declined"},
		"the whole failed, nothing said why":             {run("Internal Server Error", "", ""), "Internal Server Error"},
		"an error logged by a request that did not fail": {run("", "a warning in error's clothes", ""), ""},
		"nothing failed":                                 {run("", "", ""), ""},
	} {
		if c.got.Err != c.want {
			t.Errorf("%s: Err = %q, want %q", name, c.got.Err, c.want)
		}
	}

	// Errors logged for requests that never end do not pile up: they
	// are held under the same cap as spans.
	c, err := NewCollector(Config{Dir: "unused", MaxMemoryBytes: 32 << 10}, func(Trace) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for i := range 5000 {
		c.NoteError(fmt.Sprintf("%032x", i), "an error for a request that never ends")
		if st := c.Stats(); st.BufferedBytes > 32<<10 {
			t.Fatalf("after %d logged errors the buffer holds %d bytes, over its 32 KiB cap", i+1, st.BufferedBytes)
		}
	}
	if st := c.Stats(); st.Dropped == 0 || st.OpenTraces > 200 {
		t.Fatalf("5000 logged errors left %d traces waiting and dropped %d", st.OpenTraces, st.Dropped)
	}

	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'x'
	}
	line := AppendTrace(nil, Trace{Time: day0, TraceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Error: true, Err: string(long)})
	sum, ok := summary(line)
	if !ok || len(sum.Err) != maxErrLen {
		t.Fatalf("a 5000-byte error was written as %d bytes, want %d", len(sum.Err), maxErrLen)
	}
}

func TestLoggedErrorReadsOnlyErrorsInsideATrace(t *testing.T) {
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	line := func(level, rest string) []byte {
		return []byte(`{"time":"2026-11-13T00:00:00Z","level":"` + level + `","msg":"payment failed","trace_id":"` + id + `"` + rest + `}`)
	}
	for name, c := range map[string]struct {
		entry []byte
		text  string
	}{
		"err attribute":    {line("ERROR", `,"err":"card declined"`), "card declined"},
		"error attribute":  {line("ERROR", `,"error":"gateway down"`), "gateway down"},
		"only a message":   {line("ERROR", ``), "payment failed"},
		"above ERROR":      {line("ERROR+4", `,"err":"fatal"`), "fatal"},
		"an empty err":     {line("ERROR", `,"err":""`), "payment failed"},
		"a warning":        {line("WARN", `,"err":"slow"`), ""},
		"info":             {line("INFO", `,"err":"x"`), ""},
		"no trace":         {[]byte(`{"time":"2026-11-13T00:00:00Z","level":"ERROR","msg":"boot failed","err":"x"}`), ""},
		"a short trace ID": {[]byte(`{"time":"x","level":"ERROR","msg":"m","trace_id":"abc","err":"x"}`), ""},
		"not json":         {[]byte(`ERROR something`), ""},
	} {
		gotID, text, ok := LoggedError(c.entry)
		if text != c.text || ok != (c.text != "") || (ok && gotID != id) {
			t.Errorf("%s: %q, %q, %v; want text %q", name, gotID, text, ok, c.text)
		}
	}
}

// Two issues last seen in the same instant: the one that happens more
// is listed first, whatever their IDs.
func TestIssuesSeenTogetherListTheCommonOneFirst(t *testing.T) {
	for _, common := range []string{"alpha failed", "omega failed"} {
		rare := "omega failed"
		if common == rare {
			rare = "alpha failed"
		}
		dir := t.TempDir()
		var traces []byte
		id := 0
		add := func(at time.Time, e string) {
			id++
			traces = AppendTrace(traces, Trace{Time: at, TraceID: fmt.Sprintf("%032x", id), Route: "POST /pay", Status: 500, Error: true, Err: e})
		}
		for j := range 3 {
			add(day0.Add(time.Duration(j)*time.Second), common)
		}
		add(day0.Add(time.Minute), common)
		add(day0.Add(time.Minute), rare)
		if err := os.WriteFile(filepath.Join(dir, FileName(KindTraces, day0)), traces, 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := Open(Config{Dir: dir})
		if err != nil {
			t.Fatal(err)
		}
		issues, _, err := s.Issues(time.Time{}, time.Time{}, 10)
		s.Close()
		if err != nil || len(issues) != 2 {
			t.Fatalf("%d issues, %v", len(issues), err)
		}
		if issues[0].Err != common || issues[0].Count != 4 {
			t.Fatalf("with %q the common one, first is %+v", common, issues[0])
		}
	}
}
