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
	"slices"
	"strconv"
	"time"
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

// SearchLogs returns up to limit lines stamped in [from, to] whose JSON
// contains text, newest first. text is plain text, matched
// case-sensitively against the line as written: it finds a message, an
// attribute value or a key, and a quote in it matches a quote inside a
// value, never the JSON's own.
func (s *Store) SearchLogs(text string, from, to time.Time, limit int) ([]LogEntry, error) {
	needle := appendJSONString(nil, text)
	needle = needle[1 : len(needle)-1]
	var out []LogEntry
	err := s.logsBackward(from, to, limit, func(line []byte) (LogEntry, bool) {
		if !bytes.Contains(line, needle) {
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
	files, err := s.files(KindLogs, from, to)
	if err != nil {
		return nil, err
	}
	var out []LogEntry
	for _, df := range slices.Backward(files) { // oldest file first
		w, err := s.open(df, from, to)
		if err != nil {
			return nil, err
		}
		seen := 0
		err = scanForward(w.f, w.start, w.end, func(line []byte) bool {
			if seen++; seen%boundEvery == 0 {
				if t, ok := lineTime(line); ok && !to.IsZero() && t.After(to.Add(timeSlop)) {
					return false
				}
			}
			if bytes.Contains(line, needle) {
				if e, ok := entry(line); ok && inWindow(e.Time, from, to) {
					e.Line = bytes.Clone(e.Line) // line is the reader's buffer
					out = append(out, e)
				}
			}
			return true
		})
		_ = w.f.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// TraceByID returns the trace's line, or false when no file holds it.
// With no time to narrow by, it reads every trace file, newest first.
func (s *Store) TraceByID(traceID string) (json.RawMessage, bool, error) {
	needle, err := traceNeedle(traceID)
	if err != nil {
		return nil, false, err
	}
	files, err := s.files(KindTraces, time.Time{}, time.Time{})
	if err != nil {
		return nil, false, err
	}
	var found json.RawMessage
	for _, df := range files {
		w, err := s.open(df, time.Time{}, time.Time{})
		if err != nil {
			return nil, false, err
		}
		err = scanForward(w.f, w.start, w.end, func(line []byte) bool {
			if bytes.Contains(head(line), needle) {
				found = bytes.Clone(line)
				return false
			}
			return true
		})
		_ = w.f.Close()
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
	files, err := s.files(KindTraces, from, to)
	if err != nil {
		return nil, err
	}
	var out []TraceSummary
	trim := func() {
		slices.SortFunc(out, func(a, b TraceSummary) int {
			return cmp.Or(cmp.Compare(b.Dur, a.Dur), b.Time.Compare(a.Time))
		})
		out = out[:min(len(out), n)]
	}
	for _, df := range files {
		w, err := s.open(df, from, to)
		if err != nil {
			return nil, err
		}
		seen := 0
		err = scanForward(w.f, w.start, w.end, func(line []byte) bool {
			if seen++; seen%boundEvery == 0 {
				if t, ok := lineTime(line); ok && !to.IsZero() && t.After(to.Add(timeSlop)) {
					return false
				}
			}
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
		_ = w.f.Close()
		if err != nil {
			return nil, err
		}
	}
	trim()
	return out, nil
}

// LogQuery narrows a log listing. The zero value lists everything.
type LogQuery struct {
	// Text, when set, must appear in the line, as SearchLogs matches it.
	Text string
	// MinLevel is the lowest level listed.
	MinLevel slog.Level
	// From and To bound the line's time, inclusive; zero leaves a side
	// open. A page of older lines is the same query with To set just
	// before the oldest line already shown.
	From, To time.Time
}

// Logs returns up to limit lines matching q, newest first.
func (s *Store) Logs(q LogQuery, limit int) ([]LogEntry, error) {
	var needle []byte
	if q.Text != "" {
		needle = appendJSONString(nil, q.Text)
		needle = needle[1 : len(needle)-1]
	}
	var out []LogEntry
	err := s.logsBackward(q.From, q.To, limit, func(line []byte) (LogEntry, bool) {
		if needle != nil && !bytes.Contains(line, needle) {
			return LogEntry{}, false
		}
		e, ok := entry(line)
		return e, ok && e.Level >= q.MinLevel && inWindow(e.Time, q.From, q.To)
	}, &out)
	return out, err
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
	files, err := s.files(KindTraces, q.From, q.To)
	if err != nil {
		return nil, err
	}
	var out []TraceSummary
	for _, df := range files {
		w, err := s.open(df, q.From, q.To)
		if err != nil {
			return nil, err
		}
		seen := 0
		err = scanBackward(w.f, w.end, func(line []byte) bool {
			if seen++; seen%boundEvery == 0 && !q.From.IsZero() {
				if t, ok := lineTime(line); ok && t.Before(q.From.Add(-timeSlop)) {
					return false
				}
			}
			if needle != nil && !bytes.Contains(head(line), needle) {
				return true
			}
			sum, ok := summary(line)
			if !ok || !inWindow(sum.Time, q.From, q.To) || (q.Failed && !sum.Error) || sum.Dur < q.MinDur {
				return true
			}
			out = append(out, sum)
			return len(out) < limit
		})
		_ = w.f.Close()
		if err != nil || len(out) >= limit {
			return out, err
		}
	}
	return out, nil
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
		Spans   []struct {
			SpanID   string            `json:"span_id"`
			ParentID string            `json:"parent_id"`
			Name     string            `json:"name"`
			StartUS  int64             `json:"start_us"`
			DurUS    int64             `json:"dur_us"`
			Error    bool              `json:"error"`
			Attrs    map[string]string `json:"attrs"`
		} `json:"spans"`
	}
	if err := json.Unmarshal(line, &wire); err != nil {
		return Trace{}, fmt.Errorf("telemetry: trace line: %w", err)
	}
	t := Trace{Time: wire.Time, TraceID: wire.TraceID, Route: wire.Route, Path: wire.Path, Status: wire.Status,
		Dur: micros(wire.DurUS), Error: wire.Error, FailedSteps: wire.Failed, Spans: make([]Span, len(wire.Spans))}
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

// logsBackward walks log lines newest first across day files, keeping
// what pick accepts until *out holds limit entries.
func (s *Store) logsBackward(from, to time.Time, limit int, pick func([]byte) (LogEntry, bool), out *[]LogEntry) error {
	if limit <= 0 {
		return nil
	}
	files, err := s.files(KindLogs, from, to)
	if err != nil {
		return err
	}
	for _, df := range files {
		w, err := s.open(df, from, to)
		if err != nil {
			return err
		}
		seen := 0
		err = scanBackward(w.f, w.end, func(line []byte) bool {
			if seen++; seen%boundEvery == 0 && !from.IsZero() {
				if t, ok := lineTime(line); ok && t.Before(from.Add(-timeSlop)) {
					return false
				}
			}
			if e, ok := pick(line); ok {
				e.Line = bytes.Clone(e.Line)
				*out = append(*out, e)
			}
			return len(*out) < limit
		})
		_ = w.f.Close()
		if err != nil || len(*out) >= limit {
			return err
		}
	}
	return nil
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
