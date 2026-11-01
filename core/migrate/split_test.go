package migrate

import (
	"slices"
	"testing"
)

// A semicolon inside a string, an identifier, a dollar-quoted body, a
// comment or a BEGIN ATOMIC body is no statement boundary.
func TestSplitStatementsKeepsQuotedSemis(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"plain", "SELECT 1; SELECT 2;", []string{"SELECT 1", "SELECT 2"}},
		{"no trailing semicolon", "SELECT 1;\nSELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{"string", "SELECT ';'; SELECT 'it''s; here'", []string{"SELECT ';'", "SELECT 'it''s; here'"}},
		{"escape string", `SELECT E'a\';b'; SELECT 2`, []string{`SELECT E'a\';b'`, "SELECT 2"}},
		{"identifier", `SELECT 1 AS "a;b"; SELECT 2`, []string{`SELECT 1 AS "a;b"`, "SELECT 2"}},
		{"dollar body", "CREATE FUNCTION f() RETURNS int AS $$ SELECT 1; $$ LANGUAGE sql; SELECT 2",
			[]string{"CREATE FUNCTION f() RETURNS int AS $$ SELECT 1; $$ LANGUAGE sql", "SELECT 2"}},
		{"tagged dollar body", "DO $x$ BEGIN PERFORM 1; END $x$; SELECT 2",
			[]string{"DO $x$ BEGIN PERFORM 1; END $x$", "SELECT 2"}},
		{"parameter is no tag", "SELECT $1; SELECT 2", []string{"SELECT $1", "SELECT 2"}},
		{"line comment", "-- a; b\nSELECT 1; -- c;\nSELECT 2", []string{"-- a; b\nSELECT 1", "-- c;\nSELECT 2"}},
		{"block comment", "/* a; /* nested; */ b; */ SELECT 1; SELECT 2", []string{"/* a; /* nested; */ b; */ SELECT 1", "SELECT 2"}},
		{"comment only piece", "SELECT 1; -- trailing note", []string{"SELECT 1"}},
		{"empty pieces", ";; SELECT 1;;", []string{"SELECT 1"}},
		{"begin atomic", "CREATE FUNCTION f() RETURNS int BEGIN ATOMIC SELECT CASE WHEN true THEN 1 END; SELECT 2; END; SELECT 3",
			[]string{"CREATE FUNCTION f() RETURNS int BEGIN ATOMIC SELECT CASE WHEN true THEN 1 END; SELECT 2; END", "SELECT 3"}},
		{"empty", "  \n", nil},
		// An unterminated quote or dollar body runs to the end of the
		// body: its semicolons are no boundary, and Postgres reports the
		// syntax error on the one statement.
		{"unterminated string", "SELECT 1; SELECT 'a; b", []string{"SELECT 1", "SELECT 'a; b"}},
		{"unterminated dollar body", "SELECT 1; DO $$ x; y", []string{"SELECT 1", "DO $$ x; y"}},
		{"dollar opener at the end", "SELECT 1; SELECT $tag", []string{"SELECT 1", "SELECT $tag"}},
	}
	for _, c := range cases {
		if got := splitStatements(c.body); !slices.Equal(got, c.want) {
			t.Errorf("%s: splitStatements(%q) = %q, want %q", c.name, c.body, got, c.want)
		}
	}
}

// A multi-action rule body sits in parentheses; its semicolons belong to
// the one CREATE RULE statement.
func TestSplitStatementsKeepsRuleBody(t *testing.T) {
	body := "CREATE RULE r AS ON INSERT TO t DO INSTEAD (INSERT INTO a VALUES (1); INSERT INTO b VALUES (2)); SELECT 2"
	want := []string{"CREATE RULE r AS ON INSERT TO t DO INSTEAD (INSERT INTO a VALUES (1); INSERT INTO b VALUES (2))", "SELECT 2"}
	if got := splitStatements(body); !slices.Equal(got, want) {
		t.Errorf("splitStatements(%q) = %q, want %q", body, got, want)
	}
	// A stray closer never drives the depth negative and swallows the
	// boundaries after it.
	body = "SELECT 1); SELECT 2"
	want = []string{"SELECT 1)", "SELECT 2"}
	if got := splitStatements(body); !slices.Equal(got, want) {
		t.Errorf("splitStatements(%q) = %q, want %q", body, got, want)
	}
}
