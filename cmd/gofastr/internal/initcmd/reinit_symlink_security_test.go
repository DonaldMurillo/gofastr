package initcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// Property: `gofastr init --reinit` writes only inside the project it
// refreshes, the same rule the `gofastr agents` writers hold
// (agentscmd TestAgentFilesStayInsideTheProject). AGENTS.md, CLAUDE.md
// and DESIGN.md went through os.ReadFile / atomicfile.Rewrite, which
// follow a symlink anywhere: `--reinit --force` in a cloned repo whose
// CLAUDE.md links to ~/.bashrc overwrote ~/.bashrc, and a dangling
// DESIGN.md link created its target wherever it pointed.
func TestReinitStaysInsideTheProject(t *testing.T) {
	const precious = "precious user content\n"
	reinit := func(dir string) {
		clitest.CovT_capExit(t, func() {
			clitest.CovT_capStdout(t, func() { runReinit(dir, true) })
		})
	}
	link := func(t *testing.T, dir, name, target string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		t.Run(name+" linked out", func(t *testing.T) {
			dir := t.TempDir()
			victim := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(victim, []byte(precious), 0o644); err != nil {
				t.Fatal(err)
			}
			link(t, dir, name, victim)
			reinit(dir)
			if got, err := os.ReadFile(victim); err != nil || string(got) != precious {
				t.Errorf("SECURITY: [reinit-symlink-escape] reinit --force rewrote %s outside the project through a symlink: %.60q, %v", name, got, err)
			}
		})
	}
	t.Run("DESIGN.md dangling out", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(t.TempDir(), "DESIGN.md")
		link(t, dir, "DESIGN.md", target)
		reinit(dir)
		if _, err := os.Lstat(target); err == nil {
			t.Error("SECURITY: [reinit-symlink-escape] reinit created DESIGN.md outside the project through a dangling symlink")
		}
	})
}
