package sourcewalk

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWalkFollowsLinksOnceAndKeepsExplicitRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "build")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dist"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "cycle")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{root, link} {
		files := 0
		err := Walk(target, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if filepath.Base(path) == "dist" {
				t.Error("build output visited")
			}
			if !entry.IsDir() {
				files++
			}
			return nil
		})
		if err != nil || files != 1 {
			t.Fatalf("target=%s files=%d err=%v", target, files, err)
		}
	}
}

func TestWalkPreservesRootErrorsAndSkipDir(t *testing.T) {
	root := t.TempDir()
	if err := Walk(filepath.Join(root, "missing"), func(_ string, _ fs.DirEntry, err error) error { return err }); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("lost root error: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "source"), 0o700); err != nil {
		t.Fatal(err)
	}
	visits := 0
	if err := Walk(root, func(_ string, _ fs.DirEntry, err error) error {
		visits++
		if err != nil {
			return err
		}
		return fs.SkipDir
	}); err != nil || visits != 1 {
		t.Fatalf("skip failed: visits=%d err=%v", visits, err)
	}
}

func TestWalkErrorControls(t *testing.T) {
	stop := errors.New("stop walking")
	for _, failure := range []string{"directory-read", "directory-resolution", "symlink-stat", "missing-root", "unreadable-root"} {
		t.Run(failure, func(t *testing.T) {
			for _, control := range []struct {
				name     string
				response error
			}{
				{name: "ignore"},
				{name: "skip-directory", response: fs.SkipDir},
				{name: "skip-all", response: fs.SkipAll},
				{name: "abort", response: stop},
			} {
				t.Run(control.name, func(t *testing.T) {
					root := t.TempDir()
					wantCause := fs.ErrPermission
					wantFiles := 1
					var restricted string
					var mode fs.FileMode
					switch failure {
					case "directory-read", "directory-resolution":
						directory := filepath.Join(root, "a")
						if err := os.Mkdir(directory, 0o700); err != nil {
							t.Fatal(err)
						}
						if failure == "directory-read" {
							restricted = directory
						} else {
							restricted, mode = root, 0o400
						}
					case "symlink-stat":
						wantCause = fs.ErrNotExist
						if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "a")); err != nil {
							t.Skipf("symlinks unavailable: %v", err)
						}
						if control.response == fs.SkipDir {
							wantFiles = 0
						}
					case "missing-root":
						root = filepath.Join(root, "missing")
						wantCause, wantFiles = fs.ErrNotExist, 0
					case "unreadable-root":
						restricted, mode, wantFiles = root, 0o300, 0
					}
					if failure != "missing-root" {
						if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package app\n"), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					if restricted != "" {
						if runtime.GOOS == "windows" || os.Geteuid() == 0 {
							t.Skip("requires Unix permissions and a non-root user")
						}
						t.Cleanup(func() {
							if err := os.Chmod(restricted, 0o700); err != nil {
								t.Error(err)
							}
						})
						if err := os.Chmod(restricted, mode); err != nil {
							t.Fatal(err)
						}
					}
					if control.response == fs.SkipAll || control.response == stop {
						wantFiles = 0
					}
					files, failures := 0, 0
					err := Walk(root, func(_ string, entry fs.DirEntry, err error) error {
						if err != nil {
							failures++
							if !errors.Is(err, wantCause) {
								t.Errorf("callback error = %v, want %v", err, wantCause)
							}
							return control.response
						}
						if !entry.IsDir() {
							files++
						}
						return nil
					})
					var wantErr error
					if control.response == stop {
						wantErr = stop
					}
					if !errors.Is(err, wantErr) || failures != 1 || files != wantFiles {
						t.Fatalf("files=%d failures=%d err=%v; want files=%d failures=1 err=%v", files, failures, err, wantFiles, wantErr)
					}
				})
			}
		})
	}
}
