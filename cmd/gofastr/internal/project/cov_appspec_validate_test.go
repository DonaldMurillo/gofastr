package project

import (
	"testing"
)

func TestIsGoIdentifier(t *testing.T) {
	good := []string{"_x", "Foo", "bar9", "A_b2"}
	for _, s := range good {
		if !IsGoIdentifier(s) {
			t.Errorf("isGoIdentifier(%q) = false, want true", s)
		}
	}
	bad := []string{"", "9x", "a-b", "a b", "a.b"}
	for _, s := range bad {
		if IsGoIdentifier(s) {
			t.Errorf("isGoIdentifier(%q) = true, want false", s)
		}
	}
}
