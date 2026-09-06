package update

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// testFeed builds and signs one feed, returning the manifest bytes, the
// sig-file bytes, and the public key.
func testFeed(t *testing.T, f Feed) (manifest, sig []byte, pub ed25519.PublicKey, priv ed25519.PrivateKey) {
	t.Helper()
	_, wrong, _ := ed25519.GenerateKey(nil)
	_ = wrong
	pub, priv, _ = ed25519.GenerateKey(nil)
	manifest, sig, err := SignFeed(f, priv)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, sig, pub, priv
}

func sampleFeed() Feed {
	return Feed{
		Version: "1.2.0",
		Notes:   "What changed",
		Platforms: map[string]PlatformEntry{
			"darwin-arm64": {URL: "https://example.com/Notes-1.2.0.zip", SHA256: strings.Repeat("0", 64), Size: 12},
		},
	}
}

func TestParseFeedRoundTrip(t *testing.T) {
	manifest, sig, pub, _ := testFeed(t, sampleFeed())
	f, err := ParseFeed(manifest, sig, pub)
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != "1.2.0" || f.Notes != "What changed" {
		t.Fatalf("feed = %+v", f)
	}
	e, err := f.Select("").Entry("darwin-arm64")
	if err != nil || e.Size != 12 {
		t.Fatalf("entry = %+v, %v", e, err)
	}
}

func TestParseFeedRejectsBadSignatureBeforeParse(t *testing.T) {
	// A manifest that is not JSON at all, carrying a bad signature:
	// the SIGNATURE error must win (nothing is parsed first).
	manifest, sig, pub, _ := testFeed(t, sampleFeed())
	bad := append([]byte(nil), manifest...)
	bad[0] = '{'
	bad[len(bad)-2] = 'x' // corrupt the JSON tail
	_, err := ParseFeed(bad, sig, pub)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature before any parsing", err)
	}
	// A tampered but valid-JSON manifest fails the signature too.
	tampered := append([]byte(nil), manifest...)
	tampered[len(tampered)-3] = '9'
	_, err = ParseFeed(tampered, sig, pub)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered err = %v, want ErrBadSignature", err)
	}
	// A signature from a different key fails.
	_, otherPriv, _ := ed25519.GenerateKey(nil)
	_, wrongSig, err := SignFeed(sampleFeed(), otherPriv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFeed(manifest, wrongSig, pub); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong-key err = %v, want ErrBadSignature", err)
	}
	// Garbage in the .sig file fails as a signature error, not a decode
	// panic.
	if _, err := ParseFeed(manifest, []byte("!!!not base64!!!"), pub); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("garbage sig err = %v, want ErrBadSignature", err)
	}
	// A truncated (not 64-byte) signature fails.
	short := []byte("QUJDRA==") // "ABCD"
	if _, err := ParseFeed(manifest, short, pub); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("short sig err = %v, want ErrBadSignature", err)
	}
}

func TestParseFeedRejectsOversizeManifest(t *testing.T) {
	f := sampleFeed()
	f.Notes = strings.Repeat("n", MaxManifestSize) // pushes it over 64 KiB
	manifest, sig, pub, priv := testFeed(t, f)
	if len(manifest) <= MaxManifestSize {
		t.Fatalf("test manifest is %d bytes, need over %d", len(manifest), MaxManifestSize)
	}
	// Even a VALID signature over an oversize manifest is refused.
	if _, err := ParseFeed(manifest, sig, pub); !errors.Is(err, ErrBadManifest) {
		t.Fatalf("err = %v, want ErrBadManifest for oversize", err)
	}
	_ = priv
}

func TestParseFeedRejectsBadSemver(t *testing.T) {
	f := sampleFeed()
	f.Version = "1.2" // not major.minor.patch
	manifest, sig, pub, _ := testFeed(t, f)
	if _, err := ParseFeed(manifest, sig, pub); !errors.Is(err, ErrBadManifest) {
		t.Fatalf("err = %v, want ErrBadManifest", err)
	}
	// A bad version inside a channel entry too.
	f = sampleFeed()
	f.Channels = map[string]Release{"beta": {Version: "beta", Platforms: f.Platforms}}
	manifest, sig, pub, _ = testFeed(t, f)
	if _, err := ParseFeed(manifest, sig, pub); !errors.Is(err, ErrBadManifest) {
		t.Fatalf("channel err = %v, want ErrBadManifest", err)
	}
}

func TestParseFeedRejectsMissingVersion(t *testing.T) {
	f := sampleFeed()
	f.Version = ""
	manifest, sig, pub, _ := testFeed(t, f)
	if _, err := ParseFeed(manifest, sig, pub); !errors.Is(err, ErrBadManifest) {
		t.Fatalf("err = %v, want ErrBadManifest", err)
	}
}

func TestFeedSelectPrefersChannelWhenPresent(t *testing.T) {
	f := sampleFeed()
	f.Channels = map[string]Release{
		"beta": {Version: "1.3.0-beta.1", Notes: "beta notes", Platforms: map[string]PlatformEntry{
			"darwin-arm64": {URL: "https://example.com/b.zip", SHA256: strings.Repeat("1", 64), Size: 9},
		}},
	}
	manifest, sig, pub, _ := testFeed(t, f)
	parsed, err := ParseFeed(manifest, sig, pub)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Select("beta"); got.Version != "1.3.0-beta.1" || got.Notes != "beta notes" {
		t.Fatalf("Select(beta) = %+v", got)
	}
	// An absent channel falls back to the top level.
	if got := parsed.Select("nightly"); got.Version != "1.2.0" {
		t.Fatalf("Select(nightly) = %+v, want the top level", got)
	}
}

func TestReleaseEntryMissingPlatform(t *testing.T) {
	rel := sampleFeed().Select("")
	if _, err := rel.Entry("windows-amd64"); !errors.Is(err, ErrNoPlatformEntry) {
		t.Fatalf("err = %v, want ErrNoPlatformEntry", err)
	}
}

func TestSignFeedDeterministic(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	m1, s1, err := SignFeed(sampleFeed(), priv)
	if err != nil {
		t.Fatal(err)
	}
	m2, s2, _ := SignFeed(sampleFeed(), priv)
	if !bytes.Equal(m1, m2) || !bytes.Equal(s1, s2) {
		t.Fatal("SignFeed is not deterministic for identical input")
	}
	// The manifest is JSON with a trailing newline; the signature
	// verifies over exactly those bytes.
	var raw map[string]any
	if err := json.Unmarshal(m1, &raw); err != nil {
		t.Fatalf("manifest is not JSON: %v", err)
	}
	if m1[len(m1)-1] != '\n' {
		t.Fatal("manifest lacks the trailing newline the signature covers")
	}
}
