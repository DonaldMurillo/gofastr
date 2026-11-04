package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFeedKeepsOtherPlatforms(t *testing.T) {
	dir := t.TempDir()
	keyPath, _, pub, _ := writeKeyFile(t, dir)
	archive := writeArchive(t, dir)
	out := filepath.Join(dir, "feed")
	for _, platform := range []string{"darwin-arm64", "darwin-amd64"} {
		if err := WriteFeed(out, keyPath, "1.2.0", "notes", platform, archive, "https://example.com/"+platform+".zip"); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	signature, err := os.ReadFile(filepath.Join(out, "manifest.json.sig"))
	if err != nil {
		t.Fatal(err)
	}
	feed, err := ParseFeed(manifest, signature, pub)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Platforms) != 2 {
		t.Fatalf("platforms = %v; want both architectures", feed.Platforms)
	}
}

func TestFeedRefusesVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	keyPath, _, _, privateKey := writeKeyFile(t, dir)
	archive := writeArchive(t, dir)
	manifest, _, err := SignFeed(Feed{Version: "1.1.0"}, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFeed(dir, keyPath, "1.2.0", "", "darwin-arm64", archive, "https://example.com/a.zip"); err == nil {
		t.Fatal("accepted a different existing version")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(manifest) {
		t.Fatal("refusal changed the existing manifest")
	}
}

func TestFeedRefusesMalformedManifest(t *testing.T) {
	dir := t.TempDir()
	keyPath, _, _, _ := writeKeyFile(t, dir)
	archive := writeArchive(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFeed(dir, keyPath, "1.2.0", "", "darwin-arm64", archive, "https://example.com/a.zip"); err == nil {
		t.Fatal("accepted a malformed existing manifest")
	}
}
