package query

import (
	"reflect"
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
