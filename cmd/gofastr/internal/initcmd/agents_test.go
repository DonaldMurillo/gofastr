package initcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// A stock CLAUDE.md from a release that wrote agents/ is unmodified, so
// reinit refreshes it to the .agents/ spelling without --force; an
// edited one is still left alone.
func TestReinitRefreshesLegacyStockClaudeMD(t *testing.T) {
	legacy := bytes.ReplaceAll(claudeMDContent(), []byte(".agents/"), []byte("agents/"))
	if bytes.Equal(legacy, claudeMDContent()) {
		t.Fatal("claudeMDContent no longer names .agents/: the legacy fixture proves nothing")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	clitest.CovT_capStdout(t, func() { runReinit(dir, false) })
	if got, _ := os.ReadFile(path); !bytes.Equal(got, claudeMDContent()) {
		t.Fatalf("legacy stock CLAUDE.md was not refreshed:\n%s", got)
	}

	edited := append(append([]byte{}, legacy...), "\nOur own rule.\n"...)
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	clitest.CovT_capStdout(t, func() { runReinit(dir, false) })
	if got, _ := os.ReadFile(path); !bytes.Equal(got, edited) {
		t.Fatalf("an edited CLAUDE.md must be left alone:\n%s", got)
	}
}
