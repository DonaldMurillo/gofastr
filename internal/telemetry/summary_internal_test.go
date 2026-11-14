package telemetry

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// A trace's summary must end inside the bytes a query reads, whatever
// the route, the path and the error hold. Text that takes more room on
// disk than in memory (control characters, quotes, line separators)
// pushed the status and the error flag out of reach: a request to
// /%01%01… failed and was listed nowhere.
func TestTraceSummaryStaysInsideItsHead(t *testing.T) {
	for name, fill := range map[string]string{
		"control": "\x01", "quote": `"`, "backslash": `\`, "separator": " ", "wide": "日", "del": "\x7f", "bad utf-8": "\xff", "plain": "a",
	} {
		long := strings.Repeat(fill, 400)
		tr := Trace{Time: time.Unix(1700000000, 0), TraceID: strings.Repeat("a", 32),
			Route: "GET /" + long, Path: "/" + long, Err: "boom " + long,
			Status: 500, Dur: 2 * time.Second, Error: true, FailedSteps: 3,
			Spans: []Span{{SpanID: "s", Name: "x", Dur: time.Second, Error: true}}}
		line := AppendTrace(nil, tr)
		if !utf8.Valid(line) || bytes.ContainsAny(line[:len(line)-1], "\x00\x01\x7f\n") {
			t.Errorf("%s: the line is not clean UTF-8 on one line", name)
		}
		if at := bytes.Index(line, []byte(`,"spans":[`)); at < 0 || at >= traceHead {
			t.Errorf("%s: the summary ends at byte %d, past the %d a query reads", name, at, traceHead)
		}
		sum, ok := summary(line)
		if !ok || sum.Status != 500 || !sum.Error || sum.Dur != 2*time.Second || sum.FailedSteps != 3 || !strings.HasPrefix(sum.Err, "boom") {
			t.Errorf("%s: summary read back as %+v (ok %v)", name, sum, ok)
		}
		full, err := ParseTrace(line)
		if err != nil || full.Status != 500 {
			t.Errorf("%s: the line does not parse: %v", name, err)
		}
	}
}

// The cut counts bytes as written, falls between characters, and keeps
// ordinary text whole.
func TestSummaryText(t *testing.T) {
	for _, c := range []struct {
		in    string
		limit int
		want  string
	}{
		{"GET /orders/{id}", 256, "GET /orders/{id}"},
		{"abcdef", 4, "abcd"},
		{"日本語", 7, "日本"},
		{`a"b"c`, 5, `a"b`},
		{`\\\`, 4, `\\`},
		{"a\x00b\tc\nd\x7fe", 10, "abcde"},
		{"a\xffb", 10, "ab"},
		{"x y", 6, "x"},
		{"x y", 8, "x y"},
		{"", 10, ""},
		{"abc", 0, ""},
	} {
		if got := summaryText(c.in, c.limit); got != c.want {
			t.Errorf("summaryText(%q, %d) = %q, want %q", c.in, c.limit, got, c.want)
		}
	}
}
