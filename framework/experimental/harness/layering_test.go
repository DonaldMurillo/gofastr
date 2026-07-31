package harness

import (
	"os/exec"
	"strings"
	"testing"
)

// The framework root (github.com/DonaldMurillo/gofastr/framework) is the
// public facade plus the App spine. It must not depend on the harness,
// which is an out-of-contract, experimental subsystem (see
// framework/ARCHITECTURE.md → out-of-contract block). A root→harness edge
// would pull an entire agent runtime into every host app that links the
// framework, and would close a cycle, since the harness is itself a
// consumer of framework/.
//
// This is the enforceable form of that boundary. A future "just import it
// here, it's simpler" refactor fails here instead of silently reversing
// the layering. The shape mirrors framework/ui/layering_test.go and
// framework/crud/layering_test.go.
func TestFrameworkRootDoesNotImportHarness(t *testing.T) {
	const root = "github.com/DonaldMurillo/gofastr/framework"
	const harnessPrefix = "github.com/DonaldMurillo/gofastr/framework/experimental/harness"

	out, err := exec.Command("go", "list", "-deps", root).CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s: %v\n%s", root, err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		pkg := strings.TrimSpace(line)
		if pkg == "" {
			continue
		}
		if strings.HasPrefix(pkg, harnessPrefix) {
			t.Errorf("framework root depends on experimental harness package %q — "+
				"the harness is an out-of-contract subsystem imported only by explicit "+
				"consumers (the `gofastr harness` subcommand), never by the framework facade", pkg)
		}
	}
}
