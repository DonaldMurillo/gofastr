package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Backends that hold their bytes locally can seek; Storage.Get just erased it
// behind io.ReadCloser, which is what blocked HTTP range requests.
func TestLocalAndMemoryServeRanges(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	mem := NewMemoryStorage()
	if err := mem.Save(context.Background(), "f.bin", strings.NewReader("0123456789")); err != nil {
		t.Fatal(err)
	}

	for name, s := range map[string]Storage{
		"local":  NewLocalStorage(dir),
		"memory": mem,
	} {
		t.Run(name, func(t *testing.T) {
			rg, ok := s.(RangeGetter)
			if !ok {
				t.Fatalf("%s does not implement RangeGetter", name)
			}
			rs, err := rg.GetRange(context.Background(), "f.bin")
			if err != nil {
				t.Fatalf("GetRange: %v", err)
			}
			defer rs.Close()
			if _, err := rs.Seek(3, io.SeekStart); err != nil {
				t.Fatalf("Seek: %v", err)
			}
			got, err := io.ReadAll(rs)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if string(got) != "3456789" {
				t.Errorf("after Seek(3) read %q, want %q", got, "3456789")
			}
		})
	}
}

// GetRange must run the same key validation as Get. A capability that skipped
// it would be a path-traversal hole with a performance justification.
func TestGetRangeRejectsTraversal(t *testing.T) {
	ls := NewLocalStorage(t.TempDir())
	if _, err := ls.GetRange(context.Background(), "../../etc/passwd"); err == nil {
		t.Fatal("SECURITY: LocalStorage.GetRange accepted a traversal key")
	}
	mem := NewMemoryStorage()
	if _, err := mem.GetRange(context.Background(), "nope"); err == nil {
		t.Fatal("MemoryStorage.GetRange returned no error for a missing key")
	}
}

// A backend that declines the capability must stay a valid Storage, the
// point of a capability interface is that not implementing it is legal.
func TestS3DeclinesRangeGetter(t *testing.T) {
	var s Storage = NewS3Storage("bucket", "us-east-1")
	if _, ok := s.(RangeGetter); ok {
		t.Error("S3Storage claims RangeGetter; a network backend must buffer to seek, so it should decline and let callers presign")
	}
}

// The seekable reader must still return the whole object when read from the
// start, the capability adds resumability, it does not change the bytes.
func TestGetRangeWholeObjectMatchesGet(t *testing.T) {
	mem := NewMemoryStorage()
	want := []byte("the quick brown fox")
	if err := mem.Save(context.Background(), "k", bytes.NewReader(want)); err != nil {
		t.Fatal(err)
	}
	rs, err := mem.GetRange(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	got, _ := io.ReadAll(rs)
	if !bytes.Equal(got, want) {
		t.Errorf("GetRange full read = %q, want %q", got, want)
	}
}
