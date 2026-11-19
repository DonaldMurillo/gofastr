//go:build darwin && (arm64 || amd64) && !desktop_e2e

package macos

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// nativeCheck re-execs the test binary with no tests selected, so
// TestMain's env gate runs the native check named envVar in a fresh
// process: the checks need real AppKit state on the main thread without
// the testing framework's own scaffolding in the way. failure labels
// the fatal when the child exits nonzero.
func nativeCheck(t *testing.T, envVar, failure string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^$")
	command.Env = append(os.Environ(), envVar+"=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", failure, err, output)
	}
}
