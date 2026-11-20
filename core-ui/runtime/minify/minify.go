// Package minify shrinks the embedded runtime JavaScript and the composed stylesheets without a parser.
package minify

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Minify returns src with comments and needless whitespace removed; it never panics.
func Minify(src string) string {
	if src == "" {
		return ""
	}
	m := &minifier{src: src}
	m.run()
	return m.out.String()
}

type tokKind int

const (
	tkNone tokKind = iota
	tkIdent
	tkNumber
	tkPunct
	tkString
	tkRegex
)

type minifier struct {
	src string
	pos int
	out strings.Builder

	hasEmitted    bool
	lastKind      tokKind
	lastByte      byte
	lastIdent     string
	lastWasIncDec bool

	sawNewline bool
	sawSpace   bool

	pending byte

	lastProp bool

	braces    []braceKind
	parens    []parenKind
	lastBrace braceKind
	lastParen parenKind

	ctrlPending  bool
	funcPending  parenKind
	classPending bool
	classKind    braceKind
	classDepth   int
	asyncAtStmt  bool

	casePending   bool
	lastColonCase bool
	lastIdentStmt bool
	lastArrow     bool
}

type braceKind uint8

const (
	bkBlock braceKind = iota
	bkObject
	bkFuncExpr
	bkArrow
	bkClassDecl
	bkClassExpr
)

type parenKind uint8

const (
	pkOther parenKind = iota
	pkControl
	pkFuncDecl
	pkFuncExpr
)

var regexKeywords = map[string]bool{
	"return": true, "typeof": true, "instanceof": true, "in": true, "of": true,
	"new": true, "delete": true, "void": true, "throw": true, "case": true,
	"do": true, "else": true, "if": true, "while": true, "for": true,
	"var": true, "let": true, "const": true, "yield": true, "await": true,
	"switch": true,
}

var asiKeywords = map[string]bool{
	"return": true, "throw": true, "break": true, "continue": true, "yield": true,
}

var nonEndingKeywords = map[string]bool{
	"else": true, "do": true, "try": true, "finally": true, "case": true,
	"typeof": true, "instanceof": true, "in": true, "new": true, "delete": true,
	"void": true, "var": true, "const": true, "if": true, "while": true,
	"for": true, "switch": true, "with": true, "function": true, "class": true,
	"extends": true, "import": true, "export": true, "catch": true,
}

var objectAfterKeyword = map[string]bool{
	"return": true, "typeof": true, "instanceof": true, "in": true, "of": true,
	"new": true, "delete": true, "void": true, "throw": true, "case": true,
	"yield": true, "await": true,
}

var controlKeywords = map[string]bool{
	"if": true, "while": true, "for": true, "with": true,
}

func isIdentStart(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_' || c == '$'
}
func isIdentCont(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}
func isDigit(c byte) bool    { return c >= '0' && c <= '9' }
func isHexDigit(c byte) bool { return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') }

func isIdentByte(c byte) bool { return isIdentCont(c) || c >= utf8.RuneSelf || c == '\\' }

func jsSpace(src string, i int) (width int, newline bool) {
	c := src[i]
	switch c {
	case ' ', '\t', '\v', '\f':
		return 1, false
	case '\n', '\r':
		return 1, true
	}
	if c < utf8.RuneSelf {
		return 0, false
	}
	r, n := utf8.DecodeRuneInString(src[i:])
	switch {
	case r == '\u2028' || r == '\u2029':
		return n, true
	case r == '\u00a0' || r == '\ufeff' || unicode.Is(unicode.Zs, r):
		return n, false
	}
	return 0, false
}

func isLineTerminator(src string, i int) bool {
	n, nl := jsSpace(src, i)
	return n > 0 && nl
}

func (m *minifier) peek(off int) byte {
	if m.pos+off >= len(m.src) || m.pos+off < 0 {
		return 0
	}
	return m.src[m.pos+off]
}

func (m *minifier) run() {
	for m.pos < len(m.src) {
		m.step()
	}
	if m.pending != 0 {
		m.out.WriteByte(m.pending)
		m.pending = 0
	}
}

func (m *minifier) step() {
	c := m.src[m.pos]
	if n, nl := jsSpace(m.src, m.pos); n > 0 {
		m.sawSpace = true
		m.sawNewline = m.sawNewline || nl
		m.pos += n
		return
	}
	switch {
	case c == '/' && m.peek(1) == '/':
		m.skipLineComment()
	case c == '/' && m.peek(1) == '*':
		m.skipBlockComment()
	case c == '\'' || c == '"':
		m.emitString(c)
	case c == '`':
		m.emitTemplate()
	case c == '/' && m.prevAllowsRegex():
		m.emitRegex()
	case isIdentStart(c) || c >= utf8.RuneSelf || (c == '\\' && m.peek(1) == 'u'):
		m.emitIdent()
	case isDigit(c):
		m.emitNumber()
	case c == '.' && isDigit(m.peek(1)):
		m.emitNumber()
	default:
		m.emitPunct()
	}
}

func (m *minifier) skipLineComment() {
	for m.pos < len(m.src) && !isLineTerminator(m.src, m.pos) {
		m.pos++
	}
	m.sawSpace = true
}

func (m *minifier) skipBlockComment() {
	m.pos += 2
	for m.pos < len(m.src) {
		if m.pos+1 < len(m.src) && m.src[m.pos] == '*' && m.src[m.pos+1] == '/' {
			m.pos += 2
			break
		}
		if isLineTerminator(m.src, m.pos) {
			m.sawNewline = true
		}
		m.pos++
	}
	m.sawSpace = true
}

func (m *minifier) prevAllowsRegex() bool {
	if !m.hasEmitted {
		return true
	}
	switch m.lastKind {
	case tkIdent:
		return !m.lastProp && regexKeywords[m.lastIdent]
	case tkNumber, tkString, tkRegex:
		return false
	case tkPunct:
		switch m.lastByte {
		case ')':
			return m.lastParen == pkControl
		case ']':
			return false
		case '}':
			return m.lastBrace != bkObject && m.lastBrace != bkFuncExpr && m.lastBrace != bkClassExpr
		}
		return !m.lastWasIncDec
	}
	return true
}

func (m *minifier) endsExpr() bool {
	if m.lastWasIncDec {
		return true
	}
	switch m.lastKind {
	case tkIdent:
		if m.lastProp || m.inClassBody() {
			return true
		}
		return !asiKeywords[m.lastIdent] && !nonEndingKeywords[m.lastIdent]
	case tkNumber, tkString, tkRegex:
		return true
	case tkPunct:
		switch m.lastByte {
		case ')':
			return m.lastParen == pkOther
		case ']':
			return true
		case '}':
			switch m.lastBrace {
			case bkObject, bkFuncExpr, bkArrow, bkClassExpr:
				return true
			}
		}
	}
	return false
}

func isASIHazardPrefix(c byte) bool {
	switch c {
	case '(', '[', '+', '-', '/', '`', '!', '~', '{', '\'', '"', '#', '*':
		return true
	}
	return false
}

func fusionRisk(lastKind tokKind, last, first byte) bool {
	wordy := lastKind == tkIdent || lastKind == tkNumber || lastKind == tkRegex
	if wordy && isIdentByte(first) {
		return true
	}
	if lastKind == tkNumber && first == '.' {
		return true
	}
	if last == '<' && first == '!' {
		return true
	}
	if last == '+' && first == '+' {
		return true
	}
	if last == '-' && first == '-' {
		return true
	}
	if last == '/' && (first == '/' || first == '*') {
		return true
	}
	return false
}

func (m *minifier) emitSep(firstByte byte) {
	defer func() {
		m.sawNewline = false
		m.sawSpace = false
	}()
	if m.pending != 0 {
		if firstByte != '}' {
			m.out.WriteByte(m.pending)
		}
		m.pending = 0
	}
	if !m.hasEmitted {
		return
	}
	if m.sawNewline {
		if m.lastKind == tkIdent && !m.lastProp && asiKeywords[m.lastIdent] {
			m.out.WriteByte('\n')
			return
		}
		if m.endsExpr() && (isASIHazardPrefix(firstByte) || isIdentByte(firstByte)) {
			m.out.WriteByte('\n')
			return
		}
	}
	if fusionRisk(m.lastKind, m.lastByte, firstByte) && m.sawSpace {
		m.out.WriteByte(' ')
	}
}

func (m *minifier) emitIdent() {
	start := m.pos
	m.pos = scanIdent(m.src, m.pos)
	ident := m.src[start:m.pos]
	prop := m.hasEmitted && m.lastKind == tkPunct && m.lastByte == '.'
	kw := ident
	if prop {
		kw = ""
	}
	stmt := m.atStatementStart()
	afterAsync := m.lastKind == tkIdent && !m.lastProp && m.lastIdent == "async"
	ctrl := controlKeywords[kw] || (kw == "await" && m.ctrlPending && m.lastIdent == "for")
	if kw == "const" && m.constIsDeclaration() && m.nextStartsBinding() {
		ident = "let"
	}
	m.emitSep(ident[0])
	m.out.WriteString(ident)

	switch {
	case kw == "function":
		m.funcPending = pkFuncExpr
		if stmt || (afterAsync && m.asyncAtStmt) {
			m.funcPending = pkFuncDecl
		}
	case m.funcPending != pkOther:
	case kw == "class":
		m.classPending = true
		m.classKind = bkClassExpr
		if stmt {
			m.classKind = bkClassDecl
		}
		m.classDepth = len(m.braces) + len(m.parens)
	case kw == "async":
		m.asyncAtStmt = stmt
	case (kw == "case" || kw == "default") && m.topBrace() != bkObject:
		m.casePending = true
	}
	m.ctrlPending = ctrl
	m.lastArrow = false
	m.lastColonCase = false
	m.lastIdentStmt = stmt && !prop
	m.lastProp = prop
	m.lastKind = tkIdent
	m.lastByte = ident[len(ident)-1]
	m.lastIdent = ident
	m.lastWasIncDec = false
	m.hasEmitted = true
}

func scanIdent(src string, i int) int {
	start := i
scan:
	for i < len(src) {
		c := src[i]
		switch {
		case isIdentCont(c):
			i++
		case c == '\\' && i+1 < len(src) && src[i+1] == 'u':
			i += 2
			if i < len(src) && src[i] == '{' {
				i++
				for i < len(src) && isHexDigit(src[i]) {
					i++
				}
				if i < len(src) && src[i] == '}' {
					i++
				}
				continue
			}
			for k := 0; k < 4 && i < len(src) && isHexDigit(src[i]); k++ {
				i++
			}
		case c >= utf8.RuneSelf:
			if n, _ := jsSpace(src, i); n > 0 {
				break scan
			}
			_, n := utf8.DecodeRuneInString(src[i:])
			i += n
		default:
			break scan
		}
	}
	if i == start {
		i++
	}
	return i
}

func (m *minifier) atStatementStart() bool {
	if !m.hasEmitted {
		return true
	}
	switch m.lastKind {
	case tkPunct:
		switch m.lastByte {
		case ';', '}':
			return true
		case '{':
			return m.topBrace() != bkObject
		case ')':
			return m.lastParen == pkControl
		}
	case tkIdent:
		if !m.lastProp {
			switch m.lastIdent {
			case "else", "do", "export", "default":
				return true
			}
		}
	}
	return m.sawNewline && m.endsExpr()
}

func (m *minifier) constIsDeclaration() bool {
	return !m.inClassBody() && m.topBrace() != bkObject
}

func (m *minifier) inClassBody() bool {
	top := m.topBrace()
	return top == bkClassDecl || top == bkClassExpr
}

func (m *minifier) topBrace() braceKind {
	if len(m.braces) == 0 {
		return bkBlock
	}
	return m.braces[len(m.braces)-1]
}

func (m *minifier) openBraceKind() braceKind {
	if m.classPending && len(m.braces)+len(m.parens) == m.classDepth {
		m.classPending = false
		return m.classKind
	}
	if !m.hasEmitted {
		return bkBlock
	}
	switch m.lastKind {
	case tkPunct:
		switch m.lastByte {
		case ')':
			if m.lastParen == pkFuncExpr {
				return bkFuncExpr
			}
			return bkBlock
		case '>':
			if m.lastArrow {
				return bkArrow
			}
		case ';', '{', '}', ']':
			return bkBlock
		case ':':
			if m.lastColonCase {
				return bkBlock
			}
		}
		return bkObject
	case tkIdent:
		if !m.lastProp && objectAfterKeyword[m.lastIdent] &&
			!(asiKeywords[m.lastIdent] && m.sawNewline) {
			return bkObject
		}
	}
	return bkBlock
}

func (m *minifier) nextStartsBinding() bool {
	i := m.pos
	for i < len(m.src) {
		c := m.src[i]
		if n, _ := jsSpace(m.src, i); n > 0 {
			i += n
			continue
		}
		switch {
		case c == '/' && i+1 < len(m.src) && m.src[i+1] == '/':
			for i < len(m.src) && !isLineTerminator(m.src, i) {
				i++
			}
		case c == '/' && i+1 < len(m.src) && m.src[i+1] == '*':
			i += 2
			for i+1 < len(m.src) && !(m.src[i] == '*' && m.src[i+1] == '/') {
				i++
			}
			i += 2
		default:
			return isIdentStart(c) || c >= utf8.RuneSelf || c == '\\' || c == '{' || c == '['
		}
	}
	return false
}

func (m *minifier) emitNumber() {
	start := m.pos
	if m.src[m.pos] == '0' && m.pos+1 < len(m.src) {
		n := m.src[m.pos+1]
		if n == 'x' || n == 'X' || n == 'o' || n == 'O' || n == 'b' || n == 'B' {
			m.pos += 2
			for m.pos < len(m.src) && (isHexDigit(m.src[m.pos]) || m.src[m.pos] == '_') {
				m.pos++
			}
			m.flushNumber(start)
			return
		}
	}
	for m.pos < len(m.src) {
		c := m.src[m.pos]
		switch {
		case isDigit(c), c == '.', c == '_':
			m.pos++
		case c == 'e' || c == 'E':
			m.pos++
			if m.pos < len(m.src) && (m.src[m.pos] == '+' || m.src[m.pos] == '-') {
				m.pos++
			}
		case c == 'n':
			m.pos++
			m.flushNumber(start)
			return
		default:
			m.flushNumber(start)
			return
		}
	}
	m.flushNumber(start)
}

func (m *minifier) flushNumber(start int) {
	num := m.src[start:m.pos]
	first := num[0]
	if first == '.' {
		first = '0'
	}
	m.emitSep(first)
	m.out.WriteString(num)
	m.setLast(tkNumber, num[len(num)-1])
}

func (m *minifier) setLast(kind tokKind, last byte) {
	m.lastKind = kind
	m.lastByte = last
	m.lastIdent = ""
	m.lastWasIncDec = false
	m.hasEmitted = true
	m.lastProp = false
	m.ctrlPending = false
	m.funcPending = pkOther
	m.lastArrow = false
	m.lastColonCase = false
}

func (m *minifier) emitString(quote byte) {
	start := m.pos
	m.pos++
	for m.pos < len(m.src) {
		c := m.src[m.pos]
		if c == '\\' {
			if m.pos+1 < len(m.src) {
				m.pos += 2
				continue
			}
			m.pos++
			break
		}
		if c == quote {
			m.pos++
			break
		}
		m.pos++
	}
	str := m.src[start:m.pos]
	m.emitSep(str[0])
	m.out.WriteString(str)
	m.setLast(tkString, quote)
}

func (m *minifier) emitTemplate() {
	m.emitSep('`')
	m.out.WriteByte('`')
	m.pos++
	for m.pos < len(m.src) {
		c := m.src[m.pos]
		if c == '\\' {
			m.out.WriteByte(c)
			if m.pos+1 < len(m.src) {
				m.out.WriteByte(m.src[m.pos+1])
				m.pos += 2
			} else {
				m.pos++
			}
			continue
		}
		if c == '`' {
			m.out.WriteByte('`')
			m.pos++
			break
		}
		if c == '$' && m.peek(1) == '{' {
			m.out.WriteString("${")
			m.pos += 2
			m.minifyTemplateExpr()
			continue
		}
		m.out.WriteByte(c)
		m.pos++
	}
	m.setLast(tkString, '`')
}

func (m *minifier) minifyTemplateExpr() {
	saved := *m
	m.setLast(tkPunct, '(')
	m.sawNewline = false
	m.sawSpace = false
	m.braces = nil
	m.parens = nil
	m.classPending = false
	m.casePending = false

	depth := 1
	for depth > 0 && m.pos < len(m.src) {
		c := m.src[m.pos]
		if c == '{' {
			depth++
			m.emitPunct()
			continue
		}
		if c == '}' {
			depth--
			if depth == 0 {
				m.pos++
				m.pending = 0
				m.out.WriteByte('}')
				break
			}
			m.emitPunct()
			continue
		}
		m.step()
	}

	pos := m.pos
	out := m.out
	*m = saved
	m.pos = pos
	m.out = out
}

func (m *minifier) emitRegex() {
	start := m.pos
	m.pos++
	inClass := false
	for m.pos < len(m.src) {
		c := m.src[m.pos]
		if c == '\\' {
			if m.pos+1 < len(m.src) {
				m.pos += 2
				continue
			}
			m.pos++
			break
		}
		if isLineTerminator(m.src, m.pos) {
			m.pos = start
			m.emitPunct()
			return
		}
		if c == '[' {
			inClass = true
			m.pos++
			continue
		}
		if c == ']' && inClass {
			inClass = false
			m.pos++
			continue
		}
		if c == '/' && !inClass {
			m.pos++
			break
		}
		m.pos++
	}
	for m.pos < len(m.src) && isIdentCont(m.src[m.pos]) {
		m.pos++
	}
	tok := m.src[start:m.pos]
	m.emitSep(tok[0])
	m.out.WriteString(tok)
	m.setLast(tkRegex, tok[len(tok)-1])
}

func (m *minifier) emitPunct() {
	c := m.src[m.pos]
	m.emitPunctChar(c)
	m.pos++
}

func (m *minifier) emitPunctChar(c byte) {
	incDec := false
	if (c == '+' || c == '-') && m.lastByte == c && m.lastKind == tkPunct && !m.sawSpace && !m.lastWasIncDec {
		incDec = true
	}
	keepSemi := c == ';' && m.semiMayBeEmptyStatement()
	arrow := c == '>' && m.lastKind == tkPunct && m.lastByte == '=' && !m.sawSpace
	var opened braceKind
	if c == '{' {
		opened = m.openBraceKind()
	}
	m.emitSep(c)
	if keepSemi {
		m.out.WriteByte(c)
	} else if c == ';' || c == ',' {
		m.pending = c
	} else {
		m.out.WriteByte(c)
	}

	colonCase := false
	switch c {
	case '{':
		m.braces = append(m.braces, opened)
		m.casePending = false
	case '}':
		m.lastBrace = bkBlock
		if n := len(m.braces); n > 0 {
			m.lastBrace = m.braces[n-1]
			m.braces = m.braces[:n-1]
		}
		m.casePending = false
	case '(':
		kind := m.funcPending
		if m.ctrlPending {
			kind = pkControl
		}
		m.parens = append(m.parens, kind)
	case ')':
		m.lastParen = pkOther
		if n := len(m.parens); n > 0 {
			m.lastParen = m.parens[n-1]
			m.parens = m.parens[:n-1]
		}
	case ';':
		m.casePending = false
	case ':':
		colonCase = m.casePending || (m.lastKind == tkIdent && m.lastIdentStmt && !m.inClassBody())
		m.casePending = false
	}
	if c != '*' {
		m.funcPending = pkOther
	}
	m.ctrlPending = false
	m.lastArrow = arrow
	m.lastColonCase = colonCase
	m.lastProp = false
	m.lastKind = tkPunct
	m.lastByte = c
	m.lastIdent = ""
	m.lastWasIncDec = incDec
	m.hasEmitted = true
}

func (m *minifier) semiMayBeEmptyStatement() bool {
	switch m.lastKind {
	case tkPunct:
		return m.lastByte == ')' || m.lastByte == ':'
	case tkIdent:
		return !m.lastProp && m.lastIdent == "else"
	}
	return false
}
