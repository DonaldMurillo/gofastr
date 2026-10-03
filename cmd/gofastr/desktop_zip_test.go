package main

// Round-trip tests for the pure-Go bundle zipper: entry names relative
// to the bundle's parent (Notes.app/...), directories included, the
// executable bit preserved, no timestamps in the archive (two runs of
// the same tree produce identical bytes), and symlinks refused with a
// clear error naming the path.

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// writeFakeBundle lays out a minimal .app with the modes the real
// builder produces: an executable under Contents/MacOS, a plist, and
// an icon.
func writeFakeBundle(t *testing.T, dir string) string {
	t.Helper()
	appDir := filepath.Join(dir, "Notes.app")
	macOS := filepath.Join(appDir, "Contents", "MacOS")
	resources := filepath.Join(appDir, "Contents", "Resources")
	for _, d := range []string{macOS, resources} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(macOS, "Notes"), []byte("#!macho-bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "Contents", "Info.plist"), []byte("plist-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "icon.icns"), []byte("icns-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return appDir
}

func TestZipBundleRoundTripModes(t *testing.T) {
	dir := t.TempDir()
	appDir := writeFakeBundle(t, dir)
	zipPath := filepath.Join(dir, "Notes.zip")
	if err := zipAppBundle(appDir, zipPath); err != nil {
		t.Fatalf("zipAppBundle: %v", err)
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// WalkDir's lexical order: Contents before Info.plist before MacOS
	// before Resources, directories and files interleaved.
	want := []string{
		"Notes.app/",
		"Notes.app/Contents/",
		"Notes.app/Contents/Info.plist",
		"Notes.app/Contents/MacOS/",
		"Notes.app/Contents/MacOS/Notes",
		"Notes.app/Contents/Resources/",
		"Notes.app/Contents/Resources/icon.icns",
	}
	var names []string
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	if !slices.Equal(names, want) {
		t.Fatalf("entry names:\ngot  %v\nwant %v", names, want)
	}

	for _, f := range r.File {
		if f.ModifiedDate != 0 || f.ModifiedTime != 0 {
			t.Errorf("entry %s carries a timestamp (%d/%d), want none", f.Name, f.ModifiedDate, f.ModifiedTime)
		}
		switch f.Name {
		case "Notes.app/Contents/MacOS/Notes":
			if f.Mode()&0o111 == 0 {
				t.Errorf("executable entry mode = %v, want the exec bits", f.Mode())
			}
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != "#!macho-bytes" {
				t.Errorf("entry content = %q", b)
			}
		case "Notes.app/Contents/Info.plist", "Notes.app/Contents/Resources/icon.icns":
			if f.Mode()&0o111 != 0 {
				t.Errorf("entry %s mode = %v, want no exec bits", f.Name, f.Mode())
			}
		case "Notes.app/", "Notes.app/Contents/", "Notes.app/Contents/MacOS/", "Notes.app/Contents/Resources/":
			if !f.Mode().IsDir() {
				t.Errorf("entry %s is not a directory (mode %v)", f.Name, f.Mode())
			}
		}
	}
}

func TestZipBundleRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	appDir := writeFakeBundle(t, dir)
	if err := os.Symlink(
		filepath.Join(appDir, "Contents", "Info.plist"),
		filepath.Join(appDir, "Contents", "MacOS", "lnk"),
	); err != nil {
		t.Skipf("host cannot create symlinks: %v", err)
	}
	zipPath := filepath.Join(dir, "Notes.zip")
	err := zipAppBundle(appDir, zipPath)
	if err == nil {
		t.Fatal("symlink zipped without complaint")
	}
	if !strings.Contains(err.Error(), "symlink") || !strings.Contains(err.Error(), filepath.Join("MacOS", "lnk")) {
		t.Fatalf("error does not name the symlink:\n%v", err)
	}
	if _, statErr := os.Stat(zipPath); statErr == nil {
		t.Error("partial zip left behind after the refusal")
	}
}

func TestZipBundleDeterministicBytes(t *testing.T) {
	dir := t.TempDir()
	appDir := writeFakeBundle(t, dir)
	a := filepath.Join(dir, "a.zip")
	b := filepath.Join(dir, "b.zip")
	if err := zipAppBundle(appDir, a); err != nil {
		t.Fatal(err)
	}
	// Age one file a day between the runs: the archive zeroes mtimes,
	// so a day-old tree and a fresh one must still zip identically.
	old := time.Now().Add(24 * time.Hour)
	if err := os.Chtimes(filepath.Join(appDir, "Contents", "MacOS", "Notes"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := zipAppBundle(appDir, b); err != nil {
		t.Fatal(err)
	}
	ba, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ba, bb) {
		t.Fatalf("two zips of the same tree differ (%d vs %d bytes)", len(ba), len(bb))
	}
}
