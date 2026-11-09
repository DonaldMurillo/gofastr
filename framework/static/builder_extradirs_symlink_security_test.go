package static

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// TestExtraDirsRefuseEscapingSymlinks: dumpExtraDirs walked an ExtraDirs
// os.DirFS and read each entry through it, and os.DirFS follows a
// symlink anywhere on disk, so a link in the directory published the
// file it pointed at. The extra directory is now read through os.Root:
// a link out of it is skipped, a link inside it is copied.
func TestExtraDirsRefuseEscapingSymlinks(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outside, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "dir", "inner.txt"), []byte("INNER"), 0o600); err != nil {
		t.Fatal(err)
	}
	extra := t.TempDir()
	if err := os.WriteFile(filepath.Join(extra, "ok.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		"leak.txt": filepath.Join(outside, "secret.txt"),
		"leakdir":  filepath.Join(outside, "dir"),
		"same.txt": "ok.txt",
	} {
		if err := os.Symlink(target, filepath.Join(extra, link)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}

	a := coreapp.NewApp("extra-symlink")
	a.Register("/", &homeScreen{}, nil)
	out := t.TempDir()
	b := &Builder{Host: uihost.New(a), OutDir: out, ExtraDirs: map[string]fs.FS{"/files": os.DirFS(extra)}}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, rel := range []string{"files/leak.txt", "files/leakdir/inner.txt"} {
		if data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(rel))); err == nil {
			t.Errorf("SECURITY: [static] the export published %s, a symlink out of the extra dir (%q)", rel, data)
		}
	}
	for rel, want := range map[string]string{"files/ok.txt": "ok", "files/same.txt": "ok"} {
		if got := readOut(t, out, rel); got != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
}

// TestExtraDirsSkipLinksInWrappedDirFS: only an os.DirFS value is
// re-rooted. fs.Sub(os.DirFS(dir), "sub") is another FS type, which
// follows a link anywhere on disk, so every symlink in it is skipped,
// a linked directory included, wherever it points.
func TestExtraDirsSkipLinksInWrappedDirFS(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outside, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "dir", "inner.txt"), []byte("INNER"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	extra := filepath.Join(parent, "sub")
	if err := os.Mkdir(extra, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extra, "ok.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		"leak.txt": filepath.Join(outside, "secret.txt"),
		"leakdir":  filepath.Join(outside, "dir"),
		"same.txt": "ok.txt",
	} {
		if err := os.Symlink(target, filepath.Join(extra, link)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	wrapped, err := fs.Sub(os.DirFS(parent), "sub")
	if err != nil {
		t.Fatal(err)
	}

	a := coreapp.NewApp("extra-wrapped")
	a.Register("/", &homeScreen{}, nil)
	out := t.TempDir()
	b := &Builder{Host: uihost.New(a), OutDir: out, ExtraDirs: map[string]fs.FS{"/files": wrapped}}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, rel := range []string{"files/leak.txt", "files/leakdir/inner.txt", "files/same.txt"} {
		if data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(rel))); err == nil {
			t.Errorf("SECURITY: [static] the export published %s, a symlink in a wrapped os.DirFS (%q)", rel, data)
		}
	}
	if got := readOut(t, out, "files/ok.txt"); got != "ok" {
		t.Errorf("files/ok.txt = %q, want ok", got)
	}
}
