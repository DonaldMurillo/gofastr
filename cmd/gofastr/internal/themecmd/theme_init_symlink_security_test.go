package themecmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
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
