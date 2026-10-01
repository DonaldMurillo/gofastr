// Package ownstyle compiles, checks and registers owned stylesheets:
// the .style.css files a layout, screen, component or app owns
// (design doc: "Owned styles", v0.86.0).
//
// The pipeline, each stage a file in this package:
//
//	token.go  CSS Syntax Level 3 tokenizer, every token carrying a
//	          1-based line and column.
//	parse.go  stylesheet parser: qualified rules, at-rules, nested
//	          blocks, declarations, doc comments.
//	compile.go the compiler: scope wrap, custom-media expansion against
//	          the running theme, @keyframes renaming, minification.
//	check.go  the CSS-only GOFASTR checks (1806-1808, 1810-1813,
//	          1819-1820) plus parse errors.
//	model.go  the class/variant model the generator emits Go from.
//	sheet.go  Must / Sheet.Scope: registration and the scope marker.
//
// Dependency direction (design doc): this package imports core-ui/registry
// and core-ui/style; nothing in registry or style imports it.
//
// Positions are 1-based; columns count runes from the line start. Token
// text is the raw source slice, escapes and all — nothing is decoded, so
// what the compiler re-emits is byte-for-byte what the author wrote.
package ownstyle

import (
	"strings"
	"unicode/utf8"
)

// TokenType names the CSS Syntax Level 3 token classes this tokenizer
// produces. Comments are kept as tokens (doc comments must survive) and
// so is whitespace (the minifier needs to know where a descendant
// combinator or a required separator stood).
type TokenType int

// Token types. The zero value is not a valid type; Tokenize always sets
// one of these.
const (
	TokenIdent       TokenType = iota + 1 // foo, --spacing-md
	TokenFunction                         // var(, minmax(, color-mix(
	TokenAtKeyword                        // @media
	TokenHash                             // #fff, #main
	TokenString                           // "foo", 'foo'
	TokenBadString                        // "foo⏎ (unescaped newline)
	TokenURL                              // url(img.png)
	TokenBadURL                           // url(a b)
	TokenDelim                            // any single character
	TokenNumber                           // 1, +2.5, .5, 1e3
	TokenPercentage                       // 50%
	TokenDimension                        // 16px, 1.3rem, 400ms
	TokenWhitespace                       // runs of space, tab, newline
	TokenCDO                              // <!--
	TokenCDC                              // -->
	TokenColon                            // :
	TokenSemicolon                        // ;
	TokenComma                            // ,
	TokenOpenSquare                       // [
	TokenCloseSquare                      // ]
	TokenOpenParen                        // (
	TokenCloseParen                       // )
	TokenOpenCurly                        // {
	TokenCloseCurly                       // }
	TokenComment                          // /* … */
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

// Token is one CSS token. Text is the raw source slice; Line and Col
// point at its first character; Start and End are byte offsets into the
// source, End exclusive.
type Token struct {
	Type  TokenType
	Text  string
	Line  int
	Col   int
	Start int
	End   int
}

// scanner walks the source once, tracking a 1-based rune column.
type scanner struct {
	src  string
	pos  int // byte offset of the next unread byte
	line int
	col  int // rune column of the next unread rune
}

func newScanner(src string) *scanner {
	return &scanner{src: src, line: 1, col: 1}
}

// peek returns the byte at pos+i, or 0 at EOF.
func (s *scanner) peek(i int) byte {
	if s.pos+i >= len(s.src) {
		return 0
	}
	return s.src[s.pos+i]
}

// peekAt reads the byte at pos+i without consuming, 0 at EOF.
func (s *scanner) peekAt(pos, i int) byte {
	if pos+i >= len(s.src) {
		return 0
	}
	return s.src[pos+i]
}

// advance consumes one rune (one byte when the input is not valid UTF-8)
// and keeps line/column honest: \r\n counts as one line break, and any
// other \n, \r or \f starts a new line.
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

// isNameStart reports whether c (an ASCII byte) may start a name.
// Non-ASCII is handled by the caller via byte >= 0x80.
func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isNameChar reports whether c (an ASCII byte) may continue a name:
// name-start characters plus digits and '-'.
func isNameChar(c byte) bool {
	return isNameStart(c) || c == '-' || (c >= '0' && c <= '9')
}

// isHexDigit reports whether c is a hex digit.
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// isValidEscapeAt reports whether the bytes at pos..pos+1 form a valid
// escape: a backslash not followed by a line break or EOF.
func (s *scanner) isValidEscapeAt(pos int) bool {
	if pos >= len(s.src) || s.src[pos] != '\\' {
		return false
	}
	if pos+1 >= len(s.src) {
		return false // \ at EOF is not a valid escape
	}
	switch s.src[pos+1] {
	case '\n', '\r', '\f':
		return false
	}
	return true
}

// wouldStartIdent implements the spec's "check if three characters
// would start an identifier" (CSS Syntax 3 §4.3.3), evaluated at pos.
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

// wouldStartNumber implements the spec's "check if three characters
// would start a number": a digit, or . digit, or +/- followed by either.
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

// consumeEscape consumes one backslash escape, assuming a valid escape
// starts at the current position. Hex escapes swallow one trailing
// whitespace; \<newline> never reaches here (not a valid escape).
func (s *scanner) consumeEscape() {
	s.advance() // the backslash
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
		// One whitespace after the hex digits is part of the escape.
		if s.pos < len(s.src) {
			switch s.src[s.pos] {
			case ' ', '\t', '\n', '\r', '\f':
				s.advance()
			}
		}
		return
	}
	s.advance() // the escaped character itself
}

// consumeName consumes a sequence of name characters and escapes.
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

// consumeIdentLike consumes an identifier, returning (type, text, line,
// col, start). Callers must have checked wouldStartIdent.
func (s *scanner) consumeIdentLike() (TokenType, string, int, int, int) {
	start, line, col := s.pos, s.line, s.col
	if s.pos < len(s.src) && s.src[s.pos] == '\\' {
		s.consumeEscape()
		s.consumeName()
	} else {
		// Leading '-' and '--' are name characters; consumeName covers
		// them and everything after.
		s.consumeName()
	}
	if s.pos < len(s.src) && s.src[s.pos] == '(' {
		s.advance()
		return TokenFunction, s.src[start:s.pos], line, col, start
	}
	return TokenIdent, s.src[start:s.pos], line, col, start
}

// consumeNumber consumes the numeric part of a number token: sign,
// digits, optional fraction, optional exponent (only when a digit
// follows the optional sign after the e).
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
		s.advance() // .
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
			s.advance() // e
			if s.src[s.pos] == '+' || s.src[s.pos] == '-' {
				s.advance()
			}
			for s.pos < len(s.src) && s.src[s.pos] >= '0' && s.src[s.pos] <= '9' {
				s.advance()
			}
		}
	}
}

// consumeNumericToken consumes a number, percentage or dimension and
// returns the token (position fields not yet set).
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

// consumeString consumes a quoted string. An unescaped newline produces
// a bad-string token that stops before the newline (the newline is left
// for the whitespace consumer); EOF produces a string token (the parse
// pass reports the error).
func (s *scanner) consumeString(quote byte) Token {
	start, line, col := s.pos, s.line, s.col
	s.advance() // opening quote
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == quote:
			s.advance()
			return Token{TokenString, s.src[start:s.pos], line, col, start, s.pos}
		case c == '\n' || c == '\r' || c == '\f':
			// Unescaped line break: bad-string, newline NOT consumed.
			return Token{TokenBadString, s.src[start:s.pos], line, col, start, s.pos}
		case c == '\\':
			if s.pos+1 < len(s.src) {
				next := s.src[s.pos+1]
				if next == '\n' || next == '\r' || next == '\f' {
					// Escaped line break: string continuation.
					s.advance()
					s.advance()
					continue
				}
				s.consumeEscape()
				continue
			}
			s.advance() // backslash at EOF
		default:
			s.advance()
		}
	}
	return Token{TokenString, s.src[start:s.pos], line, col, start, s.pos}
}

// startsWithURL reports whether pos begins an ASCII case-insensitive
// "url(".
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

// consumeURL consumes a url(…) token, starting at the 'u' of "url(".
func (s *scanner) consumeURL() Token {
	start, line, col := s.pos, s.line, s.col
	for range 4 {
		s.advance() // url(
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
	// Quoted form (CSS Syntax 3 §4.3.6 step 3): url("…") is ONE url
	// token whose value is the string, closed by optional whitespace
	// and ')'.
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
			// Whitespace inside an unquoted url is only legal as the run
			// just before the closing paren.
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
				continue // the ')' arm closes the token
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
			// Quotes, parens and non-printables are invalid in an
			// unquoted url.
			s.consumeBadURLRemnants()
			return Token{TokenBadURL, s.src[start:s.pos], line, col, start, s.pos}
		default:
			s.advance()
		}
	}
	// EOF before ')': bad-url.
	return Token{TokenBadURL, s.src[start:s.pos], line, col, start, s.pos}
}

// consumeBadURLRemnants consumes everything up to the closing paren,
// honouring escapes.
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

// Tokenize splits src into CSS tokens following the CSS Syntax Level 3
// consume algorithms. It never fails: malformed input yields bad-string
// / bad-url tokens and unterminated comments that the parser reports.
func Tokenize(src string) []Token {
	s := newScanner(src)
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
			// At-keyword: '@' followed by an identifier.
			if s.wouldStartIdent(s.pos + 1) {
				s.advance() // @
				s.consumeName()
				emit(TokenAtKeyword, start, line, col)
			} else {
				s.advance()
				emit(TokenDelim, start, line, col)
			}
		case c == '#':
			n := s.peek(1)
			if isNameChar(n) || n >= 0x80 || s.isValidEscapeAt(s.pos+1) {
				s.advance() // #
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

// isTrivia reports whether a token is whitespace or a comment.
func isTrivia(t Token) bool {
	return t.Type == TokenWhitespace || t.Type == TokenComment
}

// significant reports whether a token carries CSS meaning (not trivia).
func significant(t Token) bool {
	return !isTrivia(t)
}

// trimTokens returns the token run with leading and trailing
// whitespace/comment tokens removed. Interior tokens are kept as-is.
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

// tokenRunText renders a token run the way diagnostics quote it: the
// significant token texts joined with single spaces.
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
