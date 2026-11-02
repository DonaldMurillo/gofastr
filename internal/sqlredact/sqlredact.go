// Package sqlredact turns a SQL statement into its shape: every string
// and number literal replaced by ?, comments dropped, whitespace
// collapsed, control bytes escaped. It is the one masker behind the
// db.query.text attribute core/tracing puts on SQL spans and the sql
// attr of framework/slowquery's slow-query line, so a fix to how one
// engine quotes text reaches both.
//
// Where a literal ends depends on the engine, so the caller names it.
// Unknown reads the statement under every engine's rules at once and
// masks whatever any of them reads as a literal: it may hide an
// identifier, never a value.
package sqlredact

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Dialect is the engine whose quoting rules decide where a literal ends.
type Dialect int

const (
	// Unknown masks under every rule below at once: "double-quoted" text
	// is a string, # opens a comment, block comments nest, and a string
	// whose end depends on whether a backslash escapes masks the rest of
	// the statement.
	Unknown Dialect = iota
	// Postgres: "double-quoted" text is an identifier, a backslash
	// escapes only inside E'...', block comments nest, and
	// $tag$...$tag$ is a string.
	Postgres
	// SQLite reads "double-quoted" text as a string when no column has
	// that name, which the statement alone cannot rule out, so it is
	// masked.
	SQLite
	// MySQL (and MariaDB) read "double-quoted" text as a string, a
	// backslash as an escape in every string, and # as a line comment.
	MySQL
)

// ForSystem maps a database name, as an OpenTelemetry db.system.name
// value or a database/sql driver name, to its Dialect. A name it does
// not know is Unknown.
func ForSystem(system string) Dialect {
	switch asciicase.Lower(strings.TrimSpace(system)) {
	case "postgresql", "postgres", "pgx", "pgx/v5":
		return Postgres
	case "sqlite", "sqlite3":
		return SQLite
	case "mysql", "mariadb":
		return MySQL
	}
	return Unknown
}

type rules struct {
	dqString     bool // "double-quoted" text is a string
	backslash    bool // a backslash escapes in every string
	hashComment  bool // # opens a line comment
	nestComments bool // /* /* */ */ is one comment
	ambiguous    bool // a backslash may or may not escape: mask the rest when the readings disagree
}

func rulesFor(d Dialect) rules {
	switch d {
	case Postgres:
		return rules{nestComments: true}
	case SQLite:
		return rules{dqString: true}
	case MySQL:
		return rules{dqString: true, backslash: true, hashComment: true}
	}
	return rules{dqString: true, hashComment: true, nestComments: true, ambiguous: true}
}

// Text is query with its literals replaced by ?, comments dropped and
// whitespace collapsed: 'single-quoted' strings, $tag$dollar-quoted$tag$
// strings, and numbers that do not continue an identifier or a
// placeholder ($1, ?2, :n stay as written). Double-quoted names stay
// where d reads them as identifiers. Control bytes print escaped.
func Text(query string, d Dialect) string {
	r := rulesFor(d)
	var b strings.Builder
	b.Grow(len(query))
	space := false
	emit := func(s string) {
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteString(s)
	}
	for i := 0; i < len(query); {
		c := query[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			space = true
			i++
		case (c == '-' && i+1 < len(query) && query[i+1] == '-') || (r.hashComment && c == '#'):
			j := strings.IndexByte(query[i:], '\n')
			if j < 0 {
				i = len(query)
			} else {
				i += j
			}
			space = true
		case c == '/' && i+1 < len(query) && query[i+1] == '*':
			i = skipBlockComment(query, i, r.nestComments)
			space = true
		case c == '\'' || (r.dqString && c == '"'):
			eString := i > 0 && query[i-1]|0x20 == 'e' && (i == 1 || !continuesName(query[i-2]))
			switch {
			case r.ambiguous:
				plain, escaped := skipQuoted(query, i, false), skipQuoted(query, i, true)
				if plain != escaped {
					// One engine ends the string where another reads on:
					// whatever follows may be a value under one of them.
					i = len(query)
				} else {
					i = plain
				}
			default:
				i = skipQuoted(query, i, r.backslash || eString)
			}
			emit("?")
		case c == '"' || c == '`':
			j := i + 1
			for j < len(query) && query[j] != c {
				j++
			}
			if j < len(query) {
				j++
			}
			emit(query[i:j])
			i = j
		case c == '$' && dollarTag(query[i:]) != "":
			tag := dollarTag(query[i:])
			j := strings.Index(query[i+len(tag):], tag)
			if j < 0 {
				i = len(query)
			} else {
				i += len(tag) + j + len(tag)
			}
			emit("?")
		case isDigit(c) && (i == 0 || !continuesName(query[i-1])):
			j := i
			for j < len(query) && (isDigit(query[j]) || isASCIILetter(query[j]) || query[j] == '.' ||
				((query[j] == '+' || query[j] == '-') && (query[j-1] == 'e' || query[j-1] == 'E'))) {
				j++
			}
			emit("?")
			i = j
		default:
			j := i + 1
			for continuesName(c) && j < len(query) && continuesName(query[j]) {
				j++
			}
			emit(query[i:j])
			i = j
		}
	}
	return textsafe.ScrubControlBytes(b.String())
}

// skipBlockComment returns the index after the /* comment opening at i.
// With nest set, an inner /* opens a level a */ must close first, as
// PostgreSQL reads them. An unterminated comment runs to the end.
func skipBlockComment(q string, i int, nest bool) int {
	depth := 0
	for j := i; j+1 < len(q); {
		switch {
		case q[j] == '/' && q[j+1] == '*' && (nest || depth == 0):
			depth++
			j += 2
		case q[j] == '*' && q[j+1] == '/':
			depth--
			j += 2
			if depth == 0 {
				return j
			}
		default:
			j++
		}
	}
	return len(q)
}

// skipQuoted returns the index after the string opened by the quote at i,
// where a doubled quote is an escaped one and, with escapes set, so is
// any character after a backslash. An unterminated string runs to the end.
func skipQuoted(q string, i int, escapes bool) int {
	quote := q[i]
	for j := i + 1; j < len(q); j++ {
		if escapes && q[j] == '\\' {
			j++
			continue
		}
		if q[j] == quote {
			if j+1 < len(q) && q[j+1] == quote {
				j++
				continue
			}
			return j + 1
		}
	}
	return len(q)
}

// dollarTag returns the $tag$ opening a PostgreSQL dollar-quoted string
// at the start of s, or "" when s opens a $1 placeholder or nothing. A
// tag is an identifier, so a letter outside ASCII ($é$) counts.
func dollarTag(s string) string {
	for j := 1; j < len(s); j++ {
		switch c := s[j]; {
		case c == '$':
			return s[:j+1]
		case isASCIILetter(c) || c == '_' || c >= 0x80 || (j > 1 && isDigit(c)):
		default:
			return ""
		}
	}
	return ""
}

func isDigit(c byte) bool       { return c >= '0' && c <= '9' }
func isASCIILetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

// continuesName reports whether c can sit inside an identifier or a
// placeholder, so a digit after it is part of that token, not a number.
func continuesName(c byte) bool {
	return isASCIILetter(c) || isDigit(c) || c == '_' || c == '$' || c == '?' || c == ':' || c == '@' || c >= 0x80
}
