package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxTarTotal caps the bytes a .tar.gz may expand to, across every entry.
const maxTarTotal = 2 << 30

// walkTarGz reads a .tar.gz whose entries all sit under one top-level
// directory and calls fn for each regular file or directory, with its
// cleaned name, in archive order. It refuses an entry that escapes the
// root, a second top-level name, a link, device, FIFO or any other
// type, a repeated name, more than maxZipEntries entries, an entry past
// maxEntrySize bytes and a total past maxTarTotal.
func walkTarGz(data []byte, fn func(name string, hdr *tar.Header, body io.Reader) error) (string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("%w: not a gzip archive", ErrBadArchive)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	root := ""
	seen := map[string]bool{}
	var total int64
	for count := 0; ; count++ {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("%w: not a tar archive", ErrBadArchive)
		}
		if count >= maxZipEntries {
			return "", fmt.Errorf("%w: too many entries", ErrBadArchive)
		}
		name, ok := cleanEntryName(hdr.Name)
		if !ok {
			return "", fmt.Errorf("%w: entry escapes the extraction root", ErrBadArchive)
		}
		top, _, _ := strings.Cut(name, "/")
		if root == "" {
			root = top
		}
		if top != root {
			return "", fmt.Errorf("%w: archive holds more than one top-level directory", ErrBadArchive)
		}
		if seen[name] {
			return "", fmt.Errorf("%w: the archive holds an entry twice", ErrBadArchive)
		}
		seen[name] = true
		switch hdr.Typeflag {
		case tar.TypeDir:
		case tar.TypeReg:
			if name == root {
				return "", fmt.Errorf("%w: the archive root is not a directory", ErrBadArchive)
			}
			if hdr.Size < 0 || hdr.Size > maxEntrySize {
				return "", fmt.Errorf("%w: entry too large", ErrBadArchive)
			}
			total += hdr.Size
			if total > maxTarTotal {
				return "", fmt.Errorf("%w: archive expands too large", ErrBadArchive)
			}
		default:
			return "", fmt.Errorf("%w: entry is not a regular file or directory", ErrBadArchive)
		}
		if err := fn(name, hdr, tr); err != nil {
			return "", err
		}
	}
	if root == "" {
		return "", fmt.Errorf("%w: archive is empty", ErrBadArchive)
	}
	return root, nil
}

// ExtractTarGz extracts a verified .tar.gz into the existing directory
// dest under walkTarGz's rules, with file modes from the archive less
// group and other write, setuid, setgid and sticky, and directories
// owner-writable, as ExtractZip does. It returns the path of the one
// top-level directory.
func ExtractTarGz(data []byte, dest string) (string, error) {
	root, err := walkTarGz(data, func(name string, hdr *tar.Header, body io.Reader) error {
		target := filepath.Join(dest, filepath.FromSlash(name))
		mode := hdr.FileInfo().Mode()
		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, dirPerm(mode)); err != nil {
				return fmt.Errorf("%w: extract failed", ErrBadArchive)
			}
			if err := os.Chmod(target, dirPerm(mode)); err != nil {
				return fmt.Errorf("%w: extract failed", ErrBadArchive)
			}
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("%w: extract failed", ErrBadArchive)
		}
		perm := filePerm(mode)
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
		if err != nil {
			return fmt.Errorf("%w: extract failed", ErrBadArchive)
		}
		n, err := io.Copy(out, io.LimitReader(body, hdr.Size))
		modeErr := out.Chmod(perm)
		cerr := out.Close()
		if err != nil || n != hdr.Size || modeErr != nil || cerr != nil {
			return fmt.Errorf("%w: extract failed", ErrBadArchive)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return filepath.Join(dest, root), nil
}
