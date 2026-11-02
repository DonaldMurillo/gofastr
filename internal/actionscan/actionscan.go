// Package actionscan answers one question for both embed gates: does a
// component's ClientJS call G.serverAction?
//
// G.serverAction is refused inside an embed frame, and two gates say so
// before a customer's page does: `gofastr build`'s check-embed analyzer
// (cmd/check-embed/embedcheck) and the boot walk at Mount
// (framework/uihost enforceNoServerActionsOnEmbeds). They read the same
// ClientJS and must give the same answer: when the boot walk matched the
// bytes "G.serverAction(" anywhere while check-embed read code, a
// mention in a comment or a string passed the build and panicked at
// boot, and a call written G.serverAction /* … */ ( passed the boot walk
// after failing the build. Both now call ServerActionCall.
//
// It reads code, not bytes. Comments, string literals, template text and
// regular-expression literals are skipped; a template literal's ${…} is
// read as code. The action compiler's rewrite is lexical (uihost
// actionsToJS replaces every "G.serverAction(" in the body), so the
// compiler also rewrites a mention inside a comment or a string, but
// what it produces there is still a comment or a string: no call ships.
// A string only becomes code through eval, new Function or a string
// passed to setTimeout, which the framework's default CSP refuses
// (script-src 'self', no 'unsafe-eval'); that, and a call assembled
// through a computed property or concatenation, stays outside this scan
// and outside the compiler alike.
package actionscan

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const name = "G.serverAction"

// ServerActionCall reports whether clientJS contains a call of
// G.serverAction outside comments, strings, template text and regular
// expression literals. Whitespace and comments between the callee and
// "(" still make a call: legal JavaScript the compiler cannot rewrite,
// which both gates reject rather than ship a call with no component id.
func ServerActionCall(clientJS string) bool {
	found, _ := scanCode(clientJS, 0, false)
	return found
}

// regexKeywords are the words after which a "/" starts a regular
// expression rather than a division.
var regexKeywords = map[string]bool{
	"return": true, "typeof": true, "instanceof": true, "in": true, "of": true,
	"new": true, "delete": true, "void": true, "throw": true, "case": true,
	"do": true, "else": true, "yield": true, "await": true,
}

// scanCode scans code from i. With inInterpolation it stops at the "}"
// that closes a template interpolation (braces nest) and returns the
// index after it; otherwise it runs to the end of src.
func scanCode(src string, i int, inInterpolation bool) (bool, int) {
	depth := 0
	// prev is the last significant byte outside comments (0 at the
	// start), and word the identifier or keyword that ended there, if
	// one did: together they tell a regular expression's "/" from a
	// division's.
	var prev byte
	word := ""
	for i < len(src) {
		if size := spaceLen(src[i:]); size > 0 {
			i += size
			continue
		}
		c := src[i]
		switch {
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			i = skipLineComment(src, i+2)
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i = skipBlockComment(src, i+2)
		case c == '/' && regexAllowed(prev, word):
			if end, ok := skipRegex(src, i+1); ok {
				i, prev, word = end, ')', ""
				continue
			}
			i, prev, word = i+1, c, ""
		case c == '`':
			found, end := scanTemplate(src, i+1)
			if found {
				return true, end
			}
			i, prev, word = end, '`', ""
		case c == '\'' || c == '"':
			i, prev, word = skipString(src, i+1, c), c, ""
		case inInterpolation && c == '{':
			depth++
			i, prev, word = i+1, c, ""
		case inInterpolation && c == '}':
			if depth == 0 {
				return false, i + 1
			}
			depth--
			i, prev, word = i+1, c, ""
		case strings.HasPrefix(src[i:], name) && (i == 0 || !isIdentByte(src[i-1])):
			after := i + len(name)
			if after < len(src) && isIdentByte(src[after]) {
				// G.serverActionFor and the like: another member.
				j := after
				for j < len(src) && isIdentByte(src[j]) {
					j++
				}
				i, prev, word = j, src[j-1], src[after-len("serverAction"):j]
				continue
			}
			after = skipTrivia(src, after)
			if after < len(src) && src[after] == '(' {
				return true, after
			}
			i, prev, word = after, 'n', "serverAction"
		case isIdentByte(c):
			j := i
			for j < len(src) && isIdentByte(src[j]) {
				j++
			}
			i, prev, word = j, src[j-1], src[i:j]
		default:
			i, prev, word = i+1, c, ""
		}
	}
	return false, i
}

// regexAllowed reports whether a "/" after prev (and word, when prev
// ended one) begins a regular expression: at the start, after an
// operator or an opening bracket, or after a keyword such as return. After
// a value (an identifier, a number, ")" or "]") it divides.
func regexAllowed(prev byte, word string) bool {
	if prev == 0 {
		return true
	}
	if word != "" {
		return regexKeywords[word]
	}
	return strings.IndexByte("(,=:[!&|?{};+-*%<>~^", prev) >= 0
}

// skipRegex skips a regular-expression literal from just after its
// opening "/": escapes, a character class (where "/" does not end it),
// then the flags. A line break first means it was not one.
func skipRegex(src string, i int) (int, bool) {
	inClass := false
	for i < len(src) {
		switch c := src[i]; {
		case c == '\\' && i+1 < len(src):
			i += 2
			continue
		case c == '\n' || c == '\r':
			return i, false
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '/' && !inClass:
			i++
			for i < len(src) && isIdentByte(src[i]) {
				i++
			}
			return i, true
		}
		i++
	}
	return i, false
}

// scanTemplate scans a template literal from just after its opening
// backtick, reading each ${...} interpolation as code, and returns the
// index after the closing backtick.
func scanTemplate(src string, i int) (bool, int) {
	for i < len(src) {
		switch {
		case src[i] == '\\' && i+1 < len(src):
			i += 2
		case src[i] == '`':
			return false, i + 1
		case src[i] == '$' && i+1 < len(src) && src[i+1] == '{':
			found, end := scanCode(src, i+2, true)
			if found {
				return true, end
			}
			i = end
		default:
			i++
		}
	}
	return false, i
}

func skipTrivia(src string, i int) int {
	for i < len(src) {
		if size := spaceLen(src[i:]); size > 0 {
			i += size
			continue
		}
		switch {
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '/':
			i = skipLineComment(src, i+2)
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			i = skipBlockComment(src, i+2)
		default:
			return i
		}
	}
	return i
}

func skipLineComment(src string, i int) int {
	for i < len(src) && src[i] != '\n' && src[i] != '\r' {
		i++
	}
	return i
}

func skipBlockComment(src string, i int) int {
	for i+1 < len(src) {
		if src[i] == '*' && src[i+1] == '/' {
			return i + 2
		}
		i++
	}
	return len(src)
}

func skipString(src string, i int, quote byte) int {
	for i < len(src) {
		if src[i] == '\\' && i+1 < len(src) {
			i += 2
			continue
		}
		if src[i] == quote {
			return i + 1
		}
		i++
	}
	return len(src)
}

func spaceLen(src string) int {
	r, size := utf8.DecodeRuneInString(src)
	if unicode.IsSpace(r) {
		return size
	}
	return 0
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' ||
		c >= 'a' && c <= 'z' ||
		c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9'
}
