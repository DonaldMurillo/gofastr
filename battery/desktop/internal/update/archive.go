package update

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrBadArchive is the sentinel for an archive that fails any check:
// checksum, size, shape, or an entry that tries to escape the
// extraction root.
var ErrBadArchive = errors.New("update archive failed verification")

// maxEntrySize caps one extracted file (256 MiB, the archive cap, so a
// zip bomb written entirely into one entry cannot exceed the cap the
// download already enforced; the sum of entries is bounded by the
// verified archive's own compressed size times this guard's pair with
// the count check below).
const maxEntrySize = MaxArchiveSize

// maxZipEntries caps the entry count (a decompression bomb made of
// millions of tiny entries).
const maxZipEntries = 100_000

// VerifyArchive checks the sha256 and exact size of an already
// downloaded archive against the feed entry.
func VerifyArchive(data []byte, entry PlatformEntry) error {
	sum := sha256.Sum256(data)
	want, err := hex.DecodeString(entry.SHA256)
	if err != nil || len(want) != sha256.Size {
		return fmt.Errorf("%w: feed checksum is not sha256 hex", ErrBadArchive)
	}
	if !bytes.Equal(sum[:], want) {
		return fmt.Errorf("%w: checksum mismatch", ErrBadArchive)
	}
	if int64(len(data)) != entry.Size {
		return fmt.Errorf("%w: size mismatch", ErrBadArchive)
	}
	return nil
}

// ExtractZip extracts a verified archive into dest so that exactly one
// top-level directory named appName ("<Name>.app") exists, every entry
// stays under dest after path.Clean (the rootwrite posture), no entry
// is a symlink or device, and file modes come from the archive with
// the setuid/setgid/sticky bits stripped. It returns the extracted
// bundle's path (dest/appName).
func ExtractZip(data []byte, dest, appName string) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("%w: not a zip archive", ErrBadArchive)
	}
	if len(zr.File) == 0 {
		return "", fmt.Errorf("%w: archive is empty", ErrBadArchive)
	}
	if len(zr.File) > maxZipEntries {
		return "", fmt.Errorf("%w: too many entries", ErrBadArchive)
	}
	root := path.Clean(appName)
	if root == "." || root == ".." || strings.Contains(root, "/") {
		return "", fmt.Errorf("%w: bad bundle name", ErrBadArchive)
	}
	sawRoot := false
	for _, f := range zr.File {
		name, ok := cleanEntryName(f.Name)
		if !ok {
			return "", fmt.Errorf("%w: entry escapes the extraction root", ErrBadArchive)
		}
		top, _, found := strings.Cut(name, "/")
		if !found || top != root || name == root {
			// A file at the top level, or a directory other than the
			// single expected bundle root, is not a bundle.
			if name == root && f.FileInfo().IsDir() {
				sawRoot = true
				continue
			}
			return "", fmt.Errorf("%w: archive is not one %s bundle", ErrBadArchive, appName)
		}
		if f.FileInfo().IsDir() {
			sawRoot = true
		}
	}
	if !sawRoot {
		return "", fmt.Errorf("%w: archive has no %s root directory", ErrBadArchive, appName)
	}
	for _, f := range zr.File {
		name, _ := cleanEntryName(f.Name)
		if name == root && f.FileInfo().IsDir() {
			continue
		}
		if err := extractEntry(f, dest, name); err != nil {
			return "", err
		}
	}
	return filepath.Join(dest, appName), nil
}

// cleanEntryName normalizes one zip entry name and refuses any that
// could leave the extraction root: absolute paths, drive letters, dot
// segments, and backslashes (a "\" is a legal filename byte on unix,
// but no bundle we ship carries one, so refusing keeps the grammar
// closed).
func cleanEntryName(raw string) (string, bool) {
	if strings.Contains(raw, "\\") {
		return "", false
	}
	if len(raw) >= 2 && raw[1] == ':' {
		return "", false // windows drive letter
	}
	name := path.Clean(raw)
	if name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
		return "", false
	}
	return name, true
}

// extractEntry writes one zip entry under dest at its cleaned name.
func extractEntry(f *zip.File, dest, name string) error {
	mode := f.FileInfo().Mode()
	switch {
	case mode.IsDir():
		if err := os.MkdirAll(filepath.Join(dest, filepath.FromSlash(name)), dirPerm(mode)); err != nil {
			return fmt.Errorf("%w: extract failed", ErrBadArchive)
		}
		return nil
	case mode.Type() != 0:
		// Symlinks, devices, fifos: a bundle carries regular files
		// and directories only.
		return fmt.Errorf("%w: entry is not a regular file", ErrBadArchive)
	}
	if f.UncompressedSize64 > maxEntrySize {
		return fmt.Errorf("%w: entry too large", ErrBadArchive)
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("%w: open entry failed", ErrBadArchive)
	}
	defer rc.Close()
	target := filepath.Join(dest, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("%w: extract failed", ErrBadArchive)
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.FileInfo().Mode().Perm())
	if err != nil {
		return fmt.Errorf("%w: extract failed", ErrBadArchive)
	}
	// Cap the copy too: a lying zip header could stream more than
	// UncompressedSize64.
	n, err := io.CopyN(out, rc, maxEntrySize+1)
	cerr := out.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: extract failed", ErrBadArchive)
	}
	if n > maxEntrySize {
		return fmt.Errorf("%w: entry too large", ErrBadArchive)
	}
	if cerr != nil {
		return fmt.Errorf("%w: extract failed", ErrBadArchive)
	}
	return nil
}

// dirPerm keeps a directory mode owner-writable whatever the archive
// says (a bundle that could not be re-removed is worse than one with
// wider-than-recorded directory bits, and the staging root is 0700).
func dirPerm(mode fs.FileMode) fs.FileMode {
	p := mode.Perm()
	if p&0o700 != 0o700 {
		p = (p | 0o700) &^ 0o022 // group/other write never
	}
	return p
}
