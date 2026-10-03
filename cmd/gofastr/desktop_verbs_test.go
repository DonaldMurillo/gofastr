package main

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The desktop release verbs: keygen mints the signing pair, feed
// writes a manifest the updater's verifier accepts. Pure Go, exercised
// end to end against temp dirs; the feed test verifies the signature
// with the plain stdlib so it checks the FORMAT, not our own code
// twice.

// verbOutput runs fn capturing stdout and the requested exit code.
func verbOutput(t *testing.T, fn func()) (string, int) {
	t.Helper()
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, fn)
	})
	return out, code
}

// buildFeedArchive writes a minimal Notes.app zip and returns its path.
func buildFeedArchive(t *testing.T, dir string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := map[string]struct {
		content []byte
		mode    fs.FileMode
	}{
		"Notes.app/":                     {nil, 0o755 | fs.ModeDir},
		"Notes.app/Contents/":            {nil, 0o755 | fs.ModeDir},
		"Notes.app/Contents/MacOS/":      {nil, 0o755 | fs.ModeDir},
		"Notes.app/Contents/MacOS/Notes": {[]byte("#!binary"), 0o755},
		"Notes.app/Contents/Info.plist":  {[]byte(`<?xml version="1.0"?><plist/>`), 0o644},
	}
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
	p := filepath.Join(dir, "Notes-1.2.0.zip")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDesktopKeygenWritesPair(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	_, code := verbOutput(t, func() { runDesktopKeygen([]string{"-o", key}) })
	if code != -1 {
		t.Fatalf("keygen exited %d", code)
	}
	priv, err := os.ReadFile(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(priv) != 128 {
		t.Fatalf("private key file is %d hex chars, want 128 (seed+pub)", len(priv))
	}
	info, err := os.Stat(key)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private key mode = %v, %v; want 0600", info, err)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if len(pub) != 64 {
		t.Fatalf("public key file is %d hex chars, want 64", len(pub))
	}
	if info, err := os.Stat(key + ".pub"); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("public key mode = %v, %v; want 0600", info, err)
	}
	// The seed half derives the pub half.
	seed, err := hex.DecodeString(string(priv)[:64])
	if err != nil {
		t.Fatal(err)
	}
	derived := hex.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	if derived != string(pub) {
		t.Fatal("the .pub file does not match the private key's public half")
	}
}

func TestDesktopKeygenNeedsOutput(t *testing.T) {
	_, code := verbOutput(t, func() { runDesktopKeygen(nil) })
	if code != 1 {
		t.Fatalf("keygen without -o exited %d, want 1", code)
	}
}

func TestDesktopKeygenRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	if err := os.WriteFile(key, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code := verbOutput(t, func() { runDesktopKeygen([]string{"-o", key}) })
	if code != 1 {
		t.Fatalf("keygen over an existing key exited %d, want 1", code)
	}
	if !strings.Contains(out, "refuses to overwrite") {
		t.Fatalf("output = %q", out)
	}
}

func TestDesktopKeygenFlagParsing(t *testing.T) {
	f := parseDesktopKeygenFlags([]string{"-o", "/tmp/k"})
	if f.out != "/tmp/k" {
		t.Fatalf("out = %q", f.out)
	}
	f = parseDesktopKeygenFlags([]string{"--out=/tmp/k2"})
	if f.out != "/tmp/k2" {
		t.Fatalf("out = %q", f.out)
	}
}

func TestDesktopFeedWritesVerifiedPair(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	if _, code := verbOutput(t, func() { runDesktopKeygen([]string{"-o", key}) }); code != -1 {
		t.Fatalf("keygen exited %d", code)
	}
	archive := buildFeedArchive(t, dir)
	out := filepath.Join(dir, "feed")
	_, code := verbOutput(t, func() {
		runDesktopFeed([]string{
			"--key", key,
			"--version", "1.2.0",
			"--notes", "What changed",
			"--platform", "darwin-arm64",
			"--archive", archive,
			"--url", "https://example.com/Notes-1.2.0.zip",
			"-o", out,
		})
	})
	if code != -1 {
		t.Fatalf("feed exited %d", code)
	}

	manifest, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	sigFile, err := os.ReadFile(filepath.Join(out, "manifest.json.sig"))
	if err != nil {
		t.Fatal(err)
	}

	// Verify the pair with the stdlib, independent of the battery.
	pubHex, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	pub, err := hex.DecodeString(string(pubHex))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := base64.StdEncoding.DecodeString(string(sigFile))
	if err != nil || len(sig) != ed25519.SignatureSize {
		t.Fatalf("sig decode = %v (%d bytes)", err, len(sig))
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), manifest, sig) {
		t.Fatal("the signature does not verify over the exact manifest bytes")
	}

	var m map[string]any
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatalf("manifest is not JSON: %v", err)
	}
	if m["version"] != "1.2.0" || m["notes"] != "What changed" {
		t.Fatalf("manifest = %v", m)
	}
	platforms, ok := m["platforms"].(map[string]any)
	if !ok {
		t.Fatalf("manifest platforms = %v", m["platforms"])
	}
	entry, ok := platforms["darwin-arm64"].(map[string]any)
	if !ok {
		t.Fatalf("darwin-arm64 entry = %v", platforms)
	}
	if entry["url"] != "https://example.com/Notes-1.2.0.zip" {
		t.Fatalf("entry url = %v", entry["url"])
	}
	// sha256 and size computed from the archive on disk.
	archiveBytes, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := entry["size"].(float64), float64(len(archiveBytes)); got != want {
		t.Fatalf("entry size = %v, want %v", got, want)
	}
	sum := hex.EncodeToString(sum256(archiveBytes))
	if entry["sha256"] != sum {
		t.Fatalf("entry sha256 = %v, want %v", entry["sha256"], sum)
	}
}

func TestDesktopFeedRefusesBadInputs(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	if _, code := verbOutput(t, func() { runDesktopKeygen([]string{"-o", key}) }); code != -1 {
		t.Fatalf("keygen exited %d", code)
	}
	archive := buildFeedArchive(t, dir)
	out := filepath.Join(dir, "feed")
	base := []string{"--key", key, "--platform", "darwin-arm64", "--archive", archive,
		"--url", "https://example.com/Notes.zip", "-o", out}
	cases := map[string][]string{
		"non-semver version": append(append([]string{}, base...), "--version", "1.2"),
		"bad platform":       append(append([]string{}, base[:2]...), "--version", "1.2.0", "--platform", "darwin arm64", "--archive", archive, "--url", "https://e.com/a.zip", "-o", out),
		"plain http url":     append(append([]string{}, base...), "--version", "1.0.0", "--url", "http://example.com/Notes.zip"),
	}
	for name, args := range cases {
		_, code := verbOutput(t, func() { runDesktopFeed(args) })
		if code != 1 {
			t.Errorf("%s accepted (exit %d)", name, code)
		}
	}
	// A non-zip archive and a zip without a single .app root.
	txt := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(txt, []byte("text"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, code := verbOutput(t, func() {
		runDesktopFeed([]string{"--key", key, "--version", "1.2.0", "--platform", "darwin-arm64",
			"--archive", txt, "--url", "https://e.com/a.zip", "-o", out})
	})
	if code != 1 {
		t.Errorf("non-zip archive accepted (exit %d)", code)
	}
	badZip := filepath.Join(dir, "noapp.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("readme.txt")
	_, _ = w.Write([]byte("hi"))
	_ = zw.Close()
	if err := os.WriteFile(badZip, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	_, code = verbOutput(t, func() {
		runDesktopFeed([]string{"--key", key, "--version", "1.2.0", "--platform", "darwin-arm64",
			"--archive", badZip, "--url", "https://e.com/a.zip", "-o", out})
	})
	if code != 1 {
		t.Errorf("archive without a .app root accepted (exit %d)", code)
	}
	// A missing required flag exits 1 naming it.
	outStr, code := verbOutput(t, func() { runDesktopFeed([]string{"--key", key}) })
	if code != 1 || !strings.Contains(outStr, "--version") {
		t.Fatalf("missing-flag feedback = %q, exit %d", outStr, code)
	}
}

func TestDesktopExtraVerbRouting(t *testing.T) {
	if desktopExtraVerb(nil) {
		t.Fatal("empty args handled as a verb")
	}
	if desktopExtraVerb([]string{"nope"}) {
		t.Fatal("unknown verb claimed")
	}
	// keygen with no -o is handled (usage + exit 1), still true.
	_, code := verbOutput(t, func() { desktopExtraVerb([]string{"keygen"}) })
	if code != 1 {
		t.Fatalf("keygen without -o exited %d, want 1", code)
	}
	_, code = verbOutput(t, func() { desktopExtraVerb([]string{"feed"}) })
	if code != 1 {
		t.Fatalf("feed without flags exited %d, want 1", code)
	}
}

func TestDesktopFeedFlagParsing(t *testing.T) {
	f := parseDesktopFeedFlags([]string{
		"--key", "k", "--version", "1.2.0", "--notes", "n", "--platform", "darwin-arm64",
		"--archive", "a.zip", "--url", "https://e.com/a.zip", "-o", "/tmp/feed",
	})
	if f.key != "k" || f.version != "1.2.0" || f.notes != "n" || f.platform != "darwin-arm64" ||
		f.archive != "a.zip" || f.url != "https://e.com/a.zip" || f.out != "/tmp/feed" {
		t.Fatalf("flags = %+v", f)
	}
	def := parseDesktopFeedFlags([]string{"--key=k"})
	if def.out != "." {
		t.Fatalf("default out = %q, want .", def.out)
	}
}

// sum256 is the test's own sha256 so the manifest check does not lean
// on the code under test.
func sum256(b []byte) []byte {
	h := sha256.New()
	h.Write(b)
	return h.Sum(nil)
}
