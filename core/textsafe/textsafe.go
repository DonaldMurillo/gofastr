// Package textsafe holds the predicates and scrubs for characters that forge, reorder or hide text.
package textsafe

import (
	"strings"
	"unicode/utf8"
)

// Storable reports whether s is valid UTF-8 without a NUL byte, the text Postgres stores unchanged.
func Storable(s string) bool {
	return utf8.ValidString(s) && strings.IndexByte(s, 0) < 0
}

// IsInvisible reports whether r is a zero-width, joiner, bidi, Unicode tag or Hangul filler codepoint.
func IsInvisible(r rune) bool {
	if r >= tagFirst && r <= tagLast {
		return true
	}
	switch r {
	case 0x115F, 0x1160, 0x3164, 0xFFA0:
		return true
	case 0x200B, 0x200C, 0x200D,
		0x200E, 0x200F,
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E,
		0x2066, 0x2067, 0x2068, 0x2069,
		0x061C,
		0x180E,
		0xFEFF,
		0x2060,
		0x2061, 0x2062, 0x2063, 0x2064,
		0x202F:
		return true
	}
	return false
}

// IsC1 reports whether r is in the C1 control block (U+0080–U+009F).
func IsC1(r rune) bool { return r >= 0x80 && r <= 0x9F }

// IsLineSeparator reports whether r is U+2028 LINE SEPARATOR or U+2029 PARAGRAPH SEPARATOR.
func IsLineSeparator(r rune) bool { return r == 0x2028 || r == 0x2029 }

// IsUnsafe reports whether r is a C0 control, DEL, a C1 control, a line or paragraph separator or invisible.
func IsUnsafe(r rune) bool {
	return r < 0x20 || r == 0x7F || IsC1(r) || IsLineSeparator(r) || IsInvisible(r)
}

const (
	tagFirst  = 0xE0000
	tagLast   = 0xE007F
	cancelTag = 0xE007F
	blackFlag = 0x1F3F4
)

func flagTagLen(s string) int {
	n, i := 0, 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == cancelTag:
			if n >= 3 && n <= 7 {
				return i + size
			}
			return 0
		case r >= tagFirst+'a' && r <= tagFirst+'z', r >= tagFirst+'0' && r <= tagFirst+'9':
			n++
			if n > 7 {
				return 0
			}
			i += size
		default:
			return 0
		}
	}
	return 0
}

func scan(s string, hit func(rune) bool, keep func(string)) bool {
	found, start := false, 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == blackFlag {
			i += size + flagTagLen(s[i+size:])
			continue
		}
		if !hit(r) {
			i += size
			continue
		}
		found = true
		if keep == nil {
			return true
		}
		keep(s[start:i])
		i += size
		start = i
	}
	if keep != nil && found {
		keep(s[start:])
	}
	return found
}

func strip(s string, hit func(rune) bool) string {
	if !scan(s, hit, nil) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	scan(s, hit, func(part string) { b.WriteString(part) })
	return b.String()
}

// StripInvisible removes every IsInvisible codepoint from s, keeping valid subdivision flags.
func StripInvisible(s string) string { return strip(s, IsInvisible) }

// StripUnsafe removes every IsUnsafe codepoint from s, keeping valid subdivision flags.
func StripUnsafe(s string) string { return strip(s, IsUnsafe) }

// ContainsInvisible reports whether s holds an IsInvisible codepoint outside a valid subdivision flag.
func ContainsInvisible(s string) bool { return scan(s, IsInvisible, nil) }

// ContainsUnsafe reports whether s holds an IsUnsafe codepoint outside a valid subdivision flag.
func ContainsUnsafe(s string) bool { return scan(s, IsUnsafe, nil) }
