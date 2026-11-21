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

// ErrBadArchive is the sentinel for an archive that fails any check: checksum, size, shape, or an entry that tries to escape the extraction root.
var ErrBadArchive = errors.New("update archive failed verification")

const maxEntrySize = MaxArchiveSize

const maxZipEntries = 100_000

// VerifyArchive checks the sha256 and exact size of an already downloaded archive against the feed entry.
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
// the setuid/setgid/sticky bits stripped.
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
		if err := extractEntry(f, dest, name); err != nil {
			return "", err
		}
	}
	return filepath.Join(dest, appName), nil
}

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

func extractEntry(f *zip.File, dest, name string) error {
	mode := f.FileInfo().Mode()
	switch {
	case mode.IsDir():
		target := filepath.Join(dest, filepath.FromSlash(name))
		if err := os.MkdirAll(target, dirPerm(mode)); err != nil {
			return fmt.Errorf("%w: extract failed", ErrBadArchive)
		}
		if err := os.Chmod(target, dirPerm(mode)); err != nil {
			return fmt.Errorf("%w: extract failed", ErrBadArchive)
		}
		return nil
	case mode.Type() != 0:
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
	perm := filePerm(mode)
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("%w: extract failed", ErrBadArchive)
	}
	n, err := io.CopyN(out, rc, maxEntrySize+1)
	modeErr := out.Chmod(perm)
	cerr := out.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: extract failed", ErrBadArchive)
	}
	if n > maxEntrySize {
		return fmt.Errorf("%w: entry too large", ErrBadArchive)
	}
	if cerr != nil || modeErr != nil {
		return fmt.Errorf("%w: extract failed", ErrBadArchive)
	}
	return nil
}

func dirPerm(mode fs.FileMode) fs.FileMode {
	return (mode.Perm() | 0o700) &^ 0o022 // group/other write never
}

func filePerm(mode fs.FileMode) fs.FileMode {
	return mode.Perm() &^ 0o022
}
