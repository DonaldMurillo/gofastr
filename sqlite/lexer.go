package sqlite

import (
	"strings"
)

// Lexer tokenizes a SQL input string.
type Lexer struct {
	input  string
	pos    int
	line   int
	col    int
	errors []string
}

// NewLexer creates a new SQL lexer for the given input.
func NewLexer(input string) *Lexer {
	return &Lexer{
		input: input,
		pos:   0,
		line:  1,
		col:   1,
	}
}

// Tokenize returns all tokens from the input.
func (l *Lexer) Tokenize() ([]Token, error) {
	var tokens []Token
	for {
		tok := l.Next()
		if tok.Type == TokenEOF {
			tokens = append(tokens, tok)
			break
		}
		if tok.Type == TokenError {
			return tokens, &lexerError{tok.Value, tok.Line, tok.Col}
		}
		tokens = append(tokens, tok)
	}
	return tokens, nil
}

// Next returns the next token from the input.
func (l *Lexer) Next() Token {
	l.skipWhitespace()

	if l.pos >= len(l.input) {
		return Token{Type: TokenEOF, Pos: l.pos, Line: l.line, Col: l.col}
	}

	ch := l.input[l.pos]

	switch {
	// Single-line comment --
	case ch == '-' && l.peek(1) == '-':
		l.skipLineComment()
		return l.Next()

	// Block comment /*
	case ch == '/' && l.peek(1) == '*':
		l.skipBlockComment()
		return l.Next()

	// Numbers
	case isDigit(ch) || (ch == '.' && isDigit(l.peek(1))):
		return l.readNumber()

	// Strings
	case ch == '\'':
		return l.readString()

	// Blob literals
	case (ch == 'X' || ch == 'x') && l.peek(1) == '\'':
		return l.readBlob()

	// Quoted identifiers
	case ch == '"':
		return l.readQuotedIdentifier()

	// Backtick-quoted identifiers (MySQL compat)
	case ch == '`':
		return l.readBacktickIdentifier()

	// Square bracket identifiers (SQL Server compat)
	case ch == '[':
		return l.readBracketIdentifier()

	// Identifiers and keywords
	case isIdentStart(ch):
		return l.readIdentifier()

	// Operators and punctuation
	default:
		return l.readOperator()
	}
}

// peek returns the character at offset+n, or 0 if out of bounds.
func (l *Lexer) peek(n int) byte {
	idx := l.pos + n
	if idx >= len(l.input) {
		return 0
	}
	return l.input[idx]
}

// advance moves forward one character, updating line/col.
func (l *Lexer) advance() {
	if l.pos < len(l.input) {
		if l.input[l.pos] == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}
		l.pos++
	}
}

// advanceN moves forward n characters.
func (l *Lexer) advanceN(n int) {
	for i := 0; i < n; i++ {
		l.advance()
	}
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) && isWhitespace(l.input[l.pos]) {
		l.advance()
	}
}

func (l *Lexer) skipLineComment() {
	for l.pos < len(l.input) && l.input[l.pos] != '\n' {
		l.advance()
	}
}

func (l *Lexer) skipBlockComment() {
	l.advanceN(2) // skip /*
	depth := 1
	for l.pos < len(l.input) && depth > 0 {
		if l.input[l.pos] == '/' && l.peek(1) == '*' {
			depth++
			l.advanceN(2)
		} else if l.input[l.pos] == '*' && l.peek(1) == '/' {
			depth--
			l.advanceN(2)
		} else {
			l.advance()
		}
	}
}

func (l *Lexer) readNumber() Token {
	pos, line, col := l.pos, l.line, l.col
	start := l.pos

	hasDot := false
	hasExp := false

	// Hex literals: 0x...
	if l.input[l.pos] == '0' && (l.peek(1) == 'x' || l.peek(1) == 'X') {
		l.advanceN(2) // skip 0x
		for l.pos < len(l.input) && isHexDigit(l.input[l.pos]) {
			l.advance()
		}
		return Token{
			Type:  TokenInteger,
			Value: l.input[start:l.pos],
			Pos:   pos,
			Line:  line,
			Col:   col,
		}
	}

	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == '.' && !hasDot && !hasExp {
			hasDot = true
			l.advance()
		} else if (ch == 'e' || ch == 'E') && !hasExp {
			hasExp = true
			hasDot = true // treat as float
			l.advance()
			if l.pos < len(l.input) && (l.input[l.pos] == '+' || l.input[l.pos] == '-') {
				l.advance()
			}
		} else if isDigit(ch) {
			l.advance()
		} else {
			break
		}
	}

	value := l.input[start:l.pos]
	tt := TokenInteger
	if hasDot || hasExp {
		tt = TokenFloat
	}

	return Token{
		Type:  tt,
		Value: value,
		Pos:   pos,
		Line:  line,
		Col:   col,
	}
}

func (l *Lexer) readString() Token {
	pos, line, col := l.pos, l.line, l.col
	l.advance() // skip opening quote

	var buf strings.Builder
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == '\'' {
			l.advance()
			if l.pos < len(l.input) && l.input[l.pos] == '\'' {
				// Escaped quote ''
				buf.WriteByte('\'')
				l.advance()
			} else {
				// End of string
				return Token{
					Type:  TokenString,
					Value: buf.String(),
					Pos:   pos,
					Line:  line,
					Col:   col,
				}
			}
		} else {
			buf.WriteByte(ch)
			l.advance()
		}
	}

	return Token{
		Type:  TokenError,
		Value: "unterminated string literal",
		Pos:   pos,
		Line:  line,
		Col:   col,
	}
}

func (l *Lexer) readBlob() Token {
	pos, line, col := l.pos, l.line, l.col
	l.advanceN(2) // skip X'

	var buf strings.Builder
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == '\'' {
			l.advance()
			return Token{
				Type:  TokenBlob,
				Value: "X'" + buf.String() + "'",
				Pos:   pos,
				Line:  line,
				Col:   col,
			}
		}
		if !isHexDigit(ch) {
			return Token{
				Type:  TokenError,
				Value: "invalid character in blob literal",
				Pos:   l.pos,
				Line:  l.line,
				Col:   l.col,
			}
		}
		buf.WriteByte(ch)
		l.advance()
	}

	return Token{
		Type:  TokenError,
		Value: "unterminated blob literal",
		Pos:   pos,
		Line:  line,
		Col:   col,
	}
}

func (l *Lexer) readQuotedIdentifier() Token {
	pos, line, col := l.pos, l.line, l.col
	l.advance() // skip opening "

	var buf strings.Builder
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == '"' {
			l.advance()
			if l.pos < len(l.input) && l.input[l.pos] == '"' {
				buf.WriteByte('"')
				l.advance()
			} else {
				return Token{
					Type:  TokenQuotedID,
					Value: buf.String(),
					Pos:   pos,
					Line:  line,
					Col:   col,
				}
			}
		} else {
			buf.WriteByte(ch)
			l.advance()
		}
	}

	return Token{
		Type:  TokenError,
		Value: "unterminated quoted identifier",
		Pos:   pos,
		Line:  line,
		Col:   col,
	}
}

func (l *Lexer) readBacktickIdentifier() Token {
	pos, line, col := l.pos, l.line, l.col
	l.advance() // skip opening `

	var buf strings.Builder
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == '`' {
			l.advance()
			return Token{
				Type:  TokenIdent,
				Value: buf.String(),
				Pos:   pos,
				Line:  line,
				Col:   col,
			}
		}
		buf.WriteByte(ch)
		l.advance()
	}

	return Token{
		Type:  TokenError,
		Value: "unterminated backtick identifier",
		Pos:   pos,
		Line:  line,
		Col:   col,
	}
}

func (l *Lexer) readBracketIdentifier() Token {
	pos, line, col := l.pos, l.line, l.col
	l.advance() // skip opening [

	var buf strings.Builder
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == ']' {
			l.advance()
			return Token{
				Type:  TokenIdent,
				Value: buf.String(),
				Pos:   pos,
				Line:  line,
				Col:   col,
			}
		}
		buf.WriteByte(ch)
		l.advance()
	}

	return Token{
		Type:  TokenError,
		Value: "unterminated bracket identifier",
		Pos:   pos,
		Line:  line,
		Col:   col,
	}
}

func (l *Lexer) readIdentifier() Token {
	pos, line, col := l.pos, l.line, l.col
	start := l.pos

	for l.pos < len(l.input) && isIdentPart(l.input[l.pos]) {
		l.advance()
	}

	value := l.input[start:l.pos]

	// Check if it's a keyword
	upper := strings.ToUpper(value)
	if tt, ok := KeywordMap[upper]; ok {
		return Token{
			Type:  tt,
			Value: upper,
			Pos:   pos,
			Line:  line,
			Col:   col,
		}
	}

	return Token{
		Type:  TokenIdent,
		Value: value,
		Pos:   pos,
		Line:  line,
		Col:   col,
	}
}

func (l *Lexer) readOperator() Token {
	pos, line, col := l.pos, l.line, l.col
	ch := l.input[l.pos]

	switch ch {
	case '(':
		l.advance()
		return Token{Type: TokenLParen, Value: "(", Pos: pos, Line: line, Col: col}
	case ')':
		l.advance()
		return Token{Type: TokenRParen, Value: ")", Pos: pos, Line: line, Col: col}
	case ',':
		l.advance()
		return Token{Type: TokenComma, Value: ",", Pos: pos, Line: line, Col: col}
	case '.':
		l.advance()
		return Token{Type: TokenDot, Value: ".", Pos: pos, Line: line, Col: col}
	case ';':
		l.advance()
		return Token{Type: TokenSemicolon, Value: ";", Pos: pos, Line: line, Col: col}
	case '+':
		l.advance()
		return Token{Type: TokenPlus, Value: "+", Pos: pos, Line: line, Col: col}
	case '%':
		l.advance()
		return Token{Type: TokenPercent, Value: "%", Pos: pos, Line: line, Col: col}
	case '~':
		l.advance()
		return Token{Type: TokenTilde, Value: "~", Pos: pos, Line: line, Col: col}
	case '$':
		return l.readDollarParameter()
	case '?':
		l.advance()
		return Token{Type: TokenQuestion, Value: "?", Pos: pos, Line: line, Col: col}
	case '*':
		l.advance()
		return Token{Type: TokenStar, Value: "*", Pos: pos, Line: line, Col: col}
	case '/':
		l.advance()
		return Token{Type: TokenSlash, Value: "/", Pos: pos, Line: line, Col: col}
	case '-':
		l.advance()
		return Token{Type: TokenMinus, Value: "-", Pos: pos, Line: line, Col: col}
	case '=':
		l.advance()
		if l.pos < len(l.input) && l.input[l.pos] == '=' {
			l.advance()
			return Token{Type: TokenEqual, Value: "==", Pos: pos, Line: line, Col: col}
		}
		return Token{Type: TokenEqual, Value: "=", Pos: pos, Line: line, Col: col}
	case '<':
		l.advance()
		if l.pos < len(l.input) {
			switch l.input[l.pos] {
			case '=':
				l.advance()
				return Token{Type: TokenLessEq, Value: "<=", Pos: pos, Line: line, Col: col}
			case '>':
				l.advance()
				return Token{Type: TokenNotEqual, Value: "<>", Pos: pos, Line: line, Col: col}
			case '<':
				l.advance()
				// << shift left
				return Token{Type: TokenLess, Value: "<<", Pos: pos, Line: line, Col: col}
			}
		}
		return Token{Type: TokenLess, Value: "<", Pos: pos, Line: line, Col: col}
	case '>':
		l.advance()
		if l.pos < len(l.input) {
			switch l.input[l.pos] {
			case '=':
				l.advance()
				return Token{Type: TokenGreaterEq, Value: ">=", Pos: pos, Line: line, Col: col}
			case '>':
				l.advance()
				return Token{Type: TokenGreater, Value: ">>", Pos: pos, Line: line, Col: col}
			}
		}
		return Token{Type: TokenGreater, Value: ">", Pos: pos, Line: line, Col: col}
	case '!':
		l.advance()
		if l.pos < len(l.input) && l.input[l.pos] == '=' {
			l.advance()
			return Token{Type: TokenNotEqual, Value: "!=", Pos: pos, Line: line, Col: col}
		}
		return Token{Type: TokenError, Value: "unexpected '!'", Pos: pos, Line: line, Col: col}
	case '|':
		l.advance()
		if l.pos < len(l.input) && l.input[l.pos] == '|' {
			l.advance()
			return Token{Type: TokenConcat, Value: "||", Pos: pos, Line: line, Col: col}
		}
		return Token{Type: TokenError, Value: "unexpected '|'", Pos: pos, Line: line, Col: col}
	case '&':
		l.advance()
		return Token{Type: TokenPlus, Value: "&", Pos: pos, Line: line, Col: col}
	default:
		l.advance()
		return Token{
			Type:  TokenError,
			Value: "unexpected character: " + string(ch),
			Pos:   pos,
			Line:  line,
			Col:   col,
		}
	}
}

// Character classification helpers

func isWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isHexDigit(ch byte) bool {
	return isDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentPart(ch byte) bool {
	return isIdentStart(ch) || isDigit(ch)
}

// Lexer error type
type lexerError struct {
	msg  string
	line int
	col  int
}

func (e *lexerError) Error() string {
	return formatInt64(int64(e.line)) + ":" + formatInt64(int64(e.col)) + ": " + e.msg
}
