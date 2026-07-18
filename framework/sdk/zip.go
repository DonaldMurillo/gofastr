package sdk

import (
	"archive/zip"
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// PackZip builds a deterministic zip archive: entries are written in sorted
// path order with zeroed timestamps and fixed permissions, so packing the
// same files twice yields byte-identical archives (regeneration on an
// unchanged schema must not churn the artifact or its hash).
//
// prefix, when non-empty, becomes the top-level directory every entry is
// placed under (e.g. "myapp-sdk" → "myapp-sdk/go.mod"), so extracting the
// archive never splats files into the current directory.
func PackZip(prefix string, files []File) ([]byte, error) {
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
			// Modified left zero: determinism beats mtime fidelity here.
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
