package update

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFeedRefusesInvalidChannelVersion(t *testing.T) {
	for _, version := range []string{"", "1.2", "not-a-version"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			keyPath, _, pub, privateKey := writeKeyFile(t, dir)
			archive := writeArchive(t, dir)
			manifest, signature, err := SignFeed(Feed{
				Expires:  testExpires(),
				Version:  "1.2.0",
				Channels: map[string]Release{"preview": {Version: version}},
			}, privateKey)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseFeed(manifest, signature, pub, time.Now()); err == nil {
				t.Fatal("invalid channel fixture was accepted by the updater")
			}
			for name, data := range map[string][]byte{"manifest.json": manifest, "manifest.json.sig": signature} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err = WriteFeed(dir, keyPath, "1.2.0", "", "darwin-arm64", archive, "https://example.com/a.zip", 0)
			if err == nil || !strings.Contains(err.Error(), `channel "preview" version`) {
				t.Errorf("invalid channel version error = %v", err)
			}
			for name, want := range map[string][]byte{"manifest.json": manifest, "manifest.json.sig": signature} {
				got, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || !bytes.Equal(got, want) {
					t.Errorf("refusal changed %s: %v", name, err)
				}
			}
		})
	}
}

func TestFeedKeepsValidChannel(t *testing.T) {
	dir := t.TempDir()
	keyPath, _, pub, privateKey := writeKeyFile(t, dir)
	archive := writeArchive(t, dir)
	channel := Release{Version: "1.3.0-beta.1", Notes: "preview notes", Platforms: map[string]PlatformEntry{
		"darwin-amd64": {URL: "https://example.com/preview.zip", SHA256: strings.Repeat("a", 64), Size: 123},
	}}
	manifest, _, err := SignFeed(Feed{Expires: testExpires(), Version: "1.2.0", Channels: map[string]Release{"preview": channel}}, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFeed(dir, keyPath, "1.2.0", "", "darwin-arm64", archive, "https://example.com/a.zip", 0); err != nil {
		t.Fatal(err)
	}
	manifest, err = os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	signature, err := os.ReadFile(filepath.Join(dir, "manifest.json.sig"))
	if err != nil {
		t.Fatal(err)
	}
	feed, err := ParseFeed(manifest, signature, pub, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := feed.Select("preview")
	if got.Version != channel.Version || got.Notes != channel.Notes || got.Platforms["darwin-amd64"] != channel.Platforms["darwin-amd64"] {
		t.Fatalf("retained channel = %+v; want %+v", got, channel)
	}
}

func TestFeedKeepsOtherPlatforms(t *testing.T) {
	dir := t.TempDir()
	keyPath, _, pub, _ := writeKeyFile(t, dir)
	archive := writeArchive(t, dir)
	out := filepath.Join(dir, "feed")
	for _, platform := range []string{"darwin-arm64", "darwin-amd64"} {
		if err := WriteFeed(out, keyPath, "1.2.0", "notes", platform, archive, "https://example.com/"+platform+".zip", 0); err != nil {
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
	feed, err := ParseFeed(manifest, signature, pub, time.Now())
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
	manifest, _, err := SignFeed(Feed{Expires: testExpires(), Version: "1.1.0"}, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFeed(dir, keyPath, "1.2.0", "", "darwin-arm64", archive, "https://example.com/a.zip", 0); err == nil {
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
	for _, manifest := range []string{"{", "{}", "null", `{"version":null}`} {
		t.Run(manifest, func(t *testing.T) {
			dir := t.TempDir()
			keyPath, _, _, _ := writeKeyFile(t, dir)
			archive := writeArchive(t, dir)
			if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := WriteFeed(dir, keyPath, "1.2.0", "", "darwin-arm64", archive, "https://example.com/a.zip", 0); err == nil {
				t.Fatal("accepted a malformed existing manifest")
			}
		})
	}
}
