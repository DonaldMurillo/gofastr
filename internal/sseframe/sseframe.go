// Package sseframe writes Server-Sent Event frames for core/stream and
// core/handler, so both emit the same bytes for the same event.
package sseframe

import "strings"

// Field returns s cut at its first CR, LF or NUL.
func Field(s string) string {
	if i := strings.IndexAny(s, "\r\n\x00"); i >= 0 {
		return s[:i]
	}
	return s
}

// Append writes one event frame to b: an id line when Field(id) is not
// empty, an event line when Field(event) is not empty or eventLine is
// set, one data line per LF-separated line of data with CR and NUL
// removed, and the blank line that ends the frame.
func Append(b *strings.Builder, id, event string, eventLine bool, data string) {
	if id = Field(id); id != "" {
		b.WriteString("id: ")
		b.WriteString(id)
		b.WriteByte('\n')
	}
	if event = Field(event); event != "" || eventLine {
		b.WriteString("event: ")
		b.WriteString(event)
		b.WriteByte('\n')
	}
	for line := range strings.SplitSeq(scrub(data), "\n") {
		b.WriteString("data: ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
}

func scrub(s string) string {
	if !strings.ContainsAny(s, "\r\x00") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if c := s[i]; c != '\r' && c != 0 {
			b.WriteByte(c)
		}
	}
	return b.String()
}
