package agentscmd

import (
	"os"
	"path/filepath"
	"testing"
)

// A link's relative target names a path from the directory the link
// really sits in. When a parent on the way is itself a link inside the
// project, that directory is the parent's target, not the lexical one:
// docs -> real/sub plus docs/AGENTS.md -> ../target.md is
// real/target.md, the file every other program opens through that path.
// The first confinement resolved the leaf against path.Dir(name) and
// read (and replaced) target.md at the project root instead.
func TestProjectFileResolvesThroughInRootParentLinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "real", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real", "target.md"), []byte("real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A decoy where the lexical join lands: it must be neither read nor
	// replaced.
	if err := os.WriteFile(filepath.Join(dir, "target.md"), []byte("decoy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("real", "sub"), filepath.Join(dir, "docs")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join("..", "target.md"), filepath.Join(dir, "real", "sub", "AGENTS.md")); err != nil {
		t.Fatal(err)
	}

	got, err := ReadProjectFile(dir, "docs/AGENTS.md")
	if err != nil || string(got) != "real\n" {
		t.Fatalf("ReadProjectFile(docs/AGENTS.md) = %q, %v; want the kernel's file real/target.md", got, err)
	}
	if err := WriteProjectFile(dir, "docs/AGENTS.md", []byte("fresh\n")); err != nil {
		t.Fatalf("WriteProjectFile through an in-project parent link: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "real", "target.md")); string(b) != "fresh\n" {
		t.Errorf("real/target.md = %q, want the new content", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "target.md")); string(b) != "decoy\n" {
		t.Errorf("the lexical decoy target.md was rewritten: %q", b)
	}
	if fi, err := os.Lstat(filepath.Join(dir, "real", "sub", "AGENTS.md")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the AGENTS.md link was replaced: %v, %v", fi, err)
	}
}

// The resolver walks the real directory, so a parent link plus a
// relative target that climbs past the project root is still refused.
func TestProjectFileRefusesClimbOutThroughParentLink(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "proj")
	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(base, "victim")
	if err := os.WriteFile(victim, []byte("precious\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a", filepath.Join(dir, "docs")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// From the real directory a/, ../../victim leaves the project.
	if err := os.Symlink(filepath.Join("..", "..", "victim"), filepath.Join(dir, "a", "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if err := WriteProjectFile(dir, "docs/AGENTS.md", []byte("x\n")); err == nil {
		t.Error("SECURITY: [agents-symlink-escape] a write through docs/AGENTS.md -> ../../victim was accepted")
	}
	if b, _ := os.ReadFile(victim); string(b) != "precious\n" {
		t.Errorf("SECURITY: [agents-symlink-escape] the victim outside the project was rewritten: %q", b)
	}
}
