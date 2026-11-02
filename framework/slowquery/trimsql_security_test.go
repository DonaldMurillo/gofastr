package slowquery

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// TestTrimSQLMasksStringLiterals pins that the slow-query line carries
// the statement's shape, not its literal values. Placeholders already
// keep bound arguments out (only their count is logged), but a
// statement that spells a value inline — CREATE ROLE … PASSWORD '…',
// SET app.api_key = '…', a hand-built WHERE token = '…' — went to the
// WARN line, and from there to every sink, verbatim.
func TestTrimSQLMasksStringLiterals(t *testing.T) {
	cases := []struct {
		in, secret, want string
	}{
		{"ALTER ROLE app WITH PASSWORD 's3cr3t-pw'", "s3cr3t-pw", "ALTER ROLE app WITH PASSWORD ?"},
		{"SELECT * FROM t WHERE token = 'tok''en-value' AND id = $1", "en-value", "SELECT * FROM t WHERE token = ? AND id = $1"},
		{"SET app.key = E'k\\'ey-secret'", "ey-secret", "SET app.key = E?"},
		{"SELECT $tag$dollar-secret$tag$", "dollar-secret", "SELECT ?"},
		{"SELECT $é$dollar-secret$é$", "dollar-secret", "SELECT ?"},
		{"SELECT 'unterminated-secret", "unterminated-secret", "SELECT ?"},
		{"SELECT a /* key=comment-secret */ FROM t LIMIT 10", "comment-secret", "SELECT a FROM t LIMIT ?"},
	}
	for _, c := range cases {
		got := TrimSQL(c.in)
		if strings.Contains(got, c.secret) {
			t.Errorf("TrimSQL(%q) = %q: literal value reached the log line", c.in, got)
		}
		if got != c.want {
			t.Errorf("TrimSQL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Identifiers and placeholders are the shape and stay; a quoted
	// identifier stays once the engine is named as PostgreSQL, where
	// "double-quoted" text is never a string.
	if got := TrimSQL(`SELECT name FROM posts WHERE id = $1 AND x = ?`); got != `SELECT name FROM posts WHERE id = $1 AND x = ?` {
		t.Errorf("TrimSQL changed a literal-free statement: %q", got)
	}
	var buf bytes.Buffer
	s := NewSlowQueryLogger(nil, time.Nanosecond, slog.New(slog.NewJSONHandler(&buf, nil)), WithDialect("postgres"))
	s.observe(context.Background(), "query", `SELECT "Name" FROM posts WHERE id = $1`, nil, time.Second, nil)
	if !strings.Contains(buf.String(), `"sql":"SELECT \"Name\" FROM posts WHERE id = $1"`) {
		t.Errorf("a PostgreSQL quoted identifier did not survive WithDialect(postgres): %s", buf.String())
	}
}

// TestTrimSQLTruncatesOnRuneBoundary: the 240-byte cut split a
// multi-byte rune, so the log line carried invalid UTF-8 (slog then
// writes U+FFFD; a raw sink writes the broken byte).
func TestTrimSQLTruncatesOnRuneBoundary(t *testing.T) {
	q := "SELECT " + strings.Repeat("é", 300)
	got := TrimSQL(q)
	if !utf8.ValidString(got) {
		t.Errorf("TrimSQL cut a rune in half: %q", got[len(got)-8:])
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated statement lost its ellipsis: %q", got[len(got)-8:])
	}
}

// TestSlowQueryLineMasksLiteral checks the logged attr end to end.
func TestSlowQueryLineMasksLiteral(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	s := NewSlowQueryLogger(nil, time.Nanosecond, logger)
	s.observe(context.Background(), "exec", "ALTER ROLE app PASSWORD 'hunter2-secret'", nil, time.Second, sql.ErrNoRows)
	if strings.Contains(buf.String(), "hunter2-secret") {
		t.Errorf("slow-query line leaked the literal: %s", buf.String())
	}
}

// TestTrimSQLMasksEveryDialectsLiterals: the first masker read every
// statement as PostgreSQL, so spellings other engines (or Postgres
// itself) read as literals reached the line. SQLite falls back to a
// string for "double-quoted" text no column is named; MySQL reads a
// backslash as an escape in every string, so 'a\'secret-tail' is one
// literal, and # opens a comment there; Postgres nests block comments,
// so the quote in "it's" sits inside the comment and 'secret2' is a
// literal. TrimSQL does not know the engine, so it masks under every
// reading at once.
func TestTrimSQLMasksEveryDialectsLiterals(t *testing.T) {
	cases := []struct{ in, secret string }{
		{`SELECT * FROM t WHERE token = "secret-dq"`, "secret-dq"},
		{`SELECT * FROM t WHERE a = 'a\'secret-tail' AND b = 1`, "secret-tail"},
		{`/* a /* b */ it's */ SELECT 'secret2'`, "secret2"},
		{"SELECT 1 # it's\nFROM t WHERE a = 'secret-hash'", "secret-hash"},
	}
	for _, c := range cases {
		if got := TrimSQL(c.in); strings.Contains(got, c.secret) {
			t.Errorf("TrimSQL(%q) = %q: literal value reached the log line", c.in, got)
		}
	}
	// Named, each engine masks its own spelling.
	for _, c := range []struct{ dialect, in, secret string }{
		{"sqlite3", `SELECT * FROM t WHERE token = "secret-dq"`, "secret-dq"},
		{"mysql", `SELECT * FROM t WHERE a = 'a\'secret-tail' AND b = 1`, "secret-tail"},
		{"mysql", "SELECT 1 # it's\nFROM t WHERE a = 'secret-hash'", "secret-hash"},
		{"postgres", `/* a /* b */ it's */ SELECT 'secret2'`, "secret2"},
	} {
		var buf bytes.Buffer
		s := NewSlowQueryLogger(nil, time.Nanosecond, slog.New(slog.NewJSONHandler(&buf, nil)), WithDialect(c.dialect))
		s.observe(context.Background(), "query", c.in, nil, time.Second, nil)
		if strings.Contains(buf.String(), c.secret) {
			t.Errorf("WithDialect(%s): %q leaked to the line: %s", c.dialect, c.in, buf.String())
		}
	}
}
