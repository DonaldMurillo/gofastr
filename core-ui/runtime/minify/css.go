package minify

import (
	"strings"
	"unicode/utf8"
)

// ServedCSS returns CSS(css) when Enabled, css otherwise.
func ServedCSS(css string) string {
	if !Enabled() {
		return css
	}
	return CSS(css)
}

// CSS minifies a stylesheet without parsing it; CSS(CSS(x)) == CSS(x).
func CSS(src string) string {
	m := cssMinifier{src: src, out: make([]byte, 0, len(src))}
	m.run()
	return string(m.out)
}

type cssMinifier struct {
	src           string
	out           []byte
	space         bool
	comment       bool
	punct         byte
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

func (m *cssMinifier) keepSpace(next byte) bool {
	switch m.punct {
	case '{', '}', ';', ',', '(':
		return false
	case ':':
		return next == ';' || next == '}'
	}
	switch next {
	case '{', '}', ';', ',', ')':
		return false
	}
	return true
}

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
