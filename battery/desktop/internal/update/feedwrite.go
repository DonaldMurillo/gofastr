package update

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/atomicfile"
	"github.com/DonaldMurillo/gofastr/internal/filelock"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// ErrBadKey is the sentinel for a key file that is not the keygen format (hex seed, or hex seed followed by hex public key).
var ErrBadKey = errors.New("update key file is malformed")

const keyHexLen = ed25519.PublicKeySize * 2

// LoadPrivateKey reads a keygen key file: 64 hex chars (seed) or 128 hex chars (seed followed by the public key, cross-checked in constant time).
func LoadPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: unreadable", ErrBadKey)
	}
	hexStr := string(trimSpaceNewlines(raw))
	switch len(hexStr) {
	case keyHexLen:
	case 2 * keyHexLen:
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

var rePlatformKey = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// WriteFeed builds the manifest for one platform archive (sha256 and
// size computed from the archive file), signs it with the key file's
// private key, and writes manifest.json and manifest.json.sig into
// outDir.
func WriteFeed(outDir, keyPath, version, notes, platform, archivePath, archiveURL string, validFor time.Duration) error {
	if _, err := ParseVersion(version); err != nil {
		return fmt.Errorf("version %q: %w", version, err)
	}
	if validFor < 0 || validFor > MaxFeedLifetime {
		return fmt.Errorf("feed lifetime %s must be between 0 (the default, %s) and %s", validFor, DefaultFeedLifetime, MaxFeedLifetime)
	}
	if validFor == 0 {
		validFor = DefaultFeedLifetime
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
	windows := strings.HasPrefix(platform, "windows-")
	root, err := zipRootName(data, !windows)
	if err != nil {
		return err
	}
	if windows {
		info, err := windowsArchiveInfo(data, root)
		if err != nil {
			return err
		}
		if info.ShortVersion != version {
			return fmt.Errorf("--version %q does not match the archive: %s/%s.exe says ProductVersion %q; rebuild with gofastr desktop build --version %s", version, root, root, info.ShortVersion, version)
		}
	} else {
		if err := extractCheck(data, root); err != nil {
			return err
		}
		info, err := zipBundleInfo(data, root)
		if err != nil {
			return err
		}
		if info.ShortVersion != version {
			return fmt.Errorf("--version %q does not match the archive: %s/%s says CFBundleShortVersionString %q; rebuild with gofastr desktop build --version %s", version, root, infoPlistPath, info.ShortVersion, version)
		}
	}
	sum := sha256.Sum256(data)
	if err := makeFeedDir(outDir); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(outDir, ".manifest.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("feed output lock: %w", err)
	}
	defer lock.Close()
	if err := fileperm.RestrictOpenFile(lock); err != nil {
		return fmt.Errorf("feed output lock permissions: %w", err)
	}
	if err := filelock.Lock(lock); err != nil {
		return fmt.Errorf("feed output lock: %w", err)
	}
	feed := Feed{
		Version: version,
		Notes:   notes,
	}
	if existing, err := os.ReadFile(filepath.Join(outDir, "manifest.json")); err == nil {
		existingSig, sigErr := os.ReadFile(filepath.Join(outDir, "manifest.json.sig"))
		if sigErr != nil || VerifyFeedSignature(existing, existingSig, priv.Public().(ed25519.PublicKey)) != nil {
			return errors.New("existing manifest: manifest.json.sig is missing or is not this key's signature over manifest.json; remove both files or use a new output directory")
		}
		feed = Feed{}
		if err := json.Unmarshal(existing, &feed); err != nil {
			return fmt.Errorf("existing manifest: %w", err)
		}
		if feed.Version != version {
			return fmt.Errorf("existing manifest version %q differs from --version %q; use a new output directory", feed.Version, version)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("existing manifest: %w", err)
	}
	for _, name := range slices.Sorted(maps.Keys(feed.Channels)) {
		if _, err := ParseVersion(feed.Channels[name].Version); err != nil {
			return fmt.Errorf("existing manifest: channel %q version: %w", name, err)
		}
	}
	feed.Notes = notes
	feed.Expires = time.Now().UTC().Add(validFor).Truncate(time.Second).Format(time.RFC3339)
	if feed.Platforms == nil {
		feed.Platforms = make(map[string]PlatformEntry)
	}
	feed.Platforms[platform] = PlatformEntry{
		URL:    archiveURL,
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(data)),
	}
	manifest, sigFile, err := SignFeed(feed, priv)
	if err != nil {
		return err
	}
	if err := writePublic(filepath.Join(outDir, "manifest.json"), manifest); err != nil {
		return err
	}
	return writePublic(filepath.Join(outDir, "manifest.json.sig"), sigFile)
}

func makeFeedDir(outDir string) error {
	outDir = filepath.Clean(outDir)
	err := os.Mkdir(outDir, 0o755)
	if err == nil {
		return os.Chmod(outDir, 0o755)
	}
	if st, statErr := os.Stat(outDir); statErr == nil {
		if st.IsDir() {
			return nil
		}
		return err
	}
	parent := filepath.Dir(outDir)
	if parent == outDir || !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := makeFeedDir(parent); err != nil {
		return err
	}
	if err := os.Mkdir(outDir, 0o755); err != nil {
		if st, statErr := os.Stat(outDir); statErr == nil && st.IsDir() {
			return nil // another writer made it between the two calls
		}
		return err
	}
	return os.Chmod(outDir, 0o755)
}

func writePublic(path string, data []byte) error {
	return atomicfile.WriteFile(path, data, atomicfile.Mode(0o644))
}

func zipRootName(data []byte, requireApp bool) (string, error) {
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
			if requireApp && !strings.HasSuffix(top, ".app") {
				return "", fmt.Errorf("%w: top-level directory %q is not a .app bundle", ErrBadArchive, top)
			}
			root = top
		} else if top != root {
			return "", fmt.Errorf("%w: more than one top-level directory", ErrBadArchive)
		}
	}
	if root == "" {
		return "", fmt.Errorf("%w: no top-level bundle directory", ErrBadArchive)
	}
	return root, nil
}

func extractCheck(data []byte, root string) error {
	tmp, err := os.MkdirTemp("", "gofastr-feed-check-")
	if err != nil {
		return fmt.Errorf("archive check: %w", err)
	}
	defer os.RemoveAll(tmp)
	_, err = ExtractZip(data, tmp, root)
	return err
}

func windowsArchiveInfo(data []byte, root string) (BundleInfo, error) {
	tmp, err := os.MkdirTemp("", "gofastr-feed-check-")
	if err != nil {
		return BundleInfo{}, fmt.Errorf("archive check: %w", err)
	}
	defer os.RemoveAll(tmp)
	dir, err := ExtractZip(data, tmp, root)
	if err != nil {
		return BundleInfo{}, err
	}
	if err := CheckWindowsBundle(dir, root); err != nil {
		return BundleInfo{}, err
	}
	return ReadWindowsBundleInfo(dir, root)
}
