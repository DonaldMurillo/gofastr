package pgtest

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Required mode must stay fail-closed PAST BaseDSN. With TEST_POSTGRES_DSN
// set to a keyword/value (non-URL) string, resolve() succeeds — env DSNs are
// taken as-is — and the URL-shape checks in DB/FreshDatabaseDSN/UnusedDSN
// then skip. Under PGTEST_REQUIRED that turns the CI canary into a silent
// skip through the exact override CONTRIBUTING.md advertises, which is the
// false-green the canary exists to prevent. This drives the canary in a
// subprocess and demands a FAILURE, not a skip.
func TestRequiredModeRejectsNonURLDSN(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs the package in a subprocess")
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "TestPGHarnessRequired$", ".")
	cmd.Env = append(os.Environ(),
		"PGTEST_REQUIRED=1",
		"TEST_POSTGRES_DSN=host=localhost port=5432 dbname=pgtest sslmode=disable",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("required-mode canary skipped (passed) on a non-URL DSN instead of failing:\n%s", out)
	}
	if !strings.Contains(string(out), "URL-form") {
		t.Fatalf("canary failed for a different reason than the URL-shape guard:\n%s", out)
	}
}
