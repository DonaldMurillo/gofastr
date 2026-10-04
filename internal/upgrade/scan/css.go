package scan

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
)

// cssFile runs the CSS matchers over one stylesheet through the CSS
// tokenizer.
func (e *engine) cssFile(rel, src string) {
	toks := ownstyle.Tokenize(src)
	e.cssClasses(rel, toks)
	e.cssProperties(rel, toks)
	e.cssSelectors(rel, toks)
}

// cssClasses matches ".name" class selectors (BEM forms count) in
// selector preludes, including nested rules and @media blocks. A "."
// followed by an ident is accepted only when a "{" proves the run was a
// prelude; a ";" or "}" proves it sat inside a declaration value
// (content: ".x", grid-template-areas: . a b) and drops it.
func (e *engine) cssClasses(rel string, toks []ownstyle.Token) {
	var pending []int // indices of ident tokens following a "." delim
	flush := func() {
		for _, i := range pending {
			for _, n := range e.cssNotes {
				for _, name := range n.Find.CSS.Classes {
					if cssClassMatches(cssUnescape(toks[i].Text), name) {
						e.add(n, Hit{File: rel, Line: toks[i].Line, Col: toks[i].Col, Why: "css ." + name})
					}
				}
			}
		}
		pending = nil
	}
	for i, t := range toks {
		if t.Type == ownstyle.TokenWhitespace || t.Type == ownstyle.TokenComment {
			continue
		}
		switch t.Type {
		case ownstyle.TokenOpenCurly:
			flush()
		case ownstyle.TokenSemicolon, ownstyle.TokenCloseCurly:
			pending = nil
		case ownstyle.TokenDelim:
			if t.Text != "." {
				continue
			}
			if j := nextSignificant(toks, i); j >= 0 && toks[j].Type == ownstyle.TokenIdent {
				pending = append(pending, j)
			}
		}
	}
}

// cssUnescape decodes the escapes in a raw ident token the way the CSS
// syntax spec consumes them: a backslash and one to six hex digits (plus
// the one whitespace after them) is that code point, U+FFFD for zero, a
// surrogate or anything past U+10FFFF; a backslash and any other
// character is that character. .ui\2d button names the class ui-button.
func cssUnescape(raw string) string {
	if !strings.Contains(raw, `\`) {
		return raw
	}
	var b strings.Builder
	for i := 0; i < len(raw); {
		if raw[i] != '\\' || i+1 >= len(raw) {
			b.WriteByte(raw[i])
			i++
			continue
		}
		i++ // the backslash
		j := i
		for j < len(raw) && j-i < 6 && isHexByte(raw[j]) {
			j++
		}
		if j == i {
			r, size := utf8.DecodeRuneInString(raw[i:])
			b.WriteRune(r)
			i += size
			continue
		}
		cp, _ := strconv.ParseUint(raw[i:j], 16, 32)
		if cp == 0 || (cp >= 0xD800 && cp <= 0xDFFF) || cp > unicode.MaxRune {
			cp = unicode.ReplacementChar
		}
		b.WriteRune(rune(cp))
		i = j
		if i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\f') {
			i++
		} else if strings.HasPrefix(raw[i:], "\r\n") {
			i += 2
		} else if i < len(raw) && raw[i] == '\r' {
			i++
		}
	}
	return b.String()
}

func isHexByte(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

// cssClassMatches is token equality or a BEM form of the name.
func cssClassMatches(token, name string) bool {
	if token == name {
		return true
	}
	rest, ok := strings.CutPrefix(token, name)
	return ok && (strings.HasPrefix(rest, "--") || strings.HasPrefix(rest, "__"))
}

// cssProperties matches custom property names: a declaration name (an
// ident before ":") or a var() argument.
func (e *engine) cssProperties(rel string, toks []ownstyle.Token) {
	for i, t := range toks {
		if t.Type != ownstyle.TokenIdent {
			continue
		}
		isDecl := false
		if j := nextSignificant(toks, i); j >= 0 && toks[j].Type == ownstyle.TokenColon {
			isDecl = true
		}
		isVar := false
		if j := prevSignificant(toks, i); j >= 0 &&
			toks[j].Type == ownstyle.TokenFunction && strings.EqualFold(toks[j].Text, "var(") {
			isVar = true
		}
		if !isDecl && !isVar {
			continue
		}
		ident := cssUnescape(t.Text)
		for _, n := range e.cssNotes {
			for _, name := range n.Find.CSS.Properties {
				if ident == name {
					e.add(n, Hit{File: rel, Line: t.Line, Col: t.Col, Why: "css " + name})
				}
			}
		}
	}
}

func nextSignificant(toks []ownstyle.Token, i int) int {
	for j := i + 1; j < len(toks); j++ {
		if toks[j].Type != ownstyle.TokenWhitespace && toks[j].Type != ownstyle.TokenComment {
			return j
		}
	}
	return -1
}

func prevSignificant(toks []ownstyle.Token, i int) int {
	for j := i - 1; j >= 0; j-- {
		if toks[j].Type != ownstyle.TokenWhitespace && toks[j].Type != ownstyle.TokenComment {
			return j
		}
	}
	return -1
}

// selectorRun is one rule prelude, normalized: significant tokens
// joined, one space where whitespace stood, none around a combinator
// or a comma. starts maps each token's offset in text to its position.
type selectorRun struct {
	text   strings.Builder
	starts []selectorTok
	space  bool
}

type selectorTok struct {
	off, line, col int
}

func isCombinator(t ownstyle.Token) bool {
	return t.Type == ownstyle.TokenComma ||
		(t.Type == ownstyle.TokenDelim && (t.Text == ">" || t.Text == "+" || t.Text == "~"))
}

func (s *selectorRun) add(t ownstyle.Token) {
	if t.Type == ownstyle.TokenWhitespace || t.Type == ownstyle.TokenComment {
		s.space = s.text.Len() > 0
		return
	}
	cur := s.text.String()
	if s.space && !isCombinator(t) && !strings.HasSuffix(cur, ">") && !strings.HasSuffix(cur, "+") &&
		!strings.HasSuffix(cur, "~") && !strings.HasSuffix(cur, ",") {
		s.text.WriteByte(' ')
	}
	s.space = false
	s.starts = append(s.starts, selectorTok{off: s.text.Len(), line: t.Line, col: t.Col})
	s.text.WriteString(t.Text)
}

// normalizeSelector reads a registry selector the way cssSelectors reads
// a prelude, so both sides compare in one spelling.
func normalizeSelector(sel string) string {
	var run selectorRun
	for _, t := range ownstyle.Tokenize(sel) {
		run.add(t)
	}
	return run.text.String()
}

// cssSelectors matches a selector entry (".a > .b") inside a rule
// prelude: the normalized compound appears in order, and does not run
// on into a longer name (.cui-slot-bare is not .cui-slot). Preludes
// are closed by "{"; a ";" or "}" first means the run was a
// declaration, which is dropped.
func (e *engine) cssSelectors(rel string, toks []ownstyle.Token) {
	var run selectorRun
	for _, t := range toks {
		switch t.Type {
		case ownstyle.TokenOpenCurly:
			e.matchSelectors(rel, &run)
			run = selectorRun{}
		case ownstyle.TokenSemicolon, ownstyle.TokenCloseCurly:
			run = selectorRun{}
		default:
			run.add(t)
		}
	}
}

func (e *engine) matchSelectors(rel string, run *selectorRun) {
	text := run.text.String()
	for _, n := range e.cssNotes {
		for _, sel := range n.Find.CSS.Selectors {
			want := normalizeSelector(sel)
			for from := 0; want != "" && from <= len(text); {
				i := strings.Index(text[from:], want)
				if i < 0 {
					break
				}
				at := from + i
				from = at + 1
				if !selectorBounded(text, at, len(want)) {
					continue
				}
				pos := run.starts[0]
				for _, s := range run.starts {
					if s.off > at {
						break
					}
					pos = s
				}
				e.add(n, Hit{File: rel, Line: pos.line, Col: pos.col, Why: "css " + sel})
			}
		}
	}
}

// selectorBounded: the match neither starts inside a name nor runs on
// into one.
func selectorBounded(text string, at, n int) bool {
	isName := func(c byte) bool {
		return c == '-' || c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	if end := at + n; end < len(text) && isName(text[end]) && isName(text[end-1]) {
		return false
	}
	if at > 0 && isName(text[at]) && isName(text[at-1]) {
		return false
	}
	return true
}
