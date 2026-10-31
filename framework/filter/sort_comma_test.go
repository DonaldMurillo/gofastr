package filter

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// A sort value can name several fields, comma-separated: the generated
// CLI's --sort=title,-created_at and the module broker's sort arrays
// send that form, and ParseSort refused it as one unknown field
// "title,-created_at" (2026-10-27 bug bash D26). The 16-field bound
// counts the fields, so commas cannot carry more clauses past it.
func TestSortCommaListsFields(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "created_at", Type: schema.String},
	}
	sorts, err := ParseSortValues(url.Values{"sort": {"title,-created_at"}}, fields)
	if err != nil {
		t.Fatalf("comma-separated sort refused: %v", err)
	}
	want := []ParsedSort{{Field: "title"}, {Field: "created_at", Desc: true}}
	if !sortsEqual(sorts, want) {
		t.Fatalf("sorts = %+v, want %+v", sorts, want)
	}

	if _, err := ParseSortValues(url.Values{"sort": {"title,nope"}}, fields); err == nil {
		t.Error("an unknown field after a comma was accepted")
	}

	// 17 fields in one value, and 16 + 1 across two values.
	over := strings.TrimSuffix(strings.Repeat("title,", 17), ",")
	for _, v := range []url.Values{
		{"sort": {over}},
		{"sort": {strings.TrimSuffix(strings.Repeat("title,", 16), ","), "created_at"}},
	} {
		if _, err := ParseSortValues(v, fields); err == nil || !strings.Contains(err.Error(), "17") {
			t.Errorf("sort %q: err = %v, want the 17-field refusal", v["sort"], err)
		}
	}
	if _, err := ParseSortValues(url.Values{"sort": {strings.TrimSuffix(strings.Repeat("title,", 16), ",")}}, fields); err != nil {
		t.Errorf("16 comma-separated fields refused: %v", err)
	}
}
