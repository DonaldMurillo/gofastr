package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// Smoke test: a fresh `gofastr init` lands the AI-agent files
// alongside the app scaffold. Runs the real binary as a subprocess
// (runInit uses os.Exit so it can't be called in-process) so the test
// catches the silent-drift class where init.go forgets to call
// writeAgentDetailFiles or writeHostSkill after a refactor.
func TestInitDropsAIAgentFiles(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// Build the binary inside the gofastr module (where go.mod lives),
	// then invoke it from the tempdir; `go run` from outside any
	// module fails to resolve cmd/gofastr's imports.
	binDir := t.TempDir()
	binPath := clitest.TestExecutablePath(filepath.Join(binDir, "gofastr"))
	build := exec.Command("go", "build", "-o", binPath, "./cmd/gofastr")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build gofastr: %v\n%s", err, out)
	}

	work := t.TempDir()
	cmd := exec.Command(binPath, "init", "smoke", "--no-entity")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gofastr init: %v\n%s", err, out)
	}

	for _, rel := range []string{
		"smoke/AGENTS.md",
		"smoke/DESIGN.md",
		"smoke/.agents/framework.md",
		"smoke/.agents/battery-admin.md",
		"smoke/.agents/battery-log.md",
		"smoke/.agents/battery-print.md",
		"smoke/.claude/skills/gofastr-host/SKILL.md",
	} {
		path := filepath.Join(work, rel)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file missing after `gofastr init`: %s — init.go likely dropped a writer call\nstdout/stderr:\n%s", rel, out)
		}
	}

	// AGENTS.md must be the thin TOC, not the old 500-line inline form.
	body, _ := os.ReadFile(filepath.Join(work, "smoke", "AGENTS.md"))
	if !strings.Contains(string(body), "| Section | Use this when | Details |") {
		t.Error("init wrote AGENTS.md without the TOC header — regression to old inline shape")
	}

	// A detail file must carry the AUTO-GENERATED sentinel. That proves
	// writeAgentDetailFiles ran with the right header.
	detail, _ := os.ReadFile(filepath.Join(work, "smoke", ".agents", "framework.md"))
	if !strings.HasPrefix(string(detail), "<!-- AUTO-GENERATED") {
		t.Error(".agents/framework.md missing AUTO-GENERATED header — header writer regressed")
	}
}
