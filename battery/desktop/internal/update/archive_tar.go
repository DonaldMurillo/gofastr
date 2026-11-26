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

	"github.com/DonaldMurillo/gofastr/internal/linuxbundle"
)

// The caps a .tar.gz release is held to: entries, bytes in one entry
// (an executable may be as large as desktop install accepts), and bytes
// across every entry. Variables so a test can lower them.
var (
	maxTarEntries       = maxZipEntries
	maxTarEntry   int64 = linuxbundle.MaxExecutable
	maxTarTotal   int64 = 2 << 30
	// maxTarStream caps every byte the gzip stream expands to, the tar
	// headers and the PAX records tar.Reader reads inside Next included,
	// which no per-entry count sees: the entries' total plus room for
	// their headers.
	maxTarStream int64 = maxTarTotal + 64<<20
)

// errTarStream is the read error past maxTarStream.
var errTarStream = errors.New("tar stream past its cap")

// cappedReader fails a read once more than left bytes have come through.
type cappedReader struct {
	r    io.Reader
	left int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, errTarStream
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

// walkTarGz reads a .tar.gz whose entries all sit under one top-level
// directory and calls fn for each regular file or directory, with its
// cleaned name, in archive order. It refuses an entry that escapes the
// root, a second top-level name, a link, device, FIFO or any other
// type, a repeated name, more than maxTarEntries entries, an entry past
// maxTarEntry bytes and a total past maxTarTotal, each from its header
// before reading its body.
func walkTarGz(data []byte, fn func(name string, hdr *tar.Header, body io.Reader) error) (string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("%w: not a gzip archive", ErrBadArchive)
	}
	defer gz.Close()
	tr := tar.NewReader(&cappedReader{r: gz, left: maxTarStream})
	root := ""
	seen := map[string]bool{}
	var total int64
	for count := 0; ; count++ {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if errors.Is(err, errTarStream) {
			return "", fmt.Errorf("%w: archive expands too large", ErrBadArchive)
		}
		if err != nil {
			return "", fmt.Errorf("%w: not a tar archive", ErrBadArchive)
		}
		if count >= maxTarEntries {
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
			if hdr.Size < 0 || hdr.Size > maxTarEntry {
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
