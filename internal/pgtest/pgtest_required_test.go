package pgtest

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRequiredModeRejectsInvalidDSN(t *testing.T) {
	const secret = "fake-secret-marker"
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestPGHarnessRequired$")
	cmd.Env = append(os.Environ(),
		"PGTEST_REQUIRED=1",
		"TEST_POSTGRES_DSN=host=localhost password='"+secret,
	)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "invalid TEST_POSTGRES_DSN") || strings.Contains(string(out), secret) {
		t.Fatalf("required-mode canary did not fail with a redacted DSN error: %v\n%s", err, out)
	}
}
