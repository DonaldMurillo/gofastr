package project

import (
	"testing"
)

func TestNormalizeFrameworkVersion(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"0.39.1", "v0.39.1"},
		{"v0.39.1", "v0.39.1"},
		{"dev", ""},
		{"(devel)", ""},
		{"not-a-version", ""},
	} {
		if got := normalizeFrameworkVersion(tc.in); got != tc.want {
			t.Errorf("normalizeFrameworkVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
