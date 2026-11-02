package themecmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// Property: `gofastr theme init` and `theme edit`'s Write button put
// theme/theme.go inside the project they run in. A symlink in the
// checkout (the theme/ directory, or theme.go itself, dangling or not)
// is followed only while it stays in the project, the rule `gofastr init`
// and `gofastr agents` hold (projectfs).
//
// Before the fix theme init stat'ed and wrote theme/theme.go through
// os.Stat + atomicfile.Rewrite and writeBack through os.MkdirAll +
// os.CreateTemp in filepath.Dir, all of which follow links: a cloned
// repo with theme -> ~/somewhere got the theme written there, and a
// dangling theme/theme.go link read as "no file yet" and was created
// wherever it pointed.
func TestThemeWritesStayInsideTheProject(t *testing.T) {
	const precious = "precious user content\n"
	project := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		clitest.CovT_chdir(t, dir)
		return dir
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
	themeInit := func(t *testing.T, args ...string) {
		t.Helper()
		clitest.CovT_capExit(t, func() {
			clitest.CovT_capStdout(t, func() { runThemeInit(args) })
		})
	}
	writeBack := func(t *testing.T, force bool) {
		t.Helper()
		srv := newTestServer(t)
		srv.OutPath = filepath.Join("theme", "theme.go")
		srv.force = force
		_ = srv.writeBack()
	}
	empty := func(t *testing.T, d string) {
		t.Helper()
		if ents, _ := os.ReadDir(d); len(ents) != 0 {
			t.Errorf("SECURITY: [theme-symlink-escape] files were created outside the project through a symlinked theme/: %v", ents)
		}
	}
	absent := func(t *testing.T, p string) {
		t.Helper()
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("SECURITY: [theme-symlink-escape] %s was created outside the project through a dangling link", p)
		}
	}
	unchanged := func(t *testing.T, p string) {
		t.Helper()
		if got, err := os.ReadFile(p); err != nil || string(got) != precious {
			t.Errorf("SECURITY: [theme-symlink-escape] a file outside the project was rewritten through a symlink: %.60q, %v", got, err)
		}
	}
	victim := func(t *testing.T) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "victim.go")
		if err := os.WriteFile(p, []byte(precious), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("init: theme/ linked out", func(t *testing.T) {
		dir, out := project(t), t.TempDir()
		symlink(t, out, filepath.Join(dir, "theme"))
		themeInit(t)
		empty(t, out)
	})
	t.Run("init: theme.go dangling out", func(t *testing.T) {
		dir := project(t)
		target := filepath.Join(t.TempDir(), "created.go")
		symlink(t, target, filepath.Join(dir, "theme", "theme.go"))
		themeInit(t)
		absent(t, target)
	})
	t.Run("init --force: theme.go linked out", func(t *testing.T) {
		dir, v := project(t), victim(t)
		symlink(t, v, filepath.Join(dir, "theme", "theme.go"))
		themeInit(t, "--force")
		unchanged(t, v)
	})
	t.Run("edit write: theme/ linked out", func(t *testing.T) {
		dir, out := project(t), t.TempDir()
		symlink(t, out, filepath.Join(dir, "theme"))
		writeBack(t, true)
		empty(t, out)
	})
	t.Run("edit write: theme.go dangling out", func(t *testing.T) {
		dir := project(t)
		target := filepath.Join(t.TempDir(), "created.go")
		symlink(t, target, filepath.Join(dir, "theme", "theme.go"))
		writeBack(t, true)
		absent(t, target)
	})

	// GREEN guard: a theme/ that links to a directory of the project's
	// own still gets its theme.go.
	t.Run("in-project theme/ link is followed", func(t *testing.T) {
		dir := project(t)
		if err := os.MkdirAll(filepath.Join(dir, "ui", "theme"), 0o755); err != nil {
			t.Fatal(err)
		}
		symlink(t, filepath.Join("ui", "theme"), filepath.Join(dir, "theme"))
		themeInit(t)
		if _, err := os.Stat(filepath.Join(dir, "ui", "theme", "theme.go")); err != nil {
			t.Fatalf("theme init did not write through the in-project link: %v", err)
		}
		if err := os.Remove(filepath.Join(dir, "ui", "theme", "theme.go")); err != nil {
			t.Fatal(err)
		}
		writeBack(t, false)
		if _, err := os.Stat(filepath.Join(dir, "ui", "theme", "theme.go")); err != nil {
			t.Fatalf("theme edit did not write through the in-project link: %v", err)
		}
	})
}
