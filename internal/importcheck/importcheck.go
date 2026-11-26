// Package importcheck is the body of the TestPrimitiveImports that
// cmd/primitives generates into each package under an imports rule.
package importcheck

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// Ban is one package the package under test must not link, as its
// primitive declares it.
type Ban struct {
	Deny      string
	Primitive string
	Why       string
	Instead   string
}

// Run fails t for every banned package in the dependency closure of the
// package whose test called it. The closure is the one go list builds
// for the host OS and the default build tags: an import in a file behind
// another GOOS or a build tag is not seen.
func Run(t *testing.T, bans ...Ban) {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("go list -deps: %v\n%s", err, exit.Stderr)
		}
		t.Fatalf("go list -deps: %v", err)
	}
	deps := map[string]bool{}
	for line := range strings.SplitSeq(string(out), "\n") {
		deps[strings.TrimSpace(line)] = true
	}
	if len(deps) < 2 {
		t.Fatalf("go list -deps named no dependency: the check would pass on anything\n%s", out)
	}
	for _, ban := range bans {
		if deps[ban.Deny] {
			t.Errorf("this package links %s, which primitive %s forbids.\nWhy: %s\nInstead: %s", ban.Deny, ban.Primitive, ban.Why, ban.Instead)
		}
	}
}
