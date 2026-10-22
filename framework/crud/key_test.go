package crud

import (
	"slices"
	"testing"
)

func TestKeyCondNamesEachColumn(t *testing.T) {
	ch := &CrudHandler{PrimaryKey: "code"}
	cond, args := ch.keyCond([]any{"x"})
	if cond != "code = $1" || !slices.Equal(args, []any{"x"}) {
		t.Errorf("keyCond = %q %v", cond, args)
	}
}

func TestKeyCondMismatchMatchesNothing(t *testing.T) {
	ch := &CrudHandler{PrimaryKey: "code"}
	for _, vals := range [][]any{nil, {"a", "b"}} {
		if cond, args := ch.keyCond(vals); cond != "1 = 0" || args != nil {
			t.Errorf("keyCond(%v) = %q %v, want a condition matching nothing", vals, cond, args)
		}
	}
}

func TestRowKeyValsReadsWireOrColumn(t *testing.T) {
	ch := &CrudHandler{PrimaryKey: "user_code", JSONCase: CaseCamel}
	for _, row := range []map[string]any{{"userCode": "u1"}, {"user_code": "u1"}} {
		vals, ok := ch.rowKeyVals(row)
		if !ok || !slices.Equal(vals, []any{"u1"}) {
			t.Errorf("rowKeyVals(%v) = %v, %v", row, vals, ok)
		}
	}
	if _, ok := ch.rowKeyVals(map[string]any{"userCode": nil}); ok {
		t.Error("a nil key value must not name a row")
	}
}

func TestKeyPayloadUsesWireKey(t *testing.T) {
	ch := &CrudHandler{PrimaryKey: "user_code", JSONCase: CaseCamel}
	got := ch.keyPayload("u1")
	if len(got) != 1 || got["userCode"] != "u1" {
		t.Errorf("keyPayload = %v", got)
	}
}
