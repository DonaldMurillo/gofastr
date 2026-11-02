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
		{"ALTER ROLE app WITH PASSWORD 's3cr3t-pw'", "s3cr3t-pw", "ALTER ROLE app WITH PASSWORD '?'"},
		{"SELECT * FROM t WHERE token = 'tok''en-value' AND id = $1", "en-value", "SELECT * FROM t WHERE token = '?' AND id = $1"},
		{"SET app.key = E'k\\'ey-secret'", "ey-secret", "SET app.key = E'?'"},
		{"SELECT $tag$dollar-secret$tag$", "dollar-secret", "SELECT $tag$?$tag$"},
		{"SELECT 'unterminated-secret", "unterminated-secret", "SELECT '?"},
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
	// Identifiers and placeholders are the shape and stay.
	if got := TrimSQL(`SELECT "Name" FROM posts WHERE id = $1 AND x = ?`); got != `SELECT "Name" FROM posts WHERE id = $1 AND x = ?` {
		t.Errorf("TrimSQL changed a literal-free statement: %q", got)
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
