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
