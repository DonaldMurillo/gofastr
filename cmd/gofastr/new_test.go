package main

import (
	"testing"
)

func TestNewRejectsTraversalNames(t *testing.T) {
	bad := []string{
		"../../evil",
		"../evil",
		"/abs/path",
		`win\path`,
		"a/b",
		"..",
		".",
		"",
		"foo/../bar",
	}
	for _, n := range bad {
		if err := validateScaffoldName(n); err == nil {
			t.Errorf("validateScaffoldName(%q) = nil, want error", n)
		}
	}
}

func TestNewAcceptsValidNames(t *testing.T) {
	good := []string{"User", "Post", "OrderItem", "user_profile", "x"}
	for _, n := range good {
		if err := validateScaffoldName(n); err != nil {
			t.Errorf("validateScaffoldName(%q) = %v, want nil", n, err)
		}
	}
}

func TestTitleASCII(t *testing.T) {
	cases := []struct{ in, want string }{
		{"user", "User"},
		{"User", "User"},
		{"", ""},
		{"x", "X"},
		{"123abc", "123abc"},
	}
	for _, c := range cases {
		if got := titleASCII(c.in); got != c.want {
			t.Errorf("titleASCII(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
