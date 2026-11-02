package themecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// `gofastr theme init` wrote theme/theme.go with os.MkdirAll and a write
// that follow symlinks, so a cloned project whose theme/ (or
// theme/theme.go, dangling) links out of it took the starter theme
// wherever the link pointed, --force replacing a file that was there.
// A destination inside the project refuses such a link; nothing lands
// outside.
func TestThemeInitRefusesALinkOutOfTheProject(t *testing.T) {
	for _, tc := range []struct {
		name, link string
		dir        bool
	}{
		{"linked theme dir", "theme", true},
		{"dangling theme.go", "theme/theme.go", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			proj, outside := filepath.Join(base, "proj"), filepath.Join(base, "outside")
			if err := os.MkdirAll(proj, 0o755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(outside, "victim")
			if tc.dir {
				if err := os.MkdirAll(target, 0o755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.MkdirAll(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(proj, filepath.FromSlash(tc.link))
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			clitest.CovT_chdir(t, proj)
			code := clitest.CovT_capExit(t, func() {
				clitest.CovT_capStdout(t, func() { runThemeInit([]string{"--force"}) })
			})
			if code != 1 {
				t.Errorf("theme init through %s exited %d, want 1", tc.link, code)
			}
			if tc.dir {
				if entries, _ := os.ReadDir(target); len(entries) > 0 {
					t.Errorf("theme init wrote %d entries outside the project", len(entries))
				}
			} else if _, err := os.Lstat(target); err == nil {
				t.Errorf("theme init created %s outside the project", target)
			}
		})
	}
}

// linkedThemeProject makes base/proj with theme/ linked to base/outside,
// chdirs into proj and returns both.
func linkedThemeProject(t *testing.T) (proj, outside string) {
	t.Helper()
	base := t.TempDir()
	proj, outside = filepath.Join(base, "proj"), filepath.Join(base, "outside")
	for _, d := range []string{proj, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(proj, "theme")); err != nil {
		t.Fatal(err)
	}
	clitest.CovT_chdir(t, proj)
	return proj, outside
}

// An --out spelled as an absolute path inside the project is a
// destination inside the project: filepath.IsLocal is false for it, so
// theme init skipped the link check and wrote through a linked theme/.
func TestThemeInitRefusesALinkBehindAnAbsoluteOutInsideTheProject(t *testing.T) {
	proj, outside := linkedThemeProject(t)
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() {
			runThemeInit([]string{"--out=" + filepath.Join(proj, "theme", "theme.go")})
		})
	})
	if code != 1 {
		t.Errorf("theme init --out=<abs inside> through a linked theme/ exited %d, want 1", code)
	}
	if entries, _ := os.ReadDir(outside); len(entries) > 0 {
		t.Errorf("theme init wrote %d entries outside the project", len(entries))
	}
}

// The theme editor's Write button ran MkdirAll, CreateTemp and a rename
// under the output's directory, all of which follow a linked theme/ out
// of the project. writeBack refuses such a destination and writes
// nothing outside.
func TestThemeEditWriteBackRefusesLinkedThemeDir(t *testing.T) {
	_, outside := linkedThemeProject(t)
	th := uitheme.Default()
	srv := &ThemeEditServer{Base: th, Working: th, OutPath: filepath.Join("theme", "theme.go")}
	err := srv.writeBack()
	if err == nil {
		t.Error("writeBack through a linked theme/ returned no error")
	}
	if entries, _ := os.ReadDir(outside); len(entries) > 0 {
		t.Errorf("writeBack (err=%v) wrote %d entries outside the project through the linked theme/", err, len(entries))
	}
}

// theme edit refuses the same destination at startup, before it serves
// an editor whose Write could only fail. The address cannot be bound,
// so a missing check fails on the listen error (which does not name the
// link) instead of serving forever.
func TestThemeEditRefusesLinkedThemeDirAtStartup(t *testing.T) {
	linkedThemeProject(t)
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() {
			runThemeEdit([]string{"--no-open", "--force", "--addr=no-port-here"})
		})
	})
	if code != 1 || !strings.Contains(out, "symlink") {
		t.Errorf("theme edit over a linked theme/ exited %d without refusing the link:\n%s", code, out)
	}
}
