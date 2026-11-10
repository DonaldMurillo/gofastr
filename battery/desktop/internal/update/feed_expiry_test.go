package update

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// signedWithExpiry signs sampleFeed with expires as its expiry.
func signedWithExpiry(t *testing.T, expires string) (manifest, sig []byte, pub ed25519.PublicKey) {
	t.Helper()
	f := sampleFeed()
	f.Expires = expires
	manifest, sig, pub, _ = testFeed(t, f)
	return manifest, sig, pub
}

func TestParseFeedRefusesExpired(t *testing.T) {
	expires := time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC)
	manifest, sig, pub := signedWithExpiry(t, expires.Format(time.RFC3339))
	if _, err := ParseFeed(manifest, sig, pub, expires.Add(-time.Second)); err != nil {
		t.Fatalf("a second before expiry: %v", err)
	}
	// The expiry instant itself is already expired: no grace.
	for _, now := range []time.Time{expires, expires.Add(time.Second), expires.AddDate(1, 0, 0)} {
		if _, err := ParseFeed(manifest, sig, pub, now); !errors.Is(err, ErrFeedExpired) {
			t.Fatalf("at %s: err = %v, want ErrFeedExpired", now, err)
		}
	}
}

func TestParseFeedRefusesNoExpiry(t *testing.T) {
	now := time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC)
	for name, expires := range map[string]string{
		"absent":    "",
		"words":     "tomorrow",
		"date only": "2099-01-01",
		"unix":      "4102444800",
	} {
		t.Run(name, func(t *testing.T) {
			manifest, sig, pub := signedWithExpiry(t, expires)
			if _, err := ParseFeed(manifest, sig, pub, now); !errors.Is(err, ErrBadManifest) {
				t.Fatalf("err = %v, want ErrBadManifest", err)
			}
		})
	}
}

// writtenFeed runs WriteFeed into a new directory and returns the pair.
func writtenFeed(t *testing.T, validFor time.Duration) (manifest, sig []byte, pub ed25519.PublicKey) {
	t.Helper()
	dir := t.TempDir()
	keyPath, _, pub, _ := writeKeyFile(t, dir)
	out := filepath.Join(dir, "feed")
	if err := WriteFeed(out, keyPath, "1.2.0", "", "darwin-arm64", writeArchive(t, dir), "https://example.com/a.zip", validFor); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	sig, err = os.ReadFile(filepath.Join(out, "manifest.json.sig"))
	if err != nil {
		t.Fatal(err)
	}
	return manifest, sig, pub
}

func TestWriteFeedStampsExpiry(t *testing.T) {
	day := 24 * time.Hour
	for name, tc := range map[string]struct {
		validFor time.Duration
		want     time.Duration
	}{
		"default": {0, 30 * day},
		"a week":  {7 * day, 7 * day},
		"longest": {MaxFeedLifetime, 365 * day},
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			manifest, sig, pub := writtenFeed(t, tc.validFor)
			if _, err := ParseFeed(manifest, sig, pub, start.Add(tc.want-time.Minute)); err != nil {
				t.Fatalf("a minute before the lifetime ends: %v", err)
			}
			if _, err := ParseFeed(manifest, sig, pub, start.Add(tc.want+time.Minute)); !errors.Is(err, ErrFeedExpired) {
				t.Fatalf("a minute past the lifetime: err = %v, want ErrFeedExpired", err)
			}
		})
	}
}

func TestWriteFeedRefusesBadLifetime(t *testing.T) {
	dir := t.TempDir()
	keyPath, _, _, _ := writeKeyFile(t, dir)
	archive := writeArchive(t, dir)
	for name, validFor := range map[string]time.Duration{
		"negative": -time.Hour,
		"too long": MaxFeedLifetime + time.Second,
	} {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(dir, name)
			if err := WriteFeed(out, keyPath, "1.2.0", "", "darwin-arm64", archive, "https://example.com/a.zip", validFor); err == nil {
				t.Fatal("accepted")
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatalf("a refused lifetime still wrote %s", out)
			}
		})
	}
}

// Signing the same release again is how a feed is renewed: the merge
// path must replace the old expiry, not keep it.
func TestWriteFeedRenewsExpiry(t *testing.T) {
	dir := t.TempDir()
	keyPath, _, pub, priv := writeKeyFile(t, dir)
	archive := writeArchive(t, dir)
	stale := Feed{Version: "1.2.0", Expires: "2020-01-01T00:00:00Z"}
	manifest, sig, err := SignFeed(stale, priv)
	if err != nil {
		t.Fatal(err)
	}
	writeExistingFeed(t, dir, manifest, sig)
	if err := WriteFeed(dir, keyPath, "1.2.0", "", "darwin-arm64", archive, "https://example.com/a.zip", 0); err != nil {
		t.Fatal(err)
	}
	manifest, _ = os.ReadFile(filepath.Join(dir, "manifest.json"))
	sig, _ = os.ReadFile(filepath.Join(dir, "manifest.json.sig"))
	if _, err := ParseFeed(manifest, sig, pub, time.Now()); err != nil {
		t.Fatalf("the renewed feed does not verify: %v", err)
	}
}
