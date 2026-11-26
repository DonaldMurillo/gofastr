package migrate

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/sqllex"
)

func splitStatements(body string) []string {
	return splitStatementsWithCommentNesting(body, true)
}

func splitStatementsWithCommentNesting(body string, nest bool) []string {
	var out []string
	start := 0
	hasCode := false
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
			i = sqllex.SkipBlockComment(body, i, nest)
		case c == '\'':
			escapes := i > 0 && (body[i-1] == 'E' || body[i-1] == 'e') && (i < 2 || !identByte(body[i-2]))
			hasCode = true
			i = sqllex.SkipQuoted(body, i, escapes)
		case c == '"':
			hasCode = true
			i = sqllex.SkipQuoted(body, i, false)
		case c == '$' && (i == 0 || !identByte(body[i-1])):
			hasCode = true
			if tag := sqllex.DollarTag(body[i:]); tag != "" {
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

func identByte(c byte) bool {
	return c == '_' || c == '$' || ('0' <= c && c <= '9') || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || c >= 0x80
}
