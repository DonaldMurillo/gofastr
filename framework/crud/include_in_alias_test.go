package crud

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Two scoped _in terms on one column (the column name and its wire alias)
// are two ANDed IN lists, the rule the top-level ?f_in= keys follow: the
// renderer used to merge every adjacent same-field run into one union.
func TestScopedINAliasTermsAreANDed(t *testing.T) {
	fields := []schema.Field{{Name: "author_id", Type: schema.Int, WireName: "writer"}}
	fs, err := parseScopedFilters("author_id_in=1|2,writer_in=3", fields, "comments")
	if err != nil {
		t.Fatal(err)
	}
	clause, args := filterClause(fs, 1)
	if strings.Count(clause, " IN (") != 2 {
		t.Fatalf("author_id_in=1|2,writer_in=3 rendered %q %v; want two ANDed IN lists", clause, args)
	}
	one, _ := parseScopedFilters("author_id_in=1|2", fields, "comments")
	if c, _ := filterClause(one, 1); strings.Count(c, " IN (") != 1 || !strings.Contains(c, "$1, $2") {
		t.Fatalf("one term's values must stay one IN list: %q", c)
	}
}
