package query

import (
	"reflect"
	"strings"
	"testing"
)

func TestCountAndDeleteResultsOwnArgs(t *testing.T) {
	for _, b := range []interface{ Build() (string, []any) }{Count("items").Where("id=$1", 1), Delete("items").Where("id=$1", 1)} {
		_, args := b.Build()
		args[0] = 9
		_, again := b.Build()
		if !reflect.DeepEqual(again, []any{1}) {
			t.Fatalf("Build result modified builder: %v", again)
		}
	}
}
func TestUpdateOwnsWhereArgs(t *testing.T) {
	a := []any{1}
	b := Update("items").Set("x", 2).Where("id=$1", a...)
	a[0] = 9
	_, got := b.Build()
	if !reflect.DeepEqual(got, []any{2, 1}) {
		t.Fatal(got)
	}
}
func TestSafeQuoteQualifiedIdentifier(t *testing.T) {
	q, err := SafeQuote("public.items")
	if err != nil || q != `"public"."items"` {
		t.Fatalf("%s %v", q, err)
	}
}
func TestDollarSignsInBareIdentifiers(t *testing.T) {
	got := renumberPlaceholders("col$1 = $1 AND other$tag$ = $2", 3)
	want := "col$1 = $3 AND other$tag$ = $4"
	if got != want {
		t.Fatalf("%q want %q", got, want)
	}
}

// A builder owns what it was given and what Build hands back: the
// caller reusing its slice must not rewrite a later Build.
func TestInsertOwnsSlices(t *testing.T) {
	cols, vals, ret := []string{"a"}, []any{1}, []string{"id"}
	b := Insert("t").Columns(cols...).Values(vals...).Returning(ret...)
	cols[0], vals[0], ret[0] = "b", 9, "secret"
	sql, args := b.Build()
	if !reflect.DeepEqual(args, []any{1}) || strings.Contains(sql, "b") || strings.Contains(sql, "secret") {
		t.Fatalf("caller mutation reached Build: %s %v", sql, args)
	}
	args[0] = 7
	if _, again := b.Build(); !reflect.DeepEqual(again, []any{1}) {
		t.Fatalf("mutating Build's args changed the builder: %v", again)
	}
}

func TestSelectAndReturningOwnColumns(t *testing.T) {
	cols := []string{"name"}
	q := Select(cols...).From("t")
	ret := []string{"id"}
	u := Update("t").Set("x", 1).Returning(ret...)
	cols[0], ret[0] = "secret", "secret"
	if sql, _ := q.Build(); strings.Contains(sql, "secret") {
		t.Fatalf("Select kept the caller's slice: %s", sql)
	}
	if sql, _ := u.Build(); strings.Contains(sql, "secret") {
		t.Fatalf("Returning kept the caller's slice: %s", sql)
	}
}
