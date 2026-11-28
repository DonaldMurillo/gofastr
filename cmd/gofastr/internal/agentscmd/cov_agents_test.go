package agentscmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework/agentsinv"
)

func TestRunAgentsNoSubcmdExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunAgents(nil) })
	})
	if code != 2 {
		t.Fatalf("want 2 got %d", code)
	}
}

func TestRunAgentsUnknownExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunAgents([]string{"bogus"}) })
	})
	if code != 2 {
		t.Fatalf("want 2 got %d", code)
	}
}

func TestRunAgentsInitAndSync(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_capStdout(t, func() { RunAgents([]string{"init", dir}) })
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatalf("AGENTS.md not written: %v", err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	out := clitest.CovT_capStdout(t, func() { RunAgents([]string{"sync", dir}) })
	if !strings.Contains(out, "already up to date") {
		t.Fatalf("sync of a fresh AGENTS.md should report up to date: %s", out)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md")); string(after) != string(before) {
		t.Fatal("sync rewrote an up-to-date AGENTS.md")
	}
}

func TestRunAgentsSyncRewritesDetailsWhenTOCUnchanged(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_capStdout(t, func() { RunAgents([]string{"init", dir}) })
	detail := filepath.Join(dir, agentDetailRelPath(agentsinv.All()[0]))
	want, err := os.ReadFile(detail)
	if err != nil {
		t.Fatalf("init did not write the detail file: %v", err)
	}
	if err := os.WriteFile(detail, []byte(autoGenSentinel+" STALE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := clitest.CovT_capStdout(t, func() { RunAgents([]string{"sync", dir}) })
	if !strings.Contains(out, "already up to date") {
		t.Fatalf("sync of an unchanged TOC should say so: %s", out)
	}
	got, err := os.ReadFile(detail)
	if err != nil {
		t.Fatalf("sync left no detail file behind an unchanged TOC: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("sync did not restore the generated detail file:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestRunAgentsInitExistingExits(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runAgentsInit([]string{dir}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunAgentsSyncMissingFileExits(t *testing.T) {
	dir := t.TempDir()
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runAgentsSync([]string{dir}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunAgentsSkill(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_capStdout(t, func() { RunAgents([]string{"skill", dir}) })
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "gofastr-host", "SKILL.md")); err != nil {
		t.Fatalf("skill not written: %v", err)
	}
}

func TestWarnMissingMarkdownNamesEachEntry(t *testing.T) {
	all, miss := agentsinv.All(), agentsinv.MissingMarkdown()
	t.Cleanup(func() {
		agentsinv.Reset(t)
		for _, e := range append(all, miss...) {
			agentsinv.Register(e)
		}
	})
	agentsinv.Reset(t)
	if out := clitest.CovT_capStdout(t, warnMissingMarkdown); out != "" {
		t.Fatalf("no missing entries should print nothing, got %q", out)
	}
	agentsinv.Register(agentsinv.Entry{Name: "ghost", Kind: agentsinv.KindBattery, ImportPath: "example.com/ghost"})
	out := clitest.CovT_capStdout(t, warnMissingMarkdown)
	for _, want := range []string{"1 entry registered without agents.md", "battery:ghost (example.com/ghost)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("warning missing %q: %s", want, out)
		}
	}
}

func TestPluralY(t *testing.T) {
	if pluralY(1) != "y" || pluralY(2) != "ies" {
		t.Fatal("pluralY")
	}
}
