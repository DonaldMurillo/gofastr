package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// The feed format: manifest.json plus a detached manifest.json.sig.

// MaxManifestSize caps the manifest at 64 KiB. The manifest is signed
// metadata, not content; anything larger is a broken or hostile feed.
const MaxManifestSize = 64 << 10

// ErrBadSignature is the sentinel for a feed whose signature does not
// verify. It is returned before any parsing of the manifest happens.
var ErrBadSignature = errors.New("update feed signature is invalid")

// ErrBadManifest is the sentinel for a manifest that verifies but does
// not parse or validate.
var ErrBadManifest = errors.New("update feed manifest is malformed")

// ErrFeedExpired is the sentinel for a manifest that verifies and
// parses but whose signed expiry has passed. A captured manifest stays
// validly signed forever, so without an expiry whoever serves the feed
// could hold a client on an old release by replaying it.
var ErrFeedExpired = errors.New("update feed manifest has expired")

// The lifetime WriteFeed signs into a manifest: the default when the
// caller names none, and the longest it accepts. Past the expiry every
// client refuses the manifest until the publisher signs a new one.
const (
	DefaultFeedLifetime = 30 * 24 * time.Hour
	MaxFeedLifetime     = 365 * 24 * time.Hour
)

// PlatformEntry is one platform's download in the feed.
type PlatformEntry struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Release is one selectable version of the feed: the top level, or a
// channel entry (see Feed.Select).
type Release struct {
	Version   string                   `json:"version"`
	Notes     string                   `json:"notes"`
	Platforms map[string]PlatformEntry `json:"platforms"`
}

// Feed is the parsed manifest. Channels is optional: a Channel
// configured on the updater selects feed.channels[channel] when
// present, and the top level otherwise. Expires is the RFC 3339 time
// the manifest stops being valid; it covers every channel, and a
// manifest without one is refused.
type Feed struct {
	Version   string                   `json:"version"`
	Expires   string                   `json:"expires"`
	Notes     string                   `json:"notes"`
	Platforms map[string]PlatformEntry `json:"platforms"`
	Channels  map[string]Release       `json:"channels,omitempty"`
}

// Select returns the release the updater should follow: the named
// channel when the feed carries it, the top level otherwise.
func (f Feed) Select(channel string) Release {
	if channel != "" {
		if rel, ok := f.Channels[channel]; ok {
			return rel
		}
	}
	return Release{Version: f.Version, Notes: f.Notes, Platforms: f.Platforms}
}

// ErrNoPlatformEntry is the sentinel for a release that carries no
// entry for the running platform.
var ErrNoPlatformEntry = errors.New("update feed has no entry for this platform")

// Entry returns the release's download entry for one platform key
// ("darwin-arm64").
func (r Release) Entry(platformKey string) (PlatformEntry, error) {
	e, ok := r.Platforms[platformKey]
	if !ok {
		return PlatformEntry{}, ErrNoPlatformEntry
	}
	return e, nil
}

// ParseFeed verifies the detached signature over the exact manifest
// bytes FIRST (nothing is parsed, not even for the error message, when
// the signature fails), then parses and shape-checks the manifest, and
// last refuses one whose expiry is missing, malformed, or not after
// now. There is no grace period: the publisher chose the lifetime.
func ParseFeed(manifest, sigFile []byte, pub ed25519.PublicKey, now time.Time) (*Feed, error) {
	if len(manifest) > MaxManifestSize {
		return nil, fmt.Errorf("%w: manifest exceeds %d bytes", ErrBadManifest, MaxManifestSize)
	}
	if err := VerifyFeedSignature(manifest, sigFile, pub); err != nil {
		return nil, err
	}
	var f Feed
	if err := json.Unmarshal(manifest, &f); err != nil {
		return nil, fmt.Errorf("%w: not JSON", ErrBadManifest)
	}
	if f.Version == "" {
		return nil, fmt.Errorf("%w: no version", ErrBadManifest)
	}
	if _, err := ParseVersion(f.Version); err != nil {
		return nil, fmt.Errorf("%w: feed version: %v", ErrBadManifest, err)
	}
	for name, ch := range f.Channels {
		if _, err := ParseVersion(ch.Version); err != nil {
			return nil, fmt.Errorf("%w: channel %q version: %v", ErrBadManifest, name, err)
		}
	}
	if f.Expires == "" {
		return nil, fmt.Errorf("%w: no expires; sign the feed again with this version of gofastr desktop feed", ErrBadManifest)
	}
	expires, err := time.Parse(time.RFC3339, f.Expires)
	if err != nil {
		return nil, fmt.Errorf("%w: expires is not an RFC 3339 time", ErrBadManifest)
	}
	if !now.Before(expires) {
		return nil, ErrFeedExpired
	}
	return &f, nil
}

// VerifyFeedSignature decodes the .sig file (base64 of the raw 64-byte
// ed25519 signature) and verifies it over the exact manifest bytes.
func VerifyFeedSignature(manifest, sigFile []byte, pub ed25519.PublicKey) error {
	sig, err := base64.StdEncoding.DecodeString(string(sigFile))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrBadSignature
	}
	if !ed25519.Verify(pub, manifest, sig) {
		return ErrBadSignature
	}
	return nil
}

// SignFeed produces the manifest and signature file contents for one
// release. The manifest is deterministic (fixed field order via a
// struct) so the same inputs sign to the same bytes.
func SignFeed(f Feed, seed ed25519.PrivateKey) (manifest, sigFile []byte, err error) {
	manifest, err = json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	manifest = append(manifest, '\n')
	sig := ed25519.Sign(seed, manifest)
	return manifest, []byte(base64.StdEncoding.EncodeToString(sig)), nil
}
