package codegen

import (
	"bytes"
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

// ConflictPolicy controls what WriteFiles does when a target file already
// exists on disk with different content.
type ConflictPolicy int

const (
	// ConflictOverwrite always writes — the legacy behavior, correct for a
	// quarantined output dir that is cleaned-and-rewritten each run.
	ConflictOverwrite ConflictPolicy = iota
	// ConflictSkip never clobbers an existing file: absent → write,
	// identical → no-op, differs → skip and report. This is the owned-scaffold
	// re-run contract — re-running generate adds new files without
	// overwriting code the user has hand-edited.
	ConflictSkip
)

// WriteOptions controls writing a FileSet to disk.
type WriteOptions struct {
	OutputRoot   string
	Clean        bool
	SkipManifest bool
	// Conflict selects the existing-file policy. Defaults to ConflictOverwrite
	// so existing callers are unchanged.
	Conflict ConflictPolicy
	// OnConflict, if set, is called with the slash-relative path of each
	// existing file that was skipped because it differs from the generated
	// content (ConflictSkip only). Use it to warn the user.
	OnConflict func(relPath string)
}

// WriteFiles writes generated files and records a manifest for future cleans.
func WriteFiles(files *FileSet, opts WriteOptions) error {
	// The module root (".") is a legal target only when we are NOT cleaning
	// (clean-wipe of the cwd is what SafeOutputRoot guards against). The
	// owned-scaffold path writes individual known files to root, so it never
	// deletes anything — safe even under ConflictOverwrite (--force).
	allowCWD := !opts.Clean
	root, err := safeWriteRoot(opts.OutputRoot, allowCWD)
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
		if opts.Conflict == ConflictSkip {
			if existing, readErr := os.ReadFile(path); readErr == nil {
				if bytes.Equal(existing, []byte(file.Content)) {
					continue // identical — nothing to do
				}
				// Exists and differs: never clobber owned code.
				if opts.OnConflict != nil {
					opts.OnConflict(file.Path)
				}
				continue
			}
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

// SafeOutputRoot validates a root that may be cleaned. The working directory
// itself is rejected — clean-wipe of the cwd is never safe.
func SafeOutputRoot(root string) (string, error) {
	return safeWriteRoot(root, false)
}

// safeWriteRoot validates an output root. When allowCWD is true the module
// root (".", or an empty string) is permitted — used by the owned-scaffold
// path, which writes individual known files with ConflictSkip and never
// deletes anything.
func safeWriteRoot(root string, allowCWD bool) (string, error) {
	if strings.TrimSpace(root) == "" {
		if allowCWD {
			return ".", nil
		}
		return "", fmt.Errorf("codegen output must not be empty")
	}
	clean := filepath.Clean(root)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("codegen output must be relative to the project (got %q)", root)
	}
	if clean == ".." {
		return "", fmt.Errorf("codegen output %q would target the working directory", root)
	}
	if clean == "." && !allowCWD {
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
	volume := filepath.VolumeName(clean)
	if volume != "" {
		current = volume
		clean = strings.TrimPrefix(clean, volume)
		if strings.HasPrefix(clean, string(filepath.Separator)) {
			current += string(filepath.Separator)
			clean = strings.TrimLeft(clean, `\/`)
		}
	} else if filepath.IsAbs(clean) {
		current = string(filepath.Separator)
		clean = strings.TrimLeft(clean, `\/`)
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
