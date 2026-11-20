// Package ownstyle compiles, checks and registers owned stylesheets.
package ownstyle

import (
	"github.com/DonaldMurillo/gofastr/internal/bom"
	"strings"
	"unicode/utf8"
)

// TokenType names the CSS Syntax Level 3 token classes this tokenizer
// produces.
type TokenType int

const (
	TokenIdent TokenType = iota + 1
	TokenFunction
	TokenAtKeyword
	TokenHash
	TokenString
	TokenBadString
	TokenURL
	TokenBadURL
	TokenDelim
	TokenNumber
	TokenPercentage
	TokenDimension
	TokenWhitespace
	TokenCDO
	TokenCDC
	TokenColon
	TokenSemicolon
	TokenComma
	TokenOpenSquare
	TokenCloseSquare
	TokenOpenParen
	TokenCloseParen
	TokenOpenCurly
	TokenCloseCurly
	TokenComment
)

var tokenNames = map[TokenType]string{
	TokenIdent: "ident", TokenFunction: "function", TokenAtKeyword: "at-keyword",
	TokenHash: "hash", TokenString: "string", TokenBadString: "bad-string",
	TokenURL: "url", TokenBadURL: "bad-url", TokenDelim: "delim",
	TokenNumber: "number", TokenPercentage: "percentage", TokenDimension: "dimension",
	TokenWhitespace: "whitespace", TokenCDO: "CDO", TokenCDC: "CDC",
	TokenColon: "colon", TokenSemicolon: "semicolon", TokenComma: "comma",
	TokenOpenSquare: "[", TokenCloseSquare: "]", TokenOpenParen: "(",
	TokenCloseParen: ")", TokenOpenCurly: "{", TokenCloseCurly: "}", TokenComment: "comment",
}

// String returns the spec's name for the token type ("dimension").
func (t TokenType) String() string {
	if s, ok := tokenNames[t]; ok {
		return s
	}
	return "token"
}

// Token is one CSS token.
type Token struct {
	Type  TokenType
	Text  string
	Line  int
	Col   int
	Start int
	End   int
}

type scanner struct {
	src  string
	pos  int
	line int
	col  int
}

func newScanner(src string) *scanner {
	return &scanner{src: src, line: 1, col: 1}
}

func (s *scanner) peek(i int) byte {
	if s.pos+i >= len(s.src) {
		return 0
	}
	return s.src[s.pos+i]
}

func (s *scanner) peekAt(pos, i int) byte {
	if pos+i >= len(s.src) {
		return 0
	}
	return s.src[pos+i]
}

func (s *scanner) advance() byte {
	c := s.src[s.pos]
	if c < utf8.RuneSelf {
		s.pos++
		switch c {
		case '\r':
			if s.pos < len(s.src) && s.src[s.pos] == '\n' {
				s.pos++
			}
			s.line++
			s.col = 1
		case '\n', '\f':
			s.line++
			s.col = 1
		default:
			s.col++
		}
		return c
	}
	_, size := utf8.DecodeRuneInString(s.src[s.pos:])
	s.pos += size
	s.col++
	return c
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameChar(c byte) bool {
	return isNameStart(c) || c == '-' || (c >= '0' && c <= '9')
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func (s *scanner) isValidEscapeAt(pos int) bool {
	if pos >= len(s.src) || s.src[pos] != '\\' {
		return false
	}
	if pos+1 >= len(s.src) {
		return false
	}
	switch s.src[pos+1] {
	case '\n', '\r', '\f':
		return false
	}
	return true
}

func (s *scanner) wouldStartIdent(pos int) bool {
	c1 := s.peekAt(pos, 0)
	if isNameStart(c1) || c1 >= 0x80 {
		return true
	}
	if c1 == '-' {
		c2 := s.peekAt(pos, 1)
		if isNameStart(c2) || c2 == '-' || c2 >= 0x80 {
			return true
		}
		return s.isValidEscapeAt(pos + 1)
	}
	if c1 == '\\' {
		return s.isValidEscapeAt(pos)
	}
	return false
}

func (s *scanner) wouldStartNumber(pos int) bool {
	c1, c2 := s.peekAt(pos, 0), s.peekAt(pos, 1)
	switch c1 {
	case '+', '-':
		if c2 >= '0' && c2 <= '9' {
			return true
		}
		return c2 == '.' && s.peekAt(pos, 2) >= '0' && s.peekAt(pos, 2) <= '9'
	case '.':
		return c2 >= '0' && c2 <= '9'
	}
	return c1 >= '0' && c1 <= '9'
}

func (s *scanner) consumeEscape() {
	s.advance()
	if s.pos >= len(s.src) {
		return
	}
	c := s.src[s.pos]
	if isHexDigit(c) {
		for range 6 {
			if s.pos >= len(s.src) || !isHexDigit(s.src[s.pos]) {
				break
			}
			s.advance()
		}
		if s.pos < len(s.src) {
			switch s.src[s.pos] {
			case ' ', '\t', '\n', '\r', '\f':
				s.advance()
			}
		}
		return
	}
	s.advance()
}

func (s *scanner) consumeName() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		if isNameChar(c) || c >= 0x80 {
			s.advance()
			continue
		}
		if c == '\\' && s.isValidEscapeAt(s.pos) {
			s.consumeEscape()
			continue
		}
		return
	}
}

func (s *scanner) consumeIdentLike() (TokenType, string, int, int, int) {
	start, line, col := s.pos, s.line, s.col
	if s.pos < len(s.src) && s.src[s.pos] == '\\' {
		s.consumeEscape()
		s.consumeName()
	} else {
		s.consumeName()
	}
	if s.pos < len(s.src) && s.src[s.pos] == '(' {
		s.advance()
		return TokenFunction, s.src[start:s.pos], line, col, start
	}
	return TokenIdent, s.src[start:s.pos], line, col, start
}

func (s *scanner) consumeNumber() {
	if s.pos < len(s.src) {
		switch s.src[s.pos] {
		case '+', '-':
			s.advance()
		}
	}
	for s.pos < len(s.src) && s.src[s.pos] >= '0' && s.src[s.pos] <= '9' {
		s.advance()
	}
	if s.pos+1 < len(s.src) && s.src[s.pos] == '.' &&
		s.src[s.pos+1] >= '0' && s.src[s.pos+1] <= '9' {
		s.advance()
		for s.pos < len(s.src) && s.src[s.pos] >= '0' && s.src[s.pos] <= '9' {
			s.advance()
		}
	}
	if s.pos < len(s.src) && (s.src[s.pos] == 'e' || s.src[s.pos] == 'E') {
		next := s.pos + 1
		if next < len(s.src) && (s.src[next] == '+' || s.src[next] == '-') {
			next++
		}
		if next < len(s.src) && s.src[next] >= '0' && s.src[next] <= '9' {
			s.advance()
			if s.src[s.pos] == '+' || s.src[s.pos] == '-' {
				s.advance()
			}
			for s.pos < len(s.src) && s.src[s.pos] >= '0' && s.src[s.pos] <= '9' {
				s.advance()
			}
		}
	}
}

func (s *scanner) consumeNumericToken() Token {
	start, line, col := s.pos, s.line, s.col
	s.consumeNumber()
	if s.pos < len(s.src) && s.src[s.pos] == '%' {
		s.advance()
		return Token{TokenPercentage, s.src[start:s.pos], line, col, start, s.pos}
	}
	if s.wouldStartIdent(s.pos) {
		s.consumeName()
		return Token{TokenDimension, s.src[start:s.pos], line, col, start, s.pos}
	}
	return Token{TokenNumber, s.src[start:s.pos], line, col, start, s.pos}
}

func (s *scanner) consumeString(quote byte) Token {
	start, line, col := s.pos, s.line, s.col
	s.advance()
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == quote:
			s.advance()
			return Token{TokenString, s.src[start:s.pos], line, col, start, s.pos}
		case c == '\n' || c == '\r' || c == '\f':
			return Token{TokenBadString, s.src[start:s.pos], line, col, start, s.pos}
		case c == '\\':
			if s.pos+1 < len(s.src) {
				next := s.src[s.pos+1]
				if next == '\n' || next == '\r' || next == '\f' {
					s.advance()
					s.advance()
					continue
				}
				s.consumeEscape()
				continue
			}
			s.advance()
		default:
			s.advance()
		}
	}
	return Token{TokenString, s.src[start:s.pos], line, col, start, s.pos}
}

func (s *scanner) startsWithURL(pos int) bool {
	if pos+3 >= len(s.src) {
		return false
	}
	for i := range 4 {
		if s.src[pos+i]|0x20 != "url("[i] {
			return false
		}
	}
	return true
}

func (s *scanner) consumeURL() Token {
	start, line, col := s.pos, s.line, s.col
	for range 4 {
		s.advance()
	}
	skipWS := func() {
		for s.pos < len(s.src) {
			switch s.src[s.pos] {
			case ' ', '\t', '\n', '\r', '\f':
				s.advance()
			default:
				return
			}
		}
	}
	skipWS()
	if s.pos < len(s.src) && (s.src[s.pos] == '"' || s.src[s.pos] == '\'') {
		quote := s.src[s.pos]
		st := s.consumeString(quote)
		if st.Type == TokenBadString {
			s.consumeBadURLRemnants()
			return Token{TokenBadURL, s.src[start:s.pos], line, col, start, s.pos}
		}
		skipWS()
		if s.pos < len(s.src) && s.src[s.pos] == ')' {
			s.advance()
			return Token{TokenURL, s.src[start:s.pos], line, col, start, s.pos}
		}
		s.consumeBadURLRemnants()
		return Token{TokenBadURL, s.src[start:s.pos], line, col, start, s.pos}
	}
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == ')':
			s.advance()
			return Token{TokenURL, s.src[start:s.pos], line, col, start, s.pos}
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			after := s.pos
			for after < len(s.src) {
				if a := s.src[after]; a == ' ' || a == '\t' || a == '\n' || a == '\r' || a == '\f' {
					after++
					continue
				}
				break
			}
			if after < len(s.src) && s.src[after] == ')' {
				for s.pos < after {
					s.advance()
				}
				continue
			}
			s.consumeBadURLRemnants()
			return Token{TokenBadURL, s.src[start:s.pos], line, col, start, s.pos}
		case c == '\\':
			if s.isValidEscapeAt(s.pos) {
				s.consumeEscape()
				continue
			}
			s.consumeBadURLRemnants()
			return Token{TokenBadURL, s.src[start:s.pos], line, col, start, s.pos}
		case c == '"' || c == '\'' || c == '(' || c < 0x20 || c == 0x7f:
			s.consumeBadURLRemnants()
			return Token{TokenBadURL, s.src[start:s.pos], line, col, start, s.pos}
		default:
			s.advance()
		}
	}
	return Token{TokenBadURL, s.src[start:s.pos], line, col, start, s.pos}
}

func (s *scanner) consumeBadURLRemnants() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		if c == ')' {
			s.advance()
			return
		}
		if c == '\\' && s.isValidEscapeAt(s.pos) {
			s.consumeEscape()
			continue
		}
		s.advance()
	}
}

// Tokenize splits src into CSS tokens following the CSS Syntax Level 3 consume
// algorithms.
func Tokenize(src string) []Token {
	s := newScanner(stripBOM(src))
	out := make([]Token, 0, 64)
	emit := func(t TokenType, start, line, col int) {
		out = append(out, Token{t, s.src[start:s.pos], line, col, start, s.pos})
	}
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		start, line, col := s.pos, s.line, s.col
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			for s.pos < len(s.src) {
				if w := s.src[s.pos]; w == ' ' || w == '\t' || w == '\n' || w == '\r' || w == '\f' {
					s.advance()
					continue
				}
				break
			}
			emit(TokenWhitespace, start, line, col)
		case c == '/':
			if s.peek(1) == '*' {
				s.advance()
				s.advance()
				for s.pos < len(s.src) {
					if s.src[s.pos] == '*' && s.peek(1) == '/' {
						s.advance()
						s.advance()
						break
					}
					s.advance()
				}
				emit(TokenComment, start, line, col)
			} else {
				s.advance()
				emit(TokenDelim, start, line, col)
			}
		case c == '<':
			if s.peek(1) == '!' && s.peek(2) == '-' && s.peek(3) == '-' {
				for range 4 {
					s.advance()
				}
				emit(TokenCDO, start, line, col)
			} else {
				s.advance()
				emit(TokenDelim, start, line, col)
			}
		case c == '-':
			if s.peek(1) == '-' && s.peek(2) == '>' {
				for range 3 {
					s.advance()
				}
				emit(TokenCDC, start, line, col)
			} else if s.wouldStartNumber(s.pos) {
				t := s.consumeNumericToken()
				emit(t.Type, start, line, col)
			} else if s.wouldStartIdent(s.pos) {
				typ, _, _, _, _ := s.consumeIdentLike()
				emit(typ, start, line, col)
			} else {
				s.advance()
				emit(TokenDelim, start, line, col)
			}
		case c == '"' || c == '\'':
			t := s.consumeString(c)
			emit(t.Type, start, line, col)
		case c == '@':
			if s.wouldStartIdent(s.pos + 1) {
				s.advance()
				s.consumeName()
				emit(TokenAtKeyword, start, line, col)
			} else {
				s.advance()
				emit(TokenDelim, start, line, col)
			}
		case c == '#':
			n := s.peek(1)
			if isNameChar(n) || n >= 0x80 || s.isValidEscapeAt(s.pos+1) {
				s.advance()
				s.consumeName()
				emit(TokenHash, start, line, col)
			} else {
				s.advance()
				emit(TokenDelim, start, line, col)
			}
		case c == 'u' || c == 'U':
			if s.startsWithURL(s.pos) {
				emit(s.consumeURL().Type, start, line, col)
			} else if s.wouldStartIdent(s.pos) {
				typ, _, _, _, _ := s.consumeIdentLike()
				emit(typ, start, line, col)
			} else {
				s.advance()
				emit(TokenDelim, start, line, col)
			}
		case c >= '0' && c <= '9', c == '+', c == '.':
			if s.wouldStartNumber(s.pos) {
				t := s.consumeNumericToken()
				emit(t.Type, start, line, col)
			} else {
				s.advance()
				emit(TokenDelim, start, line, col)
			}
		case c == '(':
			s.advance()
			emit(TokenOpenParen, start, line, col)
		case c == ')':
			s.advance()
			emit(TokenCloseParen, start, line, col)
		case c == '[':
			s.advance()
			emit(TokenOpenSquare, start, line, col)
		case c == ']':
			s.advance()
			emit(TokenCloseSquare, start, line, col)
		case c == '{':
			s.advance()
			emit(TokenOpenCurly, start, line, col)
		case c == '}':
			s.advance()
			emit(TokenCloseCurly, start, line, col)
		case c == ':':
			s.advance()
			emit(TokenColon, start, line, col)
		case c == ';':
			s.advance()
			emit(TokenSemicolon, start, line, col)
		case c == ',':
			s.advance()
			emit(TokenComma, start, line, col)
		case isNameStart(c) || c >= 0x80:
			typ, _, _, _, _ := s.consumeIdentLike()
			emit(typ, start, line, col)
		case c == '\\':
			if s.isValidEscapeAt(s.pos) {
				typ, _, _, _, _ := s.consumeIdentLike()
				emit(typ, start, line, col)
			} else {
				s.advance()
				emit(TokenDelim, start, line, col)
			}
		default:
			s.advance()
			emit(TokenDelim, start, line, col)
		}
	}
	return out
}

func isTrivia(t Token) bool {
	return t.Type == TokenWhitespace || t.Type == TokenComment
}

func significant(t Token) bool {
	return !isTrivia(t)
}

func trimTokens(toks []Token) []Token {
	start, end := 0, len(toks)
	for start < end && isTrivia(toks[start]) {
		start++
	}
	for end > start && isTrivia(toks[end-1]) {
		end--
	}
	return toks[start:end]
}

func tokenRunText(toks []Token) string {
	var sb strings.Builder
	for _, t := range toks {
		if !significant(t) {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(t.Text)
	}
	return sb.String()
}

func stripBOM(src string) string {
	return bom.Trim(src)
}
