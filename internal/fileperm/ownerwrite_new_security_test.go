//go:build !windows

package fileperm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// WriteNewOwnerOnly creates exclusively: an existing file, a live
// symlink and a dangling one are all refused, and nothing is written
// through them.
func TestWriteNewOwnerOnlyRefusesLinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(dir, "live")
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(target, live); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "created-through-link"), dangling); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{target, live, dangling} {
		if err := WriteNewOwnerOnly(p, []byte("secret")); !os.IsExist(err) {
			t.Fatalf("%s: err = %v, want exist", filepath.Base(p), err)
		}
	}
	if got, _ := os.ReadFile(target); string(got) != "keep" {
		t.Fatalf("the link target was written: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(dir, "created-through-link")); !os.IsNotExist(err) {
		t.Fatal("a dangling link's target was created")
	}
	fresh := filepath.Join(dir, "fresh")
	if err := WriteNewOwnerOnly(fresh, []byte("secret")); err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(fresh)
	if err != nil || st.Mode() != 0o600 {
		t.Fatalf("fresh file mode = %v, %v; want 0600 regular", st.Mode(), err)
	}
}

// A fill that fails removes the file the exclusive create made, so a
// failed keygen never leaves half a private key behind.
func TestCreateExclusiveRemovesOnFail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "key")
	want := errors.New("disk full")
	err := createExclusive(p, []byte("secret"), func(f *os.File, _ []byte) error {
		f.Close()
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want the fill error", err)
	}
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatalf("the half-written file survived: %v", err)
	}
}

// PublishNewOwnerOnly refuses what WriteNewOwnerOnly refuses (an
// existing file, a live symlink, a dangling one), writes nothing
// through them, and leaves no temporary file behind either way.
func TestPublishNewOwnerOnlyRefusesLinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(dir, "live")
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(target, live); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "created-through-link"), dangling); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{target, live, dangling} {
		if err := PublishNewOwnerOnly(p, []byte("secret")); !errors.Is(err, os.ErrExist) {
			t.Fatalf("%s: err = %v, want exist", filepath.Base(p), err)
		}
	}
	if got, _ := os.ReadFile(target); string(got) != "keep" {
		t.Fatalf("the link target was written: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(dir, "created-through-link")); !os.IsNotExist(err) {
		t.Fatal("a dangling link's target was created")
	}
	fresh := filepath.Join(dir, "fresh")
	if err := PublishNewOwnerOnly(fresh, []byte("secret")); err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(fresh)
	if err != nil || st.Mode() != 0o600 {
		t.Fatalf("fresh file mode = %v, %v; want 0600 regular", st.Mode(), err)
	}
	if got, _ := os.ReadFile(fresh); string(got) != "secret" {
		t.Fatalf("fresh file holds %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("dir holds %v, want target, live, dangling and fresh only", names)
	}
}

// TightenOpenFile clears group and other bits through a read-only
// handle and leaves an owner-only mode as it is.
func TestTightenOpenFileThroughReadHandle(t *testing.T) {
	for from, want := range map[os.FileMode]os.FileMode{0o644: 0o600, 0o640: 0o600, 0o604: 0o600, 0o600: 0o600, 0o400: 0o400} {
		path := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, from); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		err = TightenOpenFile(f)
		f.Close()
		if err != nil {
			t.Fatalf("%o: %v", from, err)
		}
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := st.Mode().Perm(); got != want {
			t.Errorf("TightenOpenFile on %o left %o, want %o", from, got, want)
		}
	}
}

// A publish whose fill fails returns that error, creates nothing at
// path and leaves no temporary file; one into a missing directory fails
// before writing anything; a fill on a closed handle reports it.
func TestPublishNewOwnerOnlyFailures(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "secret")
	want := errors.New("disk full")
	err := publishExclusive(p, []byte("secret"), func(f *os.File, _ []byte) error {
		f.Close()
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want the fill error", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a failed publish left %d files behind", len(entries))
	}
	if err := PublishNewOwnerOnly(filepath.Join(dir, "missing", "secret"), []byte("x")); !os.IsNotExist(err) {
		t.Fatalf("publish into a missing directory: err = %v, want not-exist", err)
	}
	f, err := os.CreateTemp(dir, "closed")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := fillSynced(f, []byte("x")); err == nil {
		t.Fatal("fillSynced on a closed handle returned nil")
	}
	if err := TightenOpenFile(f); err == nil {
		t.Fatal("TightenOpenFile on a closed handle returned nil")
	}
}
