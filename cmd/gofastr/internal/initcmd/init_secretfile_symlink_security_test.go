package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/settings"
)

// Owner decision (2026-11): the files init writes a generated secret
// into, .env (JWT_SECRET, DATABASE_URL) and gofastr.settings.yml, must be
// regular files. A symlink there is refused whatever it points at: in
// the project or out, dangling or not. Before, a link that stayed in the
// project was followed, so with .env -> static/robots.txt `init .
// --force` wrote the signing key into a tracked, statically served
// file, and a dangling one created it there.
func TestInitDotSecretFilesRefuseAnySymlink(t *testing.T) {
	const robots = "User-agent: *\nDisallow:\n"
	for _, name := range []string{".env", settings.FileName} {
		for _, tc := range []struct {
			label  string
			inside bool // target in the project (static/robots.txt) or outside it
			exists bool
		}{
			{"existing in-project target", true, true},
			{"dangling in-project target", true, false},
			{"existing out-of-project target", false, true},
			{"dangling out-of-project target", false, false},
		} {
			t.Run(name+" "+tc.label, func(t *testing.T) {
				dir := inPlaceDir(t, "app")
				target := filepath.Join(t.TempDir(), "robots.txt")
				linkTo := target
				if tc.inside {
					if err := os.MkdirAll(filepath.Join(dir, "static"), 0o755); err != nil {
						t.Fatal(err)
					}
					target = filepath.Join(dir, "static", "robots.txt")
					linkTo = filepath.Join("static", "robots.txt")
				}
				if tc.exists {
					if err := os.WriteFile(target, []byte(robots), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(linkTo, filepath.Join(dir, name)); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}

				if code := runInitCode(t, ".", "--force"); code != 1 {
					t.Errorf("SECURITY: [init-secret-link] init . --force with %s -> %s exited %d, want 1", name, linkTo, code)
				}
				got, err := os.ReadFile(target)
				switch {
				case tc.exists && (err != nil || string(got) != robots):
					t.Errorf("SECURITY: [init-secret-link] init . rewrote %s's target %s: %.80q, %v", name, linkTo, got, err)
				case !tc.exists && err == nil:
					t.Errorf("SECURITY: [init-secret-link] init . created %s's target %s: %.80q", name, linkTo, got)
				}
				if fi, err := os.Lstat(filepath.Join(dir, name)); err != nil || fi.Mode()&os.ModeSymlink == 0 {
					t.Errorf("%s is no longer the link it was (%v, %v): init must refuse it, not replace it", name, fi, err)
				}
				// Refused before anything is written: no half-made project.
				if _, err := os.Lstat(filepath.Join(dir, "main.go")); err == nil {
					t.Errorf("init . wrote main.go before refusing the %s link", name)
				}
			})
		}
	}
}

// GREEN guard: a regular .env and settings file are still replaced by
// init . --force, owner-only, with a fresh JWT_SECRET.
func TestInitDotForceReplacesRegularSecretFiles(t *testing.T) {
	dir := inPlaceDir(t, "app")
	for _, name := range []string{".env", settings.FileName} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("OLD=1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if code := runInitCode(t, ".", "--force"); code != -1 {
		t.Fatalf("init . --force over regular files exited %d", code)
	}
	env, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil || !strings.Contains(string(env), "JWT_SECRET=") {
		t.Errorf(".env was not rewritten: %q, %v", env, err)
	}
	for _, name := range []string{".env", settings.FileName} {
		fi, err := os.Lstat(filepath.Join(dir, name))
		if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v, %v; want a regular 0600 file", name, fi, err)
		}
	}
}
