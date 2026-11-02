package projectfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Owner decision (2026-11): a file the CLI writes a generated secret
// into is a regular file. WriteOwnerOnly, and CheckRegularLeaf before
// it, refuse a symlink at the leaf whatever it points at: a file of the
// project's own (.env -> static/robots.txt put the signing key in a
// served file), a dangling target in the project, or anything outside.
// A link on the way to the leaf is still followed while it stays in the
// project, as every writer here does.
func TestOwnerOnlyWriterRefusesALinkAtTheLeaf(t *testing.T) {
	for _, tc := range []struct {
		label, target string
		exists        bool
	}{
		{"existing in-project", "static/robots.txt", true},
		{"dangling in-project", "static/robots.txt", false},
		{"existing out of project", "", true},
		{"dangling out of project", "", false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			proj := t.TempDir()
			if err := os.MkdirAll(filepath.Join(proj, "static"), 0o755); err != nil {
				t.Fatal(err)
			}
			target, linkTo := filepath.Join(proj, filepath.FromSlash(tc.target)), filepath.FromSlash(tc.target)
			if tc.target == "" {
				target = filepath.Join(t.TempDir(), "victim")
				linkTo = target
			}
			if tc.exists {
				if err := os.WriteFile(target, []byte("keep\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(linkTo, filepath.Join(proj, ".env")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := CheckRegularLeaf(proj, ".env"); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Errorf("SECURITY: CheckRegularLeaf passed .env -> %s: %v", linkTo, err)
			}
			if err := WriteOwnerOnly(proj, ".env", []byte("JWT_SECRET=s\n")); err == nil {
				t.Errorf("SECURITY: WriteOwnerOnly wrote through .env -> %s", linkTo)
			}
			got, err := os.ReadFile(target)
			if tc.exists && string(got) != "keep\n" {
				t.Errorf("SECURITY: the link's target was rewritten: %q, %v", got, err)
			}
			if !tc.exists && err == nil {
				t.Errorf("SECURITY: the dangling link's target was created: %q", got)
			}
			if fi, err := os.Lstat(filepath.Join(proj, ".env")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
				t.Errorf(".env is no longer the link (%v, %v)", fi, err)
			}
		})
	}

	t.Run("a directory at the leaf", func(t *testing.T) {
		proj := t.TempDir()
		if err := os.Mkdir(filepath.Join(proj, ".env"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := CheckRegularLeaf(proj, ".env"); err == nil {
			t.Error("CheckRegularLeaf passed a directory")
		}
		if err := WriteOwnerOnly(proj, ".env", []byte("x")); err == nil {
			t.Error("WriteOwnerOnly replaced a directory")
		}
	})

	// GREEN guard: a missing file, a regular one, and a regular one under
	// an in-project directory link are written, owner-only.
	t.Run("regular files are written", func(t *testing.T) {
		proj := t.TempDir()
		if err := os.MkdirAll(filepath.Join(proj, "real"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("real", filepath.Join(proj, "conf")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := os.WriteFile(filepath.Join(proj, "old.env"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"new.env", "old.env", "conf/.env"} {
			if err := CheckRegularLeaf(proj, name); err != nil {
				t.Errorf("CheckRegularLeaf(%s): %v", name, err)
			}
			if err := WriteOwnerOnly(proj, name, []byte("JWT_SECRET=s\n")); err != nil {
				t.Errorf("WriteOwnerOnly(%s): %v", name, err)
			}
		}
		for _, p := range []string{"new.env", "old.env", "real/.env"} {
			fi, err := os.Lstat(filepath.Join(proj, p))
			if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 {
				t.Errorf("%s: %v, %v; want a regular 0600 file", p, fi, err)
			}
		}
	})
}
