package fuzzy

import "testing"

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"kitten", "sitting", 3},
		{"status", "statuss", 1}, // insertion
		{"status", "stauts", 2},  // transposition = 2 edits
		{"score", "scor", 1},     // deletion
	}
	for _, c := range cases {
		if got := Levenshtein(c.a, c.b); got != c.want {
			t.Errorf("Levenshtein(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
		// Symmetric.
		if got := Levenshtein(c.b, c.a); got != c.want {
			t.Errorf("Levenshtein(%q,%q)=%d want %d (asymmetric)", c.b, c.a, got, c.want)
		}
	}
}
