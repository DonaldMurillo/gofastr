package expr

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokNumber
	tokString
	tokPunct
)

type token struct {
	kind  tokenKind
	value string
	pos   int
}

// lex tokenizes src.
func lex(src string) ([]token, error) {
	var out []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case isSpace(c):
			i++

		case isDigit(c) || (c == '.' && i+1 < len(src) && isDigit(src[i+1])):
			start := i
			seenDot := false
			for i < len(src) && (isDigit(src[i]) || (!seenDot && src[i] == '.')) {
				if src[i] == '.' {
					seenDot = true
				}
				i++
			}
			out = append(out, token{kind: tokNumber, value: src[start:i], pos: start})

		case c == '"' || c == '\'':
			quote := c
			start := i
			i++
			var b strings.Builder
			for i < len(src) && src[i] != quote {
				if src[i] == '\\' && i+1 < len(src) {
					switch src[i+1] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case 'r':
						b.WriteByte('\r')
					case '\\':
						b.WriteByte('\\')
					case '"':
						b.WriteByte('"')
					case '\'':
						b.WriteByte('\'')
					default:
						b.WriteByte(src[i+1])
					}
					i += 2
					continue
				}
				b.WriteByte(src[i])
				i++
			}
			if i >= len(src) {
				return nil, fmt.Errorf("expr: unterminated string at %d", start)
			}
			i++ // closing quote
			out = append(out, token{kind: tokString, value: b.String(), pos: start})

		case isIdentStart(c):
			start := i
			for i < len(src) && isIdentCont(src[i]) {
				i++
			}
			out = append(out, token{kind: tokIdent, value: src[start:i], pos: start})

		default:
			// Multi-char punctuation first.
			matched := ""
			for _, p := range []string{"==", "!=", "<=", ">=", "&&", "||"} {
				if strings.HasPrefix(src[i:], p) {
					matched = p
					break
				}
			}
			if matched == "" {
				if strings.ContainsRune("+-*/%<>!().[],?:", rune(c)) {
					matched = string(c)
				}
			}
			if matched == "" {
				return nil, fmt.Errorf("expr: unexpected character %q at %d", c, i)
			}
			// Reject `..` directly: trailing-dot identifier would otherwise emit a single-char token.
			if matched == "." && i+1 < len(src) && src[i+1] == '.' {
				return nil, fmt.Errorf("expr: unexpected %q at %d", "..", i)
			}
			out = append(out, token{kind: tokPunct, value: matched, pos: i})
			i += len(matched)
		}
	}
	out = append(out, token{kind: tokEOF, pos: len(src)})
	return out, nil
}

func isSpace(c byte) bool      { return unicode.IsSpace(rune(c)) }
func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isIdentCont(c byte) bool  { return isIdentStart(c) || isDigit(c) }

// parseNumber returns an int64 if the value is a pure integer, else float64.
func parseNumber(s string) (any, error) {
	if !strings.Contains(s, ".") {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, err
		}
		return n, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, err
	}
	return f, nil
}
