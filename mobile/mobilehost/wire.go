package mobilehost

import (
	"net/http"
	"slices"
	"strings"
)

// The native bridge passes headers as "Name: value" lines: gomobile bind
// exports strings and byte slices, not maps.

// ParseHeaderLines reads "Name: value\n" lines into a header. Lines
// without a colon, and names or values carrying control bytes, are
// dropped: they could not have come from a well-formed request.
func ParseHeaderLines(s string) http.Header {
	h := http.Header{}
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSuffix(line, "\r")
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || hasCtl(name) || hasCtl(value) {
			continue
		}
		h.Add(name, value)
	}
	return h
}

// FormatHeaderLines writes h as sorted "Name: value\n" lines.
func FormatHeaderLines(h http.Header) string {
	var b strings.Builder
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range h {
			if !yield(k) {
				return
			}
		}
	}) {
		for _, v := range h[k] {
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(v)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func hasCtl(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 && c != '\t' || c == 0x7f {
			return true
		}
	}
	return false
}
