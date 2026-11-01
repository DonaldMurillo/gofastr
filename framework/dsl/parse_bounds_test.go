package dsl

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestDSLQuotedIncludeStoredUnquoted pins include() to keep the bare
// relation name it validated. It checked trimDSLValue(inc) but stored
// inc, so include("author") made BuildDSLQuery look up `"author"`.
func TestDSLQuotedIncludeStoredUnquoted(t *testing.T) {
	q, err := ParseDSL(`posts.include("author", 'tags', comments)`)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	if strings.Join(q.Includes, ",") != "author,tags,comments" {
		t.Fatalf("Includes = %q, want [author tags comments]", q.Includes)
	}
}

// TestDSLInListAndOrderBounded pins the DSL to the caps its sibling
// surfaces apply: an in-list holds at most filter.MaxINListEntries (1000)
// values, as on ?f_in=, ?where= and ?filter=, and a query carries at most
// 16 order() keys, as ?sort= and ParseSort do.
func TestDSLInListAndOrderBounded(t *testing.T) {
	reg := postsRegistry(t, nil)
	in := func(n int) string {
		return `posts.where(seq in [` + strings.TrimSuffix(strings.Repeat("1,", n), ",") + `])`
	}
	if _, err := BuildDSLQuery(reg, in(1001)); err == nil {
		t.Errorf("in-list of 1001 entries accepted")
	}
	if _, err := BuildDSLQuery(reg, in(1000)); err != nil {
		t.Errorf("in-list of 1000 entries: %v", err)
	}
	if _, err := ParseDSL(`posts` + strings.Repeat(".order(seq)", 17)); err == nil {
		t.Errorf("17 order() clauses accepted")
	}
	if _, err := ParseDSL(`posts` + strings.Repeat(".order(seq)", 16)); err != nil {
		t.Errorf("16 order() clauses: %v", err)
	}
}

// TestDSLUnbalancedBracketRefused pins the refusal of a bracket or paren
// that does not balance outside quotes. splitDSLArgs tracked their depth,
// so an unquoted `a]` (depth -1) or `a[` (depth 1) stopped every later
// comma from splitting, and the predicates after it vanished into the
// value.
func TestDSLUnbalancedBracketRefused(t *testing.T) {
	for _, in := range []string{
		`posts.where(title=a], seq=1)`,
		`posts.where(title=a[, seq=1)`,
		`posts.where(seq in [1,2]], status="x")`,
		`posts.include(author], tags)`,
	} {
		if q, err := ParseDSL(in); err == nil {
			t.Errorf("%s: accepted as %+v", in, q)
		}
	}
	for _, in := range []string{
		`posts.where(title="a]", seq=1)`,
		`posts.where(seq in [1,2], status="x")`,
	} {
		q, err := ParseDSL(in)
		if err != nil || len(q.Filters) != 2 {
			t.Errorf("%s: %+v, %v; want two filters", in, q.Filters, err)
		}
	}
}

// TestDSLDirectionFoldsASCIIOnly pins order directions to ASCII case
// folding. strings.ToUpper maps ſ (U+017F) onto S, so "deſc" sorted
// descending: the homoglyph shape the asciifold analyzer names.
func TestDSLDirectionFoldsASCIIOnly(t *testing.T) {
	if _, err := ParseDSL("posts.order(seq deſc)"); err == nil {
		t.Errorf("order(seq deſc) accepted")
	}
	fields := []schema.Field{{Name: "seq", Type: schema.Int}}
	if _, err := ParseSort("seq deſc", fields); err == nil {
		t.Errorf(`ParseSort("seq deſc") accepted`)
	}
	q, err := ParseDSL("posts.order(seq desc).order(id Asc)")
	if err != nil || q.Orders[0].Direction != "DESC" || q.Orders[1].Direction != "ASC" {
		t.Fatalf("ASCII directions: %+v, %v", q.Orders, err)
	}
	s, err := ParseSort("seq DeSc", fields)
	if err != nil || len(s) != 1 || !s[0].Desc {
		t.Fatalf(`ParseSort("seq DeSc") = %+v, %v`, s, err)
	}
}
