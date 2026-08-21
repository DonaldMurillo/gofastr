package evalrunner

import (
	"os/exec"
	"strings"
	"testing"
)

// Every module@version a baseline lane writes into its workspace go.mod must
// resolve, or the Gin and stdlib lanes die in setup and the eval cannot run
// at all. This rotted silently once: the sqlite driver swap carried mattn's
// v1.14.44 onto gofastr's own sqlite/stdlib path, which is not a module,
// and nothing noticed for days because evals/ is only ever compiled in CI.
// Distinguishes a bad pin (fail) from an unreachable proxy (skip).
func TestBaselineRequirementsResolve(t *testing.T) {
	if testing.Short() {
		t.Skip("hits the module proxy")
	}
	for _, mod := range baselineRequirements() {
		out, err := exec.Command("go", "list", "-m", mod).CombinedOutput()
		if err == nil {
			continue
		}
		s := string(out)
		for _, badPin := range []string{"unknown revision", "invalid version", "not a known dependency", "malformed module path", "no matching versions"} {
			if strings.Contains(s, badPin) {
				t.Fatalf("baseline requirement %s does not resolve — the eval's non-GoFastr lanes are unrunnable:\n%s", mod, s)
			}
		}
		t.Skipf("module proxy unreachable, cannot verify %s: %v\n%s", mod, err, s)
	}
}
