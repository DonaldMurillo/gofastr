package telemetry

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"
)

// File name parts. A day's files are <kind>-YYYYMMDD.jsonl, UTC.
const (
	KindLogs   = "logs"
	KindTraces = "traces"
	dayLayout  = "20060102"
	fileExt    = ".jsonl"
)

// FileName returns the file that holds kind's lines for t's UTC day.
func FileName(kind string, t time.Time) string {
	return kind + "-" + t.UTC().Format(dayLayout) + fileExt
}

// Attr is one span attribute. A slice of these, not a map, keeps the
// written bytes in the order the caller gave.
type Attr struct{ Key, Value string }

// Span is one span of a trace, timed relative to the trace's start.
type Span struct {
	SpanID   string
	ParentID string
	Name     string
	Offset   time.Duration
	Dur      time.Duration
	Attrs    []Attr
}

// Trace is one complete trace. Time is when its root span ended, which
// is when the line is written, so a trace file is ordered by Time.
type Trace struct {
	Time    time.Time
	TraceID string
	Route   string
	Status  int
	Dur     time.Duration
	Error   bool
	Spans   []Span
}

// traceHead bounds the bytes a query reads to learn a trace's summary:
// AppendTrace writes time, trace_id, route, status, dur_us and error
// before the spans, and a route is cut to fit.
const (
	traceHead   = 512
	maxRouteLen = 256
)

// AppendTrace appends t as one JSON line, newline included. The summary
// fields come first and in a fixed order: the readers depend on both.
func AppendTrace(dst []byte, t Trace) []byte {
	route := t.Route
	if len(route) > maxRouteLen {
		route = route[:maxRouteLen]
	}
	dst = append(dst, `{"time":"`...)
	dst = t.Time.UTC().AppendFormat(dst, time.RFC3339Nano)
	dst = append(dst, `","trace_id":`...)
	dst = appendJSONString(dst, t.TraceID)
	dst = append(dst, `,"route":`...)
	dst = appendJSONString(dst, route)
	dst = append(dst, `,"status":`...)
	dst = strconv.AppendInt(dst, int64(t.Status), 10)
	dst = append(dst, `,"dur_us":`...)
	dst = strconv.AppendInt(dst, t.Dur.Microseconds(), 10)
	dst = append(dst, `,"error":`...)
	dst = strconv.AppendBool(dst, t.Error)
	dst = append(dst, `,"spans":[`...)
	for i, s := range t.Spans {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = append(dst, `{"span_id":`...)
		dst = appendJSONString(dst, s.SpanID)
		dst = append(dst, `,"parent_id":`...)
		dst = appendJSONString(dst, s.ParentID)
		dst = append(dst, `,"name":`...)
		dst = appendJSONString(dst, s.Name)
		dst = append(dst, `,"start_us":`...)
		dst = strconv.AppendInt(dst, s.Offset.Microseconds(), 10)
		dst = append(dst, `,"dur_us":`...)
		dst = strconv.AppendInt(dst, s.Dur.Microseconds(), 10)
		if len(s.Attrs) > 0 {
			dst = append(dst, `,"attrs":{`...)
			for j, a := range s.Attrs {
				if j > 0 {
					dst = append(dst, ',')
				}
				dst = appendJSONString(dst, a.Key)
				dst = append(dst, ':')
				dst = appendJSONString(dst, a.Value)
			}
			dst = append(dst, '}')
		}
		dst = append(dst, '}')
	}
	return append(dst, "]}\n"...)
}

// appendJSONString appends s as a JSON string the way slog's JSON
// handler spells it: no HTML escaping. Needles built with it match the
// bytes on disk in both file kinds.
func appendJSONString(dst []byte, s string) []byte {
	plain := true
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c >= 0x7f || c == '"' || c == '\\' {
			plain = false
			break
		}
	}
	if plain {
		dst = append(dst, '"')
		dst = append(dst, s...)
		return append(dst, '"')
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		// A string always encodes; keep the line valid if that changes.
		return append(dst, `""`...)
	}
	return append(dst, bytes.TrimSuffix(buf.Bytes(), []byte{'\n'})...)
}
