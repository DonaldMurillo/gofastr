package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// gofastrHostSkill is the source for the gofastr-host Claude Code
// skill that `gofastr init` and `gofastr agents skill` drop into a
// host project's .claude/skills/ tree.
//
// Canonical edit location is .claude/skills/gofastr-host/SKILL.md in
// the gofastr repo. cmd/gofastr/embedded/gofastr-host-skill.md is a
// byte-for-byte copy kept in sync by TestEmbeddedHostSkillMatchesRepo.
// Failing that test means the two files have drifted — copy the canon
// over.
//
//go:embed embedded/gofastr-host-skill.md
var gofastrHostSkill string

// writeHostSkill drops the gofastr-host skill into <dir>/.claude/skills/
// gofastr-host/SKILL.md. Idempotent — overwrites any existing copy.
func writeHostSkill(dir string) error {
	target := filepath.Join(dir, ".claude", "skills", "gofastr-host")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}
	skillPath := filepath.Join(target, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(gofastrHostSkill), 0o644); err != nil {
		return fmt.Errorf("write skill: %w", err)
	}
	return nil
}
