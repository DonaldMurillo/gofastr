package yaml

import "testing"

// A quote opens a quoted scalar only where a scalar begins. An
// apostrophe inside a plain scalar ("O'Reilly") is a character: it used
// to start quote mode, so a trailing comment stayed in the value and an
// inline list split on nothing and failed as unterminated.
func TestPlainApostropheIsNotAQuote(t *testing.T) {
	cases := []struct {
		in, key string
		want    any
	}{
		{"name: O'Reilly # note\n", "name", "O'Reilly"},
		{"name: it's \"fine\" # note\n", "name", `it's "fine"`},
		{"name: 'it''s # kept' # note\n", "name", "it's # kept"},
		{"name: \"a # b\" # note\n", "name", "a # b"},
		{"owner's: x # note\n", "owner's", "x"},
		{"'k # 1': v # note\n", "k # 1", "v"},
	}
	for _, c := range cases {
		n, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if got := n.Map[c.key]; got == nil || got.Value != c.want {
			t.Errorf("Parse(%q)[%q] = %#v, want %q", c.in, c.key, got, c.want)
		}
	}

	n, err := Parse("authors: [O'Reilly, Jane, 'a, b', \"c # d\"] # note\n")
	if err != nil {
		t.Fatalf("inline list with an apostrophe: %v", err)
	}
	want := []string{"O'Reilly", "Jane", "a, b", "c # d"}
	got := n.Map["authors"]
	if got == nil || len(got.List) != len(want) {
		t.Fatalf("inline list = %#v, want %q", got, want)
	}
	for i, w := range want {
		if got.List[i].Value != w {
			t.Errorf("item %d = %#v, want %q", i, got.List[i].Value, w)
		}
	}

	n, err = Parse("- O'Reilly # note\n- 'x # y'\n")
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if n.List[0].Value != "O'Reilly" || n.List[1].Value != "x # y" {
		t.Errorf("list items = %#v, %#v", n.List[0].Value, n.List[1].Value)
	}
}
