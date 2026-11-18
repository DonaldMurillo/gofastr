package sdk

import (
	"archive/zip"
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"
)

// PackZip builds a deterministic zip of files under prefix; escaping paths error.
func PackZip(prefix string, files []File) ([]byte, error) {
	if prefix != "" {
		clean := path.Clean(prefix)
		if strings.HasPrefix(prefix, "/") || clean == "." || clean == ".." ||
			strings.HasPrefix(clean, "../") || strings.Contains(prefix, "..") {
			return nil, fmt.Errorf("sdk: unsafe zip prefix %q", prefix)
		}
		prefix = clean
	}
	sorted := make([]File, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range sorted {
		if f.Path == "" || strings.HasPrefix(f.Path, "/") || strings.Contains(f.Path, "..") {
			return nil, fmt.Errorf("sdk: unsafe zip entry path %q", f.Path)
		}
		name := f.Path
		if prefix != "" {
			name = prefix + "/" + name
		}
		hdr := &zip.FileHeader{
			Name:   name,
			Method: zip.Deflate,
		}
		hdr.SetMode(0o644)
		fw, err := w.CreateHeader(hdr)
		if err != nil {
			return nil, fmt.Errorf("sdk: zip entry %q: %w", name, err)
		}
		if _, err := fw.Write(f.Data); err != nil {
			return nil, fmt.Errorf("sdk: zip entry %q: %w", name, err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("sdk: close zip: %w", err)
	}
	return buf.Bytes(), nil
}
