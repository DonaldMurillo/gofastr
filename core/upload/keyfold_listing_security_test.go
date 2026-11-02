package upload

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fold walk (RefuseFoldedKey) enumerated the parent of every existing
// key component on every Get, GetRange, Exists, Delete and overwriting
// Save: a readdir plus an lstat per entry until the exact name turned up.
// Upload directories are flat by design (uploads/<entity>/<field>/), so
// the cost of serving one object grew with the number of objects stored
// beside it: 50,000 avatars made each unauthenticated GET ~85ms of
// syscalls, and Save ran the same walk under the process-wide commit lock.
// Property: on a directory that cannot fold the requested name, the walk
// never lists it. Where folding is possible it still does.

// caseSensitiveTempDir returns a temp dir on a filesystem that keeps "A"
// and "a" apart, or skips: on a folding filesystem every name must be
// listed and there is nothing to measure.
func caseSensitiveTempDir(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "probe-A"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "probe-a")); err == nil {
		t.Skip("filesystem folds case; the fold walk must list here")
	}
	if err := os.Remove(filepath.Join(base, "probe-A")); err != nil {
		t.Fatal(err)
	}
	return base
}

func countFoldListings(t *testing.T) *int {
	t.Helper()
	n := new(int)
	foldListHook = func(string) { *n++ }
	t.Cleanup(func() { foldListHook = nil })
	return n
}

func TestFoldWalkDoesNotListCaseSensitiveDirs(t *testing.T) {
	base := caseSensitiveTempDir(t)
	ctx := context.Background()
	s := NewLocalStorage(base)
	t.Cleanup(func() { s.Close() })

	const key = "uploads/posts/avatar/photo_1_abc.png"
	for _, k := range []string{key, "uploads/posts/avatar/other_2_def.png"} {
		if err := s.Save(ctx, k, strings.NewReader("x")); err != nil {
			t.Fatalf("Save %q: %v", k, err)
		}
	}

	listings := countFoldListings(t)
	ops := []struct {
		name string
		run  func() error
	}{
		{"Get", func() error {
			rc, err := s.Get(ctx, key)
			if err == nil {
				_, _ = io.Copy(io.Discard, rc)
				rc.Close()
			}
			return err
		}},
		{"GetRange", func() error {
			rc, err := s.GetRange(ctx, key)
			if err == nil {
				rc.Close()
			}
			return err
		}},
		{"Exists", func() error { _, err := s.Exists(ctx, key); return err }},
		{"Save overwrite", func() error { return s.Save(ctx, key, strings.NewReader("y")) }},
		{"Delete", func() error { return s.Delete(ctx, key) }},
	}
	for _, op := range ops {
		*listings = 0
		if err := op.run(); err != nil {
			t.Fatalf("%s: %v", op.name, err)
		}
		if *listings != 0 {
			t.Errorf("%s listed %d director(ies) on a case-sensitive filesystem; the cost of one object grows with its directory", op.name, *listings)
		}
	}
}

// A directory that resolves the case-swapped spelling to the same file
// is what a folding filesystem looks like; a hard link spelled that way
// stands in for one here. The walk must fall back to the listing, and
// the exact name it finds there keeps the key usable.
func TestFoldWalkListsWhenTheSwappedSpellingIsTheSameFile(t *testing.T) {
	base := caseSensitiveTempDir(t)
	ctx := context.Background()
	s := NewLocalStorage(base)
	t.Cleanup(func() { s.Close() })

	if err := s.Save(ctx, "docs/report.txt", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(base, "docs", "report.txt"), filepath.Join(base, "docs", "REPORT.TXT")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	listings := countFoldListings(t)
	rc, err := s.Get(ctx, "docs/report.txt")
	if err != nil {
		t.Fatalf("Get of the exact stored key: %v", err)
	}
	rc.Close()
	if *listings == 0 {
		t.Fatal("the swapped spelling resolved to the same file, yet the walk skipped the listing")
	}
}

// Names a case-sensitive filesystem can still fold by normalization are
// always listed: any non-ASCII byte, and the ASCII targets of the
// canonical singletons (K from U+212A, ';' from U+037E, '`' from U+1FEF).
func TestDirMayFoldListsNormalizationCandidates(t *testing.T) {
	base := caseSensitiveTempDir(t)
	for _, name := range []string{"café.txt", "Key.png", "a;b.txt", "a`b.txt"} {
		p := filepath.Join(base, name)
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		if !dirMayFold(fi, name, base, nil) {
			t.Errorf("dirMayFold(%q) = false; a normalization-insensitive filesystem can fold it", name)
		}
	}
	p := filepath.Join(base, "123_456.png")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Lstat(p)
	if dirMayFold(fi, "123_456.png", base, nil) {
		t.Error("dirMayFold of an ASCII name whose swapped spelling is absent = true; want false")
	}
}
