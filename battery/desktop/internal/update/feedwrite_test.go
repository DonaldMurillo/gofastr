package update

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeKeyFile writes a keygen-format key pair and returns the paths.
func writeKeyFile(t *testing.T, dir string) (keyPath, pubPath string, pub ed25519.PublicKey, priv ed25519.PrivateKey) {
	t.Helper()
	pub, priv, _ = ed25519.GenerateKey(nil)
	seed := priv.Seed()
	body := hex.EncodeToString(seed) + hex.EncodeToString(pub)
	keyPath = filepath.Join(dir, "update.key")
	pubPath = keyPath + ".pub"
	if err := os.WriteFile(keyPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pubPath, []byte(hex.EncodeToString(pub)), 0o600); err != nil {
		t.Fatal(err)
	}
	return keyPath, pubPath, pub, priv
}

func writeArchive(t *testing.T, dir string) string {
	t.Helper()
	data := bundleZip(t)
	p := filepath.Join(dir, "Notes-1.2.0.zip")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWriteFeedProducesVerifiablePair(t *testing.T) {
	dir := t.TempDir()
	keyPath, _, pub, _ := writeKeyFile(t, dir)
	archive := writeArchive(t, dir)
	out := filepath.Join(dir, "feed")
	err := WriteFeed(out, keyPath, "1.2.0", "What changed", "darwin-arm64", archive,
		"https://example.com/Notes-1.2.0.zip")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile(filepath.Join(out, "manifest.json.sig"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFeed(manifest, sig, pub)
	if err != nil {
		t.Fatalf("the written pair does not verify: %v", err)
	}
	e, err := f.Select("").Entry("darwin-arm64")
	if err != nil {
		t.Fatal(err)
	}
	if f.Notes != "What changed" || e.URL != "https://example.com/Notes-1.2.0.zip" {
		t.Fatalf("feed = %+v entry = %+v", f, e)
	}
	// sha256 and size computed from the archive on disk.
	archiveBytes, _ := os.ReadFile(archive)
	if err := VerifyArchive(archiveBytes, e); err != nil {
		t.Fatalf("entry does not match its archive: %v", err)
	}
	// The output files are owner-only.
	for _, p := range []string{filepath.Join(out, "manifest.json"), filepath.Join(out, "manifest.json.sig")} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", p, info.Mode().Perm())
		}
	}
}

func TestWriteFeedRefusesBadInputs(t *testing.T) {
	dir := t.TempDir()
	keyPath, _, _, _ := writeKeyFile(t, dir)
	archive := writeArchive(t, dir)
	ok := func() error {
		return WriteFeed(filepath.Join(dir, "out"), keyPath, "1.2.0", "", "darwin-arm64", archive,
			"https://example.com/Notes.zip")
	}
	if err := ok(); err != nil {
		t.Fatalf("baseline write failed: %v", err)
	}
	bad := map[string]func() error{
		"non-semver version": func() error {
			return WriteFeed(filepath.Join(dir, "o1"), keyPath, "1.2", "", "darwin-arm64", archive, "https://e.com/a.zip")
		},
		"bad platform grammar": func() error {
			return WriteFeed(filepath.Join(dir, "o2"), keyPath, "1.2.0", "", "darwin arm64", archive, "https://e.com/a.zip")
		},
		"plain http url": func() error {
			return WriteFeed(filepath.Join(dir, "o3"), keyPath, "1.2.0", "", "darwin-arm64", archive, "http://example.com/a.zip")
		},
		"missing key file": func() error {
			return WriteFeed(filepath.Join(dir, "o4"), filepath.Join(dir, "nope.key"), "1.2.0", "", "darwin-arm64", archive, "https://e.com/a.zip")
		},
		"missing archive": func() error {
			return WriteFeed(filepath.Join(dir, "o5"), keyPath, "1.2.0", "", "darwin-arm64", filepath.Join(dir, "nope.zip"), "https://e.com/a.zip")
		},
		"not a zip": func() error {
			p := filepath.Join(dir, "plain.txt")
			if err := os.WriteFile(p, []byte("text"), 0o600); err != nil {
				t.Fatal(err)
			}
			return WriteFeed(filepath.Join(dir, "o6"), keyPath, "1.2.0", "", "darwin-arm64", p, "https://e.com/a.zip")
		},
		"no app root": func() error {
			p := filepath.Join(dir, "plain.zip")
			data := buildZip(t, map[string]struct {
				content []byte
				mode    fs.FileMode
			}{"readme.txt": {[]byte("hi"), 0o644}})
			if err := os.WriteFile(p, data, 0o600); err != nil {
				t.Fatal(err)
			}
			return WriteFeed(filepath.Join(dir, "o7"), keyPath, "1.2.0", "", "darwin-arm64", p, "https://e.com/a.zip")
		},
		"two app roots": func() error {
			p := filepath.Join(dir, "two.zip")
			data := buildZip(t, map[string]struct {
				content []byte
				mode    fs.FileMode
			}{
				"Notes.app/Contents/x": {[]byte("x"), 0o644},
				"B.app/x":              {[]byte("x"), 0o644},
			})
			if err := os.WriteFile(p, data, 0o600); err != nil {
				t.Fatal(err)
			}
			return WriteFeed(filepath.Join(dir, "o8"), keyPath, "1.2.0", "", "darwin-arm64", p, "https://e.com/a.zip")
		},
	}
	for name, fn := range bad {
		if err := fn(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestLoadPrivateKeyFormats(t *testing.T) {
	dir := t.TempDir()
	_, _, pub, priv := writeKeyFile(t, dir)
	seedHex := hex.EncodeToString(priv.Seed())

	// Seed-only file loads.
	p := filepath.Join(dir, "seed.key")
	if err := os.WriteFile(p, []byte(seedHex), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPrivateKey(p)
	if err != nil || !got.Equal(priv) {
		t.Fatalf("seed-only load = %v, %v", got, err)
	}

	// Seed+pub with a newline-wrapped body loads.
	p2 := filepath.Join(dir, "full.key")
	body := seedHex + hex.EncodeToString(pub)
	if err := os.WriteFile(p2, []byte("\n"+body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPrivateKey(p2); err != nil {
		t.Fatalf("seed+pub load: %v", err)
	}

	// A pub half that does not match the seed is refused.
	p3 := filepath.Join(dir, "wrong.key")
	// Flip the last hex digit so the pub half never matches, whatever
	// the key: a fixed "0" collided one time in sixteen.
	pubHex := hex.EncodeToString([]byte(pub))
	last := "0"
	if pubHex[63] == '0' {
		last = "1"
	}
	wrong := seedHex + pubHex[:63] + last
	if wrong == body {
		t.Fatal("test failed to build a mismatched pub half")
	}
	if err := os.WriteFile(p3, []byte(wrong), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPrivateKey(p3); !errors.Is(err, ErrBadKey) {
		t.Fatalf("mismatched pub err = %v, want ErrBadKey", err)
	}

	// Garbage lengths and non-hex bodies are refused.
	for _, content := range []string{"", "zz", strings.Repeat("a", 63), strings.Repeat("g", 128), "xx" + seedHex[2:]} {
		p := filepath.Join(dir, "bad.key")
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPrivateKey(p); !errors.Is(err, ErrBadKey) {
			t.Errorf("LoadPrivateKey(%d bytes) = %v, want ErrBadKey", len(content), err)
		}
	}
}
