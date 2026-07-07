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
//     `{const: 1}`, `class A{const=1}`) are left alone: the rewrite
//     applies only when the keyword follows a non-`.` token and the
//     next token starts a binding (identifier, `{`, or `[`).
//   - Drop a `;` or `,` immediately before a `}` (`a=1;}` → `a=1}`,
//     `{a:1,}` → `{a:1}`). In valid JS a `}` can only follow `;` as a
//     statement/class-element terminator (droppable) and `,` as a
//     trailing comma in an object literal / destructuring /
//     import-specifier list (droppable; array-hole commas sit before
//     `]`, which is never touched). Exception: a `;` whose previous
//     token is `)` is never dropped — it may be an empty statement
//     serving as an if/while/for body (`if(x);}` → `if(x)}` is a
//     SyntaxError), and a scanner can't tell that apart from `g();}`.
//     The same rule keeps the do-while terminator (`do{}while(x);}`).
//
// Out of scope (would be tier-3): identifier renaming, dead-code
// elimination, constant folding, anything that requires a parser.
//
// The output is intentionally still valid, readable-ish JavaScript —
// it stays parseable by the browser without source maps and a developer
// can still set breakpoints inside it.
package minify

import "strings"

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
	// written when the NEXT token arrives — unless that token starts
	// with '}', in which case it's redundant and dropped. State
	// (lastKind/lastByte) is updated at defer time, so separator and
	// regex-vs-division decisions behave exactly as if it were written.
	pending byte
}

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

func isIdentStart(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_' || c == '$'
}
func isIdentCont(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}
func isDigit(c byte) bool    { return c >= '0' && c <= '9' }
func isHexDigit(c byte) bool { return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') }

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
	switch {
	case c == ' ' || c == '\t' || c == '\r':
		m.sawSpace = true
		m.pos++
	case c == '\n':
		m.sawNewline = true
		m.sawSpace = true
		m.pos++
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
	case isIdentStart(c):
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
	for m.pos < len(m.src) && m.src[m.pos] != '\n' {
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
		if m.src[m.pos] == '\n' {
			m.sawNewline = true
		}
		m.pos++
	}
	m.sawSpace = true
}

// prevAllowsRegex returns true when a `/` at the current position
// should be lexed as the start of a regex literal. See the comment on
// regexKeywords for the keyword set; otherwise: after a punct other
// than `)`/`]`, OR at the very start of input.
func (m *minifier) prevAllowsRegex() bool {
	if !m.hasEmitted {
		return true
	}
	switch m.lastKind {
	case tkIdent:
		return regexKeywords[m.lastIdent]
	case tkNumber, tkString, tkRegex:
		return false
	case tkPunct:
		if m.lastByte == ')' || m.lastByte == ']' {
			return false
		}
		if m.lastWasIncDec {
			return false
		}
		return true
	}
	return true
}

// endsExpr reports whether the last emitted token completes an
// expression — which makes a following newline ASI-relevant when the
// next token starts with an ambiguous prefix character.
func (m *minifier) endsExpr() bool {
	if m.lastWasIncDec {
		return true
	}
	switch m.lastKind {
	case tkIdent:
		return !asiKeywords[m.lastIdent]
	case tkNumber, tkString, tkRegex:
		return true
	case tkPunct:
		return m.lastByte == ')' || m.lastByte == ']'
	}
	return false
}

// isASIHazardPrefix lists the bytes that, when starting a new line,
// can be interpreted as continuation of the previous expression
// instead of the start of a new statement. Keeping the newline alive
// preserves ASI.
func isASIHazardPrefix(c byte) bool {
	switch c {
	case '(', '[', '+', '-', '/', '`':
		return true
	}
	return false
}

// fusionRisk reports whether two adjacent bytes would form a different
// token than the two original tokens did with whitespace between them.
func fusionRisk(last, first byte) bool {
	if isIdentCont(last) && isIdentCont(first) {
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

// emitSep writes whatever separator (if any) is needed between the
// last-emitted token and the next chunk starting with firstByte.
// Resets the whitespace-seen flags afterward.
func (m *minifier) emitSep(firstByte byte) {
	defer func() {
		m.sawNewline = false
		m.sawSpace = false
	}()
	// Flush (or drop) a deferred ';'/',' now that the next token is
	// known. Dropping before '}' is always safe in valid JS — see the
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
		if m.lastKind == tkIdent && asiKeywords[m.lastIdent] {
			m.out.WriteByte('\n')
			return
		}
		// After an expression-ending token, a newline before an
		// identifier-start or digit MUST survive: dropping it either
		// fuses tokens (`foo bar`, `1 2`) or yields a SyntaxError
		// (`a++b`, `)b`). Covers postfix ++/-- ASI (`a++\nb`) and
		// class-body element separation (`class A{x=1\ny=2}`) — a
		// space is NOT a valid separator in either position.
		if m.endsExpr() && (isASIHazardPrefix(firstByte) || isIdentStart(firstByte) || isDigit(firstByte)) {
			m.out.WriteByte('\n')
			return
		}
	}
	// Only insert a fusion-guard space when the original source had
	// whitespace between these two tokens. Adjacent characters that
	// were never separated cannot fuse into a different token by
	// definition — and forcing a space there would break `i++` into
	// `i+ +`, regex flags into `/x/ g`, and so on.
	if fusionRisk(m.lastByte, firstByte) && m.sawSpace {
		m.out.WriteByte(' ')
	}
}

func (m *minifier) emitIdent() {
	start := m.pos
	for m.pos < len(m.src) && isIdentCont(m.src[m.pos]) {
		m.pos++
	}
	ident := m.src[start:m.pos]
	if ident == "const" && m.lastByte != '.' && m.nextStartsBinding() {
		ident = "let" // see the package comment — safe for any working program
	}
	m.emitSep(ident[0])
	m.out.WriteString(ident)
	m.lastKind = tkIdent
	m.lastByte = ident[len(ident)-1]
	m.lastIdent = ident
	m.lastWasIncDec = false
	m.hasEmitted = true
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
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '/' && i+1 < len(m.src) && m.src[i+1] == '/':
			for i < len(m.src) && m.src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(m.src) && m.src[i+1] == '*':
			i += 2
			for i+1 < len(m.src) && !(m.src[i] == '*' && m.src[i+1] == '/') {
				i++
			}
			i += 2
		default:
			return isIdentStart(c) || c == '{' || c == '['
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
	m.emitSep(num[0])
	m.out.WriteString(num)
	m.lastKind = tkNumber
	m.lastByte = num[len(num)-1]
	m.lastIdent = ""
	m.lastWasIncDec = false
	m.hasEmitted = true
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
	m.lastKind = tkString
	m.lastByte = quote
	m.lastIdent = ""
	m.lastWasIncDec = false
	m.hasEmitted = true
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
	m.lastKind = tkString
	m.lastByte = '`'
	m.lastIdent = ""
	m.lastWasIncDec = false
	m.hasEmitted = true
}

// minifyTemplateExpr runs the standard token loop with a brace-depth
// counter; exits (and emits the closing `}`) when depth returns to 0.
// On entry we've just written `${`. On exit, m.pos points one past
// the matching `}`.
func (m *minifier) minifyTemplateExpr() {
	saved := *m
	// Reset emit state so the first token inside ${...} doesn't get a
	// stale separator (we've just written `${`, which acts like an
	// opener).
	m.hasEmitted = true
	m.lastKind = tkPunct
	m.lastByte = '{'
	m.lastIdent = ""
	m.lastWasIncDec = false
	m.sawNewline = false
	m.sawSpace = false

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
	// (verbatim) or the closing backtick — neither cares about prev-token
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
		if c == '\n' {
			// A regex literal cannot contain a raw newline. We
			// misclassified — fall back to emitting `/` as punct.
			m.pos = start
			m.emitPunctChar('/')
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
	m.lastKind = tkRegex
	m.lastByte = tok[len(tok)-1]
	m.lastIdent = ""
	m.lastWasIncDec = false
	m.hasEmitted = true
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
	semiAfterParen := c == ';' && m.lastKind == tkPunct && m.lastByte == ')'
	m.emitSep(c)
	if semiAfterParen {
		// A `;` directly after `)` may be an empty statement serving
		// as an if/while/for body: dropping it before `}` would turn
		// `if(x);}` into `if(x)}` — a SyntaxError. A token scanner
		// can't tell that apart from a droppable `g();}` (both end
		// `);}`), so a `;` after `)` is always written immediately,
		// never deferred.
		m.out.WriteByte(c)
	} else if c == ';' || c == ',' {
		m.pending = c // written (or dropped) when the next token arrives
	} else {
		m.out.WriteByte(c)
	}
	m.lastKind = tkPunct
	m.lastByte = c
	m.lastIdent = ""
	m.lastWasIncDec = incDec
	m.hasEmitted = true
}
