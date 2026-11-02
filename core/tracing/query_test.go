package tracing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/sqlredact"
)

func TestQueryTextStripsLiterals(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`SELECT * FROM t WHERE a = $1 AND b = ?2 AND c = :name`, `SELECT * FROM t WHERE a = $1 AND b = ?2 AND c = :name`},
		{`SELECT * FROM t WHERE x = 'a''b' AND y = 3.5e+2`, `SELECT * FROM t WHERE x = ? AND y = ?`},
		{`SELECT $$secret$$, $tag$more$tag$ FROM t`, `SELECT ?, ? FROM t`},
		{"SELECT a -- 'secret'\nFROM t /* 99 */ WHERE b = -1", `SELECT a FROM t WHERE b = -?`},
		{`SELECT "col 42", t1.c2 FROM t1 LIMIT 10 OFFSET 0x1F`, `SELECT "col 42", t1.c2 FROM t1 LIMIT ? OFFSET ?`},
		{"INSERT INTO t\n\t(a)\n VALUES ('unterminated", `INSERT INTO t (a) VALUES (?`},
		{"SELECT 'a\x1b[2J'", `SELECT ?`},
		{"SELECT \"a\x1b\"", `SELECT "a%1b"`},
	} {
		if got := queryText(tc.in, sqlredact.Postgres); got != tc.want {
			t.Errorf("queryText(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
}

func TestQueryTextPerSystemQuoting(t *testing.T) {
	for _, tc := range []struct {
		in     string
		system string
		want   string
	}{
		{`SELECT E'prefix\'TOPSECRET' FROM t`, "postgresql", `SELECT E? FROM t`},
		{`SELECT e'a\\' , 'b' FROM t`, "postgresql", `SELECT e? , ? FROM t`},
		{`SELECT 'C:\' FROM t WHERE x LIKE'y'`, "postgresql", `SELECT ? FROM t WHERE x LIKE?`},
		{`SELECT "col" FROM t`, "postgresql", `SELECT "col" FROM t`},
		{`SELECT 'a\'TOPSECRET' FROM t`, "mysql", `SELECT ? FROM t`},
		{`SELECT "TOPSECRET", 'b' FROM t`, "mysql", `SELECT ?, ? FROM t`},
		{`SELECT * FROM t WHERE email = "a@b.c"`, "sqlite", `SELECT * FROM t WHERE email = ?`},
		{`SELECT 'C:\' FROM t WHERE x LIKE'y'`, "sqlite", `SELECT ? FROM t WHERE x LIKE?`},
		// PostgreSQL nests block comments: the quote in "it's" is inside
		// the comment, and 'TOPSECRET' is a literal.
		{`/* a /* b */ it's */ SELECT 'TOPSECRET'`, "postgresql", `SELECT ?`},
		// MySQL reads # as a line comment, so its quote opens nothing.
		{"SELECT 1 # it's\nFROM t WHERE a = 'TOPSECRET'", "mysql", `SELECT ? FROM t WHERE a = ?`},
		// An engine this package does not know is read under every rule.
		{`SELECT "TOPSECRET", 'a\'TOPSECRET' FROM t`, "sqlserver", `SELECT ?, ?`},
	} {
		if got := queryText(tc.in, sqlredact.ForSystem(tc.system)); got != tc.want {
			t.Errorf("queryText(%q) on %s\n got %q\nwant %q", tc.in, tc.system, got, tc.want)
		}
	}
}

func TestQueryTextCapsLength(t *testing.T) {
	q := "INSERT INTO t (a) VALUES " + strings.Repeat("(?), ", 5000)
	if got := queryText(q, sqlredact.Postgres); len(got) > maxQueryText {
		t.Errorf("len = %d, want at most %d", len(got), maxQueryText)
	}
}

func TestSpanNameIsFirstKeyword(t *testing.T) {
	for in, want := range map[string]string{
		"select 1":                       "SELECT",
		"  /* app:x */ -- note\n with q": "WITH",
		"(SELECT 1)":                     "SQL",
		"":                               "SQL",
		"PRAGMA foreign_keys=on":         "PRAGMA",
	} {
		if got := spanName(in); got != want {
			t.Errorf("spanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestErrorTypeSeesThroughWrapping(t *testing.T) {
	err := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", &customErr{}))
	if got := errorType(err); got != "*tracing.customErr" {
		t.Errorf("errorType = %q", got)
	}
	if got := errorType(errors.New("plain")); got != "*errors.errorString" {
		t.Errorf("errorType = %q", got)
	}
	// A two-%w wrapper is *fmt.wrapErrors; the first wrapped error is
	// the cause (2026-10-27 C12).
	multi := fmt.Errorf("begin: %w (attempt %w)", &customErr{}, context.Canceled)
	if got := errorType(fmt.Errorf("outer: %w", multi)); got != "*tracing.customErr" {
		t.Errorf("errorType through a multi-%%w wrapper = %q", got)
	}
}

type customErr struct{}

func (*customErr) Error() string { return "boom" }
