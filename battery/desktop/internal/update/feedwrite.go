package update

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// The release-side helpers: reading a keygen key file, and writing a
// signed feed pair for the `gofastr desktop feed` verb.

// ErrBadKey is the sentinel for a key file that is not the keygen
// format (hex seed, or hex seed followed by hex public key).
var ErrBadKey = errors.New("update key file is malformed")

// keyHexLen is the length of one hex-encoded ed25519 component.
const keyHexLen = ed25519.PublicKeySize * 2

// LoadPrivateKey reads a keygen key file: 64 hex chars (seed) or 128
// hex chars (seed followed by the public key, cross-checked in
// constant time).
func LoadPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: unreadable", ErrBadKey)
	}
	hexStr := string(trimSpaceNewlines(raw))
	switch len(hexStr) {
	case keyHexLen:
		// seed only
	case 2 * keyHexLen:
		// seed + pub; the pub half must match the seed
	default:
		return nil, ErrBadKey
	}
	seedHex := hexStr[:keyHexLen]
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, ErrBadKey
	}
	priv := ed25519.NewKeyFromSeed(seed)
	if len(hexStr) == 2*keyHexLen {
		pubHex := hexStr[keyHexLen:]
		want := hex.EncodeToString(priv.Public().(ed25519.PublicKey))
		if !constantTimeEqualString(want, pubHex) {
			return nil, fmt.Errorf("%w: public key half does not match the seed", ErrBadKey)
		}
	}
	return priv, nil
}

// constantTimeEqualString compares two same-length hex strings without
// leaking position information (the strings are hex, so length equality
// is public).
func constantTimeEqualString(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range len(a) {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// trimSpaceNewlines trims ASCII whitespace around the key file body.
func trimSpaceNewlines(raw []byte) []byte {
	start, end := 0, len(raw)
	for start < end && isASCIISpace(raw[start]) {
		start++
	}
	for end > start && isASCIISpace(raw[end-1]) {
		end--
	}
	return raw[start:end]
}

func isASCIISpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

// rePlatformKey is the platform-entry grammar: GOOS-GOARCH shaped,
// lowercase alphanumerics joined by single dashes.
var rePlatformKey = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// WriteFeed builds the manifest for one platform archive (sha256 and
// size computed from the archive file), signs it with the key file's
// private key, and writes manifest.json and manifest.json.sig into
// outDir. It refuses a non-semver version, a platform key outside the
// grammar, an archive URL outside the https rule, and an archive that
// is not a zip with exactly one top-level directory whose name ends in
// .app.
func WriteFeed(outDir, keyPath, version, notes, platform, archivePath, archiveURL string) error {
	if _, err := ParseVersion(version); err != nil {
		return fmt.Errorf("version %q: %w", version, err)
	}
	if !rePlatformKey.MatchString(platform) {
		return fmt.Errorf("platform %q must be GOOS-GOARCH shaped (lowercase, dashes)", platform)
	}
	if !AllowedURL(archiveURL) {
		return ErrBadURL
	}
	priv, err := LoadPrivateKey(keyPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return fmt.Errorf("archive unreadable: %w", err)
	}
	if int64(len(data)) > MaxArchiveSize {
		return fmt.Errorf("archive exceeds the %d byte cap", int64(MaxArchiveSize))
	}
	if _, err := zipRootName(data); err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	feed := Feed{
		Version: version,
		Notes:   notes,
		Platforms: map[string]PlatformEntry{
			platform: {
				URL:    archiveURL,
				SHA256: hex.EncodeToString(sum[:]),
				Size:   int64(len(data)),
			},
		},
	}
	manifest, sigFile, err := SignFeed(feed, priv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}
	// The .sig verifies over the exact manifest bytes, so both files
	// are written atomically-enough for a release step: manifest first,
	// signature second, neither truncated by a later step.
	if err := fileperm.WriteOwnerOnly(filepath.Join(outDir, "manifest.json"), manifest); err != nil {
		return err
	}
	return fileperm.WriteOwnerOnly(filepath.Join(outDir, "manifest.json.sig"), sigFile)
}

// zipRootName reports the single top-level directory of a zip archive
// and requires its name to end in .app. It shares the entry-cleaning
// guard with ExtractZip.
func zipRootName(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("%w: not a zip archive", ErrBadArchive)
	}
	root := ""
	for _, f := range zr.File {
		name, ok := cleanEntryName(f.Name)
		if !ok {
			return "", fmt.Errorf("%w: entry escapes the root", ErrBadArchive)
		}
		top, _, _ := strings.Cut(name, "/")
		if top == name {
			continue // a file at the top level: ignored, the root check is on the tree
		}
		if root == "" {
			if !strings.HasSuffix(top, ".app") {
				return "", fmt.Errorf("%w: top-level directory %q is not a .app bundle", ErrBadArchive, top)
			}
			root = top
		} else if top != root {
			return "", fmt.Errorf("%w: more than one top-level directory", ErrBadArchive)
		}
	}
	if root == "" {
		return "", fmt.Errorf("%w: no top-level .app directory", ErrBadArchive)
	}
	return root, nil
}
