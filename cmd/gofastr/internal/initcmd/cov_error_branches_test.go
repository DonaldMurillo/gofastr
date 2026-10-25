package initcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// ── init write-failure branches ───────────────────────────────────────

func TestWriteCLAUDEmdFailure(t *testing.T) {
	if err := writeCLAUDEmd(filepath.Join(t.TempDir(), "missing", "deep")); err == nil {
		t.Fatal("writeCLAUDEmd should fail for missing parent dir")
	}
}

func TestRunReinitWriteFailureExits(t *testing.T) {
	// Pass a path whose AGENTS detail dir can't be created.
	blocker := filepath.Join(t.TempDir(), "blk")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runReinit(blocker, false) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}
