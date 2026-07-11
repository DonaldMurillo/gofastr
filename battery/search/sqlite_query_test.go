package search

import (
	"strings"
	"testing"
)

// TestBuildFts5Query covers the pure FTS5 query-text sanitizer that is the
// single chokepoint between untrusted query strings and the SQLite FTS5
// MATCH operator. No DB needed. Each surviving term is DOUBLE-QUOTED so
// FTS5 operators (AND/OR/NOT/NEAR), column filters (col:), parens, and
// quotes are all neutralised — they become literal phrase text. The last
// term carries a trailing * (outside the quote) for prefix matching.
func TestBuildFts5Query(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   \t\n ", ""},
		{"single term prefixes last", "pagination", `"pagination"*`},
		{"two terms space joined", "hello world", `"hello" "world"*`},
		{"prefix only on last term", "a b c", `"a" "b" "c"*`},
		{"drops operators and punctuation", "a & b | c", `"a" "b" "c"*`},
		{"strips fts5 metachars", ":* ! ( )", ""},
		{"sql injection attempt", "'; DROP TABLE x; --", `"drop" "table" "x"*`},
		// FTS5 operators AND/OR/NOT/NEAR are double-quoted → literal phrases.
		{"fts5 AND operator neutralised", "a AND b", `"a" "and" "b"*`},
		{"fts5 OR operator neutralised", "a OR b", `"a" "or" "b"*`},
		{"fts5 NOT operator neutralised", "a NOT b", `"a" "not" "b"*`},
		{"fts5 NEAR operator neutralised", "a NEAR b", `"a" "near" "b"*`},
		// Parens payload — all stripped, never grouped.
		{"parens payload", "(a) (b)", `"a" "b"*`},
		{"dedupes repeated", "go go go", `"go"*`},
		{"dedupes case-insensitive", "Go go GO", `"go"*`},
		{"hyphen term kept internal", "e-commerce", `"e-commerce"*`},
		// Column filter colon is stripped within a token (same sanitizer as Postgres).
		{"column filter stripped", "col:value other", `"colvalue" "other"*`},
		{"pure punctuation dropped", "---", ""},
		{"underscore kept", "foo_bar baz", `"foo_bar" "baz"*`},
		{"unicode letters kept", "café résumé", `"café" "résumé"*`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildFts5Query(tc.in); got != tc.want {
				t.Fatalf("buildFts5Query(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestBuildFts5QueryBounded mirrors the Postgres backend's term-cap property:
// an attacker-controlled query cannot grow cost without bound. The builder
// caps distinct terms at maxQueryTerms.
func TestBuildFts5QueryBounded(t *testing.T) {
	var b []byte
	for i := 0; i < maxQueryTerms*4; i++ {
		b = append(b, 't')
		b = append(b, byte('a'+i%26))
		b = append(b, byte('0'+i%10))
		b = append(b, ' ')
	}
	out := buildFts5Query(string(b))
	if out == "" {
		t.Fatal("expected non-empty query")
	}
	// Each term is double-quoted: count quote chars / 2 = term count.
	quotes := strings.Count(out, `"`)
	if terms := quotes / 2; terms > maxQueryTerms {
		t.Fatalf("buildFts5Query emitted %d terms, cap is %d", terms, maxQueryTerms)
	}
	// The last term must carry the trailing * (outside the closing quote).
	if !strings.HasSuffix(out, `"*`) {
		t.Fatalf("query %q missing trailing prefix marker", out)
	}
}
