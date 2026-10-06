package dsl

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// ParsePredicate parses the filter text the admin's filter chips, query
// box, a list view's Where and the ?filter= URL parameter carry, into a
// [filter.Predicate] that has already passed [filter.ValidatePredicate]
// against fields. It never builds SQL itself and never touches
// BuildDSLQuery, which applies no row scope; hand the predicate to a
// caller that does (the CRUD handler's in-process list), where owner,
// tenant, soft-delete and read scopes are ANDed around it.
//
// Grammar (keywords are case-insensitive):
//
//	expr     := and_expr ("or" and_expr)*
//	and_expr := term ("and" term)*
//	term     := "(" expr ")" | field op value
//	op       := "=" | "!=" | "<" | ">" | "<=" | ">=" | "contains" | "in"
//	value    := quoted string ("\"" and "\\" escapes) | number | true | false
//	in value := "[" value ("," value)* "]"
//
// `contains` maps to OpLike with the value wrapped for a literal substring
// match: the `%`, `_` and escape characters in the user's text are escaped
// and the clause carries an ESCAPE, so `contains "50%"` matches a literal
// 50%, not a wildcard pattern. `field` must be a bare identifier; whether
// it names a real, non-Hidden, non-NoQuery column of the right type is
// ValidatePredicate's question, answered before the tree is returned.
//
// Empty or all-whitespace text returns (nil, nil). Input over
// maxDSLInputSize bytes is refused outright. Errors name the byte offset
// and quote at most a short, control-byte-scrubbed excerpt of the input.
func ParsePredicate(text string, fields []schema.Field) (*filter.Predicate, error) {
	if len(text) > maxDSLInputSize {
		return nil, fmt.Errorf("filter: input exceeds %d bytes", maxDSLInputSize)
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
		return nil, p.errorf("unexpected input after expression")
	}
	if err := filter.ValidatePredicate(root, fields); err != nil {
		return nil, err
	}
	return root, nil
}

// maxSortTerms bounds how many keys one Sort string may carry. Mirrors
// filter's maxSortFields cap on ?sort= so a hostile ?sort= cannot inflate
// the ORDER BY through this surface either.
const maxSortTerms = 16

// ParseSort parses a view's Sort text — `due_on ASC`,
// `amount DESC, number ASC` — into [filter.ParsedSort] values. Field names
// are validated against fields: unknown and Hidden names are refused
// (indistinguishably, matching ParseSortValues), NoQuery names are refused
// by name, and a direction must be ASC or DESC, case-insensitive, defaulting
// to ASC when omitted. Empty or all-whitespace text returns (nil, nil).
func ParseSort(text string, fields []schema.Field) ([]filter.ParsedSort, error) {
	if len(text) > maxDSLInputSize {
		return nil, fmt.Errorf("sort: input exceeds %d bytes", maxDSLInputSize)
	}
	src := strings.TrimSpace(text)
	if src == "" {
		return nil, nil
	}
	parts := strings.Split(src, ",")
	if len(parts) > maxSortTerms {
		return nil, fmt.Errorf("sort: too many sort keys: %d (max %d)", len(parts), maxSortTerms)
	}

	// The allow-list mirrors ParseSortValues: Hidden folds into unknown,
	// NoQuery is refused by name, wire aliases resolve to columns.
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
			return nil, fmt.Errorf("sort: empty sort key")
		}
		toks := strings.Fields(part)
		if len(toks) > 2 {
			return nil, fmt.Errorf("sort: %q is not `field [ASC|DESC]`", textsafe.SanitizeControlBytes(part))
		}
		name := toks[0]
		if !dslIdentRe.MatchString(name) {
			return nil, fmt.Errorf("sort: invalid field name at %q", textsafe.SanitizeControlBytes(name))
		}
		desc := false
		if len(toks) == 2 {
			switch strings.ToUpper(toks[1]) {
			case "ASC":
			case "DESC":
				desc = true
			default:
				return nil, fmt.Errorf("sort: invalid direction %q (want ASC or DESC)", textsafe.SanitizeControlBytes(toks[1]))
			}
		}
		if noQuery[name] {
			return nil, fmt.Errorf("field %q cannot be sorted on", name)
		}
		if !allowed[name] {
			return nil, fmt.Errorf("invalid sort field %q", name)
		}
		if col, ok := alias[name]; ok {
			name = col
		}
		sorts = append(sorts, filter.ParsedSort{Field: name, Desc: desc})
	}
	return sorts, nil
}

// predParser is a recursive-descent parser over one filter string. It
// walks bytes; every method leaves pos on the first unconsumed byte.
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

// errorf builds an error naming the current offset and a short, scrubbed
// excerpt around it. Control bytes are stripped and the quote is capped so
// a hostile ?filter= cannot forge log lines or dump its payload through
// the error path.
func (p *predParser) errorf(msg string) error {
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
	return fmt.Errorf("filter: offset %d: %s (near %q)", p.pos, msg, near)
}

// parseExpr implements expr := and_expr ("or" and_expr)*. A chain of ors
// becomes ONE group with a child per operand, never a left-nested pair per
// or: nesting would spend a level of the predicate depth cap on every
// term, so a plain ten-term filter would be refused as too deep.
func (p *predParser) parseExpr() (*filter.Predicate, error) {
	return p.parseChain("or", true, p.parseAnd)
}

// parseAnd implements and_expr := term ("and" term)*, flattened the same
// way parseExpr flattens ors.
func (p *predParser) parseAnd() (*filter.Predicate, error) {
	return p.parseChain("and", false, p.parseTerm)
}

// parseChain reads operand (kw operand)* and returns the lone operand, or
// one group holding every operand when the keyword appeared.
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

// parseTerm implements term := "(" expr ")" | field op value. Parentheses
// group; they do not create a node of their own.
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
			return nil, p.errorf("expected )")
		}
		p.pos++
		return inner, nil
	}
	field, err := p.parseIdent("field name")
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

// parseIdent scans one [A-Za-z_][A-Za-z0-9_]* token, the same allow-list
// dslIdentRe spells for every other DSL identifier.
func (p *predParser) parseIdent(what string) (string, error) {
	p.skipSpace()
	start := p.pos
	if p.pos < len(p.src) && (isAlpha(p.src[p.pos]) || p.src[p.pos] == '_') {
		p.pos++
		for p.pos < len(p.src) && (isAlnum(p.src[p.pos]) || p.src[p.pos] == '_') {
			p.pos++
		}
	}
	if p.pos == start {
		return "", p.errorf("expected " + what)
	}
	return p.src[start:p.pos], nil
}

func isAlpha(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }
func isAlnum(b byte) bool { return isAlpha(b) || b >= '0' && b <= '9' }

// parseOp scans a symbolic or keyword operator. Two-byte symbols are
// matched before their one-byte prefixes.
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
	return "", p.errorf("expected an operator (=, !=, <, >, <=, >=, contains, in)")
}

// peekKeyword reports whether a case-insensitive keyword starts at pos,
// as a whole word (followed by a delimiter, so `orb` is not `or`).
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

// parseValue scans one scalar: a double-quoted string with \" and \\
// escapes, a bare number, or true/false (case-insensitive, normalized to
// lower case so Bool columns bind a Go bool).
func (p *predParser) parseValue() (string, error) {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return "", p.errorf("expected a value")
	}
	switch b := p.src[p.pos]; {
	case b == '"':
		return p.parseQuoted()
	case b == '-' || b == '+' || b >= '0' && b <= '9':
		return p.parseNumber()
	case isAlpha(b) || b == '_':
		start := p.pos
		tok, err := p.parseIdent("value")
		if err != nil {
			return "", err
		}
		switch strings.ToLower(tok) {
		case "true":
			return "true", nil
		case "false":
			return "false", nil
		}
		p.pos = start
		return "", p.errorf("bare words must be quoted; only numbers, true and false may go unquoted")
	default:
		return "", p.errorf("expected a value")
	}
}

// parseQuoted scans a double-quoted string. Only \" and \\ are escapes;
// any other backslash is refused rather than silently reinterpreted, so
// what matched is what the user typed.
func (p *predParser) parseQuoted() (string, error) {
	p.pos++ // opening quote
	var sb strings.Builder
	for p.pos < len(p.src) {
		b := p.src[p.pos]
		switch b {
		case '"':
			p.pos++
			return sb.String(), nil
		case '\\':
			if p.pos+1 >= len(p.src) {
				return "", p.errorf("unterminated escape in string")
			}
			switch p.src[p.pos+1] {
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			default:
				return "", p.errorf(`only \" and \\ may be escaped`)
			}
			p.pos += 2
		case '\n', '\r':
			return "", p.errorf("unterminated string")
		default:
			sb.WriteByte(b)
			p.pos++
		}
	}
	return "", p.errorf("unterminated string")
}

// parseNumber scans an optionally signed decimal with optional fraction
// and exponent. The token's text is kept verbatim; the SQL binder owns
// numeric coercion, as it does for ?field_gt=.
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
		return "", p.errorf("expected a number")
	}
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		p.pos++
		if !digits() {
			p.pos = start
			return "", p.errorf("expected digits after the decimal point")
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
			return "", p.errorf("expected digits in the exponent")
		}
	}
	return p.src[start:p.pos], nil
}

// parseValueList scans ["a", "b", 3] for the in operator: at least one
// value, values separated by commas, the whole list in brackets.
func (p *predParser) parseValueList() ([]string, error) {
	p.skipSpace()
	if p.pos >= len(p.src) || p.src[p.pos] != '[' {
		return nil, p.errorf("in requires a bracketed list like [\"a\", \"b\"]")
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
			return nil, p.errorf("unterminated list, expected , or ]")
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return values, nil
		default:
			return nil, p.errorf("expected , or ] in list")
		}
	}
}
