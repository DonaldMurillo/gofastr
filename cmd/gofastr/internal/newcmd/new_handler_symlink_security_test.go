package newcmd

import (
	"os"
	"path/filepath"
	"testing"
)

// scaffoldHandler resolved its base dir but not the leaf: a dangling
// foo_handler.go link passed the exists check and the write created its
// target outside the project, and -overwrite wrote through a link that
// leaves it. Both are refused and nothing lands outside.
func TestNewHandlerRefusesALeafLinkOutOfTheProject(t *testing.T) {
	for _, dangling := range []bool{true, false} {
		base := t.TempDir()
		proj, target := filepath.Join(base, "proj"), filepath.Join(base, "victim")
		if err := os.Mkdir(proj, 0o755); err != nil {
			t.Fatal(err)
		}
		if !dangling {
			if err := os.WriteFile(target, []byte("precious\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(target, filepath.Join(proj, "foo_handler.go")); err != nil {
			t.Fatal(err)
		}
		if err := scaffoldHandler(proj, "Foo", "GET", "/foo", !dangling); err == nil {
			t.Errorf("dangling=%v: scaffoldHandler wrote through a link out of the project", dangling)
		}
		got, err := os.ReadFile(target)
		switch {
		case dangling && err == nil:
			t.Errorf("scaffoldHandler created %s through a dangling link", target)
		case !dangling && string(got) != "precious\n":
			t.Errorf("-overwrite replaced %s through the link: %q", target, got)
		}
	}
}
