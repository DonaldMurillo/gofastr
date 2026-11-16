package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// hlToken is one classified run of raw source text; class is "" for plain text.
type hlToken struct {
	class string
	text  string
}

// HighlightLines tokenizes code for lang and returns one escaped line of
// <span class="tk-*"> tokens per source line, for CodeBlockConfig.Lines.
func HighlightLines(code, lang string) []render.HTML {
	tokens := tokenize(code, normalizeLang(lang))

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
	flush()
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

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
		case c == '/' && i+1 < n && src[i+1] == '*' && (slashComments(lang) || lang == langSQL):
			j := i + 2
			for j < n && !(src[j] == '*' && j+1 < n && src[j+1] == '/') {
				j++
			}
			if j < n {
				j += 2
			} else {
				j = n
			}
			add("tk-com", src[i:j])
			i = j
		case c == '"' || (c == '\'' && quoteOpensString(src, i, lang)):
			j := scanQuoted(src, i, c, true)
			class := "tk-str"
			if isMappingKey(src, i, j, lang) {
				class = "tk-fn"
			}
			add(class, src[i:j])
			i = j
		case c == '`' && (lang == langGo || lang == langJS || lang == langShell || lang == langGeneric):
			j := scanQuoted(src, i, c, false)
			add("tk-str", src[i:j])
			i = j
		case c >= '0' && c <= '9':
			j := i + 1
			for j < n && isNumChar(src[j]) {
				j++
			}
			add("tk-num", src[i:j])
			i = j
		case isIdentStart(c):
			j := i + 1
			for j < n && (isIdentPart(src[j]) || (src[j] == '-' && hyphenIdents(lang) && j+1 < n && isIdentPart(src[j+1]))) {
				j++
			}
			word := src[i:j]
			add(classifyWord(src, i, j, word, lang, toks), word)
			i = j
		default:
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
		lw := strings.ToLower(word)
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
			return "tk-fn"
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

// goTypePosition reports whether a Go identifier follows `type`, opens
// an exported composite literal, or follows * or ], qualified or not.
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
	k := i
	if k > 0 && src[k-1] == '.' {
		k--
		for k > 0 && isIdentPart(src[k-1]) {
			k--
		}
	}
	return k > 0 && (src[k-1] == '*' || src[k-1] == ']')
}

// prevWord returns the previous token's text when only same-line blanks separate it.
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

// isMappingKey reports whether src[i:j] is followed by a colon and
// starts a YAML line (after an optional "- "), follows {, , or a line
// start in JS, or is any JSON token.
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

// skipBlanksBack returns the last non-blank index at or before b, or -1.
func skipBlanksBack(src string, b int) int {
	for b >= 0 && (src[b] == ' ' || src[b] == '\t') {
		b--
	}
	return b
}

// quoteOpensString reports whether a single quote starts a string.
func quoteOpensString(src string, i int, lang string) bool {
	switch lang {
	case langGo, langJS, langSQL, langShell, langJSON:
		return true
	}
	return i == 0 || !isIdentPart(src[i-1])
}

// hashStartsComment reports whether # opens a comment.
func hashStartsComment(src string, i int, lang string) bool {
	if lang == langGeneric || i == 0 {
		return true
	}
	p := src[i-1]
	return p == ' ' || p == '\t' || p == '\n'
}

// atCommandStart reports whether a shell word names the command being run.
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

// scanQuoted returns the index just past the quote run starting at i.
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

// isPlainRun reports whether c cannot start a token, so it continues a plain run.
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
