package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestActivateRefusesSKILLmdSwappedForSymlinkAfterLoad(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	dir := filepath.Join(root, "s")
	writeSkill(t, dir, "s", "d", "inside body")
	writeSkill(t, outside, "s", "d", "OUTSIDE-BODY")
	r := NewRegistry(root)
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	skillmd := filepath.Join(dir, "SKILL.md")
	if err := os.Remove(skillmd); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "SKILL.md"), skillmd); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if body, err := r.Activate("s"); err == nil {
		t.Fatalf("Activate read through a symlinked SKILL.md: %q", body)
	}
}
