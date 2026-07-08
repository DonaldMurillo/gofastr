package search

import "testing"

// TestBuildTsQuery covers the pure query-text sanitizer that is the single
// chokepoint between untrusted query strings and to_tsquery. No DB needed.
func TestBuildTsQuery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   \t\n ", ""},
		{"single term prefixes last", "pagination", "pagination:*"},
		{"two terms and joined", "hello world", "hello & world:*"},
		{"prefix only on last term", "a b c", "a & b & c:*"},
		{"drops operators and punctuation", "a & b | c", "a & b & c:*"},
		{"strips tsquery metachars", ":* ! ( )", ""},
		{"sql injection attempt", "'; DROP TABLE x; --", "drop & table & x:*"},
		{"parens payload matches nothing-everything", "a & b | c :* ! (", "a & b & c:*"},
		{"dedupes repeated", "go go go", "go:*"},
		{"dedupes case-insensitive", "Go go GO", "go:*"},
		{"hyphen term kept internal", "e-commerce", "e-commerce:*"},
		{"leading trailing hyphens trimmed", "--foo--", "foo:*"},
		{"pure punctuation dropped", "---", ""},
		{"underscore kept", "foo_bar baz", "foo_bar & baz:*"},
		{"unicode letters kept", "café résumé", "café & résumé:*"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildTsQuery(tc.in); got != tc.want {
				t.Fatalf("buildTsQuery(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestBuildTsQueryBounded mirrors the Memory backend's term-cap property:
// an attacker-controlled query cannot grow cost without bound. The builder
// caps distinct terms at maxQueryTerms.
func TestBuildTsQueryBounded(t *testing.T) {
	// Build a query of far more distinct terms than the cap.
	var b []byte
	for i := 0; i < maxQueryTerms*4; i++ {
		b = append(b, 't')
		b = append(b, byte('a'+i%26))
		b = append(b, byte('0'+i%10))
		b = append(b, ' ')
	}
	out := buildTsQuery(string(b))
	if out == "" {
		t.Fatal("expected non-empty query")
	}
	// Count terms: every '&' separates two terms, so terms = ampersands + 1.
	amp := 0
	for i := 0; i < len(out); i++ {
		if out[i] == '&' {
			amp++
		}
	}
	if terms := amp + 1; terms > maxQueryTerms {
		t.Fatalf("buildTsQuery emitted %d terms, cap is %d", terms, maxQueryTerms)
	}
	// The last term must carry the prefix flag.
	if !endsWith(out, ":*") {
		t.Fatalf("query %q missing trailing :*", out)
	}
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
