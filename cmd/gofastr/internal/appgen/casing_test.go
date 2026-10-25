package appgen

import (
	"testing"
)

func TestLowerFirst(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"A", "a"},
		{"Hello", "hello"},
		{"XMLReader", "xMLReader"},
	}
	for _, c := range cases {
		if got := lowerFirst(c.in); got != c.want {
			t.Errorf("lowerFirst(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
