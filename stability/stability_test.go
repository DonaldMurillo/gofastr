package stability

import (
	"os/exec"
	"strings"
	"testing"
)

// modulePackages returns every package in the module via `go list ./...`.
func modulePackages(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "./...").Output()
	if err != nil {
		t.Fatalf("go list ./...: %v", err)
	}
	var pkgs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			pkgs = append(pkgs, line)
		}
	}
	if len(pkgs) == 0 {
		t.Fatal("go list returned no packages")
	}
	return pkgs
}

// TestEveryPackageIsClassified fails when any package in the module has no
// tier. Adding a package under a new top-level tree therefore breaks the build
// until it is classified on purpose in manifest — the enforcement half of the
// support policy in docs/public-api.md.
func TestEveryPackageIsClassified(t *testing.T) {
	var unclassified []string
	for _, pkg := range modulePackages(t) {
		if _, ok := Classify(pkg); !ok {
			unclassified = append(unclassified, pkg)
		}
	}
	if len(unclassified) > 0 {
		t.Fatalf("packages missing a stability tier — add each to the manifest in stability.go:\n  %s",
			strings.Join(unclassified, "\n  "))
	}
}

// TestNoStableBeforeV1 pins the invariant that the pre-v1 tree freezes nothing.
// When the v1 freeze begins, delete this test in the same change that marks the
// first package Stable, and record the decision in the commit message.
func TestNoStableBeforeV1(t *testing.T) {
	for _, r := range manifest {
		if r.tier == Stable {
			t.Errorf("package prefix %q is marked Stable before v1.0.0; the freeze is an explicit release act — see docs/public-api.md", r.prefix)
		}
	}
}

func TestClassifyLongestPrefixWins(t *testing.T) {
	cases := []struct {
		path string
		want Tier
	}{
		{ModulePath + "/framework/crud", Provisional},
		{ModulePath + "/framework/experimental/apiversions", Experimental},
		{ModulePath + "/kiln/journal", Experimental},
		{ModulePath + "/codegen", Experimental},
		{ModulePath + "/battery/auth", Provisional},
		{ModulePath + "/examples/site", Excluded},
		{ModulePath + "/sqlite/stdlib", Provisional},
		{ModulePath + "/cmd/gofastr", Provisional},
	}
	for _, c := range cases {
		got, ok := Classify(c.path)
		if !ok {
			t.Errorf("%s: not classified", c.path)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %s, want %s", c.path, got, c.want)
		}
	}
}

func TestClassifyInternalAlwaysWins(t *testing.T) {
	for _, p := range []string{
		ModulePath + "/internal/foo",
		ModulePath + "/framework/internal/bar",
		ModulePath + "/core-ui/widget/internal",
	} {
		got, ok := Classify(p)
		if !ok || got != Internal {
			t.Errorf("%s: got (%s, %v), want (internal, true)", p, got, ok)
		}
	}
}
