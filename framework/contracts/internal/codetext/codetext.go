// Package codetext separates comments from code in Go and JavaScript
// source for the contracts text rules, with a lexer that tracks string,
// raw-string, rune and template literals.
package codetext

import "strings"

// Comment is a comment's text, 1-based start line and half-open byte span.
type Comment struct {
	Line       int
	Text       string
	Start, End int
}

// Strip returns src with every // and /* */ comment removed, comments
// nested inside literals included, keeping newlines so line numbers
// match. script selects JavaScript literal rules.
func Strip(src string, script bool) string {
	var b strings.Builder
	b.Grow(len(src))
	scan(src, script, func(c byte) { b.WriteByte(c) }, nil)
	return b.String()
}

// Comments returns every top-level comment in src, in order; one inside
// a string literal is not returned.
func Comments(src string, script bool) []Comment {
	var out []Comment
	scan(src, script, nil, func(line, start, end int) {
		out = append(out, Comment{Line: line, Text: src[start:end], Start: start, End: end})
	})
	return out
}

func scan(src string, script bool, code func(byte), comment func(line, start, end int)) {
	emit := func(c byte) {
		if code != nil {
			code(c)
		}
	}
	line := 1
	inLine, inBlock := false, false
	start, startLine := 0, 0
	closeComment := func(end int) {
		if comment != nil {
			comment(startLine, start, end)
		}
	}
	var quote byte
	litLine, litBlock, litSpace := false, false, false
	escapes := func() bool { return quote != '`' || script }
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case inLine:
			if c == '\n' {
				inLine = false
				end := i
				if src[i-1] == '\r' {
					end--
				}
				closeComment(end)
				emit(c)
			}
		case inBlock:
			if c == '\n' {
				emit(c)
			} else if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				inBlock = false
				i++
				closeComment(i + 1)
			}
		case quote != 0 && (litLine || litBlock):
			switch {
			case c == '\\' && escapes():
				if i+1 < len(src) {
					i++
					if src[i] == '\n' {
						emit('\n')
						line++
					}
				}
			case c == quote:
				litLine, litBlock, quote = false, false, 0
				emit(c)
			case c == '\n':
				emit(c)
				litLine = false
				if quote != '`' {
					litBlock, quote = false, 0
				}
			case litBlock && c == '*' && i+1 < len(src) && src[i+1] == '/':
				litBlock = false
				litSpace = false
				i++
			}
		case quote != 0:
			switch {
			case c == '\\' && escapes():
				emit(c)
				if i+1 < len(src) {
					i++
					emit(src[i])
					litSpace = src[i] == 'n' || src[i] == 't' || src[i] == '\n'
					if src[i] == '\n' {
						line++
					}
				}
				goto next
			case c == quote:
				quote = 0
				emit(c)
			case c == '\n' && quote != '`':
				quote = 0
				emit(c)
			case c == '/' && i+1 < len(src) && src[i+1] == '/' && litSpace && commentGap(src, i+2):
				litLine = true
				i++
			case c == '/' && i+1 < len(src) && src[i+1] == '*' && closesInLiteral(src, i+2, quote, escapes()):
				litBlock = true
				i++
			default:
				emit(c)
			}
			litSpace = c == ' ' || c == '\t' || c == '\n'
		case c == '"' || c == '\'' || c == '`':
			quote = c
			litSpace = true
			emit(c)
		case c == '/' && i+1 < len(src) && (src[i+1] == '/' || src[i+1] == '*'):
			inLine, inBlock = src[i+1] == '/', src[i+1] == '*'
			start, startLine = i, line
			i++
		default:
			emit(c)
		}
	next:
		if c == '\n' {
			line++
		}
	}
	if inLine || inBlock {
		closeComment(len(src))
	}
}

func closesInLiteral(src string, i int, quote byte, escapes bool) bool {
	for ; i < len(src); i++ {
		switch c := src[i]; {
		case c == '\\' && escapes:
			i++
		case c == quote:
			return false
		case c == '\n' && quote != '`':
			return false
		case c == '*' && i+1 < len(src) && src[i+1] == '/':
			return true
		}
	}
	return false
}

func commentGap(src string, i int) bool {
	if i >= len(src) {
		return false
	}
	switch src[i] {
	case ' ', '\t', '\n', '\\':
		return true
	}
	return false
}
