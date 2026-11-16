package query

import "testing"

// OrderTerm is the one renderer of an ORDER BY term: a pinned term spells
// nulls last ascending and first descending, an unpinned one is bare.
func TestOrderTermPinsPostgresNullPosition(t *testing.T) {
	for _, c := range []struct {
		column, dir string
		pin         bool
		want        string
	}{
		{"views", "ASC", false, "views ASC"},
		{"views", "DESC", false, "views DESC"},
		{"views", "", false, "views"},
		{"views", "ASC", true, "views ASC NULLS LAST"},
		{"views", "asc", true, "views ASC NULLS LAST"},
		{"views", "", true, "views NULLS LAST"},
		{"views", "DESC", true, "views DESC NULLS FIRST"},
		{"order", "DESC", true, `"order" DESC NULLS FIRST`},
		{`"views"`, "", true, `"views" NULLS LAST`},
		{"views", "DESC; DROP TABLE t", true, "views NULLS LAST"},
	} {
		if got := OrderTerm(c.column, c.dir, c.pin); got != c.want {
			t.Errorf("OrderTerm(%q, %q, %v) = %q, want %q", c.column, c.dir, c.pin, got, c.want)
		}
	}
	sql, _ := Select("id").From("posts").OrderPinNulls("views", "DESC").Order("id", "ASC").Build()
	if want := "SELECT id FROM posts ORDER BY views DESC NULLS FIRST, id ASC"; sql != want {
		t.Errorf("Build = %q, want %q", sql, want)
	}
}
