package framework_test

import (
	"os"
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

// ARCHITECTURE.md's "Out-of-contract" list: these trees are exempt from
// the layering rules (test helpers, fixtures, dev tooling, experimental).
func outOfContract(pkg string) bool {
	root := modRoot + "/framework"
	for _, prefix := range []string{
		root + "/experimental",
		root + "/testkit",
		root + "/factory",
		root + "/isolation",
	} {
		if pkg == prefix || strings.HasPrefix(pkg, prefix+"/") {
			return true
		}
	}
	return false
}

// The framework root package is the facade: it imports the subpackages,
// never the reverse. A subpackage that pulls the root into its dependency
// closure closes a cycle the facade pattern exists to prevent. One edge is
// sanctioned: pluginhost imports battery/auth (the //gofastr:allow(GOFASTR1301)
// exception in pluginhost/gate.go), and battery/auth links the root, so
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
		if !ok || pkg == root || outOfContract(pkg) {
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

	// The exemption is valid only via the documented route: pluginhost's
	// direct battery/auth import, annotated //gofastr:allow(GOFASTR1301) in
	// gate.go. If the route or its annotation is gone, the root dependency
	// arrived some other way and must not ride the old carve-out.
	imp, err := exec.Command("go", "list", "-f", "{{join .Imports \" \"}}",
		root+"/pluginhost").CombinedOutput()
	if err != nil {
		t.Fatalf("go list pluginhost imports: %v\n%s", err, imp)
	}
	if !strings.Contains(" "+string(imp)+" ", " "+modRoot+"/battery/auth ") {
		t.Error("pluginhost no longer imports battery/auth — its root exemption " +
			"above no longer describes reality; re-derive how the root enters " +
			"its closure and cut or re-sanction that edge")
	}
	gate, err := os.ReadFile("pluginhost/gate.go")
	if err != nil {
		t.Fatalf("read pluginhost/gate.go: %v", err)
	}
	if !strings.Contains(string(gate), "gofastr:allow(GOFASTR1301)") {
		t.Error("pluginhost/gate.go lost its gofastr:allow(GOFASTR1301) " +
			"annotation — the sanctioned edge this test exempts is no longer " +
			"marked in code")
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
