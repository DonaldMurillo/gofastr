package filter

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// The ?where= predicate path must coerce Bool-column values the same way
// ParseFiltersValues does, otherwise ?published=true matches rows while
// the equivalent where-tree binds TEXT 'true' against an INTEGER column
// and matches nothing on SQLite.

func whereBoolFields() []schema.Field {
	return []schema.Field{
		{Name: "published", Type: schema.Bool},
		{Name: "title", Type: schema.String},
	}
}

func TestWhereBoolValuesBindAsBools(t *testing.T) {
	p, err := ParseWhere(`{"field":"published","value":"true"}`, whereBoolFields())
	if err != nil {
		t.Fatalf("ParseWhere: %v", err)
	}
	cond := BuildPredicate(p)
	if len(cond.Args) != 1 {
		t.Fatalf("args = %v, want 1", cond.Args)
	}
	if b, ok := cond.Args[0].(bool); !ok || !b {
		t.Fatalf("args[0] = %#v, want bool true", cond.Args[0])
	}
}

func TestWhereBoolInValuesBindAsBools(t *testing.T) {
	p, err := ParseWhere(`{"field":"published","op":"in","values":["true","false"]}`, whereBoolFields())
	if err != nil {
		t.Fatalf("ParseWhere: %v", err)
	}
	cond := BuildPredicate(p)
	if len(cond.Args) != 2 {
		t.Fatalf("args = %v, want 2", cond.Args)
	}
	if cond.Args[0] != true || cond.Args[1] != false {
		t.Fatalf("args = %#v, want [true false]", cond.Args)
	}
}

func TestWhereNonBoolValuesStayStrings(t *testing.T) {
	p, err := ParseWhere(`{"field":"title","value":"true"}`, whereBoolFields())
	if err != nil {
		t.Fatalf("ParseWhere: %v", err)
	}
	cond := BuildPredicate(p)
	if len(cond.Args) != 1 || cond.Args[0] != "true" {
		t.Fatalf("args = %#v, want [\"true\"] (string, not coerced)", cond.Args)
	}
}
