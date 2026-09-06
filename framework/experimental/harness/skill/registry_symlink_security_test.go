// Pins, found by the 2026-09-04 red-probe round, that skill-tree reads
// (tier-1 Load scan, tier-2 Activate, tier-3 SupportingFile) contained
// paths lexically only (Clean + ".." refusal / paths trusted from the
// walk), so a symlinked directory or SKILL.md inside the tree read
// content from outside it; fixed by routing every tree read through an
// *os.Root, which refuses symlink escapes in the kernel.
//
// Property: reads under the skill tree (or a skill directory) must stay
// there even when a path component is a symlink pointing outside it.
// A symlink whose target stays inside the root still reads.
//
// Surfaces: Registry.Load (SKILL.md scan), Registry.Activate (tier-2
// body), Registry.SupportingFile (tier-3 files).
package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkillTreeSymlinkEscapeRefused walks the three read tiers through
// symlinked components pointing outside the tree and expects refusal.
func TestSkillTreeSymlinkEscapeRefused(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	secret := "---\nname: evil\ndescription: outside tree\n---\n\nOUTSIDE-BODY"
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "run.sh"), []byte("#!/bin/sh\nOUTSIDE-SCRIPT\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// A clean skill (control).
	writeSkill(t, filepath.Join(root, "good"), "good", "g", "good body")

	// A skill whose scripts/ dir is a symlink out of the tree.
	bad := filepath.Join(root, "bad")
	writeSkill(t, bad, "bad", "b", "bad body")
	if err := os.Symlink(outside, filepath.Join(bad, "scripts")); err != nil {
		t.Fatal(err)
	}
	// A skill with a leaf symlink out of the tree.
	if err := os.Symlink(filepath.Join(outside, "run.sh"), filepath.Join(bad, "leak.sh")); err != nil {
		t.Fatal(err)
	}
	// A skill directory whose SKILL.md is itself a symlink out.
	linked := filepath.Join(root, "linked")
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(linked, "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(root)
	_ = r.Load() // errors on the refused symlinked SKILL.md; the rest loads

	// Tier 1: the outside-tree skill must not be indexed.
	for _, name := range r.Names() {
		if name == "evil" {
			t.Error("SECURITY: [skill-symlink] Load indexed a SKILL.md symlinked outside the skill tree")
		}
	}

	// Tier 2: Activate of the symlinked skill must not read outside
	// bytes; the good skill still activates.
	if body, err := r.Activate("evil"); err == nil {
		t.Errorf("SECURITY: [skill-symlink] Activate(evil) succeeded on a symlinked-out SKILL.md (body %q)", truncate(body, 40))
	}
	if body, err := r.Activate("good"); err != nil || !strings.Contains(body, "good body") {
		t.Errorf("control skill activation: body %q err %v", body, err)
	}

	// Tier 3: SupportingFile must refuse both the directory-component
	// escape and the leaf escape.
	if b, err := r.SupportingFile("bad", "scripts/run.sh"); err == nil || b != nil {
		t.Errorf("SECURITY: [skill-symlink] SupportingFile read through a symlinked dir out of the skill tree (%d bytes, err %v)", len(b), err)
	}
	if b, err := r.SupportingFile("bad", "leak.sh"); err == nil || b != nil {
		t.Errorf("SECURITY: [skill-symlink] SupportingFile read through a symlinked file out of the skill tree (%d bytes, err %v)", len(b), err)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
