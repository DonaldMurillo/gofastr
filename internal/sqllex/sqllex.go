// Package sqllex skips the lexical runs of a SQL text that can hold a
// semicolon or a value: quoted strings, block comments and Postgres
// dollar-quoted strings.
package sqllex

// SkipQuoted returns the index after the string opened by the quote at
// i. A doubled quote is an escaped one and, with escapes, so is any byte
// after a backslash. An unterminated string runs to the end.
func SkipQuoted(s string, i int, escapes bool) int {
	quote := s[i]
	for j := i + 1; j < len(s); j++ {
		if escapes && s[j] == '\\' {
			j++
			continue
		}
		if s[j] == quote {
			if j+1 < len(s) && s[j+1] == quote {
				j++
				continue
			}
			return j + 1
		}
	}
	return len(s)
}

// SkipBlockComment returns the index after the /* comment opening at i.
// With nest, an inner /* opens a level a */ must close first, as
// Postgres reads them. An unterminated comment runs to the end.
func SkipBlockComment(s string, i int, nest bool) int {
	depth := 0
	for j := i; j+1 < len(s); {
		switch {
		case s[j] == '/' && s[j+1] == '*' && (nest || depth == 0):
			depth++
			j += 2
		case s[j] == '*' && s[j+1] == '/':
			depth--
			j += 2
			if depth == 0 {
				return j
			}
		default:
			j++
		}
	}
	return len(s)
}

// DollarTag returns the $tag$ opening a Postgres dollar-quoted string at
// the start of s, or "" when s opens a $1 parameter or nothing. The tag
// is an identifier, so a byte outside ASCII counts as a letter.
func DollarTag(s string) string {
	for j := 1; j < len(s); j++ {
		switch c := s[j]; {
		case c == '$':
			return s[:j+1]
		case (c|0x20 >= 'a' && c|0x20 <= 'z') || c == '_' || c >= 0x80 || (j > 1 && c >= '0' && c <= '9'):
		default:
			return ""
		}
	}
	return ""
}
