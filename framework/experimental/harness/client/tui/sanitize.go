package tui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// sanitizeModalText scrubs a modal title or body line one step past
// textsafe.ScrubTerminal: whole ANSI escape sequences (OSC title rewrites,
// CSI mode flips) are removed WITH their parameter bytes, not just
// their introducer. Byte-level scrubbing alone leaves the defanged
// payload visible — "\x1b[?1049h" becomes "[?1049h" — and modal body
// lines are model-borne (the /tasks snapshot renders TaskList content
// verbatim), so the digits of an alternate-screen flip must not
// survive as visible text inside the frame. Embedded newlines become
// spaces: a modal line is one visual row, and a raw newline would
// break the frame geometry.
func sanitizeModalText(s string) string {
	s = stripANSISequences(s)
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return ' '
		}
		if r == '\t' {
			return r
		}
		if textsafe.IsUnsafe(r) {
			return -1
		}
		return r
	}, s)
}

// stripANSISequences removes complete terminal escape sequences: an
// ESC or C1 CSI introducer through its final byte, and OSC/DCS/PM/APC
// strings through their BEL or ST terminator. The 8-bit C1 introducers
// (U+009B CSI, U+009D OSC, U+009E PM, U+009F APC) drive the same
// sequences in C1-aware terminals and get the same treatment.
func stripANSISequences(s string) string {
	if !strings.ContainsAny(s, "\x1b") {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(rs); i++ {
		switch rs[i] {
		case 0x1b: // ESC
			if i+1 >= len(rs) {
				continue // dangling introducer: drop it
			}
			i = skipEscaped(rs, i+1)
		case 0x9b: // 8-bit CSI
			i = skipToFinal(rs, i+1)
		case 0x9d, 0x9e, 0x9f: // 8-bit OSC / PM / APC string
			i = skipToStringEnd(rs, i+1)
		default:
			b.WriteRune(rs[i])
		}
	}
	return b.String()
}

// skipEscaped consumes one sequence's body after ESC and returns the
// index of the last consumed rune: '[' is CSI (through the final
// byte), ']','P','X','^','_' open strings (through BEL/ST), anything
// else is a two-byte ESC sequence with optional intermediates.
func skipEscaped(rs []rune, j int) int {
	switch rs[j] {
	case '[':
		return skipToFinal(rs, j+1)
	case ']', 'P', 'X', '^', '_':
		return skipToStringEnd(rs, j+1)
	default:
		k := j
		for k < len(rs) && rs[k] >= 0x20 && rs[k] <= 0x2f {
			k++ // intermediate bytes (charset designators et al.)
		}
		if k < len(rs) {
			k++ // the final byte
		}
		return k - 1
	}
}

// skipToFinal consumes a CSI parameter run and returns the index of
// the final byte (0x40–0x7E per ECMA-48), or of the last rune when
// the input ends first.
func skipToFinal(rs []rune, j int) int {
	for k := j; k < len(rs); k++ {
		if rs[k] >= 0x40 && rs[k] <= 0x7e {
			return k
		}
	}
	return len(rs) - 1
}

// skipToStringEnd consumes an OSC/DCS/PM/APC string through its BEL
// or ST terminator. A bare ESC inside the string (no following '\')
// ends the skip before it so it is re-examined as a new introducer.
func skipToStringEnd(rs []rune, j int) int {
	for k := j; k < len(rs); k++ {
		switch rs[k] {
		case 0x07, 0x9c: // BEL, 8-bit ST
			return k
		case 0x1b:
			if k+1 < len(rs) && rs[k+1] == '\\' {
				return k + 1 // ST
			}
			return k - 1
		}
	}
	return len(rs) - 1
}
