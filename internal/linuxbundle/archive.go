package linuxbundle

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"time"
)

// Entry is one release archive member: Data, or the regular file at
// Source (the built executable, never a directory walk).
type Entry struct {
	Name   string
	Mode   int64
	Data   []byte
	Source string
}

// CopyTo writes the entry's bytes to dst.
func (e Entry) CopyTo(dst io.Writer) (int64, error) {
	if e.Source == "" {
		n, err := dst.Write(e.Data)
		return int64(n), err
	}
	st, err := os.Lstat(e.Source)
	if err != nil {
		return 0, err
	}
	if !st.Mode().IsRegular() {
		return 0, fmt.Errorf("archive source must be a regular file, not a symlink: %s", e.Source)
	}
	f, err := os.Open(e.Source)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return io.Copy(dst, f)
}

// WithManifest sorts the payload entries by name, inventories them into
// m.Files with their sizes and sha256, and returns them with the
// manifest.json they make appended last, as gofastr desktop build
// writes a bundle.
func WithManifest(m Manifest, entries []Entry) ([]Entry, error) {
	entries = slices.Clone(entries)
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	m.Files = nil
	for _, entry := range entries {
		h := sha256.New()
		size, err := entry.CopyTo(h)
		if err != nil {
			return nil, err
		}
		m.Files = append(m.Files, File{Path: entry.Name, Mode: fmt.Sprintf("%04o", entry.Mode), Size: size, SHA256: hex.EncodeToString(h.Sum(nil))})
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(entries, Entry{Name: ManifestName, Mode: 0o644, Data: append(data, '\n')}), nil
}

// WriteArchive writes entries under the directory root as a tar.gz with
// names in order, regular files only, modes 0644 or 0755 and the epoch
// as every time, so the same bytes make the same archive. It checks
// every name and mode before writing anything.
func WriteArchive(dst io.Writer, root string, entries []Entry) error {
	if root == "." || !fs.ValidPath(root) || strings.ContainsAny(root, "/\\\x00") || path.Clean(root) != root {
		return fmt.Errorf("unsafe archive root %q", root)
	}
	entries = slices.Clone(entries)
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	previous := ""
	for _, entry := range entries {
		if !fs.ValidPath(entry.Name) || strings.ContainsAny(entry.Name, "\\\x00") || path.Clean(entry.Name) != entry.Name || entry.Name == "." || entry.Name == previous {
			return fmt.Errorf("unsafe or duplicate archive path %q", entry.Name)
		}
		if entry.Mode != 0o644 && entry.Mode != 0o755 {
			return fmt.Errorf("unsupported archive mode %o", entry.Mode)
		}
		previous = entry.Name
	}
	gz := gzip.NewWriter(dst)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		size := int64(len(entry.Data))
		if entry.Source != "" {
			st, err := os.Lstat(entry.Source)
			if err != nil {
				return err
			}
			if !st.Mode().IsRegular() {
				return fmt.Errorf("archive source is not a regular file: %s", entry.Source)
			}
			size = st.Size()
		}
		header := &tar.Header{Name: path.Join(root, entry.Name), Typeflag: tar.TypeReg, Mode: entry.Mode, Size: size, ModTime: time.Unix(0, 0), Format: tar.FormatPAX}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if _, err := entry.CopyTo(tw); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}
