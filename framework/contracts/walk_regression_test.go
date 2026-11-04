package contracts

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPassFollowsSymlinkRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "actual")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "linked")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	pass, err := NewPass(link, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pass.Files()) != 1 {
		t.Fatalf("symlink root discovered %d files, want 1", len(pass.Files()))
	}
}

func TestPassDiscoversSiblingOfUnreadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix permissions and a non-root user")
	}
	root := t.TempDir()
	unreadable := filepath.Join(root, "a")
	if err := os.Mkdir(unreadable, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTemp(t, root, "b/source.go", "package app\n")
	t.Cleanup(func() {
		if err := os.Chmod(unreadable, 0o700); err != nil {
			t.Error(err)
		}
	})
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	pass, err := NewPass(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	files := pass.Files()
	if len(files) != 1 || files[0].Rel != "b/source.go" {
		t.Fatalf("discovered %v, want b/source.go", files)
	}
}

func restrictPassFixture(t *testing.T, path string, mode fs.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix permissions and a non-root user")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, info.Mode().Perm()); err != nil {
			t.Error(err)
		}
	})
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestPassRejectsUnreadableRoot(t *testing.T) {
	root := t.TempDir()
	writeTemp(t, root, "source.go", "package app\n")
	restrictPassFixture(t, root, 0o300)
	pass, err := NewPass(root, nil)
	if !errors.Is(err, fs.ErrPermission) || pass != nil {
		t.Fatalf("NewPass = %v, %v; want no pass and a root permission error", pass, err)
	}
	if !strings.Contains(err.Error(), root) {
		t.Fatalf("root error omits its path: %v", err)
	}
}

func TestPassRecordsUnreadableSources(t *testing.T) {
	for _, name := range []string{"nested/source.go", "nested/site.css", "nested/app.js", "nested/app.mjs", "nested/app.cjs"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeTemp(t, root, "readable.go", "package app\n")
			path := writeTemp(t, root, name, "unreadable source\n")
			restrictPassFixture(t, path, 0)
			if _, err := os.ReadFile(path); !errors.Is(err, fs.ErrPermission) {
				t.Fatalf("fixture read = %v, want a permission error", err)
			}
			pass, err := NewPass(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			unparsed := pass.Unparsed()
			if len(unparsed) != 1 || !strings.Contains(unparsed[name], name) {
				t.Fatalf("Unparsed = %v, want the unreadable source and its read error", unparsed)
			}
			if _, ok := pass.Source(name); ok {
				t.Fatalf("unreadable source %s is available to analyzers", name)
			}
			if _, ok := pass.Source("readable.go"); !ok {
				t.Fatal("readable sibling source was not discovered")
			}
		})
	}
}
