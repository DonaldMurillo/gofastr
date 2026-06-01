package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunAgentsNoSubcmdExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runAgents(nil) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunAgentsUnknownExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runAgents([]string{"bogus"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunAgentsInitAndSync(t *testing.T) {
	dir := t.TempDir()
	covT_capStdout(t, func() { runAgents([]string{"init", dir}) })
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatalf("AGENTS.md not written: %v", err)
	}
	// sync over existing file.
	covT_capStdout(t, func() { runAgents([]string{"sync", dir}) })
}

func TestRunAgentsInitExistingExits(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runAgentsInit([]string{dir}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunAgentsSyncMissingFileExits(t *testing.T) {
	dir := t.TempDir()
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runAgentsSync([]string{dir}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunAgentsSkill(t *testing.T) {
	dir := t.TempDir()
	covT_capStdout(t, func() { runAgents([]string{"skill", dir}) })
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "gofastr-host", "SKILL.md")); err != nil {
		t.Fatalf("skill not written: %v", err)
	}
}

func TestWarnMissingMarkdownNoCrash(t *testing.T) {
	covT_capStdout(t, warnMissingMarkdown)
}

func TestPluralY(t *testing.T) {
	if pluralY(1) != "y" || pluralY(2) != "ies" {
		t.Fatal("pluralY")
	}
}
