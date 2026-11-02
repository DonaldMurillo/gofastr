package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/agentscmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// The scaffold and onboarding writers followed symlinks out of the
// project: in a cloned repo carrying
// .claude/skills/gofastr-host/SKILL.md -> ~/.bashrc, `gofastr agents
// skill` and `gofastr init . --reinit` replaced the developer's file with
// the skill, a symlinked .agents/ received every detail file, and a .env
// linked outside received the generated signing key. Each writer refuses
// a path that leaves the project through a link, and creates nothing on
// the way; a link that stays inside is followed as before.
func TestOnboardingWritersRefuseLinksOutOfTheProject(t *testing.T) {
	const precious = "precious user data\n"
	type surface struct {
		name  string
		link  string // path in the project made a symlink to the outside
		dir   bool   // the link stands for a directory
		write func(proj string) error
	}
	surfaces := []surface{
		{"agents skill leaf", ".claude/skills/gofastr-host/SKILL.md", false, agentscmd.WriteHostSkill},
		{"agents skill parent", ".claude", true, agentscmd.WriteHostSkill},
		{"agents detail dir", ".agents", true, agentscmd.WriteAgentDetailFiles},
		{"agents detail file", ".agents/framework.md", false, agentscmd.WriteAgentDetailFiles},
		{"scaffold file", "main.go", false, func(p string) error { return writeScaffoldFile(p, "main.go", []byte("package main\n")) }},
		{"scaffold under a linked dir", "static", true, func(p string) error {
			return writeScaffoldFile(p, filepath.Join("static", ".gitkeep"), nil)
		}},
		{".env", ".env", false, func(p string) error { return writeEnvFile(p, ".env", "JWT_SECRET=s3cret\n") }},
		{".gitignore", ".gitignore", false, func(p string) error { return writeGitignore(p, ".gitignore", ".env\n") }},
	}
	for _, s := range surfaces {
		for _, dangling := range []bool{false, true} {
			name := s.name
			if dangling {
				name += " (dangling)"
			}
			t.Run(name, func(t *testing.T) {
				base := t.TempDir()
				proj, outside := filepath.Join(base, "proj"), filepath.Join(base, "outside")
				for _, d := range []string{proj, outside} {
					if err := os.MkdirAll(d, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				target := filepath.Join(outside, "victim")
				if !dangling {
					if s.dir {
						if err := os.Mkdir(target, 0o755); err != nil {
							t.Fatal(err)
						}
					} else if err := os.WriteFile(target, []byte(precious), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				link := filepath.Join(proj, filepath.FromSlash(s.link))
				if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}

				if err := s.write(proj); err == nil {
					t.Errorf("the writer followed %s out of the project without an error", s.link)
				}
				switch {
				case dangling:
					if _, err := os.Lstat(target); err == nil {
						t.Errorf("the writer created %s outside the project through the dangling %s", target, s.link)
					}
				case s.dir:
					if entries, _ := os.ReadDir(target); len(entries) > 0 {
						t.Errorf("the writer put %d entries in %s through the linked %s", len(entries), target, s.link)
					}
				default:
					if got, _ := os.ReadFile(target); string(got) != precious {
						t.Errorf("the writer replaced %s through %s: now %q", target, s.link, got)
					}
				}
			})
		}
	}
}

// `gofastr init . --reinit` is the command that runs these writers on a
// project it did not create: it exits non-zero and leaves the outside
// file alone.
func TestReinitRefusesALinkedSkill(t *testing.T) {
	base := t.TempDir()
	proj, outside := filepath.Join(base, "proj"), filepath.Join(base, "victim")
	if err := os.WriteFile(outside, []byte("precious\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(proj, ".claude", "skills", "gofastr-host", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, skill); err != nil {
		t.Fatal(err)
	}
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runReinit(proj, true) })
	})
	if code != 1 {
		t.Errorf("reinit over a linked skill exited %d, want 1", code)
	}
	if got, _ := os.ReadFile(outside); string(got) != "precious\n" {
		t.Errorf("reinit replaced the file the skill links to: %q", got)
	}
}

// A link that stays inside the project is followed, as atomicfile.Rewrite
// documents: CLAUDE.md linked to AGENTS.md stays a link.
func TestScaffoldWriterFollowsALinkInsideTheProject(t *testing.T) {
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "AGENTS.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(proj, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	if err := writeScaffoldFile(proj, "CLAUDE.md", []byte("new\n")); err != nil {
		t.Fatalf("an in-project link was refused: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(proj, "AGENTS.md")); !strings.Contains(string(got), "new") {
		t.Errorf("the write did not go through the in-project link: AGENTS.md = %q", got)
	}
	if fi, err := os.Lstat(filepath.Join(proj, "CLAUDE.md")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("CLAUDE.md is no longer a link")
	}
}
