package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The scaffold's agent guidance must put the dev loop where an agent reads
// first. Before this, CLAUDE.md listed `gofastr dev` only in a closing
// quick-reference list and AGENTS.md only in a trigger-table row; the
// "not `go run .`" warning sat in agents/framework.md, which an agent
// finds only by grepping for it. evals/dev-loop measures the behavior;
// this pins the placement it depends on.
func TestScaffoldLeadsWithDevLoop(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() { runInit([]string{"devapp", "--no-entity"}) })

	for _, c := range []struct {
		file   string
		before string // the dev loop must appear above this line
	}{
		{"CLAUDE.md", "## Framework docs"},
		{"AGENTS.md", "## Two maps, then the detail"},
		{".claude/skills/gofastr-host/SKILL.md", "For UI work"},
	} {
		raw, err := os.ReadFile(filepath.Join(dir, "devapp", c.file))
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		dev := strings.Index(body, "`gofastr dev`")
		anchor := strings.Index(body, c.before)
		if dev < 0 || anchor < 0 || dev > anchor {
			t.Errorf("%s: `gofastr dev` (byte %d) must appear before %q (byte %d)", c.file, dev, c.before, anchor)
		}
		if run := strings.Index(body, "go run"); run >= 0 && run < dev {
			t.Errorf("%s: `go run` (byte %d) appears before `gofastr dev` (byte %d)", c.file, run, dev)
		}
		if !strings.Contains(body, "GOFASTR_DEV=1") {
			t.Errorf("%s: does not say why plain `go run .` never reloads (GOFASTR_DEV=1)", c.file)
		}
	}
}
