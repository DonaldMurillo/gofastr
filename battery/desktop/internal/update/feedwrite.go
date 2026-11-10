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

	"github.com/DonaldMurillo/gofastr/internal/filelock"
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
// grammar, an archive URL outside the https rule, an archive that is
// not a zip with exactly one top-level directory, and an archive the
// updater's own ExtractZip refuses. A darwin-* archive's directory must
// end in .app and its Contents/Info.plist must declare version as its
// CFBundleShortVersionString. A windows-* archive's directory must hold
// exactly <dir>.exe and WebView2Loader.dll, and the executable's
// ProductVersion must be version. An existing manifest in outDir must
// have the same version; its other platforms and channels are retained
// and the merged manifest is signed again.
//
// validFor is how long the manifest stays valid from now: zero means
// DefaultFeedLifetime, and a negative one or one past MaxFeedLifetime
// is refused. Every write stamps a new expiry, so running it again with
// the same inputs is how a publisher renews a feed that has no newer
// release.
//
// manifest.json and manifest.json.sig are written 0644, and a directory
// this call creates is 0755: the pair is public by design, served as it
// lies by a static web server running as another user. The lock file
// beside them stays owner-only.
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
	// A windows-* entry carries the executable and its loader under one
	// directory named for the executable; every other platform carries
	// one .app bundle.
	windows := strings.HasPrefix(platform, "windows-")
	root, err := zipRootName(data, !windows)
	if err != nil {
		return err
	}
	// Every client runs ExtractZip on the download, so the feed runs it
	// too: an archive the updater refuses (a stray top-level README.txt
	// or .DS_Store, no directory entry for the bundle root, a symlink,
	// an oversized entry) is never signed. The label must be the
	// bundle's own version: a feed calling a 0.1.0 bundle 1.2.0
	// reinstalls it on every check, and an old archive under a new
	// label is a downgrade the updater cannot see.
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

// makeFeedDir makes the feed's output directory. One this call creates
// is 0755 whatever the umask, since a web server reads the feed from
// it; one that already exists keeps the mode its owner gave it.
func makeFeedDir(outDir string) error {
	if err := os.MkdirAll(filepath.Dir(outDir), 0o755); err != nil {
		return err
	}
	err := os.Mkdir(outDir, 0o755)
	if err == nil {
		return os.Chmod(outDir, 0o755)
	}
	if st, statErr := os.Stat(outDir); statErr == nil && st.IsDir() {
		return nil
	}
	return err
}

// writePublic writes one public feed file as 0644, on create and on
// overwrite alike: the mode is set on the open handle, so a file an
// older release wrote 0600, or one created under a tight umask, comes
// out readable by the web server too.
func writePublic(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// zipRootName names the archive's top-level directory and, when
// requireApp is set, requires it to end in .app. It shares the entry-cleaning guard with ExtractZip;
// the full shape check (no top-level files, a directory entry for the
// root) is ExtractZip's, which extractCheck runs.
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

// extractCheck extracts the archive into a throwaway 0700 directory with
// the same ExtractZip every client runs, so the release side and the
// install side cannot disagree on what a valid archive is.
func extractCheck(data []byte, root string) error {
	tmp, err := os.MkdirTemp("", "gofastr-feed-check-")
	if err != nil {
		return fmt.Errorf("archive check: %w", err)
	}
	defer os.RemoveAll(tmp)
	_, err = ExtractZip(data, tmp, root)
	return err
}

// windowsArchiveInfo extracts a Windows archive the way the client
// does, checks it holds exactly the executable and its loader, and
// reads the identity the executable's version resource declares.
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
