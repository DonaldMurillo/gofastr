package crud

import (
	"net/url"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Sol #16. A client is told the field is "content"; every filter surface must
// accept that name. Resolving it on the flat path but not on nested or scoped
// filters splits the wire contract by entry point.
func TestNestedFilter_AcceptsWireNameAndEmitsColumn(t *testing.T) {
	users := mkEntity("users", "", []schema.Field{
		{Name: "id", Type: schema.Int},
		{Name: "body_text", Type: schema.String, WireName: "content"},
		{Name: "secret", Type: schema.String, Hidden: true, WireName: "leaky"},
	})
	reg := versionedReg{m: map[string]*entity.Entity{"users|": users}}
	posts := mkRelEntity("posts", "", "users")

	got, err := parseNestedFiltersValues(url.Values{"author.content": []string{"hi"}}, posts, reg)
	if err != nil {
		t.Fatalf("?author.content rejected: %v", err)
	}
	if len(got) != 1 || got[0].Field != "body_text" {
		t.Fatalf("got %+v — Field reaches SQL and must be the column", got)
	}

	// The column name still works (existing callers).
	if _, err := parseNestedFiltersValues(url.Values{"author.body_text": []string{"hi"}}, posts, reg); err != nil {
		t.Errorf("?author.body_text rejected — existing callers break: %v", err)
	}
	// Hidden stays unreachable under BOTH names.
	for _, k := range []string{"author.secret", "author.leaky"} {
		if _, err := parseNestedFiltersValues(url.Values{k + "_like": []string{"x"}}, posts, reg); err == nil {
			t.Errorf("hidden field reachable via %q", k)
		}
	}
}

func TestScopedFilter_AcceptsWireNameAndEmitsColumn(t *testing.T) {
	fields := []schema.Field{
		{Name: "body_text", Type: schema.String, WireName: "content"},
		{Name: "secret", Type: schema.String, Hidden: true, WireName: "leaky"},
	}
	got, err := parseScopedFilters("content=hi", fields, "comments")
	if err != nil {
		t.Fatalf("scoped wire name rejected: %v", err)
	}
	if len(got) != 1 || got[0].Field != "body_text" {
		t.Fatalf("got %+v — Field reaches WHERE and must be the column", got)
	}
	if _, err := parseScopedFilters("body_text=hi", fields, "comments"); err != nil {
		t.Errorf("scoped column name rejected: %v", err)
	}
	for _, k := range []string{"secret=x", "leaky=x"} {
		if _, err := parseScopedFilters(k, fields, "comments"); err == nil {
			t.Errorf("hidden field reachable via scoped %q", k)
		}
	}
}
