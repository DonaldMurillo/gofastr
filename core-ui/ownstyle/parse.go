package ownstyle

import (
	"fmt"
	"strings"
)

// Pos is a 1-based source position. Columns count runes.
type Pos struct {
	Line int
	Col  int
}

// Stylesheet is a parsed owned-style source: a list of top-level rules.
type Stylesheet struct {
	Rules []*Rule
}

// Rule is one rule.
type Rule struct {
	// At reports an at-rule; Name is its keyword without '@', lowercased
	// ("media", "keyframes").
	At   bool
	Name string

	// Prel is the prelude.
	Prel []Token

	// Block is true when the rule ended with '{…}' rather than ';'.
	Block bool

	// Decls are the block's declarations, in source order.
	Decls []*Decl

	// Nested are the block's nested rules, in source order.
	Nested []*Rule

	// Doc is the comment block immediately preceding the rule.
	Doc string

	// Pos is the first significant token of the prelude.
	Pos Pos

	// Text is the prelude's source text with outer whitespace trimmed — the
	// selector string diagnostics quote and GOFASTR1813 matches.
	Text string
}

// Decl is one declaration, such as "padding: 16px !important" or "--accent: red".
type Decl struct {
	// Name is the property name.
	Name string
	// Value holds the value tokens with interior trivia and without priority.
	Value []Token
	// Important reports a trailing !important; ImpPos points at the '!'.
	Important bool
	ImpPos    Pos
	// Pos points at the name token; ValuePos at the first value token (zero Pos
	// when the value is empty).
	Pos      Pos
	ValuePos Pos
	// ValueSrc is the raw source slice from the first to the last significant
	// value token, interior trivia included — the value a browser reads, the
	// string GOFASTR1807 judges whole.
	ValueSrc string
}

type parser struct {
	src    string
	toks   []Token
	i      int
	issues []parseIssue
}

type parseIssue struct {
	pos Pos
	msg string
}

// Parse parses an owned-style stylesheet.
func Parse(src string) (*Stylesheet, []Diagnostic) {
	src = stripBOM(src)
	p := &parser{src: src, toks: tokenize(src)}
	rules := p.parseRuleList(false)
	diags := make([]Diagnostic, 0, len(p.issues))
	for _, is := range p.issues {
		diags = append(diags, Diagnostic{
			Rule:     RuleParseError,
			Severity: SeverityError,
			Line:     is.pos.Line,
			Col:      is.pos.Col,
			Message:  is.msg,
		})
	}
	return &Stylesheet{Rules: rules}, diags
}

func (p *parser) peek() Token {
	if p.i < len(p.toks) {
		return p.toks[p.i]
	}
	return Token{}
}

func (p *parser) next() Token {
	t := p.peek()
	if p.i < len(p.toks) {
		p.i++
	}
	return t
}

func (p *parser) errorf(pos Pos, format string, args ...any) {
	p.issues = append(p.issues, parseIssue{pos, fmt.Sprintf(format, args...)})
}

func tokPos(t Token) Pos { return Pos{Line: t.Line, Col: t.Col} }

type docAccum struct {
	lines   []string
	onLine  bool
	pending bool
}

func (d *docAccum) observe(t Token) {
	switch t.Type {
	case TokenComment:
		if d.onLine {
			d.lines = append(d.lines, stripCommentMarkers(t.Text))
			d.pending = true
		}
		if strings.ContainsAny(t.Text, "\n\r\f") {
			d.onLine = true
		}
	case TokenWhitespace:
		if strings.ContainsAny(t.Text, "\n\r\f") {
			d.onLine = true
		}
	}
}

func (d *docAccum) take() string {
	out := ""
	if d.pending {
		out = strings.Trim(strings.Join(d.lines, "\n"), "\n")
	}
	d.lines, d.pending, d.onLine = nil, false, false
	return out
}

func stripCommentMarkers(text string) string {
	s := strings.TrimPrefix(text, "/*")
	s = strings.TrimSuffix(s, "*/")
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimSpace(ln)
	}
	return strings.Join(lines, "\n")
}

func (p *parser) parseRuleList(inBlock bool) []*Rule {
	var rules []*Rule
	doc := &docAccum{onLine: true}
	for {
		t := p.peek()
		switch {
		case t.Type == 0:
			return rules
		case t.Type == TokenWhitespace || t.Type == TokenComment:
			doc.observe(t)
			p.next()
		case t.Type == TokenCloseCurly && inBlock:
			p.next()
			return rules
		case t.Type == TokenCloseCurly:
			p.errorf(tokPos(t), "unexpected '}'")
			p.next()
			doc.take()
		default:
			if rule := p.parseRule(doc.take()); rule != nil {
				rules = append(rules, rule)
			}
		}
	}
}

func (p *parser) parseRule(doc string) *Rule {
	r := &Rule{Doc: doc}
	var prel []Token
	depth := 0
	var startTok Token
	haveStart := false
	srcStart, srcEnd := -1, -1
	done := false
	for !done {
		t := p.peek()
		if t.Type == 0 {
			pos := Pos{1, 1}
			if haveStart {
				pos = tokPos(startTok)
			}
			p.errorf(pos, "unexpected end of stylesheet inside a prelude")
			break
		}
		switch t.Type {
		case TokenFunction, TokenOpenParen, TokenOpenSquare:
			depth++
		case TokenCloseParen, TokenCloseSquare:
			depth--
		case TokenOpenCurly:
			if depth == 0 {
				p.next()
				r.Block = true
				done = true
			} else {
				depth++
			}
		case TokenSemicolon:
			if depth == 0 {
				p.next()
				done = true
			}
		case TokenCloseCurly:
			if depth == 0 {
				p.errorf(tokPos(t), "expected '{' or ';' after prelude %q, found '}'", tokenRunText(prel))
				return nil
			}
			depth--
		case TokenBadString:
			p.errorf(tokPos(t), "unterminated string: %s", t.Text)
		case TokenBadURL:
			p.errorf(tokPos(t), "invalid url(): %s", t.Text)
		case TokenString:
			if unterminatedString(t) {
				p.errorf(tokPos(t), "unterminated string: %s", t.Text)
			}
		}
		if done {
			break
		}
		if significant(t) {
			if !haveStart {
				startTok, haveStart = t, true
				srcStart = t.Start
			}
			srcEnd = t.End
		}
		prel = append(prel, t)
		p.next()
	}
	r.Prel = trimTokens(prel)
	if haveStart {
		r.Pos = tokPos(startTok)
		r.Text = strings.TrimSpace(p.src[srcStart:srcEnd])
	}
	if len(r.Prel) > 0 && r.Prel[0].Type == TokenAtKeyword {
		r.At = true
		r.Name = strings.ToLower(strings.TrimPrefix(r.Prel[0].Text, "@"))
	}
	if r.Block {
		r.Decls, r.Nested = p.parseBlock()
	}
	return r
}

func (p *parser) parseBlock() (decls []*Decl, nested []*Rule) {
	doc := &docAccum{onLine: true}
	sawNested := false
	for {
		t := p.peek()
		switch {
		case t.Type == 0:
			p.errorf(Pos{1, 1}, "unexpected end of stylesheet inside a block: unclosed '{'")
			return decls, nested
		case t.Type == TokenWhitespace || t.Type == TokenComment:
			doc.observe(t)
			p.next()
		case t.Type == TokenCloseCurly:
			p.next()
			return decls, nested
		default:
			run, opened := p.collectRun()
			if opened {
				r := &Rule{Block: true}
				r.Prel = trimTokens(run)
				if len(r.Prel) > 0 {
					r.Pos = tokPos(r.Prel[0])
					r.Text = strings.TrimSpace(p.src[r.Prel[0].Start:r.Prel[len(r.Prel)-1].End])
					if r.Prel[0].Type == TokenAtKeyword {
						r.At = true
						r.Name = strings.ToLower(strings.TrimPrefix(r.Prel[0].Text, "@"))
					}
				}
				r.Doc = doc.take()
				r.Decls, r.Nested = p.parseBlock()
				nested = append(nested, r)
				sawNested = true
				continue
			}
			for _, d := range p.declFromRun(run) {
				if sawNested {
					p.errorf(d.Pos, "declaration after a nested rule; move it above the first nested rule")
				}
				decls = append(decls, d)
			}
			doc.take()
		}
	}
}

func (p *parser) collectRun() (run []Token, opened bool) {
	depth := 0
	for {
		t := p.peek()
		if t.Type == 0 {
			p.errorf(lastRunPos(run), "unexpected end of stylesheet inside a declaration")
			return run, false
		}
		if depth == 0 {
			switch t.Type {
			case TokenSemicolon:
				p.next()
				return run, false
			case TokenOpenCurly:
				p.next()
				return run, true
			case TokenCloseCurly:
				return run, false
			case TokenBadString:
				p.errorf(tokPos(t), "unterminated string: %s", t.Text)
			case TokenBadURL:
				p.errorf(tokPos(t), "invalid url(): %s", t.Text)
			case TokenString:
				if unterminatedString(t) {
					p.errorf(tokPos(t), "unterminated string: %s", t.Text)
				}
			}
		}
		switch t.Type {
		case TokenFunction, TokenOpenParen, TokenOpenSquare, TokenOpenCurly:
			depth++
		case TokenCloseParen, TokenCloseSquare, TokenCloseCurly:
			depth--
		}
		run = append(run, t)
		p.next()
	}
}

func lastRunPos(run []Token) Pos {
	if len(run) == 0 {
		return Pos{1, 1}
	}
	last := run[len(run)-1]
	return Pos{Line: last.Line, Col: last.Col + 1}
}

func (p *parser) declFromRun(run []Token) []*Decl {
	run = trimTokens(run)
	if len(run) == 0 {
		return nil
	}
	name := run[0]
	if name.Type != TokenIdent {
		p.errorf(tokPos(name), "expected a property name before ':', found %s %q", name.Type, name.Text)
		return nil
	}
	colon := 1
	for colon < len(run) && isTrivia(run[colon]) {
		colon++
	}
	if colon == len(run) || run[colon].Type != TokenColon {
		p.errorf(tokPos(name), "declaration %q has no ':'", name.Text)
		return nil
	}
	d := &Decl{Name: name.Text, Pos: tokPos(name)}
	value := trimTokens(run[colon+1:])
	if len(value) >= 2 {
		last := value[len(value)-1]
		if last.Type == TokenIdent && strings.EqualFold(last.Text, "important") {
			prefix := trimTokens(value[:len(value)-1])
			prev := prefix[len(prefix)-1]
			if prev.Type == TokenDelim && prev.Text == "!" {
				d.Important = true
				d.ImpPos = tokPos(prev)
				value = trimTokens(prefix[:len(prefix)-1])
			}
		}
	}
	d.Value = value
	if len(value) > 0 {
		d.ValuePos = tokPos(value[0])
		d.ValueSrc = strings.TrimSpace(p.src[value[0].Start:value[len(value)-1].End])
	}
	return []*Decl{d}
}

func unterminatedString(t Token) bool {
	if t.Type != TokenString || len(t.Text) < 1 {
		return false
	}
	last := t.Text[len(t.Text)-1]
	return last != '"' && last != '\''
}
