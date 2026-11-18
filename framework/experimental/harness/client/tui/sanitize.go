package tui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

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

func stripANSISequences(s string) string {
	if !strings.ContainsAny(s, "\x1b\u0090\u0098\u009b\u009d\u009e\u009f") {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(rs); i++ {
		switch rs[i] {
		case 0x1b:
			if i+1 >= len(rs) {
				continue
			}
			i = skipEscaped(rs, i+1)
		case 0x9b:
			i = skipToFinal(rs, i+1)
		case 0x90, 0x98, 0x9d, 0x9e, 0x9f:
			i = skipToStringEnd(rs, i+1)
		default:
			b.WriteRune(rs[i])
		}
	}
	return b.String()
}

func skipEscaped(rs []rune, j int) int {
	switch rs[j] {
	case '[':
		return skipToFinal(rs, j+1)
	case ']', 'P', 'X', '^', '_':
		return skipToStringEnd(rs, j+1)
	default:
		k := j
		for k < len(rs) && rs[k] >= 0x20 && rs[k] <= 0x2f {
			k++
		}
		if k < len(rs) {
			k++
		}
		return k - 1
	}
}

func skipToFinal(rs []rune, j int) int {
	for k := j; k < len(rs); k++ {
		if rs[k] >= 0x40 && rs[k] <= 0x7e {
			return k
		}
	}
	return len(rs) - 1
}

func skipToStringEnd(rs []rune, j int) int {
	for k := j; k < len(rs); k++ {
		switch rs[k] {
		case 0x07, 0x9c:
			return k
		case 0x1b:
			if k+1 < len(rs) && rs[k+1] == '\\' {
				return k + 1
			}
			return k - 1
		}
	}
	return len(rs) - 1
}
