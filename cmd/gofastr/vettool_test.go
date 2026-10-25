package main

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/verifycmd"
)

// GOFASTR_VETTOOL left in a shell must not turn `gofastr docs` into
// the analyzer driver: only vet's own calls hand off.
func TestVettoolEnvNeedsVetArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"docs"}, false},
		{[]string{"verify", "./..."}, false},
		{[]string{"-V=full"}, true},
		{[]string{"-flags"}, true},
		{[]string{"-printf.funcs=Logf", "/tmp/b001/vet.cfg"}, true},
	} {
		if got := verifycmd.IsVetCall(c.args); got != c.want {
			t.Errorf("isVetCall(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}
