package agentscmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// linkedAgentsMD makes base/proj whose AGENTS.md links to base/outside/AGENTS.md,
// writing body there unless it is empty (a dangling link).
func linkedAgentsMD(t *testing.T, body string) (proj, victim string) {
	t.Helper()
	base := t.TempDir()
	proj, outside := filepath.Join(base, "proj"), filepath.Join(base, "outside")
	for _, d := range []string{proj, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	victim = filepath.Join(outside, "AGENTS.md")
	if body != "" {
		if err := os.WriteFile(victim, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(victim, filepath.Join(proj, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	return proj, victim
}

func agentsExit(t *testing.T, fn func()) int {
	t.Helper()
	var code int
	clitest.CovT_capStdout(t, func() { code = clitest.CovT_capExit(t, fn) })
	return code
}

// `gofastr agents sync` replaced AGENTS.md through atomicfile.Rewrite,
// which resolves a leaf symlink and replaces the file it names, so an
// AGENTS.md linked out of the project had the outside file rewritten
// with the refreshed table of contents. sync refuses that link and
// leaves the outside file as it was.
func TestAgentsSyncRefusesAnAGENTSmdLinkedOutside(t *testing.T) {
	stale := "# Mine\n\n" + agentsAutoStart + "\n\nstale table of contents\n\n" + agentsAutoEnd + "\n"
	proj, victim := linkedAgentsMD(t, stale)
	if code := agentsExit(t, func() { runAgentsSync([]string{proj}) }); code != 1 {
		t.Errorf("agents sync through a linked AGENTS.md exited %d, want 1", code)
	}
	if got, _ := os.ReadFile(victim); string(got) != stale {
		t.Errorf("agents sync rewrote %s outside the project:\n%s", victim, got)
	}
}

// `gofastr agents init` writes AGENTS.md with os.WriteFile, which
// creates a dangling link's target and replaces an existing one. Any
// AGENTS.md already there, a link included, is refused; nothing is
// written outside.
func TestAgentsInitRefusesAnAGENTSmdLinkedOutside(t *testing.T) {
	for _, dangling := range []bool{true, false} {
		t.Run(map[bool]string{true: "dangling", false: "existing"}[dangling], func(t *testing.T) {
			const precious = "precious\n"
			body := precious
			if dangling {
				body = ""
			}
			proj, victim := linkedAgentsMD(t, body)
			if code := agentsExit(t, func() { runAgentsInit([]string{proj}) }); code != 1 {
				t.Errorf("agents init through a linked AGENTS.md exited %d, want 1", code)
			}
			got, err := os.ReadFile(victim)
			if dangling && err == nil {
				t.Errorf("agents init created %s outside the project", victim)
			}
			if !dangling && string(got) != precious {
				t.Errorf("agents init replaced %s outside the project: %q", victim, got)
			}
		})
	}
}
