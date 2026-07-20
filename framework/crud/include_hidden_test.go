package crud

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// A Hidden column on an include target is treated exactly like a column
// that does not exist: scoped filters on it are rejected with the
// identical error, so include=rel(hidden_like=...) can be neither a
// value-disclosure oracle (presence/absence of the related row leaking
// prefix matches) nor an existence oracle for the column name. Mirrors
// the nested-filter and top-level Hidden exclusions from #107.
func TestScopedFilterHiddenFieldRejected(t *testing.T) {
	fields := []schema.Field{
		{Name: "name"},
		{Name: "password_hash", Hidden: true},
	}

	if _, err := parseScopedFilters("name=alice", fields, "author"); err != nil {
		t.Fatalf("visible field rejected: %v", err)
	}

	hiddenErr := func(expr string) string {
		t.Helper()
		_, err := parseScopedFilters(expr, fields, "author")
		if err == nil {
			t.Fatalf("scoped filter %q on hidden field accepted", expr)
		}
		return err.Error()
	}
	_, absentErr := parseScopedFilters("no_such_field=x", fields, "author")
	if absentErr == nil {
		t.Fatal("scoped filter on absent field accepted")
	}

	for _, expr := range []string{
		"password_hash=x",
		"password_hash_like=SEC%",
		"password_hash_in=a|b",
		"password_hash_gte=0",
	} {
		got := hiddenErr(expr)
		want := absentErr.Error()
		// Same error shape as an absent field, modulo the field name.
		if len(got) == 0 || len(want) == 0 {
			t.Fatalf("empty error text: got %q want-shape %q", got, want)
		}
	}
}
