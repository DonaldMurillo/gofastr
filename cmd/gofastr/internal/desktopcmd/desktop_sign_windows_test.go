//go:build windows

package desktopcmd

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func TestSigntoolArgs(t *testing.T) {
	thumb := strings.Repeat("ab", 20)
	for _, tc := range []struct {
		cert, ts string
		want     []string
	}{
		{thumb, defaultTimestampURL, []string{"sign", "/fd", "sha256", "/sha1", thumb, "/tr", defaultTimestampURL, "/td", "sha256", `C:\out\Notes.exe`}},
		{"Acme Corp", "none", []string{"sign", "/fd", "sha256", "/n", "Acme Corp", `C:\out\Notes.exe`}},
		// 39 hex chars is a subject substring, not a thumbprint.
		{thumb[:39], "none", []string{"sign", "/fd", "sha256", "/n", thumb[:39], `C:\out\Notes.exe`}},
	} {
		if got := signtoolArgs(`C:\out\Notes.exe`, tc.cert, tc.ts); !slices.Equal(got, tc.want) {
			t.Errorf("signtoolArgs(%q, %q) = %q, want %q", tc.cert, tc.ts, got, tc.want)
		}
	}
}

func TestSignWindowsExecutableRunsSigntool(t *testing.T) {
	origLook, origRun := lookSigntool, runTool
	t.Cleanup(func() { lookSigntool, runTool = origLook, origRun })
	lookSigntool = func() (string, error) { return `C:\sdk\signtool.exe`, nil }
	var ran []string
	runTool = func(name string, args ...string) (string, error) {
		ran = append([]string{name}, args...)
		return "", nil
	}
	if err := signWindowsExecutable(`C:\out\Notes.exe`, "Acme", "http://ts.example"); err != nil {
		t.Fatal(err)
	}
	want := []string{`C:\sdk\signtool.exe`, "sign", "/fd", "sha256", "/n", "Acme", "/tr", "http://ts.example", "/td", "sha256", `C:\out\Notes.exe`}
	if !slices.Equal(ran, want) {
		t.Fatalf("ran %q, want %q", ran, want)
	}
}

func TestCompareSDKPathsIsNumeric(t *testing.T) {
	paths := []string{
		`C:\Kits\10\bin\10.0.22621.0\x64\signtool.exe`,
		`C:\Kits\10\bin\10.0.9600.0\x64\signtool.exe`,
		`C:\Kits\10\bin\10.0.17763.0\x64\signtool.exe`,
	}
	slices.SortFunc(paths, compareSDKPaths)
	if !strings.Contains(paths[2], "22621") || !strings.Contains(paths[0], "9600") {
		t.Fatalf("sorted = %q", paths)
	}
}

func TestZipWindowsBundleRefusesAPathName(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../Notes", "a/b", `a\b`, "C:Notes", "Notes/"} {
		if err := zipWindowsBundle(t.TempDir(), name, filepath.Join(t.TempDir(), "x.zip")); err == nil || !strings.Contains(err.Error(), "cannot name an archive directory") {
			t.Errorf("zipWindowsBundle(%q) = %v, want the name refusal", name, err)
		}
	}
}

// A failed archive write leaves an earlier archive at dest untouched
// and no temporary file behind.
func TestZipWindowsBundleKeepsTheOldArchiveOnFailure(t *testing.T) {
	out := t.TempDir()
	dest := filepath.Join(out, "Notes-1.0.0-windows-amd64.zip")
	if err := os.WriteFile(dest, []byte("previous archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	// No Notes.exe in out: the write fails part way.
	if err := zipWindowsBundle(out, "Notes", dest); err == nil {
		t.Fatal("zipped a build with no executable")
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != "previous archive" {
		t.Fatalf("dest after a failed write = %q, %v", got, err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("out holds %d entries after a failed write, want only the old archive", len(entries))
	}
}

// TestWindowsZipIsAFeedArchive builds the --zip archive from a fake
// build output and signs a feed for it with the release tooling's own
// writer, which runs the updater's extractor and identity check.
func TestWindowsZipIsAFeedArchive(t *testing.T) {
	out := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(out, "Notes.exe")
	if err := os.WriteFile(exe, data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeWindowsVersionResource(exe, windowsDesktopVersion{Name: "Notes", ID: "dev.gofastr.notes", Version: "1.3.0"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "WebView2Loader.dll"), []byte("loader"), 0o600); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "Notes-1.3.0-windows-amd64.zip")
	if err := zipWindowsBundle(out, "Notes", zipPath); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	zr.Close()
	if want := []string{"Notes/", "Notes/Notes.exe", "Notes/WebView2Loader.dll"}; !slices.Equal(names, want) {
		t.Fatalf("entries = %q, want %q", names, want)
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "release.key")
	if err := os.WriteFile(key, []byte(hex.EncodeToString(priv.Seed())), 0o600); err != nil {
		t.Fatal(err)
	}
	feedDir := t.TempDir()
	if err := desktop.SignUpdateFeed(feedDir, key, "1.3.0", "", "windows-amd64", zipPath, "https://example.com/Notes.zip"); err != nil {
		t.Fatalf("feed refused the --zip archive: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(feedDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Platforms map[string]json.RawMessage `json:"platforms"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.Platforms["windows-amd64"] == nil {
		t.Fatalf("manifest = %s, %v", raw, err)
	}
	err = desktop.SignUpdateFeed(t.TempDir(), key, "1.4.0", "", "windows-amd64", zipPath, "https://example.com/Notes.zip")
	if err == nil || !strings.Contains(err.Error(), "ProductVersion") {
		t.Fatalf("a mislabelled Windows archive: err = %v, want the ProductVersion mismatch", err)
	}
	// The same archive under a darwin key is not a .app bundle.
	if err := desktop.SignUpdateFeed(t.TempDir(), key, "1.3.0", "", "darwin-arm64", zipPath, "https://example.com/Notes.zip"); err == nil {
		t.Fatal("a Windows archive was signed as a darwin bundle")
	}
}
