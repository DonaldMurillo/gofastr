package telemetry

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"log/slog"
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
	Status  int
	Dur     time.Duration
	Error   bool
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
	status, _ := intField(h, `"status":`)
	sum.Status = int(status)
	us, _ := intField(h, `"dur_us":`)
	sum.Dur = time.Duration(us) * time.Microsecond
	sum.Error = bytes.Contains(h, []byte(`"error":true`))
	return sum, true
}

func head(line []byte) []byte { return line[:min(len(line), traceHead)] }

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
