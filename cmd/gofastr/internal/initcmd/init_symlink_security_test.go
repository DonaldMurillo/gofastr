package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
