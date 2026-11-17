package dsl

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// ParsePredicate parses filter text into a predicate that has passed
// filter.ValidatePredicate against fields. It builds no SQL and applies
// no row scope. Empty text returns (nil, nil). Keywords are
// case-insensitive:
//
//	expr     := and_expr ("or" and_expr)*
//	and_expr := term ("and" term)*
//	term     := "(" expr ")" | field op value
//	op       := "=" | "!=" | "<" | ">" | "<=" | ">=" | "contains" | "in"
//	value    := quoted string ("\"" and "\\" escapes) | number | true | false
//	in value := "[" value ("," value)* "]"
func ParsePredicate(text string, fields []schema.Field) (*filter.Predicate, error) {
	if len(text) > maxDSLInputSize {
		return nil, i18n.NewError(reasonFilterTooLong, map[string]any{"max": maxDSLInputSize})
	}
	src := strings.TrimSpace(text)
	if src == "" {
		return nil, nil
	}
	p := &predParser{src: src}
	root, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos < len(p.src) {
		return nil, p.errorf(reasonUnexpectedInput)
	}

	return filter.ValidatePredicate(root, fields)
}

// Quote writes v as a quoted value ParsePredicate reads back as v.
func Quote(v string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`) + `"`
}

const maxSortTerms = 16

// ParseSort parses Sort text such as "amount DESC, number ASC" into
// sorts checked against fields; a direction defaults to ASC. Empty text
// returns (nil, nil).
func ParseSort(text string, fields []schema.Field) ([]filter.ParsedSort, error) {
	if len(text) > maxDSLInputSize {
		return nil, i18n.NewError(reasonSortTooLong, map[string]any{"max": maxDSLInputSize})
	}
	src := strings.TrimSpace(text)
	if src == "" {
		return nil, nil
	}
	parts := strings.Split(src, ",")
	if len(parts) > maxSortTerms {
		return nil, i18n.NewError(reasonSortTooMany, map[string]any{"count": len(parts), "max": maxSortTerms})
	}

	allowed := make(map[string]bool, len(fields))
	var noQuery map[string]bool
	alias := make(map[string]string, len(fields))
	for _, f := range fields {
		if f.Hidden {
			continue
		}
		if f.NoQuery {
			if noQuery == nil {
				noQuery = make(map[string]bool, len(fields))
			}
			noQuery[f.Name] = true
			if f.WireName != "" && f.WireName != f.Name {
				noQuery[f.WireName] = true
			}
			continue
		}
		allowed[f.Name] = true
		if f.WireName != "" && f.WireName != f.Name {
			allowed[f.WireName] = true
			alias[f.WireName] = f.Name
		}
	}

	sorts := make([]filter.ParsedSort, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, i18n.NewError(reasonSortEmptyKey)
		}
		toks := strings.Fields(part)
		if len(toks) > 2 {
			return nil, i18n.NewError(reasonSortBadTerm, map[string]any{"term": strconv.Quote(textsafe.SanitizeControlBytes(part))})
		}
		name := toks[0]
		if !dslIdentRe.MatchString(name) {
			return nil, i18n.NewError(reasonSortBadFieldName, map[string]any{"name": strconv.Quote(textsafe.SanitizeControlBytes(name))})
		}
		desc := false
		if len(toks) == 2 {
			switch asciicase.Lower(toks[1]) {
			case "asc":
			case "desc":
				desc = true
			default:
				return nil, i18n.NewError(reasonSortBadDirection, map[string]any{"dir": strconv.Quote(textsafe.SanitizeControlBytes(toks[1]))})
			}
		}
		if noQuery[name] {
			return nil, i18n.NewError(reasonNotSortable, map[string]any{"field": strconv.Quote(name)})
		}
		if !allowed[name] {
			return nil, i18n.NewError(reasonInvalidSortField, map[string]any{"field": strconv.Quote(name)})
		}
		if col, ok := alias[name]; ok {
			name = col
		}
		sorts = append(sorts, filter.ParsedSort{Field: name, Desc: desc})
	}
	return sorts, nil
}

type predParser struct {
	src string
	pos int
}

func (p *predParser) skipSpace() {
	for p.pos < len(p.src) && isPredSpace(p.src[p.pos]) {
		p.pos++
	}
}

func isPredSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func (p *predParser) errorf(reason i18n.Phrase) error {
	lo := p.pos - 16
	if lo < 0 {
		lo = 0
	}
	hi := p.pos + 16
	if hi > len(p.src) {
		hi = len(p.src)
	}
	near := textsafe.SanitizeControlBytes(p.src[lo:hi])
	if near == "" {
		near = "start of input"
	}
	return i18n.NewError(reason, map[string]any{"offset": p.pos, "near": strconv.Quote(near)})
}

func (p *predParser) parseExpr() (*filter.Predicate, error) {
	return p.parseChain("or", true, p.parseAnd)
}

func (p *predParser) parseAnd() (*filter.Predicate, error) {
	return p.parseChain("and", false, p.parseTerm)
}

func (p *predParser) parseChain(kw string, or bool, operand func() (*filter.Predicate, error)) (*filter.Predicate, error) {
	first, err := operand()
	if err != nil {
		return nil, err
	}
	var children []filter.Predicate
	for {
		p.skipSpace()
		if !p.peekKeyword(kw) {
			break
		}
		p.consumeKeyword(kw)
		next, err := operand()
		if err != nil {
			return nil, err
		}
		if children == nil {
			children = []filter.Predicate{*first}
		}
		children = append(children, *next)
	}
	if children == nil {
		return first, nil
	}
	return &filter.Predicate{Or: or, Children: children}, nil
}

func (p *predParser) parseTerm() (*filter.Predicate, error) {
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == '(' {
		p.pos++
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.pos >= len(p.src) || p.src[p.pos] != ')' {
			return nil, p.errorf(reasonExpectedParen)
		}
		p.pos++
		return inner, nil
	}
	field, err := p.parseIdent(reasonExpectedFieldName)
	if err != nil {
		return nil, err
	}
	op, err := p.parseOp()
	if err != nil {
		return nil, err
	}
	if op == filter.OpIn {
		values, err := p.parseValueList()
		if err != nil {
			return nil, err
		}
		return &filter.Predicate{Field: field, Op: filter.OpIn, Values: values}, nil
	}
	value, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	return &filter.Predicate{Field: field, Op: op, Value: value}, nil
}

func (p *predParser) parseIdent(missing i18n.Phrase) (string, error) {
	p.skipSpace()
	start := p.pos
	if p.pos < len(p.src) && (isAlpha(p.src[p.pos]) || p.src[p.pos] == '_') {
		p.pos++
		for p.pos < len(p.src) && (isAlnum(p.src[p.pos]) || p.src[p.pos] == '_') {
			p.pos++
		}
	}
	if p.pos == start {
		return "", p.errorf(missing)
	}
	return p.src[start:p.pos], nil
}

func isAlpha(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }
func isAlnum(b byte) bool { return isAlpha(b) || b >= '0' && b <= '9' }

func (p *predParser) parseOp() (filter.FilterOp, error) {
	p.skipSpace()
	rest := p.src[p.pos:]
	for _, s := range [...]struct {
		tok string
		op  filter.FilterOp
	}{
		{"!=", filter.OpNe},
		{"<=", filter.OpLte},
		{">=", filter.OpGte},
		{"=", filter.OpEq},
		{"<", filter.OpLt},
		{">", filter.OpGt},
	} {
		if strings.HasPrefix(rest, s.tok) {
			p.pos += len(s.tok)
			return s.op, nil
		}
	}
	for _, kw := range [...]struct {
		tok string
		op  filter.FilterOp
	}{
		{"contains", filter.OpLike},
		{"in", filter.OpIn},
	} {
		if p.peekKeyword(kw.tok) {
			p.consumeKeyword(kw.tok)
			return kw.op, nil
		}
	}
	return "", p.errorf(reasonExpectedOperator)
}

func (p *predParser) peekKeyword(kw string) bool {
	rest := p.src[p.pos:]
	if len(rest) < len(kw) || !strings.EqualFold(rest[:len(kw)], kw) {
		return false
	}
	if len(rest) == len(kw) {
		return true
	}
	next := rest[len(kw)]
	return isPredSpace(next) || next == '(' || next == ')' || next == '[' || next == ']' || next == ','
}

func (p *predParser) consumeKeyword(kw string) {
	p.pos += len(kw)
}

func (p *predParser) parseValue() (string, error) {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return "", p.errorf(reasonExpectedValue)
	}
	switch b := p.src[p.pos]; {
	case b == '"':
		return p.parseQuoted()
	case b == '-' || b == '+' || b >= '0' && b <= '9':
		return p.parseNumber()
	case isAlpha(b) || b == '_':
		start := p.pos
		tok, err := p.parseIdent(reasonExpectedValueWord)
		if err != nil {
			return "", err
		}
		switch asciicase.Lower(tok) {
		case "true":
			return "true", nil
		case "false":
			return "false", nil
		}
		p.pos = start
		return "", p.errorf(reasonBareWord)
	default:
		return "", p.errorf(reasonExpectedValue)
	}
}

func (p *predParser) parseQuoted() (string, error) {
	p.pos++
	var sb strings.Builder
	for p.pos < len(p.src) {
		b := p.src[p.pos]
		switch b {
		case '"':
			p.pos++
			return sb.String(), nil
		case '\\':
			if p.pos+1 >= len(p.src) {
				return "", p.errorf(reasonUnterminatedEscape)
			}
			switch p.src[p.pos+1] {
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			default:
				return "", p.errorf(reasonBadEscape)
			}
			p.pos += 2
		case '\n', '\r':
			return "", p.errorf(reasonUnterminatedString)
		default:
			sb.WriteByte(b)
			p.pos++
		}
	}
	return "", p.errorf(reasonUnterminatedString)
}

func (p *predParser) parseNumber() (string, error) {
	start := p.pos
	if p.src[p.pos] == '-' || p.src[p.pos] == '+' {
		p.pos++
	}
	digits := func() bool {
		n := 0
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
			n++
		}
		return n > 0
	}
	if !digits() {
		p.pos = start
		return "", p.errorf(reasonExpectedNumber)
	}
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		p.pos++
		if !digits() {
			p.pos = start
			return "", p.errorf(reasonExpectedFraction)
		}
	}
	if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
		mark := p.pos
		p.pos++
		if p.pos < len(p.src) && (p.src[p.pos] == '-' || p.src[p.pos] == '+') {
			p.pos++
		}
		if !digits() {
			p.pos = mark
			return "", p.errorf(reasonExpectedExponent)
		}
	}
	return p.src[start:p.pos], nil
}

func (p *predParser) parseValueList() ([]string, error) {
	p.skipSpace()
	if p.pos >= len(p.src) || p.src[p.pos] != '[' {
		return nil, p.errorf(reasonInNeedsList)
	}
	p.pos++
	var values []string
	for {
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		values = append(values, v)
		p.skipSpace()
		if p.pos >= len(p.src) {
			return nil, p.errorf(reasonUnterminatedList)
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return values, nil
		default:
			return nil, p.errorf(reasonExpectedListSep)
		}
	}
}
