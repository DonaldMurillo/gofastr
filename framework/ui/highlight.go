package ui

// Lightweight syntax highlighter for fenced code blocks. It is deliberately
// small — a single-pass scanner, not a full grammar — but robust enough not to
// mis-tokenize the common cases (it understands string/comment boundaries, so a
// `//` inside a string is not treated as a comment). It emits per-line
// []render.HTML where each token is an HTML-escaped <span class="tk-*">,
// matching the token palette the site theme already styles
// (.tk-kw/.tk-fn/.tk-str/.tk-num/.tk-com/.tk-type/.tk-pn → var(--tk-*)).
//
// Go gets keyword/type/builtin/function awareness. Other languages get a
// generic pass (comments + strings + numbers), which covers the visually
// dominant tokens for JS/TS/SQL/JSON/YAML/shell without per-grammar code.
// Unknown languages fall back to plain (escaped, untokenized) text — still
// rendered through ui.CodeBlock so they keep the chrome + copy button.

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// hlToken is one classified run of source text. Class is "" for plain text.
type hlToken struct {
	class string // tk-kw, tk-str, … or "" for plain
	text  string // raw (un-escaped) source slice
}

// highlightLines tokenizes code for the given language and returns one
// []render.HTML per source line (newline-split AFTER tokenizing, so multi-line
// strings/comments keep their class across the break). Each entry is the line's
// concatenated token spans, ready to pass as ui.CodeBlockConfig.Lines.
func highlightLines(code, lang string) []render.HTML {
	tokens := tokenize(code, normalizeLang(lang))

	// Split tokens on newlines into logical lines.
	var lines []render.HTML
	var b strings.Builder
	flush := func() {
		lines = append(lines, render.HTML(b.String()))
		b.Reset()
	}
	emit := func(class, text string) {
		esc := escapeHTML(text)
		if class == "" {
			b.WriteString(esc)
			return
		}
		b.WriteString(`<span class="`)
		b.WriteString(class)
		b.WriteString(`">`)
		b.WriteString(esc)
		b.WriteString(`</span>`)
	}
	for _, t := range tokens {
		// A token may itself contain newlines (raw strings, block comments).
		parts := strings.Split(t.text, "\n")
		for i, p := range parts {
			if i > 0 {
				flush()
			}
			if p != "" {
				emit(t.class, p)
			}
		}
	}
	flush() // trailing line (also handles single-line input)
	// A trailing newline in the source yields a final empty line; drop it so
	// the block doesn't render a blank last row.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

func normalizeLang(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "go", "golang":
		return "go"
	case "":
		return "plain"
	default:
		return "generic"
	}
}

var goKeywords = newSet(
	"break", "case", "chan", "const", "continue", "default", "defer", "else",
	"fallthrough", "for", "func", "go", "goto", "if", "import", "interface",
	"map", "package", "range", "return", "select", "struct", "switch", "type",
	"var",
)

var goBuiltins = newSet(
	"append", "cap", "close", "complex", "copy", "delete", "imag", "len",
	"make", "new", "panic", "print", "println", "real", "recover",
	"true", "false", "nil", "iota",
)

var goTypes = newSet(
	"bool", "byte", "complex64", "complex128", "error", "float32", "float64",
	"int", "int8", "int16", "int32", "int64", "rune", "string", "uint",
	"uint8", "uint16", "uint32", "uint64", "uintptr", "any",
)

func newSet(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// tokenize is the single-pass scanner. lang is normalized ("go"/"generic"/"plain").
func tokenize(src, lang string) []hlToken {
	if lang == "plain" {
		return []hlToken{{"", src}}
	}
	var toks []hlToken
	add := func(class, text string) {
		if text != "" {
			toks = append(toks, hlToken{class, text})
		}
	}
	i, n := 0, len(src)
	for i < n {
		c := src[i]
		switch {
		// Line comment: // (all) or # (generic only — Go has no # comments).
		case c == '/' && i+1 < n && src[i+1] == '/':
			j := i + 2
			for j < n && src[j] != '\n' {
				j++
			}
			add("tk-com", src[i:j])
			i = j
		case c == '#' && lang == "generic":
			j := i + 1
			for j < n && src[j] != '\n' {
				j++
			}
			add("tk-com", src[i:j])
			i = j
		// Block comment: /* ... */
		case c == '/' && i+1 < n && src[i+1] == '*':
			j := i + 2
			for j < n && !(src[j] == '*' && j+1 < n && src[j+1] == '/') {
				j++
			}
			if j < n {
				j += 2 // consume the closing */
			} else {
				j = n
			}
			add("tk-com", src[i:j])
			i = j
		// Strings: "double", `raw`, 'rune/char'.
		case c == '"' || c == '\'':
			j := scanQuoted(src, i, c, true)
			add("tk-str", src[i:j])
			i = j
		case c == '`':
			j := scanQuoted(src, i, c, false) // no escapes in raw strings
			add("tk-str", src[i:j])
			i = j
		// Numbers (incl. hex/float/exponent — loose but fine for display).
		case c >= '0' && c <= '9':
			j := i + 1
			for j < n && isNumChar(src[j]) {
				j++
			}
			add("tk-num", src[i:j])
			i = j
		// Identifiers / keywords.
		case isIdentStart(c):
			j := i + 1
			for j < n && isIdentPart(src[j]) {
				j++
			}
			word := src[i:j]
			class := ""
			if lang == "go" {
				switch {
				case goKeywords[word]:
					class = "tk-kw"
				case goBuiltins[word]:
					class = "tk-kw"
				case goTypes[word]:
					class = "tk-type"
				case j < n && src[j] == '(':
					class = "tk-fn" // call site
				}
			}
			add(class, word)
			i = j
		default:
			// Run of non-token bytes (whitespace, punctuation). Coalesce so we
			// don't emit a span per character.
			j := i + 1
			for j < n && isPlainRun(src[j], lang) {
				j++
			}
			add("", src[i:j])
			i = j
		}
	}
	return toks
}

// scanQuoted returns the index just past a quote run starting at i with the
// given delimiter. When allowEsc, a backslash escapes the next byte.
func scanQuoted(src string, i int, delim byte, allowEsc bool) int {
	n := len(src)
	j := i + 1
	for j < n {
		if allowEsc && src[j] == '\\' && j+1 < n {
			j += 2
			continue
		}
		if src[j] == delim {
			return j + 1
		}
		// Interpreted strings/runes don't span raw newlines; bail to avoid
		// swallowing the rest of the file on an unterminated quote.
		if allowEsc && src[j] == '\n' {
			return j
		}
		j++
	}
	return n
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isIdentPart(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') }
func isNumChar(c byte) bool {
	return (c >= '0' && c <= '9') || c == '.' || c == 'x' || c == 'X' || c == '_' ||
		(c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// isPlainRun reports whether c continues a plain (untokenized) run — i.e. it is
// NOT the start of a token the scanner recognizes.
func isPlainRun(c byte, lang string) bool {
	if isIdentStart(c) || (c >= '0' && c <= '9') {
		return false
	}
	switch c {
	case '"', '\'', '`', '/':
		return false
	case '#':
		return lang != "generic"
	}
	return true
}
