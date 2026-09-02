// Package sinkmatch pins the sink-resolution side of the gate: the write
// happens in map order even when the call carries no selector syntax for
// the analyzer to match on — a method value bound before the loop, a
// package function bound to a variable, or a dot-imported fmt.
package sinkmatch

import (
	"fmt"
	"strconv"
	"strings"
)

func boundMethodValue(m map[string]string) string {
	var sb strings.Builder
	write := sb.WriteString
	for k := range m {
		write(k) // want `writing to output while ranging a map`
	}
	return sb.String()
}

func boundPackageFunc(m map[int]string) string {
	var sb strings.Builder
	fprintf := fmt.Fprintf
	for k := range m {
		fprintf(&sb, "%s", strconv.Itoa(k)) // want `writing to output while ranging a map`
	}
	return sb.String()
}

// sink is an output sink in everything but method set: it has the
// strings.Builder spelling of the write but no Write([]byte), so the
// io.Writer probe in isWriterish does not recognize it either.
type sink struct{ b strings.Builder }

func (s *sink) WriteString(v string) { s.b.WriteString(v) }

func customSinkType(m map[int]string) string {
	var s sink
	for _, v := range m {
		s.WriteString(v) // want `writing to output while ranging a map`
	}
	return s.b.String()
}
