package crud

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// A nested _like value is a text bind like any other: invalid UTF-8 or a
// NUL is Postgres's 22021 (a 500) while SQLite answers 200, so the nested
// path refuses it the way the top-level filter parser does.
func TestNestedLikeRefusesUnstorableText(t *testing.T) {
	for _, v := range []string{"a\xffb", "a\x00b"} {
		nf := nestedFilter{Op: filter.OpLike, Value: v, colType: schema.String}
		if err := checkNestedValues("author.name_like", nf); err == nil {
			t.Errorf("nested _like %q accepted; Postgres refuses this text bind", v)
		}
	}
	ok := nestedFilter{Op: filter.OpLike, Value: "ada", colType: schema.String}
	if err := checkNestedValues("author.name_like", ok); err != nil {
		t.Fatalf("plain nested _like refused: %v", err)
	}
}
