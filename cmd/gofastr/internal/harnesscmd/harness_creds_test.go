package harnesscmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// TestHarnessCreds_Add verifies creds add stores a credential.
func TestHarnessCreds_Add(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("GOFASTR_HARNESS_PASSPHRASE", "test-pass")
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "")

	var exitCode int
	out := clitest.CovT_capStdout(t, func() {
		exitCode = clitest.CovT_capExit(t, func() {
			runHarnessCreds([]string{"add", "openrouter", "default", "sk-test-key"})
		})
	})
	if exitCode != -1 {
		t.Fatalf("creds add exited with code %d; output=%s", exitCode, out)
	}
	// Verify the cred file was written.
	credFile := filepath.Join(dir, "gofastr", "harness", "creds.enc")
	if _, err := os.Stat(credFile); err != nil {
		t.Fatalf("creds.enc not written: %v", err)
	}
}

// TestHarnessCreds_AddRetrieve verifies the stored cred is retrievable.
func TestHarnessCreds_AddRetrieve(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("GOFASTR_HARNESS_PASSPHRASE", "test-pass2")
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "")

	// Store.
	clitest.CovT_capStdout(t, func() {
		runHarnessCreds([]string{"add", "zai", "default", "z-secret-value"})
	})
	// List; should print the stored entry.
	out := clitest.CovT_capStdout(t, func() {
		runHarnessCreds([]string{"list"})
	})
	if !strings.Contains(out, "zai") {
		t.Fatalf("list output missing 'zai'; got: %s", out)
	}
}

// TestHarnessCreds_AddMissingArgs verifies error for too-few args.
func TestHarnessCreds_AddMissingArgs(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() {
			runHarnessCreds([]string{"add", "openrouter"}) // missing account + secret
		})
	})
	if code != 1 {
		t.Fatalf("expected exit 1 for missing args, got %d", code)
	}
}

// TestHarnessCreds_UnknownSubcmd verifies error for unknown subcommand.
func TestHarnessCreds_UnknownSubcmd(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() {
			runHarnessCreds([]string{"nope"})
		})
	})
	if code != 1 {
		t.Fatalf("expected exit 1 for unknown subcommand, got %d", code)
	}
}
