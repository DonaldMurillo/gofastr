package ui

import "testing"

// TestParseButtonVariant pins the recognised spellings: exactly the
// four registered variants, case-sensitive, and ok=false for anything
// else so every caller keeps its own default.
func TestParseButtonVariant(t *testing.T) {
	cases := []struct {
		in   string
		want ButtonVariant
		ok   bool
	}{
		{"primary", ButtonPrimary, true},
		{"secondary", ButtonSecondary, true},
		{"danger", ButtonDanger, true},
		{"ghost", ButtonGhost, true},
		{"", ButtonVariant(""), false},
		{"Primary", ButtonVariant(""), false},
		{"SECONDARY", ButtonVariant(""), false},
		{"link", ButtonVariant(""), false},
		{"outline", ButtonVariant(""), false},
		{"bogus", ButtonVariant(""), false},
	}
	for _, c := range cases {
		got, ok := ParseButtonVariant(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseButtonVariant(%q) = (%q, %v), want (%q, %v)",
				c.in, got, ok, c.want, c.ok)
		}
	}
}
