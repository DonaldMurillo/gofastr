package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The embedded copy under cmd/gofastr/embedded/ must stay byte-equal to
// the canonical skill source in .claude/skills/gofastr-host/SKILL.md.
// If they drift, the binary ships stale content.
func TestEmbeddedHostSkillMatchesRepo(t *testing.T) {
	repoCopy, err := os.ReadFile("../../.claude/skills/gofastr-host/SKILL.md")
	if err != nil {
		t.Fatalf("read repo SKILL.md: %v", err)
	}
	if gofastrHostSkill != string(repoCopy) {
		t.Fatalf("embedded skill drifted from .claude/skills/gofastr-host/SKILL.md — copy the latter over cmd/gofastr/embedded/gofastr-host-skill.md")
	}
}

func TestWriteHostSkillDropsFileWithFrontmatter(t *testing.T) {
	dir := t.TempDir()
	if err := writeHostSkill(dir); err != nil {
		t.Fatalf("writeHostSkill: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".claude/skills/gofastr-host/SKILL.md"))
	if err != nil {
		t.Fatalf("read written skill: %v", err)
	}
	body := string(got)
	if !strings.HasPrefix(body, "---\n") {
		t.Fatalf("written skill missing frontmatter:\n%s", body[:120])
	}
	if !strings.Contains(body, "name: gofastr-host") {
		t.Fatal("written skill missing name field")
	}
	if !strings.Contains(body, "Don't reinvent") {
		t.Fatal("written skill missing the reinvent-guidance heading")
	}
}
