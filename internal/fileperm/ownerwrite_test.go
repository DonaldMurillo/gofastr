//go:build !windows

package fileperm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteOwnerOnlyTightensExistingLooseFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteOwnerOnly(p, []byte("new")); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode after overwrite = %o, want 600", st.Mode().Perm())
	}
	got, _ := os.ReadFile(p)
	if string(got) != "new" {
		t.Fatalf("content = %q, want new", got)
	}
}

func TestWriteOwnerOnlyCreates0600(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fresh")
	if err := WriteOwnerOnly(p, []byte("x")); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode on create = %o, want 600", st.Mode().Perm())
	}
}

func TestSeedOwnerOnlyTightensAndKeepsContent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "db")
	if err := os.WriteFile(p, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SeedOwnerOnly(p); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", st.Mode().Perm())
	}
	got, _ := os.ReadFile(p)
	if string(got) != "keep" {
		t.Fatalf("SeedOwnerOnly truncated the file: %q", got)
	}
}

func TestWriteOwnerOnlyReportsOpenFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing-dir", "secret")
	if err := WriteOwnerOnly(p, []byte("x")); err == nil {
		t.Fatal("WriteOwnerOnly into a missing directory must fail")
	}
	if err := SeedOwnerOnly(p); err == nil {
		t.Fatal("SeedOwnerOnly into a missing directory must fail")
	}
}

func TestWriteOwnerOnlyRefusesDirectoryTarget(t *testing.T) {
	// A directory cannot be opened for writing: the failure must surface
	// from the open, never from a later write on a half-opened handle.
	dir := t.TempDir()
	if err := WriteOwnerOnly(dir, []byte("x")); err == nil {
		t.Fatal("WriteOwnerOnly on a directory must fail")
	}
}

func TestFillOwnerOnlyReportsHandleFailures(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	closed, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	if err := fillOwnerOnly(closed, []byte("x")); err == nil {
		t.Fatal("chmod on a closed handle must fail")
	}
	if err := tightenAndClose(closed); err == nil {
		t.Fatal("tightenAndClose on a closed handle must fail")
	}
	// Chmod succeeds on a read-only handle (owner), the write cannot.
	ro, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := fillOwnerOnly(ro, []byte("x")); err == nil {
		t.Fatal("write on a read-only handle must fail")
	}
}

func TestCreateOwnerOnlyTightensAndTruncates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "stream")
	if err := os.WriteFile(p, []byte("old secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := CreateOwnerOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 || st.Size() != 0 {
		t.Fatalf("handle mode %o size %d before any write, want 600 and 0", st.Mode().Perm(), st.Size())
	}
	if _, err := f.WriteString("new"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "new" {
		t.Fatalf("content = %q, want new", got)
	}
	if _, err := CreateOwnerOnly(filepath.Join(t.TempDir(), "missing-dir", "x")); err == nil {
		t.Fatal("CreateOwnerOnly into a missing directory must fail")
	}
}

func TestRestrictedHandleReportsChmodFailure(t *testing.T) {
	closed, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	if f, err := restrictedHandle(closed); err == nil || f != nil {
		t.Fatalf("restrictedHandle on a closed handle = %v, %v; want nil and an error", f, err)
	}
}
