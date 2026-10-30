package dsl

import "testing"

// A where() term splits at its leftmost operator, never at one inside
// its value. Each operator was searched across the whole term in turn,
// " in " first, so title="Drive in Cinema" split at the " in " inside
// the quotes and was refused (2026-10-27 bug bash D32).
func TestWhereSplitsAtLeftmostOp(t *testing.T) {
	for _, tc := range []struct{ in, field, op, value string }{
		{`title="Drive in Cinema"`, "title", "=", "Drive in Cinema"},
		{`title = 'a contains b'`, "title", "=", "a contains b"},
		{`title="a>=b"`, "title", "=", "a>=b"},
		{`title contains "x = y"`, "title", "contains", "x = y"},
		{`views>=10`, "views", ">=", "10"},
		{`views<=10`, "views", "<=", "10"},
		{`views<10`, "views", "<", "10"},
		{`status in ["a", "b"]`, "status", "in", `["a", "b"]`},
	} {
		q, err := ParseDSL("Post.where(" + tc.in + ")")
		if err != nil {
			t.Errorf("%s: %v", tc.in, err)
			continue
		}
		if len(q.Filters) != 1 {
			t.Errorf("%s: %d filters, want 1", tc.in, len(q.Filters))
			continue
		}
		f := q.Filters[0]
		if f.Field != tc.field || f.Operator != tc.op || f.Value != tc.value {
			t.Errorf("%s: got %q %q %q, want %q %q %q", tc.in, f.Field, f.Operator, f.Value, tc.field, tc.op, tc.value)
		}
	}
}
