package textsafe

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// HasControlBytes reports whether s holds an ASCII C0 control byte or DEL.
func HasControlBytes(s string) bool {
	for i := range s {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// SanitizeControlBytes removes every C0 control byte and DEL from s and leaves all other bytes.
func SanitizeControlBytes(s string) string {
	if !HasControlBytes(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func needsControlScrub(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return r < 0x20 || r == 0x7f || r >= utf8.RuneSelf
	})
}

// ScrubControlBytes percent-encodes every IsUnsafe rune and stray 0x80–0x9F byte in s for a log line.
func ScrubControlBytes(s string) string {
	if !needsControlScrub(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&b, "%%%02x", c)
			} else {
				b.WriteByte(c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size == 1 {
			if c <= 0x9f {
				fmt.Fprintf(&b, "%%%02x", c)
			} else {
				b.WriteByte(c)
			}
			i++
			continue
		}
		if IsUnsafe(r) {
			for j := i; j < i+size; j++ {
				fmt.Fprintf(&b, "%%%02x", s[j])
			}
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}
