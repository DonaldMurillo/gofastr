// Package settings finds and checks gofastr.settings.yml, a project's
// committed gofastr configuration, and decodes and rewrites its endpoints and
// unmounted sections.
package settings

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

const maxSettingsBytes = 4 << 20 // 4 MiB cap on gofastr.settings.yml

// FileName is the settings file's name at the project root.
const FileName = "gofastr.settings.yml"

var sections = map[string]bool{
	"version":   true,
	"codegen":   true,
	"contracts": true,
	"endpoints": true,
	"isolation": true,
	"plugins":   true,
	"unmounted": true,
}

// IsSection reports whether name is a section other than version.
func IsSection(name string) bool { return name != "version" && sections[name] }

// File is a parsed settings file.
type File struct {
	// Path is the file's path, absolute when Find located it.
	Path string
	// Dir is the directory holding the file.
	Dir  string
	root *coreyaml.Node
}

// Section returns the named top-level section, or nil.
func (f File) Section(name string) *coreyaml.Node {
	if f.root == nil || f.root.Kind != coreyaml.Map {
		return nil
	}
	return f.root.Map[name]
}

// Root returns the whole parsed document.
func (f File) Root() *coreyaml.Node { return f.root }

var retired = map[string][]string{
	"codegen":   {"gofastr.codegen.yml", "gofastr.codegen.yaml"},
	"contracts": {"gofastr.contracts.yml", "gofastr.contracts.yaml"},
	"isolation": {"gofastr.isolation.yml", "gofastr.isolation.yaml"},
}

// Find loads the first FileName in SearchDirs(start); ok is false when there
// is none. It errors when a directory searched holds the retired file of
// section (gofastr.codegen.yml for codegen).
func Find(start, section string) (f File, ok bool, err error) {
	dirs, err := SearchDirs(start)
	if err != nil {
		return File{}, false, err
	}
	for _, dir := range dirs {
		for _, name := range retired[section] {
			old := filepath.Join(dir, name)
			if _, err := os.Stat(old); err == nil {
				return File{}, false, fmt.Errorf("%s is no longer read: move its content under %q in %s", old, section+":", filepath.Join(dir, FileName))
			}
		}
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, FileName)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return File{}, false, err
		}
		f, err := Load(path)
		if err != nil {
			return File{}, false, err
		}
		return f, true, nil
	}
	return File{}, false, nil
}

// Load reads and checks the settings file at path. It refuses non-regular
// files (such as devices or FIFOs) without blocking and caps reads at
// maxSettingsBytes so a hostile symlink cannot hang or exhaust memory.
func Load(path string) (File, error) {
	f, err := openRegularReadOnly(path)
	if err != nil {
		return File{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSettingsBytes+1))
	if err != nil {
		return File{}, err
	}
	if len(data) > maxSettingsBytes {
		return File{}, fmt.Errorf("%s: settings file exceeds %d byte limit", path, maxSettingsBytes)
	}
	return Parse(path, data)
}

// Parse checks a settings document: a map of known sections, with version 1
// when a version is given.
func Parse(path string, data []byte) (File, error) {
	root, err := coreyaml.Parse(string(data))
	if err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	if root.Kind != coreyaml.Map {
		return File{}, fmt.Errorf("%s: the settings file must be a map of sections (%s)", path, sectionList())
	}
	for _, key := range slices.Sorted(maps.Keys(root.Map)) {
		if !sections[key] {
			return File{}, fmt.Errorf("%s: unknown section %q; the settings file holds %s", path, key, sectionList())
		}
	}
	if v := root.Map["version"]; v != nil {
		if n, ok := v.Value.(int64); v.Kind != coreyaml.Scalar || !ok || n != 1 {
			return File{}, fmt.Errorf("%s: version %v is not supported; the settings file format is version 1", path, v.Value)
		}
	}
	return File{Path: path, Dir: filepath.Dir(path), root: root}, nil
}

func sectionList() string {
	var names []string
	for _, k := range slices.Sorted(maps.Keys(sections)) {
		if k != "version" {
			names = append(names, k)
		}
	}
	return strings.Join(names, ", ")
}

// SearchDirs lists start and each parent up to and including the project
// root, or start alone when no directory above it is a project root.
func SearchDirs(start string) ([]string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for {
		dirs = append(dirs, dir)
		if IsProjectRoot(dir) {
			return dirs, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return dirs[:1], nil
}

var projectRootMarkers = []string{"go.work", "go.mod", ".git"}

// IsProjectRoot reports whether dir holds go.work, go.mod or .git.
func IsProjectRoot(dir string) bool {
	for _, m := range projectRootMarkers {
		if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}
	return false
}
