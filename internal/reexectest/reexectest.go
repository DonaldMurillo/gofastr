// Package reexectest holds the parent-side harness for tests that pin
// process-level panic isolation by re-execing the test binary: the child
// scenario runs behind an env gate inside the same test, and the parent
// asserts the child process survived it. The child side (the scenario and
// its exit codes) stays with each test; this package owns only the spawn
// and the triage of its outcome.
//
// The package has no test files of its own by design: its behavior is
// exercised through the packages that import it (see
// scripts/coverage-floors.sh on helper packages).
package reexectest

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// RunChild re-execs the test binary running only the test named testName,
// with childEnv=1 in the child's environment, and triages the outcome as
// a process-survival pin:
//
//   - exit 0 with no panic in the output: the panic was contained, and
//     nothing is reported;
//   - exit >= 10: the child scenario's own contract broke (a harness
//     bug), failing the parent loudly instead of reading as a finding;
//   - any other failure — notably the runtime-crash exit 2 — means the
//     panic escaped its goroutine and killed the process: the SECURITY
//     finding, reported verbatim as finding;
//   - exit 0 but a panic in the output: the child survived yet reported
//     an unrecovered panic, reported under tag (finding's "[family]"
//     prefix).
//
// Callers keep their SECURITY wording: finding and tag carry it verbatim.
func RunChild(t *testing.T, testName, childEnv, tag, finding string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run", "^"+testName+"$", "-test.count=1")
	cmd.Env = append(os.Environ(), childEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() >= 10 {
			t.Fatalf("child scenario contract broken (%s, exit %d):\n%s", testName, exitErr.ExitCode(), out)
		}
		t.Errorf("SECURITY: %s: %v\n--- child output ---\n%s", finding, err, out)
		return
	}
	if strings.Contains(string(out), "panic:") {
		t.Errorf("SECURITY: %s child survived but reported a panic:\n%s", tag, out)
	}
}
