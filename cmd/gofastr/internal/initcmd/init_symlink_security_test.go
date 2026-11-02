package initcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/agentscmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/core/settings"
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
		{"settings file", settings.FileName, false, func(p string) error { return exitAsError(t, func() { writeSettingsFile(p, "") }) }},
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

// `gofastr init .` ran go mod init when os.Stat said go.mod did not exist,
// which a dangling go.mod link also says, so go mod init created the
// link's target outside the project; over an existing outside target, go
// mod edit rewrote it. A go.mod that leaves the project through a link is
// refused before any go subprocess runs, dangling or not.
func TestInitRefusesGoModLinkedOutside(t *testing.T) {
	for _, dangling := range []bool{true, false} {
		t.Run(map[bool]string{true: "dangling", false: "existing"}[dangling], func(t *testing.T) {
			dir := inPlaceDir(t, "linkapp")
			outsideDir := filepath.Join(filepath.Dir(dir), "other")
			if err := os.Mkdir(outsideDir, 0o755); err != nil {
				t.Fatal(err)
			}
			victim := filepath.Join(outsideDir, "go.mod")
			const precious = "module example.com/other\n\ngo 1.21\n"
			if !dangling {
				if err := os.WriteFile(victim, []byte(precious), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(victim, filepath.Join(dir, "go.mod")); err != nil {
				t.Fatal(err)
			}
			code := runInitCode(t, ".", "--no-entity")
			if code != 1 {
				t.Errorf("init . over a go.mod linked outside exited %d, want 1", code)
			}
			got, err := os.ReadFile(victim)
			if dangling && err == nil {
				t.Errorf("init . (exit %d) created %s outside the project through go.mod:\n%s", code, victim, got)
			}
			if !dangling && string(got) != precious {
				t.Errorf("init . (exit %d) rewrote %s outside the project through go.mod:\n%s", code, victim, got)
			}
		})
	}
}

// `git init .` over a .git linked outside the project reinitialises the
// repository the link names (it rewrites that repo's config and hooks).
// init . refuses such a .git before writing anything.
func TestInitRefusesGitDirLinkedOutside(t *testing.T) {
	dir := inPlaceDir(t, "gitlinkapp")
	outsideRepo := filepath.Join(filepath.Dir(dir), "outside-repo")
	if err := os.Mkdir(outsideRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideRepo, filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	code := runInitCode(t, ".", "--no-entity")
	if code != 1 {
		t.Errorf("init . over a .git linked outside exited %d, want 1", code)
	}
	if entries, _ := os.ReadDir(outsideRepo); len(entries) > 0 {
		t.Errorf("init . (exit %d) put %d entries in %s through the linked .git", code, len(entries), outsideRepo)
	}
}

// exitAsError runs a writer that reports failure through cli.OsExit and
// turns a non-zero exit into an error, so it fits the surfaces table.
func exitAsError(t *testing.T, fn func()) error {
	t.Helper()
	var code int
	clitest.CovT_capStdout(t, func() { code = clitest.CovT_capExit(t, fn) })
	if code != -1 && code != 0 {
		return fmt.Errorf("exit %d", code)
	}
	return nil
}

// init . creates static/, entities/ and migrations/ with os.MkdirAll and
// writes the scaffold into them, so a cloned directory whose entities/
// links out of it took the generated entity files wherever the link
// pointed. The directory loop refuses such a link before any scaffold
// file is written, so a refused init leaves no half-written project
// (the per-file writers would refuse the link too, but only after
// main.go and its siblings were already in place).
func TestInitRefusesALinkedScaffoldDir(t *testing.T) {
	for _, linked := range []string{"static", "entities", "migrations"} {
		t.Run(linked, func(t *testing.T) {
			dir := inPlaceDir(t, "linkdirapp")
			outside := filepath.Join(filepath.Dir(dir), "outside")
			if err := os.Mkdir(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dir, linked)); err != nil {
				t.Fatal(err)
			}
			code := runInitCode(t, ".")
			if code != 1 {
				t.Errorf("init . over a linked %s/ exited %d, want 1", linked, code)
			}
			if entries, _ := os.ReadDir(outside); len(entries) > 0 {
				t.Errorf("init . (exit %d) put %d entries outside the project through the linked %s/", code, len(entries), linked)
			}
			if _, err := os.Lstat(filepath.Join(dir, "main.go")); err == nil {
				t.Errorf("init . refused the linked %s/ only after writing main.go", linked)
			}
		})
	}
}
