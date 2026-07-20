package sqlite

import (
	"strconv"
	"strings"
)

// FormatExpr renders an Expr back to SQL source text suitable for
// re-parsing by the engine's own parser. It exists to make non-constant
// column DEFAULT expressions (CURRENT_TIMESTAMP, datetime('now'), etc.)
// and partial-index WHERE predicates survive SaveSchema / LoadSchema.
//
// The renderer covers the AST forms that are legal in those positions:
// literals, column references (including the bare CURRENT_TIMESTAMP /
// CURRENT_DATE / CURRENT_TIME keywords, which the parser produces as
// ColumnRef), function calls, unary and binary expressions, and
// parenthesized expressions. Anything it cannot render returns "" so
// the caller can fall back to dropping the default rather than writing
// a value the parser would later reject.
func FormatExpr(expr Expr) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case LiteralExpr:
		return formatLiteralExpr(e)
	case ColumnRef:
		if e.Table != "" {
			return quoteIdent(e.Table) + "." + quoteIdent(e.Column)
		}
		return quoteIdent(e.Column)
	case FunctionCall:
		return formatFunctionCall(e)
	case ParenExpr:
		inner := FormatExpr(e.Expr)
		if inner == "" {
			return ""
		}
		return "(" + inner + ")"
	case UnaryExpr:
		inner := FormatExpr(e.Expr)
		if inner == "" {
			return ""
		}
		op, ok := formatUnaryOp(e.Op)
		if !ok {
			return ""
		}
		if op == "NOT" {
			return "NOT " + inner
		}
		return op + inner
	case BinaryExpr:
		left := FormatExpr(e.Left)
		right := FormatExpr(e.Right)
		if left == "" || right == "" {
			return ""
		}
		op, ok := formatBinaryOp(e.Op)
		if !ok {
			return ""
		}
		return left + " " + op + " " + right
	default:
		return ""
	}
}

func formatLiteralExpr(l LiteralExpr) string {
	switch l.Type {
	case DataTypeNull:
		return "NULL"
	case DataTypeInteger:
		return strconv.FormatInt(l.IntVal, 10)
	case DataTypeFloat:
		// Match the parser's float literal form so the value round-trips.
		return strconv.FormatFloat(l.FloatVal, 'g', -1, 64)
	case DataTypeText:
		return quoteStringLiteral(l.TextVal)
	case DataTypeBlob:
		var b strings.Builder
		b.WriteString("X'")
		const hex = "0123456789ABCDEF"
		for _, c := range l.BlobVal {
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0F])
		}
		b.WriteByte('\'')
		return b.String()
	default:
		return ""
	}
}

func formatFunctionCall(fc FunctionCall) string {
	var b strings.Builder
	b.WriteString(fc.Name)
	b.WriteByte('(')
	if fc.Star {
		b.WriteByte('*')
	} else {
		if fc.Distinct {
			b.WriteString("DISTINCT ")
		}
		for i, arg := range fc.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			rendered := FormatExpr(arg)
			if rendered == "" {
				return ""
			}
			b.WriteString(rendered)
		}
	}
	b.WriteByte(')')
	return b.String()
}

func formatUnaryOp(op UnaryOp) (string, bool) {
	switch op {
	case OpNegate:
		return "-", true
	case OpBitNot:
		return "~", true
	case OpNot:
		return "NOT", true
	default:
		return "", false
	}
}

func formatBinaryOp(op BinaryOp) (string, bool) {
	switch op {
	case OpAdd:
		return "+", true
	case OpSub:
		return "-", true
	case OpMul:
		return "*", true
	case OpDiv:
		return "/", true
	case OpMod:
		return "%", true
	case OpConcat:
		return "||", true
	case OpEq:
		return "=", true
	case OpNe:
		return "!=", true
	case OpLt:
		return "<", true
	case OpLe:
		return "<=", true
	case OpGt:
		return ">", true
	case OpGe:
		return ">=", true
	case OpAnd:
		return "AND", true
	case OpOr:
		return "OR", true
	case OpBitAnd:
		return "&", true
	case OpBitOr:
		return "|", true
	case OpShiftLeft:
		return "<<", true
	case OpShiftRight:
		return ">>", true
	default:
		return "", false
	}
}

// quoteStringLiteral returns a single-quoted SQL string literal with
// embedded single quotes doubled, matching how the lexer reads them.
func quoteStringLiteral(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		if r == '\'' {
			b.WriteString("''")
			continue
		}
		b.WriteRune(r)
	}
	b.WriteByte('\'')
	return b.String()
}

// quoteIdent renders an identifier. Bare ASCII identifiers that the
// lexer accepts as TokenIdent (letters, digits, underscore, not starting
// with a digit) are written as-is; anything else uses double quotes so
// it survives a re-parse.
func quoteIdent(name string) string {
	if name == "" {
		return "\"\""
	}
	if isBareIdent(name) {
		return name
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range name {
		if r == '"' {
			b.WriteString(`""`)
			continue
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

func isBareIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// ParseExpression parses a single SQL expression (the source form
// produced by FormatExpr) back into an AST. It returns nil, nil for an
// empty input so callers can use it uniformly for "absent".
func ParseExpression(src string) (Expr, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, nil
	}
	p := NewParser(src)
	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	// Tolerate a trailing semicolon so callers can pass through text
	// that originated as part of a statement.
	if p.cur.Type == TokenSemicolon {
		p.advance()
	}
	if p.cur.Type != TokenEOF {
		return nil, &ParseError{Msg: "unexpected trailing input in expression: " + p.cur.Value}
	}
	return expr, nil
}
