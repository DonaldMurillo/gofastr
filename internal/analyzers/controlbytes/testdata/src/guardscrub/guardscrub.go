// Package guardscrub is core/stream's SSEWriter around df472714: the
// queued id was tested for emptiness, then stripped, so an id made only
// of control bytes passed the test, stripped to "", and wrote an empty
// "id:" line, which resets the client's Last-Event-ID.
package guardscrub

import (
	"fmt"
	"net/http"
	"strings"
)

type SSEWriter struct {
	nextID string
	b      strings.Builder
}

func stripSSEControlChars(s string) string {
	if i := strings.IndexAny(s, "\r\n\x00"); i >= 0 {
		return s[:i]
	}
	return s
}

// writeEventPreFix is WriteEvent before df472714.
func (s *SSEWriter) writeEventPreFix() {
	if s.nextID != "" {
		s.b.WriteString("id: ")
		s.b.WriteString(stripSSEControlChars(s.nextID)) // want `controlbytes: s.nextID is tested for emptiness before stripSSEControlChars strips it`
		s.b.WriteByte('\n')
		s.nextID = ""
	}
}

// writeQueuedID is the fix: strip first, then test the result.
func (s *SSEWriter) writeQueuedID() {
	id := stripSSEControlChars(s.nextID)
	s.nextID = ""
	if id == "" {
		return
	}
	s.b.WriteString("id: ")
	s.b.WriteString(id)
}

func sanitizeID(s string) string { return strings.Map(dropCtl, s) }

func dropCtl(r rune) rune {
	if r < 0x20 {
		return -1
	}
	return r
}

func escapeValue(s string) string { return strings.ReplaceAll(s, "\n", `\n`) }

// The len spelling and a header write are the same shape.
func lenGuard(w http.ResponseWriter, id string) {
	if len(id) > 0 && w != nil {
		w.Header().Set("X-Request-Id", sanitizeID(id)) // want `controlbytes: id is tested for emptiness before sanitizeID strips it`
	}
}

// Quiet: the stripped result is tested before it is written.
func retested(b *strings.Builder, v string) {
	if v != "" {
		id := stripSSEControlChars(v)
		if id == "" {
			return
		}
		b.WriteString(id)
	}
}

// Near misses.
func nearMisses(b *strings.Builder, v, other string) string {
	// An escaper never empties a non-empty value.
	if v != "" {
		b.WriteString(escapeValue(v))
	}
	// Behind a fixed label an empty result is an empty label, not an
	// empty field.
	line := "event"
	if v != "" {
		line += "; tail: " + stripSSEControlChars(v)
	}
	// A formatted line carries its label in the format string.
	if v != "" {
		fmt.Fprintf(b, "fix: %s\n", stripSSEControlChars(v))
	}
	// The test and the strip name different values.
	if v != "" {
		b.WriteString(stripSSEControlChars(other))
	}
	// Reassigned before the strip: the test no longer covers it.
	if v != "" {
		v = other
		b.WriteString(stripSSEControlChars(v))
	}
	return line
}
