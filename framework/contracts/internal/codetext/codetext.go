// Package codetext separates comments from code in Go and JavaScript
// source for the contracts text rules. It is a lexer, not a parser: it
// walks the text left to right and tracks string, raw-string, rune and
// template literals, so a "*/*" Accept value or a "locales/*.json" glob
// stays code instead of opening a comment that blanks the rest of the
// file.
package codetext

import "strings"

// Comment is one comment and the 1-indexed line it starts on. Text keeps
// its delimiters ("// …", "/* … */").
type Comment struct {
	Line int
	Text string
}

// Strip returns src with every // and /* */ comment removed. Newlines
// inside a comment are kept, so line numbers still match the original.
//
// A string literal is code, but the code it holds may carry its own
// comments: a generator emits "// fontFaceCSS holds the @font-face
// rules", and a stylesheet in a raw string has /* knob notes */. Those
// are prose too, so they go, with two limits that keep the "*/*" class
// out. A /* inside a literal opens a comment only when its */ closes
// inside the same literal ("image/*" never does). A // inside a literal
// opens one only at the literal's start or after whitespace or an
// escaped newline, with whitespace after it, and runs to the end of the
// line or the literal, whichever comes first ("https://cdn" and
// '//cdn.example/x.js' stay).
//
// script selects JavaScript literal rules: a backslash escapes inside a
// template literal, which it never does inside a Go raw string. A
// regular-expression literal is not tracked; a quote inside one at worst
// keeps the rest of that line as code.
func Strip(src string, script bool) string {
	var b strings.Builder
	b.Grow(len(src))
	scan(src, script, func(c byte) { b.WriteByte(c) }, nil)
	return b.String()
}

// Comments returns every comment in src, in order. A comment inside a
// string literal is not one: it belongs to the emitted code, and a
// directive spelled there is documentation, never a live directive.
func Comments(src string, script bool) []Comment {
	var out []Comment
	scan(src, script, nil, func(line int, text string) {
		out = append(out, Comment{Line: line, Text: text})
	})
	return out
}

// scan feeds each code byte (newlines inside comments included) to code
// and each whole top-level comment to comment. Either may be nil.
func scan(src string, script bool, code func(byte), comment func(line int, text string)) {
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
			comment(startLine, src[start:end])
		}
	}
	var quote byte
	// litLine/litBlock: inside a comment nested in the current literal.
	// litSpace: the previous literal byte lets a nested // open.
	litLine, litBlock, litSpace := false, false, false
	escapes := func() bool { return quote != '`' || script }
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case inLine:
			if c == '\n' {
				inLine = false
				closeComment(i)
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
				// An escaped quote does not close the literal.
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
					// An escaped newline or tab is whitespace in the
					// emitted text: "…\n// next line" starts a comment.
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
				// Only a backtick literal spans lines. A quote left open
				// here is a syntax error, and closing it at the newline
				// keeps one bad line from swallowing the file.
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

// closesInLiteral reports whether a "*/" occurs at or after i before the
// literal opened by quote ends.
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

// commentGap reports whether the byte at i, just past a "//" inside a
// literal, is the whitespace (or escape) a comment's text starts with.
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
