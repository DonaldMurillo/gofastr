package scan

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
)

// cssFile runs the CSS matchers over one stylesheet through the CSS
// tokenizer.
func (e *engine) cssFile(rel, src string) {
	toks := ownstyle.Tokenize(src)
	e.cssClasses(rel, toks)
	e.cssProperties(rel, toks)
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
					if cssClassMatches(toks[i].Text, name) {
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
			toks[j].Type == ownstyle.TokenFunction && toks[j].Text == "var(" {
			isVar = true
		}
		if !isDecl && !isVar {
			continue
		}
		for _, n := range e.cssNotes {
			for _, name := range n.Find.CSS.Properties {
				if t.Text == name {
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
