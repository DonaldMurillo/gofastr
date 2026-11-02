package agentscmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework/agentsinv"
)

// Property: every file `gofastr agents` (and `gofastr init --reinit`,
// which calls the same writers) puts in a project stays inside that
// project. A symlink in the project, whether a directory on the path
// or the file itself, is followed only while it stays in the project;
// one that leads out is refused or replaced, never written through.
//
// Before the fix the writers joined paths onto the project directory and
// wrote with os.WriteFile / atomicfile.Rewrite, both of which follow
// links: a cloned repo carrying .claude/skills/gofastr-host/SKILL.md ->
// ~/.bashrc had `gofastr agents skill` overwrite the user's .bashrc with
// the skill markdown, and a .agents -> /elsewhere link had sync create
// the detail files there. Only the legacy cleanup went through os.Root.
func TestAgentFilesStayInsideTheProject(t *testing.T) {
	const precious = "precious user content\n"
	symlink := func(t *testing.T, target, link string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	victimFile := func(t *testing.T) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "victim")
		if err := os.WriteFile(p, []byte(precious), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	unchanged := func(t *testing.T, p string) {
		t.Helper()
		if got, err := os.ReadFile(p); err != nil || string(got) != precious {
			t.Errorf("SECURITY: [agents-symlink-escape] a file outside the project was rewritten through a symlink: %q, %v", got, err)
		}
	}
	empty := func(t *testing.T, d string) {
		t.Helper()
		ents, err := os.ReadDir(d)
		if err != nil {
			t.Fatal(err)
		}
		if len(ents) != 0 {
			t.Errorf("SECURITY: [agents-symlink-escape] files were created outside the project through a symlink: %v", ents)
		}
	}
	absent := func(t *testing.T, p string) {
		t.Helper()
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("SECURITY: [agents-symlink-escape] %s was created outside the project through a dangling symlink", p)
		}
	}
	if len(agentsinv.All()) == 0 {
		t.Fatal("setup broken: no inventory entries registered")
	}
	firstDetail := agentDetailRelPath(agentsinv.All()[0])

	t.Run("skill file linked out", func(t *testing.T) {
		dir, victim := t.TempDir(), victimFile(t)
		symlink(t, victim, filepath.Join(dir, ".claude", "skills", "gofastr-host", "SKILL.md"))
		_ = WriteHostSkill(dir)
		unchanged(t, victim)
	})
	t.Run("skill directory linked out", func(t *testing.T) {
		dir, out := t.TempDir(), t.TempDir()
		symlink(t, out, filepath.Join(dir, ".claude"))
		_ = WriteHostSkill(dir)
		empty(t, out)
	})
	t.Run("detail directory linked out", func(t *testing.T) {
		dir, out := t.TempDir(), t.TempDir()
		symlink(t, out, filepath.Join(dir, AgentsDetailDir))
		_ = WriteAgentDetailFiles(dir)
		empty(t, out)
	})
	t.Run("detail file dangling out", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(t.TempDir(), "created")
		symlink(t, target, filepath.Join(dir, filepath.FromSlash(firstDetail)))
		_ = WriteAgentDetailFiles(dir)
		absent(t, target)
	})
	t.Run("init AGENTS.md dangling out", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(t.TempDir(), "created")
		symlink(t, target, filepath.Join(dir, "AGENTS.md"))
		clitest.CovT_capExit(t, func() {
			clitest.CovT_capStdout(t, func() { runAgentsInit([]string{dir}) })
		})
		absent(t, target)
	})
	t.Run("sync AGENTS.md linked out", func(t *testing.T) {
		dir := t.TempDir()
		victim := filepath.Join(t.TempDir(), "AGENTS.md")
		stale := "mine\n" + agentsAutoStart + "\nSTALE\n" + agentsAutoEnd + "\n"
		if err := os.WriteFile(victim, []byte(stale), 0o644); err != nil {
			t.Fatal(err)
		}
		symlink(t, victim, filepath.Join(dir, "AGENTS.md"))
		clitest.CovT_capExit(t, func() {
			clitest.CovT_capStdout(t, func() { runAgentsSync([]string{dir}) })
		})
		if got, _ := os.ReadFile(victim); string(got) != stale {
			t.Errorf("SECURITY: [agents-symlink-escape] sync rewrote an AGENTS.md outside the project through a symlink")
		}
	})

	// GREEN guard: a link that stays in the project is still written
	// through, so a project keeping AGENTS.md as a link to a file of its
	// own keeps the link and gets the refresh.
	t.Run("link inside the project is followed", func(t *testing.T) {
		dir := t.TempDir()
		real := filepath.Join(dir, "docs", "AGENTS.md")
		if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
			t.Fatal(err)
		}
		stale := "mine\n" + agentsAutoStart + "\nSTALE\n" + agentsAutoEnd + "\n"
		if err := os.WriteFile(real, []byte(stale), 0o644); err != nil {
			t.Fatal(err)
		}
		symlink(t, filepath.Join("docs", "AGENTS.md"), filepath.Join(dir, "AGENTS.md"))
		clitest.CovT_capStdout(t, func() { runAgentsSync([]string{dir}) })
		got, err := os.ReadFile(real)
		if err != nil || strings.Contains(string(got), "STALE") || !strings.HasPrefix(string(got), "mine\n") {
			t.Fatalf("sync did not refresh the in-project link target: %q, %v", got, err)
		}
		if fi, err := os.Lstat(filepath.Join(dir, "AGENTS.md")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("the in-project AGENTS.md link was replaced: %v, %v", fi, err)
		}
	})
}
