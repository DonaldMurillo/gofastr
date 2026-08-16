package framework_test

import (
	"os/exec"
	"strings"
	"testing"
)

// framework/ARCHITECTURE.md fixes the layering: core-ui sits below every
// framework package, framework subpackages sit below the framework root
// facade, and framework/uihost stays below the framework/ui component
// layer. The gofastr verify gate enforces layer order but permits
// intra-layer edges, so until now these three invariants held by
// convention only. Same enforcement style as framework/ui/layering_test.go
// and framework/crud/layering_test.go: go list, then fail on the edge.

const modRoot = "github.com/DonaldMurillo/gofastr"

// The framework root package is the facade: it imports the subpackages,
// never the reverse. A subpackage that pulls the root into its dependency
// closure closes a cycle the facade pattern exists to prevent. One edge is
// sanctioned: pluginhost imports battery/auth (the //gofastr:allow(GOFASTR1301)
// exception in pluginhost/gate.go), and battery/auth links the root — so
// pluginhost is pinned here as the only package allowed to carry the root
// transitively. If that edge ever goes away, this test fails too, so the
// exemption cannot outlive its reason.
func TestFrameworkPkgsDontImportRoot(t *testing.T) {
	root := modRoot + "/framework"
	sanctioned := map[string]bool{root + "/pluginhost": true}
	out, err := exec.Command("go", "list",
		"-f", "{{.ImportPath}}: {{join .Deps \" \"}}",
		root+"/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list %s/...: %v\n%s", root, err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		pkg, deps, ok := strings.Cut(line, ": ")
		if !ok || pkg == root {
			continue
		}
		hasRoot := false
		for _, dep := range strings.Fields(deps) {
			if dep == root {
				hasRoot = true
			}
		}
		switch {
		case hasRoot && !sanctioned[pkg]:
			t.Errorf("%s depends on the framework root facade — move the "+
				"shared code into a subpackage both sides can import", pkg)
		case !hasRoot && sanctioned[pkg]:
			t.Errorf("%s no longer depends on the root — delete its exemption "+
				"from this test so the allowlist cannot rot", pkg)
		}
	}
}

// framework/uihost is the SSR host below the component layer. It serves
// whatever screens the app hands it; pulling framework/ui in would bake
// the component catalog into every host binary and invert the layering.
func TestUIHostDoesNotLinkFrameworkUI(t *testing.T) {
	const banned = modRoot + "/framework/ui"
	out, err := exec.Command("go", "list", "-deps",
		modRoot+"/framework/uihost").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps framework/uihost: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == banned {
			t.Errorf("framework/uihost depends on %s — the host renders screens "+
				"it is handed; components stay above it", banned)
		}
	}
}

// core-ui is the bottom layer: framework builds on it, never the other
// way around. Any framework/... dep from a core-ui package is an
// inverted edge.
func TestCoreUIDoesNotImportFramework(t *testing.T) {
	const banned = modRoot + "/framework"
	out, err := exec.Command("go", "list",
		"-f", "{{.ImportPath}}: {{join .Deps \" \"}}",
		modRoot+"/core-ui/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list core-ui/...: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		pkg, deps, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		for _, dep := range strings.Fields(deps) {
			if dep == banned || strings.HasPrefix(dep, banned+"/") {
				t.Errorf("%s depends on %s — core-ui sits below framework; "+
					"invert the edge or move the code up", pkg, dep)
			}
		}
	}
}
