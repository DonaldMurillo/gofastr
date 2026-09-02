// Package a holds the rootwrite fixture reduced from the real bug
// sites: framework/contracts report.go containedPath/Apply as it was
// before fix 77fdbaf4 (probe TestApplyRefusesSymlinkEscape) and
// framework/sdk zip.go PackZip before fix 1501a555 (probe
// TestPackZipPrefixCannotEscapeDir), each with its fixed spelling next
// to it.
package a

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// File is one entry handed to PackZip.
type File struct {
	Path string
	Data []byte
}

// containedPath is the pre-fix containment check: Join plus a prefix
// comparison, which a symlinked directory component steps over.
func containedPath(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("apply fixes to %s: absolute paths are not applied", rel)
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	base := filepath.Clean(root)
	if abs != base && !strings.HasPrefix(abs, base+string(filepath.Separator)) {
		return "", fmt.Errorf("apply fixes to %s: resolves outside the project root", rel)
	}
	return abs, nil
}

// Report carries the root that Apply writes under.
type Report struct {
	Root string
}

// Apply, reduced: writes the fix under r.Root through containedPath.
// Pre-fix, a rel crossing a symlinked directory escaped the root.
func (r *Report) Apply(rel string, out []byte) error {
	abs, err := containedPath(r.Root, rel)
	if err != nil {
		return err
	}
	if err := os.WriteFile(abs, out, 0o644); err != nil { // want `write under a root with lexical containment only`
		return err
	}
	return nil
}

// containedPathFixed is the fix posture: resolve symlinks on both sides
// before comparing.
func containedPathFixed(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("apply fixes to %s: absolute paths are not applied", rel)
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	base := filepath.Clean(root)
	if abs != base && !strings.HasPrefix(abs, base+string(filepath.Separator)) {
		return "", fmt.Errorf("apply fixes to %s: resolves outside the project root", rel)
	}
	realAbs, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("apply fixes to %s: %w", rel, err)
	}
	realRoot, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("apply fixes: resolve root: %w", err)
	}
	if realAbs != realRoot && !strings.HasPrefix(realAbs, realRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("apply fixes to %s: symlink resolves outside the project root", rel)
	}
	return abs, nil
}

// ApplyFixed writes through the symlink-resolving containment check.
func (r *Report) ApplyFixed(rel string, out []byte) error {
	abs, err := containedPathFixed(r.Root, rel)
	if err != nil {
		return err
	}
	return os.WriteFile(abs, out, 0o644)
}

// readUnderRoot is a read: containment is not a write-safety property
// here, and the rule stays quiet by construction.
func (r *Report) readUnderRoot(rel string) ([]byte, error) {
	abs, err := containedPath(r.Root, rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

// literalJoinOnly appends nothing caller-controlled under the root.
func literalJoinOnly(root string) error {
	return os.WriteFile(filepath.Join(root, "manifest.json"), []byte("{}"), 0o644)
}

// stageInTmp writes under a throwaway directory: os.MkdirTemp roots are
// silent.
func stageInTmp(rel string) error {
	dir, err := os.MkdirTemp("", "stage")
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(dir, rel), 0o755)
}

// PackZip is the pre-fix packer: the prefix parameter is concatenated
// into entry names with no Clean, so a "../" prefix placed entries
// above the target directory on extract.
func PackZip(prefix string, files []File) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range files {
		name := f.Path
		if prefix != "" {
			name = prefix + "/" + name
		}
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		fw, err := w.CreateHeader(hdr) // want `zip entry name built from a parameter without path.Clean`
		if err != nil {
			return nil, fmt.Errorf("zip entry %q: %w", name, err)
		}
		if _, err := fw.Write(f.Data); err != nil {
			return nil, fmt.Errorf("zip entry %q: %w", name, err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// PackZipFixed cleans the prefix and rejects escapes.
func PackZipFixed(prefix string, files []File) ([]byte, error) {
	if prefix != "" {
		clean := path.Clean(prefix)
		if strings.HasPrefix(prefix, "/") || clean == "." || clean == ".." ||
			strings.HasPrefix(clean, "../") || strings.Contains(prefix, "..") {
			return nil, fmt.Errorf("unsafe zip prefix %q", prefix)
		}
		prefix = clean
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range files {
		name := f.Path
		if prefix != "" {
			name = prefix + "/" + name
		}
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		fw, err := w.CreateHeader(hdr)
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(f.Data); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// literalEntryNames assemble names from literals only: nothing
// caller-controlled reaches the archive.
func literalEntryNames(files []File) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for range files {
		if _, err := w.Create("static/" + "payload"); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), w.Close()
}

// writeJSON forwards its own name parameter to Create verbatim: the
// wrapper composes nothing (its callers do), so it stays quiet under
// the composition requirement.
func writeJSON(zw *zip.Writer, name string, body []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}
