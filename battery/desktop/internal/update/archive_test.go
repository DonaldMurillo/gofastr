package update

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// buildZip assembles an in-memory zip from name -> (content, mode)
// pairs; a nil content marks a directory entry.
func buildZip(t *testing.T, entries map[string]struct {
	content []byte
	mode    fs.FileMode
}) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, e := range entries {
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		hdr.SetMode(e.mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if e.content != nil {
			if _, err := w.Write(e.content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// bundleZip is a minimal Notes.app bundle: dirs, an executable, a file.
func bundleZip(t *testing.T) []byte {
	t.Helper()
	return buildZip(t, map[string]struct {
		content []byte
		mode    fs.FileMode
	}{
		"Notes.app/":                     {nil, 0o755 | fs.ModeDir},
		"Notes.app/Contents/":            {nil, 0o755 | fs.ModeDir},
		"Notes.app/Contents/MacOS/":      {nil, 0o755 | fs.ModeDir},
		"Notes.app/Contents/MacOS/Notes": {[]byte("#!binary\n"), 0o755},
		"Notes.app/Contents/Info.plist":  {[]byte(`<?xml version="1.0"?><plist/>`), 0o644},
		"Notes.app/Contents/Resources/":  {nil, 0o755 | fs.ModeDir},
	})
}

func TestExtractZipWritesBundle(t *testing.T) {
	dest := t.TempDir()
	app, err := ExtractZip(bundleZip(t), dest, "Notes.app")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dest, "Notes.app"); app != want {
		t.Fatalf("app = %q, want %q", app, want)
	}
	exe := filepath.Join(app, "Contents", "MacOS", "Notes")
	info, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("executable mode = %o, want 755", info.Mode().Perm())
	}
	plist := filepath.Join(app, "Contents", "Info.plist")
	pinfo, err := os.Stat(plist)
	if err != nil {
		t.Fatal(err)
	}
	if pinfo.Mode().Perm() != 0o644 {
		t.Fatalf("plist mode = %o, want 644", pinfo.Mode().Perm())
	}
	data, err := os.ReadFile(plist)
	if err != nil || len(data) == 0 {
		t.Fatalf("plist = %q, %v", data, err)
	}
}

func TestExtractZipRefusesEscapeEntries(t *testing.T) {
	base := func() map[string]struct {
		content []byte
		mode    fs.FileMode
	} {
		return map[string]struct {
			content []byte
			mode    fs.FileMode
		}{
			"Notes.app/Contents/Info.plist": {[]byte("x"), 0o644},
		}
	}
	for _, evil := range []string{
		"../evil.txt",
		"Notes.app/../../evil.txt",
		"/etc/evil",
		"Notes.app/Contents/../../../../../../../../tmp/evil.txt",
		"..\\evil.txt",
		"C:/evil.txt",
	} {
		entries := base()
		entries[evil] = struct {
			content []byte
			mode    fs.FileMode
		}{[]byte("x"), 0o644}
		data := buildZip(t, entries)
		if _, err := ExtractZip(data, t.TempDir(), "Notes.app"); !errors.Is(err, ErrBadArchive) {
			t.Errorf("entry %q: err = %v, want ErrBadArchive", evil, err)
		}
	}
}

func TestExtractZipRefusesWrongRoot(t *testing.T) {
	// A different bundle name inside.
	other := buildZip(t, map[string]struct {
		content []byte
		mode    fs.FileMode
	}{
		"Other.app/Contents/x": {[]byte("x"), 0o644},
	})
	if _, err := ExtractZip(other, t.TempDir(), "Notes.app"); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("err = %v, want ErrBadArchive (name mismatch)", err)
	}
	// Two top-level bundles.
	two := bundleZip(t)
	two = append(two, buildZip(t, map[string]struct {
		content []byte
		mode    fs.FileMode
	}{
		"Extra.app/Contents/x": {[]byte("x"), 0o644},
	})...)
	if _, err := ExtractZip(two, t.TempDir(), "Notes.app"); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("two roots: err = %v, want ErrBadArchive", err)
	}
	// A top-level file named exactly like the bundle.
	flat := buildZip(t, map[string]struct {
		content []byte
		mode    fs.FileMode
	}{
		"Notes.app": {[]byte("x"), 0o755},
	})
	if _, err := ExtractZip(flat, t.TempDir(), "Notes.app"); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("flat file: err = %v, want ErrBadArchive", err)
	}
	// Not a zip at all.
	if _, err := ExtractZip([]byte("not a zip"), t.TempDir(), "Notes.app"); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("not a zip: err = %v", err)
	}
}

func TestExtractZipRefusesSymlinkEntry(t *testing.T) {
	entries := map[string]struct {
		content []byte
		mode    fs.FileMode
	}{
		"Notes.app/Contents/":           {nil, 0o755 | fs.ModeDir},
		"Notes.app/Contents/Info.plist": {[]byte("x"), 0o644},
		"Notes.app/Contents/link":       {[]byte("../../../../etc/passwd"), fs.ModeSymlink | 0o777},
	}
	data := buildZip(t, entries)
	if _, err := ExtractZip(data, t.TempDir(), "Notes.app"); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("err = %v, want ErrBadArchive (symlink refused)", err)
	}
}

func TestExtractZipKeepsStagingInsideRoot(t *testing.T) {
	// The extraction root is the only place entries may land; nothing
	// may appear beside it even for deep paths.
	outside := t.TempDir()
	dest := filepath.Join(outside, "stage")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractZip(bundleZip(t), dest, "Notes.app"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "stage" {
		t.Fatalf("outside dir now holds %d entries, want just the staging root", len(entries))
	}
}

func TestVerifyArchiveChecksSumAndSize(t *testing.T) {
	data := bundleZip(t)
	sum := sha256.Sum256(data)
	entry := PlatformEntry{
		URL:    "https://example.com/Notes.zip",
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(data)),
	}
	if err := VerifyArchive(data, entry); err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), data...)
	tampered[len(tampered)-1] ^= 0xff
	if err := VerifyArchive(tampered, entry); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("tampered err = %v", err)
	}
	entry.Size++
	if err := VerifyArchive(data, entry); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("size-mismatch err = %v", err)
	}
	entry.SHA256 = "zz" + entry.SHA256[2:]
	entry.Size = int64(len(data))
	if err := VerifyArchive(data, entry); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("bad-hex err = %v", err)
	}
}

func TestCleanEntryNameRefusesEscapes(t *testing.T) {
	ok := []string{
		"Notes.app",
		"Notes.app/Contents/Info.plist",
		"Notes.app/Contents/MacOS/Notes",
	}
	for _, name := range ok {
		if got, good := cleanEntryName(name); !good || got != name {
			t.Errorf("cleanEntryName(%q) = %q, %v; want unchanged, true", name, got, good)
		}
	}
	bad := []string{
		"..", "../evil", "Notes.app/../../evil", "/etc/passwd",
		"Notes.app/Contents/../../../../../../../../tmp/evil.txt",
		"..\\\\evil", "\\\\.\\\\etc", "C:/evil.txt", "c:/evil.txt", ".",
	}
	for _, name := range bad {
		if _, good := cleanEntryName(name); good {
			t.Errorf("cleanEntryName(%q) accepted", name)
		}
	}
}
