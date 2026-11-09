package routeseg

import "testing"

func TestConstraintOKByName(t *testing.T) {
	for _, c := range []struct {
		seg, val string
		want     bool
	}{
		{":id:int", "42", true},
		{":id:int", "4a", false},
		{":id:int", "", false},
		{":id:uuid", "0f8fad5b-d9cb-469f-a165-70867728950e", true},
		{":id:uuid", "0f8fad5b-d9cb-469f-a165-70867728950", false},
		{":slug:alpha", "abc", true},
		{":slug:alpha", "ab1", false},
		{":code:alnum", "ab1", true},
		{":code:alnum", "ab-1", false},
		{":id", "anything", true},
		{"literal", "x", true},
	} {
		if got := ConstraintOK(c.seg, c.val); got != c.want {
			t.Errorf("ConstraintOK(%q, %q) = %v, want %v", c.seg, c.val, got, c.want)
		}
	}
	if Valid("string") || !Valid("int") {
		t.Error(`Valid: want "int" accepted and "string" refused`)
	}
	if Constraint(":id:int") != "int" || Constraint(":path*") != "" {
		t.Error(`Constraint: want "int" for ":id:int" and "" for ":path*"`)
	}
}
