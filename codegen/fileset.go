package codegen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ManifestName is the generated-file ownership manifest written under output roots.
const ManifestName = ".codegen-manifest.json"

// FileSet stores pending generated files and rejects silent collisions.
type FileSet struct {
	files map[string]GeneratedFile
}

// NewFileSet creates an empty pending generated-file collection.
func NewFileSet() *FileSet {
	return &FileSet{files: map[string]GeneratedFile{}}
}

// Add inserts one pending generated file and rejects path/content collisions.
func (fs *FileSet) Add(file GeneratedFile) error {
	path, err := SafeRelativePath(file.Path)
	if err != nil {
		return err
	}
	file.Path = path
	if existing, ok := fs.files[path]; ok {
		if existing.Content == file.Content && existing.Owner == file.Owner {
			return nil
		}
		return fmt.Errorf("generated file collision at %q between %q and %q", path, existing.Owner, file.Owner)
	}
	fs.files[path] = file
	return nil
}

// Delete removes a pending generated file by relative path.
func (fs *FileSet) Delete(path string) error {
	path, err := SafeRelativePath(path)
	if err != nil {
		return err
	}
	delete(fs.files, path)
	return nil
}

// DeleteOwned removes a pending generated file only if it is owned by owner.
func (fs *FileSet) DeleteOwned(path, owner string) error {
	path, err := SafeRelativePath(path)
	if err != nil {
		return err
	}
	existing, ok := fs.files[path]
	if !ok {
		return nil
	}
	if existing.Owner != owner {
		return fmt.Errorf("generated file delete collision at %q between %q and %q", path, existing.Owner, owner)
	}
	delete(fs.files, path)
	return nil
}

// All returns pending files in stable path order.
func (fs *FileSet) All() []GeneratedFile {
	out := make([]GeneratedFile, 0, len(fs.files))
	for _, file := range fs.files {
		out = append(out, file)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// SafeRelativePath validates a generated path.
func SafeRelativePath(path string) (string, error) {
	path = strings.TrimSpace(filepath.ToSlash(path))
	if path == "" {
		return "", fmt.Errorf("generated path must not be empty")
	}
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("generated path %q must be relative", path)
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." || clean == ".." {
		return "", fmt.Errorf("generated path %q is not a file path", path)
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return "", fmt.Errorf("generated path %q must not contain parent traversal", path)
		}
	}
	return clean, nil
}

// WriteOptions controls writing a FileSet to disk.
type WriteOptions struct {
	OutputRoot   string
	Clean        bool
	SkipManifest bool
}

// WriteFiles writes generated files and records a manifest for future cleans.
func WriteFiles(files *FileSet, opts WriteOptions) error {
	root, err := SafeOutputRoot(opts.OutputRoot)
	if err != nil {
		return err
	}
	if err := EnsureNoSymlinkPath(root); err != nil {
		return err
	}
	if opts.Clean {
		if err := cleanManifestFiles(root); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if err := EnsureNoSymlinkPath(root); err != nil {
		return err
	}
	all := files.All()
	for _, file := range all {
		path := filepath.Join(root, filepath.FromSlash(file.Path))
		parent := filepath.Dir(path)
		if err := EnsureNoSymlinkPath(parent); err != nil {
			return err
		}
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
		if err := EnsureNoSymlinkPath(parent); err != nil {
			return err
		}
		if err := EnsureNoSymlinkLeaf(path); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(file.Content), 0o644); err != nil {
			return err
		}
	}
	if opts.SkipManifest {
		return nil
	}
	return writeManifest(root, all)
}

// SafeOutputRoot validates a root that may be cleaned.
func SafeOutputRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("codegen output must not be empty")
	}
	clean := filepath.Clean(root)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("codegen output must be relative to the project (got %q)", root)
	}
	if clean == "." || clean == ".." {
		return "", fmt.Errorf("codegen output %q would target the working directory", root)
	}
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		if part == ".." {
			return "", fmt.Errorf("codegen output %q must not contain parent traversal", root)
		}
	}
	return clean, nil
}

type manifest struct {
	Version int      `json:"version"`
	Files   []string `json:"files"`
}

func cleanManifestFiles(root string) error {
	if err := EnsureNoSymlinkPath(root); err != nil {
		return err
	}
	manifestPath := filepath.Join(root, ManifestName)
	if err := EnsureNoSymlinkLeaf(manifestPath); err != nil {
		return err
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("read codegen manifest: %w", err)
	}
	if m.Version != ProtocolVersion {
		return fmt.Errorf("codegen manifest version %d is not supported", m.Version)
	}
	for _, path := range m.Files {
		rel, err := SafeRelativePath(path)
		if err != nil {
			return err
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := EnsureNoSymlinkPath(filepath.Dir(path)); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		pruneEmptyParents(root, filepath.Dir(filepath.Join(root, filepath.FromSlash(rel))))
	}
	return nil
}

// EnsureNoSymlinkPath rejects a path whose existing components include symlinks.
func EnsureNoSymlinkPath(path string) error {
	clean := filepath.Clean(path)
	current := ""
	if filepath.IsAbs(clean) {
		current = string(filepath.Separator)
		clean = strings.TrimPrefix(clean, current)
	}
	parts := strings.Split(filepath.ToSlash(clean), "/")
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if current == "" {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through symlink %s", current)
		}
	}
	return nil
}

// EnsureNoSymlinkLeaf rejects path when the final component is an existing symlink.
func EnsureNoSymlinkLeaf(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write through symlink %s", path)
	}
	return nil
}

func pruneEmptyParents(root, dir string) {
	root = filepath.Clean(root)
	for filepath.Clean(dir) != root && strings.HasPrefix(filepath.Clean(dir), root) {
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

func writeManifest(root string, files []GeneratedFile) error {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	sort.Strings(paths)
	data, err := json.MarshalIndent(manifest{Version: ProtocolVersion, Files: paths}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(root, ManifestName)
	if err := EnsureNoSymlinkPath(filepath.Dir(path)); err != nil {
		return err
	}
	if err := EnsureNoSymlinkLeaf(path); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func prefixPath(prefix, path string) (string, error) {
	path, err := SafeRelativePath(path)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(prefix) == "" {
		return path, nil
	}
	prefix, err = SafeRelativePath(prefix)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join(prefix, path)), nil
}
