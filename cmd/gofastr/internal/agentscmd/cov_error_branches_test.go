package agentscmd

import (
	"os"
	"path/filepath"
	"testing"
)

// ── skill.go write-failure branches ───────────────────────────────────

func TestWriteHostSkillMkdirFailure(t *testing.T) {
	// A regular file where the .claude tree must be created → MkdirAll fails.
	base := t.TempDir()
	blocker := filepath.Join(base, "blk")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteHostSkill(blocker); err == nil {
		t.Fatal("writeHostSkill should fail when target path is a file")
	}
}
