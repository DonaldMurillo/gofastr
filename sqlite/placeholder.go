package sqlite

func (l *Lexer) readDollarParameter() Token {
	pos, line, col := l.pos, l.line, l.col
	start := l.pos
	l.advance()
	if !isDigit(l.peek(0)) {
		return Token{
			Type:  TokenError,
			Value: "expected digits after $",
			Pos:   pos,
			Line:  line,
			Col:   col,
		}
	}
	for l.pos < len(l.input) && isDigit(l.input[l.pos]) {
		l.advance()
	}
	return Token{
		Type:  TokenQuestion,
		Value: l.input[start:l.pos],
		Pos:   pos,
		Line:  line,
		Col:   col,
	}
}
