package telemetry

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// LogEntry is one log line. Line is the JSON object as written.
type LogEntry struct {
	Time  time.Time
	Level slog.Level
	Line  json.RawMessage
}

// TraceSummary is what a trace list shows; TraceByID returns the rest.
type TraceSummary struct {
	Time    time.Time
	TraceID string
	Route   string
	Path    string
	Status  int
	Dur     time.Duration
	Error   bool
	// FailedSteps counts the failed spans under the root.
	FailedSteps int
	// Err is what went wrong, when something did.
	Err string
}

// ErrBadTraceID is returned for a trace ID that is not 32 hex digits.
var ErrBadTraceID = errors.New("telemetry: trace ID must be 32 hex digits")

// TailLogs returns the newest n lines at minLevel or above, oldest
// first.
func (s *Store) TailLogs(n int, minLevel slog.Level) ([]LogEntry, error) {
	var out []LogEntry
	err := s.logsBackward(time.Time{}, time.Time{}, n, func(line []byte) (LogEntry, bool) {
		e, ok := entry(line)
		return e, ok && e.Level >= minLevel
	}, &out)
	slices.Reverse(out)
	return out, err
}

// wordMatcher reports whether a stored line holds every word of text,
// in any order and any letter case (ASCII letters; others match as
// written). Words are what spaces and equals signs separate, so text
// copied off a screen that shows a line as "GET /search → 200" or
// "dur_ms=52" finds it: the file holds those as separate JSON values,
// and the arrow is the screen's own. A quote in a word matches a quote
// inside a value, never the JSON's own. Text wrapped in double quotes
// is one word, spaces and all: "mailbox busy" finds those two words
// side by side. Empty text matches every line.
//
// The returned func keeps a scratch buffer: one goroutine at a time.
func wordMatcher(text string) func(line []byte) bool {
	var needles [][]byte
	words := strings.FieldsFunc(text, func(r rune) bool {
		return r == '=' || r == '→' || unicode.IsSpace(r)
	})
	if t := strings.TrimSpace(text); len(t) > 2 && t[0] == '"' && t[len(t)-1] == '"' {
		words = []string{t[1 : len(t)-1]}
	}
	for _, w := range words {
		n := appendJSONString(nil, w)
		needles = append(needles, lowerASCII(n[1:len(n)-1]))
	}
	if len(needles) == 0 {
		return func([]byte) bool { return true }
	}
	var low []byte
	return func(line []byte) bool {
		low = lowerASCII(append(low[:0], line...))
		for _, n := range needles {
			if !bytes.Contains(low, n) {
				return false
			}
		}
		return true
	}
}

// lowerASCII lowers A to Z in place. It leaves every other byte alone,
// so it cannot map one character onto another the way Unicode folding
// does.
func lowerASCII(b []byte) []byte {
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return b
}

// SearchLogs returns up to limit lines stamped in [from, to] that hold
// every word of text, newest first. See wordMatcher for what a word is
// and how it is compared.
func (s *Store) SearchLogs(text string, from, to time.Time, limit int) ([]LogEntry, error) {
	has := wordMatcher(text)
	var out []LogEntry
	err := s.logsBackward(from, to, limit, func(line []byte) (LogEntry, bool) {
		if !has(line) {
			return LogEntry{}, false
		}
		e, ok := entry(line)
		return e, ok && inWindow(e.Time, from, to)
	}, &out)
	return out, err
}

// LogsForTrace returns the lines stamped in [from, to] that carry
// traceID, oldest first. The window is the trace's own start and end,
// which is what keeps this a short read.
func (s *Store) LogsForTrace(traceID string, from, to time.Time) ([]LogEntry, error) {
	needle, err := traceNeedle(traceID)
	if err != nil {
		return nil, err
	}
	var out []LogEntry
	err = s.scan(KindLogs, from, to, false, false, func(line []byte) bool {
		if bytes.Contains(line, needle) {
			if e, ok := entry(line); ok && inWindow(e.Time, from, to) {
				e.Line = bytes.Clone(e.Line) // line is the reader's buffer
				out = append(out, e)
			}
		}
		return true
	})
	return out, err
}

// TraceByID returns the trace's line, or false when no file holds it.
// It has no time to narrow by, so it asks every day, newest first: a
// sealed day answers from its ID table, a plain day is read through.
func (s *Store) TraceByID(traceID string) (json.RawMessage, bool, error) {
	needle, err := traceNeedle(traceID)
	if err != nil {
		return nil, false, err
	}
	days, err := s.days(KindTraces, time.Time{}, time.Time{})
	if err != nil {
		return nil, false, err
	}
	for _, d := range days {
		var found []byte
		if d.sealed {
			found, err = s.findSealed(d, traceID, needle)
		} else {
			err = s.scanPlain(d, time.Time{}, time.Time{}, false, func(line []byte) bool {
				if bytes.Contains(head(line), needle) {
					found = bytes.Clone(line)
					return false
				}
				return true
			})
		}
		// As scan does: a day packed since the listing answers from its
		// blocks, and one that is gone is passed over.
		if errors.Is(err, errDayMoved) {
			found, err = s.findSealed(d, traceID, needle)
		}
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		if err != nil || found != nil {
			return found, found != nil, err
		}
	}
	return nil, false, nil
}

// SlowestTraces returns the n longest traces that ended in [from, to],
// longest first. An empty route means every route.
func (s *Store) SlowestTraces(route string, from, to time.Time, n int) ([]TraceSummary, error) {
	if n <= 0 {
		return nil, nil
	}
	var needle []byte
	if route != "" {
		needle = appendJSONString([]byte(`"route":`), route)
	}
	var out []TraceSummary
	trim := func() {
		slices.SortFunc(out, func(a, b TraceSummary) int {
			return cmp.Or(cmp.Compare(b.Dur, a.Dur), b.Time.Compare(a.Time))
		})
		out = out[:min(len(out), n)]
	}
	err := s.scan(KindTraces, from, to, false, true, func(line []byte) bool {
		if needle != nil && !bytes.Contains(head(line), needle) {
			return true
		}
		if sum, ok := summary(line); ok && inWindow(sum.Time, from, to) {
			if out = append(out, sum); len(out) >= 4*n+64 {
				trim()
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	trim()
	return out, nil
}

// LogQuery narrows a log listing. The zero value lists everything.
type LogQuery struct {
	// Text, when set, names words the line must hold, as SearchLogs
	// matches them.
	Text string
	// MinLevel is the lowest level listed.
	MinLevel slog.Level
	// From and To bound the line's time, inclusive; zero leaves a side
	// open. A page of older lines is the same query with To set just
	// before the oldest line already shown.
	From, To time.Time
	// SkipPaths leaves out access lines for paths under these prefixes
	// ("/__gofastr/"): the fetches of scripts and styles that otherwise
	// fill a page of the log.
	SkipPaths []string
	// OldestFirst takes the limit from the old end of the window: the
	// lines just after From, which is the page of newer lines when From
	// is set just after the newest line already shown. The result is
	// still returned newest first.
	OldestFirst bool
}

// Logs returns up to limit lines matching q, newest first.
func (s *Store) Logs(q LogQuery, limit int) ([]LogEntry, error) {
	out, _, err := s.LogsWithin(q, limit, 0)
	return out, err
}

// budgetEvery is how many lines are read between looks at the clock.
const budgetEvery = 1024

// LogsWithin is Logs that gives up after budget (zero for no limit): a
// search for a rare word reads every line of every day, and a sealed
// day must be unpacked to be read. When it gives up it returns what it
// found and the time it had read to: lines on the far side of stopped
// (older, or newer for an OldestFirst query) were not looked at, and
// the same query bounded there carries on. A zero stopped means the
// whole window was read.
func (s *Store) LogsWithin(q LogQuery, limit int, budget time.Duration) (out []LogEntry, stopped time.Time, err error) {
	has := wordMatcher(q.Text)
	skips := make([][]byte, len(q.SkipPaths))
	for i, p := range q.SkipPaths {
		// The path attribute, up to but not including its closing quote.
		quoted := appendJSONString([]byte(`"path":`), p)
		skips[i] = quoted[:len(quoted)-1]
	}
	pick := func(line []byte) (LogEntry, bool) {
		if !has(line) {
			return LogEntry{}, false
		}
		for _, skip := range skips {
			if bytes.Contains(line, skip) {
				return LogEntry{}, false
			}
		}
		e, ok := entry(line)
		return e, ok && e.Level >= q.MinLevel && inWindow(e.Time, q.From, q.To)
	}
	if limit <= 0 {
		return nil, time.Time{}, nil
	}
	start, seen := time.Now(), 0
	err = s.scan(KindLogs, q.From, q.To, !q.OldestFirst, false, func(line []byte) bool {
		// Only lines inside the window count toward a look at the
		// clock, and only one of them can be where the search stops. A
		// scan starts a little outside its window, and a search resumed
		// from where the last one stopped must end up a full stride
		// further on: counting the lines outside, a stride was what
		// was left of it after them, which could be a few lines.
		if budget > 0 {
			if t, ok := lineTime(line); ok && inWindow(t, q.From, q.To) && !t.Equal(q.To) && !t.Equal(q.From) {
				if seen++; seen%budgetEvery == 0 && time.Since(start) > budget {
					stopped = t
					return false
				}
			}
		}
		if e, ok := pick(line); ok {
			e.Line = bytes.Clone(e.Line)
			out = append(out, e)
		}
		return len(out) < limit
	})
	if q.OldestFirst {
		slices.Reverse(out)
	}
	return out, stopped, err
}

// TraceQuery narrows a trace listing. The zero value lists everything.
type TraceQuery struct {
	// Route, when set, is the exact route listed.
	Route string
	// Failed lists only traces with a failed span.
	Failed bool
	// MinDur lists only traces that took at least this long.
	MinDur time.Duration
	// From and To bound when the trace ended, inclusive; zero leaves a
	// side open. To pages older traces, as in LogQuery.
	From, To time.Time
	// Issue, when set, lists only the failed traces of that issue (an
	// Issue's ID).
	Issue string
	// OldestFirst takes the limit from the old end of the window, as in
	// LogQuery. The result is still returned newest first.
	OldestFirst bool
}

// Traces returns up to limit trace summaries matching q, newest first.
func (s *Store) Traces(q TraceQuery, limit int) ([]TraceSummary, error) {
	if limit <= 0 {
		return nil, nil
	}
	var needle []byte
	if q.Route != "" {
		needle = appendJSONString([]byte(`"route":`), q.Route)
	}
	match := func(line []byte) (TraceSummary, bool) {
		if needle != nil && !bytes.Contains(head(line), needle) {
			return TraceSummary{}, false
		}
		sum, ok := summary(line)
		if !ok || !inWindow(sum.Time, q.From, q.To) || (q.Failed && !sum.Error) || sum.Dur < q.MinDur {
			return sum, false
		}
		return sum, q.Issue == "" || (sum.Error && IssueID(sum) == q.Issue)
	}
	var out []TraceSummary
	err := s.scan(KindTraces, q.From, q.To, !q.OldestFirst, true, func(line []byte) bool {
		if sum, ok := match(line); ok {
			out = append(out, sum)
		}
		return len(out) < limit
	})
	if q.OldestFirst {
		slices.Reverse(out)
	}
	return out, err
}

// maxMicros is the most microseconds a time.Duration holds.
const maxMicros = int64(math.MaxInt64 / int64(time.Microsecond))

// micros reads a count of microseconds off a line as a duration. A line
// is a file anyone with the directory can write: a count past what a
// duration holds stops at the most one can, and a negative one at zero,
// instead of wrapping into a small number that looks real.
func micros(n int64) time.Duration {
	if n < 0 {
		return 0
	}
	if n > maxMicros {
		n = maxMicros
	}
	return time.Duration(n) * time.Microsecond
}

// ParseTrace decodes a trace line, as TraceByID returns it.
func ParseTrace(line []byte) (Trace, error) {
	var wire struct {
		Time    time.Time `json:"time"`
		TraceID string    `json:"trace_id"`
		Route   string    `json:"route"`
		Path    string    `json:"path"`
		Status  int       `json:"status"`
		DurUS   int64     `json:"dur_us"`
		Error   bool      `json:"error"`
		Failed  int       `json:"failed_steps"`
		Err     string    `json:"err"`
		Spans   []struct {
			SpanID   string            `json:"span_id"`
			ParentID string            `json:"parent_id"`
			Name     string            `json:"name"`
			StartUS  int64             `json:"start_us"`
			DurUS    int64             `json:"dur_us"`
			Error    bool              `json:"error"`
			Attrs    map[string]string `json:"attrs"`
		} `json:"spans"`
		Stack string `json:"stack"`
	}
	if err := json.Unmarshal(line, &wire); err != nil {
		return Trace{}, fmt.Errorf("telemetry: trace line: %w", err)
	}
	t := Trace{Time: wire.Time, TraceID: wire.TraceID, Route: wire.Route, Path: wire.Path, Status: wire.Status,
		Dur: micros(wire.DurUS), Error: wire.Error, FailedSteps: wire.Failed, Err: wire.Err, Stack: wire.Stack, Spans: make([]Span, len(wire.Spans))}
	for i, ws := range wire.Spans {
		sp := Span{SpanID: ws.SpanID, ParentID: ws.ParentID, Name: ws.Name,
			Offset: micros(ws.StartUS), Dur: micros(ws.DurUS), Error: ws.Error}
		// A line holds attributes in the order they were set; a decoded
		// object does not keep it, so they come back sorted by key.
		for _, k := range slices.Sorted(maps.Keys(ws.Attrs)) {
			sp.Attrs = append(sp.Attrs, Attr{Key: k, Value: ws.Attrs[k]})
		}
		t.Spans[i] = sp
	}
	return t, nil
}

// logsBackward walks log lines newest first, keeping what pick accepts
// until *out holds limit entries.
func (s *Store) logsBackward(from, to time.Time, limit int, pick func([]byte) (LogEntry, bool), out *[]LogEntry) error {
	if limit <= 0 {
		return nil
	}
	return s.scan(KindLogs, from, to, true, false, func(line []byte) bool {
		if e, ok := pick(line); ok {
			e.Line = bytes.Clone(e.Line)
			*out = append(*out, e)
		}
		return len(*out) < limit
	})
}

func inWindow(t, from, to time.Time) bool {
	return (from.IsZero() || !t.Before(from)) && (to.IsZero() || !t.After(to))
}

// entry reads a log line's time and level. Line aliases the input.
func entry(line []byte) (LogEntry, bool) {
	t, ok := lineTime(line)
	if !ok {
		return LogEntry{}, false
	}
	e := LogEntry{Time: t, Line: line}
	if lv, ok := stringField(line, `"level":`); ok {
		// A level slog cannot read stays at Info: the line still shows.
		_ = e.Level.UnmarshalText([]byte(lv))
	}
	return e, true
}

// summary reads the fields AppendTrace writes ahead of the spans.
func summary(line []byte) (TraceSummary, bool) {
	t, ok := lineTime(line)
	if !ok {
		return TraceSummary{}, false
	}
	h := head(line)
	sum := TraceSummary{Time: t}
	if sum.TraceID, ok = stringField(h, `"trace_id":`); !ok {
		return TraceSummary{}, false
	}
	sum.Route, _ = stringField(h, `"route":`)
	sum.Path, _ = stringField(h, `"path":`)
	status, _ := intField(h, `"status":`)
	sum.Status = int(status)
	us, _ := intField(h, `"dur_us":`)
	sum.Dur = micros(us)
	sum.Error = bytes.Contains(h, []byte(`"error":true`))
	failed, _ := intField(h, `"failed_steps":`)
	sum.FailedSteps = int(failed)
	sum.Err, _ = stringField(h, `"err":`)
	return sum, true
}

// head is the part of a trace line that holds its summary: what comes
// before the spans, which carry "error" and "dur_us" keys of their own.
// The cut is exact: inside a string the quotes would be escaped.
func head(line []byte) []byte {
	h := line[:min(len(line), traceHead)]
	if i := bytes.Index(h, []byte(`,"spans":[`)); i >= 0 {
		h = h[:i]
	}
	return h
}

// traceNeedle is the bytes a line carrying traceID contains. Refusing
// anything but hex keeps a caller's string from shaping the match.
func traceNeedle(traceID string) ([]byte, error) {
	if len(traceID) != 32 {
		return nil, ErrBadTraceID
	}
	for i := 0; i < len(traceID); i++ {
		c := traceID[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return nil, ErrBadTraceID
		}
	}
	return []byte(`"trace_id":"` + traceID + `"`), nil
}

// stringField returns the string value that follows the first key in b.
// key is spelled with its quotes and colon; inside a JSON string those
// quotes would be escaped, so a match is a real key.
func stringField(b []byte, key string) (string, bool) {
	i := bytes.Index(b, []byte(key))
	if i < 0 {
		return "", false
	}
	rest := b[i+len(key):]
	if len(rest) == 0 || rest[0] != '"' {
		return "", false
	}
	for j := 1; j < len(rest); j++ {
		switch rest[j] {
		case '\\':
			j++
		case '"':
			var s string
			if json.Unmarshal(rest[:j+1], &s) != nil {
				return "", false
			}
			return s, true
		}
	}
	return "", false
}

// intField returns the integer that follows the first key in b.
func intField(b []byte, key string) (int64, bool) {
	i := bytes.Index(b, []byte(key))
	if i < 0 {
		return 0, false
	}
	rest := b[i+len(key):]
	j := 0
	for j < len(rest) && (rest[j] == '-' || (rest[j] >= '0' && rest[j] <= '9')) {
		j++
	}
	v, err := strconv.ParseInt(string(rest[:j]), 10, 64)
	return v, err == nil
}
