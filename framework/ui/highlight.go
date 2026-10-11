package ui

// Lightweight syntax highlighter for code blocks. It is deliberately
// small, a single-pass scanner, not a full grammar, but correct on the
// common cases (it understands string/comment boundaries, so a `//`
// inside a string is not treated as a comment). It emits per-line
// []render.HTML where each token is an HTML-escaped <span class="tk-*">,
// the kit's token classes: ui.CodeBlock's stylesheet colors them from
// the theme's Code slots (.tk-kw/.tk-fn/.tk-str/.tk-num/.tk-com/.tk-type/
// .tk-pn → var(--tk-*)).
//
// Language families:
//
//   - go: keywords, builtins, predeclared types, call sites, and named
//     types in type position (after `type`, a composite literal `Pong{`,
//     behind `*`/`[]`, package-qualified or not).
//   - js/ts: keywords, a few built-in types, call sites, object keys.
//   - sql: case-insensitive keywords and column types, `--` comments.
//   - shell (sh/bash/console): keywords, the command word of each line
//     or pipeline stage, `#` comments.
//   - yaml / json: mapping keys, true/false/null.
//   - anything else non-empty: a generic pass (comments + strings +
//     numbers), which covers the visually dominant tokens.
//   - "" / text / plain: escaped, untokenized text.
//
// Every family still renders through ui.CodeBlock, so all of them keep
// the chrome and the copy button.

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// hlToken is one classified run of source text. Class is "" for plain text.
type hlToken struct {
	class string // tk-kw, tk-str, … or "" for plain
	text  string // raw (un-escaped) source slice
}

// HighlightLines tokenizes code for the given language and returns one
// []render.HTML per source line (newline-split AFTER tokenizing, so multi-line
// strings/comments keep their class across the break). Each entry is the line's
// concatenated token spans, ready to pass as ui.CodeBlockConfig.Lines. Pass the
// result as CodeBlockConfig.Lines with ShowCopy/LineNumbers/Scroll as desired.
func HighlightLines(code, lang string) []render.HTML {
	tokens := tokenize(code, normalizeLang(lang))

	// Split tokens on newlines into logical lines.
	var lines []render.HTML
	var b strings.Builder
	flush := func() {
		lines = append(lines, render.HTML(b.String()))
		b.Reset()
	}
	emit := func(class, text string) {
		esc := render.Escape(text)
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

// Language families the scanner knows. Each family picks its comment
// syntax, its keyword set, and whether mapping keys are classed.
const (
	langPlain   = "plain"
	langGo      = "go"
	langJS      = "js"
	langSQL     = "sql"
	langShell   = "shell"
	langYAML    = "yaml"
	langJSON    = "json"
	langGeneric = "generic"
)

func normalizeLang(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "go", "golang":
		return langGo
	case "js", "javascript", "jsx", "mjs", "ts", "typescript", "tsx":
		return langJS
	case "sql", "postgres", "postgresql", "sqlite", "psql":
		return langSQL
	case "sh", "bash", "shell", "zsh", "console", "shellsession":
		return langShell
	case "yaml", "yml":
		return langYAML
	case "json", "jsonc", "json5":
		return langJSON
	case "", "text", "txt", "plain", "plaintext":
		return langPlain
	default:
		return langGeneric
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

var jsKeywords = newSet(
	"async", "await", "break", "case", "catch", "class", "const", "continue",
	"default", "delete", "do", "else", "export", "extends", "finally", "for",
	"from", "function", "if", "import", "in", "instanceof", "interface", "let",
	"new", "of", "return", "static", "switch", "this", "throw", "try", "type",
	"typeof", "var", "void", "while", "yield", "true", "false", "null",
	"undefined",
)

var jsTypes = newSet(
	"string", "number", "boolean", "any", "unknown", "never", "object",
	"Promise", "Record", "Array",
)

// sqlKeywords and sqlTypes match case-insensitively: the scanner
// lowercases the (ASCII-only) identifier before the lookup.
var sqlKeywords = newSet(
	"select", "from", "where", "and", "or", "not", "insert", "into", "values",
	"update", "set", "delete", "create", "table", "index", "unique", "on",
	"primary", "key", "references", "foreign", "default", "null", "is", "in",
	"as", "join", "left", "right", "inner", "outer", "group", "by", "order",
	"limit", "offset", "having", "returning", "alter", "add", "drop", "if",
	"exists", "begin", "commit", "rollback", "with", "distinct", "case",
	"when", "then", "else", "end", "asc", "desc", "true", "false", "like",
	"between", "conflict", "do", "nothing", "cascade", "constraint", "check",
	"view", "trigger", "using", "union", "all",
)

var sqlTypes = newSet(
	"integer", "int", "bigint", "smallint", "text", "varchar", "char",
	"boolean", "bool", "real", "numeric", "decimal", "timestamp",
	"timestamptz", "date", "time", "uuid", "jsonb", "json", "blob", "bytea",
	"serial", "bigserial",
)

var shellKeywords = newSet(
	"if", "then", "else", "elif", "fi", "for", "in", "do", "done", "while",
	"case", "esac", "function", "export", "local", "return", "set", "unset",
)

func newSet(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// hashComments reports whether # starts a line comment in the family.
func hashComments(lang string) bool {
	return lang == langShell || lang == langYAML || lang == langGeneric
}

// slashComments reports whether // and /* */ are comments in the family.
func slashComments(lang string) bool {
	return lang != langSQL && lang != langShell && lang != langYAML
}

// tokenize is the single-pass scanner. lang is a normalized family.
func tokenize(src, lang string) []hlToken {
	if lang == langPlain {
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
		// Line comments: // (C-like families), # (hash families), -- (SQL).
		case c == '/' && i+1 < n && src[i+1] == '/' && slashComments(lang):
			j := lineEnd(src, i)
			add("tk-com", src[i:j])
			i = j
		case c == '#' && hashComments(lang) && hashStartsComment(src, i, lang):
			j := lineEnd(src, i)
			add("tk-com", src[i:j])
			i = j
		case c == '-' && lang == langSQL && i+1 < n && src[i+1] == '-':
			j := lineEnd(src, i)
			add("tk-com", src[i:j])
			i = j
		// Block comment: /* ... */
		case c == '/' && i+1 < n && src[i+1] == '*' && (slashComments(lang) || lang == langSQL):
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
		// Strings: "double" and 'rune/char/single'.
		case c == '"' || (c == '\'' && quoteOpensString(src, i, lang)):
			j := scanQuoted(src, i, c, true)
			class := "tk-str"
			if isMappingKey(src, i, j, lang) {
				class = "tk-fn" // a JSON / YAML / JS object key
			}
			add(class, src[i:j])
			i = j
		// Raw strings / template literals / shell command substitution.
		case c == '`' && (lang == langGo || lang == langJS || lang == langShell || lang == langGeneric):
			j := scanQuoted(src, i, c, false) // no escapes in raw strings
			add("tk-str", src[i:j])
			i = j
		// Numbers (incl. hex/float/exponent, loose but fine for display).
		case c >= '0' && c <= '9':
			j := i + 1
			for j < n && isNumChar(src[j]) {
				j++
			}
			add("tk-num", src[i:j])
			i = j
		// Identifiers / keywords. YAML keys and shell flags may carry
		// inner hyphens (max-age, --dry-run's dry-run).
		case isIdentStart(c):
			j := i + 1
			for j < n && (isIdentPart(src[j]) || (src[j] == '-' && hyphenIdents(lang) && j+1 < n && isIdentPart(src[j+1]))) {
				j++
			}
			word := src[i:j]
			add(classifyWord(src, i, j, word, lang, toks), word)
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

func hyphenIdents(lang string) bool { return lang == langYAML || lang == langShell }

// classifyWord returns the token class for the identifier src[i:j].
// prev is the token list so far (its last entry precedes the word).
func classifyWord(src string, i, j int, word, lang string, prev []hlToken) string {
	callSite := j < len(src) && src[j] == '('
	switch lang {
	case langGo:
		switch {
		case goKeywords[word], goBuiltins[word]:
			return "tk-kw"
		case goTypes[word]:
			return "tk-type"
		case callSite:
			return "tk-fn"
		case goTypePosition(src, i, j, word, prev):
			return "tk-type"
		}
	case langJS:
		switch {
		case jsKeywords[word]:
			return "tk-kw"
		case jsTypes[word]:
			return "tk-type"
		case callSite:
			return "tk-fn"
		case isMappingKey(src, i, j, lang):
			return "tk-fn"
		case isUpperASCII(word[0]) && (prevWord(prev) == "new" || prevWord(prev) == "class" || prevWord(prev) == "extends"):
			return "tk-type"
		}
	case langSQL:
		lw := strings.ToLower(word) // word is ASCII: isIdentStart/Part are ASCII-only
		switch {
		case sqlKeywords[lw]:
			return "tk-kw"
		case sqlTypes[lw]:
			return "tk-type"
		case callSite:
			return "tk-fn"
		}
	case langShell:
		switch {
		case shellKeywords[word]:
			return "tk-kw"
		case atCommandStart(src, i):
			return "tk-fn" // the command being run
		}
	case langYAML:
		switch {
		case isMappingKey(src, i, j, lang):
			return "tk-fn"
		case word == "true" || word == "false" || word == "null":
			return "tk-kw"
		}
	case langJSON:
		if word == "true" || word == "false" || word == "null" {
			return "tk-kw"
		}
	}
	return ""
}

// goTypePosition reports whether a Go identifier sits where a named type
// does: right after the `type` keyword, as an exported name opening a
// composite literal (`Pong{`), or behind a pointer / slice marker,
// optionally package-qualified (`*http.Request`, `[]schema.Field`).
func goTypePosition(src string, i, j int, word string, prev []hlToken) bool {
	if prevWord(prev) == "type" {
		return true
	}
	if !isUpperASCII(word[0]) {
		return false
	}
	if j < len(src) && src[j] == '{' {
		return true
	}
	// Step back over a package qualifier: `pkg.`.
	k := i
	if k > 0 && src[k-1] == '.' {
		k--
		for k > 0 && isIdentPart(src[k-1]) {
			k--
		}
	}
	return k > 0 && (src[k-1] == '*' || src[k-1] == ']')
}

// prevWord returns the text of the token before the current position when
// only same-line blanks separate them, so `type Pong` yields "type".
func prevWord(prev []hlToken) string {
	if len(prev) < 2 {
		return ""
	}
	gap := prev[len(prev)-1].text
	if strings.Trim(gap, " \t") != "" {
		return ""
	}
	return prev[len(prev)-2].text
}

// isMappingKey reports whether the token src[i:j] is a mapping key: it is
// followed by a colon, and (YAML) starts its line after indentation and an
// optional "- " list marker, or (JS) follows `{`, `,` or a line start, so
// a ternary's `a ? b : c` branch is not a key. Every JSON string followed
// by a colon is a key.
func isMappingKey(src string, i, j int, lang string) bool {
	if lang != langJSON && lang != langYAML && lang != langJS {
		return false
	}
	k := j
	for k < len(src) && (src[k] == ' ' || src[k] == '\t') {
		k++
	}
	if k >= len(src) || src[k] != ':' {
		return false
	}
	if lang == langJSON {
		return true
	}
	b := skipBlanksBack(src, i-1)
	if lang == langYAML {
		if b >= 0 && src[b] == '-' {
			b = skipBlanksBack(src, b-1)
		}
		return b < 0 || src[b] == '\n'
	}
	return b < 0 || src[b] == '\n' || src[b] == '{' || src[b] == ','
}

// skipBlanksBack returns the index of the last non-space, non-tab byte at
// or before b, or -1.
func skipBlanksBack(src string, b int) int {
	for b >= 0 && (src[b] == ' ' || src[b] == '\t') {
		b--
	}
	return b
}

// quoteOpensString reports whether a single quote starts a string. In
// YAML and the generic family an apostrophe inside prose (don't) is not
// a string, so there the quote must not follow a word character.
func quoteOpensString(src string, i int, lang string) bool {
	switch lang {
	case langGo, langJS, langSQL, langShell, langJSON:
		return true
	}
	return i == 0 || !isIdentPart(src[i-1])
}

// hashStartsComment reports whether # opens a comment: in shell and YAML
// only at a word boundary (`a#b` and `${#x}` are not comments).
func hashStartsComment(src string, i int, lang string) bool {
	if lang == langGeneric || i == 0 {
		return true
	}
	p := src[i-1]
	return p == ' ' || p == '\t' || p == '\n'
}

// atCommandStart reports whether a shell word is the first word of its
// line (after indentation and an optional `$ ` prompt) or follows a
// pipe / && / || / ;, i.e. it names the command being run.
func atCommandStart(src string, i int) bool {
	b := skipBlanksBack(src, i-1)
	if b >= 0 && src[b] == '$' {
		b = skipBlanksBack(src, b-1)
	}
	return b < 0 || src[b] == '\n' || src[b] == '|' || src[b] == '&' || src[b] == ';'
}

func lineEnd(src string, i int) int {
	j := i
	for j < len(src) && src[j] != '\n' {
		j++
	}
	return j
}

func isUpperASCII(c byte) bool { return c >= 'A' && c <= 'Z' }

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

// isPlainRun reports whether c continues a plain (untokenized) run, i.e. it
// could NOT start a token the scanner recognizes. A byte that turns out not
// to start a token after all (an apostrophe in prose, a lone /) still ends
// the run; the scanner's default arm then consumes it as plain text.
func isPlainRun(c byte, lang string) bool {
	if isIdentStart(c) || (c >= '0' && c <= '9') {
		return false
	}
	switch c {
	case '"', '\'', '`', '/':
		return false
	case '#':
		return !hashComments(lang)
	case '-':
		return lang != langSQL
	}
	return true
}
