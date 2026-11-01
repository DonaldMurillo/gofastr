package update

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseInfoPlistReadsIdentity(t *testing.T) {
	info, err := ParseInfoPlist(testInfoPlist("dev.gofastr.notes", "1.2.0"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Identifier != "dev.gofastr.notes" || info.ShortVersion != "1.2.0" {
		t.Fatalf("info = %+v", info)
	}
}

func TestParseInfoPlistRefusesAmbiguity(t *testing.T) {
	good := string(testInfoPlist("dev.gofastr.notes", "1.2.0"))
	cases := map[string]string{
		"binary":         "bplist00" + good,
		"missing id":     strings.Replace(good, "<key>CFBundleIdentifier</key>\n\t<string>dev.gofastr.notes</string>", "", 1),
		"missing ver":    strings.Replace(good, "<key>CFBundleShortVersionString</key>\n\t<string>1.2.0</string>", "", 1),
		"duplicate ver":  strings.Replace(good, "<key>NSHighResolutionCapable</key>", "<key>CFBundleShortVersionString</key><string>9.9.9</string><key>NSHighResolutionCapable</key>", 1),
		"non-string ver": strings.Replace(good, "<string>1.2.0</string>", "<integer>1</integer>", 1),
		"empty id":       strings.Replace(good, "<string>dev.gofastr.notes</string>", "<string></string>", 1),
		"array root":     strings.Replace(strings.Replace(good, "<dict>\n\t<key>CFBundleName", "<array><dict>\n\t<key>CFBundleName", 1), "</dict>\n</plist>", "</dict></array>\n</plist>", 1),
		"not xml":        "not a plist",
	}
	for name, body := range cases {
		if info, err := ParseInfoPlist([]byte(body)); !errors.Is(err, ErrBadBundleInfo) {
			t.Errorf("%s: info=%+v err=%v", name, info, err)
		}
	}
}

func TestReadBundleInfoFromExtractedApp(t *testing.T) {
	dest := t.TempDir()
	app, err := ExtractZip(bundleZip(t), dest, "Notes.app")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ReadBundleInfo(app)
	if err != nil || info.ShortVersion != "1.2.0" || info.Identifier != "dev.gofastr.notes" {
		t.Fatalf("info = %+v, %v", info, err)
	}
	if err := os.Remove(filepath.Join(app, "Contents", "Info.plist")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBundleInfo(app); !errors.Is(err, ErrBadBundleInfo) {
		t.Fatalf("bundle without Info.plist read: %v", err)
	}
}

func TestReadBundleInfoStaysInsideBundle(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "Info.plist")
	if err := os.WriteFile(outside, testInfoPlist("dev.gofastr.notes", "1.2.0"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(t.TempDir(), "Notes.app")
	if err := os.MkdirAll(filepath.Join(app, "Contents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(app, "Contents", "Info.plist")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBundleInfo(app); !errors.Is(err, ErrBadBundleInfo) {
		t.Fatalf("Info.plist symlinked out of the bundle was read: %v", err)
	}
}

func TestWriteFeedRefusesVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	keyPath, _, _, _ := writeKeyFile(t, dir)
	archive := writeArchive(t, dir) // Info.plist says 1.2.0
	err := WriteFeed(filepath.Join(dir, "out"), keyPath, "1.3.0", "", "darwin-arm64", archive, "https://e.com/a.zip")
	if err == nil || !strings.Contains(err.Error(), "CFBundleShortVersionString") {
		t.Fatalf("feed labelled 1.3.0 over a 1.2.0 bundle: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "out", "manifest.json")); !os.IsNotExist(statErr) {
		t.Fatal("a refused feed still wrote manifest.json")
	}
	noPlist := filepath.Join(dir, "noplist.zip")
	data := buildZip(t, map[string]struct {
		content []byte
		mode    fs.FileMode
	}{
		// The directory entries keep the archive one the extractor
		// accepts, so the refusal under test is the missing plist.
		"Notes.app/":                     {nil, 0o755 | fs.ModeDir},
		"Notes.app/Contents/":            {nil, 0o755 | fs.ModeDir},
		"Notes.app/Contents/MacOS/":      {nil, 0o755 | fs.ModeDir},
		"Notes.app/Contents/MacOS/Notes": {[]byte("x"), 0o755},
	})
	if err := os.WriteFile(noPlist, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFeed(filepath.Join(dir, "o2"), keyPath, "1.2.0", "", "darwin-arm64", noPlist, "https://e.com/a.zip"); !errors.Is(err, ErrBadBundleInfo) {
		t.Fatalf("archive without Info.plist accepted: %v", err)
	}
}
