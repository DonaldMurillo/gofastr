package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunAgentsInitWriteFailureExits(t *testing.T) {
	// Target dir is a regular file → writing dir/AGENTS.md fails.
	blocker := filepath.Join(t.TempDir(), "blk")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runAgentsInit([]string{blocker}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunAgentsSkillFailureExits(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blk")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runAgentsSkill([]string{blocker}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunAgentsSyncCorruptMarkersExits(t *testing.T) {
	dir := t.TempDir()
	// AGENTS.md present but with duplicate markers → refreshAgentsMD errors.
	body := "preamble\n" + agentsAutoStart + "\nx\n" + agentsAutoEnd + "\n" + agentsAutoStart + "\ny\n" + agentsAutoEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runAgentsSync([]string{dir}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunAgentsSyncChangedRewrites(t *testing.T) {
	dir := t.TempDir()
	// Fresh init, then mutate the auto section so sync detects a change.
	covT_capStdout(t, func() { runAgentsInit([]string{dir}) })
	body, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Replace the content between markers with a stale value.
	stale := string(body)
	if i := indexOf(stale, agentsAutoStart); i >= 0 {
		stale = stale[:i] + agentsAutoStart + "\nSTALE\n" + agentsAutoEnd + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	covT_capStdout(t, func() { runAgentsSync([]string{dir}) })
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
