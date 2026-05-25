package main

import (
	"strings"
	"testing"
)

// TestColors_NoANSIWhenNotTTY pins the operator-quality fix: when
// stdout isn't a terminal (the typical CI / piped / `go test`
// scenario), ANSI escape codes must NOT be emitted — piping
// `gofastr docs` through `less` or `wc -l` should not see
// `\033[32m...` garbage.
//
// Under `go test`, stdout is a pipe to the test runner, so colorize
// helpers MUST return the input unchanged.
func TestColors_NoANSIWhenNotTTY(t *testing.T) {
	cases := []struct {
		name string
		got  string
	}{
		{"green", green("hello")},
		{"red", red("hello")},
		{"bold", bold("hello")},
		{"dim", dim("hello")},
		{"yellow", yellow("hello")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.got, "\x1b[") {
				t.Errorf("%s(): emitted ANSI escape when not on a TTY: %q", tc.name, tc.got)
			}
			if tc.got != "hello" {
				t.Errorf("%s(): expected raw passthrough, got %q", tc.name, tc.got)
			}
		})
	}
}
