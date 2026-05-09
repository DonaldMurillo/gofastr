package expr

import "fmt"

// node is the AST interface. eval is the only contract.
type node interface {
	eval(scope Scope, env *Env) (any, error)
}

type literalNode struct{ value any }
type identNode struct{ name string }
type memberNode struct {
	target node
	field  string
}
type indexNode struct{ target, index node }
type callNode struct {
	name string
	args []node
}
type unaryNode struct {
	op   string
	expr node
}
type binaryNode struct {
	op          string
	left, right node
}
type listNode struct{ items []node }

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() token {
	if p.pos >= len(p.tokens) {
		return token{kind: tokEOF}
	}
	return p.tokens[p.pos]
}

func (p *parser) advance() token {
	t := p.peek()
	p.pos++
	return t
}

func (p *parser) eat(kind tokenKind, value string) bool {
	t := p.peek()
	if t.kind != kind {
		return false
	}
	if value != "" && t.value != value {
		return false
	}
	p.pos++
	return true
}

func (p *parser) expectPunct(v string) error {
	t := p.peek()
	if t.kind != tokPunct || t.value != v {
		return fmt.Errorf("expr: expected %q at %d, got %q", v, t.pos, t.value)
	}
	p.pos++
	return nil
}

// Pratt-style: each parse* function handles a precedence level.

func (p *parser) parseExpr() (node, error) { return p.parseOr() }

func (p *parser) parseOr() (node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokPunct && p.peek().value == "||" {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{op: "||", left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (node, error) {
	left, err := p.parseComp()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokPunct && p.peek().value == "&&" {
		p.advance()
		right, err := p.parseComp()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{op: "&&", left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseComp() (node, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	t := p.peek()
	if t.kind == tokPunct {
		switch t.value {
		case "==", "!=", "<", ">", "<=", ">=":
			p.advance()
			right, err := p.parseAdd()
			if err != nil {
				return nil, err
			}
			return &binaryNode{op: t.value, left: left, right: right}, nil
		}
	}
	return left, nil
}

func (p *parser) parseAdd() (node, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.kind != tokPunct || (t.value != "+" && t.value != "-") {
			return left, nil
		}
		p.advance()
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{op: t.value, left: left, right: right}
	}
}

func (p *parser) parseMul() (node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.kind != tokPunct || (t.value != "*" && t.value != "/" && t.value != "%") {
			return left, nil
		}
		p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{op: t.value, left: left, right: right}
	}
}

func (p *parser) parseUnary() (node, error) {
	t := p.peek()
	if t.kind == tokPunct && (t.value == "!" || t.value == "-" || t.value == "+") {
		p.advance()
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: t.value, expr: inner}, nil
	}
	return p.parsePostfix()
}

func (p *parser) parsePostfix() (node, error) {
	primary, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.kind != tokPunct {
			return primary, nil
		}
		switch t.value {
		case ".":
			p.advance()
			id := p.peek()
			if id.kind != tokIdent {
				return nil, fmt.Errorf("expr: expected identifier after '.' at %d", t.pos)
			}
			p.advance()
			primary = &memberNode{target: primary, field: id.value}
		case "[":
			p.advance()
			idx, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expectPunct("]"); err != nil {
				return nil, err
			}
			primary = &indexNode{target: primary, index: idx}
		case "(":
			// Function call only on a bare ident.
			id, ok := primary.(*identNode)
			if !ok {
				return nil, fmt.Errorf("expr: cannot call non-identifier at %d", t.pos)
			}
			p.advance()
			args, err := p.parseArgList(")")
			if err != nil {
				return nil, err
			}
			primary = &callNode{name: id.name, args: args}
		default:
			return primary, nil
		}
	}
}

func (p *parser) parseArgList(closer string) ([]node, error) {
	var args []node
	if p.peek().kind == tokPunct && p.peek().value == closer {
		p.advance()
		return args, nil
	}
	for {
		arg, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		t := p.peek()
		if t.kind == tokPunct && t.value == "," {
			p.advance()
			continue
		}
		break
	}
	if err := p.expectPunct(closer); err != nil {
		return nil, err
	}
	return args, nil
}

func (p *parser) parsePrimary() (node, error) {
	t := p.peek()
	switch t.kind {
	case tokNumber:
		p.advance()
		v, err := parseNumber(t.value)
		if err != nil {
			return nil, fmt.Errorf("expr: bad number %q: %w", t.value, err)
		}
		return &literalNode{value: v}, nil
	case tokString:
		p.advance()
		return &literalNode{value: t.value}, nil
	case tokIdent:
		switch t.value {
		case "true":
			p.advance()
			return &literalNode{value: true}, nil
		case "false":
			p.advance()
			return &literalNode{value: false}, nil
		case "null":
			p.advance()
			return &literalNode{value: nil}, nil
		}
		p.advance()
		return &identNode{name: t.value}, nil
	case tokPunct:
		switch t.value {
		case "(":
			p.advance()
			inner, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expectPunct(")"); err != nil {
				return nil, err
			}
			return inner, nil
		case "[":
			p.advance()
			items, err := p.parseArgList("]")
			if err != nil {
				return nil, err
			}
			return &listNode{items: items}, nil
		}
	}
	return nil, fmt.Errorf("expr: unexpected token %q at %d", t.value, t.pos)
}
