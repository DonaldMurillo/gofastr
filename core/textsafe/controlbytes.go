package textsafe

import (
	"strings"
	"unicode/utf8"
)

// IsControlByte reports whether c is an ASCII C0 control byte or DEL.
func IsControlByte(c byte) bool {
	return c < 0x20 || c == 0x7f
}

// HasControlBytes reports whether s holds an ASCII C0 control byte or DEL.
func HasControlBytes(s string) bool {
	for i := range s {
		if IsControlByte(s[i]) {
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
	for i := range len(s) {
		if IsControlByte(s[i]) {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func needsControlScrub(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return r < 0x20 || r == 0x7f || r >= utf8.RuneSelf
	})
}

// AppendPercentByte appends c to dst as the lowercase %xx the log and
// mail-header scrubs write for one unsafe byte.
func AppendPercentByte(dst []byte, c byte) []byte {
	const hexDigits = "0123456789abcdef"
	return append(dst, '%', hexDigits[c>>4], hexDigits[c&0x0f])
}

// ScrubControlBytes percent-encodes every IsUnsafe rune and stray 0x80–0x9F byte in s for a log line.
func ScrubControlBytes(s string) string {
	if !needsControlScrub(s) {
		return s
	}
	buf := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if IsControlByte(c) {
				buf = AppendPercentByte(buf, c)
			} else {
				buf = append(buf, c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size == 1 {
			if c <= 0x9f {
				buf = AppendPercentByte(buf, c)
			} else {
				buf = append(buf, c)
			}
			i++
			continue
		}
		if IsUnsafe(r) {
			for j := i; j < i+size; j++ {
				buf = AppendPercentByte(buf, s[j])
			}
		} else {
			buf = append(buf, s[i:i+size]...)
		}
		i += size
	}
	return string(buf)
}
