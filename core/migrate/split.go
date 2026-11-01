package migrate

import (
	"context"
	"strings"
)

// splitStatements splits a Postgres SQL body at its statement boundaries:
// each semicolon outside a quoted string ('…', E'…' with backslash
// escapes, "…"), a dollar-quoted body ($$…$$, $tag$…$tag$), a comment
// (-- to the end of the line, /* … */ nested), a parenthesized group
// (the multi-action body of CREATE RULE … DO INSTEAD (a; b)) and a
// BEGIN ATOMIC … END function body. Each statement comes back trimmed and without its
// semicolon; a piece that holds only comments and space is dropped.
//
// A NoTransaction migration runs its statements one at a time through it.
// Sent in one Exec with no arguments, a multi-statement body reached
// Postgres as one simple-query message, which it runs as one implicit
// transaction, so a second CREATE INDEX CONCURRENTLY failed with "cannot
// run inside a transaction block".
func splitStatements(body string) []string {
	var out []string
	start := 0
	hasCode := false // the current piece holds something besides comments
	atomic, cases, parens := 0, 0, 0
	prevWord := ""
	flush := func(end int) {
		if hasCode {
			out = append(out, strings.TrimSpace(body[start:end]))
		}
		start, hasCode = end+1, false
	}
	n := len(body)
	for i := 0; i < n; {
		c := body[i]
		switch {
		case c == '-' && i+1 < n && body[i+1] == '-':
			for i < n && body[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && body[i+1] == '*':
			depth := 0
			for i < n {
				if body[i] == '/' && i+1 < n && body[i+1] == '*' {
					depth++
					i += 2
					continue
				}
				if body[i] == '*' && i+1 < n && body[i+1] == '/' {
					depth--
					i += 2
					if depth == 0 {
						break
					}
					continue
				}
				i++
			}
		case c == '\'':
			escapes := i > 0 && (body[i-1] == 'E' || body[i-1] == 'e') && (i < 2 || !identByte(body[i-2]))
			hasCode = true
			i = skipQuoted(body, i, '\'', escapes)
		case c == '"':
			hasCode = true
			i = skipQuoted(body, i, '"', false)
		case c == '$' && (i == 0 || !identByte(body[i-1])):
			hasCode = true
			if tag, ok := dollarTag(body, i); ok {
				end := strings.Index(body[i+len(tag):], tag)
				if end < 0 {
					i = n
				} else {
					i += len(tag) + end + len(tag)
				}
				continue
			}
			i++
		case c == '(':
			hasCode = true
			parens++
			i++
		case c == ')':
			// A stray closer is Postgres's syntax error to report; it
			// must not drive the depth negative and hide later boundaries.
			hasCode = true
			if parens > 0 {
				parens--
			}
			i++
		case c == ';':
			if atomic == 0 && parens == 0 {
				flush(i)
			}
			i++
		case identByte(c) && (i == 0 || !identByte(body[i-1])):
			hasCode = true
			j := i
			for j < n && identByte(body[j]) {
				j++
			}
			word := strings.ToUpper(body[i:j])
			switch {
			case word == "ATOMIC" && prevWord == "BEGIN":
				atomic++
			case atomic > 0 && word == "CASE":
				cases++
			case atomic > 0 && word == "END" && cases > 0:
				cases--
			case atomic > 0 && word == "END":
				atomic--
			}
			prevWord = word
			i = j
		default:
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				hasCode = true
			}
			i++
		}
	}
	flush(n)
	return out
}

// execNoTx runs a NoTransaction migration body outside any transaction.
// On Postgres each statement goes in its own Exec (see splitStatements).
// SQLite runs a multi-statement Exec without wrapping it in a transaction,
// and a CREATE TRIGGER body there holds semicolons, so its body goes as
// one Exec. A failed statement stops the run; the ones before it stay
// applied, which is what the dirty row records.
func (m *Migrator) execNoTx(ctx context.Context, x connish, body string) error {
	if m.dialect == DialectSQLite {
		_, err := x.ExecContext(ctx, body)
		return err
	}
	for _, stmt := range splitStatements(body) {
		if _, err := x.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// identByte reports whether c can be part of an unquoted identifier or
// keyword.
func identByte(c byte) bool {
	return c == '_' || c == '$' || ('0' <= c && c <= '9') || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || c >= 0x80
}

// skipQuoted returns the index just past the quoted run opening at i. A
// doubled quote is an escaped one; with escapes, so is a backslashed one.
// An unterminated run ends the body.
func skipQuoted(body string, i int, q byte, escapes bool) int {
	for i++; i < len(body); i++ {
		switch body[i] {
		case '\\':
			if escapes {
				i++
			}
		case q:
			if i+1 < len(body) && body[i+1] == q {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(body)
}

// dollarTag reads the dollar-quote opener at i: "$$" or "$tag$" with tag
// an identifier that does not start with a digit ($1 is a parameter).
func dollarTag(body string, i int) (string, bool) {
	j := i + 1
	for j < len(body) && body[j] != '$' {
		c := body[j]
		if !(c == '_' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || c >= 0x80 || (j > i+1 && '0' <= c && c <= '9')) {
			return "", false
		}
		j++
	}
	if j >= len(body) {
		return "", false
	}
	return body[i : j+1], true
}
