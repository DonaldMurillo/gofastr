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

// Property: `gofastr init .` writes only inside the directory it
// scaffolds, the rule `init --reinit` and `gofastr agents` already hold
// (TestReinitStaysInsideTheProject, agentscmd
// TestAgentFilesStayInsideTheProject). A symlink in the checkout,
// whether the file itself, a dangling one, or a directory on the way, is
// followed only while it stays in the project.
//
// Before the fix every plain-init write went through os.ReadFile /
// atomicfile.Rewrite / fileperm.ReplaceOwnerOnly on a joined path, which
// follow links anywhere. Without --force a cloned repo carrying
// .gitignore -> ~/.bashrc had init append "gen/", ".env" … to the
// user's shell profile, and a static/ or migrations/ link had init
// create files in a directory the project does not own. With --force
// main.go, screens.go, AGENTS.md, CLAUDE.md, DESIGN.md, the .env (with
// a fresh JWT_SECRET) and the settings file were written through.
func TestInitDotStaysInsideTheProject(t *testing.T) {
	const precious = "precious user content\n"
	victimFile := func(t *testing.T) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "victim")
		if err := os.WriteFile(p, []byte(precious), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	symlink := func(t *testing.T, target, link string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}

	// A file linked out, and a dangling link out, for every file init
	// writes. The files init refuses to replace without --force need it
	// for the write to be reached; .gitignore is merged, so it is reached
	// without.
	files := []struct {
		name  string
		force bool
	}{
		{".gitignore", false},
		{"main.go", true},
		{"screens.go", true},
		{"screens_auth.go", true},
		{"AGENTS.md", true},
		{"CLAUDE.md", true},
		{"DESIGN.md", true},
		{".env", true},
		{"gofastr.settings.yml", true},
		{"entities/entities.go", true},
		// go.mod is read for its module line before anything is written;
		// a dangling one read as "no go.mod", and `go mod init` then
		// created the link's target wherever it pointed.
		{"go.mod", false},
	}
	for _, f := range files {
		args := []string{"."}
		if f.force {
			args = append(args, "--force")
		}
		t.Run(f.name+" linked out", func(t *testing.T) {
			dir := inPlaceDir(t, "app")
			victim := victimFile(t)
			symlink(t, victim, filepath.Join(dir, filepath.FromSlash(f.name)))
			runInitCode(t, args...)
			if got, err := os.ReadFile(victim); err != nil || string(got) != precious {
				t.Errorf("SECURITY: [init-symlink-escape] init . rewrote %s's target outside the project: %.80q, %v", f.name, got, err)
			}
		})
		t.Run(f.name+" dangling out", func(t *testing.T) {
			dir := inPlaceDir(t, "app")
			target := filepath.Join(t.TempDir(), "created")
			symlink(t, target, filepath.Join(dir, filepath.FromSlash(f.name)))
			runInitCode(t, args...)
			if _, err := os.Lstat(target); err == nil {
				t.Errorf("SECURITY: [init-symlink-escape] init . created %s's target outside the project through a dangling link", f.name)
			}
		})
	}

	// A directory on the way linked out: nothing may land in it.
	for _, d := range []string{"static", "migrations", "entities"} {
		t.Run(d+"/ linked out", func(t *testing.T) {
			dir := inPlaceDir(t, "app")
			out := t.TempDir()
			symlink(t, out, filepath.Join(dir, d))
			runInitCode(t, ".")
			if ents, _ := os.ReadDir(out); len(ents) != 0 {
				t.Errorf("SECURITY: [init-symlink-escape] init . created files outside the project through %s/: %v", d, ents)
			}
		})
	}

	// GREEN guard: links that stay in the project still work. A
	// .gitignore kept as a link to a file of the project's own is merged
	// through, and stays a link; a static/ linked to an in-project
	// directory gets its .gitkeep there.
	t.Run("links inside the project are followed", func(t *testing.T) {
		dir := inPlaceDir(t, "app")
		if err := os.MkdirAll(filepath.Join(dir, "config", "assets"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config", "gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		symlink(t, filepath.Join("config", "gitignore"), filepath.Join(dir, ".gitignore"))
		symlink(t, filepath.Join("config", "assets"), filepath.Join(dir, "static"))
		if code := runInitCode(t, ".", "--no-entity"); code != -1 {
			t.Fatalf("init . with in-project links exited %d", code)
		}
		got, err := os.ReadFile(filepath.Join(dir, "config", "gitignore"))
		if err != nil || !strings.HasPrefix(string(got), "node_modules/\n") || !strings.Contains(string(got), "\n.env\n") {
			t.Errorf("the in-project .gitignore target was not merged: %q, %v", got, err)
		}
		if fi, err := os.Lstat(filepath.Join(dir, ".gitignore")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("the in-project .gitignore link was replaced: %v, %v", fi, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "config", "assets", ".gitkeep")); err != nil {
			t.Errorf("static/.gitkeep did not land in the in-project link's target: %v", err)
		}
	})
}

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
		{".env", ".env", false, func(p string) error { return writeEnvFile(p, "JWT_SECRET=s3cret\n") }},
		{".gitignore", ".gitignore", false, func(p string) error { return writeGitignore(p, ".env\n") }},
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
