package filter

import (
	"strings"
	"testing"
)

func TestSearchConditions_ShapeAndParens(t *testing.T) {
	conds := SearchConditions([]string{"title", "body"}, "hello")
	if len(conds) != 1 {
		t.Fatalf("got %d conditions, want 1", len(conds))
	}
	c := conds[0]
	// Each condition must be a single parenthesized OR-group.
	if !strings.HasPrefix(c.SQL, "(") || !strings.HasSuffix(c.SQL, ")") {
		t.Fatalf("condition not parenthesized: %q", c.SQL)
	}
	if !strings.Contains(c.SQL, "LOWER(title)") || !strings.Contains(c.SQL, "LOWER(body)") {
		t.Fatalf("missing LOWER(field): %q", c.SQL)
	}
	if !strings.Contains(c.SQL, "ESCAPE '\\'") {
		t.Fatalf("missing ESCAPE: %q", c.SQL)
	}
	if !strings.Contains(c.SQL, " OR ") {
		t.Fatalf("missing OR: %q", c.SQL)
	}
}

func TestSearchConditions_ArgAlignment(t *testing.T) {
	conds := SearchConditions([]string{"title", "body", "tags"}, "hello")
	if len(conds[0].Args) != 3 {
		t.Fatalf("got %d args, want 3 (one per field)", len(conds[0].Args))
	}
	for _, a := range conds[0].Args {
		s, ok := a.(string)
		if !ok {
			t.Fatalf("arg not string: %T", a)
		}
		if !strings.Contains(s, "hello") {
			t.Fatalf("arg missing token: %q", s)
		}
	}
}

func TestSearchConditions_PlaceholderNumbering(t *testing.T) {
	// 3 fields → $1, $2, $3 within one condition.
	conds := SearchConditions([]string{"a", "b", "c"}, "x")
	if !strings.Contains(conds[0].SQL, "$1") || !strings.Contains(conds[0].SQL, "$2") || !strings.Contains(conds[0].SQL, "$3") {
		t.Fatalf("missing sequential placeholders: %q", conds[0].SQL)
	}
	if strings.Contains(conds[0].SQL, "$4") {
		t.Fatalf("unexpected $4: %q", conds[0].SQL)
	}
}

func TestSearchConditions_TokenizationDedupCap(t *testing.T) {
	// Dedup: repeated tokens collapse.
	conds := SearchConditions([]string{"f"}, "alpha beta alpha")
	if len(conds) != 2 {
		t.Fatalf("dedup: got %d conditions, want 2", len(conds))
	}

	// Cap: 10 tokens → 8 conditions (MaxSearchTerms).
	many := "one two three four five six seven eight nine ten"
	conds = SearchConditions([]string{"f"}, many)
	if len(conds) != MaxSearchTerms {
		t.Fatalf("cap: got %d conditions, want %d", len(conds), MaxSearchTerms)
	}
}

func TestSearchConditions_BlankReturnsNil(t *testing.T) {
	if conds := SearchConditions([]string{"f"}, ""); conds != nil {
		t.Fatalf("empty term should return nil, got %d", len(conds))
	}
	if conds := SearchConditions([]string{"f"}, "   "); conds != nil {
		t.Fatalf("whitespace term should return nil, got %d", len(conds))
	}
	if conds := SearchConditions(nil, "hello"); conds != nil {
		t.Fatalf("nil fields should return nil, got %d", len(conds))
	}
}

func TestSearchConditions_UnicodeLowering(t *testing.T) {
	// Uppercase Unicode token is lowercased before building the pattern.
	conds := SearchConditions([]string{"f"}, "Café")
	if len(conds) != 1 {
		t.Fatalf("got %d conditions, want 1", len(conds))
	}
	pat := conds[0].Args[0].(string)
	if !strings.Contains(pat, "café") {
		t.Fatalf("token not lowercased in pattern: %q", pat)
	}
}

func TestSearchConditions_EscapesWildcards(t *testing.T) {
	// A literal % in the search term must be escaped to \% so it matches
	// literally, not as a wildcard.
	conds := SearchConditions([]string{"f"}, "50%")
	pat := conds[0].Args[0].(string)
	if !strings.Contains(pat, `\%`) {
		t.Fatalf("percent not escaped in pattern: %q", pat)
	}
	// Underscore must be escaped too.
	conds = SearchConditions([]string{"f"}, "a_b")
	pat = conds[0].Args[0].(string)
	if !strings.Contains(pat, `\_`) {
		t.Fatalf("underscore not escaped in pattern: %q", pat)
	}
}

func TestSearchConditions_MultiTokenANDComposition(t *testing.T) {
	// Two tokens → two conditions that must AND together.
	conds := SearchConditions([]string{"title"}, "foo bar")
	if len(conds) != 2 {
		t.Fatalf("got %d conditions, want 2", len(conds))
	}
	// Each has its own arg aligned to its own $1 placeholder.
	if len(conds[0].Args) != 1 || len(conds[1].Args) != 1 {
		t.Fatalf("arg count mismatch: %d, %d", len(conds[0].Args), len(conds[1].Args))
	}
	if conds[0].Args[0].(string) == conds[1].Args[0].(string) {
		t.Fatal("two distinct tokens produced identical patterns")
	}
}
