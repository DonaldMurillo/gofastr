package minify

import (
	"strings"
	"unicode/utf8"
)

// ServedCSS is a stylesheet as it goes on the wire: CSS(css) when the
// process serves minified code (Enabled), css unchanged otherwise.
// Every path that composes CSS for a browser sends it through here.
func ServedCSS(css string) string {
	if !Enabled() {
		return css
	}
	return CSS(css)
}

// CSS minifies a stylesheet without parsing it. The design system's
// sources carry long explanatory comments, and those, with the
// indentation, are about half of every sheet after gzip.
//
//   - Strip comments. A comment is not whitespace: `.a/**/.b` selects
//     one element with both classes, and a space would make it a
//     descendant combinator. So a comment goes where whitespace would
//     go too, beside `{` `;` and the like, and where no whitespace
//     surrounds it and the tokens around it could join (`1px/**/solid`)
//     it stays as an empty `/**/`.
//   - Collapse each whitespace run to one space, then drop the space
//     next to `{`, `}`, `;` and `,`, after `(` and `:`, and before `)`.
//     The space before `(` stays (`and (` must not become the function
//     token `and(`), and so does the space before `:` (`a :hover`
//     selects descendants), around `>`/`+`/`~`, and inside calc().
//   - Drop the run of `;` right before `}`: `a:b;;}` is `a:b}`.
//   - Keep strings, escapes and unquoted url() bodies byte for byte.
//     A hex escape keeps its terminating space (`\31 0` is "10").
//   - A space between `:` and `;` or `}` stays: `--x: ;` declares an
//     empty custom property, and `--x:;` is invalid in older engines.
//
// The output is a fixed point: CSS(CSS(x)) == CSS(x).
func CSS(src string) string {
	m := cssMinifier{src: src, out: make([]byte, 0, len(src))}
	m.run()
	return string(m.out)
}

type cssMinifier struct {
	src string
	out []byte
	// space records whitespace since the last token.
	space bool
	// comment records a comment since the last token.
	comment bool
	// punct is the last token when it was a bare `{};,(:` character,
	// 0 otherwise: an escaped `\;` or a string ending in `;` is not
	// punctuation and keeps the space after it.
	punct byte
	// semi is the length of the output just past a bare `;`, and
	// semiRun where the run of bare `;` ending there began, so a
	// following `}` can drop the whole run.
	semi, semiRun int
}

func (m *cssMinifier) run() {
	src := m.src
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return
			}
			i += 2 + end + 2
			m.comment = true
		case isCSSSpace(c):
			m.space = true
			i++
		case c == '"' || c == '\'':
			j := scanCSSString(src, i)
			m.token(src[i:j], 0)
			i = j
		case c == '\\':
			j := scanCSSEscape(src, i)
			m.token(src[i:j], 0)
			i = j
		case (c == 'u' || c == 'U') && isURLStart(src, i):
			i = m.url(i)
		case strings.IndexByte("{};,(:)", c) >= 0:
			m.token(src[i:i+1], c)
			i++
		default:
			j := i + 1
			for j < len(src) && !isCSSBreak(src[j]) {
				j++
			}
			m.token(src[i:j], 0)
			i = j
		}
	}
}

// token writes t, first settling the pending space. p is t's byte when
// t is bare punctuation, 0 otherwise.
func (m *cssMinifier) token(t string, p byte) {
	if (m.space || m.comment) && len(m.out) > 0 && m.keepSpace(p) {
		if m.space {
			m.out = append(m.out, ' ')
		} else {
			m.out = append(m.out, "/**/"...)
		}
	}
	m.space, m.comment = false, false
	if p == '}' && m.semi > 0 && m.semi == len(m.out) {
		m.out = m.out[:m.semiRun]
	}
	run := len(m.out)
	if p == ';' && m.semi > 0 && m.semi == len(m.out) {
		run = m.semiRun
	}
	m.out = append(m.out, t...)
	m.punct = p
	m.semi = 0
	if p == ';' {
		m.semi, m.semiRun = len(m.out), run
	}
}

// keepSpace reports whether the whitespace before a token whose bare
// punctuation byte is next (0 for anything else) is significant.
func (m *cssMinifier) keepSpace(next byte) bool {
	switch m.punct {
	case '{', '}', ';', ',', '(':
		return false
	case ':':
		// `--x: ;` and `--x: }` keep their space: an empty value.
		return next == ';' || next == '}'
	}
	switch next {
	case '{', '}', ';', ',', ')':
		return false
	}
	return true
}

// url copies an unquoted url(...) body verbatim (a url token may hold
// `/*` and `//`); a quoted one goes through the string path.
func (m *cssMinifier) url(i int) int {
	src := m.src
	j := i + 4
	for j < len(src) && isCSSSpace(src[j]) {
		j++
	}
	if j < len(src) && (src[j] == '"' || src[j] == '\'') {
		m.token(src[i:i+4], '(')
		return i + 4
	}
	// end is past the body's last unit that is not whitespace: an
	// escaped space (`url(a\ )`) is part of the URL.
	k, end := j, j
	for k < len(src) && src[k] != ')' {
		if src[k] == '\\' {
			k = scanCSSEscape(src, k)
			end = k
			continue
		}
		if !isCSSSpace(src[k]) {
			end = k + 1
		}
		k++
	}
	body := src[j:end]
	m.token(src[i:i+4]+body, 0)
	if k < len(src) {
		m.token(")", ')')
		k++
	}
	return k
}

// isURLStart reports whether src[i:] begins a url( function token: the
// name is not the tail of a longer identifier.
func isURLStart(src string, i int) bool {
	if len(src) < i+4 || !strings.EqualFold(src[i:i+4], "url(") {
		return false
	}
	return i == 0 || !isCSSNameByte(src[i-1])
}

func scanCSSString(src string, i int) int {
	q := src[i]
	j := i + 1
	for j < len(src) {
		switch src[j] {
		case '\\':
			j = scanCSSEscape(src, j)
			continue
		case q:
			return j + 1
		}
		j++
	}
	return j
}

// scanCSSEscape returns the end of the escape at src[i] (a backslash):
// up to six hex digits plus one terminating whitespace (CRLF counts as
// one), or the single code point after the backslash.
func scanCSSEscape(src string, i int) int {
	j := i + 1
	if j >= len(src) {
		return j
	}
	if !isHex(src[j]) {
		_, n := utf8.DecodeRuneInString(src[j:])
		return j + n
	}
	for k := 0; k < 6 && j < len(src) && isHex(src[j]); k++ {
		j++
	}
	if j < len(src) && isCSSSpace(src[j]) {
		if src[j] == '\r' && j+1 < len(src) && src[j+1] == '\n' {
			return j + 2
		}
		return j + 1
	}
	return j
}

func isCSSSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// isCSSBreak reports the bytes that end a run of ordinary characters.
func isCSSBreak(c byte) bool {
	return isCSSSpace(c) || c == '/' || c == '"' || c == '\'' || c == '\\' ||
		c == 'u' || c == 'U' || strings.IndexByte("{};,(:)", c) >= 0
}

func isCSSNameByte(c byte) bool {
	return c == '-' || c == '_' || c >= 0x80 ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
