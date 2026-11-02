package agentscmd

import (
	_ "embed"
	"fmt"
)

// gofastrHostSkill is the source for the gofastr-host Claude Code
// skill that `gofastr init` and `gofastr agents skill` drop into a
// host project's .claude/skills/ tree.
//
// Canonical edit location is .claude/skills/gofastr-host/SKILL.md in
// the gofastr repo. cmd/gofastr/embedded/gofastr-host-skill.md is a
// byte-for-byte copy kept in sync by TestEmbeddedHostSkillMatchesRepo.
// Failing that test means the two files have drifted: copy the canon
// over.
//
//go:embed embedded/gofastr-host-skill.md
var gofastrHostSkill string

// hostSkillDir is where the gofastr-host skill lives in a project.
const hostSkillDir = ".claude/skills/gofastr-host"

// WriteHostSkill drops the gofastr-host skill into <dir>/.claude/skills/
// gofastr-host/SKILL.md. Idempotent: overwrites any existing copy. The
// write is confined to dir: a symlinked .claude or SKILL.md that leads
// out of the project is refused, never written through.
func WriteHostSkill(dir string) error {
	root, err := openProjectRoot(dir)
	if err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}
	defer root.Close()
	if err := root.MkdirAll(hostSkillDir, 0o755); err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}
	if err := replaceInRoot(root, hostSkillDir+"/SKILL.md", []byte(gofastrHostSkill), 0o644); err != nil {
		return fmt.Errorf("write skill: %w", err)
	}
	return nil
}
