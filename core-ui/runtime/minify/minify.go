// Package minify implements a token-aware JavaScript minifier used to
// shrink the embedded runtime sources before they're served.
//
// Scope (tier-2):
//
//   - Strip line + block comments.
//   - Collapse whitespace, preserving newlines only where ASI cares.
//   - Distinguish regex literals from division.
//   - Preserve string + template-literal payloads byte-for-byte.
//   - Insert a single space when removing whitespace would fuse two
//     adjacent tokens (`let a` → `leta`, `+ +` → `++`, `/ /` → `//`).
//   - Rewrite a `const` declaration keyword to `let`. For any program
//     that runs without throwing, the two are semantically identical
//     (same block scoping, same TDZ, same per-iteration for-of/for-in
//     binding); const's extra assignment check can only fire in code
//     that is already broken. Non-declaration uses (`a.const`,
//     `{const: 1}`, `class A{const=1}`, `class A{const\nx=1}`) are
//     left alone: the rewrite applies only when the keyword is not a
//     property name, does not sit directly inside a class body or an
//     object literal, and the next token starts a binding (identifier,
//     `{`, or `[`).
//   - Drop a `;` or `,` immediately before a `}` (`a=1;}` → `a=1}`,
//     `{a:1,}` → `{a:1}`). In valid JS a `}` can only follow `;` as a
//     statement/class-element terminator (droppable) and `,` as a
//     trailing comma in an object literal / destructuring /
//     import-specifier list (droppable; array-hole commas sit before
//     `]`, which is never touched). Exception: a `;` whose previous
//     token is `)` is never dropped, it may be an empty statement
//     serving as an if/while/for body (`if(x);}` → `if(x)}` is a
//     SyntaxError), and a scanner can't tell that apart from `g();}`.
//     The same rule keeps the do-while terminator (`do{}while(x);}`),
//     and a `;` after `else` or a label's `:` (`else;}`, `foo:;}`) is
//     an empty statement too, so it is kept.
//
// Context the scanner tracks, because a token stream alone cannot
// decide regex-vs-division or whether a newline carries ASI:
//
//   - A paren stack: the `)` closing an if/while/for/with head is
//     followed by a statement (so `/` starts a regex and a newline is
//     not needed), and the `)` closing a function's parameters
//     announces a body.
//   - A brace stack: each `{` is classified as a block (statement
//     level, function declaration or method body, class declaration
//     body) or an expression (object literal, function/class
//     expression, arrow body). A newline after an expression `}` keeps
//     ASI alive (`f=()=>{}\nfoo()`); after a block `}` it is dropped.
//     The class-body kinds also keep `const` class fields away from the
//     const→let rewrite.
//   - Identifiers after `.` are property names, never keywords
//     (`o.in / 2` is a division).
//
// Line terminators are LF, CR, U+2028 and U+2029; non-ASCII letters
// and `\u` escapes are identifier characters; NBSP, BOM and the Zs
// spaces are whitespace.
//
// Out of scope (would be tier-3): identifier renaming, dead-code
// elimination, constant folding, anything that requires a parser.
//
// The output is intentionally still valid, readable-ish JavaScript,
// it stays parseable by the browser without source maps and a developer
// can still set breakpoints inside it.
package minify

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Minify returns a shrunk-but-equivalent JavaScript source. The output
// preserves the semantics of the input across every construct the
// embedded runtime currently uses. Inputs that aren't valid JS produce
// undefined-but-stable output (the minifier never panics).
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

	// pending holds a deferred ';' or ',' (0 = none). The byte is
	// written when the NEXT token arrives, unless that token starts
	// with '}', in which case it's redundant and dropped. State
	// (lastKind/lastByte) is updated at defer time, so separator and
	// regex-vs-division decisions behave exactly as if it were written.
	pending byte

	// lastProp: the last identifier followed a `.`, so it is a
	// property name (`o.in`, `a.return`), never a keyword.
	lastProp bool

	// braces / parens are the open `{` and `(` kinds; lastBrace and
	// lastParen describe the most recently CLOSED one, read when the
	// last emitted token is `}` or `)`.
	braces    []braceKind
	parens    []parenKind
	lastBrace braceKind
	lastParen parenKind

	// ctrlPending: an if/while/for/with keyword was just emitted, so
	// the next `(` opens a control head. funcPending: a `function`
	// keyword's parameter list has not opened yet (pkFuncDecl or
	// pkFuncExpr; pkOther = none). classPending: a `class` keyword's
	// body has not opened yet; it opens at nesting depth classDepth.
	ctrlPending  bool
	funcPending  parenKind
	classPending bool
	classKind    braceKind
	classDepth   int
	asyncAtStmt  bool // the last `async` keyword started a statement

	// casePending: a `case`/`default` label's `:` has not arrived yet;
	// lastColonCase: the last `:` ended such a label (a `{` after it is
	// a block, not an object literal). lastArrow: the last `>` was the
	// tail of `=>`.
	casePending   bool
	lastColonCase bool
	// lastIdentStmt: the last identifier began a statement, so a `:`
	// right after it ends a label and the `{` that follows is a block.
	lastIdentStmt bool
	lastArrow     bool
}

// braceKind classifies an open `{`.
type braceKind uint8

const (
	bkBlock     braceKind = iota // block, function declaration / method body
	bkObject                     // object literal (or destructuring pattern)
	bkFuncExpr                   // function expression body
	bkArrow                      // arrow function body
	bkClassDecl                  // class declaration body
	bkClassExpr                  // class expression body
)

// parenKind classifies an open `(`.
type parenKind uint8

const (
	pkOther    parenKind = iota // call, grouping, method parameters
	pkControl                   // if/while/for/with head
	pkFuncDecl                  // function declaration parameters
	pkFuncExpr                  // function expression parameters
)

// Tokens after which a `/` starts a regex literal rather than a
// division operator. JavaScript's grammar is famously context-sensitive
// here; this set covers every keyword that legitimately precedes a
// regex.
var regexKeywords = map[string]bool{
	"return": true, "typeof": true, "instanceof": true, "in": true, "of": true,
	"new": true, "delete": true, "void": true, "throw": true, "case": true,
	"do": true, "else": true, "if": true, "while": true, "for": true,
	"var": true, "let": true, "const": true, "yield": true, "await": true,
	"switch": true,
}

// Keywords that trigger ASI when followed by a newline. The newline
// MUST survive minification or the program semantics change.
var asiKeywords = map[string]bool{
	"return": true, "throw": true, "break": true, "continue": true, "yield": true,
}

// Reserved words that can never end an expression: a newline after
// one never triggers ASI, whatever follows it.
var nonEndingKeywords = map[string]bool{
	"else": true, "do": true, "try": true, "finally": true, "case": true,
	"typeof": true, "instanceof": true, "in": true, "new": true, "delete": true,
	"void": true, "var": true, "const": true, "if": true, "while": true,
	"for": true, "switch": true, "with": true, "function": true, "class": true,
	"extends": true, "import": true, "export": true, "catch": true,
}

// Keywords after which a `{` opens an object literal (an expression
// follows them), not a block.
var objectAfterKeyword = map[string]bool{
	"return": true, "typeof": true, "instanceof": true, "in": true, "of": true,
	"new": true, "delete": true, "void": true, "throw": true, "case": true,
	"yield": true, "await": true,
}

// Keywords whose following `(` opens a control head: its `)` is
// followed by a statement, never an operator.
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

// isIdentByte reports whether c can begin or continue an identifier
// or number in the output: ASCII identifier bytes, every byte of a
// non-ASCII letter (whitespace runes are consumed before emit), and
// the backslash of a `\u` escape. Two such bytes must never touch.
func isIdentByte(c byte) bool { return isIdentCont(c) || c >= utf8.RuneSelf || c == '\\' }

// jsSpace classifies the rune at src[i] when it is whitespace or a
// line terminator, returning its byte width (0 = neither).
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

// isLineTerminator reports whether src[i] starts LF, CR, U+2028 or
// U+2029.
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
	// A trailing deferred ';'/',' at EOF is kept verbatim: dropping it
	// is usually safe but concatenation contexts (inline <script>
	// assembly) may rely on the terminator.
	if m.pending != 0 {
		m.out.WriteByte(m.pending)
		m.pending = 0
	}
}

// step consumes the next thing in the input: whitespace, a comment,
// or one token. Whitespace + comments are absorbed silently (only
// their newline-ness is recorded); tokens are emitted via the
// emit* helpers, which call emitSep first to decide on separator.
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

// prevAllowsRegex returns true when a `/` at the current position
// should be lexed as the start of a regex literal. See the comment on
// regexKeywords for the keyword set (a property name is never a
// keyword); otherwise: after a punct other than `]`, a `)` that does
// not close a control head, or a `}` that closes an expression, OR at
// the very start of input. A `}` of unknown kind leans regex: a
// division misread as a regex is copied verbatim, while a regex
// misread as division gets its body re-tokenized.
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

// endsExpr reports whether the last emitted token completes an
// expression, which makes a following newline ASI-relevant when the
// next token starts with an ambiguous prefix character.
func (m *minifier) endsExpr() bool {
	if m.lastWasIncDec {
		return true
	}
	switch m.lastKind {
	case tkIdent:
		if m.lastProp || m.inClassBody() {
			return true // a property or class element name, never a keyword
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

// isASIHazardPrefix lists the bytes that, when starting a new line,
// can be interpreted as continuation of the previous expression
// instead of the start of a new statement. Keeping the newline alive
// preserves ASI.
func isASIHazardPrefix(c byte) bool {
	switch c {
	// '*' opens a generator method after an initializer-less class
	// field (`a\n*g(){}`); a newline before a continuing `*` costs a byte.
	case '(', '[', '+', '-', '/', '`', '!', '~', '{', '\'', '"', '#', '*':
		return true
	}
	return false
}

// fusionRisk reports whether the last token (of kind lastKind, ending
// in byte last) and a next token starting with first would form a
// different token than they did with whitespace between them.
func fusionRisk(lastKind tokKind, last, first byte) bool {
	wordy := lastKind == tkIdent || lastKind == tkNumber || lastKind == tkRegex
	if wordy && isIdentByte(first) {
		return true // `var é`, `/a/ in`, `typeof \u0061`
	}
	if lastKind == tkNumber && first == '.' {
		return true // `1 .toString()`
	}
	if last == '<' && first == '!' {
		return true // `a < !--b` must not open an HTML comment `<!--`
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

// emitSep writes whatever separator (if any) is needed between the
// last-emitted token and the next chunk starting with firstByte.
// Resets the whitespace-seen flags afterward.
func (m *minifier) emitSep(firstByte byte) {
	defer func() {
		m.sawNewline = false
		m.sawSpace = false
	}()
	// Flush (or drop) a deferred ';'/',' now that the next token is
	// known. Dropping before '}' is always safe in valid JS. See the
	// package comment.
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
		// After an expression-ending token, a newline before an
		// identifier-start or digit MUST survive: dropping it either
		// fuses tokens (`foo bar`, `1 2`) or yields a SyntaxError
		// (`a++b`, `)b`). Covers postfix ++/-- ASI (`a++\nb`) and
		// class-body element separation (`class A{x=1\ny=2}`), a
		// space is NOT a valid separator in either position.
		if m.endsExpr() && (isASIHazardPrefix(firstByte) || isIdentByte(firstByte)) {
			m.out.WriteByte('\n')
			return
		}
	}
	// Only insert a fusion-guard space when the original source had
	// whitespace between these two tokens. Adjacent characters that
	// were never separated cannot fuse into a different token by
	// definition, and forcing a space there would break `i++` into
	// `i+ +`, regex flags into `/x/ g`, and so on.
	if fusionRisk(m.lastKind, m.lastByte, firstByte) && m.sawSpace {
		m.out.WriteByte(' ')
	}
}

func (m *minifier) emitIdent() {
	start := m.pos
	m.pos = scanIdent(m.src, m.pos)
	ident := m.src[start:m.pos]
	// Read every decision off the PREVIOUS token before emitSep resets
	// the whitespace flags and the state below is overwritten.
	prop := m.hasEmitted && m.lastKind == tkPunct && m.lastByte == '.'
	kw := ident // the keyword this identifier spells, "" for a property name
	if prop {
		kw = ""
	}
	stmt := m.atStatementStart()
	afterAsync := m.lastKind == tkIdent && !m.lastProp && m.lastIdent == "async"
	ctrl := controlKeywords[kw] || (kw == "await" && m.ctrlPending && m.lastIdent == "for")
	if kw == "const" && m.constIsDeclaration() && m.nextStartsBinding() {
		ident = "let" // see the package comment, safe for any working program
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
		// The function's name: its parameter list is still to come.
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

// scanIdent returns the end of the identifier starting at src[i]:
// ASCII identifier bytes, non-ASCII letters (any rune that is not
// whitespace or a line terminator), and `\uXXXX` / `\u{X…}` escapes.
// It always consumes at least one byte.
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

// atStatementStart reports whether the next token begins a statement,
// which makes a `function` or `class` keyword a declaration (its body
// `}` then needs no ASI newline) rather than an expression.
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

// constIsDeclaration reports whether a `const` here can be a
// declaration keyword: never directly inside a class body (`const` is
// then a field or method name) or an object literal (a key).
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

// openBraceKind classifies a `{` about to be emitted, from the token
// before it. Unsure cases lean object/expression: that keeps an extra
// newline after the `}` (safe) instead of dropping one ASI needs.
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
			return bkBlock // control head, declaration or method parameters
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

// nextStartsBinding peeks (without consuming) past whitespace and
// comments for the next significant byte and reports whether it can
// start a declaration binding: an identifier, `{`, or `[`. Anything
// else (`:` object key, `=` class field, `(` accessor, EOF) means the
// preceding `const` was NOT a declaration keyword.
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
	// Hex / octal / binary prefixes: 0x.. 0o.. 0b..
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
	// Decimal: digits, '.', exponent, BigInt 'n' suffix, '_' separators.
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
		// `.5` starts a number, which ASI separates like a digit: `e\n.5`
		// minified to `e.5` is a SyntaxError.
		first = '0'
	}
	m.emitSep(first)
	m.out.WriteString(num)
	m.setLast(tkNumber, num[len(num)-1])
}

// setLast records a just-emitted literal (number, string, template,
// regex) as the previous token. A literal ends every keyword-driven
// expectation (a pending control head, function parameter list, arrow).
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
	m.pos++ // opening quote
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

// emitTemplate handles a backtick template literal. The literal text
// is copied verbatim (whitespace is significant inside backticks), but
// each `${...}` expression is recursively minified.
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

// minifyTemplateExpr runs the standard token loop with a brace-depth
// counter; exits (and emits the closing `}`) when depth returns to 0.
// On entry we've just written `${`. On exit, m.pos points one past
// the matching `}`.
func (m *minifier) minifyTemplateExpr() {
	saved := *m
	// Reset emit state so the first token inside ${...} doesn't get a
	// stale separator (we've just written `${`, which acts like an
	// opener). It reads as `(`: an expression comes next, so a `{`
	// there opens an object literal. The expression gets fresh brace
	// and paren stacks (restored below with the rest of the state).
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
				m.pending = 0 // deferred ';'/',' before the closing '}' is redundant
				m.out.WriteByte('}')
				break
			}
			m.emitPunct()
			continue
		}
		m.step()
	}

	// Restore the OUTSIDE state. From the template's POV, all that
	// happened is we wrote one logical "expression block" between `${`
	// and `}`. The next thing emitted will be more template text
	// (verbatim) or the closing backtick, neither cares about prev-token
	// classification, so restoring saved is the correct stance.
	pos := m.pos
	out := m.out
	*m = saved
	m.pos = pos
	m.out = out
}

func (m *minifier) emitRegex() {
	start := m.pos
	m.pos++ // opening '/'
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
			// A regex literal cannot contain a line terminator. We
			// misclassified: emit `/` as punct and resume AFTER it
			// (re-reading the same `/` would loop forever).
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
	// Flags.
	for m.pos < len(m.src) && isIdentCont(m.src[m.pos]) {
		m.pos++
	}
	tok := m.src[start:m.pos]
	m.emitSep(tok[0])
	m.out.WriteString(tok)
	m.setLast(tkRegex, tok[len(tok)-1])
}

// emitPunct emits exactly one byte of punctuation. Multi-char
// operators in the source (`===`, `=>`, `**=`, …) come through as
// consecutive single-byte emits with no whitespace between them, so
// the output is byte-identical to the source for any compound op.
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
	// Decisions that read the previous token, made before emitSep
	// resets the whitespace flags.
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
		m.pending = c // written (or dropped) when the next token arrives
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
		// A case label, or a statement label (`foo: {}`): either way
		// the `{` after it is a block, not an object literal.
		colonCase = m.casePending || (m.lastKind == tkIdent && m.lastIdentStmt && !m.inClassBody())
		m.casePending = false
	}
	if c != '*' { // `function*` keeps its parameter list pending
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

// semiMayBeEmptyStatement reports whether a `;` about to be emitted
// may be an empty statement, which must survive even before `}`:
//
//   - after `)`: an if/while/for body (`if(x);}`); a scanner can't
//     tell that apart from a droppable `g();}` (both end `);}`), so a
//     `;` after `)` is always written. The same rule keeps the do-while
//     terminator (`do{}while(x);}`).
//   - after `else` (`else;}`) or a label's `:` (`foo:;}`).
func (m *minifier) semiMayBeEmptyStatement() bool {
	switch m.lastKind {
	case tkPunct:
		return m.lastByte == ')' || m.lastByte == ':'
	case tkIdent:
		return !m.lastProp && m.lastIdent == "else"
	}
	return false
}
