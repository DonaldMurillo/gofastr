package agentscmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// `gofastr agents init --force` (and sync, skill) took the flag as the
// project directory: init and skill created a directory named "--force",
// wrote AGENTS.md, .agents/ and the skill into it, and reported success
// with exit 0. A second positional was dropped without a word. Each verb
// now refuses a flag and a second argument before it writes anything.
func TestAgentsVerbsRefuseFlagsAndExtraArgs(t *testing.T) {
	verbs := map[string]func([]string){
		"init":  runAgentsInit,
		"sync":  runAgentsSync,
		"skill": runAgentsSkill,
	}
	for name, run := range verbs {
		for _, args := range [][]string{{"--force"}, {"-f"}, {".", "extra"}, {"--dir=x"}} {
			dir := t.TempDir()
			clitest.CovT_chdir(t, dir)
			code := -1
			out := clitest.CovT_capStdout(t, func() {
				code = clitest.CovT_capExit(t, func() { run(args) })
			})
			if code != 2 {
				t.Errorf("agents %s %q: exit %d, want 2:\n%s", name, args, code, out)
			}
			if !strings.Contains(out, "Usage: gofastr agents") {
				t.Errorf("agents %s %q printed no usage:\n%s", name, args, out)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				t.Errorf("agents %s %q wrote %s", name, args, filepath.Join(dir, e.Name()))
			}
		}
	}
}

// A directory argument still works, the dash-free spelling of the same
// call.
func TestAgentsInitTakesADirectory(t *testing.T) {
	dir := t.TempDir()
	code := -1
	clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { runAgentsInit([]string{dir}) })
	})
	if code != -1 {
		t.Fatalf("agents init <dir> exited %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
}
